package operation_setting

// DisableOnlinePayments 在线支付总开关（Option "DisableOnlinePayments"，默认
// true=关闭在线支付）。当前产品模式使用第三方商城兑换码充值：为 true 时后端
// 拒绝所有在线支付/订阅在线支付订单创建请求（Epay/Stripe/Creem/Waffo/
// WaffoPancake），仅保留商城兑换、离线订单与 Legacy 兑换；GetTopUpInfo 同时
// 强制下发全部支付入口关闭。历史支付代码、配置与订单全部保留，未来恢复在线
// 支付时将该 Option 置为 false 即可，无需改动代码。
var DisableOnlinePayments = true
