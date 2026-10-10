# Protocol reference

## OpenAI Chat Completions — source route; known plain-text success record

`POST /v1/chat/completions` accepts OpenAI-style `model`, `messages`, and `stream`. Streaming uses SSE. Details depend on model and upstream capability.

## Models — source route and mock contract

`GET /v1/models` and `GET /v1/models/{model}` are registered. The list is scoped to the current key.

## Responses — conditional source route, not provider verified

`POST /v1/responses` is supplied by the task-plugin protocol registry and applies only to registered Responses models. Retrieval is conditional too. Compaction exists in source but should not be advertised without deployment confirmation. No full compatibility claim is made.

## Anthropic Messages — source route, not provider verified

`POST /v1/messages` is registered. Anthropic model-list shape is selected by `x-api-key` with `anthropic-version: 2023-06-01`. No real Claude provider acceptance was performed.

## Gemini — source route, not provider verified

`GET /v1beta/models`, `POST /v1beta/models/{model}:generateContent`, and `POST /v1beta/models/{model}:streamGenerateContent` exist in source. Native authentication accepts `x-goog-api-key` or `?key=`. `:countTokens` is explicitly not implemented.

Other routes exist for embeddings, audio, images, moderation, and rerank, but are not provider-verified in this delivery.
