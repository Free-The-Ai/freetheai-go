---
description: Install the FreeTheAI Go SDK, create a free API key, do the daily check-in, and send your first request.
---

# Getting started

## 1. Install

```bash
go get github.com/Free-The-Ai/freetheai-go@latest
```

Go 1.21 or newer. There are no other dependencies.

## 2. Get a key

1. Sign up at [freetheai.org](https://freetheai.org).
2. Open [API keys](https://freetheai.org/dashboard/api-keys) and create a key. It starts with `ftai_` and is shown once, so copy all of it.
3. Free models need the [daily check-in](https://freetheai.org/checkin), one quick captcha a day.

The free tier is 50 requests a day, reset at 00:00 UTC. The [plan page](https://freetheai.org/dashboard/plan) shows how to raise it.

=== "macOS / Linux"

    ```bash
    export FREETHEAI_API_KEY="ftai_..."
    ```

=== "Windows (PowerShell)"

    ```powershell
    $env:FREETHEAI_API_KEY = "ftai_..."
    ```

## 3. Pick a model

Model IDs look like `fta/<provider>/<model>`. Browse them on [freetheai.org/models](https://freetheai.org/models) or list them in code:

```go
models, err := client.Models.List(ctx)
if err != nil {
	log.Fatal(err)
}
for _, model := range models {
	fmt.Println(model.ID)
}
```

## 4. Send a request

```go
reply, err := client.Chat.Completions.New(ctx, freetheai.ChatCompletionParams{
	Model: "fta/zai/glm-5.3",
	Messages: []freetheai.Message{
		{Role: "system", Content: "You are a concise assistant."},
		{Role: "user", Content: "What is an API gateway?"},
	},
})
if err != nil {
	log.Fatal(err)
}
fmt.Println(reply.Choices[0].Message.Content)
fmt.Println(reply.Usage.TotalTokens)
```

Pass a `context.Context` to every call so you can cancel it or set a deadline.
