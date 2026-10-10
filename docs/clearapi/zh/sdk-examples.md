# curl、Python 与 Node.js 示例

以下均使用已确认存在的 Chat Completions 路径。先通过 `/v1/models` 确认模型 ID。

## curl

```bash
curl "${CLEAR_API_BASE_URL:-https://www.clear999.com/v1}/chat/completions" \
  -H "Authorization: Bearer $CLEAR_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"gpt-6-luna","messages":[{"role":"user","content":"Hello"}]}'
```

## Python

```python
import os
import requests

base = os.getenv("CLEAR_API_BASE_URL", "https://www.clear999.com/v1")
response = requests.post(
    f"{base}/chat/completions",
    headers={"Authorization": f"Bearer {os.environ['CLEAR_API_KEY']}"},
    json={"model": "gpt-6-luna", "messages": [{"role": "user", "content": "Hello"}]},
    timeout=120,
)
response.raise_for_status()
print(response.json())
```

## Node.js

```js
const base = process.env.CLEAR_API_BASE_URL || "https://www.clear999.com/v1";
const response = await fetch(`${base}/chat/completions`, {
  method: "POST",
  headers: { Authorization: `Bearer ${process.env.CLEAR_API_KEY}`, "Content-Type": "application/json" },
  body: JSON.stringify({ model: "gpt-6-luna", messages: [{ role: "user", content: "Hello" }] }),
});
if (!response.ok) throw new Error(`${response.status}: ${await response.text()}`);
console.log(await response.json());
```
