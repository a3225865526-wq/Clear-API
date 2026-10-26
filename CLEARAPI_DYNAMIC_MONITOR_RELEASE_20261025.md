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

## 正式发布实际结果（2026-10-25T06:17:28.381085+00:00）

- **运行应用完整 Commit：`1aef1aec0e6337992f382836aaeea62c07eb41fd`**；当前工作区干净时经正式 `scripts/deploy-local.ps1 -DryRun` 后执行同一脚本，无 Push/main 合并/旧 GitHub 生产链重启。
- 同工作区实际 Dockerfile 构建成功；本地 engine/digest `sha256:61614562f14aeb12bfd060e766510a1c727802fe92eea588afe6c652484b9c08`。正式 OCI config/image ID `sha256:3c2f84e2b4a162313676320f4aa640114136993dc03fbb20c159c505b9d7a47f`；生产 engine ID `sha256:61614562f14aeb12bfd060e766510a1c727802fe92eea588afe6c652484b9c08`，运行 revision 精确等于上述应用 Commit。归档 SHA256 `996d0af25b4113070b0c83f6436425d2ca7dabac346e41f0044c0b323709919d` 与运行部署 state 一致。
- Gateway CID `3c1e4ab2ae3c4680bde376d9893492ed79b5174a8b9362ff86887064ae474f2c`，created `2026-10-25T06:13:09.68791956Z`，started `2026-10-25T06:13:10.011644231Z`。生产 `/new-api` SHA256 `fcb484e1a07aef6dd2f22d2508aa3df1976c14cb928bfa856674697107ea4a57` 与本地最终镜像逐字节相同；没有把仅源码存在当作镜像包含证明。
- **切换前真实备份 `20261025T061238Z`**：systemd `Result=success; ExecMainStatus=0`。完成 manifest SHA256 `e5c64bf7ba3d36c5d1ec4fa1863b17d13607f765218ce60d1943e3e3201a862c`，密文 `clearapi-backup-20261025T061238Z.tar.gz.gpg`，`12420860` bytes，SHA256 `eb3c0416dd2bf902d527672f04b5f2cd5796b9484816848a8c359c0983c042e2`，重新读取密文校验一致。备份 running_identity 绑定 dc685cbe 前驱与 aaf0638277ab12b71f6df795066e315d196e64952c4aa951d09d6998e0b433e0，不能声称备份绑定新候选。正式 helper 返回 health PASS / rollback false；当前安装 helper/state/backup 字节与仓库一致。
- 首页 HTML SHA256 `e5fa880131fb941e8423fb0de9b65b08c4acd775183eea8883f595c1ccd4253e`；所有入口 JS/CSS 与最终候选隔离容器字节相同。新监测合同提示和两个公告 status 配置 key 均存在于实际生产包。首页、登录、注册、公告设置及渠道状态路由壳 200，公开状态 success=true，无凭据普通 API 401。**路由壳 200 不证明已登录业务操作通过。**
- 生产旧公告 hash `70c1d2a7a57c7a84147bd7e123eca0f6` 与发布前相同，所有价格选项 hash 相同。没有生产测试公告、改价、余额/历史账单操作；注册/密码登录开关与公开状态记录一致。PostgreSQL、共享 Executor、Caddy 的 CID/镜像/started 不变；POOL_ENABLED/DEDICATED 仍 false，D 专属提交与应用页面未纳入。

| 功能 | 隔离证据 | 本次生产证据 | 当前结论 |
|---|---|---|---|
| 公告刷新与旧公告保留 | 9 项联合回归；前一候选真实浏览器新旧公告读取证据保留 | 二进制/JS 精确包含修复；旧公告 hash 不变 | 修复已发布；本轮未在生产试发，生产完整发布操作未验收 |
| 普通渠道缓存 | 新增/禁用/启用、DB/ability/路由跨库证据；本轮 SQLite 回归 | 修复已进入同一镜像；没有生产改渠道或付费路由请求 | 已发布，本轮生产写操作/实际付费路由未验收 |
| 动态监测 | 三库预算/并发/异常；管理 HTTP→传输→持久化→用户视图；合成 provider | 可执行软件进入真实镜像；策略仍原暂停、probes=0 | **PARTIAL，真实收费执行及激活未完成** |
| A 定价/计费 | 原隔离真实网关结算/改价即时生效证据保留 | 原商业配置 hash 不变，没有财务写入 | PARTIAL；不能推定零售/供应商商业价格已验收 |
| C 五家 | 各自模拟流/非流测试证据保留 | 与上一发布的适配实现保留 | 各家 PARTIAL；真实调用/计费未知 |
| E 备份门禁 | 现有锁/证明定向证据复用，不重跑 30 项 | 本次真实备份、归档、身份、健康回执一致 | 本次正式部署门禁 PASS |

资源下载限制：Windows 侧两次公网 JS 下载分别超时（一次零字节，一次 45 秒仅收到部分内容）。随后从 VPS 原生 curl 同时核对源站和公网，HTML、5 JS、1 CSS 全部与候选镜像哈希相同。证明资源内容一致，不证明所有用户地域/网络链路正常；原失败日志一并保留，没有把失败改记为通过。

配置提示：现有唯一监测处于 archived 状态。费用依据补齐后，可用既有 Root `/:id/switch/archived`、`published`、`enabled` 接口及 allowlist/配置接口完成显式恢复与授权，不需要直接改生产数据库。上述配置及真实收费触发本轮均未执行。

**未完成项没有掩盖：** 未收到 Channel1 供应商收费/限额依据，没有发起任何收费推理请求，人民币 1 元预算没有被擅自换成虚构美元预算。动态监测不是已经在线执行成功；唯一待补外部条件为可核实供应商计费/限额依据，之后需在授权预算内做真实任务及费用验收。生产用户登录/公告写入和普通付费 relay 也未冒充验证。详见本目录真实 JSON 回执。后续本文件记录提交只更新文档，不改变实际运行应用 SHA。
