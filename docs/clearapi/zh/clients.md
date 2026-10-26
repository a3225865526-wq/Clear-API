# 客户端接入

## 配置前准备

先完成[第一次 API 请求](quickstart.md#第一次-api-请求)。准备三项：密钥页复制的 **API 地址**、**完整 API Key**、当前 Key 的 `/v1/models` 返回的 **模型 ID**。Clear API 是 API 服务，不是客户端会员；客户端自带的登录或订阅不能替代这些配置。

## OpenAI 兼容客户端通用配置

适用于客户端当前版本确实提供自定义 OpenAI 兼容服务的场景。以下为字段含义，不代表已逐个验证客户端版本、流式响应、图片或工具调用。

1. 打开客户端的服务商或模型设置，添加自定义服务，选择 OpenAI-compatible / OpenAI API，并选择 Chat Completions 协议（若有协议选项）。
2. API 地址 / Base URL 填实际服务地址加 `/v1`。不要填写网站的登录、文档页面，也不要把完整 `/chat/completions` 端点填入 Base URL。
3. API Key 填“复制密钥”的完整结果；大多数 Key 输入框不需要自行添加 `Bearer `。
4. 获取模型列表，或手动添加从当前 Key 的模型列表复制的完整 ID；先选支持 Chat Completions 的模型。
5. 保存配置，在新对话中明确选择该服务和模型，设置模型支持的小输出上限，发送一句短文本，再到使用日志核对。

```text
Provider / API type: OpenAI-compatible (Chat Completions)
Base URL: https://api.example.com/v1
API Key: <YOUR_API_KEY>
Model: <YOUR_MODEL_ID>
```

`api.example.com`、`<YOUR_API_KEY>` 和 `<YOUR_MODEL_ID>` 都是占位符，必须替换。如果客户端自动追加 `/v1`，Base URL 只填根地址；最终请求路径应为 `/v1/chat/completions`，不能是 `/v1/v1/chat/completions`。

## Cherry Studio、Chatbox 与 OpenCode

这些客户端可作为自定义服务配置的候选；具体字段、安装方式和配置文件格式以所用版本的客户端说明为准。本站代码无法证明某个版本已经兼容，因此本指南不提供未经核实的安装命令、配置文件路径或功能保证。

- Cherry Studio / Chatbox：若设置页提供自定义 OpenAI 兼容服务，按上表填写；获取不到模型时可使用已查询到的模型 ID 手动添加。
- OpenCode：若当前版本提供 OpenAI-compatible Provider，配置实际 Base URL、Key 和模型 ID；不要将默认 Responses Provider 与 Chat Completions 混用。

## CC Switch 导入

[API 密钥](/keys)列表的操作菜单已有“CC Switch”入口。已安装并信任 CC Switch 时，可选择应用、填写配置名称和主模型，再点击“打开 CC Switch”。导入会将该 Key 交给本机客户端，请只在自己的设备操作，不要转发导入链接。

该入口只生成客户端导入配置，不安装客户端，也不证明目标应用能完成请求。导入后仍要核对 API 地址、协议及模型，并做一次短文本验证。

## Codex CLI 与 Claude Code

仓库有 Responses 与 Anthropic Messages 源码路径，但没有完成真实 Provider、官方 CLI、流式事件和工具调用端到端验收。因此只能标记为“可能可配置、未验证”；不承诺完全兼容，也不建议生产依赖。

Gemini CLI 及其他依赖专用协议的客户端同样不能仅因存在服务端路由就视为已支持。详情见[兼容性矩阵](compatibility.md)。

## 配置失败按这个顺序检查

1. 先用同一地址、Key、模型执行最小 API 请求；这也失败时先查服务端错误，不反复改客户端。
2. 最小请求成功而客户端失败时，检查实际选中的 Provider、模型、Base URL，以及旧配置是否仍在生效。
3. 纯文本成功而图片、工具或流式失败时，核对模型和客户端支持的具体能力，不能据此断言全部兼容。
4. 保留客户端版本、发生时间、接口路径、状态码及脱敏错误，查看[常见错误](pricing-errors.md)。不要提交含 Key 的配置文件或导入链接。
