package freetheai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testKey = "ftai_abab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab"

// recorder captures sleeps instead of waiting.
type recorder struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (r *recorder) sleep(ctx context.Context, d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.waits = append(r.waits, d)
	return ctx.Err()
}

func (r *recorder) all() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.waits...)
}

// setup replaces the retry sleep for one test.
func setup(t *testing.T) *recorder {
	t.Helper()
	rec := &recorder{}
	previous := sleepFunc
	sleepFunc = rec.sleep
	t.Cleanup(func() { sleepFunc = previous })
	return rec
}

// newTestClient starts a server with the handler and returns a client pointed at it.
func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	all := append([]Option{WithAPIKey(testKey), WithBaseURL(server.URL + "/v1")}, opts...)
	client, err := NewClient(all...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func writeSSE(w http.ResponseWriter, done bool, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range events {
		fmt.Fprintf(w, "data: %s\n\n", event)
	}
	if done {
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

func chunkJSON(text, finish string) string {
	payload := map[string]any{
		"id":    "chatcmpl-1",
		"model": "m",
		"choices": []map[string]any{{
			"index": 0,
			"delta": map[string]any{"content": text},
		}},
	}
	if finish != "" {
		payload["choices"].([]map[string]any)[0]["finish_reason"] = finish
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func replyHandler(w http.ResponseWriter, text string) {
	writeSSE(w, true, chunkJSON(text, ""), chunkJSON("", "stop"), `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)
}

func apiError(w http.ResponseWriter, status int, message, code string, headers map[string]string) {
	for key, value := range headers {
		w.Header().Set(key, value)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"error": map[string]any{"message": message, "code": code, "error_id": "freetheai-abc"}}
	_ = json.NewEncoder(w).Encode(body)
}

func readBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode body: %v (%s)", err, raw)
	}
	return out
}

func simpleParams(model string) ChatCompletionParams {
	return ChatCompletionParams{Model: model, Messages: []Message{{Role: "user", Content: "hi"}}}
}

// Basics ---------------------------------------------------------------------

func TestChatIsStreamedAndAssembled(t *testing.T) {
	setup(t)
	var gotAuth, gotUA, gotPath string
	var gotBody map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotUA, gotPath = r.Header.Get("Authorization"), r.Header.Get("User-Agent"), r.URL.Path
		gotBody = readBody(t, r)
		replyHandler(w, "Hello")
	})
	temp := 0.5
	reply, err := client.Chat.Completions.New(context.Background(), ChatCompletionParams{
		Model:       "fta/zai/glm-5.3",
		Messages:    []Message{{Role: "user", Content: "yo"}},
		Temperature: &temp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Choices[0].Message.Content != "Hello" || reply.Choices[0].FinishReason != "stop" {
		t.Fatalf("unexpected reply: %+v", reply.Choices[0])
	}
	if reply.Usage == nil || reply.Usage.TotalTokens != 3 {
		t.Fatalf("usage missing: %+v", reply.Usage)
	}
	if gotPath != "/v1/chat/completions" || gotAuth != "Bearer "+testKey || gotUA != "freetheai-go/"+Version {
		t.Fatalf("headers/path wrong: %q %q %q", gotPath, gotAuth, gotUA)
	}
	if gotBody["stream"] != true || gotBody["model"] != "fta/zai/glm-5.3" || gotBody["temperature"] != 0.5 {
		t.Fatalf("body wrong: %v", gotBody)
	}
}

func TestPlainTransportWhenTurnedOff(t *testing.T) {
	setup(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if _, present := readBody(t, r)["stream"]; present {
			t.Error("stream should not be sent")
		}
		fmt.Fprint(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"plain"},"finish_reason":"stop"}]}`)
	}, WithStreamTransport(false))
	reply, err := client.Chat.Completions.New(context.Background(), simpleParams("m"))
	if err != nil {
		t.Fatal(err)
	}
	if reply.Choices[0].Message.Content != "plain" {
		t.Fatalf("got %q", reply.Choices[0].Message.Content)
	}
}

func TestAssemblesToolCallsReasoningAndThinkTags(t *testing.T) {
	setup(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, true,
			`{"choices":[{"index":0,"delta":{"reasoning_content":"plan "}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_","arguments":"{\"a\""}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"weather","arguments":": 1}"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		)
	})
	reply, err := client.Chat.Completions.New(context.Background(), simpleParams("m"))
	if err != nil {
		t.Fatal(err)
	}
	message := reply.Choices[0].Message
	if message.ReasoningContent != "plan " {
		t.Fatalf("reasoning %q", message.ReasoningContent)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != "get_weather" ||
		message.ToolCalls[0].Function.Arguments != `{"a": 1}` || message.ToolCalls[0].ID != "call_1" {
		t.Fatalf("tool calls: %+v", message.ToolCalls)
	}

	thinker := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, true, chunkJSON("<think>hmm</think>\nAnswer", ""), chunkJSON("", "stop"))
	})
	thought, err := thinker.Chat.Completions.New(context.Background(), simpleParams("m"))
	if err != nil {
		t.Fatal(err)
	}
	if thought.Choices[0].Message.Content != "Answer" || thought.Choices[0].Message.ReasoningContent != "hmm" {
		t.Fatalf("think tags: %+v", thought.Choices[0].Message)
	}
}

func TestStreamingChunksAndUsage(t *testing.T) {
	setup(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		replyHandler(w, "Hi")
	})
	stream, err := client.Chat.Completions.NewStreaming(context.Background(), simpleParams("m"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var last ChatCompletionChunk
	count := 0
	for stream.Next() {
		count++
		last = stream.Current()
	}
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if count == 0 || last.Usage == nil || last.Usage.TotalTokens != 3 {
		t.Fatalf("count=%d last=%+v", count, last)
	}
	if stream.Text() != "Hi" {
		t.Fatalf("text %q", stream.Text())
	}
}

func TestStreamTextSkipsKeepalives(t *testing.T) {
	setup(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, ": keepalive\n\n")
		writeSSE(w, true, chunkJSON("Hel", ""), chunkJSON("lo", ""), chunkJSON("", "stop"))
	})
	var out strings.Builder
	err := client.Chat.Completions.StreamText(context.Background(), simpleParams("m"), func(text string) error {
		out.WriteString(text)
		return nil
	})
	if err != nil || out.String() != "Hello" {
		t.Fatalf("got %q err %v", out.String(), err)
	}
}

func TestSplitEventsAcrossNetworkChunks(t *testing.T) {
	setup(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		event := "data: " + chunkJSON("split", "") + "\n\n"
		w.Write([]byte(event[:9]))
		w.(http.Flusher).Flush()
		w.Write([]byte(event[9:]))
		w.(http.Flusher).Flush()
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunkJSON("", "stop"))
	})
	reply, err := client.Chat.Completions.New(context.Background(), simpleParams("m"))
	if err != nil {
		t.Fatal(err)
	}
	if reply.Choices[0].Message.Content != "split" {
		t.Fatalf("got %q", reply.Choices[0].Message.Content)
	}
}

// Known API answers -----------------------------------------------------------

func TestKeyIsCleanedAndPartialKeysFailFast(t *testing.T) {
	client, err := NewClient(WithAPIKey(`  "Bearer `+testKey+`"`+"\n"), WithBaseURL("http://localhost:1/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if client.APIKey() != testKey {
		t.Fatalf("key %q", client.APIKey())
	}
	for _, partial := range []string{"ftai_23dc791...", "ftai_23dc791…"} {
		_, err := NewClient(WithAPIKey(partial))
		var incomplete *IncompleteKeyError
		if !errors.As(err, &incomplete) {
			t.Fatalf("%q: expected IncompleteKeyError, got %v", partial, err)
		}
	}
}

func TestFinalAnswersAreNotRetried(t *testing.T) {
	setup(t)
	cases := []struct {
		name    string
		status  int
		message string
		check   func(error) bool
	}{
		{"checkin", 403, "Daily check-in required for free models. Visit https://freetheai.org/checkin to solve the captcha.", func(err error) bool {
			var target *CheckinRequiredError
			return errors.As(err, &target) && target.CheckinURL() == CheckinURL
		}},
		{"daily limit", 429, "Daily free request limit reached. It resets at 00:00 UTC; see https://freetheai.org/dashboard/plan.", func(err error) bool {
			var target *DailyLimitError
			return errors.As(err, &target) && target.ResetsAt().Hour() == 0
		}},
		{"unauthorized", 401, "unauthorized", func(err error) bool {
			var target *AuthenticationError
			return errors.As(err, &target)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				apiError(w, tc.status, tc.message, "", nil)
			})
			_, err := client.Chat.Completions.New(context.Background(), simpleParams("m"))
			if err == nil || !tc.check(err) {
				t.Fatalf("wrong error: %v", err)
			}
			if calls != 1 {
				t.Fatalf("calls %d, want 1", calls)
			}
			var base *APIError
			if !errors.As(err, &base) || base.ErrorID != "freetheai-abc" {
				t.Fatalf("base error id missing: %v", err)
			}
		})
	}
}

func TestBusyModelMovesToFallbackAtOnce(t *testing.T) {
	rec := setup(t)
	var models []string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		model := readBody(t, r)["model"].(string)
		models = append(models, model)
		if model == "fta/kimi/k3" {
			apiError(w, 503, "this model is busy right now; try again in a minute or pick another model.", "", map[string]string{"Retry-After": "30"})
			return
		}
		replyHandler(w, "from fallback")
	}, WithFallbackModels("fta/zai/glm-5.3"))
	reply, err := client.Chat.Completions.New(context.Background(), simpleParams("fta/kimi/k3"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(models, ",") != "fta/kimi/k3,fta/zai/glm-5.3" || reply.Choices[0].Message.Content != "from fallback" {
		t.Fatalf("models %v", models)
	}
	if len(rec.all()) != 0 {
		t.Fatalf("should not sleep, slept %v", rec.all())
	}
}

func TestBusyModelWithoutFallbackWaitsRetryAfter(t *testing.T) {
	rec := setup(t)
	first := true
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if first {
			first = false
			apiError(w, 503, "this model is busy right now", "", map[string]string{"Retry-After": "30"})
			return
		}
		replyHandler(w, "hi")
	})
	if _, err := client.Chat.Completions.New(context.Background(), simpleParams("m")); err != nil {
		t.Fatal(err)
	}
	waits := rec.all()
	if len(waits) != 1 || waits[0] != 30*time.Second {
		t.Fatalf("waits %v", waits)
	}
}

func TestBusyEverywhereRaisesModelBusy(t *testing.T) {
	setup(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		apiError(w, 503, "this model is busy right now", "", nil)
	}, WithMaxRetries(1), WithFallbackModels("b"))
	_, err := client.Chat.Completions.New(context.Background(), simpleParams("a"))
	var busy *ModelBusyError
	if !errors.As(err, &busy) {
		t.Fatalf("expected ModelBusyError, got %v", err)
	}
}

func TestContentFilterTriesNextModel(t *testing.T) {
	setup(t)
	handler := func(w http.ResponseWriter, r *http.Request) {
		if readBody(t, r)["model"] == "fta/dog/gpt-5" {
			apiError(w, 400, "Content filtered: ...", "content_filter", nil)
			return
		}
		replyHandler(w, "hi")
	}
	client := newTestClient(t, handler)
	params := simpleParams("fta/dog/gpt-5")
	params.FallbackModels = []string{"fta/zai/glm-5.3"}
	reply, err := client.Chat.Completions.New(context.Background(), params)
	if err != nil || reply.Choices[0].Message.Content != "hi" {
		t.Fatalf("fallback failed: %v", err)
	}
	strict := newTestClient(t, handler)
	_, err = strict.Chat.Completions.New(context.Background(), simpleParams("fta/dog/gpt-5"))
	var filtered *ContentFilterError
	if !errors.As(err, &filtered) {
		t.Fatalf("expected ContentFilterError, got %v", err)
	}
}

func TestConcurrencyRefusalLowersTheLimitAndRetries(t *testing.T) {
	setup(t)
	first := true
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if first {
			first = false
			apiError(w, 429, "concurrency limit reached: your account already has as many requests running", "", nil)
			return
		}
		replyHandler(w, "hi")
	})
	reply, err := client.Chat.Completions.New(context.Background(), simpleParams("m"))
	if err != nil || reply.Choices[0].Message.Content != "hi" {
		t.Fatalf("retry failed: %v", err)
	}
	if client.gate.learned != 1 || client.gate.inFlightCount() != 0 {
		t.Fatalf("learned %d inFlight %d", client.gate.learned, client.gate.inFlightCount())
	}
}

func TestCutOffStreamRetriedWhenNothingWasShown(t *testing.T) {
	setup(t)
	first := true
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if first {
			first = false
			writeSSE(w, false, chunkJSON("half a sen", ""))
			return
		}
		replyHandler(w, "whole")
	})
	reply, err := client.Chat.Completions.New(context.Background(), simpleParams("m"))
	if err != nil || reply.Choices[0].Message.Content != "whole" {
		t.Fatalf("expected retry, got %v", err)
	}
}

func TestCutOffUserStreamRaisesWithPartialText(t *testing.T) {
	setup(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, true, chunkJSON("half a ", ""), chunkJSON("sentence", "")) // [DONE], no finish reason
	})
	stream, err := client.Chat.Completions.NewStreaming(context.Background(), simpleParams("m"))
	if err != nil {
		t.Fatal(err)
	}
	for stream.Next() {
	}
	var stopped *StreamInterruptedError
	if !errors.As(stream.Err(), &stopped) || stopped.Partial != "half a sentence" {
		t.Fatalf("expected interrupted with partial, got %v", stream.Err())
	}
	if client.gate.inFlightCount() != 0 {
		t.Fatalf("slot not released")
	}
}

func TestErrorEventMidStream(t *testing.T) {
	setup(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "data: %s\n\n", chunkJSON("so", ""))
		fmt.Fprint(w, `data: {"error": {"message": "stream interrupted", "error_id": "freetheai-9"}}`+"\n\n")
	})
	stream, err := client.Chat.Completions.NewStreaming(context.Background(), simpleParams("m"))
	if err != nil {
		t.Fatal(err)
	}
	for stream.Next() {
	}
	var stopped *StreamInterruptedError
	if !errors.As(stream.Err(), &stopped) || stopped.ErrorID != "freetheai-9" || stopped.Partial != "so" {
		t.Fatalf("got %+v", stream.Err())
	}
}

func TestModelIDMissingItsPrefixIsFixed(t *testing.T) {
	setup(t)
	var models []string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"fta/zai/glm-5.3"},{"id":"fta/kimi/k3"}]}`)
			return
		}
		model := readBody(t, r)["model"].(string)
		models = append(models, model)
		if model == "zai/glm-5.3" {
			apiError(w, 404, `model "zai/glm-5.3" was not found.`, "model_not_found", nil)
			return
		}
		replyHandler(w, "ok")
	})
	if _, err := client.Chat.Completions.New(context.Background(), simpleParams("zai/glm-5.3")); err != nil {
		t.Fatal(err)
	}
	if strings.Join(models, ",") != "zai/glm-5.3,fta/zai/glm-5.3" {
		t.Fatalf("models %v", models)
	}
}

func TestUnknownModelListsSuggestions(t *testing.T) {
	setup(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"fta/zai/glm-5.3"},{"id":"fta/zai/glm-5.3-flash"}]}`)
			return
		}
		apiError(w, 404, `model "fta/zai/glm-5.4" was not found.`, "model_not_found", nil)
	})
	_, err := client.Chat.Completions.New(context.Background(), simpleParams("fta/zai/glm-5.4"))
	var notFound *ModelNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected ModelNotFoundError, got %v", err)
	}
	if !contains(notFound.Suggestions, "fta/zai/glm-5.3") || !strings.Contains(err.Error(), "Did you mean") {
		t.Fatalf("suggestions %v / %v", notFound.Suggestions, err)
	}
}

func TestEdgeHTMLErrorsAreRetried(t *testing.T) {
	setup(t)
	calls := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(502)
			fmt.Fprint(w, "<html>Bad gateway</html>")
			return
		}
		fmt.Fprint(w, `{"data":[]}`)
	})
	models, err := client.Models.List(context.Background())
	if err != nil || len(models) != 0 {
		t.Fatalf("retry failed: %v", err)
	}

	down := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(530)
		fmt.Fprint(w, "<html>origin down</html>")
	}, WithMaxRetries(0))
	_, err = down.Models.List(context.Background())
	var internal *InternalServerError
	if !errors.As(err, &internal) || !strings.Contains(err.Error(), "temporarily unreachable") {
		t.Fatalf("expected unreachable, got %v", err)
	}
}

func TestRateLimitRetriesThenGivesUp(t *testing.T) {
	setup(t)
	calls := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		apiError(w, 429, "rate limit reached", "", map[string]string{"Retry-After": "0"})
	}, WithMaxRetries(2))
	_, err := client.Search(context.Background(), SearchParams{Query: "cats"})
	var limited *RateLimitError
	if !errors.As(err, &limited) {
		t.Fatalf("expected RateLimitError, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls %d, want 3", calls)
	}
}

func TestErrorIDFromHeader(t *testing.T) {
	setup(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Error-ID", "freetheai-h")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(502)
		fmt.Fprint(w, `{"error":{"message":"upstream provider temporarily unavailable"}}`)
	}, WithMaxRetries(0))
	_, err := client.Search(context.Background(), SearchParams{Query: "x"})
	var internal *InternalServerError
	if !errors.As(err, &internal) || internal.ErrorID != "freetheai-h" {
		t.Fatalf("got %v", err)
	}
}

// Search, tasks, config ---------------------------------------------------------

func TestSearchAndAnswerBodies(t *testing.T) {
	setup(t)
	type seen struct {
		path string
		body map[string]any
	}
	var calls []seen
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, seen{r.URL.Path, readBody(t, r)})
		fmt.Fprint(w, `{"results":[],"answer":"x"}`)
	})
	if _, err := client.Search(context.Background(), SearchParams{Query: "go", NumResults: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Answer(context.Background(), SearchParams{Query: "why"}); err != nil {
		t.Fatal(err)
	}
	if calls[0].path != "/v1/search" || calls[0].body["query"] != "go" || calls[0].body["num_results"] != float64(3) {
		t.Fatalf("search body %+v", calls[0])
	}
	if calls[1].path != "/v1/answer" || calls[1].body["query"] != "why" {
		t.Fatalf("answer body %+v", calls[1])
	}
	if _, present := calls[1].body["num_results"]; present {
		t.Fatalf("zero num_results should be omitted")
	}
}

func TestTaskWaitPollsUntilDone(t *testing.T) {
	setup(t)
	states := []string{"queued", "in_progress", "completed"}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/content") {
			if r.URL.Query().Get("index") != "0" {
				t.Errorf("index %q", r.URL.Query().Get("index"))
			}
			io.WriteString(w, "MP4")
			return
		}
		status := states[0]
		if len(states) > 1 {
			states = states[1:]
		}
		fmt.Fprintf(w, `{"id":"task_1","status":%q,"model":"m"}`, status)
	})
	task, err := client.Tasks.Wait(context.Background(), "task_1", WaitOptions{PollInterval: time.Millisecond})
	if err != nil || task.Status != "completed" {
		t.Fatalf("wait: %v", err)
	}
	data, err := client.Tasks.Content(context.Background(), "task_1", 0)
	if err != nil || string(data) != "MP4" {
		t.Fatalf("content %q %v", data, err)
	}
}

func TestTaskFailureReturnsTaskFailedError(t *testing.T) {
	setup(t)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"t","status":"failed","model":"m","error":{"message":"refused","error_id":"e1"}}`)
	})
	_, err := client.Tasks.Wait(context.Background(), "t", WaitOptions{PollInterval: time.Millisecond})
	var failed *TaskFailedError
	if !errors.As(err, &failed) || failed.ErrorID != "e1" {
		t.Fatalf("expected TaskFailedError, got %v", err)
	}
}

func TestMissingKeyIsAnError(t *testing.T) {
	t.Setenv("FREETHEAI_API_KEY", "")
	if _, err := NewClient(); err == nil {
		t.Fatal("expected an error without a key")
	}
}

func TestEnvKeyAndBaseURL(t *testing.T) {
	t.Setenv("FREETHEAI_API_KEY", testKey)
	t.Setenv("FREETHEAI_BASE_URL", "http://localhost:9/v1/")
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	if client.APIKey() != testKey || client.BaseURL() != "http://localhost:9/v1" {
		t.Fatalf("got %q %q", client.APIKey(), client.BaseURL())
	}
}

func TestFixedConcurrencyNeverExceedsTheCap(t *testing.T) {
	setup(t)
	var mu sync.Mutex
	active, peak := 0, 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		replyHandler(w, "x")
	}, WithMaxConcurrency(1))
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.Chat.Completions.New(context.Background(), simpleParams("m")); err != nil {
				t.Errorf("call failed: %v", err)
			}
		}()
	}
	wg.Wait()
	if peak != 1 {
		t.Fatalf("peak concurrency %d, want 1", peak)
	}
}

func TestSuggestModelsMatchesDifflibBehaviour(t *testing.T) {
	ids := []string{"fta/zai/glm-5.3", "fta/zai/glm-5.3-flash", "fta/kimi/k3"}
	got := suggestModels("fta/zai/glm-5.4", ids)
	if len(got) == 0 || got[0] != "fta/zai/glm-5.3" {
		t.Fatalf("suggestions %v", got)
	}
	if fixed := fixable("zai/glm-5.3", []string{"fta/zai/glm-5.3"}); fixed != "fta/zai/glm-5.3" {
		t.Fatalf("fixable %q", fixed)
	}
}
