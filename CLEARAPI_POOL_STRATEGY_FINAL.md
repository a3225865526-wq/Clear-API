# ClearAPI 账号池、运营与客户端选型最终报告

日期：2026-10-30（Asia/Shanghai）。本轮为独立架构支线；未部署生产，未操作香港 VPS、生产数据库、真实用户数据或 PG03 master。

**明确推荐：未来多厂商执行层采用独立 Sub2API，内部运营采用其原有后台，终端用户统一使用 ClearAPI。当前继续使用第三方普通 API 渠道。PG03 候选和全部验收成果保留，不删除、不重写，也不作为普通渠道的强制依赖。**

这是源码和局部实验支持的技术选择，**不是 Sub2API 已完成 ClearAPI 端到端验收或已可生产启用的声明**。本轮 Sub2API 已从固定源码构建并启动，但管理 API 被其强制部署条款确认门禁阻断；没有代替所有者接受条款或绕过门禁。其余集成项按下面的证据边界交付。

## A. 固定源码、工作区与历史证据

| 项目 | 实际版本/位置 |
|---|---|
| 原主工作区 HEAD | `f30b247383ee5a18f370d1ad65b30204d32c8833`；大量既有未提交修改保留 |
| GitHub production/main，本轮 gh API + fetch 核实 | `716cba323d59ef9c5a8e942a1678ce09f315a1cf` |
| 本轮 ClearAPI 基线/最新 PG03 Gateway 候选 | `87732dc22fcecdc5195833fff34785b7c477bda7`，其发布基线为 `732a2d5162c15604aa7ec7bbba12bb5c9d356031` |
| 当前 PG03 Executor 候选 | `1264c6c178fc9ef135b36fd9821daf83fba56bdc`，发布基线 `201ce387c12750f545009295636375786af98204` |
| 最新 CLIProxyAPI 参考 | `90c447d3a8727d115aa7552ec93ce023736ed492`，`D:/AI/API/references/cliproxyapi` |
| 最新 Sub2API 参考 | `d851a38477597157e78450d30af2d8504764d085`，`D:/AI/API/references/sub2api` |
| 本轮独立工作区 | `D:/AI/API/worktrees/clearapi-pool-strategy-20261030` |
| 分支 | `feature/pool-strategy-integration`；基于 PG03 候选，未用 main 覆盖候选 |

参考仓库通过 agent-reach 的 GitHub/gh 路径克隆，与 ClearAPI 工作树独立。完整 worktree 清单、原工作区 dirty 文件清单见 `evidence/pool-strategy-20261030/workspace-inventory.json`；版本与所审阅关键文件 SHA256 分别在 `versions.json`、`reviewed-source-sha256.json`。应用聊天清单显示前一个 Sub2API 任务 idle、相关 PG03 任务 notLoaded；这只用于识别并行上下文，不等于全机进程互斥锁。

原主工作区总控 10 月 29 日的最新 Git 交付/出口记录已追加至本支线总控，保留该支线基线的全部历史。原工作区和 Executor 工作区未修改。Executor 原有三个未跟踪诊断目录也保留。

复用 `evidence/pg03-general-pool-20261029/RESULT.md`、`verification.json`、`source-manifest.json`，以及原主工作区 `evidence/pg03-production-finish-20261029/RESULT.md`、`pinned-candidates.json`。历史有效结论：隔离 Codex Responses SSE 四次真实调用、四条日志、合计 112 units，对账及 durable ledger/outbox 通过；账号级调度、CAS、租约、零派发回退有定向证据。它们不是五家厂商均通过的证据。生产最后记录为 Gateway `732a2d51` / Executor `201ce387`、master=false；本轮未重新读取生产，不能冒充新鲜生产快照。

## B. 多厂商执行能力

### 真实源码核实，而非 README 推断

| 厂商 | 当前 Executor 候选 | 最新 CLIProxyAPI | Sub2API 固定源码 | 进入当前 PG03 的缺口 |
|---|---|---|---|---|
| OpenAI / Codex | Codex OAuth、API Key 配置、Responses/SSE；PG03 实证仅订阅 OAuth 路径 | 同类执行器、刷新及传输实现；没有本项目 PG03 定制证明链 | OpenAI OAuth/API Key、Codex 模型目录、专用账号调度 | OAuth 已有隔离成功证据；API Key 不能据此视为已准入 PG03 |
| Claude | Claude OAuth/API Key，`claude_executor*` 有执行、刷新、流处理 | 同类能力存在 | Anthropic OAuth/setup-token/API Key，原生 Messages 及转换路径 | 当前派发门禁拒绝非 codex；缺账号级自动发现、dispatch fencing 和计量证明适配 |
| Gemini | `GeminiExecutor` 明确用于官方 API Key；Vertex/AI Studio/Antigravity 另有路径 | 官方 Gemini API Key；`sdk/auth/filestore.go` 仍跳过 `type=gemini`，不能声称 Gemini CLI OAuth 已支持 | Gemini OAuth（Google One/GCP 等）、API Key、原生 Gemini 路由；另有 Antigravity | ClearAPI catalog 中 Code Assist OAuth 禁用；需补执行/授权集成，不能以 API Key 替代订阅 OAuth |
| Grok / xAI | `xai_executor*`、xAI auth 有实现；ClearAPI Grok Web/Build catalog 另指向 grok2api | xAI 执行及刷新/传输实现存在 | Grok OAuth/API Key、专用 quota/model/service 及管理表单 | xAI、Grok Build、Web SSO 不是同一账号类型；不能直接映射通过。Web 估算 usage 不等于精确计量 |
| Kimi | `kimi_executor.go` 有设备认证相关刷新；普通 Moonshot Key 可走兼容渠道 | 同类 Kimi 执行器、refresh 与 auth 测试 | Kimi API Key，支持 pay-as-you-go / Coding Plan 配置及额度路径 | Sub2API 的 Coding Plan Key 不等于 Kimi 设备 OAuth；当前 PG03 不接纳其自动调度/计量 |

证据入口：Executor `sdk/cliproxy/service_models.go`、`internal/runtime/executor/{codex_pool_models,gemini_executor,kimi_executor,xai_executor}.go`；最新 CLIProxyAPI 同类文件及 `sdk/auth/filestore.go`；Sub2API `backend/internal/domain/platforms.go`、`backend/internal/service/{gemini_oauth_service,grok_oauth_service,account_kimi_coding_test}.go`、`frontend/src/components/account/CreateAccountModal.vue`。可在上述固定 SHA 的 GitHub tree 中定位，文件摘要已归档。

### 为什么不是“适配器存在，PG03 只加几个名字”

1. Executor `sdk/cliproxy/auth/pool_guard.go:415` 明确只允许 `Provider == "codex"` 且 OAuth。之后要求执行器实现 `PoolDispatchFencing()`；已找到的该实现位于 Codex 执行器。
2. `pool_guard.go` 的 lease 绑定 `usage.WithCodexMeteringObserver`；receipt、幂等和已开始后不得回退的边界必须保留，不能删除这些校验来开新厂商。
3. `codex_pool_models.go` 才有账号认证后的 `DiscoverPoolModels`；`service_models.go` 中其他厂商大量使用静态 registry/config 模型，不是已确认账号支持的动态清单。
4. Gateway `service/accountpool/policy_snapshot.go:131` 的显式成员路径限制 codex；自动路径还需与 Executor 的身份/模型同步及协议门禁一致。只修改这一处不会完成集成。
5. `service/accountpool/catalog.go` 的界面目录展示了更多平台，但 `Claims=unverified`、Code Assist 禁用、Grok 计量限制仍存在。目录不是可执行准入。

预计需要处理的明确模块至少包括 Gateway `catalog.go`、`policy_snapshot.go` 和 admission/执行器管理衔接，以及 Executor `pool_guard.go`、`pool_model_sync.go`、`service_models.go` 和对应厂商执行器的发现/fencing/metering；另需集中回归。**这不是承诺固定文件数或开发小时数。当前为其他厂商实际改动的 Gateway/Executor 生产文件数均为 0。**

调度算法可复用，通常不应重写 Scheduler；但筛选输入、会话、失败分类需要新厂商证据。租约存储机制可复用，派发/取消/结算生命周期却不能无验证照搬。最终 ClearAPI Billing 不需要另写，新增上游 usage 到既有计费事实的衔接必须验证。当前未证明需要 schema 变更；若仅扩展现有字符串/provider 数据可不迁移，但不预先保证所有运营扩展零 DB 改动。修改上述共享核心会引入已验证 Codex 的回归风险，因此本轮不实施多厂商补丁链。

### 16 项执行能力对照

| 能力 | PG03 + 当前 Executor | 独立 Sub2API |
|---|---|---|
| OAuth / Key 接入 | Executor 多种方式存在；PG03 Codex OAuth 实证，其他需准入 | 原后台按平台选账号类型；Kimi 主要 Key，不能泛称五家 OAuth |
| 凭据刷新 | Executor 有 provider refresh；长周期到期本轮未实测 | token_refresh_service + 后台手动刷新存在；长周期未实测 |
| 到期/失效 | PG03 身份、generation、过期拒绝及撤销已有定向证据 | 状态、expired/auto_pause、刷新失败及冷却有实现；本轮未真实账号注入 |
| 自动发现 / 动态更新 | Codex 账号 manifest + 原 model_update 自动同步已验；其他多为静态/config | Codex 目录、Grok 目录、available models、mapping 存在；不同平台能力不统一，不能把空 mapping 当确认支持 |
| 健康 / 额度 | 有失败状态、冷却和 quota observation；部分仅历史观测，非主动查询 | provider quota、状态恢复、账号测试及额度组件成熟；仍依厂商/账号类型 |
| 调度 / 会话亲和 | Codex 定向证据，Scheduler 复用 | openai_account_scheduler、concurrency_service、session hash/sticky 及 Redis 依赖 |
| 并发 / 回退 | 租约/CAS/续期/释放及零派发回退证据；已开始/未知不得重放 | 账号并发、槽位、失败切换实现；不等同于 PG03 receipt 语义，需对失败重试预算单独验收 |
| SSE | Codex Responses SSE 历史隔离实证 | Chat/Messages/Responses 有处理器；本轮 drain/usage 单元契约验证，非双服务全事件验收 |
| 取消 | Gateway 创建带请求 Context 的上游请求；PG03 租约有取消释放边界 | 流及部分 Responses 使用 WithoutCancel；客户端断开后可能继续 drain 上游以取得 usage，**不保证立即中止推理** |
| 实际 Usage | Codex 历史明确字段/receipt/最终账务证明 | 服务读取 upstream usage；协议转换、断流、缺 usage 时各有处理，不保证所有厂商都提供精确事实 |
| ClearAPI 最终计费 | 现有预扣/结算/退款/日志，历史隔离对账通过 | 通过既有渠道接收 usage；内部 simple 模式只记用量、不扣用户余额；双服务恰好一次结算本轮未验 |

## C. 运营后台完整清单

标记含义：已完整存在=存在可达 UI 及对应操作/接口源码，不是本轮浏览器实操通过；部分存在=明确能力受限；缺失=在所检查池页面和接口契约中没有该运营功能；无法确认=证据不足。所有“完整”不代表每个厂商均同等支持。

| 功能 | PG03 / ClearAPI upstream-pool | Sub2API 后台 |
|---|---|---|
| 账号列表 | 已完整存在，accounts-table/card | 已完整存在，AccountsView |
| 添加账号 | 已完整存在，create-account-dialog（受 catalog 准入限制） | 已完整存在，CreateAccountModal |
| OAuth 登录/导入 | 部分存在，Codex/Claude/Kimi 按类型；Gemini CLI 禁用 | 已完整存在于适用平台，Create/ReAuth + OAuth composable；Kimi 不等于设备 OAuth |
| API Key 导入 | 部分存在，Executor 配置与普通渠道具备；池页不统一开放五家 | 已完整存在，账号表单按平台选择 apikey |
| 批量添加 | 已完整存在，CredentialImportPanel 多文件结果/重试，类型受限 | 已完整存在，导入与批量账号操作路径 |
| 暂停/启用 | 已完整存在，版本化 pause/resume | 已完整存在，status/schedulable 操作 |
| 批量管理 | 部分存在，批量文件导入；缺统一批量暂停/恢复界面 | 已完整存在，bulk action bar / bulk edit |
| 自动刷新凭据 | 部分存在，Executor 自动刷新；池页有最后刷新和重新授权 | 已完整存在，后台刷新动作 + token_refresh_service，适用 OAuth 类型 |
| 账号到期提醒 | 部分存在，到期/失效状态；未发现统一主动提醒 | 部分存在，expiry 列、自动暂停；主动通知全平台无法确认 |
| 健康状态 | 部分存在，run_status/last_error/手动同步；非统一主动探测 | 已完整存在，状态指示、测试、恢复操作；探测范围依类型 |
| 剩余额度 | 部分存在，QuotaSummary；可能 observations_only/unknown | 部分存在，AccountUsageCell/QuotaInfo，依平台数据可得性 |
| 配额更新时间 | 已完整存在，checked_at/next_refresh/stale 显示 | 部分存在，usage batch/refresh、usage window；无统一全平台更新时间保证 |
| 已支持模型 | 部分存在，模型准入查询和平台能力，不是完整账号模型清单页 | 部分存在，available models/Codex manifest/model mapping；实际访问能力仍需发现/验证 |
| 账号并发数 | 缺失于现有池 UI；Executor 的 lease 上限不等于页面展示 | 已完整存在，AccountCapacityCell 显示 current/max |
| 当前在途请求 | 缺失于现有池 UI；Executor 管理接口另有 in-flight | 部分存在，当前并发/槽位可见，不保证有每请求完整在途列表 |
| 账号成功率 | 缺失于现有池 UI | 部分存在，账号/用量统计和运维错误；统一账号成功率口径未确认 |
| 最近失败原因 | 已完整存在，detail last_error | 已完整存在，AccountStatusIndicator.error_message 及运维记录 |
| 调度记录 | 部分存在，配置/策略事件与日志，缺运营决策时间线 | 部分存在，scheduler score/调度动作/运维，不等于每次决策历史 |
| 账号使用量 | 缺失于现有池详情的独立聚合；有 Gateway 消费日志 | 已完整存在，AccountStatsModal/TodayStats/UsageView |
| 用量趋势图 | 缺失于现有池页面 | 已完整存在，AccountStatsModal 日用量/成本图 |
| 账号与模型关系 | 部分存在，按模型准入查询/路由管理 | 部分存在，mapping/manifest 与分组；不自动证明账号能访问全部模型 |
| 代理配置 | 缺失于现有池 UI；Executor YAML/账号代理可配置 | 已完整存在，账号 proxy_id、Proxy 管理及代理显示 |

ClearAPI 证据入口：`web/src/features/upstream-pool/{index,detail,api,catalog}.tsx/ts` 及 `components/{accounts-table,account-row-actions,account-observations,credential-import-panel}.tsx`。Sub2API 证据入口：`frontend/src/views/admin/AccountsView.vue`、`components/account/{CreateAccountModal,EditAccountModal,AccountCapacityCell,AccountStatusIndicator,AccountStatsModal,AccountUsageCell}.vue`，以及 `components/admin/account/AccountBulkActionsBar.vue`。

管理决定：使用 Sub2API 原后台，不将 Vue 管理台移植到 React。ClearAPI 暂不增加没有真实状态来源的摘要卡片，也不暴露第二套用户前台。未来新增账号只在一个执行层管理，PG03 作为保留候选不与 Sub2API 同时承担同一账号的调度。

## D. 四种客户端现状、改造与使用

| 客户端 | ClearAPI 原状 | Sub2API 源码现状 | 本轮 ClearAPI 最小实现 |
|---|---|---|---|
| Codex CLI | 静态文档提示未完整验收；Key 页有 CC Switch 导入 | UseKeyModal 有认证模式、模型目录、config.toml 和复制 | 选 Responses 可用模型，生成用户级 config.toml，env_key=CLEARAPI_API_KEY |
| Claude Code | 静态兼容边界说明、CC Switch | UseKeyModal 的环境配置/说明/复制 | 选 Anthropic 模型，生成 settings.json.env 的 BASE_URL、AUTH_TOKEN 占位符和 MODEL |
| OpenCode | 通用文档，需手工核对 provider 格式 | UseKeyModal 有 provider/models 配置 | 选 Chat 模型，生成 opencode.json；@ai-sdk/openai-compatible、baseURL、环境变量引用 |
| Cherry Studio | 通用服务商字段说明 | 本轮未确认与前三者同等的专用向导，不能推断完整支持 | 选 Chat 模型，复制 Base URL/Key 占位符/模型参数，按客户端自定义服务商字段填写 |

新版区块位于现有 `/usage-guide`，不新建页面。模型来自已有 `/api/user/models` 与 `/api/pricing` 交集，按真实端点标记 `openai-response` / `anthropic` / `openai` 筛选。无匹配模型时不生成配置，获取失败显示重试。这里列出的是用户可用分组的模型集合，特定 Token 的分组/模型限制及健康波动仍由服务器最终执行，不承诺未来请求一定成功。

Base URL 使用系统 status 的 server_address，未配置时使用当前网站 origin；保留部署路径前缀，规范 `/v1`，拒绝含凭据、query/hash 或非 HTTP(S) 的地址。Codex/OpenCode 使用 `/v1`，Claude BASE_URL 使用根地址。Cherry 给出 OpenAI Chat 的 `/v1` Base URL，客户端自动补版本时须核对最终路径，避免重复 `/v1`。

密钥不读取、不输入、不持久化到本向导，也不放入 URL 或日志。Codex/OpenCode 引用用户本地环境变量；Claude/Cherry 留 `<YOUR_API_KEY>`。用户自行将片段合并到已有配置，不覆盖其他设置。路径采用客户端文档的 `~` 表示法；Windows 对应用户主目录。页面说明新会话、选模型、短消息、核对消费日志；这是一条验证说明，不会自动发收费请求。

复用审查：继续使用现有 `CopyButton`、`ModelSelector`、NativeSelect、EmptyState、LoadingState、ErrorState。客户端选择器是四个固定选项，模型使用既有业务选择器。`pricing/model-details-api` 是单模型 API 请求示例，`keys/dialogs/cc-switch-dialog` 是携带 Key 的本机导入链接，均不直接提供本任务所需四种客户端文件/参数生成；因此新增小型 feature 组合与纯配置生成函数，不新增通用 UI 框架，也不扩展有密钥的 URL 导入。

格式核实于 [OpenAI 官方 gateway 配置](https://learn.chatgpt.com/docs/enterprise/connect-to-a-gateway)、[Claude Code gateway 接入](https://code.claude.com/docs/en/llm-gateway-connect)、[OpenCode provider 文档](https://opencode.ai/docs/providers/)、[Cherry Studio 自定义服务商](https://cherryai.com.cn/docs/zh-cn/pre-basic/providers/zi-ding-yi-fu-wu-shang/)。Codex 使用 Responses 和 env_key；OpenCode 本轮明确选 Chat 兼容 SDK。没有复制 Sub2API 的硬编码模型表或未经本项目验证的远程目录配置。四个真实客户端、工具调用、图片、多轮恢复均未端到端实测。

## E. 实际集成与验证结果

### 已有渠道无需开发新网关

ClearAPI 不仅有 OpenAI(type=1)、Anthropic(type=14)、Gemini(type=24) 普通渠道，**已经有 Sub2API(type=59)**。`relay/channel/sub2api/adaptor.go` 嵌入 `newapi.Adaptor`：请求 URL 保留原协议路径；Chat/Responses 不做额外协议改写；Claude/Gemini 复用本项目对应适配器及响应处理。普通 type=1 适合 OpenAI 路径；多协议内部服务优先 type=59，而不是强行用 type=1 代表所有协议。

内部接入配置应为：type=59，Base URL=`http://sub2api:8080`（不追加 `/v1`），Key=Sub2API 专用内部 API Key，模型填写内部对应组实际可用 ID，绑定测试分组。Key 不用 ClearAPI 普通用户 Key；不要把下游 Key/Authorization 透传为上游凭据。若同名保留可不配 mapping；需别名时使用现有 model_mapping，并核对 usage/日志仍关联到正确实际模型。

Sub2API 已有 `/v1/chat/completions`、`/v1/responses`、`/v1/messages`、`/v1beta` 路由；按内部 Key 绑定组的平台选择 handler，不是任何平台/任何 Key 均支持所有协议。优先保持原生协议，跨平台 fallback 才可能产生转换。

会话：Gateway body DTO 保留相关标准字段，额外会话头须用现有明确 header override/透传白名单配置；不要开放全部鉴权头。Sub2API 的 prompt_cache_key/session hash 和 Key/用户隔离有源码，不能把它与 PG03 session/receipt 自动等同。实际亲和持续性仍需双服务测试。

### 本轮实际观察

- 固定 Sub2API 后端 `go build -buildvcs=false -o /out/sub2api ./cmd/server` 成功；未使用 `-tags embed`，所以本地实例没有管理前端，不能声称浏览器后台已验。标准未来镜像须构建原有前端并 embed。
- 新建专用 Compose 项目、网络及卷；PostgreSQL **17.11**、Redis **7.4.11**；端口仅绑定 `127.0.0.1:18881`。新建随机本地管理员密码和 secret，只留在仓库外运行目录，未复制生产凭据或数据。
- `/health`=200；正常管理员登录=200；`GET /api/v1/admin/accounts`=423、`ADMIN_COMPLIANCE_ACK_REQUIRED`。没有调用接受确认接口，没有手改 DB 绕过。
- 四个协议入口无认证请求均返回 JSON 401（即使 Accept:SSE），证明入口和鉴权可观察；**不证明推理、SSE 或 Usage 已通过**。
- ClearAPI `go test ./relay/channel/sub2api ./relay/channel/newapi -count=1 -timeout=90s` PASS：Sub2API 现有 3 项 URL/compact/Claude 参数行为测试通过；newapi 无独立测试文件。
- Sub2API domain 定向测试 `go test ./internal/domain -run TestPlatform -count=1` PASS。
- Sub2API `go test -tags unit ./internal/service -run 'Test(HandleCCStreamingFromAnthropic_ClientDisconnectDrainsUpstreamUsage|HandleResponsesStreamingResponse_ClientDisconnectDrainsUpstreamUsage|AnthropicCompatStreaming_RequestContextCanceledDrainsWithoutWriting)$' -count=1 -v -timeout=90s`：3 个顶层测试及取消场景子项 PASS。覆盖合成流的 Chat/Responses 取消后停止写客户端、继续 drain，保留 input/output/cache usage；这不是厂商真实调用，也不是两个完整服务串联。
- 前端最终检查：typecheck PASS；所改 4 个 TS/TSX 文件 lint 0 errors/0 warnings；usage-guide 4 文件 **14 tests PASS**；生产 build PASS。构建的明细输出尾部被输出通道截断，日志保留了 `ready built in 20.5s`，不以文件大小清单代替结果。
- 七语言各新增 7 个 key、49 条翻译；通过 sanctioned script 写入并执行过 i18n:sync。sync 暴露其他语言原有 75 个缺失键及大范围排序/补 fallback，已通过该脚本将无关变化限定回原基线；最后逐语言验证原有 key/value 全部保留，仅新增本任务 7 key。没有把原有缺口称为本轮已修复。

验证过程中的非成功项也保留说明：最初复用依赖缺 Linux rolldown binding，改为独立 Linux named volume 按锁文件安装；离线 Sub2API 测试缺缓存依赖，联网获取后运行；一次未加 unit tag 显示 `[no tests to run]`，不计通过，最终命令明确加 tag。未重复历史 PG03 全矩阵。

### 尚未完成的集成项

**BLOCKED：ClearAPI → 实际 Sub2API → 可控上游的完整成功路径。** 当前管理门禁阻断通过受支持 API 创建账号/分组/内部 Key；用户未完成其明确承诺确认。没有拿旧版本绕门禁，也没有写入造假确认记录。PG03 已有路径的有效隔离实证已复用，但没有把它冒充本轮新增 Sub2API 集成。

因此本轮普通请求成功、Responses 完整事件、真实 SSE、模型映射实际派发、会话亲和、跨服务取消、Gateway 账务恰好一次及同条件性能对比均 **NOT RUN / DEFERRED**。源代码协议入口和局部测试不能替代这些验收。当前没有完整美国 VPS 生产验收。

内部计费边界从源码可确认：`api_key_auth.go` 的 simple 分支仍检查 Key/用户等身份；`gateway_usage_billing.go` 的 simple 且非 Key 窗口扣额路径记录 usage 后返回，不扣用户余额。部署时显式 `RUN_MODE=simple`，`SIMPLE_MODE_KEY_RATE_LIMIT_ENABLED=false`；ClearAPI 保持所有原预扣、结算、退款和日志控制。Sub2API 内部日志只作运营对账，不作为终端用户钱包。上游订阅额度的消耗不等于第二份用户扣款。

避免双调度：ClearAPI 只选服务/渠道，Sub2API 选池账号；该渠道不再绑定 PG03 账号路由。不在两个系统维护同一账号清单。隔离验收先限定单渠道、检查现有重试配置，不在可能已派发后再扩大重试；上线前必须验证两个系统的失败/重试预算与恰好一次账务，不能仅凭“默认有重试”宣称安全。

安全参考已核对 [OWASP ASVS 5.0.0](https://asvs.owasp.org/)、[Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)。本轮不改变服务器认证/会话实现；保留鉴权、确认门禁，不采集/输出 usable credential，客户端配置只给环境引用/占位符。本报告不声明整个产品通过 ASVS 合规认证或 OAuth 全面安全审计。

## F. 性能、资源与依赖

| 项目 | PG03 路线 | Sub2API 路线 |
|---|---|---|
| 同条件额外处理延迟/TTFB/SSE 首事件/总耗时 | NOT MEASURED | NOT MEASURED；完整转发被账号管理门禁阻断 |
| 请求中 CPU/内存 | NOT MEASURED | NOT MEASURED |
| 本轮空闲瞬时采样 | NOT MEASURED，不使用他人运行实例当对照 | app 0.67% CPU / 40.18 MiB；Postgres 2.54% / 61.42 MiB；Redis 0.41% / 4.672 MiB |
| 增量服务 | Executor 1 个；仍需自身持久状态 | Sub2API + PostgreSQL + Redis 共 3 个，基于独立数据库/缓存最简隔离计数 |
| ClearAPI 原 DB | 原三库支持保持 | 不变；Sub2API 自身要求 PostgreSQL，不能推断可改用 MySQL/SQLite |
| 同机部署 | 可行，需保持身份/receipt/ledger持久化 | 可行，用 Docker 内部网；DB可用现有 PG 集群的独立库/角色，但不得共表，Redis优先独立实例 |

空闲采样是在 Windows Docker Desktop、15.37 GiB 容器可见内存、多其他容器运行条件下取得，既非容量规划，也不能推断 A/B 性能优劣。未产生模型推理耗时，不会将其当作网关开销。原始采样及二进制 SHA 在 `idle-resources.json`。本地轻量后端实例日志中外部版本同步有 x509 证书错误，本轮没有关闭 TLS 校验；未来正式镜像须用仓库标准含 CA 的运行层验证受控出口，此处没有厂商网络连通性结论。

## G. 工作量对比与最终决策

| 项目 | PG03 路线 | Sub2API 路线 |
|---|---|---|
| 多厂商池覆盖 | 已验 Codex；底层适配器多，但池门禁/发现/计量专用 | 多厂商账号/UI/scheduler在同一系统；Kimi 设备 OAuth 等不等价能力仍有缺口 |
| 新厂商成本 | 要跨 Gateway 身份/准入、Executor fencing/manifest/usage、集中回归 | 已实现类型主要配置账号/组/Key/渠道；全新协议仍需开发，不承诺只填配置 |
| 运营缺口 | 缺并发/在途/账号聚合用量和趋势等页面 | 大部分已在后台，少量指标仍不统一 |
| 客户端适配 | 统一本轮 ClearAPI 前端增强 | 同左，不迁移终端用户网站 |
| 本轮后端生产文件改动 | 0；未扩厂商 | 0；复用既有 type=59 |
| 本轮共同前端改动 | 4 个 TS/TSX（含测试）、7 个 locale，共11文件 | 同左 |
| 新运行配置 | 无PG03配置改动 | 1 个隔离 Compose 文件；未写生产配置 |
| 新服务/数据库 | 1 Executor；已有 ClearAPI DB 与 Executor 状态 | 3组件；Sub2API独立PG库与Redis |
| Billing 改动 | 本轮无；扩厂商需补usage事实衔接 | 本轮无；simple内部不扣最终用户钱包，跨服务需补结算实证 |
| 延迟/内存比较 | 未测 | 仅空闲采样，无法比较 |
| 后期维护 | Codex现有成果复用高；五家扩展要长期自维护定制安全/计量链和运营页 | 多一服务栈，但上游原生维护账号/运营能力，ClearAPI边界标准化 |
| 扩至五家 | 中高复杂度，不是少量名单变更 | 配置复杂度较低；账号方式/usage精度/取消语义必须分别验，不能声称五家实测通过 |

**A 执行层：选 Sub2API 作为未来多厂商方案；B 运营：选 Sub2API 原后台；C 用户客户端接入：选 ClearAPI 现有使用指南。** 这符合“最少重复系统”目标；不是因为功能列表更长就抛弃现有成果，而是当前 PG03 多厂商扩展会触及共享派发与计量核心，运营页面还需补建。Sub2API 的代价是独立 PG/Redis、较新的 Go toolchain、版本更新及取消/drain差异；这些代价必须保留在运营决策中。

当前不深改两套执行层，不整体升级 Executor，也不同时启用两套账号调度。若未来要求“订阅账号在所有厂商上相同方式 OAuth、立即硬取消、每次派发可证明 receipt”等条件，Sub2API 当前源码也不能直接满足；这些是后续明确需求，不在本轮追加框架。

## H. 实际交付与运行

本轮实际代码及配置提交：`39a7b9f824e45d984d14eb2165ae8340a6cbb9dd`（12 文件、513 行新增；仅本地提交，未推送）。其父提交为本报告 A 节 ClearAPI 基线。报告/总控/证据另行提交。

源码：`web/src/features/usage-guide/client-config.ts`、`client-setup.tsx`、`index.tsx`、`__tests__/client-setup.test.tsx`，七语言 locale。配置：根目录 `compose.pool-strategy-test.yml`。证据：`evidence/pool-strategy-20261030/`。未新增 docs/ 或 plugin 文档，未改定价、钱包、登录、数据库schema或普通渠道行为。

本地 Linux 前端依赖在独立 Docker volume `pool-strategy-web-deps`，按原 `bun.lock` frozen 安装；未改锁文件。可在新工作区 web 下用 Bun 运行 `bun install --frozen-lockfile`、`bun run typecheck`、`bun run test src/features/usage-guide`、`bun run build`；该工作区没有本机 Bun 时，复用此次 Docker 挂载方式。依赖和构建产物未提交。

隔离 Sub2API 重现（需要 Docker，固定参考仓库已克隆）：

```powershell
# 后端无 embed 的本地接口实验；生产须用上游标准镜像流程
# 参考 SHA、完整 go build 及测试命令见本报告 A/E 与 evidence/pool-strategy-20261030/verification.json
# 仓库外 test.env 已有随机测试密码，不要复制/输出其值
$runtime = 'D:/AI/API/runtime/pool-strategy-20261030'
docker compose -p clearapi-pool-strategy-20261030 --env-file "$runtime/test.env" -f compose.pool-strategy-test.yml up -d
# 实验结束保留卷，不执行 down -v
docker compose -p clearapi-pool-strategy-20261030 --env-file "$runtime/test.env" -f compose.pool-strategy-test.yml stop
```

仍需后续集成：负责人在适当的正式/隔离管理台阅读并自行完成其强制确认；随后按 E 节 type=59 连接，补真实服务+mock普通/Responses/SSE/错误/取消/usage与两层账务对账。关闭 pool 不应影响第三方渠道。账号管理 UI 现成，不在 ClearAPI 再造一套。真实厂商验证只在所选代表路径确有需要时进行，隔离且限额；不要求逐模型收费验收。

## I. 美国 VPS 最简准备与新增账号流程

1. 保留现有 ClearAPI Gateway 与第三方渠道。将本分支客户端增强按既有 CI/发布机制集成；本轮没有 push/PR/部署，不声称已运行在 clear999.com。
2. 从固定 Sub2API SHA 构建含原前端的镜像，固定镜像 digest；同 VPS 启动 app、独立 PG 库/角色、Redis 与持久卷。管理入口只绑定回环或受控管理网络，不作为普通用户第二站。为正常 TLS 配置 CA/可用出口，不关闭校验。
3. 负责人完成 Sub2API 固有管理员初始化和条款确认，再进入“账号管理→添加账号→选厂商与实际账号类型→OAuth授权/导入或API Key→绑定内部组→设置并发/代理/到期→账号测试及额度/模型核对”。OpenAI/Claude/Gemini/Grok按其支持的 OAuth/Key 类型；Kimi按实际 Key/Coding Plan，不能套用设备OAuth教程。先单账号，再批量。
4. 在 Sub2API 创建专用内部用户/组/API Key，simple 模式只记内部使用量。每个内部 Key 绑定正确平台组；跨平台优先独立 Key/渠道，避免混组导致不必要转换。绝不迁移 ClearAPI 用户或余额。
5. ClearAPI 后台“渠道→新增→Sub2API(type59)”填 Docker 服务名根地址、内部 Key、已确认模型，先仅给测试分组；禁用该渠道的 PG03 账号路由。普通渠道保留原状态和优先级。账号由 Sub2API 调度，ClearAPI仍管模型售价/用户权限/最终计费。
6. 在新卷/mock上完成 E 节剩余集成验证：成功一次只产生一条最终结算日志，取消/失败不重复扣款；核对缓存/输出usage、mapping、SSE terminal事件和重试上限。再按原发布流程受控启用少量业务。不能把该待办称为已验。
7. 日常新增同厂商账号只在 Sub2API后台添加并绑定内部组，通常无需改ClearAPI源码；新增模型按ClearAPI原模型/定价/渠道自动化办理。新增全新协议厂商先判断Sub2API是否已有真正执行器。
8. 回滚优先禁用新增池渠道/移出其测试或业务组，恢复原第三方渠道选择；保留消费日志/账务，不回滚钱包或抹除DB。不会执行本轮生产切换，PG03 master保持原生产关闭记录。

本轮未修改任何 ClearAPI 数据库行为，因此未触发三数据库矩阵；本地 Sub2API PG17运行不等于ClearAPI三库验证。未执行完整 CI、真实客户端E2E、五家真实上游、美国VPS部署及生产账务验收。完整转发待确认门禁解除后继续，当前交付为**有明确推荐、实际客户端代码与可运行隔离服务配置的部分完成**。
