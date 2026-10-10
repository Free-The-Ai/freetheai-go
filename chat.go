package freetheai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ChatService groups the chat resources.
type ChatService struct {
	// Completions creates chat completions.
	Completions *CompletionsService
}

// CompletionsService creates chat completions.
type CompletionsService struct {
	client *Client
}

// Message is one message you send. Content is a string, or a []ContentPart for models that read images.
type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ContentPart is one piece of multimodal content: text, or an image URL.
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL points at an image for a ContentPart.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// Tool declares a function the model may call.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a callable function. Parameters is a JSON Schema object.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToolCall is a function call the model made.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction holds the name and JSON arguments of a call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionParams is the request for a chat completion.
//
// Known fields are typed; Extra holds any other Chat Completions field and is sent as it is.
type ChatCompletionParams struct {
	// Model is an ID from Client.Models.List, such as "fta/zai/glm-5.3".
	Model    string    `json:"-"`
	Messages []Message `json:"messages"`
	// FallbackModels overrides the client's fallback list for this call. Nil keeps the client's list.
	FallbackModels []string `json:"-"`

	Temperature     *float64       `json:"temperature,omitempty"`
	TopP            *float64       `json:"top_p,omitempty"`
	MaxTokens       *int           `json:"max_tokens,omitempty"`
	Stop            []string       `json:"stop,omitempty"`
	Seed            *int64         `json:"seed,omitempty"`
	Tools           []Tool         `json:"tools,omitempty"`
	ToolChoice      any            `json:"tool_choice,omitempty"`
	ResponseFormat  any            `json:"response_format,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	WebSearch       map[string]any `json:"web_search_options,omitempty"`
	Extra           map[string]any `json:"-"`
}

// body builds the wire body for one attempt on model.
func (p ChatCompletionParams) body(model string, stream bool) (map[string]any, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	for key, value := range p.Extra {
		if _, taken := out[key]; !taken {
			out[key] = value
		}
	}
	out["model"] = model
	if stream {
		out["stream"] = true
	}
	return out, nil
}

// ChatCompletion is a finished (non-streamed) chat reply.
type ChatCompletion struct {
	ID      string       `json:"id,omitempty"`
	Object  string       `json:"object,omitempty"`
	Created int64        `json:"created,omitempty"`
	Model   string       `json:"model,omitempty"`
	Choices []ChatChoice `json:"choices"`
	Usage   *Usage       `json:"usage,omitempty"`
}

// ChatChoice is one choice of a ChatCompletion.
type ChatChoice struct {
	Index        int             `json:"index"`
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// ResponseMessage is the assistant's reply.
type ResponseMessage struct {
	Role             string       `json:"role"`
	Content          string       `json:"content"`
	ReasoningContent string       `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall   `json:"tool_calls,omitempty"`
	Annotations      []Annotation `json:"annotations,omitempty"`
}

// Annotation is a web source a search-enabled model used (url_citation).
type Annotation struct {
	Type        string       `json:"type"`
	URLCitation *URLCitation `json:"url_citation,omitempty"`
}

// URLCitation is the page a web search answer came from.
type URLCitation struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// Usage reports token counts. The final streamed chunk carries it.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk is one streamed piece of a reply.
type ChatCompletionChunk struct {
	ID      string        `json:"id,omitempty"`
	Model   string        `json:"model,omitempty"`
	Created int64         `json:"created,omitempty"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

// ChunkChoice is one choice of a streamed chunk.
type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason string     `json:"finish_reason"`
}

// ChunkDelta is the new text or tool-call pieces in a chunk.
type ChunkDelta struct {
	Role             string          `json:"role,omitempty"`
	Content          string          `json:"content,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ToolCalls        []ChunkToolCall `json:"tool_calls,omitempty"`
	Annotations      []Annotation    `json:"annotations,omitempty"`
}

// ChunkToolCall is a piece of a tool call; pieces with the same Index belong together.
type ChunkToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function"`
}

// New creates a chat completion and returns the whole reply.
// Fallback models are tried in order when a model is busy, filtered, unknown, or still failing.
func (s *CompletionsService) New(ctx context.Context, params ChatCompletionParams) (*ChatCompletion, error) {
	c := s.client
	models := c.chainFor(params.Model, params.FallbackModels)
	return policy(ctx, c, models, true, false, func(ctx context.Context, model string) (*ChatCompletion, error) {
		if !c.streamTransport {
			body, err := params.body(model, false)
			if err != nil {
				return nil, err
			}
			var out ChatCompletion
			if err := c.doJSON(ctx, http.MethodPost, "/chat/completions", nil, body, &out); err != nil {
				return nil, err
			}
			return &out, nil
		}
		body, err := params.body(model, true)
		if err != nil {
			return nil, err
		}
		resp, cancel, err := c.do(ctx, http.MethodPost, "/chat/completions", nil, body, true)
		if err != nil {
			return nil, err
		}
		defer cancel()
		defer resp.Body.Close()
		asm := newAssembler()
		reader := newSSEReader(resp.Body)
		for {
			data, ok, readErr := reader.next()
			if readErr != nil {
				return nil, readFailure(ctx, readErr, asm)
			}
			if !ok {
				break
			}
			chunk, decodeErr := decodeChunk(data)
			if decodeErr != nil {
				return nil, decodeErr
			}
			asm.feed(chunk)
		}
		if !asm.finished() {
			return nil, interrupted(asm, "")
		}
		return asm.result(), nil
	})
}

// NewStreaming starts a streamed chat completion. It returns once the first piece of the reply
// has arrived, so a busy model can fall back before anything is shown. Read it with Next, then
// check Err, and Close it when you are done (Close is safe to call more than once).
func (s *CompletionsService) NewStreaming(ctx context.Context, params ChatCompletionParams) (*ChatCompletionStream, error) {
	c := s.client
	models := c.chainFor(params.Model, params.FallbackModels)
	return policy(ctx, c, models, true, true, func(ctx context.Context, model string) (*ChatCompletionStream, error) {
		body, err := params.body(model, true)
		if err != nil {
			return nil, err
		}
		resp, cancel, err := c.do(ctx, http.MethodPost, "/chat/completions", nil, body, true)
		if err != nil {
			return nil, err
		}
		stream := &ChatCompletionStream{
			body:      resp.Body,
			cancel:    cancel,
			reader:    newSSEReader(resp.Body),
			assembler: newAssembler(),
			release:   c.gate.release,
		}
		for {
			data, ok, readErr := stream.reader.next()
			if readErr != nil {
				// The policy frees the slot on error; only the body is closed here.
				resp.Body.Close()
				cancel()
				return nil, readFailure(ctx, readErr, stream.assembler)
			}
			if !ok {
				resp.Body.Close()
				cancel()
				return nil, &StreamInterruptedError{Message: "the reply stream ended before any reply"}
			}
			chunk, decodeErr := decodeChunk(data)
			if decodeErr != nil {
				resp.Body.Close()
				cancel()
				return nil, decodeErr
			}
			stream.pending = append(stream.pending, chunk)
			if len(chunk.Choices) > 0 {
				return stream, nil
			}
		}
	})
}

// StreamText returns the reply text piece by piece, through the callback. It stops at the first
// error from the callback or from the stream.
func (s *CompletionsService) StreamText(ctx context.Context, params ChatCompletionParams, onText func(string) error) error {
	stream, err := s.NewStreaming(ctx, params)
	if err != nil {
		return err
	}
	defer stream.Close()
	for stream.Next() {
		for _, choice := range stream.Current().Choices {
			if choice.Delta.Content == "" {
				continue
			}
			if err := onText(choice.Delta.Content); err != nil {
				return err
			}
		}
	}
	return stream.Err()
}

// ChatCompletionStream reads a streamed reply chunk by chunk.
type ChatCompletionStream struct {
	body      io.ReadCloser
	cancel    func()
	reader    *sseReader
	assembler *assembler
	pending   []ChatCompletionChunk
	current   ChatCompletionChunk
	err       error
	done      bool
	release   func()
	closeOnce sync.Once
}

// Next advances to the next chunk. It returns false at the end of the reply or on error.
func (s *ChatCompletionStream) Next() bool {
	if s.done || s.err != nil {
		return false
	}
	var chunk ChatCompletionChunk
	if len(s.pending) > 0 {
		chunk = s.pending[0]
		s.pending = s.pending[1:]
	} else {
		data, ok, readErr := s.reader.next()
		if readErr != nil {
			s.fail(readFailure(context.Background(), readErr, s.assembler))
			return false
		}
		if !ok {
			s.finish()
			return false
		}
		decoded, decodeErr := decodeChunk(data)
		if decodeErr != nil {
			s.fail(decodeErr)
			return false
		}
		chunk = decoded
	}
	s.assembler.feed(chunk)
	s.current = chunk
	return true
}

// Current returns the chunk Next advanced to.
func (s *ChatCompletionStream) Current() ChatCompletionChunk { return s.current }

// Err returns the error that ended the stream, if any. A reply that stops before the model
// finished returns a *StreamInterruptedError with the text so far in Partial.
func (s *ChatCompletionStream) Err() error { return s.err }

// Text returns the reply text received so far.
func (s *ChatCompletionStream) Text() string { return s.assembler.text() }

// Close ends the stream and frees its concurrency slot. It is safe to call more than once.
func (s *ChatCompletionStream) Close() error {
	s.closeOnce.Do(func() {
		s.body.Close()
		s.cancel()
		s.release()
	})
	return nil
}

func (s *ChatCompletionStream) finish() {
	s.done = true
	if !s.assembler.finished() {
		s.err = interrupted(s.assembler, "")
	}
	s.Close()
}

func (s *ChatCompletionStream) fail(err error) {
	s.done = true
	var stopped *StreamInterruptedError
	if errors.As(err, &stopped) {
		stopped.Partial = s.assembler.text()
	}
	s.err = err
	s.Close()
}

// readFailure turns a read error from the body into a StreamInterruptedError, keeping the partial text.
func readFailure(ctx context.Context, err error, asm *assembler) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var stopped *StreamInterruptedError
	if errors.As(err, &stopped) {
		stopped.Partial = asm.text()
		return stopped
	}
	return interrupted(asm, "")
}

func interrupted(asm *assembler, errorID string) error {
	return &StreamInterruptedError{
		Message: "the reply stream stopped before the model finished",
		ErrorID: errorID,
		Partial: asm.text(),
	}
}

// decodeChunk parses one event's data. An error event mid-stream becomes a StreamInterruptedError.
func decodeChunk(data []byte) (ChatCompletionChunk, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return ChatCompletionChunk{}, &StreamInterruptedError{Message: "the reply stream sent a broken event"}
	}
	if _, hasError := raw["error"]; hasError {
		if _, hasChoices := raw["choices"]; !hasChoices {
			var detail struct {
				Error struct {
					Message string `json:"message"`
					ErrorID string `json:"error_id"`
				} `json:"error"`
			}
			_ = json.Unmarshal(data, &detail)
			message := detail.Error.Message
			if message == "" {
				message = "stream interrupted"
			}
			return ChatCompletionChunk{}, &StreamInterruptedError{Message: message, ErrorID: detail.Error.ErrorID}
		}
	}
	var chunk ChatCompletionChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return ChatCompletionChunk{}, &StreamInterruptedError{Message: "the reply stream sent a broken event"}
	}
	return chunk, nil
}

// sseReader yields the data of each server-sent event until [DONE] or the end of the body.
type sseReader struct {
	scanner *bufio.Scanner
	data    []string
	done    bool
}

func newSSEReader(body io.Reader) *sseReader {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	return &sseReader{scanner: scanner}
}

// next returns the next event's data. ok is false at [DONE] or the end of the stream.
func (r *sseReader) next() ([]byte, bool, error) {
	if r.done {
		return nil, false, nil
	}
	for r.scanner.Scan() {
		line := strings.TrimSuffix(r.scanner.Text(), "\r")
		if line == "" {
			if len(r.data) == 0 {
				continue
			}
			data := strings.Join(r.data, "\n")
			r.data = r.data[:0]
			if strings.TrimSpace(data) == "[DONE]" {
				r.done = true
				return nil, false, nil
			}
			return []byte(data), true, nil
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line[5:], " ")
			r.data = append(r.data, value)
		}
	}
	if err := r.scanner.Err(); err != nil {
		return nil, false, err
	}
	// The body ended without a blank line: flush what is left.
	if len(r.data) > 0 {
		data := strings.Join(r.data, "\n")
		r.data = nil
		if strings.TrimSpace(data) == "[DONE]" {
			return nil, false, nil
		}
		return []byte(data), true, nil
	}
	return nil, false, nil
}

// assembler builds one chat completion from streamed chunks.
type assembler struct {
	id      string
	model   string
	created int64
	usage   *Usage
	choices map[int]*assembledChoice
	ordered []int
}

type assembledChoice struct {
	content        strings.Builder
	reasoning      strings.Builder
	tools          map[int]*ToolCall
	toolOrder      []int
	finishReason   string
	annotations    []Annotation
	hasAnnotations bool
}

func newAssembler() *assembler {
	return &assembler{choices: map[int]*assembledChoice{}}
}

func (a *assembler) feed(chunk ChatCompletionChunk) {
	if a.id == "" {
		a.id = chunk.ID
	}
	if a.model == "" {
		a.model = chunk.Model
	}
	if a.created == 0 {
		a.created = chunk.Created
	}
	if chunk.Usage != nil {
		a.usage = chunk.Usage
	}
	for _, choice := range chunk.Choices {
		slot, ok := a.choices[choice.Index]
		if !ok {
			slot = &assembledChoice{tools: map[int]*ToolCall{}}
			a.choices[choice.Index] = slot
			a.ordered = append(a.ordered, choice.Index)
		}
		slot.content.WriteString(choice.Delta.Content)
		slot.reasoning.WriteString(choice.Delta.ReasoningContent)
		for _, call := range choice.Delta.ToolCalls {
			tool, ok := slot.tools[call.Index]
			if !ok {
				tool = &ToolCall{Type: "function"}
				slot.tools[call.Index] = tool
				slot.toolOrder = append(slot.toolOrder, call.Index)
			}
			if call.ID != "" {
				tool.ID = call.ID
			}
			tool.Function.Name += call.Function.Name
			tool.Function.Arguments += call.Function.Arguments
		}
		if choice.FinishReason != "" {
			slot.finishReason = choice.FinishReason
		}
		if len(choice.Delta.Annotations) > 0 {
			slot.annotations = append(slot.annotations, choice.Delta.Annotations...)
			slot.hasAnnotations = true
		}
	}
}

// finished reports whether every choice has a finish reason.
func (a *assembler) finished() bool {
	if len(a.choices) == 0 {
		return false
	}
	for _, slot := range a.choices {
		if slot.finishReason == "" {
			return false
		}
	}
	return true
}

// text returns the reply text received so far.
func (a *assembler) text() string {
	var out strings.Builder
	for _, index := range a.sortedIndexes() {
		out.WriteString(a.choices[index].content.String())
	}
	return out.String()
}

func (a *assembler) sortedIndexes() []int {
	indexes := append([]int(nil), a.ordered...)
	sort.Ints(indexes)
	return indexes
}

var thinkTag = regexp.MustCompile(`(?s)^\s*<(?:think|thinking)>(.*?)</(?:think|thinking)>\s*`)

// result builds the chat.completion object. A leading <think> block moves to ReasoningContent.
func (a *assembler) result() *ChatCompletion {
	out := &ChatCompletion{ID: a.id, Object: "chat.completion", Created: a.created, Model: a.model, Usage: a.usage}
	for _, index := range a.sortedIndexes() {
		slot := a.choices[index]
		content := slot.content.String()
		reasoning := slot.reasoning.String()
		if match := thinkTag.FindStringSubmatchIndex(content); match != nil {
			if reasoning == "" {
				reasoning = strings.TrimSpace(content[match[2]:match[3]])
			}
			content = content[match[1]:]
		}
		message := ResponseMessage{Role: "assistant", Content: content, ReasoningContent: reasoning}
		for _, key := range sortedKeys(slot.tools) {
			message.ToolCalls = append(message.ToolCalls, *slot.tools[key])
		}
		if slot.hasAnnotations {
			message.Annotations = slot.annotations
		}
		out.Choices = append(out.Choices, ChatChoice{Index: index, Message: message, FinishReason: slot.finishReason})
	}
	return out
}

func sortedKeys(m map[int]*ToolCall) []int {
	keys := make([]int, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}

// Ensure fmt stays imported for error wrapping in other files.
var _ = fmt.Sprintf
