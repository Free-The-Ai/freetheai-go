<p align="center">
  <a href="https://free-the-ai.github.io/freetheai-go/"><img src="https://raw.githubusercontent.com/Free-The-Ai/freetheai-go/main/.github/banner.jpg" alt="FreeTheAI Go SDK" width="100%"></a>
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/Free-The-Ai/freetheai-go"><img src="https://pkg.go.dev/badge/github.com/Free-The-Ai/freetheai-go.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/Free-The-Ai/freetheai-go"><img src="https://goreportcard.com/badge/github.com/Free-The-Ai/freetheai-go" alt="Go report card"></a>
  <a href="https://github.com/Free-The-Ai/freetheai-go/actions/workflows/ci.yml"><img src="https://github.com/Free-The-Ai/freetheai-go/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-111111" alt="MIT license"></a>
  <a href="https://discord.gg/secrets"><img src="https://img.shields.io/badge/Discord-join-5865F2?logo=discord&logoColor=white" alt="Discord"></a>
</p>

<p align="center">
  <b><a href="https://free-the-ai.github.io/freetheai-go/">Documentation</a></b> ·
  <a href="https://free-the-ai.github.io/freetheai-go/getting-started/">Getting started</a> ·
  <a href="https://freetheai.org/models">Models</a> ·
  <a href="https://freetheai.org">freetheai.org</a>
</p>

# FreeTheAI Go SDK

The official Go client for [FreeTheAI](https://freetheai.org), a free OpenAI-compatible AI API. One key gives you dozens of chat models, streaming, tool calls, web search with sourced answers, and image and video tasks. It uses only the Go standard library.

```bash
go get github.com/Free-The-Ai/freetheai-go@latest
```

## Quickstart

Create a key at [freetheai.org](https://freetheai.org/dashboard/api-keys), do the [daily check-in](https://freetheai.org/checkin) for free models, and set `FREETHEAI_API_KEY`.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Free-The-Ai/freetheai-go"
)

func main() {
	client, err := freetheai.NewClient() // reads FREETHEAI_API_KEY
	if err != nil {
		log.Fatal(err)
	}
	reply, err := client.Chat.Completions.New(context.Background(), freetheai.ChatCompletionParams{
		Model:    "fta/zai/glm-5.3",
		Messages: []freetheai.Message{{Role: "user", Content: "Write a haiku about open models."}},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(reply.Choices[0].Message.Content)
}
```

Stream it:

```go
stream, err := client.Chat.Completions.NewStreaming(ctx, params)
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
	log.Fatal(err)
}
```

## Built for the FreeTheAI API

Generic OpenAI clients treat every error the same. This one knows how the FreeTheAI API answers:

- **Busy models** wait for the server's `Retry-After`, or move straight to your fallback models (`WithFallbackModels`).
- **Concurrency limits** are learned on the first refusal; extra calls queue instead of failing.
- **Long replies** are fetched as a stream and assembled, so they don't hit idle timeouts.
- **Cut-off streams** return a `*StreamInterruptedError` with the partial text, never a silently short answer.
- **The daily check-in and daily limit** return `*CheckinRequiredError` and `*DailyLimitError` at once, with the link or reset time.
- **Mistyped model IDs** come back with suggestions; a missing `fta/` prefix is fixed for you.

Every error is a typed value, so `errors.As` works. Read the [errors guide](https://free-the-ai.github.io/freetheai-go/errors/).

## Documentation

| Guide | |
| --- | --- |
| [Getting started](https://free-the-ai.github.io/freetheai-go/getting-started/) | Key, check-in, first request |
| [Chat and streaming](https://free-the-ai.github.io/freetheai-go/guides/chat/) | Parameters, streaming, reasoning, tool calls |
| [Web search](https://free-the-ai.github.io/freetheai-go/guides/web-search/) | Results and sourced answers |
| [Images and video](https://free-the-ai.github.io/freetheai-go/guides/media/) | Media tasks |
| [Reliability](https://free-the-ai.github.io/freetheai-go/reliability/) | What the client does for you |
| [Errors](https://free-the-ai.github.io/freetheai-go/errors/) | Every error type and what to do |
| [Configuration](https://free-the-ai.github.io/freetheai-go/configuration/) | All options |

## More from FreeTheAI

- [Python SDK](https://github.com/Free-The-Ai/freetheai-python) and [JavaScript SDK](https://github.com/Free-The-Ai/freetheai-js)
- [Cookbook](https://github.com/Free-The-Ai/freetheai-cookbook): runnable examples
- [Setup guides](https://freetheai.org/setup) for SillyTavern, JanitorAI, Claude Code, Cline, and more
- [API status](https://freetheai.org/status) and [Discord](https://discord.gg/secrets)

## License

MIT. Built by [Vibhek Soni](https://vibheksoni.com).
