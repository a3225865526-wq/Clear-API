# Pricing and troubleshooting

Quota is calculated from the account's configured model pricing and upstream usage. Text commonly uses input/output tokens; cache and multimodal meters may also apply. The dashboard and current operator announcement are authoritative; no potentially stale price table is copied here.

Troubleshoot by listing models, checking for exactly one `/v1`, verifying the Bearer key and JSON content type, then retrying with a minimal text request. Preserve status, structured error, and request ID, but never disclose keys, passwords, or private prompts.

Typical statuses: `400` invalid request, `401` invalid credential, `403` access denied, `404` unknown path/model, `429` quota/rate limit, `5xx` gateway/upstream failure. Anthropic-style errors have an outer `type: error` and `error`; other APIs commonly return an `error` object.
