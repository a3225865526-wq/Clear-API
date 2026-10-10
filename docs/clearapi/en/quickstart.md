# Quickstart

```text
Base URL: https://www.clear999.com
API base: https://www.clear999.com/v1
Authentication: Authorization: Bearer <YOUR_API_KEY>
```

Create an API key in the Clear API user dashboard and keep it secret:

```bash
export CLEAR_API_KEY='sk-your-key'
curl https://www.clear999.com/v1/models -H "Authorization: Bearer $CLEAR_API_KEY"
```

Call the known OpenAI-style chat route:

```bash
curl https://www.clear999.com/v1/chat/completions \
  -H "Authorization: Bearer $CLEAR_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"gpt-6-luna","messages":[{"role":"user","content":"Hello"}]}'
```

`gpt-6-luna` is an example from a known business record, not a guarantee for every account. Use `/v1/models` as the authority. Responses, Anthropic, Gemini, and coding-client compatibility are not provider-verified here.
