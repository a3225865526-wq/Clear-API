package operation_setting

// StoreRechargeEnabled 商城卡密充值总开关（默认关闭，通过 Option
// "StoreRechargeEnabled" 读写）。仅控制用户端商城兑换入口与商品下发；
// 管理端批次/商品/VIP 配置不受影响，便于管理员先配置完成再开启。
var StoreRechargeEnabled = false
