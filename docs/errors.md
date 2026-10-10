---
description: Every error type in the FreeTheAI Go SDK, when it is returned, and what to do about it.
---

# Errors

Every API error is a typed value that embeds `freetheai.APIError`, which carries `StatusCode`, `Code`, `Message`, `ErrorID`, `RetryAfter`, and `Body`. Use `errors.As` to match the type you care about; `errors.As` with `*freetheai.APIError` matches every API error.

```go
var daily *freetheai.DailyLimitError
var checkin *freetheai.CheckinRequiredError
var base *freetheai.APIError

_, err := client.Chat.Completions.New(ctx, params)
switch {
case errors.As(err, &checkin):
	fmt.Println("Do the daily check-in:", checkin.CheckinURL())
case errors.As(err, &daily):
	fmt.Println("Free requests are used up until", daily.ResetsAt())
case errors.As(err, &base):
	fmt.Println("API error, id", base.ErrorID, ":", base.Message)
case err != nil:
	fmt.Println("failed:", err)
}
```

| Error type | Status | When | What to do |
| --- | --- | --- | --- |
| `*IncompleteKeyError` | 401 | The key is only part of a key (from `NewClient`) | Copy the whole key, or create a new one |
| `*AuthenticationError` | 401 | The key is wrong or revoked | Check the key on the [API keys page](https://freetheai.org/dashboard/api-keys) |
| `*CheckinRequiredError` | 403 | Free models need today's check-in | Visit `CheckinURL()` |
| `*EmailNotVerifiedError` | 403 | The account's email isn't confirmed | Confirm it on the website |
| `*PermissionDeniedError` | 403 | The key's scope or IP list doesn't allow this | Check the key's settings |
| `*ModelNotFoundError` | 404 | No model has this ID | See `Suggestions` |
| `*NotFoundError` | 404 | Unknown task or route | |
| `*BadRequestError` | 400 | The request is invalid | Read `Message` |
| `*ContentFilterError` | 400 | The model's content filter refused | Rephrase or use another model |
| `*ImagesNotSupportedError` | 400 | The model reads text only | Remove images or pick another model |
| `*RequestTooLargeError` | 413 | The request is over the size limit | Shorten the chat or drop images |
| `*ConcurrencyLimitError` | 429 | Too many replies running (after the client's own queueing gives up) | Wait for running replies |
| `*DailyLimitError` | 429 | Today's free requests are used | Wait until `ResetsAt()` |
| `*ProviderBudgetError` | 429 | This model's daily token budget is used | Use another model |
| `*RateLimitError` | 429 | Too many requests | Slow down |
| `*ModelBusyError` | 503 | Every route for the model is busy | Retry later or add fallback models |
| `*InternalServerError` | 5xx | The service is temporarily unavailable | Retry later |
| `*StreamInterruptedError` | | A stream stopped early | `Partial` has the text so far |
| `*TaskFailedError` | | An image or video task ended without output | Read `Message` and `ErrorID` |
| `*ConnectionError` | | No answer (network or timeout) | Check your connection |

Include the error id when you ask for help.
