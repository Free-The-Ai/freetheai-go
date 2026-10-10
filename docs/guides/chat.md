---
description: Chat completions and streaming with the FreeTheAI Go SDK - messages, streams, reasoning, tool calls, and extra parameters.
---

# Chat and streaming

## A chat completion

```go
temp := 0.7
maxTokens := 400
reply, err := client.Chat.Completions.New(ctx, freetheai.ChatCompletionParams{
	Model:       "fta/zai/glm-5.3",
	Messages:    []freetheai.Message{{Role: "user", Content: "Name three uses for a paperclip."}},
	Temperature: &temp,
	MaxTokens:   &maxTokens,
})
```

Optional numbers are pointers, so an unset field is left out of the request. Any other Chat Completions field can go in `Extra`:

```go
params.Extra = map[string]any{"frequency_penalty": 0.2}
```

## Streaming

`NewStreaming` returns once the first piece of the reply has arrived. Read it with `Next`, then check `Err`:

```go
stream, err := client.Chat.Completions.NewStreaming(ctx, freetheai.ChatCompletionParams{
	Model:    "fta/kimi/k3",
	Messages: []freetheai.Message{{Role: "user", Content: "Tell me a short story."}},
})
if err != nil {
	log.Fatal(err)
}
defer stream.Close()

for stream.Next() {
	for _, choice := range stream.Current().Choices {
		fmt.Print(choice.Delta.Content)
	}
}
if err := stream.Err(); err != nil {
	var stopped *freetheai.StreamInterruptedError
	if errors.As(err, &stopped) {
		fmt.Println("\nstopped early, text so far:", stopped.Partial)
	}
}
```

If you only want the text, `StreamText` calls your function for each piece:

```go
err := client.Chat.Completions.StreamText(ctx, params, func(text string) error {
	_, err := io.WriteString(os.Stdout, text)
	return err
})
```

The last chunk carries `Usage`.

## Reasoning

Thinking models return their reasoning in `ReasoningContent`. Turn thinking off with `ReasoningEffort: "none"`. Text inside a leading `<think>` block moves to `ReasoningContent`, so `Content` holds only the answer.

## Tool calls

```go
tools := []freetheai.Tool{{
	Type: "function",
	Function: freetheai.ToolFunction{
		Name:        "get_weather",
		Description: "Current weather for a city",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"city": map[string]any{"type": "string"}},
			"required":   []string{"city"},
		},
	},
}}
messages := []freetheai.Message{{Role: "user", Content: "Is it raining in Paris?"}}
reply, err := client.Chat.Completions.New(ctx, freetheai.ChatCompletionParams{
	Model: "fta/zai/glm-5.3", Messages: messages, Tools: tools,
})
// Run each reply.Choices[0].Message.ToolCalls entry, then append a role "tool" message
// with the same ToolCallID and send the conversation again.
```

## Images in messages

Models that read images take content parts:

```go
messages := []freetheai.Message{{
	Role: "user",
	Content: []freetheai.ContentPart{
		{Type: "text", Text: "What is in this picture?"},
		{Type: "image_url", ImageURL: &freetheai.ImageURL{URL: "https://example.com/cat.jpg"}},
	},
}}
```

A text-only model returns `*ImagesNotSupportedError`; with fallback models set, the next model is tried.
