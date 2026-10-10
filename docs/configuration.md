---
description: Configuration options for the FreeTheAI Go SDK client, including the key, base URL, retries, fallback models, concurrency, timeouts, and a custom HTTP client.
---

# Configuration

Pass options to `freetheai.NewClient`:

```go
client, err := freetheai.NewClient(
	freetheai.WithAPIKey("ftai_..."),                 // default: FREETHEAI_API_KEY
	freetheai.WithBaseURL("https://api.freetheai.org/v1"), // default: FREETHEAI_BASE_URL
	freetheai.WithMaxRetries(3),                      // retries per model for retryable errors
	freetheai.WithFallbackModels("fta/zai/glm-5.3-flash"),
	freetheai.WithMaxConcurrency(2),                  // fixed cap; learned when left out
	freetheai.WithStreamTransport(true),              // fetch non-streamed chats as a stream
	freetheai.WithTimeout(10*time.Minute),            // plain (non-streamed) requests
	freetheai.WithHeader("X-Trace", "my-service"),    // extra header on every request
	freetheai.WithHTTPClient(&http.Client{}),         // your own *http.Client
)
```

| Environment variable | Used for |
| --- | --- |
| `FREETHEAI_API_KEY` | The API key when `WithAPIKey` isn't passed |
| `FREETHEAI_BASE_URL` | The API address when `WithBaseURL` isn't passed |

## Your own HTTP client

Pass an `*http.Client` for proxies, custom TLS, or transports:

```go
transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
client, err := freetheai.NewClient(freetheai.WithHTTPClient(&http.Client{Transport: transport}))
```

The client sets the `Authorization` and `User-Agent` headers itself.

## Timeouts and contexts

`WithTimeout` limits plain requests. Streams have no overall time limit; they end when the reply ends, when you call `Close`, or when the context you passed is done. Pass a context with a deadline to bound any call.
