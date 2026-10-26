# curl、Python 与 Node.js 示例

示例中的 `https://api.example.com` 是占位域名，须换成“API 密钥 → API 地址”显示的服务地址。先按[快速开始](quickstart.md)设置环境变量，将 `YOUR_MODEL_ID` 替换为当前 Key 可用且支持 Chat Completions 的模型 ID。输出上限示例为 128；如模型要求 `max_completion_tokens`，替换字段名。Python 示例需先安装 `requests`，Node.js 示例需使用提供内置 `fetch` 的运行时。

以下均使用已确认存在的 Chat Completions 路径。先通过 `/v1/models` 确认模型 ID。

## curl

```bash
curl "${CLEAR_API_BASE_URL:-https://api.example.com/v1}/chat/completions" \
  -H "Authorization: Bearer $CLEAR_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","max_tokens":128,"messages":[{"role":"user","content":"Hello"}]}'
```

## Python

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

## Node.js

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
