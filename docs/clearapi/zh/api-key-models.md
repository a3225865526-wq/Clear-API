# API Key、Base URL 与模型

登录 Clear API 面板后，在用户侧 API Key/Token 页面创建 Key。创建后安全保存；不要放入浏览器公开代码、日志或客户端安装包。

Relay 请求使用 `Authorization: Bearer <API_KEY>`。当前实现也保留 Gemini 兼容认证 `x-goog-api-key` 或 `?key=`，但公开示例优先使用 Bearer，避免 Key 暴露在 URL。

OpenAI 风格客户端 Base URL：`https://www.clear999.com/v1`。如果客户端单独配置 `/v1` 路径，Base URL 填 `https://www.clear999.com`，不要重复追加 `/v1`。

模型取决于账号、分组、Key 限制和当前配置。调用 `GET /v1/models` 并使用返回的 `data[].id`。价格和额度以面板及运营方当前公告为准；本分支不修改正式价格。
