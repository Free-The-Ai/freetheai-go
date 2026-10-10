---
description: API reference for the FreeTheAI Go SDK, listing the client, its services, methods, and error types.
---

# API reference

Full Go documentation is on [pkg.go.dev](https://pkg.go.dev/github.com/Free-The-Ai/freetheai-go). This page lists the main calls.

## Client

| Call | Returns |
| --- | --- |
| `freetheai.NewClient(opts ...Option)` | `(*Client, error)` |
| `client.Search(ctx, SearchParams)` | `(*SearchResponse, error)` |
| `client.Answer(ctx, SearchParams)` | `(*AnswerResponse, error)` |

## Services

| Service | Methods |
| --- | --- |
| `client.Chat.Completions` | `New(ctx, ChatCompletionParams) (*ChatCompletion, error)`, `NewStreaming(ctx, ChatCompletionParams) (*ChatCompletionStream, error)`, `StreamText(ctx, ChatCompletionParams, func(string) error) error` |
| `client.Models` | `List(ctx) ([]Model, error)` |
| `client.Tasks` | `List(ctx, limit int)`, `Get(ctx, id)`, `Wait(ctx, id, WaitOptions{})`, `Content(ctx, id, index int) ([]byte, error)` |
| `client.Videos` | `Create(ctx, VideoCreateParams) (*Task, error)` |
| `client.Images` | `Generate(ctx, ImageGenerateParams) (*Task, error)` |

## ChatCompletionStream

| Method | Purpose |
| --- | --- |
| `Next() bool` | Advances to the next chunk |
| `Current() ChatCompletionChunk` | The chunk Next advanced to |
| `Err() error` | The error that ended the stream |
| `Text() string` | The reply text received so far |
| `Close() error` | Ends the stream and frees its slot; safe to call more than once |

## Options

`WithAPIKey`, `WithBaseURL`, `WithHTTPClient`, `WithMaxRetries`, `WithFallbackModels`, `WithMaxConcurrency`, `WithStreamTransport`, `WithTimeout`, `WithHeader`. See [Configuration](configuration.md).

## Errors

See [Errors](errors.md) for every error type.
