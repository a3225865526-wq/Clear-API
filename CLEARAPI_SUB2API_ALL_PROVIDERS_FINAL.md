# CLEARAPI — Sub2API 全平台批量准备及隔离收尾

日期：2026-10-30，Asia/Shanghai。固定源码：`d851a38477597157e78450d30af2d8504764d085`。

**结论：13 个原生登记平台、31 种接入配置档案已统一整理；12 份 type=59 渠道规划保持禁用，TypeSafe 专用协议单独记录缺口。没有厂商完成本轮实际调用，完整双服务结算仍 BLOCKED / UNVERIFIED，所有新增账号池渠道必须保持关闭。** 423 尚未解除；本轮没有代替负责人确认、伪造记录、创建账号/内部 Key、推理调用、推送、合并或生产部署。

## 1. 复用范围、当前门禁及实际变更

复用干净隔离工作区 `D:/AI/API/worktrees/clearapi-sub2api-integration-20261030`，分支 `codex/sub2api-isolated-integration-20261030`，起始 HEAD `8d2842b2d2878210e1179f42e3e74ef47371abdc`。原实现提交 `871bdf75ba22d6482c7d7633be4a8360ca0a4db6` 和全部原证据保留；逐项按归一 LF 的 SHA256 验证原 evidence manifest，全部匹配。Sub2API 固定源码工作树仍干净，没有更新版本或重新选型。

沿用已经运行的同一隔离 Compose 项目/数据库/Redis/镜像和持久卷；没有重建、重启、拉新镜像或新搭服务。应用聊天清单仅当前任务 active，前选型 idle，相关 PG03 notLoaded。本轮只新增报告、静态规划和证据；应用 Go/前端/Billing/鉴权/数据库/Compose/测试代码零修改，不制造代码修复或空提交。

**本轮新鲜检查，2026-10-30 20:43:55（Asia/Shanghai）：** 本地管理员正常登录200；`GET /api/v1/admin/compliance` 为200，required=true、version=`v2026.06.10`、无 acknowledgement；`GET /api/v1/admin/accounts` 仍423 `ADMIN_COMPLIANCE_ACK_REQUIRED`。登录 token 只在检查进程内存中，证据不含可用凭据。没有调用 accept。隔离库只读计数仍 accounts=0、api_keys=0、usage_logs=0。

所以本轮**实际后台批量创建为0**。`CONFIG_READY` 在此严格表示离线配置规划完整，不代表账号、内部 Key、模型或线上渠道已创建。`REAL_CALL_UNVERIFIED` 表示没有该平台实际调用证据；原 PG03 Codex 调用不能移作 Sub2API 实证。

## 2. 全部原生登记平台与接入方式

清单从后端 `domain/platforms.go` / `constants.go` 与前端生成的 `platformCatalog.builtin.json` 交叉核对：**13 个具体平台，Composite 是分组路由概念，不是第14个厂商。** 账号类型是 oauth、setup-token、apikey、bedrock、service_account 等；同名字段不代表授权来源和权益相同。

协议缩写：C=Chat Completions；R=Responses；M=Anthropic Messages；G=Gemini v1beta；S=System One。下表“兼容入口”包含服务内转换，**不表示厂商原生支持所有这些协议或特性**。源码提供的默认模型/价格目录也不证明某账号真正有权限。

| 平台 ID / 名称 | 实际接入档案及所需授权/凭据 | 协议与模型来源 | 准备与真实调用状态 |
|---|---|---|---|
| `openai` OpenAI / Codex | ChatGPT/Codex OAuth：本人授权或有效 token 导入，access/refresh/expiry及账号元数据；原生 Agent Identity 导入属于 oauth 子模式，需官方导入数据、runtime_id及PKCS#8 Ed25519私钥；另有 OpenAI Platform API Key。三者分别记录 | OAuth/Codex 以R为核心；API Key按实际C/R能力；C/M等兼容入口可能转换。OAuth使用认证 Codex manifest，API Key使用上游 `/models`，结合账号 mapping | CONFIG_READY；3档案；REAL_CALL_UNVERIFIED |
| `anthropic` Claude | 订阅 OAuth、inference-scope setup-token、Console API Key 分别记录；AWS Bedrock SigV4需AK/SK/可选临时session/region，Bedrock API Key需Key/region；Google Vertex Anthropic需SA JSON/project/location及IAM/模型权限 | 原生M，C/R兼容转换；Bedrock/Vertex有各自传输。OAuth/Key可查上游models；Anthropic Bedrock/Vertex不支持通用模型同步按钮，须用确有权限的publisher/model ID与mapping | CONFIG_READY；6档案；REAL_CALL_UNVERIFIED |
| `gemini` Google Gemini | Code Assist OAuth、Google One OAuth、AI Studio OAuth分别记录；AI Studio OAuth受服务能力开关限制，不能假定可用；另有AI Studio API Key；Vertex SA JSON，或源码以service_account类型保存的Vertex API Key子模式 | 原生G，M/C/R有兼容路径；models API、Vertex publisher清单、mapping/源码候选。带project_id的Code Assist OAuth不支持通用同步按钮，不冒充发现成功 | CONFIG_READY；6档案，其中AI Studio OAuth额外FEATURE_GATED；REAL_CALL_UNVERIFIED |
| `antigravity` Antigravity | 本人Google/Antigravity OAuth；原UI还提供“upstream”基址+Key，**实际保存为apikey**，是已有兼容执行层接入，不是直接订阅OAuth | G/M及C/R兼容路径；OAuth fetchAvailableModels、上游relay模型接口与mapping；默认候选非已验证模型 | CONFIG_READY；2档案；REAL_CALL_UNVERIFIED |
| `grok` Grok / xAI | Grok Build OAuth 与 xAI官方API Key分开。源码支持将Web SSO或密码授权结果转换成Build token；raw SSO/password不是持久账号类型，也不等于通用Web SSO直转发 | 原生/兼容路径由OAuth或Key执行器决定，C/R/M入口可能转换；认证Build/xAI目录及observed catalog/mapping | CONFIG_READY；2档案；REAL_CALL_UNVERIFIED |
| `kimi` Kimi / Moonshot | Moonshot按量Key与Kimi Coding Plan Key，`account_mode=payg/coding`；两者都不是Kimi设备OAuth | 源码profile分别提供C/R/M默认端点，adaptive按入站选择；认证models、mapping、源码候选；实际上游支持未实测 | CONFIG_READY；2档案；REAL_CALL_UNVERIFIED |
| `zhipu` 智谱 GLM | BigModel PaaS按量Key、GLM Coding Plan Key；团队版可能需要organization/project字段 | 原生profile为C/M，**没有原生R默认端点**，R入口如走转换不能称原生Responses；认证models与mapping | CONFIG_READY；2档案；REAL_CALL_UNVERIFIED |
| `deepseek` DeepSeek | 官方按量API Key；固定profile只有payg，不杜撰订阅OAuth/Coding档 | 源码profile为C/M/R；Responses路径覆盖为`/responses`，不能自行拼成另一版本路径；认证models与mapping | CONFIG_READY；1档案；REAL_CALL_UNVERIFIED |
| `minimax` MiniMax | 按量Key、Coding Plan Key分别记录；大陆/国际端点按账号授权选择，不把Key跨区域复用作为事实 | 源码profile为C/M/R，按入站协议分流；认证models、mapping和目录候选 | CONFIG_READY；2档案；REAL_CALL_UNVERIFIED |
| `opencode_go` OpenCode Go / Zen 服务 | Go/Zen服务API Key，account_mode分别为go/zen；**这是聚合服务账号，不是用户的OpenCode客户端配置，也不是各原厂OAuth** | C/M/R的每模型协议由模式规则选择，如gpt/grok→R、部分模型→M；未知模型不能仅按入口猜协议。models/规则/mapping为来源 | CONFIG_READY；2档案；REAL_CALL_UNVERIFIED |
| `command_code` Command Code 服务 | 该聚合服务API Key；profile按模型路由，需其服务授权 | C/M/R，upstream `/models` 的supported_endpoints可参与选协议，账号规则优先；目录不保证账户权益 | CONFIG_READY；1档案；REAL_CALL_UNVERIFIED |
| `cline` Cline 服务 | 官方API Key；积分与`cline-pass/*`订阅模型共用同Key/端点，源码只有一个payg接入模式；不额外造OAuth/Coding模式 | 原生profile只有C，M/R若有入口要转换，非原生支持；认证models、显式mapping；不能把客户端名称当原厂身份 | CONFIG_READY；1档案；REAL_CALL_UNVERIFIED |
| `typesafe` TypeSafe / Jev | TypeSafe API Key；后端明确拒绝非apikey账号 | **仅S：`POST /v1/systemone`，非流式，`jev-latest`**；源码在C/R/M主动拒绝该平台，防止错误协议发送Key。ClearAPI现有router/type59无S入口/DTO/计费集成 | SOURCE_SUPPORTED_GATEWAY_PROTOCOL_GAP；1档案；REAL_CALL_UNVERIFIED；无可启用type59描述 |

AWS Bedrock、Vertex是上述原厂模型的独立授权/传输子类型，已计入档案，不另算具体platform。OpenCode Go/Zen、Command Code、Cline虽然被Sub2API原生集成，仍是聚合服务商。自定义OpenAI/Anthropic兼容Base URL不自动变成一个新的原生厂商，也不证明官方Key/订阅权益。没有为填满表格购买账号、寻找生产secret或发起收费调用。

## 3. 静态批量配置交付与最小隔离方案

`evidence/sub2api-all-providers-20261030/provider-plan.json` 是**不会执行、不能直接导入管理API的规划描述文件**。包含13个平台组描述、31档案的实际credential字段名/授权前提、上游源码默认端点与模式、模型来源、内部Key引用和12个禁用type59渠道描述。没有secret值、实际group ID或已确认模型。类型/模式细节集中在此，不需要拆成逐厂商任务。

统一规则：

1. 每平台一个原生组 `clearapi-internal-<platform>`，platform使用原生ID，禁止Composite；每平台专用内部Key引用 `SUB2API_INTERNAL_<PLATFORM>_KEY`。计划单独使用执行用户，不迁移ClearAPI客户/余额。组ID/Key等由负责人解除门禁后，通过原后台正常创建并回填。
2. 对12个常规协议平台，渠道type=59、基址`http://sub2api:8080`、status=2/disabled，初始models空、mapping空，只规划到ClearAPI隔离测试组。TypeSafe不创建伪装Chat渠道，不将jev-latest提供给LLM客户端；保留专用协议适配缺口，不能直接暴露Sub2API绕过ClearAPI。
3. 合法账号接入后读取该账号真实模型/端点，核对ClearAPI售价及所需参数，再填写models/mapping。可给同名不同来源的模型用显式别名，由现有mapping还原上游ID；不把静态价格目录或默认模型直接全部发布。用户最终分组、Token权限、模型定价和计费继续由ClearAPI执行。
4. `RUN_MODE=simple`、`SIMPLE_MODE_KEY_RATE_LIMIT_ENABLED=false`不变。Sub2API只记录内部Usage，用户Key不转发给Sub2API，渠道只用内部Key；会话头沿用已验证显式白名单，不通配透传Authorization/Cookie。保持PG03账号不重复入池，不添加PG03绑定。
5. Antigravity `mixed_scheduling`保持false，不使用跨平台fallback或Composite“全厂商Key”。不得将同一账号在PG03和Sub2API同时调度。已有第三方普通API渠道、组和优先级完全不改。

**simple模式隔离的最终结论：**

| 边界 | 固定源码实际行为 | 本轮最小配置 |
|---|---|---|
| 内部API Key身份 | simple分支前仍验证Key/关联用户/组状态和组授权；有效Key不等于允许任意厂商 | 每平台独立内部Key并绑定正确原生组，服务只在内部网络/回环使用 |
| 平台协议选择 | 组platform用于handler/平台选择；不同Key可区分组路由 | 13原生组，禁止一个Composite替代所有平台；关闭混合调度 |
| 同平台账号集合 | scheduler simple将groupID归零，按platform取账号；不同组/Key不形成账号集合硬隔离 | 一个实例只服务ClearAPI同一信任域，同平台账号可共同调度 |
| 同平台OAuth/官方Key/Coding来源 | 组/Key/渠道分开也不能保证特定account_type/account_mode只用某一组账号 | 文档和档案明确分开；若业务必须指定来源、订阅或套餐，须独立执行实例/凭据/状态等真实边界，当前未创建新环境，不声称靠组已解决 |
| 高级组配置 | normalizer在simple创建时只保留名称/描述/i18n/platform，重置费率等；更新仅名称/描述/i18n。模型allowlist、require_oauth_only、model_routing、fallback、exclusive等不能靠此配置生效 | 不发送这些字段当安全机制；模型权限和价格由ClearAPI已有组/Token/channel abilities执行，账号支持由真实mapping校验 |
| Composite | 创建/转换/直接访问/绑定均受simple限制；高级路由操作返回unsupported | 不规划Composite、不改DB绕过、不改鉴权核心 |
| 模型列表与亲和 | 可有默认候选/账号mapping；展示列表和最终调度的来源不必相同；共享内部Key不代表知道ClearAPI每个客户身份 | 真实模型确认后才入渠道；亲和/多轮/终端用户隔离仍需跨服务验证 |

独立Key和渠道仍有路由、审计、吊销价值，但不能被报告为同平台账号硬隔离。若要求所有31档案同时上线并能严格指定来源，现有单实例simple环境不具备这种证明；本轮不会偷偷切standard模式恢复第二份客户钱包，也不会新搭31套环境。

## 4. 共性跨服务验收和计费最终结果

**本轮没有ClearAPI→实际Sub2API→上游成功调用，全部厂商真实调用数量为0。** 423阻止通过受支持管理流程准备实际账号/内部Key，依赖它的管理、完整转发和账务项目均暂停；不是“所有支持平台已接通”。

复用上一轮全部有效结果，不重复运行正式镜像构建、restart/recreate、六组type59协议测试、钱包对账和既有回归。原最完整链路为正式type59适配器→HTTP mock→真实临时SQLite钱包/消费日志：1派发、1000input/20output/200cache-read、最终9units，用户/Token余额9991，渠道/预留/唯一消费日志9；同BillingSession重复Settle/Refund/recovery不重复扣款。它不是双服务账务结果，更不是所有厂商恰好一次证明。

| 必要项目 | 最终状态 |
|---|---|
| C/R/M请求、模型映射、内部Key替换、代表性SSE完成 | 原6组HTTP mock契约PASS并复用；双服务BLOCKED / UNVERIFIED |
| SSE异常终止、缺Usage、取消、不确定完成 | 原聚焦测试和Sub2API drain契约复用；两服务终态Usage与账务未闭环 |
| input/output/cache及预扣/最终结算/退款/唯一日志 | Gateway mock精确对账PASS并复用；各厂商及实际Sub2API侧对账UNVERIFIED |
| simple内部不重复扣用户余额 | 新增运行既有 `TestSimpleModeRecordUsageWindowOptIn`：Anthropic/OpenAI服务 × 是否开启Key窗口扣额 × stream/非stream，共8场景PASS；关闭窗口时记录Usage且billing调用0，余额/订阅扣额0；开启窗口也不扣余额。**这是服务stub契约，不是两服务运行证明** |
| Composite和高级组字段限制 | 4项此前未运行的现有聚焦测试PASS，确认拒绝Composite、创建/更新时丢弃unsupported字段；不是实际后台CRUD通过 |
| 两层重试/厂商重复派发 | 源码及原dispatch fence测试复用；实际attempt数量、未知状态和两层预算UNVERIFIED。钱包最终幂等不保证上游只执行一次 |
| 内部分组/模型授权 | 源码边界已核实、静态规划完成；创建及跨服务拒绝用例BLOCKED |
| 关闭池渠道后普通API正常 | 禁用渠道被channel cache跳过，abilities以enabled过滤，源码保持独立；本轮未改实际普通渠道、未发其收费请求。**实际运行验收NOT RUN**，不以静态检查冒充通过 |
| TypeSafe专用S协议 | Sub2API原生实现存在；ClearAPI缺入口/DTO/Usage计费衔接，保持关闭，不发送错误协议测试“碰运气” |

取消仍有实质差异：Gateway断开后Sub2API可能继续drain获取终态Usage，Gateway未必收到它。尚无双服务证据证明取消后能取得同一input/output/cache事实并与唯一终端扣费一致。原保留预留/review marker策略保留，不通过删除资金保护或推测Usage来放行。

解除门禁后，剩余验收应**集中为一次共性测试批次**：使用原环境和受控上游，选择适用C/R/M代表路径，记录实际Sub2API账号/内部Key/组、Gateway request_id、上游attempt和终态Usage；完成一次正常SSE、一次异常终止/未知、失败退款、取消后drain、禁止不安全重放、model_mapping/组越权拒绝及禁用池后普通mock渠道可用，对账两服务Usage与Gateway预扣/结算/日志。其他平台只补其特殊认证/协议差异，不逐模型重复账务和压力测试。不得把此待办写成已执行。

## 5. 本轮实际检查、代码与证据

新运行命令仅下列5个此前未运行的现有测试，使用已存在的Go缓存和固定源码只读挂载，`--network none`；无生产DSN：

```text
go test -tags unit ./internal/service -run 'Test(SimpleModeRecordUsageWindowOptIn|AdminServiceSimpleModeRejectsDirectCompositeGroupAccess|AdminServiceSimpleModeRejectsCompositeCreateAndConversionDirectly|AdminServiceSimpleModeNormalizesAllUnsupportedCreateFieldsDirectly|AdminServiceSimpleModeNormalizesAllUnsupportedUpdateFieldsDirectly)$' -count=1 -v -timeout=90s
```

结果：5个顶层测试及RecordUsage的8个子场景PASS，package耗时0.028s。没有新增测试代码，没有将stub当作真实服务。未重跑上一轮已通过项目，未运行全量CI、三库矩阵、逐模型调用或压测；没有生产数据库行为变更，三库矩阵不适用本轮文档/静态描述。

其他实际检查：官方GET门禁状态、同隔离库只读计数；旧证据hash匹配；13个平台与源码登记顺序/集合完全一致；31档案覆盖；12渠道全部disabled、无Composite、无模型与Key值；固定源工作树干净；原主工作区历史保留。验证命令、时间、源码SHA和结果在新 `verification.json`；`current-gate.json`、`new-simple-contracts.log`和`provider-plan.json`共同交付。

实际应用代码提交仍为继承的 `871bdf75b`；此前交付 `8d2842b2d`保留。本轮没有应用代码变更或修复提交；新增文档/规划若提交，是有实际内容的本地交付提交，不称为新适配器或Billing修复。没有新环境、真实OAuth授权、生产凭据访问或条款接受。

## 6. 负责人最少事项、发布判断与权威总控

负责人只需处理以下真实外部条件：

1. 在本机现有后台 `http://127.0.0.1:18882/login`，本人阅读原强制承诺并自行决定是否有权、愿意按官方流程确认；测试管理员/密码位置沿用前报告，不在报告复制secret。仍未确认就继续保持423；不要改DB、绕旧版或让AI点击accept。
2. 如决定继续，通过原后台提供**实际准备使用且有授权**的账号/Key/OAuth，避免与PG03重叠，按本清单创建相应原生组和专用内部Key。不必一次购买13个平台账号，不必逐模型收费调用；没有凭据的平台可长期保留禁用规划。
3. 如存在同平台套餐/账号来源必须硬隔离的要求，明确需要分开的集合；现有simple组不能承担该约束，届时只为确需集合准备独立执行边界。无此要求时，不新增审批或基础设施。

后续双服务mock和账务验证是集成工作，不要求负责人手工承担；TypeSafe仅在确实需要其S协议产品入口时再做最小正式适配与计费验收，不能为“全平台”名义将它冒充Chat开放。

下一次统一发布：**可纳入静态规划、既有已验证type59代码和独立客户端配置指南；全部新增Sub2API池渠道继续关闭。** 12个平台只有源码与配置准备，未达到完整隔离转发/结算标准；TypeSafe另有明确Gateway协议缺口；无凭据/无权益的平台均不得对客户宣传可用。原普通API渠道和PG03历史不被这一状态阻塞，不以本轮任务触发任何部署/开关变更。

本轮总控按用户指定，直接追加至唯一权威文件 `D:/AI/API/worktrees/clearapi-controlled-launch-20261020/CLEARAPI_MASTER_STATUS.md`，先核对其最新记录，补齐前轮Sub2API事实并合并本轮状态，保留原字节前缀与历史。**本轮不更新其他worktree内同名副本。** 主工作区其他dirty成果不动，不在原工作区提交混杂历史修改；本地交付/总控追加凭证单独记录。
