---
description: Image and video generation tasks with the FreeTheAI Go SDK.
---

# Images and video

Image and video models run as tasks: you start one, it runs in the background, and you wait for it to finish.

## Images

```go
task, err := client.Images.Generate(ctx, freetheai.ImageGenerateParams{
	Model:  IMAGE_MODEL_ID,
	Prompt: "a lighthouse at dusk, watercolor",
	N:      1,
})
if err != nil {
	log.Fatal(err)
}
finished, err := client.Tasks.Wait(ctx, task.ID, freetheai.WaitOptions{}) // polls every 5 s
if err != nil {
	log.Fatal(err)
}
for index := range finished.Data {
	data, err := client.Tasks.Content(ctx, task.ID, index)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(fmt.Sprintf("lighthouse-%d.png", index), data, 0o644); err != nil {
		log.Fatal(err)
	}
}
```

## Video

```go
task, err := client.Videos.Create(ctx, freetheai.VideoCreateParams{
	Model:   VIDEO_MODEL_ID,
	Prompt:  "a paper boat drifting on a pond",
	Seconds: "6",
})
if err != nil {
	log.Fatal(err)
}
if _, err := client.Tasks.Wait(ctx, task.ID, freetheai.WaitOptions{}); err != nil {
	log.Fatal(err)
}
data, err := client.Tasks.Content(ctx, task.ID, 0)
```

`Wait` returns a `*TaskFailedError` when the task fails. `WaitOptions` sets the poll interval and the overall timeout.

## Listing tasks

```go
tasks, err := client.Tasks.List(ctx, 10)
for _, task := range tasks {
	fmt.Println(task.ID, task.Status, task.Model)
}
```

A task that fails gives its free request back. Outputs are kept for a limited time (`ExpiresAt`).
