# 客户端接入

## OpenCode、Cherry Studio、Chatbox

选择 OpenAI-compatible / OpenAI API 服务，Base URL 填 `https://www.clear999.com/v1`，输入 Clear API Key，并使用 `/v1/models` 返回的模型 ID。若客户端自动追加 `/v1`，Base URL 改用 `https://www.clear999.com` 并单独设置路径，避免重复。

## Codex CLI 与 Claude Code

仓库有 Responses 与 Anthropic Messages 源码路径，但没有完成真实 Provider、官方 CLI、流式事件和工具调用端到端验收。因此只能标记为“可能可配置、未验证”；不承诺完全兼容，也不建议生产依赖。
