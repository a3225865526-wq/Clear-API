# PG03 通用账号池技术收尾 — 本地链路通过，生产未启用

2026-10-29，Asia/Shanghai。选择方案 A：继续最小修复 PG03。**已完成本轮 Codex OAuth / Responses SSE 路径的代码、定向测试和隔离真实调用；不能报告生产部署、生产 PG03 启用或完整生产验收通过。**

生产最终现场取证于 07:50:08：Gateway `732a2d5162c15604aa7ec7bbba12bb5c9d356031`，Executor `201ce387c12750f545009295636375786af98204`，容器、启动时间和 deployment state 与本轮初始观察一致；helper 已安装，两个环境开关 true。master=false，账号 3/4 version=4/not_admitted，capability=0、grant=0、model-routes=[]，Token 8/渠道 6 禁用、用量 0。不是账号未导入、helper 未安装或旧 Gateway 前驱问题。

**原约束已被本次用户指令取消。** 不再要求 128 输出 tokens、0.05 USD 最终费用硬上限、仅一次派发、固定模型或专用分组灰度。没有修改 Codex 转换器恢复 token 限制，没有建立费用硬封顶或第二套 Billing。生产测试身份原 25000 units 余额保留且未使用，不作为费用硬限或本轮阻断依据。

**实际缺口及处理**

| 模块 | 已有能力 | 确认的问题 | 本轮处理 |
|---|---|---|---|
| 模型能力 | 身份摘要、trust generation、模型撤销、CAS | 静态套餐目录不是账号支持证明；逐账号×模型手工维护 | 用账号自己的 Codex manifest 自动同步 grant.Models 和 capability |
| 动态路由 | ClearAPI 渠道、分组、模型映射及上游模型自动同步任务 | 空 model-routes 拒绝池请求；正常自动同步会丢弃池绑定和开关 | 缺省沿用正常渠道路由，显式 api/paused 覆盖仍有效；DTO 保留两项池设置 |
| Scheduler | 健康/冷却、亲和性、注册模型过滤、并发槽位 | 未发现需要替换调度核心的问题 | 复用现有实现；不同账号能力集合和未知模型拒绝通过测试 |
| 故障回退 | 零派发证明、receipt 链、已开始/未知执行禁止重放 | Gateway 普通 auto-ban 可把池账号错误升级成共享渠道禁用 | 池渠道健康由 Executor 管理；普通 API 渠道禁用规则保留 |
| 并发与 Lease | 原子占用、派发前复核、取消和释放 | 自动 grant 续期必须保留仍有效的在途租约 | 同身份/同代际/同权限续期重绑定；失效模型取消；并发/CAS/释放测试通过 |
| Usage / Billing | 原用量、预扣、结算及 outbox | Gateway 与 Executor receipt 请求 ID 不一致，难以直接关联 | 复用 Gateway 服务端请求 ID，保持原 HMAC 和请求体冻结；真实账务核对通过 |
| Grant 续期 | 30 分钟期限、有效授权、checkpoint | 缺少账号能力同步及正常运行续期 | 明确 opt-in 的 automatic_models 账号每 5 分钟同步；过期/禁用/撤销不自动恢复 |

自动模式仍须一次账号身份授权和受保护的账号级策略，不从未可信凭据自动提升身份。同步接口不接受客户端模型列表，模型只来自该账号认证后的上游清单；变更受当前 revision、身份、generation、有效期限及 checkpoint 约束。显式模型撤销保留，不被刷新覆盖。账号替换或撤销仍会失效；发现失败不续期，也不会静默假造支持。

新模型不要求改代码或手工逐模型 capability/grant。Gateway 复用已有 `upstream_model_update_check_enabled`、`upstream_model_update_auto_sync_enabled` 和原 `model_update` 系统任务，自动把 Executor 已准入清单纳入原渠道能力。生产该任务默认 30 分钟；本地只为验收临时设为 1 分钟。没有新增调度/模型路由系统。正常模型价格、用户/Token 权限及分组规则继续适用；新模型没有配置价格时不把它当作免费模型。

**真实账号及实际账务**

同一批已刷新的测试凭据，通过相同 Executor 传输在本机 Docker 成功发现 `gpt-reserve`、`gpt-5.6-terra`、`gpt-5.6-luna`、`codex-auto-review`；清单没有旧验收指定的 `gpt-6-luna`。这是上游清单证据，不是四个模型均已执行的声明。

隔离 Gateway 和 Executor 只监听 127.0.0.1，使用新建本地数据库/卷及凭据副本，没有挂载生产数据库或生产凭据目录。保持正常计费，完成 **4 次真实模型派发、4 次 response.completed、4 条唯一消费日志**：

| 请求 | 实际账号副本 | 模型 | 输入/输出/缓存读取 | 计价及扣费 |
|---|---|---|---|---|
| 1 | A | gpt-5.6-luna | 17 / 8 / 0 | 隔离初始倍率模式，33 units |
| 2 | B（A 临时暂停） | gpt-5.6-terra | 17 / 8 / 0 | 复制线上原表达式，65 units |
| 3 | A | gpt-5.6-luna | 17 / 8 / 0 | 线上原表达式 + durable wallet/outbox，7 units |
| 4 | A | gpt-5.6-luna | 17 / 8 / 0 | 自动同步修复后，原表达式 + durable wallet/outbox，7 units |

四次缓存创建、reasoning 用量也均为上游明确的 0。用户、Token、渠道用量和消费日志合计 **112 units（站内 0.000224 USD 等值，不是供应商账单）**，用户余额 100000000→99999888，Token 余额 10000000→9999888。两条 durable ledger 记录均 settled，reserved=target=applied=7；两条 outbox 均 delivered，各只有一条交付 receipt。request ID、账号摘要、attempt、receipt、response ID、usage、ledger 和消费日志已直接关联。自动调度没有额外消费日志，结束在途租约为 0。

第 4 次请求发生在修复后的自动任务已从两模型扩充至四模型，并仍保留 `upstream_pool`/`pool_routing_enabled` 之后，证明同步不会再次关闭池路由。两个隔离组件最终正常停止，local master 已关闭，私有状态保留以便核查。

另有 2 次本地 Gateway 准备请求返回 503：隔离脚本误用了 `other_settings`，正确 API 字段为 `settings`。这两次没有到达 Executor，没有上游派发或扣费；修正本地配置后才继续。登录脚本也已按正常登录响应使用 access JWT，没有绕过登录校验。模型 metadata GET 不计为模型派发。

**定向验证**

实际命令、范围、日志与 SHA 见 [verification.json](verification.json)。Go 检查使用固定镜像、断网缓存和 `GOWORK=off`。观察到：

- Gateway：模型路由/别名/来源覆盖测试；自动模式 Gateway proof 与变更请求体拒绝测试；池账号错误不禁用共享渠道、普通渠道通知及日志测试，均 PASS。
- Executor：自动模型集合、新模型发现、未知模型拒绝、撤销、CAS 冲突、身份代际、过期拒绝、有效租约跨续期、并发上限和释放，`-race` PASS；管理鉴权及白名单 `-race` PASS。
- 原有零派发、已开始/未知状态禁止回退、receipt 证明与重放测试 `-race` PASS。真实验证包含 A 暂停后选择 B；没有人为制造可能已执行的请求再重放。未把合成故障测试称为真实上游故障注入。
- `cd relaykit && GOWORK=off go build ./...` PASS；设置 round-trip 聚焦测试 PASS；Gateway/Executor server 构建 PASS；修改文件已 gofmt。
- 一次新增管理鉴权用例触发原有第 5 次错误密钥封禁，改为独立测试 IP 后 PASS；中间测试脚本作用域错误已修正。没有削弱认证/封禁机制。

未运行完整 CI、E2E、所有模型或三数据库矩阵。没有修改数据库访问代码、GORM 模型、Scanner/Valuer 或项目迁移；仅在新建隔离 SQLite 数据库用原 CLI 建立账务测试表，未修改生产 schema/历史账务，不宣称三库矩阵或生产 PostgreSQL 计费实测通过。未专门制造非零缓存用量、生产故障或长周期 OAuth 到期换证；原受控 OAuth 维护路径保持。

涉及的安全控制按 [OWASP ASVS 5.0.0](https://asvs.owasp.org/)、[Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html) 和 [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) 核对：服务端管理鉴权、有效授权及到期拒绝、身份一致性、单次/绑定证明、撤销、防重放和非秘密审计。未修改用户登录/会话协议，也不声明整站 ASVS 认证。

**源码交付与路线选择**

正式工作区仍为 `D:/AI/API/worktrees/clearapi-controlled-launch-20261020`，开始时 HEAD `f30b247383ee5a18f370d1ad65b30204d32c8833`，其大量既有修改保留。Gateway 改动已带回原工作区；原先 dirty 的 relay-error 代码只做定向插入，未覆盖原工作。

为了与已发布代码精确比较，另外保留：

- Gateway：`D:/AI/API/worktrees/pg03-general-pool-gateway-20261029`，基于线上 `732a2d51`。
- Executor：`D:/AI/API/worktrees/pg03-general-pool-executor-20261029`，基于线上 `201ce387`；旧 Executor 工作区及其 dirty fixture 未修改。

主要代码为 Gateway 的 `model/pool_routing.go`、`service/accountpool/policy_snapshot.go`、`relaykit/dto/channel_settings.go`、`service/channel.go`、`service/relay_error.go`、`service/request_policy.go`，以及 Executor 的 `codex_pool_models.go`、`pool_model_sync.go`、原 grant/capability、服务注册/刷新及管理入口。完整 21 文件 SHA、可审阅补丁与固定字节副本在 [source-manifest.json](source-manifest.json)、[gateway-review.patch](gateway-review.patch)、[executor-review.patch](executor-review.patch) 和 `source/`。未提交、推送或创建 PR；生产镜像未替换。

已通过 agent-reach 的 GitHub/gh 路径实际克隆 Sub2API，固定 [a3298b46ebe239fb0720b068a1eb3383955bdac2](https://github.com/Wei-Shaw/sub2api/tree/a3298b46ebe239fb0720b068a1eb3383955bdac2)，只读比较 `openai_codex_models_service.go`、`account.go`、`openai_account_scheduler.go`、`concurrency_service.go`。它提供账号清单获取和成熟的亲和/并发模块，但调度依赖自身账号仓库、Redis 槽位、分组和服务结构；空 model mapping 也不等于已确认账号支持。移植需要改造账号身份、并发、失败边界和 ClearAPI 账务连接，超过本次沿用现有租约/receipt 的修复范围。实际收益是确认账号级 manifest 路线并避免引入第二套调度核心；未复制或移植 Sub2API 源码。

**剩余真实阻断与验收状态**

VPS 上两个账号的 `/backend-api/codex/models` 在 Python 和实际 Executor TLS/HTTP 传输下均 HTTP 403；Python 响应为 Cloudflare HTML。相同凭据和传输在本机成功，随后真实调用也成功。**已证明的是 VPS 模型发现入口不可用；未派发 VPS 模型请求，不能据此声称 VPS 模型执行已通过或一定失败。** 需要可用的 VPS 上游出口/现有代理配置，再按原备份、发布、身份准入流程发布这两个候选并完成生产验收。没有把静态目录回填为“已确认支持”来绕过该问题，也未擅自新建长期网络代理。

生产没有部署新 Gateway/Executor，没有重启共享服务，没有签发 trust/grant/capability，没有改价格、分组、master、Token/渠道状态或历史账务。诊断阶段只向现有受限目录上传了一次性模型清单读取二进制，不等于发布应用；正常 owner GET 可产生既有认证审计，不宣称整库绝对零写入。

| 状态 | 结果 |
|---|---|
| 代码实现 | 已完成本轮验证范围，独立候选及补丁保留 |
| 定向测试 | 通过 |
| 真实模型调用 | 隔离本地 Gateway→Executor→真实上游，4 次通过 |
| 真实用量与账务 | 隔离环境核对通过，含原 durable wallet/outbox |
| 实际生产部署 | 未执行 |
| PG03 生产启用 | 否，master=false |
| 技术验收 | 隔离 Codex Responses SSE 路径通过；生产验收未通过 |
| 正式运行结论 | 尚不能宣布生产正式可用；其他 Provider/协议未新增或实测 |

最终机器回执：[receipt.json](receipt.json)。账务证据：[verified-closeout.json](verified-closeout.json)。自动同步回归：[existing-autosync-fixed.json](existing-autosync-fixed.json)。生产现场：[production-closeout.stdout.json](production-closeout.stdout.json)。


Git 交付补充：按用户后续“提交Git”指令提交独立候选；上述“未提交”是前次验收报告生成时的历史状态。Executor 提交为 `1264c6c178fc9ef135b36fd9821daf83fba56bdc`；Gateway 提交位于 `codex/pg03-general-pool-gateway-20261029`。未推送、未部署。Git 纳入代码及必要回执/日志/补丁；诊断二进制、原始响应、私有运行状态和临时工具不纳入。Executor 已有 CRLF 文件保留原换行，代码与已测副本在规范化换行后相同。
