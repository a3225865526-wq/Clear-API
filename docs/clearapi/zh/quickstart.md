# 快速开始

## 注册并登录

1. 打开[注册页](/register)，按页面要求填写邮箱或用户名，设置独立密码。密码为 8–128 个字符，不要与邮箱密码相同。
2. 如果页面要求邮箱验证，填写能正常收信的邮箱，点击“发送验证码”，从邮件中复制验证码；邮箱域名限制以页面提示为准，并非固定只支持 QQ 邮箱。
3. 若显示人机验证，请先完成验证；发送邮件后若验证组件重置，提交注册前再次完成。
4. 阅读并勾选页面要求的协议，点击“创建账户”。成功后前往[登录页](/sign-in)，按提示登录。邮箱验证注册模式可使用注册邮箱登录；其他模式使用页面要求的账号信息。
5. 登录后检查账号是否正确，再充值或创建密钥。注册不保证自动赠送额度；活动资格、额度和有效期以实际页面说明为准。

收不到邮件时，检查邮箱拼写、垃圾邮件和拦截规则，等待倒计时结束后重发。不要连续点击。忘记密码可从登录页进入找回密码。

## 充值与兑换

当前钱包使用购买卡密后兑换的流程。商城支付完成不等于 Clear API 余额到账；当前钱包页面没有站内在线支付入口。

### 从充值商城购买的卡密

1. 打开[充值商城](/shop)，核对商品说明、金额和可用状态，点击购买按钮前往页面提供的第三方商城。
2. 在商城完成购买，从订单或发货页面取得完整卡密。仅使用站点提供的购买入口，不根据旧教程猜测收款地址。
3. 返回[钱包](/wallet)，点击“兑换已有卡密”，或展开“其他兑换方式”，选择“原版兑换码（Legacy）”。这是当前公开充值商城页面引导的兑换方式。
4. 粘贴卡密，去掉前后空格，点击“立即兑换”，等待结果；确认登录的是要充值的账号。
5. 成功后核对钱包“当前余额”。如需核对充值、赠送等余额调整，点击余额下方的“余额明细”。

### 钱包内的其他码类型

| 你持有的码 | 兑换入口 |
| --- | --- |
| 公开充值商城购买、按商城说明使用的卡密 | 钱包 → 其他兑换方式 → 原版兑换码（Legacy） |
| 明确标注为商城兑换码的码 | 钱包 → 商城兑换码（仅启用该功能时显示） |
| 明确标注为线下订单兑换码的码 | 钱包 → 其他兑换方式 → 线下订单兑换码 |

不同入口不会自动互相尝试。类型不明确时，先核对商品说明或通过站点公布的支持渠道咨询。不要把 API Key 当作充值卡密，不要公开完整卡密。

兑换失败先检查账号、码类型、完整性和余额明细；网络中断时先刷新核对到账结果，不要立即再次购买。仍有问题时保留订单号、操作时间、错误提示及卡密尾号，通过站点公布的支持渠道处理。没有商品或兑换入口时，不代表可以改用旧支付教程。

## 连接信息

在 [API 密钥](/keys) 页点击“API 地址”，复制当前显示的地址。示例中的 `https://api.example.com` 是占位域名，不能直接调用；请换成你复制的实际服务地址。

| 配置项 | 填写方式 |
| --- | --- |
| 服务地址 | API 地址显示的站点根地址 |
| OpenAI 兼容 Base URL | 服务地址后接 `/v1`，只保留一个 `/v1` |
| 模型列表 | `GET /v1/models` |
| 文本请求 | `POST /v1/chat/completions` |
| 认证 | `Authorization: Bearer <YOUR_API_KEY>` |

## 第一次 API 请求

先[创建 API Key](api-key-models.md)，确认账户有余额及 Key 有可用额度。下面的请求会实际消耗额度；先测试一句短文本，输出上限示例为 128。使用支持 Chat Completions 及该参数的模型；若模型要求 `max_completion_tokens`，替换字段名，不要同时发送两个输出上限。

### Windows PowerShell

复制代码前，先把 Base URL 换成实际地址。Key 用隐藏输入读取，不会显示在屏幕上；不要截图或输出变量内容。

```powershell
$base = 'https://api.example.com/v1'
$secret = Read-Host 'API Key' -AsSecureString
$env:CLEAR_API_KEY = [System.Net.NetworkCredential]::new('', $secret).Password
$headers = @{ Authorization = "Bearer $env:CLEAR_API_KEY" }
$models = Invoke-RestMethod -Uri "$base/models" -Headers $headers
$models.data | Select-Object id
```

从列表复制目标模型的完整 `id`，替换 `YOUR_MODEL_ID`。列表中有模型不代表它支持所有接口；先在[模型广场](/pricing)核对协议。

```powershell
$body = @{
  model = 'YOUR_MODEL_ID'
  max_tokens = 128
  messages = @(@{ role = 'user'; content = 'Please reply: connected' })
} | ConvertTo-Json -Depth 5
$result = Invoke-RestMethod -Uri "$base/chat/completions" -Method Post -Headers $headers -ContentType 'application/json' -Body $body
$result.choices[0].message.content
```

### macOS / Linux（Bash）

替换地址后执行，输入 Key 时终端不回显：

```bash
export CLEAR_API_BASE_URL='https://api.example.com/v1'
read -r -s -p 'API Key: ' CLEAR_API_KEY; printf '\n'
export CLEAR_API_KEY
curl -sS -i "$CLEAR_API_BASE_URL/models" -H "Authorization: Bearer $CLEAR_API_KEY"
```

复制返回的 `data[].id`，替换 `YOUR_MODEL_ID`：

```bash
curl -sS -i "$CLEAR_API_BASE_URL/chat/completions" \
  -H "Authorization: Bearer $CLEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","max_tokens":128,"messages":[{"role":"user","content":"Please reply: connected"}]}'
```

### 判断结果与清理

收到 JSON 中的模型回复（通常在 `choices[0].message.content`），再到[使用日志](/usage-logs/common)按时间、Key 名称和模型核对本次消耗。返回网页 HTML 时先检查 API 地址和路径；返回 `error` 时查看[常见错误](pricing-errors.md)。

完成后关闭测试终端，或执行对应清理代码。终端环境变量不是加密保险箱，不要将真实 Key 写入示例、公开脚本或日志。

```powershell
Remove-Item Env:CLEAR_API_KEY
Remove-Variable headers, secret, body, result, models -ErrorAction SilentlyContinue
```

```bash
unset CLEAR_API_KEY CLEAR_API_BASE_URL
```

下一步：[客户端接入](clients.md)；开发者可查看 [Python / Node.js 示例](sdk-examples.md)。
