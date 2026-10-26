# Clear API 用户指南

第一次使用 API？按下面顺序完成即可。Clear API 根据当前模型配置和账号权限提供 API 服务；注册账号、购买充值卡和创建 API Key 是不同操作。本文档默认中文，英文版见 [English](../en/)。

## 开始使用

1. [注册并登录](quickstart.md#注册并登录)：准备可收信的邮箱及独立密码。
2. [充值与兑换](quickstart.md#充值与兑换)：从商城购买卡密，回到钱包选择对应类型兑换，确认余额到账。
3. [创建 API Key](api-key-models.md)：按工具命名，设置分组、额度和有效期。
4. [选择模型和查看价格](pricing-errors.md)：核对模型 ID、协议、计费单位及可用分组。
5. [完成第一次请求](quickstart.md#第一次-api-请求)，再[配置客户端](clients.md)。
6. [查看消费与排查错误](pricing-errors.md#查看消费记录)。

## 常用页面入口

| 要做什么 | 页面 |
| --- | --- |
| 注册 / 登录 | [注册](/register) / [登录](/sign-in) |
| 购买卡密 / 兑换 / 查余额 | [充值商城](/shop) / [钱包](/wallet) |
| 创建或停用 Key、复制 API 地址 | [API 密钥](/keys) |
| 查模型价格 | [模型广场](/pricing) |
| 查单次调用和任务 | [使用日志](/usage-logs/common) / [任务日志](/usage-logs/task) |
| 控制台内阅读教程 | [帮助中心](/usage-guide) |

文档无需登录；钱包、密钥和个人记录需要登录。部分入口是否显示取决于站点配置。充值入口或模型不可用时，以页面实际提示为准。

## 开始前须知

- API 按调用消耗额度，充值不会自动创建 Key，也不等于购买某个聊天客户端的会员。
- API Key 相当于调用凭证；登录密码、邮箱验证码、充值卡密都不能代替它。
- 示例中的地址、Key 和模型占位符必须替换。不要分享完整 Key、卡密、密码、验证码或私人请求内容。
- Responses、Anthropic、Gemini 及编程客户端的真实上游和端到端兼容性尚未在本指南中验证，详见[兼容性矩阵](compatibility.md)。

## 页面

- [快速开始](quickstart.md)
- [API Key、Base URL 与模型](api-key-models.md)
- [协议参考](api-reference.md)
- [SDK 示例](sdk-examples.md)
- [客户端接入](clients.md)
- [模型、额度与错误排查](pricing-errors.md)
- [兼容性矩阵](compatibility.md)
