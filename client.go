// Package freetheai is the official Go client for FreeTheAI, the free,
// OpenAI-compatible AI API. It uses only the standard library.
//
//	client, err := freetheai.NewClient() // reads FREETHEAI_API_KEY
//	if err != nil {
//		log.Fatal(err)
//	}
//	reply, err := client.Chat.Completions.New(ctx, freetheai.ChatCompletionParams{
//		Model:    "glm-5.3",
//		Messages: []freetheai.Message{{Role: "user", Content: "Hello!"}},
//	})
package freetheai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Version is the module version, sent in the User-Agent header.
const Version = "0.1.0"

const (
	defaultBaseURL  = "https://api.freetheai.org/v1"
	defaultTimeout  = 10 * time.Minute
	modelCacheTTL   = 5 * time.Minute
	defaultMaxTries = 3
)

// Client is a FreeTheAI API client. It is safe for concurrent use.
type Client struct {
	apiKey          string
	baseURL         string
	maxRetries      int
	fallbackModels  []string
	maxConcurrency  int
	streamTransport bool
	timeout         time.Duration
	headers         http.Header
	httpClient      *http.Client
	gate            *gate

	modelMu      sync.Mutex
	modelIDs     []string
	modelIDsAt   time.Time
	modelsLoaded bool

	// Chat holds the chat completion resource.
	Chat *ChatService
	// Models lists the models this key can use.
	Models *ModelsService
	// Tasks reads image and video tasks.
	Tasks *TasksService
	// Videos starts video tasks.
	Videos *VideosService
	// Images starts image tasks.
	Images *ImagesService
}

// Option configures a Client. Pass options to NewClient.
type Option func(*Client)

// WithAPIKey sets the API key. Without it, NewClient reads FREETHEAI_API_KEY.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithBaseURL sets the API address. Without it, NewClient reads FREETHEAI_BASE_URL.
func WithBaseURL(base string) Option { return func(c *Client) { c.baseURL = base } }

// WithHTTPClient uses your own *http.Client (proxies, TLS, transports).
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.httpClient = hc } }

// WithMaxRetries sets the retries per model for retryable errors. The default is 3.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// WithFallbackModels sets the models tried in order after the first one fails.
func WithFallbackModels(models ...string) Option {
	return func(c *Client) { c.fallbackModels = append([]string(nil), models...) }
}

// WithMaxConcurrency caps the replies in flight. Without it, the client learns the account's limit.
func WithMaxConcurrency(n int) Option { return func(c *Client) { c.maxConcurrency = n } }

// WithStreamTransport turns the stream transport on or off. It is on by default: non-streamed
// chats are fetched as a stream and assembled, so long replies never idle out.
func WithStreamTransport(on bool) Option { return func(c *Client) { c.streamTransport = on } }

// WithTimeout sets how long a plain (non-streamed) request may take. The default is 10 minutes.
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// WithHeader adds a header to every request.
func WithHeader(key, value string) Option {
	return func(c *Client) { c.headers.Set(key, value) }
}

// NewClient creates a client. It fails when no key is given or the key is only part of a key.
func NewClient(opts ...Option) (*Client, error) {
	c := &Client{
		maxRetries:      defaultMaxTries,
		streamTransport: true,
		timeout:         defaultTimeout,
		headers:         http.Header{},
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.apiKey == "" {
		c.apiKey = os.Getenv("FREETHEAI_API_KEY")
	}
	if c.apiKey == "" {
		return nil, errors.New("freetheai: no API key. Pass WithAPIKey or set FREETHEAI_API_KEY " +
			"(create a key at https://freetheai.org/dashboard/api-keys)")
	}
	key, err := cleanKey(c.apiKey)
	if err != nil {
		return nil, err
	}
	c.apiKey = key
	if c.baseURL == "" {
		c.baseURL = os.Getenv("FREETHEAI_BASE_URL")
	}
	if c.baseURL == "" {
		c.baseURL = defaultBaseURL
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	c.gate = newGate(c.maxConcurrency)
	c.Chat = &ChatService{Completions: &CompletionsService{client: c}}
	c.Models = &ModelsService{client: c}
	c.Tasks = &TasksService{client: c}
	c.Videos = &VideosService{client: c}
	c.Images = &ImagesService{client: c}
	return c, nil
}

// APIKey returns the cleaned key in use.
func (c *Client) APIKey() string { return c.apiKey }

// BaseURL returns the API address in use.
func (c *Client) BaseURL() string { return c.baseURL }

// Search searches the web and returns results with title, url, and text.
func (c *Client) Search(ctx context.Context, params SearchParams) (*SearchResponse, error) {
	var out SearchResponse
	if err := c.doJSON(ctx, http.MethodPost, "/search", nil, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Answer answers a question from current web sources and returns the answer and its sources.
func (c *Client) Answer(ctx context.Context, params SearchParams) (*AnswerResponse, error) {
	var out AnswerResponse
	if err := c.doJSON(ctx, http.MethodPost, "/answer", nil, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// chainFor returns the model followed by the fallbacks, without repeats.
// A non-nil override replaces the client's fallback list for one call.
func (c *Client) chainFor(model string, override []string) []string {
	fallbacks := c.fallbackModels
	if override != nil {
		fallbacks = override
	}
	chain := []string{model}
	for _, extra := range fallbacks {
		if !contains(chain, extra) {
			chain = append(chain, extra)
		}
	}
	return chain
}

// do makes one HTTP call with no retries. Error statuses come back as the matching error type.
// The returned cancel must be called once the body has been read or closed.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, streaming bool) (*http.Response, context.CancelFunc, error) {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("freetheai: encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	reqCtx, cancel := ctx, context.CancelFunc(func() {})
	if !streaming && c.timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, c.timeout)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, target, reader)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "freetheai-go/"+Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range c.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, &ConnectionError{Message: fmt.Sprintf("could not reach the API: %v", err)}
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		cancel()
		return nil, nil, errorFor(resp.StatusCode, raw, resp.Header)
	}
	return resp, cancel, nil
}

// doJSON runs a JSON call under the retry policy and decodes the answer into out.
func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	_, err := policy(ctx, c, []string{""}, false, false, func(ctx context.Context, _ string) (struct{}, error) {
		resp, cancel, err := c.do(ctx, method, path, query, body, false)
		if err != nil {
			return struct{}{}, err
		}
		defer cancel()
		defer resp.Body.Close()
		if out == nil {
			return struct{}{}, nil
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return struct{}{}, &ConnectionError{Message: fmt.Sprintf("could not read the API answer: %v", err)}
		}
		return struct{}{}, nil
	})
	return err
}

// fetchModelIDs returns the model IDs, cached for five minutes. The cache starts empty.
func (c *Client) fetchModelIDs(ctx context.Context) ([]string, error) {
	c.modelMu.Lock()
	defer c.modelMu.Unlock()
	if c.modelsLoaded && time.Since(c.modelIDsAt) <= modelCacheTTL {
		return c.modelIDs, nil
	}
	var payload struct {
		Data []Model `json:"data"`
	}
	resp, cancel, err := c.do(ctx, http.MethodGet, "/models", nil, nil, false)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(payload.Data))
	for _, model := range payload.Data {
		ids = append(ids, model.ID)
	}
	c.modelIDs = ids
	c.modelIDsAt = time.Now()
	c.modelsLoaded = true
	return ids, nil
}

// suggest adds close model IDs to an unknown-model error and returns the ID the model
// clearly meant (only the "fta/" prefix missing), or "".
func (c *Client) suggest(ctx context.Context, found *ModelNotFoundError, model string) string {
	ids, err := c.fetchModelIDs(ctx)
	if err != nil {
		return ""
	}
	found.Suggestions = suggestModels(model, ids)
	if len(found.Suggestions) > 0 {
		found.Message += " Did you mean: " + strings.Join(found.Suggestions, ", ") + "?"
	}
	return fixable(model, found.Suggestions)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// policy runs attempt for each model in turn with the retry and fallback rules.
// With hold, the concurrency slot stays taken after success; the caller releases it.
func policy[T any](
	ctx context.Context,
	c *Client,
	models []string,
	gated bool,
	hold bool,
	attempt func(ctx context.Context, model string) (T, error),
) (T, error) {
	var zero T
	var last error
	for index := 0; index < len(models); index++ {
		tries := 0
		for {
			if gated {
				if err := c.gate.acquire(ctx); err != nil {
					return zero, err
				}
			}
			result, err := attempt(ctx, models[index])
			if err == nil {
				if gated && !hold {
					c.gate.release()
				}
				return result, nil
			}
			if gated {
				var limited *ConcurrencyLimitError
				if errors.As(err, &limited) {
					c.gate.refused()
				}
				c.gate.release()
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return zero, ctxErr
			}
			var notFound *ModelNotFoundError
			if errors.As(err, &notFound) && models[index] != "" {
				if fixed := c.suggest(ctx, notFound, models[index]); fixed != "" && !contains(models, fixed) {
					models[index] = fixed
					continue
				}
			}
			next := decide(err, tries, c.maxRetries, index+1 < len(models))
			if next.action == actionRaise {
				return zero, err
			}
			last = err
			if next.action == actionFallback {
				break
			}
			tries++
			if waitErr := sleepFor(ctx, next.delay); waitErr != nil {
				return zero, waitErr
			}
		}
	}
	if last == nil {
		last = errors.New("freetheai: no model answered")
	}
	return zero, last
}

// cleanKey removes copy-paste debris and refuses a key that is only part of one.
func cleanKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	key = strings.Trim(key, "\"'")
	key = strings.TrimSpace(key)
	if len(key) >= 7 && strings.EqualFold(key[:7], "bearer ") {
		key = strings.TrimSpace(key[7:])
	}
	if strings.HasPrefix(key, "ftai_") && !keyPattern.MatchString(key) {
		return "", &IncompleteKeyError{APIError{
			StatusCode: 401,
			Message: "This is not a complete FreeTheAI API key. A key starts with ftai_ and is 53 characters long; " +
				"it is shown only once, when you create it. Create a new key at https://freetheai.org/dashboard/api-keys.",
		}}
	}
	return key, nil
}
