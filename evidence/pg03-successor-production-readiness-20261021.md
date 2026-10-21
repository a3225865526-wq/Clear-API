> 2026-10-21 本轮更新：本地候选资格、连续/执行窗口、适配器协调器、固定host动作、外部challenge启动器及离线恢复runner已补齐开发，状态与验证以 `pg03-production-approval-package-20261021.md` 为准。下文为既有阶段记录，保留其历史实现和真实证据边界。现场身份/collectors、真实备份恢复、生产授权/验收与正式部署仍 BLOCKED / NOT RUN。

# PG03 successor：生产准备及边界（2026-10-21）

状态：本地准备；生产事实、真实备份/恢复、完整 successor 生产执行器仍 BLOCKED。此报告不是生产授权。候选及真实隔离验收以总控当前记录为准。

## 可复核实物

- `backup-identity.py` 默认 running/v1 契约保留；新增显式 stopped_failed_gateway/v2：精确容器、exited、restart=no、双 failed manifest、同项目其他 Gateway writer 不存在，capture/finalize 两次身份完全一致。
- `production-backup.sh` 只有显式 CLEARAPI_BACKUP_STOPPED_GATEWAY 且 CLEARAPI_BACKUP_PRESERVE_EXISTING=true 才选 v2，不写回原 failed 发布状态、不删除历史备份。仅改仓库源码，未安装/运行生产脚本。
- `successor_completion` 校验本次实际进程结果、时间、完整 PG03 artifact 清单、停止态身份、明文 staging 清理和密文大小/SHA/文件稳定性；仅生成待持久化的校验结果，restore_acceptance=not_run，production_execution_allowed=false。现有 legacy manifest 在 EXIT 清理失败时仍可能存在，所以不能单独当完成信号；定向测试实证该失败分支。
- Linux 离线合成备份测试使用真实 GPG/加密/归档/SHA，但 Docker、数据库 dump 和 TOC 是替身。不能标记生产备份或 PostgreSQL 恢复 PASS。
- 独立审查先发现文件原地修改/路径替换/staging 父链接及遗漏 inventory，已修复，复核通过；范围只含本地组件。

## 完整 successor 执行器的前置依赖

现 `pg03-successor-plan.py` 仅离线 admission，execution_allowed 恒 false；另已形成本地 `pg03-successor-transaction.py` 严格request/观测合同和一次性有限状态机，9项Linux集中定向测试通过；无生产adapter或mutation CLI，不代表完整executor。现 Deploy V2 要求 running predecessor，不能直接接收 failed/stopped 的事故源，也不能篡改 failed 为 success。仓库尚无完整 successor 生产执行器，不以备份组件或离线提案冒充已完成。

本地kernel已覆盖不可覆盖持久claim、固定阶段、逐步auth/lease复核、两次不同进程/challenge、保护投影、失败contain/UNKNOWN与禁止重试；trusted adapter接口强制把server认证/证据来源/真实观测留为显式依赖，未提供假默认实现。完整adapter、实际生产绑定及apply仍待实施和审查，不能把缺生产事实当作所有本地实现的障碍，也不能注册或调用生产入口来补证。最终必须绑定真实配置的列级启动写入范围、全部writer资格、备份/恢复和新的生产一次性审批。

1. 新、独立的 owner 手动生产审批，服务端认证 current-main/仓库/actor/triggering_actor/精确 attempt、保护及实际批准，不继承旧失败 workflow 或聊天消息。绑定 request hash、候选 source/digest/binary、RC run/attempt、隔离 artifact hash、备份及恢复 hash、写入范围 hash、UTC期限、一次性 nonce。
2. root 持有现有 deployment_lock 至完整收敛；在任何可能变更前 fsync 不可覆盖的 attempt，断连/超时/UNKNOWN 消耗额度，禁止自动重放。保留 failed manifest/target/hold/overlay 原始字节副本和 previous_success；Executor 原容器/epoch 不变、Caddy 仍 stopped/restart=no。
3. backup 阶段用精确 stopped identity + preserve，独占窗口覆盖所有 Gateway、后台进程及 Executor 可写状态。前后快照只能发现变化，不能单独证明中间没有瞬时 writer。备份脚本进程、cleanup、密文及持久化结果必须共同通过，未来 apply 审批必须绑定实际最终结果。
4. 私钥仅在获准离线恢复环境。实际 decrypt + 安全 archive 清单/hash 校验 + 独立 PostgreSQL17.6 pg_restore + 数据/索引/约束/唯一性、PG03 state 恢复验收；pg_restore -l 不等价。恢复只写独立测试库，不能回灌生产或回滚 Pool 高水位。日志库若独立配置也必须纳入。
5. 仅替换 Gateway：候选 OCI/RepoDigest/image ID/binary、Compose loopback/mount/entrypoint、/run/clearapi runtime 目录与 boot hooks、目标安装资产 hash 都要现场核验。使用新的 successor 事务记录；不能调用旧失败恢复入口当升级器。
6. 启动后验证新进程 PID/start ticks/binary/完整 digest、结构化迁移证据、认证 Runtime/Executor 通道、写入投影、金融基线、schema、重复启动/恢复。任一失败保持入口隔离，停止新增 Gateway 并记录 UNKNOWN/FAIL；不自动启动旧应用、不自动恢复数据库、不重置 Pool checkpoint/admission/receipts。

## 已确认的源码启动写入面（不是获准生产写入清单）

| 触发位置 | 可能写入/条件 | 生产前必须核对 |
| --- | --- | --- |
| model.InitDB / migrateDB | AutoMigrate 全模型/索引/约束；token/prefill唯一性、options PK、plan price/model_limits；VIP seed、offline_order回填、auth version、referral config、credit catalog、external identity claims | RC精确 target DDL、真实 schema/数据、幂等与受保护数据投影；schema相同不能排除DML |
| authz.Init | master启动事务化seed roles和reconcile Casbin/default policies | 现有角色/策略与候选内置规则差异、允许新增/删除集合 |
| InitPasswordEncryption / CheckSetup | 登录密钥缺失时生成并保存；setup 状态维护 | 只采集存在性/模式/hash或计数，不读取密钥值；不得把缺失视作自动授权新建 |
| MigrateRetiredFrontendOptions | 旧options迁移、删除/新key写入；坏值不产成功迁移凭证 | 精确受影响key及脱敏hash，失效配置不默认成功 |
| InitLogDB | 共享或独立日志库迁移 | 真实 LOG_SQL_DSN 是否存在的布尔资格、日志引擎与schema；不输出DSN |
| StartAuthArtifactCleanup | master立即清理过期/撤销sessions和auth flows | 到期对象计数、保留策略、允许删除范围；不读session/token值 |
| MemoryCache初始化失败修复 | FixAbility可能重建能力记录 | MemoryCache开关、abilities/channel状态投影 |
| RunReferralRewards | 立即轮询pending，可能结算奖励与金融记录 | pending数量/配置/保护金融hash；关闭公网不等于worker停写 |
| SyncTaskPlugins | 启动即同步注册与每30秒重载，可能更新插件记录/状态 | 有效插件源、registry/DB差异和是否存在外联；不能由空schema结果推断无写入 |
| UpdateQuotaData / channel updater / Codex credential refresh | 报表聚合、条件频道更新；master立即扫描/刷新Codex凭据 | 开关与待处理数量；只采资格/计数，不读取或备份明文到报告 |
| StartFinanceSyncWorker | owner finance启用才立即重试 | 有效启用标志、pending/review基线及外部财务影响 |
| StartSubscriptionQuotaResetTask | master立即过期/reset/清理preconsume | 到期订阅、preconsume计数及允许额度变化 |
| SystemInstanceReporter / SystemTaskRunner | 立即upsert实例、调度/claim pending、lease/任务执行；handler可产生后续业务写入 | 任务类型、Enabled开关、pending/lease/claim状态，不能只允许system_tasks表就放行任务副作用 |
| BillingProtectionWorker | protection或durable wallet启用后立即recover completion/outbox/log，每10秒重复 | pending outbox、保留needs_review、settled+review marker等金融基线；不自动清理审查项 |
| RecoverRouteChainLedgerInFlight | HTTP前把未终结attempt/request标记OUTCOME_UNCERTAIN/incomplete_restart | 允许的精确非终态集合与结果；不重放派发、不自动退款或清零 |
| Redis/缓存/本地文件 | 配置允许时缓存写入、CleanupOldCacheFiles、日志、runtime状态 | 实际mount/持久卷/外联/写目录、备份一致窗口及回滚边界 |

以上为当前源码可确定入口；最终按新 RC commit、有效配置、数据库行状态形成精确投影再批准。当前无该现场证据，投影验收 UNKNOWN，不宣称完整生产执行器/生产就绪 PASS。

## 最小人工动作及资格证明

沿用 `CLEARAPI_READONLY_IDENTITY_ADMIN_CHECKLIST.md` 的受限只读身份设计及R2审查；历史账号/窗口已过期，不能复用。R2的一次性gate及observer接线目前NOT IMPLEMENTED，没有可直接安装的实物；需先单独明确该身份实现范围、完成本地代码/测试/审查，再由管理员另批安装/核验专用SSH/最小sudo链、helper hash/属主/权限/host-key，签发单次UTC300秒授权与新身份期限，经安全通道注入凭据。本会话不安装、不SSH、不修改账号/sudo。

现有 recovery-tool-vps-helper.py 可交付工具库存、进程/容器最小状态与锁等投影；它不执行DB/schema/金融查询，不交付完整配置或Executor epoch资格。R2 A至多一次调用也不能证明连续独占窗口。应分别设计并审查后，另批以下最小渠道和窗口：DB/schema/受保护金融hash/待处理计数及配置布尔的无秘密只读投影；完整Executor身份/epoch/健康；覆盖GitHub所有refs队列/开关与VPS全部writer的连续独占核验。不能把安装现helper当作这些完整证据。缺失即BLOCKED，不通过扩权/PAT/管理员SSH补齐。

随后单独批准：停止态加密备份及独占窗口；离线私钥恢复环境/数据库隔离；最终 successor 代码/安装资产、写入范围及一次生产执行；最后另批开放入口。每个批准绑定新 RC 和当次证据，不能由本任务的安全非生产授权推导。

## 部署前/后与回滚材料

部署前核对 candidate/RC/control/artifact整链、source CI与审查、双启动证据、最新冻结状态/资格/窗口、备份和真实恢复、startup写入范围、runtime/boot安装hash、Executor身份及Caddy隔离。生产后优先验证新进程/迁移证据/Runtime认证/Executor通道和所有保护投影，再验证获准重启恢复；公网开放、真实login/model调用和扣费只能另行批准并如实记录。

回滚默认是停止新增Gateway、Caddy保持隔离、保存失败证据并人工评估：旧failed仍failed；previous_success只是历史引用，不代表与当前schema/账务可直接回退。数据库、Pool高水位/receipts不可自动覆盖或恢复；需要真实新备份、兼容性和单独授权。未验证项仍UNKNOWN，禁止把本地模拟标成生产验收。

本地kernel依据OWASP Transaction Authorization（服务器验证、交易绑定、限时与一次性消费）、Authentication/Session Management指引分离认证与数据校验；当前生产adapter未实现，未声明ASVS5.0.0合规。官方参考：https://cheatsheetseries.owasp.org/cheatsheets/Transaction_Authorization_Cheat_Sheet.html 、https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html 、https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html 。


## 本轮后续本地准备增量（未安装/未激活/未发布）

以下增量在工作区形成待审差异，未纳入隔离修复PR80，也未把既有未跟踪只读工具整体发布。既有helper/window/readers字节和PINS保留。此节取代上文“gate尚无本地实现”的状态，但不改变身份方案待批及生产禁令。

- `scripts/clearapi-observer-gate.py`：R2 A一次性候选，固定无参root入口、SUDO_UID、root dirfd路径/模式/单链接/ACL/capability、授权实际ext4/XFS/Btrfs存储和相同device、稳定flock；active/boot/UTC-CLOCK_BOOTTIME双锚与≤300秒；claim先于stdin和fork，排他创建/fsync文件+目录。固定helper、绝对期限/输入输出边界、进程组和pidfd、HUP/TERM/INT/PIPE/ALRM异常收敛、发布后再验期限/撤销。无初始化/重置/重放或凭据loader。
- 回执保守记录HELPER_VERIFIED_DELIVERY_UNKNOWN或UNKNOWN，客户端交付失败不返还claim。任意claim形态永久消耗；SIGKILL/不可中断内核操作/恶意root/磁盘快照回滚不冒称自动收敛，后续批次须管理员先证明旧会话/进程结束。候选脚本不安装，active/授权目录/锁/key均未在生产创建。
- `scripts/recovery-tool-observer.py`：独立OneShotReader契约，固定gate/UID/auth_id/key指纹/期限/管理员资格引用及代码hash，首次attempt先消耗、任何结果不能第二次read；复用原reader并核对helper/runtime投影。无live默认，输出gate_claim_verified=false、continuous_window_qualified=false、execution_allowed=false。原v1 WindowDriver不变且拒绝R2 schema；A仍不能用于连续60秒窗口，原helper仍没有DB/schema/金融/Executor epoch投影。服务端claim实际来源仍待管理员提供，helper原JSON不冒充claim证明。
- `scripts/pg03-successor-adapter.py`：注入GET-only transport的live main/strict保护/两false开关核验、successful exact run/attempt/artifact API digest与原ZIP来源、六类payload字节hash与唯一JSON；scope精确列/有界行/选择与前后hash/背景worker合同；真实恢复必须有decrypt/archive/pg_restore/data/index/constraint/unique/PG03/log/cleanup完整合同且绑定本次backup和request。所有内容仍需真实来源，不由JSON中的pass/approval_ref授予权限。
- 固定Gateway命令只编译待审argv，不执行；沿用事故`/opt/clearapi/app`、`/run/clearapi`与150秒停止合同，仅new-api、no-deps/no-build/pull-never。无SSH、Compose执行、生产workflow/审批协议或完整production adapter；旧failed/source/previous_success不篡改。R2 gate中的bounded runner已实际测试，但尚未与生产adapter接线。

实际验证：Linux root gate14项PASS/exit0，包括真实临时文件持久化、flock、重复请求、claim sync失败、helper输出/时限/错误、pidfd失败、跨期限/撤销及实际HUP/TERM后helper退出。初次input泄漏断言误含合成审批ref，已改唯一输入标识；初次信号测试发现selectors会吞InterruptedError，改为自定义GateAbort后同测试通过。没有删除失败事实。Windows adapter6项合同/来源/精确绑定/命令计划测试PASS；Windows OneShot3项PASS；Linux root原observer全16项PASS（随后strict regex/runtime投影变更已定向3项复跑PASS）。集中successor新增adapter方法在原文件，未新增散落测试。

独立审查发现并修复：gate的发布后期限、auth实际挂载、启动审计、运行中信号收敛；adapter三处raw/backup hash绑定、真实路径与超时；OneShot auth/key正则。最终复核以实际返回记录为准；这些局部PASS不代表完整生产adapter或端到端PASS。

### 仍需负责人决定/管理员事实的最小依赖

1. 为只读生产资格单独选择并批准身份方案；A单次只能获得有限snapshot，连续全writer窗口需要不同的明确协议与获准通道。不能把本地A实现当作负责人已选择A，更不能据此SSH或安装。
2. 新鲜候选配置/任务和金融状态的无秘密只读投影，明确允许的schema/DML/背景任务与列/行级范围；现在不能自行批准奖励结算、额度清理、策略变更、外联或队列消费。字段合同已准备，真实collector和批准内容仍缺。
3. 覆盖全部writer的独占窗口承诺与实际连续证明、停止态加密备份授权、离线私钥环境及真实PostgreSQL恢复验收。合成GPG/DB替身不替代这些事实。
4. 独立的一次性生产审批协议与服务器来源，最终adapter安装资产/文件/SSH/sudo资格及端到端验收。当前只完成基础和kernel，完整production apply adapter NOT IMPLEMENTED；不把“仅剩部署按钮”写为事实。

依赖清晰前保留BLOCKED，不调用旧failed recovery、deploy/VPS或生产SSH补齐。


### 候选source与生产current-main门禁

独立只读复核确认：现`pg03-release-control.py`的approved_run/approved_rc都要求候选/部署commit等于实时main；799a候选与后续隔离控制merge已分开。独立隔离PASS（若实际取得）只绑定799a/729fdd，不能因此声明该旧source具备现生产入口资格。本轮修复只改独立控制，不改候选应用Go/Dockerfile，故当前隔离对象仍799a；但生产current-main检查完整保留，不自动降级或沿用过期资格。

生产前选择：最终代码冻结后，以最终main构建并验收新RC；或负责人另行审阅并批准source/control分离的一次性successor授权协议，再实现/审查/验收。该协议尚未决定，完整adapter不能靠自行跳过旧门禁完成。任务安全本地准备组件已独立复核通过，现场及协议缺口仍明确BLOCKED。
