## 2026-10-27 发布准备回执（正式部署尚未授权）

- 应用已提交：`1f0d26934a0a9ff540938616c5cc497b68bdf530`；真实 `scripts/deploy-local.ps1 -DryRun` exit 0，`DRY_RUN_NO_PRODUCTION_WRITES`，范围 backend、amd64/new-api，27 个差异文件，正式部署需要新备份。写入总控及本记录后补做最终提交的只读 DryRun，最终候选身份及实测结果保存在 `D:/AI/API/evidence/monitor-release-preflight-20261027/final-dry-run.json` 与对应退出码中。
- 本次费用路径补验四个顶层测试 PASS；MySQL/PG 可选费用子项跳过。其余既有本地权限、前端、媒体及归档删除三库验证证据复用，无完整 CI/E2E 或生产构建。详见总控顶部发布条目及该目录 `release-cost-tests.log`。
- pinned SSH、运行身份、部署锁、既有加密备份只读校验 PASS，已安装部署 helper 与仓库哈希一致；PG03 运行与有效 Compose 均关闭。现网仍是 `fb695711c19106209ddbe7de73cb273a0cd67b20`，规范 OCI config ID `sha256:b7bb8edcc21a898bc4a098fe998bbde7de0929d5aedcb9ecde0ad23eb44bea8c`；候选镜像尚未构建。
- 真实生产只读确认缺少 `console_monitors.deleted_at`。这是明确的显式监测迁移前置，DryRun 不会迁移，现有 helper 不会自动迁移；需要独立授权及备份安排。未进行生产配置写入、数据库迁移、备份启动、服务重启、正式部署或真实上游调用。
- 后续等待“本次授权正式部署”，并解决迁移授权/实施前置；GitHub 不作为部署入口。本节之后旧“未提交”等表述为当时历史状态，以上应用提交记录取代其当前交付结论。

---

## 2026-10-27 补充：取消模型/协议名单，包含图片、音频、视频

本节取代下文 A 中“保留 Luna/Responses 专用条件”的本地实施结论；下文现场快照、原始调查及 B 的记录完整保留。用户明确要求模型/协议不限制，并进一步确认包含图片、音频和视频。本轮仍限本地源码、合成测试和状态记录，没有新增生产操作授权。

- 已删除渠道 ID=1、gpt-6-luna、Responses 的硬编码准入条件；渠道监测也不再要求模型属于渠道目录的 models 列表。模型及协议只保留非空、长度和非法字符等输入校验。界面改为自由填写，移除两项协议选择列表及过时的“只支持 Luna”错误文本。
- 已把所有 channel 监测纳入同一个已有费用合同/预算边界。权限、白名单资格、发布/启用/归档状态、启用渠道、非池渠道、预算、并发、幂等、未完成任务及费用证据仍受保护。没有启用 PG03，没有部署 D 生图。
- 内置文本样本按配置构造 Chat、Responses、Claude、Gemini 请求；实际提交使用目标模型，不再发送固定 Luna 请求。Claude 使用原生鉴权头，Gemini 使用其原生路径和鉴权头。成本观察复用既有 `dto.BillingUsage` 标准化，计入 Claude 缓存输入和 Gemini 思考输出；缺失计量不会当作零。
- 图片、音频、视频及其他原生 HTTP 协议通过费用合同中的 `native_request` 提供供应商匹配的固定样本，不设协议名称名单。支持 JSON、二进制和 multipart 样本、自定义 API Key 请求头、响应类型及 JSON 成功条件，以及异步任务创建后的同源 GET 完成查询。JSON 样本中的标准数量/时长按现有 `dto.MaxImageN` / `relaycommon.MaxTaskDurationSeconds` 检查，JSON model 字段必须与探测目标一致；这些是一致性和费用安全校验，不是特定模型名称名单。
- 对 HTTP 原生样本采用显式 `billing_unit=request` 合同，`fixed_fee_micros` 必须是供应商证据确认的固定样本总费用，覆盖所有非 token 成本及整个任务/查询过程；不能把未证实的估价写成实际费用。按实际动态量计费、没有可验证固定样本总价的供应商不能伪造此合同。文本原有 token 单价合同继续受支持。
- 新合同为 `monitor-native-v2`：凭据绑定同时包含实际 endpoint、渠道凭据、目标模型/协议、实际样本字节及原生请求定义。目标或样本改变后旧合同失效，需重新审核保存，不会默默沿用旧模型价格。没有自动迁移或伪造生产合同。
- 异步任务只在已配置完成状态匹配后报告正常。确认终止的失败报告 error、结束并发占用、保留完整预算预留且不编造实际费用；超时、错误网页或无法确认终止的请求保留未解决状态和预算，不自动退款或重试。没有供应商响应 ID 时不制造虚假 ID。

### 原生样本配置入口

复用已有 Root 接口 `PUT /api/console/monitor-admin/:id/cost-contract`，不新增第二套删除或账务接口。`contract.native_request` 字段：

- `path`：相对于当前渠道 base URL 的供应商实际提交路径；本轮提交传输使用 POST。
- `content_type`、`body_base64`：样本的真实 MIME 和原始字节（支持 JSON、文件 multipart、二进制）。内容必须是已审核测试样本，不能放入真实业务请求或敏感用户资料。
- `key_header`：可选，默认 Authorization/Bearer；自定义合法鉴权头使用渠道现有凭据，不能替换 Host、Content-Type、Content-Length、Connection、Transfer-Encoding 或 Cookie。样本中不要放凭据，生产凭据不经 URL 传递。
- `response_content_type`：期望的实际响应类型。JSON 同步结果需 `success_path`；可用 `success_values` 限定成功值。错误 HTML 不会报告正常。
- `poll`：异步任务的 `path`（含 `{id}`）、`id_path`、`status_path`、`completed_states`、`failed_states`。只在同源查询已确认完成后释放成功结果，不把提交成功当成生成完成。

配置范围是原生 HTTP 请求。没有声称自动实现所有厂商的私有签名、模型映射或 WebSocket/gRPC 等其他传输；供应商需要相符的端点、鉴权、样本和计量证据，未知能力不会伪装成已验证。当前生产 ID=2 的实际配置本轮未重新采集；下文缺项仅是上一轮只读快照。本轮不填写生产费用、预算或白名单。

### 本轮实际验证

使用既有 golang:1.25.1 / Node 24 容器和缓存，`--network none`，仅连接进程内合成供应商。关键命令及观察结果：

- `go test ./service -run '^TestConsoleProbe|^TestConsoleMonitor' -count=1 -v`：18 个顶层测试 PASS，覆盖费用有效期/溢出、单次传输、取消/超时、预算/并发/租约、实际路由 Root 权限、原归档删除权限及原状态流转。原有可选 MySQL/PG 子项未配置而跳过，不记成本轮三库通过。
- 修改鉴权细节及样本目标校验后，仅补跑 `go test ./service -run '^TestConsoleProbeArbitraryModelsAndNativeProtocols$' -count=1 -v`：11 个用例最终全部 PASS。包含四类文本、图片、语音生成、multipart 转写、异步视频、自定义协议，以及 HTML 错误和视频明确失败两种保护路径；验证渠道 7 和未列入渠道 models 的模型可执行，模型/协议变化使合同失效，超量 n 和错误样本模型被拒绝。有效 PCM/multipart 样本仅用于合成传输验证，没有声称实际语音或厂商媒体处理通过。
- `go test ./controller -run '^TestConsoleMonitor' -count=1 -v`：PASS，包含当前目标不再被特定模型/协议拒绝、配置拒绝原因及合成配置闭环。
- `node node_modules/vitest/vitest.mjs run src/features/channel-status/__tests__/administration.test.tsx`：12/12 PASS，包含自由填写目录外模型和视频协议，以及原删除确认/刷新/失败保留交互。
- `node node_modules/@typescript/native-preview/bin/tsgo -b`：PASS；form、administration 测试和 static-keys 的 oxlint：0 errors / 0 warnings。修改 Go 文件 gofmt、前端修改文件 oxfmt、七语言 JSON/原词条保留检查和 `git diff --check` 通过。
- 本轮未新增 DDL、ORM 字段、SQL 事务或驱动变更；B 的真实三库删除/迁移证据复用，不重复完整矩阵、CI、生产构建或 E2E。没有真实上游或真实计费验收，不能将合成验证写成厂商验证。

主要新增/修改在 `controller/console_monitor_probe.go`、`model/console_monitor_control.go`、`service/console_probe_request.go`、`service/console_probe_cost.go`、`service/console_probe_transport.go`、`service/console_probe.go`、现有两份后端监测测试、`monitor-admin-form.tsx` 及前端交互测试；旧的仅限 Luna 提示从七语言删除，其余旧词条和保护标识保留。B 的软删除实现保留。

上游凭据处理参照 [OWASP Authentication 的 TLS 要求](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html#transmit-passwords-only-over-tls-or-other-strong-transport) 及 [Session Management 的传输保护原则](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html#transport-layer-security)：保留生产 HTTPS、禁止重定向转发凭据、凭据仅进入请求头、不输出请求/凭据日志。没有改登录、会话或浏览器鉴权流程，不声称完成全量 ASVS 认证。

原始日志：`D:/AI/API/evidence/monitor-all-protocols-20261026/` 中的 `final-service-tests.log`、`final-native-tests.log`、`final-backend-tests.log`、`frontend-tests.log`。修改仍在 `codex/clearapi-monitor-archive-fix-20261026`，HEAD 仍为 fb695711c19106209ddbe7de73cb273a0cd67b20，未提交/推送/部署。后续生产使用仍需单独授权 B 的显式监测 schema 迁移、v2 合同/样本配置及受控部署。到此停止。

---

# 2026-10-26 ClearAPI 监测诊断与归档目录删除（仅本地）

## 工作区与边界

- 初始实际目录：`D:/AI/API/worktrees/clearapi-controlled-launch-20261020`；分支 `codex/clearapi-non-pg03-rc-20261022`；HEAD `f30b247383ee5a18f370d1ad65b30204d32c8833`。该目录存在大量既有修改，旧版监测实现不含当前生产费用合同路径。
- 本轮从用户指定生产源码 `fb695711c19106209ddbe7de73cb273a0cd67b20` 建立隔离工作树：`D:/Codex/worktrees/clearapi-monitor-archive-fix/clearapi-controlled-launch-20261020`，分支 `codex/clearapi-monitor-archive-fix-20261026`。修改尚未提交、未推送。初始目录只追加本轮总控条目，保留原内容和其他修改。
- 用户已明确澄清 B：删除已归档的**监测目录条目**，保留真实上游渠道及历史数据；并非调用 `/api/channel/:id` 删除真实渠道。
- 本轮没有部署、重启生产、写生产数据库、修改生产配置/价格/余额/账务/退款/凭据或发送真实上游请求。PG03 保持关闭，D 生图未部署。现场调查仅 SSH 只读日志与 PostgreSQL `BEGIN READ ONLY` 查询；本地 HTTP 测试只使用合成服务。

## A：真实根因及结果

- 现场运行镜像 tag 为 `clearapi-local:fb695711c19106209ddbe7de73cb273a0cd67b20`。日志显示 `/api/console/monitor-admin/2/switch/enabled` 多次 PUT 返回 **HTTP 403**，例如 `2026-10-26T14:46:00Z`、`14:50:31Z`（上海时间 22:46、22:50）。未重放生产 PUT。
- 监测 ID=2：渠道 1、模型 `gpt-6.1-sol`、`openai_chat`、published=true、enabled=false、allowlisted=false，daily_requests=12，request_cost_micros=1。渠道 1 启用且包含该模型，没有模型映射。
- **首先触发的真实条件**：`channelConsoleProbeAdapter.Authorize` 只支持渠道 1 / `gpt-6-luna` / `openai_responses`，模型或协议不匹配直接返回 `MONITOR_TARGET_DENIED`，尚未读取费用合同。页面“费用合同和预算策略”是固定提示，不能作为这次拒绝的根因。
- 另有独立缺项：`options` 没有任何 `console_monitor.cost.<id>` 合同；全局 `console_monitor_policies` enabled=false、daily_requests=0、daily_cost_micros=0。原 ID=1 的 Luna/Responses 目录已归档、未发布、未加入白名单，单目标预算也为零。
- 现有传输请求体、usage 验证及成本证明绑定了固定 Luna/Responses 请求。直接取消模型/协议校验会向错误模型发请求，因此本轮保留安全阻断，不增加 Chat 探测协议，不伪造供应商合同或开放执行。
- 最小源码修复：不支持的目标改为明确的 `MONITOR_TARGET_UNSUPPORTED`（HTTP 403）；说明仅支持的渠道/模型/协议；补充白名单/发布、合同和未完成探测的可操作错误说明，前端确认框展示实际原因并补齐七语言。新错误仍包装 `ErrMonitorDenied`，保留执行器既有拒绝记录和状态语义。
- 生产日志未保存原响应体。旧响应 `{"success":false,"code":"MONITOR_TARGET_DENIED","message":"MONITOR_TARGET_DENIED"}` 来自源码映射，不能称为生产抓包。本地回归实际验证新 HTTP 403 响应中的 `code` 和说明文本，并验证缺失/损坏合同返回 409、失败不启用也不创建探测 attempt。
- **结果区分**：源码诊断修复通过；支持的合成目标本地启用/探测闭环通过；ID=2 生产启用仍阻断，真实上游未验证，生产尚未部署。
- 后续最小人工决策：当前 Chat/Sol 目标继续暂停；若选用现有支持的 Luna/Responses 目标，需要单独授权恢复/创建对应目录、设定目标预算、在“监测目标”页加入白名单并发布，再由供应商事实提供费用合同和全局预算。费用合同、全局策略目前**没有后台编辑表单**，已有 Root API 分别是 `PUT /api/console/monitor-admin/:id/cost-contract` 与 `PUT /api/console/monitor-admin/policy`；合同读取来自 options，策略直接读取策略表，不是站点 OptionMap。启用执行后会产生供应商费用，预算不是供应商强制限额。以上操作本轮均未执行。

## B：实现与数据安全

- 监测目录通过 `channel_id`、`target_kind/target_ref` 引用业务目标；路由、模型、分组、账号和用户请求引用真实渠道或业务 ID，并不以监测目录为路由。`ConsoleProbe.MonitorID`、`ConsoleMonitorAttempt.MonitorID` 是历史归属；费用合同以目录 ID 为 option key。
- 源码没有监测目录删除 API、关联删除钩子或关系字段。现场 PostgreSQL 查询也未发现指向/来自 `console_monitors` 的外键。现场 ID=1 无探测历史，ID=2 有一条 probe，均无未完成 attempt；没有删除任何现场记录。
- 复用项目已有 GORM `DeletedAt` 软删除模式，为目录增加 nullable `deleted_at` 及索引。删除仅软标记目录并释放 nullable identity 唯一键；原 ID、名称、目标和所有历史原行保留，不执行物理 DELETE。重新添加相同目标获得新 ID，不继承原历史或费用合同。
- 新接口 `DELETE /api/console/monitor-admin/:id` 位于原有 RootAuth、SessionCookieOriginGuard、ConsoleMonitorWriteGuard 组，必须 JSON `confirm=true` 和既有监测请求标记。普通管理员、普通用户、禁用用户和匿名身份均不能删除。
- 后端事务持有与状态变更及探测准入相同的策略锁；要求 archived=true、enabled=false、published=false，且没有 finished_at=0 的 attempt（即使 lease 已过期仍阻断）。重复/并发重复删除幂等；删除失败时 identity 释放与删除标记同时回滚。
- 目录列表、白名单和调度默认查询过滤软删除条目。公开历史仍遵守发布/归档保护；Root 历史接口仍按原 ID 读取保留的 probes，管理员必要时可通过 Unscoped 读取原目录归属。合同、attempt、probe、真实 channel/ability、用户余额、钱包、消费/退款/用量、供应商账单与审计历史均无删除或更新路径。只有成功操作追加一条既有管理审计事件。
- 前端仅在监测管理的已归档目录显示“删除”；独立 `MonitorDeleteDialog` 复用 `ConfirmDialog`，展示名称与 ID，保留失败说明，成功后刷新目录/白名单/状态查询。未新造通用弹窗或修改渠道管理原删除逻辑。
- **发布前置条件**：此版本需要通过现有显式监测迁移 `MigrateConsoleMonitoringDB` / `cmd/monitor-init` 增加 `deleted_at`。未迁移时写接口返回 `MONITOR_NOT_INITIALIZED`，状态页呈现未初始化，不隐式写数据库。生产迁移和后续 Windows→SSH helper 部署需另行授权；本轮没有执行。

## 修改文件

- A：`controller/console_monitor_probe.go`、`controller/console_monitor_admin.go` 的错误映射，`web/src/features/channel-status/monitor-admin.tsx` 的错误展示。
- B：`model/console_monitor.go`、`model/console_monitor_control.go`、`controller/console_monitor_admin.go` 的独立删除 handler、`router/api-router.go`、`web/src/features/channel-status/monitor-delete-dialog.tsx` 及管理页入口。
- 验证：集中扩展现有 `controller/console_monitor_probe_test.go`、`service/console_probe_test.go`、前端 `administration.test.tsx`；现有 `model/console_test.go` 仅补齐读模型已经依赖的 Channel 表夹具，修复实际暴露的 `no such table: channels` 测试失败。
- 翻译：`web/src/i18n/static-keys.ts` 和七个 locale，每种语言仅新增七项；使用技能规定的临时脚本和 `bun run i18n:sync`，随后由同一脚本限定保留本任务新增项，避免重排原词条或填充无关旧缺项。原有翻译值、受保护标识原字节和插值均保留；临时脚本已移除。

## 实际执行的验证

Go 使用本地缓存的 `golang:1.25.1` 容器，挂载此工作树、`codex-go-mod-cache` 与 `codex-go-build-cache`。无外网的隔离容器网络仅允许本地合成 HTTP 服务；数据库测试连接本轮新建的专用临时 MySQL/PostgreSQL，与生产及其他开发实例无连接。测试完成后仅移除本轮创建的三个隔离容器。

| 命令（容器工作目录 /app） | 最终结果 |
| --- | --- |
| `go test ./controller -run '^TestConsoleMonitor(EnableDenials\|ConfiguredHTTPFlow\|TriggerRecordsCostDenialWithoutDispatch)$' -count=1 -v` | PASS；三个顶层测试，含五种启用拒绝条件及合成闭环 |
| `go test ./service -run '^TestConsoleMonitorAdminAuthorizationPublicationAndFailClosedExecution$' -count=1 -v` | PASS；实际路由 Root 权限、确认、请求标记、重复删除、列表过滤及原状态流转 |
| `go test ./controller -run '^TestDeleteArchivedConsoleMonitorDatabaseMatrix$' -count=1 -v` | **三库全部 PASS，无跳过**；SQLite 3.50.4、MySQL 8.4.11、PostgreSQL 17.6 |
| `go test ./model -run '^TestConsoleDatabaseMatrix$' -count=1 -v` | SQLite PASS；此额外既有读模型测试的 MySQL/PG 子项未配置并跳过，不冒充上项三库验证 |

三库删除测试各自验证 fresh migration 两次、带原目录/历史的上一版 schema 重建后升级两次、identity 唯一性、状态及未完成探测阻断、并发重复删除、注入删除失败后的事务回滚，以及真实渠道、probe/attempt、用户额度、钱包、退款、计费 reservation、**独立日志数据库**中的 token 用量/请求消费记录保持不变。没有重跑完整三库业务回归、CI 或 E2E。

前端使用 Node 24.22.1 容器和已有依赖卷（不安装/升级依赖）：

- `node node_modules/vitest/vitest.mjs run src/features/channel-status/__tests__/administration.test.tsx`：**11/11 PASS**，含原因展示、只显示归档删除、取消/二次确认、成功刷新及业务失败保留目录。
- `node node_modules/@typescript/native-preview/bin/tsgo -b`：PASS。
- `node node_modules/oxlint/bin/oxlint -c .oxlintrc.json src/features/channel-status/monitor-admin.tsx src/features/channel-status/monitor-delete-dialog.tsx src/features/channel-status/__tests__/administration.test.tsx src/i18n/static-keys.ts`：0 errors / 0 warnings。
- 上述四文件 `oxfmt --check`、修改 Go 文件 `gofmt`、`git diff --check`：PASS。
- 七语言逐项检查：每种语言七个新增键，原有全部值不变、插值集合一致。没有重跑完整生产构建或部署。

本地原始证据位于 `D:/AI/API/evidence/monitor-archive-fix-20261026/`：`monitor-readonly.json`、`delete-database-tests.log`、`focused-backend-tests.log`、`frontend-tests.log`、`frontend-static-checks.log`。历史失败输出保留；上表只将最终实际执行且通过的项目记录为 PASS。

## 交付状态

- A：根因已确认、错误诊断源码及本地功能验证通过；生产 Chat/Sol 目标仍有协议支持及必要配置阻塞，不声称已能启用。
- B：本地实现及必要测试通过；需要另行授权监测 schema 迁移和受控部署，才可在生产使用。
- 当前源码未提交、未推送，生产未部署、未修改。完成本轮工作并停止，等待后续指示。
