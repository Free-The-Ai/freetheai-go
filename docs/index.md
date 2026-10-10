---
description: Official Go client for FreeTheAI, the free OpenAI-compatible AI API, with typed errors, built-in retries, fallback models, and rate-limit handling. Standard library only.
---

# FreeTheAI Go SDK

![FreeTheAI Go SDK](assets/banner.jpg)

`freetheai-go` is the official Go client for [FreeTheAI](https://freetheai.org), a free OpenAI-compatible AI API. One key gives you dozens of chat models, streaming, tool calls, web search with sourced answers, and image and video tasks. It uses only the standard library and needs Go 1.21 or newer.

```bash
go get github.com/Free-The-Ai/freetheai-go@latest
```

```go
client, err := freetheai.NewClient() // reads FREETHEAI_API_KEY
if err != nil {
	log.Fatal(err)
}
reply, err := client.Chat.Completions.New(ctx, freetheai.ChatCompletionParams{
	Model:    "fta/zai/glm-5.3",
	Messages: []freetheai.Message{{Role: "user", Content: "Write a haiku about open models."}},
})
```

## Why use it instead of a generic OpenAI client

The client is written around how the FreeTheAI API actually behaves, so your code gets answers instead of retry logic:

- **Busy models** are retried after the server's `Retry-After`, or skipped for the next of your [fallback models](reliability.md#fallback-models).
- **The per-account concurrency limit** is learned on the first refusal; extra calls wait their turn.
- **Long replies** are fetched as a stream and assembled, so they never hit an idle timeout.
- **Cut-off streams** return an error with the partial text.
- **Daily check-in and daily limits** return their own error types at once, with the link or reset time.
- **Typos in model IDs** come back with suggestions, and a missing `fta/` prefix is fixed for you.

## Where to next

- [Getting started](getting-started.md): key, check-in, first request
- [Chat and streaming](guides/chat.md): messages, streaming, parameters
- [Errors](errors.md): every error type and when it happens
- [Configuration](configuration.md): all options

Need help? Ask in [Discord](https://discord.gg/secrets) and include the error id.
