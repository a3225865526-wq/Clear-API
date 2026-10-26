# ClearAPI 公告刷新与渠道状态修复候选

日期：2026-10-25，Asia/Shanghai。本轮交付是本地候选和验证，不是生产发布。

## 结论

| 项目 | 本轮结果 | 生产状态 |
|---|---|---|
| 公告保存/发布后的通知刷新 | **隔离PASS**：9项回归，真实管理员发布→同页即时展示→普通用户读取；新旧公告同时保留 | 尚未部署 |
| 普通渠道新增、禁用、启用及路由缓存 | **隔离PASS**：修复新增漏刷新；三数据库和真实HTTP模拟上游链路均通过 | 尚未部署 |
| 动态监测运行适配/触发接口 | **PARTIAL / 真实执行BLOCKED**：已接入受保护执行器，保留费用拒绝，补强白名单/发布准入 | 不能宣称可用；未启用 |
| A 价格与计费证据 | 新增普通relay→实际隔离额度/日志的闭环PASS，价格修改即时生效、历史账单保留 | 生产商业价格正确性/真实交易仍未验证 |
| C 五家上游 | 保留前轮逐家模拟证据，适配代码未删除或覆盖 | 真实上游与真实计费仍未验证 |
| E 部署链 | 当前代码未改，复用已核实的17+7+2项及正式备份身份，未重跑30项E2E | 不部署，不安装资产 |

**不能把“已接线”写成“动态监测已经解决”。** 上游硬费用上限证明仍缺失，生产策略/启用开关及前端测试按钮保持关闭；不删除或绕过成本门禁、不用零散模拟回包冒充供应商合同。

## 1. 原始成果保护与候选身份

- 原公告/审计工作区：`D:/Codex/worktrees/clearapi-abce-release-20261024/clearapi-controlled-launch-20261020`；分支 `codex/clearapi-abce-release-20261024`；HEAD `a9fe76f301c023fb8ae716901bbe4f94a4184b3e`。全部29个已修改/未跟踪文件及binary patch保存，原工作区HEAD、文件字节和未提交状态保留。
- 原渠道适配工作区：`D:/AI/runtime/clearapi-monitor-runtime-activation`；分支 `codex/monitor-runtime-activation`；HEAD `3124d8a1b6bbc44e8a6203266567f72c1734c877`；4个修改和1个未跟踪文件完整保留。
- 双重保护目录：`D:/Codex/backups/clearapi-fix-closeout-20261025/`，manifest逐文件SHA256；独立保护分支：`codex/abce-audit-preserved-20261025` → `8230b6013c36b5ead13217c46bffcc18e6be039d`；`codex/monitor-activation-preserved-20261025` → `c3dd1a2cfecbf474ce673f792e2ae3dcb032e19e`。临时Git index/commit-tree建立快照，没有stash、clean、reset或修改原工作区index。
- 新工作区：`D:/Codex/worktrees/clearapi-announcement-monitor-fix-20261025/clearapi-controlled-launch-20261020`，分支 `codex/clearapi-announcement-monitor-fix-20261025`。
- 功能提交：`0c5a06fd93ade4e710ca215a64e5ec025b4b4854`；公告缓存修复已包含于其祖先8230b601。后续本分支仅提交本报告、状态与证据。
- **最终候选是本分支提交完成后的干净HEAD**。完整SHA、同一HEAD的正式DryRun、最终本地镜像revision/digest及二进制对应关系，保存于提交后生成的外部回执 `D:/Codex/backups/clearapi-fix-closeout-20261025/final-candidate-receipt.json`，并复制到本工作区忽略目录 `.local-tests/closeout-20261025/final-candidate-receipt.json`。这样最终提交不需为记录自身SHA而再次改变身份。不能把功能提交或早期隔离镜像的标签误称为最终构建身份。

相关工作区完整HEAD/状态：[保护清单](evidence/announcement-monitor-fix-20261025/protected-workspaces.json)。功能提交后再次逐文件验SHA，两个原工作区均保持原样；历史A/B/C/E发布记录和恢复证据保留，前轮忽略的日志也复制留存。

## 2. 公告权限、刷新和历史保留

用户提供生产用户名 `admin`。本轮生产**只读事务**实际查到 id1、role100、status1：账号启用且为超级管理员；不应再把入口问题归因于该账号角色不足。未取得其浏览器会话，不冒称检查了该会话的localStorage或已登录页面。

现有入口：`系统设置 → 控制台内容 → 公告`（`/system-settings/content/announcements`）。另一处 `站点与品牌 → 系统公告` 仍是单条Notice编辑器。本轮不迁移/替换两种数据，也不删旧公告。

最小修复保留在 `web/src/features/system-settings/hooks/use-update-option.ts`：公告数组/开关保存后使status失效，Notice保存使notice查询失效，共6行。没有修改后端公告存储、认证或角色规则。

本次候选镜像实际浏览器验证：

1. 隔离PG专用库预置2020年历史公告（无status的旧格式），管理员打开通知时间线，预热旧缓存。
2. 管理页面新增草稿，点保存设置，行菜单发布，再保存。
3. **全程不刷新页面**，通知立即出现新公告，旧公告同时存在。
4. 普通 `viewer` 用户登录，从通知时间线读到新旧两条；普通用户真实options请求403。
5. 管理员只删除本轮新增测试公告并保存；SQL回读和管理页仍保留2020年旧公告，id/date/content不丢失。

三项缓存回归+六项生命周期回归当前9 PASS，typecheck/lint/format通过。生产未发布测试公告，未删除、改写生产旧记录。通知最近20条的原显示上限没有变成历史删除。

截图：[同页刷新与旧记录](evidence/announcement-monitor-fix-20261025/B-published-without-reload-old-retained.png)、[普通用户实际读取](evidence/announcement-monitor-fix-20261025/B-viewer-new-and-old.png)；[清理仅新测试公告](evidence/announcement-monitor-fix-20261025/B-cleanup-preserves-history.json)。

实际构建主包 `index.848fcb2e07.js`，SHA256 `58950d0fff7fab7b2ba3498de3e175e096e7acb16b63d0ca64d8276977f0ff40`。已检查最终压缩代码中的两个status key和notice invalidate调用，不仅核对源码；证据：[前端/二进制及保留记录](evidence/announcement-monitor-fix-20261025/isolated-artifacts-and-retention.json)。最终正式标签镜像须与该隔离验证二进制一致，见提交后回执。

## 3. 渠道状态：已修复的普通缓存问题

真实隔离 `/v1/chat/completions` 首次暴露503：刚新增渠道时数据库status1、ability=true，但内存缓存为空。`controller.AddChannel` 的事务创建成功后缺少 `model.InitChannelCache()`；Delete/Update/Status已有刷新。补一行调用，位置在事务成功后，沿用现有服务逻辑，不直接修数据库或异步等定时同步。

回归先失败：新增后 `GetRandomSatisfiedChannel` 返回nil；修复后通过真实AddChannel、状态handler验证：新增立即选中→禁用DB/ability/cache均不可选→启用立即恢复。SQLite3.50.4、MySQL8.4.11、PostgreSQL17.6分别通过；既有AddChannel两项权限/default绑定回归也通过。

本轮真实HTTP普通relay验证（上游是内部网络模拟服务）：成功请求→修改价格→成功请求→正常管理API禁用→请求503且上游零新增调用→管理API启用→成功。总计仅3次上游请求，模型映射为 `audit-price-model → upstream-synthetic`。普通管理状态、持久化、缓存和实际relay选择一致。[HTTP与账本证据](evidence/announcement-monitor-fix-20261025/runtime-routing-billing.json)。

该证据只证明隔离候选行为。未切换生产Channel2，不宣称本轮读到了生产进程缓存。

## 4. 动态监测：接入有效部分，保留真实阻塞

选择性集成原未提交文件的有效部分：

- `controller/console_monitor_admin.go`：原始占位501改为id/JSON/confirm校验，创建现有执行器并注册channel适配，调用 `RunWithResult`，返回attempt/probe/status/error并记录管理审计。既有RootAuth、来源保护和X-ClearAPI-Monitor标记不变。
- `controller/console_monitor_probe.go`：保存原channel适配，仅限channel1/gpt-6-luna/Responses，检查白名单/发布/启用/非归档、渠道启用/非pool及模型授权。**Authorize仍拒绝真实发送**，没有伪造费用保证。
- `model/console_monitor_control.go`：预留事务同时要求allowlisted、published、enabled且不archived，堵住旧代码对非成本目标仅检查enabled的漏洞。先失败回归证明未发布/不在白名单仍执行，修复后通过。
- 当前main的 `service/console_probe.go`、`console_probe_cost.go`、`console_probe_transport.go` **字节不变**，保留成本证明、费用拒绝码、并发预留、清理/恢复和错误脱敏。原旧分支这些文件会覆盖新成本保护，因此没有整文件复制或整分支合并。
- 原分支其他改动全部保存在c3dd1a2c，未丢弃；只复用所需准入用例并保留main较新的结果ID回归。未新增scheduler启动goroutine。

隔离控制器证明：确认/header/id边界生效；已允许且启用的精确目标在费用证明缺失时，保存 `status=error / MONITOR_COST_UNVERIFIED`、attempt/probe ID和预留记录，上游请求数为0。`success=true`仅表示触发请求成功处理，**不表示探测健康**；本轮按Data.status/ErrorCode判定。未经确认400，普通/禁用账号拒绝，未支持的协议仍403。真实策略启用/前端Test按钮保持受阻。

真实阻塞包括该供应商固定payload/隐藏token/非token费用及硬上限证明；已有普通渠道relay传输也不能自动视为满足监测的单次发送、无重试与取消确认合同。**未完成这些证明，不能启用真实动态检测。** 普通缓存修复可单独评审；本候选不是动态监测全面上线验收。

## 5. A新增的关键价格/计费闭环

仅在隔离数据库和账户，模拟返回prompt1000、completion500、cached200 tokens：

| 已保存表达式（USD/百万token） | 计算 | 实际消费日志quota |
|---|---|---|
| p×2+c×8+cr×0.2 | (800×2+500×8+200×0.2) / 1e6 × 500000 | 2820 |
| 改为p×4+c×10+cr×1 | (800×4+500×10+200×1) / 1e6 × 500000 | 4200 |
| 禁用后恢复，价格不变 | 同上 | 4200 |

保存使用统一model_pricing GET/PATCH，包含版本校验、读回及公开pricing；没有手动清价格缓存。余额减11220、令牌used11220、消费日志之和11220，第一笔日志在改价后逐字段保持不变。补齐的是**模拟usage下的实际隔离网关结算闭环**，不是供应商真实账单或生产消费证明。

既有A三库与97项前端证据保留，不无故重跑。生产gpt-6-luna仍没有显式覆盖map，是否符合期望商业价格尚需业务确认；本轮不改生产价格、余额和历史账单。

## 6. 验证与发布边界

本轮命令、日志和源文件差异：[验证汇总](evidence/announcement-monitor-fix-20261025/validation-and-source-review.json)，同目录logs。

- 监测准入/成本三数据库真实实例PASS，覆盖新库、代表性旧schema升级、重复迁移；没有新DDL或驱动改动。
- 首次监测定向汇总里，矩阵通过但一个旧测试仍期待占位501而失败；更新为新接口的400/403边界后仅重跑该测试，PASS。没有把原失败日志涂改为整体PASS。
- 新增渠道缓存回归修复前FAIL，修复后三真实数据库PASS；新增触发器/已有公告权限定向测试PASS，AddChannel既有两项PASS。
- 根Go与relaykit独立build、Dockerfile前端/Go/迁移工具构建通过；前端9项和类型/lint/格式通过。修复缓存的一行后，重新构建隔离镜像并完成HTTP/浏览器链路，未拿旧镜像代替。
- 新候选最终提交后，再从**同一干净工作区**执行 `./scripts/deploy-local.ps1 -DryRun`，不使用FixturePath或TestTarget；实际目标、源码SHA、backend范围、备份需求和返回结果以外部最终回执为准。DryRun不构建镜像，因此另以同一HEAD进行本地Buildx构建并核对revision与验证二进制。
- 无Push，因此没有新的远端CI run，不沿用历史CI冒充本候选CI成功。E工具与GitHub生产workflow均未改，30项隔离部署E2E未重跑。

本轮初始生产只读证据：[production-readonly.json](evidence/announcement-monitor-fix-20261025/production-readonly.json)。生产仍是 `dc685cbe338e8f700cae347e2eaac351d984c9c7` / engine `sha256:aec51220165189cae1eec95882de39e1ebb5c2c51a7ba195eb1ad8747dba3020`，Gateway CID `aaf0638277ab12b71f6df795066e315d196e64952c4aa951d09d6998e0b433e0`，启动时间 `2026-10-24T15:18:11.692823281Z`。生产公告原值MD5 `70c1d2a7a57c7a84147bd7e123eca0f6`；六项价格覆盖值均为`{}`的MD5，收尾只读复查记录在最终回执。

重新检查D提交9725fe43、112f1cad、f126bd10均不在候选祖先，专属image-generation目录不存在，应用diff只有公告hook与本报告列出的普通渠道/受保护监测代码及测试。PG03 pool/dedicated/closed-launch三个生产标记仍false；共享Executor、普通API、登录注册和业务配置未改。没有修改生产数据库、旧公告、渠道/价格/余额/账单、部署配置、DNS或Cloudflare，没有重启生产服务。

停止点：完成干净候选、同HEAD本地构建、正式DryRun及回执后停止。**不得据此自动发布；动态监测真实执行仍BLOCKED。**
