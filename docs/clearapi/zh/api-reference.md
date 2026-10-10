# 协议参考

## OpenAI Chat Completions — 源码路由；文本调用有已知成功记录

`POST /v1/chat/completions` 接受 OpenAI 风格的 `model`、`messages`、`stream` 等字段。`stream: true` 使用 SSE。字段能力仍取决于目标模型与上游。

## Models — 源码路由；Mock 合同测试

`GET /v1/models` 和 `GET /v1/models/{model}` 已注册。列表按当前 Key 可用模型生成。

## Responses — 有条件源码路由；未完成 Provider 验收

`POST /v1/responses` 由任务插件协议注册表提供，仅适用于已注册 Responses 模型。检索能力同样依赖插件协议。`POST /v1/responses/compact` 有源码路径，但部署公开前须由运营方确认启用。不得宣传完全兼容。

## Anthropic Messages — 源码路由；未完成 Provider 验收

`POST /v1/messages` 已注册。提供 `x-api-key` 与 `anthropic-version: 2023-06-01` 时，`GET /v1/models` 选择 Anthropic 形状的模型列表。没有真实 Claude Provider 验收，不作完全兼容承诺。

## Gemini — 源码路由；未完成 Provider 验收

```text
GET  /v1beta/models
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:streamGenerateContent
```

原生路径可用 `x-goog-api-key` 或 `?key=`。`:countTokens` 当前明确未实现。

源码还注册了 embeddings、audio、image、moderations、rerank 等路径，但本次没有真实 Provider 验收，应标记为“源码存在、未验证”。
