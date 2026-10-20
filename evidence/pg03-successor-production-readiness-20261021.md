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
