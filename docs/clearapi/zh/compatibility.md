# API 协议兼容性矩阵

| 能力 | 源码路由 | Mock/合同 | 真实 Provider | 公开说明 |
| --- | --- | --- | --- | --- |
| OpenAI Chat Completions | 有 | 有 | 已知业务记录有纯文本成功调用 | 首选入口；非全模型验收 |
| OpenAI Models | 有 | 有 | 按账号配置 | 以实际列表为准 |
| OpenAI Responses | 有条件 | 路由定义 | 未验证 | 不代表完全兼容 |
| Anthropic Messages | 有 | 路由定义 | 未验证 | 不承诺完全兼容 |
| Gemini native | 有 | 路由定义 | 未验证 | 不承诺完全兼容 |
| Codex CLI | 协议路由不等同于客户端支持 | 未验证 | 未验证 | 不宣传兼容 |
| Claude Code | 协议路由不等同于客户端支持 | 未验证 | 未验证 | 不宣传兼容 |

已有普通纯文本 Chat Completions 的历史成功记录；不代表所有账号、模型、协议和客户端验收通过。本次指南完善未连接真实上游，未验证生产开放状态。
