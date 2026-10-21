# PG03 自主推进最终报告 — 2026-10-21

结论：**P2-1 PASS；P2-2 PASS（本次固定候选的真实隔离迁移验收）；PG03真实隔离 PASS；生产就绪/整体仍 BLOCKED。** 本任务按情况B收尾，不是部署待批状态，不执行生产部署。

## 实际候选及验收对象

| 项目 | 实际身份 |
| --- | --- |
| 应用source | 799a608d1b5a79df0f2ef8031a7885f414125778 |
| RC | 39511197302 / attempt1，SUCCESS |
| 精确镜像 | ghcr.io/a3225865526-wq/clearapi@sha256:729fdd37591fb333c9cefe202d7c2b7ff4486df31a2689a9b2152abdf7f6965f |
| image ID | sha256:729fdd37591fb333c9cefe202d7c2b7ff4486df31a2689a9b2152abdf7f6965f |
| binary SHA256 | 37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5 |
| RC原始ZIP SHA256 | b02e4189c742e50f2d62312dcb80b60033380ae00c3f442bda2990b948e358a7 |
| 验收control | f02528c351d4812336980fd56810658a6339b05c |
| 真实隔离run | 39517646061 / attempt1，SUCCESS |
| 隔离artifact | 12308671421，3179bytes；原ZIP SHA256 816b0127874f3a77d50b33e52353742300c8d01ce4d3e3c0d43ea8779589136b |

RC原始ZIP/API digest/payload hashes/source/OCI/workflow/CI/平台/二进制/目标DDL均核验。旧source69a/digest07fd/RC39357150402不含新凭证逻辑，未用于修复验收。新源码修复已构建新RC；后续两项修复只改独立控制，Go应用/Dockerfile/依赖保持，不重解释镜像source。生产既有current-main门禁仍要求新的资格处理，见下文。

## 阶段A/C：权威迁移证据和真实结果

候选在真实InitDB→旧选项迁移→InitLogDB完成路径产生专用凭证。driver/Row/Trace/非SQL best-effort错误锁存、顺序、文件排他创建/同步及失败门禁已修复并针对性测试。每次凭证绑定fresh challenge、真实binary、PID、实际hostname与主/日志库类型；成功不由日志、健康、正常退出或相同schema推断。

真实演练在GitHub hosted Ubuntu24.04独立runner，以packages:read实际获取精确候选；固定PostgreSQL17.6、network none、tmpfs、无生产DSN/凭据/数据/host卷/公网端口。两个新Gateway在同一个独立数据库上依次完整启动：

| 验证 | 第一次 | 第二次 |
| --- | --- | --- |
| create/start/healthy/SIGTERM退出 | 0 / 0 / true / 0 | 0 / 0 / true / 0 |
| 实际进程 | exited，非OOM，无Docker error | exited，非OOM，无Docker error |
| 前后schema | 3c1c77f355a2f9376f7cd4133267c99856cd4905b6116ad2ba615dbf84305035 | 同一目标hash |
| 原始专用receipt | success，9字段完整验证 | success，9字段完整验证 |
| proof SHA256 | e0e510d1b0cf8380331c2ff88b9d44c9b9dcbc8194e199b0108d0082641a3478 | 88ef48c257a9c21189e568e9ec743d80c3baa86db81cbe7b0c21206f9d3951d0 |

两次完整CID与challenge不同；同一PG网络共享hostname65537a0e89b7，分别由精确Gateway CID的Docker Config.Hostname与live hostname读取相等后绑定，未用CID前缀假设。原始非秘密receipt/challenge及hash随artifact持久保留，finalize、上传前回读和本地artifact验证均重新核对。

另建空白独立只读PG，SHOW确认default_transaction_read_only=on；同一候选真实退出1、非OOM/无Docker错误并捕获SQLSTATE25006，迁移拒绝检测PASS。负向容器tmpfs退出后proof不可采集，明确保留unavailable_after_tmpfs_exit，不声称观察到文件不存在。该启动无成功凭证资格。

正向4个、负向2个owned容器全部清理，final label查询无剩余；临时registry auth在启动前及最终均移除。成功凭证/结果写入、文件与目录fsync、finalizer结果、单独verify-public及白名单上传均成功。下载后API来源/run/attempt/control/ZIP digest与大小/文件allowlist/control文件hash/运行身份/全部receipt/清理再次验证exit0；原ZIP及result/success/verification保留在evidence/pg03-isolated-39517646061/。

限制：这是schema-only、master、单PG共享main/log的该固定镜像验收，未复制生产业务行，不等于生产Runtime、真实数据升级、全部三库历史发布升级或生产备份恢复。migration.capture_complete=false为legacy日志语法解析标记，保持原值；本次PASS依据专用receipt，不宣称普通日志完整解析PASS。

## 两次失败和有限修复

| run | 实际结果 | 后续 |
| --- | --- | --- |
| 39514525897 | FAIL，create0/start1；未进入迁移；3owned/auth清理成功；无success | 删除Docker不接受的显式max-size=-1，start前严格回读json-file/blocking且无rotation；原始stderr未留，根因仅源码支持的推断 |
| 39516153502 | FAIL，start/health/stop/schema通过，proof invalid_structured_proof；第二次/负向未运行；3owned/auth清理成功 | 原CID-prefix hostname假设被Docker源码否定，改双源实际hostname绑定；原raw未留，不伪造具体失败字段 |
| 39517646061 | SUCCESS，完整双启动/负向/持久化/回读/清理通过 | 第2轮修复重跑结束，2/2，不再自动重跑 |

三份原始artifact和失败事实完整保留。修复不跳过日志、迁移或权限门禁，不拿正常退出/schema相同替代证据。

## 阶段B：代码、CI和独立审查

PR78/79/80/81均使用正常分支/PR，精确head、strict required checks、enforce_admins与当前required approving=0核实后正常merge，未admin bypass。每次注册/dispatch前固定远端workflow字节、两自动发布开关false及触发链；新workflow仅独立手动，contents/packages/actions read，不匹配生产workflow_run消费者。

- 应用source CI39510261549：7 jobs SUCCESS。真实SQLite3.50.4、MySQL8.4.6、PostgreSQL17.6，fresh/repeated startup、合成数据保留、共享/独立日志路径及错误锁存；CI Go1.25.1，本地便携Go1.26.1。未宣称所有引擎最低版本或全部历史生产升级矩阵通过。
- PR79 CI39513279138、PR80 CI39515073600、PR81 CI39516682473：各7 jobs SUCCESS；对应main已完成的自动CI亦如实留证。最终main f02528的自动CI39517515354亦已7 jobs SUCCESS，元数据保留；收尾重新读取main与两false开关一致。
- PR81实际日志：集中67 tests OK、0skip；备份26 tests OK、0skip。其他wrapper92 tests的2平台skip单列。此前jobs/logs接口采集exit1；后通过gh run view --job --log获取真实摘要，未重跑CI补数。
- 本地实际命令：`wsl -u root --cd /mnt/d/AI/API/worktrees/clearapi-controlled-launch-20261020 python3 scripts/ci/test-pg03-successor-plan.py`，73 tests OK/exit0，其中6项adapter基础仅本地；Windows MigrationEvidenceTest15 OK/exit0；Linux root gate14 OK/exit0；OneShot3 OK/exit0（完整原observer16项Linux root结果复用，最后改变的3项定向复跑）；备份真实GPG/归档但DB/Docker替身，不是生产恢复。
- 独立子代理审查覆盖实际源码/driver、备份完成验证、kernel、日志配置、hostname绑定、R2 gate/OneShot/adapter基础及最终artifact；发现的问题已修复并复核，当前本地组件和该镜像隔离证据无剩余直接阻断。它们不是人工审批或生产验收。

修改实物：main.go；model/main.go、gorm_logger.go、frontend_option_migration.go及既有测试；internal/migrationproof/{proof.go,sql.go,proof_test.go}；schema/successor/transaction/isolated控制及集中测试；backup-identity.py、production-backup.sh及备份集中测试；ci.yml和独立pg03-isolated-rehearsal.yml。未改依赖/锁文件/生产开关/Secrets/保护。

## 阶段D：安全本地准备及真正阻塞

生产启动写入面、备份前置/停止态failed Gateway模式与完成reader、部署前后检查/回滚禁区、身份资格清单已更新。successor kernel与严格reader已在PR79合入；新增R2 gate、独立OneShot、adapter的GET来源/六类receipt/列行级scope/真实restore合同与固定Gateway argv计划保留为本地待审差异，具体文件和hash见pg03-successor-local-components-20261021.json。它们未安装或激活，无mutation CLI/SSH/生产workflow；完整production adapter NOT IMPLEMENTED，不能标完整执行器PASS。

仍需最小决定/实际证据：

1. **生产源码资格协议**：现release-control的approved_run/approved_rc要求候选/RC commit等于实时main。799a与已更新控制mainf025分离，隔离PASS不会自动通过旧生产入口。保持检查；负责人选择最终main冻结后新RC及相应真实验收，或另行审阅/批准source-control分离的一次性successor协议，再实施完整adapter，不能自动降级现门禁。
2. **新鲜生产只读渠道与连续全writer窗口**：旧身份/期限不可复用；R2 A尚未选择/安装/激活，且A单次不能证明60秒连续窗口。需负责人明确通道/窗口，管理员按独立批准配置最小SSH/sudo/key/host-key/权限并证明L1–L4。当前helper仅工具/进程/容器/锁，不含DB/schema/金融/完整配置/Executor epoch；完整无秘密投影和scope批准仍缺，不能SSH补证。
3. **准确获准写入与真实备份恢复**：有效开关、任务/待处理业务及金融保护投影未知，不能自行批准奖励/额度/队列/策略/外联变化。需单独批准覆盖所有writer的独占窗口、停止态加密备份、获准离线私钥环境与独立PG真实恢复（含数据/索引/约束/唯一性/PG03/日志）。模拟PASS不替代。
4. **最终adapter和生产执行**：在上述真实合同/事实后完成生产绑定与端到端审查，新的限时一次性审批绑定source/digest/全部证据，再由负责人另行授权生产执行。当前不是仅剩部署按钮；不调用旧failed recovery/V2绕过，previous_success不等于可自动回退，不覆盖数据库或Pool高水位/receipts。

本轮未生产SSH、改账号/sudo/生产配置/发布清单、写生产DB、操作生产Gateway/Executor/Caddy/DB容器、部署或恢复公网。所有旧工作与历史证据保留；无reset/clean/破坏性清理。达到真实隔离PASS并完成安全准备后，停于以上真实授权/协议/证据边界。
