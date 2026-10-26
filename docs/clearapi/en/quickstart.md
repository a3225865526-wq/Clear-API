# Quickstart

`https://api.example.com` is a placeholder domain. Copy your actual address from [API Keys](/keys) → API Addresses. The Chinese [quickstart](../zh/quickstart.md) covers registration and recharge. The output budget example is 128; use `max_completion_tokens` instead of `max_tokens` if the model requires it.


```text
Base URL: https://api.example.com
API base: https://api.example.com/v1
Authentication: Authorization: Bearer <YOUR_API_KEY>
```

Create an API key in the Clear API user dashboard and keep it secret:

```bash
export CLEAR_API_KEY='<YOUR_API_KEY>'
curl https://api.example.com/v1/models -H "Authorization: Bearer $CLEAR_API_KEY"
```

Call the known OpenAI-style chat route:

```bash
curl https://api.example.com/v1/chat/completions \
  -H "Authorization: Bearer $CLEAR_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","max_tokens":128,"messages":[{"role":"user","content":"Hello"}]}'
```

`YOUR_MODEL_ID` is a placeholder. Replace it with an ID returned for your own key and confirm that model supports Chat Completions. Use `/v1/models` as the authority. Responses, Anthropic, Gemini, and coding-client compatibility are not provider-verified here.
