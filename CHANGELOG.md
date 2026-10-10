# Changelog

## v0.1.0 (2026-10-10)

First release.

- Chat completions (whole replies and streams), models, web search and sourced answers, image and video tasks. Standard library only; Go 1.21 or newer.
- Typed errors for each API answer, usable with `errors.As`, with the error id on every API error.
- Built-in handling for the FreeTheAI API: fallback models, a learned concurrency limit, retries on busy models with `Retry-After`, non-streamed chats fetched as a stream and assembled, cut-off stream detection, model ID suggestions and prefix fixes, and key cleanup.
