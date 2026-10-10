---
description: How the FreeTheAI Go SDK handles busy models, concurrency limits, cut-off streams, content filters, and other API behaviour for you.
---

# Reliability

Generic OpenAI clients treat every error the same. This client knows the FreeTheAI API's answers and handles each one the way it should be handled.

| What happens | What the client does |
| --- | --- |
| A model is busy (503 with `Retry-After`) | Tries the next [fallback model](#fallback-models) at once, or waits `Retry-After` (at most 30 s) and retries |
| The account already has as many replies running as its plan allows (429) | [Learns the limit](#concurrency) and queues the rest instead of failing them |
| A long reply without streaming idles past a proxy timeout | Fetches non-streamed chats as a stream and assembles them ([stream transport](#stream-transport)) |
| A stream ends without a finish reason | Returns `*StreamInterruptedError` with `Partial`; retries when nothing was shown yet |
| The content filter refuses a request | Returns `*ContentFilterError`, or tries the next fallback model |
| A model reads text only and the request has images | Returns `*ImagesNotSupportedError`, or tries the next fallback model |
| A model's daily token budget is used up | Returns `*ProviderBudgetError`, or tries the next fallback model |
| A model ID is mistyped | Fixes a missing `fta/` prefix; otherwise lists close IDs in `Suggestions` |
| The daily check-in isn't done | Returns `*CheckinRequiredError` at once, with `CheckinURL()` |
| Today's free requests are used up | Returns `*DailyLimitError` at once, with `ResetsAt()` |
| The key has quotes, spaces, or `Bearer ` around it | Cleans it |
| The key is only part of a key | Returns `*IncompleteKeyError` from `NewClient`, before any request |
| Edge errors during a deploy (HTML 502 or 530 pages) | Retries with backoff, then returns `*InternalServerError` |
| Network errors and other 5xx answers | Retries with backoff (`WithMaxRetries`, default 3) |
| A model puts its thinking inside `<think>` tags | Moves it to `ReasoningContent` |

Errors that retrying cannot fix are returned at once, so you never wait on retries that cannot succeed.

## Fallback models

```go
client, _ := freetheai.NewClient(freetheai.WithFallbackModels("fta/zai/glm-5.3-flash", "fta/mmx/minimax-m3"))
reply, _ := client.Chat.Completions.New(ctx, freetheai.ChatCompletionParams{Model: "fta/kimi/k3", Messages: messages})
fmt.Println(reply.Model) // the model that answered
```

The chain is tried in order when a model is busy, filtered, can't read the request's images, is over its daily budget, is unknown, or still fails after its retries. Set `FallbackModels` on a single call to override the list for that call.

For streams, fallbacks only happen before the first piece of the reply arrives, so you never get two answers mixed.

## Concurrency

Accounts run a limited number of replies at once (one or two on the free tier). The client starts with no limit. The first time the API refuses a call for concurrency, the client lowers its own limit to the number of replies that were running and queues further calls until a slot frees up. Ten minutes after the last refusal it tries more again, so a plan upgrade is picked up without a restart.

To set a fixed cap instead: `freetheai.WithMaxConcurrency(2)`.

A stream holds its slot until you finish reading it or call `Close`. Always `defer stream.Close()`.

## Stream transport

Some proxies close a connection that sends nothing for a while, and a non-streamed request sends nothing until the whole reply is written. With the stream transport on (the default), `New` asks the API for a stream and assembles it into one ordinary chat completion. Your code sees no difference.

Turn it off with `freetheai.WithStreamTransport(false)` to get the API's own non-streamed answer.

## Error ids

Every API error carries an error id (also sent as the `X-Error-ID` header). Quote it in [Discord](https://discord.gg/secrets) or a support ticket so the exact failure can be found.
