# CLEARAPI — Sub2API 最终隔离验收

日期：2026-10-30，Asia/Shanghai。**结论：423 已解除，实际配置和双服务 MOCK E2E 已完成；正常结算及一次内部重试后的唯一最终扣款通过。取消/不确定完成仍需保留金额与人工核对，不具备全部自动精确结算及生产启用条件。** 本轮真实厂商调用0、生产访问0、推送/合并/部署0。

## 1. 真实版本、条款与环境

- 工作区：`D:/AI/API/worktrees/clearapi-sub2api-integration-20261030`；分支 `codex/sub2api-isolated-integration-20261030`；开始干净、HEAD `2324d837ec21a26aa112f0c0090a973c7c9d6d75`。原 `871bdf75b`、`8d2842b2d` 及全部历史保留。
- Sub2API仍为固定 `d851a38477597157e78450d30af2d8504764d085` 的完整前端镜像，镜像ID `sha256:bd3c64a8b3cf74d3ddd19e25d10ba4a7888f3fe69ce2a250500c358ed370220a`。原PostgreSQL17.11、Redis7.4.11、应用卷和原管理容器保留，没有重建数据库、清空或复制生产数据。
- 21:07:54新鲜官方检查：管理员登录200，compliance200、required=false、version=`v2026.06.10`、正式acknowledgement存在，accounts200。负责人已通过正常流程确认；本轮AI没有调用accept、写数据库造确认或绕过门禁。证据 `current-gate.json`；正式记录投影保留于 `operations.json`。
- 为执行用户明确要求的完整链路，在**同一原隔离网络**增加仅供验收的Gateway和mock两个进程/容器。Gateway回环18883，mock控制口回环18884；Sub2API管理口仍18882。它们不是第二套生产平台或新选型，没有增加真实厂商执行层。原三服务未为本轮重建或重启。
- Gateway从当前ClearAPI源码断网构建，复用此前已验证的前端dist，不重跑前端构建/测试。其数据库是独立合成SQLite，不是Sub2API库或生产库；通过仓库既有billing/finance迁移CLI显式创建账务、授权快照、outbox/receipt、review表，保留DURABLE_WALLET、BILLING_OUTBOX和直接数据库结算，未关闭资金保护。

## 2. 13平台的实际配置结果

复用13平台/31档案规划，没有重做选型或逐模型测试。通过官方管理/用户API实际建立：**13个原生测试组、1个专用执行用户、13个专用内部Key**。另建立2条明确标记为合成mock的API Key账号，分别属于OpenAI和Anthropic；它们只指向Docker内mock，不含真实厂商认证。

| 原生平台 | 本轮实际接入准备 | 真实厂商状态 |
|---|---|---|
| OpenAI/Codex | 原生组/内部Key；本地mock API Key账号；C/R原生HTTP与兼容转换路径实测 | CONFIG_READY / REAL_CALL_UNVERIFIED；没有订阅OAuth、Agent Identity或官方付费Key实测 |
| Anthropic/Claude | 原生组/内部Key；本地mock API Key账号；Messages及启停实测 | CONFIG_READY / REAL_CALL_UNVERIFIED；OAuth/setup-token/Bedrock/Vertex档案沿用模板 |
| Gemini | 原生组/内部Key；6种接入档案保留，AI Studio OAuth能力开关不假定开启 | CONFIG_READY / REAL_CALL_UNVERIFIED |
| Antigravity | 原生组/内部Key；OAuth和兼容relay档案分开，混合调度不启用 | CONFIG_READY / REAL_CALL_UNVERIFIED |
| Grok/xAI | 原生组/内部Key；Build OAuth、SSO转换、官方Key的区别保留 | CONFIG_READY / REAL_CALL_UNVERIFIED |
| Kimi/Moonshot | 原生组/内部Key；按量Key与Coding Plan Key模板 | CONFIG_READY / REAL_CALL_UNVERIFIED |
| GLM/Zhipu | 原生组/内部Key；PaaS/Coding及团队字段模板，原生Responses缺省不支持 | CONFIG_READY / REAL_CALL_UNVERIFIED |
| DeepSeek | 原生组/内部Key；官方Key与源码`/responses`路径模板 | CONFIG_READY / REAL_CALL_UNVERIFIED |
| MiniMax | 原生组/内部Key；按量/Coding、大陆/国际端点档案分开 | CONFIG_READY / REAL_CALL_UNVERIFIED |
| OpenCode Go/Zen服务 | 原生组/内部Key；两个服务模式与每模型协议规则模板 | CONFIG_READY / REAL_CALL_UNVERIFIED；不等同于客户端或原厂OAuth |
| Command Code服务 | 原生组/内部Key；API Key及supported_endpoints目录模板 | CONFIG_READY / REAL_CALL_UNVERIFIED |
| Cline服务 | 原生组/内部Key；积分/ClinePass共用Key/端点的模板 | CONFIG_READY / REAL_CALL_UNVERIFIED |
| TypeSafe/Jev | 原生组/内部Key已准备；仅原生非流式System One | SOURCE_SUPPORTED / CLEARAPI_PROTOCOL_GAP / REAL_CALL_UNVERIFIED；未伪装为Chat开放 |

31档案中没有合法凭据的方式均保留模板，没有创建带假OAuth token的“真实账号”，没有购买或收费调用。普通第三方API和PG03现有账号均未读入或重复调度。实际ID、组平台、Key ID及关闭状态见 `operations.json`；secret只在仓库外 `D:/AI/API/runtime/sub2api-integration-20261030/`，不随Git交付。

## 3. 分组、模型和渠道边界

内部基址沿用 `http://sub2api:8080`，不追加`/v1`；ClearAPI只用专用内部Key，mock收到的是单独的模拟上游Key，所有实际mock尝试的认证匹配均为true。没有将终端ClearAPI用户Key转发给Sub2API或mock。

在独立Gateway通过正常API创建type59 OpenAI、Anthropic两个测试渠道，并按用例增加不可连接/重试专用type59渠道；另有type1普通API mock渠道用于故障隔离。公开测试名 `accept-*` 通过原model_mapping映射到 `mock-*`，实际mock日志及Sub2API账号/Key ID证实两条正常路径分别进入正确平台与账号。所有定价为显式合成测试价格，不改真实模型售价。

simple模式的既有限制没有改变：原生组/内部Key决定平台路由和访问身份，**不能把同平台不同组当账号集合、OAuth/API Key/Coding来源的硬隔离**；basic组管理会忽略高级路由/allowlist/exclusive等字段，Composite不支持。本轮未用这些字段冒充约束，终端权限仍由ClearAPI组/Token/渠道可用模型执行。若未来确需同平台硬分源，仍需独立执行边界；没有为此自动新搭多套实例。

OpenAI mock先覆盖了Responses→Chat的现有转换路径，再通过原账号设置的passthrough开关核对原生Responses JSON/SSE，未改适配器代码。passthrough不应被当作额外模型授权机制。TypeSafe专用`/v1/systemone`仍无ClearAPI入口/DTO/资金路径；它不阻塞本轮C/R/M共性验收，故没有追加专用业务功能。

## 4. 实际双服务与账务结果

链路为 **实际运行的ClearAPI Gateway → 实际Sub2API → Docker内可控HTTP mock**，不是把适配器单测改称E2E。主记录有16个业务用例，另有3个明确保留的前期环境/fixture/观察器失败试次。没有13家逐模型收费测试。

| 用例 | 实测结果 | 最终钱包/日志 |
|---|---|---|
| Chat JSON | 200，实际C转发，映射与认证匹配 | 890 units，用户/Token/预留及唯一日志一致 |
| Responses JSON/SSE，经既有Chat转换 | 200，兼容转换回复/终态事件完成 | 每业务请求890，唯一日志 |
| Responses原生JSON/SSE | 200，mock实际路径`/openai/v1/responses`，SSE completed | 每业务请求890，唯一日志 |
| Anthropic Messages JSON | 200，`/anthropic/v1/messages?beta=true`，原生消息与cache Usage | 890，唯一日志 |
| Chat SSE | 200，完成及Usage转发 | 890，唯一日志 |
| 可控503→成功的内部重试 | **同一Gateway请求，Sub2API同账号2次mock尝试，第一次明确拒绝执行，第二次200** | 最终只扣890，1个reservation/1条最终消费日志 |
| Sub2API连接建立前拒绝 | 500，mock尝试0 | 预留退款，净扣款0，reservation=refunded，无消费日志 |
| 停用模拟Messages账号 | 原API确认schedulable=false；请求503、mock尝试0；随后按API恢复测试 | Gateway保留6 units并标review，见下述边界 |
| 内部Key停用 | Sub2API401，mock新增尝试0，随后恢复供后续验收 | 不经过终端Gateway扣费 |
| 池渠道关闭后普通API | type59池渠道均禁用，type1普通mock路径200 | 890，唯一日志，mock路径为`/ordinary/…` |

正常C/R Usage为input总量1000、output20、cache-read200。Sub2API内部记录input800、cache-read200、output20，缓存从input拆开，并非丢200 tokens；Messages原生input800、cache-read200、output20。既有表达式归一化后都按 `tier("mock", p*2 + cr*0.5 + c*4)`，QuotaPerUnit=500000，实际(800×2+200×0.5+20×4)×0.5=890。Sub2API专用用户余额始终0，simple模式只记录运营Usage，没有第二份用户余额扣除。合成模型没有内部售价时记录成本0，不将其冒充真实厂商成本对账。

最终全数据集包括前期试次：19个reservation、18条唯一消费日志（连接前失败退款无消费日志），**钱包减少=Token UsedQuota=reservation applied合计=消费日志quota合计=8068 units**；outbox全部delivered，无同request_id重复消费日志。正常请求逐笔精确对账通过；重复尝试测试的两次上游尝试没有变成两次终端扣款。原单元层重复Settle/Refund/recovery证据继续复用，不重复运行。

`user.used_quota`为8038，与上述8068相差30；这30来自带review标记的错误路径保留额，不能称为已确认Usage。账本/钱包/Token/消费日志金额一致，不代表所有用量计数都应当被混为“已完成消费”。历史记录保留，未为整理报表改写它们。

## 5. 取消、不确定完成及明确未通过部分

| 主记录场景 | Gateway结果/保留额 | 可证明的状态 |
|---|---|---|
| 上游400拒绝 | HTTP400，保留12 | `upstream_usage_unknown` review marker；并非已精确退款 |
| SSE缺terminal | HTTP200后异常结束，保留8 | `interrupted_unknown_usage`；没有将EOF称为完成 |
| 客户端取消 | 收到delta后真实关闭客户端socket，保留8；取消前观测到4 units预留 | Sub2API继续drain，实际记录800input+200cache/20output；Gateway标interrupted/review，**没有自动补成890** |
| 非JSON上游回包 | HTTP500，保留4 | unknown review marker，不假装正常Usage |
| 响应前连接丢失 | HTTP502，保留4 | unknown review marker，mock实际尝试1次 |
| 已停用账号、无可用执行账号 | HTTP503、mock尝试0，Gateway保留6 | unknown review marker；Gateway尚无可信跨服务零执行证明 |

上述每个主记录场景都只有至多一条最终消费日志，没有因不确定性重放并形成多笔终端扣款。但**它们未达到自动精确结算/退款标准**：尤其取消后Sub2API得到完整Usage，Gateway没有可信终态回传/对账衔接；即使mock能证明一次400未执行或停用后0派发，Gateway也不能从一般代理HTTP错误安全推断所有真实链路的零成本。本轮没有按错误字符串一概退款，或新增静默估算来掩盖差额。

全数据集仍有7个review marker和1条前期SQLite I/O失败造成的`needs_review`记录（原始预留/applied12、target890），均在**合成隔离账本**保留。部分marker对应前期观察器失败试次；这不是生产待处理金额。没有删除失败记录、改DB伪造通过或人工追加扣费“凑平”。

因此状态必须分开：`MOCK_HTTP_PASS`、`NORMAL_SETTLEMENT_PASS`、`RETRY_SINGLE_FINAL_CHARGE_PASS`；`CANCEL/UNKNOWN_REVIEW_BOUNDARY_OBSERVED`；`FULL_AUTOMATIC_SETTLEMENT_NOT_ACCEPTED`；`REAL_PROVIDER_E2E_UNVERIFIED`。不保证物理上游恰好执行一次，不保证真实客户端重发产生的新业务请求会自动幂等。

## 6. 运营验收、修复和Git

原生后台API实际确认了组/账号创建、专用Key绑定、账号schedulable停用与恢复、Key停用401、内部Usage、账号健康投影、错误查询接口200及3条原生错误记录。管理员页面继续由原完整前端服务；本轮没有另外做全页面浏览器回归或复制运营后台。

修复仅涉及隔离验收配置与fixture：

- 初始Gateway的Windows绑定目录SQLite出现真实I/O错误。完整保留原库及失败记录，停掉该测试Gateway，checkpoint后完整转存至专用Linux named volume `clearapi-sub2api-isolated-20261030_acceptance-gateway-data`，源库integrity_check=ok，原Sub2API PG库未动。随后正常结算逐笔通过；账务观察器也在同Linux文件系统读取，使用有界busy_timeout。
- mock按URL path识别协议，修正携带`?beta=true`时错误返回Chat JSON的fixture问题；没有将错误fixture强塞进生产适配器。
- 对账观察器按request_id关联最终日志，等待现有outbox异步投递，不把响应刚返回时尚未投递的日志误判丢失；PG聚合使用完整JSON对象，未改生产数据格式。

本轮没有修改ClearAPI/Sub2API生产Go、鉴权、Billing、数据库模型或迁移代码。代码提交 **`216f2e67eb08aaab74d2e71be76f233bd8939be9`**，仅新增 `compose.sub2api-acceptance.yml` 与 `scripts/sub2api-acceptance-mock.py`；报告/证据另行本地提交。没有推送、合并或部署。

验证是新边界的实际双服务HTTP/SSE和持久账务断言、mock认证/尝试记录、正常管理API、代码构建、Python AST与diff检查。未重跑原已通过六组单测/全量CI/前端；没有生产数据库行为代码变更，未运行新的三数据库矩阵。新Gateway SQLite初始化/现有迁移CLI运行不冒充三库兼容性验收。

## 7. 收尾、复现范围及统一发布判断

收尾已通过官方API：13个内部Key全部inactive；2个模拟账号均unschedulable；5个Gateway测试渠道全部disabled。新增Gateway/mock容器已stop，保留SQLite卷、原绑定目录备份和所有证据；原Sub2API/PG/Redis仍healthy，管理后台继续可用，条款确认不撤回。没有停止生产或其他已有容器。

可复现资产：固定镜像/源码、原Compose、新验收overlay、有限mock fixture和 `evidence/sub2api-final-acceptance-20261030/`。执行驱动及私有配置位于仓库外runtime目录的 `prepare-actual.py`、`run-e2e.py`、`ops-and-finalize.py`、`run-retry.py`、`close-acceptance.py`；这些包含本轮分阶段观察/续跑，不是“一条命令重复全部用例”的新测试框架。runtime/test.env、acceptance.env及acceptance-state.json含本地测试secret，不提交。恢复验收仅启动新增两个服务并按原API重新启用所需**测试**Key/账号/渠道；不得把overlay合入生产。

唯一权威 `D:/AI/API/worktrees/clearapi-controlled-launch-20261020/CLEARAPI_MASTER_STATUS.md` 追加本轮事实，保留原历史；不更新其他worktree的同名副本。

**下一次统一发布：** 可以将现有type59源码及本轮mock验收结果作为默认关闭的候选依据；不能据此正式启用全厂商池。取消/错误后的可信Usage对账与保留额处理仍是实际集成边界，未有真实厂商或商业授权证明，TypeSafe缺口保持关闭。本轮没有生产候选镜像/部署验收，也不把本测试Gateway二进制当正式发布包。

负责人不再需要重复确认条款，也不必购买全部厂商账号。若未来准备正式试用，只需提供**确实拟用**厂商的合法、与PG03不重叠的测试凭据及必要调用授权/费用范围；没有这些条件的平台继续CONFIG_READY/REAL_CALL_UNVERIFIED。取消/未知账务的技术闭环仍由后续集成处理，不要求负责人替AI修复或手工核对本轮合成余额。没有新增一套人工发布审批或要求现在部署。
