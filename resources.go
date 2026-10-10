package freetheai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Model is one entry from the models list. ID is what you pass as the model.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	Created int64  `json:"created,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

// ModelsService lists models.
type ModelsService struct {
	client *Client
}

// List returns every model this key can use.
func (s *ModelsService) List(ctx context.Context) ([]Model, error) {
	var payload struct {
		Data []Model `json:"data"`
	}
	if err := s.client.doJSON(ctx, http.MethodGet, "/models", nil, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

// Task is an image or video task.
type Task struct {
	ID          string       `json:"id"`
	Object      string       `json:"object,omitempty"`
	Kind        string       `json:"kind,omitempty"`
	Model       string       `json:"model,omitempty"`
	Status      string       `json:"status"`
	CreatedAt   int64        `json:"created_at,omitempty"`
	CompletedAt int64        `json:"completed_at,omitempty"`
	ExpiresAt   int64        `json:"expires_at,omitempty"`
	ContentURL  string       `json:"content_url,omitempty"`
	Data        []TaskOutput `json:"data,omitempty"`
	Error       *TaskError   `json:"error,omitempty"`
}

// TaskOutput is one finished image: base64 bytes, or a download URL.
type TaskOutput struct {
	B64JSON string `json:"b64_json,omitempty"`
	URL     string `json:"url,omitempty"`
}

// TaskError says why a task failed.
type TaskError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	ErrorID string `json:"error_id,omitempty"`
}

// WaitOptions tunes how Tasks.Wait polls.
type WaitOptions struct {
	// PollInterval is the wait between polls. Default 5 seconds.
	PollInterval time.Duration
	// Timeout gives up after this long. Default 30 minutes.
	Timeout time.Duration
}

// TasksService reads image and video tasks.
type TasksService struct {
	client *Client
}

// List returns your tasks, newest first. Limit 0 uses the API default (20).
func (s *TasksService) List(ctx context.Context, limit int) ([]Task, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var payload struct {
		Data []Task `json:"data"`
	}
	if err := s.client.doJSON(ctx, http.MethodGet, "/tasks", query, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

// Get returns one task by ID.
func (s *TasksService) Get(ctx context.Context, id string) (*Task, error) {
	var task Task
	if err := s.client.doJSON(ctx, http.MethodGet, "/tasks/"+url.PathEscape(id), nil, nil, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// Wait polls until the task finishes. It returns a *TaskFailedError when the task failed,
// and an error when the timeout passes first.
func (s *TasksService) Wait(ctx context.Context, id string, opts WaitOptions) (*Task, error) {
	interval := opts.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		task, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		switch task.Status {
		case "completed":
			return task, nil
		case "failed", "cancelled", "expired":
			return nil, taskFailure(task)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("freetheai: task %s did not finish in %s", id, timeout)
		}
		if err := sleepFor(ctx, interval); err != nil {
			return nil, err
		}
	}
}

// Content downloads a finished task's output (image or video bytes). index picks one of several outputs.
func (s *TasksService) Content(ctx context.Context, id string, index int) ([]byte, error) {
	return downloadBytes(ctx, s.client, "/tasks/"+url.PathEscape(id)+"/content", index)
}

func taskFailure(task *Task) error {
	failure := &TaskFailedError{Message: "task " + task.Status, Task: task}
	if task.Error != nil {
		if task.Error.Message != "" {
			failure.Message = task.Error.Message
		}
		failure.Code = task.Error.Code
		failure.ErrorID = task.Error.ErrorID
	}
	return failure
}

// downloadBytes fetches a binary body under the retry policy.
func downloadBytes(ctx context.Context, c *Client, path string, index int) ([]byte, error) {
	query := url.Values{"index": {strconv.Itoa(index)}}
	return policy(ctx, c, []string{""}, false, false, func(ctx context.Context, _ string) ([]byte, error) {
		resp, cancel, err := c.do(ctx, http.MethodGet, path, query, nil, false)
		if err != nil {
			return nil, err
		}
		defer cancel()
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, &ConnectionError{Message: fmt.Sprintf("could not read the file: %v", err)}
		}
		return raw, nil
	})
}

// VideoCreateParams starts a video task.
type VideoCreateParams struct {
	Model string `json:"model"`
	// Prompt says what should happen in the video (up to 7000 characters).
	Prompt string `json:"prompt,omitempty"`
	// InputReference is the first frame: an https image URL or a base64 data URL.
	InputReference string `json:"input_reference,omitempty"`
	// Seconds is the duration, as a string such as "6".
	Seconds string `json:"seconds,omitempty"`
	// Size is the output size as WIDTHxHEIGHT, such as "720x1280".
	Size string `json:"size,omitempty"`
}

// VideosService starts video tasks.
type VideosService struct {
	client *Client
}

// Create starts a video task. Wait for it with Tasks.Wait.
func (s *VideosService) Create(ctx context.Context, params VideoCreateParams) (*Task, error) {
	var task Task
	if err := s.client.doJSON(ctx, http.MethodPost, "/videos", nil, params, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// ImageGenerateParams starts an image task.
type ImageGenerateParams struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	// N is how many images, 1 to 4 (default 1).
	N int `json:"n,omitempty"`
	// Size is WIDTHxHEIGHT, such as "1024x1024".
	Size string `json:"size,omitempty"`
	// ResponseFormat is "b64_json" (default) or "url".
	ResponseFormat string `json:"response_format,omitempty"`
	// Image is an optional source image: an https URL or a base64 data URL.
	Image string `json:"image,omitempty"`
}

// ImagesService starts image tasks.
type ImagesService struct {
	client *Client
}

// Generate starts an image task. Wait for it with Tasks.Wait.
func (s *ImagesService) Generate(ctx context.Context, params ImageGenerateParams) (*Task, error) {
	var task Task
	if err := s.client.doJSON(ctx, http.MethodPost, "/images/generations", nil, params, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// SearchParams is the input for Search and Answer.
type SearchParams struct {
	Query string `json:"query"`
	// NumResults is 1 to 25 (default 10).
	NumResults int `json:"num_results,omitempty"`
}

// SearchResult is one web page.
type SearchResult struct {
	Title         string   `json:"title,omitempty"`
	URL           string   `json:"url,omitempty"`
	Text          string   `json:"text,omitempty"`
	Highlights    []string `json:"highlights,omitempty"`
	PublishedDate string   `json:"publishedDate,omitempty"`
	Score         float64  `json:"score,omitempty"`
}

// SearchResponse is the result of Client.Search.
type SearchResponse struct {
	Query       string         `json:"query"`
	Results     []SearchResult `json:"results"`
	Suggestions []string       `json:"suggestions,omitempty"`
	DailyUsed   int            `json:"daily_used,omitempty"`
	DailyLimit  int            `json:"daily_limit,omitempty"`
}

// AnswerResponse is the result of Client.Answer.
type AnswerResponse struct {
	Query      string         `json:"query"`
	Answer     string         `json:"answer"`
	Sources    []SearchResult `json:"sources"`
	DailyUsed  int            `json:"daily_used,omitempty"`
	DailyLimit int            `json:"daily_limit,omitempty"`
}

// errNoModel is returned when no model answered and nothing else explains why.
var errNoModel = errors.New("freetheai: no model answered")
