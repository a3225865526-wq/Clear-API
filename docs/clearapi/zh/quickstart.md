# 快速开始

## 连接信息

```text
Base URL: https://www.clear999.com
API endpoint: https://www.clear999.com/v1
Authentication: Authorization: Bearer <YOUR_API_KEY>
```

```bash
export CLEAR_API_KEY='sk-your-key'
```

先查询模型，避免猜测模型 ID：

```bash
curl https://www.clear999.com/v1/models -H "Authorization: Bearer $CLEAR_API_KEY"
```

调用 Chat Completions：

```bash
curl https://www.clear999.com/v1/chat/completions \
  -H "Authorization: Bearer $CLEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-6-luna","messages":[{"role":"user","content":"Hello"}]}'
```

`gpt-6-luna` 是当前已知业务记录中的示例模型 ID，不保证对所有账号可用。以 `/v1/models` 返回为准。当前已知记录使用 `default` 分组；分组不是公开 API 请求必须发送的字段。

常见状态码：`401` 凭据无效；`403` 无访问权限；`404` 路径或模型不存在；`400` 请求格式错误；`429` 限流或额度限制；`5xx` 网关或上游错误。排查时保存 request ID，不要分享 API Key。
