# API keys, base URL, and models

Create a key in the Clear API user dashboard and send it as `Authorization: Bearer <API_KEY>`. Avoid embedding keys in public browser code, logs, or URLs.

Use `https://www.clear999.com/v1` as an OpenAI-compatible base URL. If the client has a separate API path setting, use `https://www.clear999.com` plus `/v1`; do not append `/v1` twice.

Model availability depends on account, group, key restrictions, and configuration. Call `GET /v1/models` and use `data[].id`. Current prices and quota are shown in the dashboard/operator announcements, not hard-coded here.
