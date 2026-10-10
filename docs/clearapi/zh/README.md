# Clear API 开发者文档

Clear API 提供 OpenAI 风格的 API，并根据已配置模型和用户权限转发到上游服务。本文档默认中文，英文版见 [`../en/`](../en/)。

## 开始使用

1. 在 Clear API 面板中创建 API Key。
2. 将 API Key 保存到本地环境变量，不要提交到代码仓库。
3. 使用[快速开始](quickstart.md)发起第一次请求。
4. 使用 `/v1/models` 查看当前账号实际可用的模型。

生产账号池未开放。Responses、Anthropic、Gemini 及编程客户端尚未完成真实 Provider 验收，不能按“完全兼容”宣传。

## 页面

- [快速开始](quickstart.md)
- [API Key、Base URL 与模型](api-key-models.md)
- [协议参考](api-reference.md)
- [SDK 示例](sdk-examples.md)
- [客户端接入](clients.md)
- [模型、额度与错误排查](pricing-errors.md)
- [兼容性矩阵](compatibility.md)
