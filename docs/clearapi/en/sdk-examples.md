# curl, Python, and Node.js

The `api.example.com` domain and `YOUR_MODEL_ID` are placeholders. Copy the service address from API Keys → API Addresses and query models with your own key. Use a Chat Completions model; replace `max_tokens` with `max_completion_tokens` if required. Python needs `requests`; Node.js needs a runtime with built-in `fetch`.

These examples use the Chat Completions route. Replace the example model with an ID from `/v1/models`.

```bash
curl "${CLEAR_API_BASE_URL:-https://api.example.com/v1}/chat/completions" \
  -H "Authorization: Bearer $CLEAR_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","max_tokens":128,"messages":[{"role":"user","content":"Hello"}]}'
```

```python
import os
import requests

base = os.getenv("CLEAR_API_BASE_URL", "https://api.example.com/v1")
response = requests.post(
    f"{base}/chat/completions",
    headers={"Authorization": f"Bearer {os.environ['CLEAR_API_KEY']}"},
    json={"model": "YOUR_MODEL_ID", "max_tokens": 128, "messages": [{"role": "user", "content": "Hello"}]},
    timeout=120,
)
response.raise_for_status()
print(response.json())
```

```js
const base = process.env.CLEAR_API_BASE_URL || "https://api.example.com/v1";
const response = await fetch(`${base}/chat/completions`, {
  method: "POST",
  headers: { Authorization: `Bearer ${process.env.CLEAR_API_KEY}`, "Content-Type": "application/json" },
  body: JSON.stringify({ model: "YOUR_MODEL_ID", max_tokens: 128, messages: [{ role: "user", content: "Hello" }] }),
});
if (!response.ok) throw new Error(`${response.status}: ${await response.text()}`);
console.log(await response.json());
```
