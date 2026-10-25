# 动态监测与公告/普通渠道修复发布记录（2026-10-25）

## 范围与真实状态

本次从干净候选 `74ff807aa95050a3ac2603e67b20b484a7dda34b` 的独立分支继续。发布前生产实核仍为 `dc685cbe338e8f700cae347e2eaac351d984c9c7`，不是把工具提交或历史 PASS 当作上线证据。最新用户授权允许正式本地部署；不 Push、不合并 main。

动态监测软件路径已接通，但**真实供应商调用、供应商费用硬上界及生产启用尚未验收**。用户授权复用启用 Channel 1，最高人民币 1 元；这不是供应商单价/限额证明。只读 `/v1/models` 确认 gpt-6-luna；价格和用量接口 404，未返回可核实计费依据。没有发送收费推理请求，没有编造价格、费用、执行回执或供应商验收。

## 根因与最小修改

- 旧适配器永远拒绝真实发送，管理启用固定 501，测试按钮禁用，未向现有 SystemTask 注册监测处理器；传输层仍只接受 loopback。现接通 Root 管理触发和 `console_monitor` 任务，复用现有执行器、数据库租约和预算账本。PG03/account_pool_group 仍未开放。
- `service/console_probe_cost.go`：按监测 ID 在既有 options 存储操作员确认的上游费用合同，无新表/新列。合同含来源、有效期、输入 token 上界、保守向上取整的每 token microUSD 费率、全部固定费和单次硬上限；绑定实际 endpoint 和密钥摘要，密钥变更即失效。**合同字段本身不能证明供应商遵守限额，必须有真实外部依据。**
- `model/console_monitor_control.go`：复用现有跨方言行锁，在同一策略锁下保存合同并拒绝未结束任务期间改合同。执行前预留完整单次费用上界；不按较低实际用量返还预算，因此失败与并发不能多发请求。
- `service/console_probe_transport.go`：绑定 Channel 1/gpt-6-luna/Responses，固定短请求、max_output_tokens=16、单次 HTTPS HTTP/1 传输，无重试/重定向/路由回退。绑定监测与 attempt 请求 ID；校验响应 ID、模型、完成状态和非负有界用量。incomplete 为 degraded；用量未知/超时/传输不明保留未完成租约并停止后续请求，不能自动当成未收费释放容量。此类不明任务需要供应商权威停止/结算证据再协调恢复。
- `service/console_probe.go`：返回经合同费率计算的费用上界和供应商响应 ID；持久化保守预留和状态，Root 手动触发审计记录响应 ID。它不是供应商发票，也没有修改普通用户余额/账单。监测只进入已有渠道健康视图，不自动改变普通渠道开关。
- `controller/console_monitor_{admin,probe}.go`、`router/api-router.go`：费用合同 GET/PUT、真实触发、配置满足时允许启用；保留 RootAuth、来源防护、JSON/header/confirm。管理列表 readiness 由实际策略/合同判断。`controller/system_task_handlers.go`、`model/system_task.go` 复用主任务调度，暂停策略不调度。
- `web/src/features/channel-status/monitor-admin.tsx`：普通监测发布并启用后可以明确确认测试，展示实际错误/状态并刷新公开监测查询；7 种语言仅新增一个费用合同提示。预算和合同目前由现有 Root API 配置，未添加第二套执行系统或巨大配置页面。
- 前一候选包含的公告缓存失效修复和 AddChannel 缓存刷新原样保留。公告管理位于「控制台内容 → 公告」；admin 实际 role=100/status=1。旧公告不自动删除，生产不试发公告。

## 本轮实际验证

详细原始输出见 `evidence/monitor-runtime-20261025/logs/`；本地完整构建输出位于 `.local-tests/monitor-runtime-20261025/`。

- `go test -count=1 -v ./service -run 'TestConsoleProbeConfiguredExecutionMatrix|TestConsoleProbeWorstCaseCost|TestConsoleProbeLocalTransport|TestConsoleMonitorAdminAuthorizationPublicationAndFailClosedExecution'`：真实 SQLite 3.50.4、MySQL 8.4.11、PostgreSQL 17.6 隔离实例覆盖合同保存读取、正常 HTTP、额度耗尽、竞争、缺用量、超时；测试数据库启动迁移两次。没有新增 schema，生产不手工改数据库。
- 补充 `TestConsoleProbeConfiguredExecutionMatrix/sqlite`：密钥变更失效、过期合同拒绝、incomplete 降级通过。
- `go test -count=1 -v ./controller -run 'TestConsoleMonitorConfiguredHTTPFlow|TestConsoleMonitorTriggerRecordsCostDenialWithoutDispatch'`：实际 HTTP 管理保存合同/预算→启用→触发→真实本地 HTTP 服务→入库→用户健康视图正常，另测费用缺失零发送。此测试没有加载 RootAuth；权限/来源边界使用上项既有 Root 测试，不能将这里当作登录 E2E。
- 调度暂停/发布、未决租约、panic 保留容量、普通渠道 SQLite 禁用/启用和 SystemTask 定向回归通过。
- 所有本轮 HTTP 上游均为**显式合成 fixture**。它们证明软件调用链与预算，不证明真实供应商传输合同、费用或可用性。未以静态 JSON 文件或手工回执替代供应商验收。
- 前端监测 8 项通过，typecheck/lint 通过；公告 9 项此前已通过，本轮联合回归和最终生产构建另见发布结果。此前真实隔离浏览器新增→保存→发布→即时通知→普通用户新旧公告读取→仅清理新测试公告证据继续有效，没有重跑无关 30 项部署 E2E。
- A 保留隔离普通 relay 的价格修改即时结算 2820/4200/4200 quota 证据；C 五家模拟测试保留，真实调用/计费未验证；E 正式部署链不改，使用其备份、锁与身份校验。

## 配置与未完成的唯一外部条件

需取得 Channel 1 上游 gpt-6-luna 的可核实收费/限额依据，覆盖固定输入的 token 上界、全部输出/隐藏 token 和固定费，并据此确认人民币 1 元的测试预算换算。不能将网关零售价格当作供应商成本。

具备依据后，通过 Root API `PUT /api/console/monitor-admin/:id/cost-contract` 保存 `{confirm:true,contract:{source,valid_from,expires_at,input_token_limit,input_micros,output_micros,fixed_fee_micros,hard_ceiling_micros}}`；服务端填写 endpoint/credential_binding。再按既有 allowlist、published、enabled 及 `/policy` 明确预算配置；写请求要求 `X-ClearAPI-Monitor: 1`。保存合同不会自动启用。当前生产策略和旧归档监测保持原状态，未为了发布关闭任何已开启的功能。

**软件隔离可运行；真实生产动态监测还未具备无条件立即启用的费用依据。** 仅停止受此缺口影响的真实收费执行和激活，继续发布已验证的公告、普通渠道缓存与监测软件修复。供应商真实验收须记录真实 receipt、用量与授权预算后才能改为 PASS。

## 候选与正式发布结果

以下在实际构建、DryRun、正式部署和线上核对后追加；没有发生的操作不记为 PASS。
