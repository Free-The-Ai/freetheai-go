---
description: Web search and sourced answers with the FreeTheAI Go SDK.
---

# Web search

Two calls, both counted against a daily web search allowance (each response shows `DailyUsed` and `DailyLimit`).

## Search results

```go
results, err := client.Search(ctx, freetheai.SearchParams{Query: "go 1.23 release notes", NumResults: 5})
if err != nil {
	log.Fatal(err)
}
for _, hit := range results.Results {
	fmt.Println(hit.Title, hit.URL)
}
```

## Answers with sources

```go
answer, err := client.Answer(ctx, freetheai.SearchParams{Query: "What changed in Go 1.23?"})
if err != nil {
	log.Fatal(err)
}
fmt.Println(answer.Answer)
for _, source := range answer.Sources {
	fmt.Println("-", source.Title, source.URL)
}
```

## Search inside a chat

Models with built-in search take `Extra: map[string]any{"web_search_options": map[string]any{}}`; their sources come back as `Annotations` on the message:

```go
params := freetheai.ChatCompletionParams{
	Model:    "fta/zai/glm-5.3",
	Messages: []freetheai.Message{{Role: "user", Content: "What is the weather in Tokyo today?"}},
	WebSearch: map[string]any{},
}
reply, err := client.Chat.Completions.New(ctx, params)
```

For any other model, call `client.Search` and pass the results to the model yourself.
