---
description: Install the FreeTheAI Go SDK, create an API key, add funds, and send your first request.
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
3. Add funds to your wallet (from $5, crypto or card) and follow a provider at [freetheai.org/providers](https://freetheai.org/providers). Each request bills the tokens it used at the listed price.

=== "macOS / Linux"

    ```bash
    export FREETHEAI_API_KEY="ftai_..."
    ```

=== "Windows (PowerShell)"

    ```powershell
    $env:FREETHEAI_API_KEY = "ftai_..."
    ```

## 3. Pick a model

A model ID is the model name, such as `glm-5.3`; `<provider>/<model>` picks one provider. Browse them on [freetheai.org/models](https://freetheai.org/models) or list them in code:

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
	Model: "glm-5.3",
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
