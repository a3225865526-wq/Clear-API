# ClearAPI 分组启用/禁用验收

交付时间：2026-10-31T23:59:03+08:00。仅 Windows 本地隔离实验；实验分支 `codex/shared-infrastructure-optimization-20261031`。应用源码 `fa96a74df2b46c0f3617068e0f77fbdc2fae67fe`，离线操作配置源码 `dd68a49afb63fc66433f5b54b2e4eb4a8ada4ef4`。源码基线和原 release-v2 均保留；未推送 GitHub、未操作生产、未修改 DNS、未发送真实收费请求。

## 结果

方案 B 已实现并通过本地验收。缺少状态记录的旧分组和新分组默认启用。禁用后的有效分组在 TokenAuth 和最终 Distribute 分发处实时查主库，发生在余额预留和上游派发之前；数据库异常拒绝放行。显式有效 Token 分组保持既有语义；Auto 同时复核原始候选，不通过过滤后的列表静默回退。禁用前已正式接收的长连接允许正常完成结算。

`options` 复用现有表，无新 schema：`GroupStatus:` + 原始名称 SHA256 为键，JSON 保存原名和 enabled。MySQL 的大小写/重音不敏感排序规则曾使原型键碰撞，已用真实 MySQL 复现并修复。原分组名、用户绑定、价格和历史交易不改写。状态不进入 Redis、Token 或用户缓存；并发修改采用原子 upsert。管理操作要求管理员和现有 Origin 防护，并记录分组、目标状态和操作者审计。

管理界面支持搜索、状态筛选、开关、禁用确认、等待期间防重复操作和失败状态保留；用户可用分组、订阅商品展示及四类支付发起、钱包购买入口执行禁用判断。已有订单完成和历史查询继续按原账务规则处理。

| 验收项 | 结果与证据 |
|---|---|
| 原有/新建分组默认启用；旧 options 保留 | SQLite 3.50.4、MySQL 8.4.11、PG 17.6 实例回归通过 |
| 管理员禁用、普通用户越权拒绝、Origin 保护、审计 | 后端定向测试、前端 5 项分组/入口测试及实机 group-acceptance.json |
| 老绑定/旧 Key、六种协议拒绝 | Chat、Responses、Anthropic、Gemini、Images、MJ 均 403 GROUP_DISABLED；否定用例不代表这些协议真实厂商已验收 |
| 无预留、扣款、上游请求 | 比较完整账本与 Mock 访问次数，拒绝前后不变 |
| 已接收 SSE 的安全收尾 | 禁用时在途 SSE 完成，Usage 只结算一次 |
| 其他启用分组可用、重新启用恢复原 Key | VIP Mock 成功；default 原 Key 恢复；绑定及既有消费记录摘要不变 |
| 缓存与并发边界 | 主库实时读取；保留原始 Auto 候选再检查；12 请求占满 10 连接池时取消后仍无迟到派发 |
| 历史订单/记录 | 实现不修改历史表；实际既有消费行摘要保持；支付入口的既有回归通过。未制造真实支付订单 |

实机分组验收结束时累计合成扣量 196、14 条唯一消费、待投递 outbox 0；其中 14 来自早期测试脚本因缺 Origin 被拒前已经接收的一次 SSE，仍被如实结算，未删除或伪造退款。后续容量和故障测试的最终账本见总报告。

关键文件：`model/group_status.go`、`middleware/group_status.go`、`middleware/auth.go`、`middleware/distributor.go`、`controller/group_status.go`、订阅支付控制器、`service/group.go`、`web/src/features/system-settings/models/group-status-panel.tsx`。

验证：`go test ./middleware ./controller ./service ./model -count=1`（4 包通过）；固定 Go 1.26.1 的定向测试；`TEST_GROUP_DIALECT=mysql/postgres TEST_GROUP_DSN=<各自隔离库> go test ./middleware -run TestGroupStatusAdmissionDatabase -count=1` 均通过，SQLite 同一测试无外部 DSN；实机命令 `python <private runtime>/group-acceptance.py`、`fault-runtime.py`。日志见 `evidence/commercial-shared-20261031/`。

回滚：旧二进制忽略新状态行，因此须先围栏入口并保持禁用分组受控，再回滚应用；不得用旧钱包快照覆盖新写入。状态行必须随 options 备份保存。
