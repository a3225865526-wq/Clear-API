# ClearAPI 非 PG03 发布准备 — 2026-10-21

## 2026-10-23 — OFF 发布链闭环完成，生产动作仍需独立审批

- PR #87 已合并：`60ea62441e4a628517e8d3e3c8762d30d0a1556b` / tree `6127ddc5e2a3d2c3f20bbf2c7ff27470f410a36c`；保护 CI run `39806922042` 的 8 项检查全部 PASS。
- 最新正式 RC `39808120659/1` 成功，artifact `clearapi-off-rc-39808120659-1`；镜像 `sha256:0ae055c5ab47a20b27a850491b2de0b7931c73393b3ccbaef891c9b062f561fa`；二进制 SHA256 `37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5`。精确 source/tree/run/attempt/artifact/hash 身份 validator PASS。
- OFF Deployment Rehearsal `39810841937/1` 成功并上传 `deployment-proof-39810841937-1`；最终 proof 本地 SHA256 `d17a8bdfaf873d7a947f2ae2739b8741a39897ce8e23de854d195735845f26c2`。11/11 OFF gates PASS：runtime、billing、backup_restore（离线 backup identity regression）、三数据库升级幂等、普通 Provider usage/billing、pool_disabled、replay/duplicate charge、state isolation、safe rollback。PG03-only gates按模式 skipped，不代表生产验证。
- schema proof 仍是 isolated-only：PostgreSQL 17.6 两次启动通过并收敛，生产 schema `unverified`。`CLEARAPI_AUTO_PRODUCTION_ENABLED=false`，automatic-deploy 对应 runs skipped。没有生产连接、真实生产备份/恢复、部署、流量切换或 PG03/账号池/Executor 启用。
- 当前最短路径：负责人/管理员先确认生产恢复基线与 OFF host/mode-exit 合同；生产 authorizer 仍限定 PG03 workflow/artifact，且保留 OFF execution blocker，须在合同获批后完成对应生产适配与验收。真实生产备份、独立恢复、回滚验证、Gateway 部署和流量切换另行审批。不得把 OFF rehearsal proof 或 isolated schema 证据升级为生产 acceptance。

## 2026-10-22 — OFF RC 真实通过；演练调度参数待修复

- PR #85 已合并（`98a3a1d32ddd078bb0b446ea3b47dab39923c91e`），8 项必需 CI 全部 PASS，run `39794014652`。
- OFF RC `39797527304/1` 基于 main `98a3a1d32ddd078bb0b446ea3b47dab39923c91e` 成功。artifact `clearapi-off-rc-39797527304-1`；镜像 digest `sha256:740d50a5062565d300635dd25bd9bb5223a2bff6a037dea981a06d50a134f759`；二进制 SHA256 `37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5`。精确身份 validator 与 artifact hash 检查通过。
- 隔离 PostgreSQL 17.6 schema 演练两次启动通过并收敛，迁移完成 proof 与 RC digest/binary/commit 绑定；生产 schema 仍 `unverified`。正式 artifact 与独立 diagnostics artifact 均已下载核验。
- OFF Rehearsal 尚未运行：GitHub workflow dispatch 返回 422，因 choice 列表中裸写 `off` 被解释为布尔值，导致服务端选项拒绝。已修正为引号字符串并加 producer regression；WSL producer 11/11、automatic-release 45/45 通过，workflow YAML parse 与 `git diff --check` 通过，待 PR/CI。由于 commit 绑定，合并后必须在最新 main 重新生成正式 RC，再 dispatch Rehearsal。
- OFF Deployment Rehearsal `39804761415/1` 已绑定 RC `39802315993/1`。runtime_integrity、billing、SQLite/MySQL/PostgreSQL upgrade idempotency、ordinary_provider_usage_billing、pool_disabled、replay_and_duplicate_charge、state_isolation、safe_rollback 共 10 项 receipts PASS；PG03-only evidence 按 OFF 模式 skipped。唯一失败是 `backup_restore` 离线回归中的 stopped-incident case：祖先目录安全检查拒绝 root-owned 系统祖先。Assembler 因失败 gate 拒绝最终 proof；artifact `deployment-proof-39804761415-1` 留存，不代表 rehearsal 成功。
- 已放宽祖先目录检查至 root-owned/current-user-owned 且不可由组/其他用户写入，同时保留目标及产物目录、manifest、staging/encrypted、链接和哈希校验。WSL `test-backup-identity.py` 28/28、`test-rehearsal-producer.py` 11/11 PASS。待 PR/CI；合并后仍须基于最新 main 重建 RC、重跑 OFF rehearsal。
- `CLEARAPI_AUTO_PRODUCTION_ENABLED=false`。没有生产连接、真实生产备份/恢复、部署、流量切换或 PG03/账号池/Executor 启用。OFF backup gate 为离线测试，不是生产证据。剩余路径：合并修复 -> 新 RC -> 重跑 rehearsal 并取得成功 proof -> 负责人/管理员审批及现场执行真实生产备份、恢复、回滚与部署。

## 2026-10-22 — 新正式 RC 阻塞与迁移 proof 修复

- PR #84 已合并，merge commit `45c819d15855d4c7cbfce3c56d8739b2dd920052`；8 个必需 CI checks 全部 PASS。
- 新 RC run `39754699152` 绑定上述源码。前端、竞态与 SQLite/MySQL/PostgreSQL matrix 成功；Docker image digest `sha256:4db763f26ab72b4f80732c34a48f4c57603084e59f9ef9ce9b5a4ee319c40a2d` 推送成功。OFF schema rehearsal 阶段失败，故无正式新 RC artifact、无可用 schema receipt、无 Deployment Rehearsal proof。不要使用该失败 digest。
- 已确认 OFF 首轮 schema 变化会被原有共享 helper 过早拒绝；该代码缺陷已限定为仅 OFF 接受完整绑定的候选迁移 proof，PG03 仍拒绝 schema conflict，第二次启动继续要求 schema 收敛。失败 run 未保留 observation，因此实际触发点尚未确认；不把此代码缺陷推定为该 run 的唯一根因。新 RC 将保留单独诊断 artifact，失败也不会签发成功 receipt。生产 schema 继续 unverified。
- 回归：WSL `python3 -B scripts/ci/test-pg03-successor-plan.py`（84/84 PASS）；`test-rehearsal-producer.py`（10/10 PASS）；`test-automatic-release.py`（45/45 PASS）；Python compile、两份 workflow YAML parse、`git diff --check` PASS。
- 下一步：保护 PR/CI 后合并，以最新 main 构建新正式 RC 并核对 observation/artifact/schema proof；只有真实证据完整后才运行已获本任务授权的 OFF Deployment Rehearsal，并先确认自动生产发布关闭。真实备份、恢复、部署、流量切换仍须负责人单独批准。

## 2026-10-22 — OFF RC/Rehearsal 合同修复（本地验证）

- `deployment-rehearsal.yml` 现在按 `pool_mode` 严格选择 RC workflow 和 artifact；OFF 绑定 `release-candidate.yml` 与 `clearapi-off-rc-{run_id}-{attempt}`，PG03 路径保持旧 workflow/artifact 和所有 Executor gate。
- `release-candidate.yml` 为 OFF RC 生成 exact-digest candidate 两次启动的隔离 schema receipt，并把 schema transition 摘要、run/attempt、image digest、binary SHA256 和 runtime artifact hashes 纳入 manifest/inventory。
- proof assembler 与 authorizer 拒绝 OFF 缺失或错配的 schema artifact；OFF proof 绑定仓库、精确 artifact 名、manifest/schema receipt SHA256，且仅记录 `off_isolated_startup_migration` 与 `production_schema=unverified`，不会把隔离验证升级为生产接受。
- 验证命令：WSL `python3 -B scripts/ci/test-rehearsal-producer.py`（10/10 PASS）；WSL `python3 -B scripts/ci/test-automatic-release.py`（45/45 PASS）；Windows Python compile、PyYAML workflow parse、`git diff --check` PASS。
- 本节仅证明代码合同和本地回归；没有生成新的 GitHub RC、GHCR digest 或真实 Deployment Rehearsal。旧 RC `39698686281/1` 不含本修复后的 schema artifact，必须由合并后的最新 main 重新构建。

## 2026-10-22 — 正式 main CI 与非 PG03 RC 身份验证（最新）

### 正式分支与保护 CI

- PR #83 已于 2026-10-22 合并：https://github.com/a3225865526-wq/Clear-API/pull/83。Merge commit f5751ffba116048551312d6ffbb3473fd83e63ad；tree 39d06ca31f4b19eb30c1555221d2c1318b24b12e。
- PR head 63d70974bb6f28a757c685c5b4e578629286a545 的保护 CI run 39696740955 全绿，8 个 jobs 成功。合并后的 main push CI run 39697652261 全绿，包含 required backend/frontend/secret checks、SQLite/MySQL/PostgreSQL 矩阵，以及 disposable runner 上的 PG03 restore contract。该 PG03 测试没有接触生产或启用账号池。
- 首轮保护 CI 发现 off 手动 wrapper 合同与测试不一致，以及 installer 测试依赖未随提交的旧本地 evidence 文件。已分别修正并通过新的 PR CI；stale inventory 测试现在在隔离临时目录生成 fixture，不依赖本地历史事故证据。

### 正式 RC 与精确身份

- Release Candidate workflow run 39698686281，事件 workflow_dispatch，source commit 与 main 一致，结论 SUCCESS。Race tests、SQLite/MySQL/PostgreSQL DB matrix、frontend production build 和 Docker/GHCR job 均 PASS。
- GHCR image：ghcr.io/a3225865526-wq/clearapi@sha256:5f7ddd26258a2fa411e55cd5e13ccf36e43ec4b20506cc44dcb001ce97903bf7，平台 linux/amd64，pool mode off，manifest 明确 production_ready=false。
- 源码 commit/tree：f5751ffba116048551312d6ffbb3473fd83e63ad / 39d06ca31f4b19eb30c1555221d2c1318b24b12e。GitHub RC job 从 GHCR 按上述 immutable digest 拉取镜像，校验 OCI revision 后提取 Gateway 二进制及启动资产。
- Gateway new-api SHA256：37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5。下载的 RC artifact clearapi-off-rc-39698686281-1 中 manifest、binary、runtime installer、inventory 和 helper 共 7 个已列文件全部通过本地 SHA256 复算；binary SHA 与 manifest 一致。
- Runtime inventory schema clearapi.runtime-assets/v2，绑定同一 commit/tree、run 39698686281 attempt 1、Gateway manifest SHA bd892006ab3053121e62ceae280782eeda80aed489a10a43c9f9890631f1ce4d、镜像 digest、installer template、两个 helper 与四项嵌入启动资产哈希。目标路径 /opt/clearapi/runtime/new-api -> 容器 /run/clearapi。
- 逐文件 artifact hashes、manifest、inventory 和二进制位于 evidence/non-pg03-20261021/formal-rc-39698686281/。与此前 local snapshot 不同，这一组身份来自正式 main commit 和 GitHub RC；旧 local digest 仅为历史本地证据。

### 当前边界与真实阻塞

- 自动 RC/生产开关为 false。PG03 RC、deployment rehearsal、automatic production release 的关联 workflow-run 为 skipped。本轮没有连接生产、生产数据库或 SSH，没有部署或改动生产配置。CI 的隔离数据库恢复只证明测试环境合同。
- BLOCKED / NOT RUN：真实生产备份及清洁退出/加密 manifest 证据；独立环境真实恢复与数据完整性核验；旧生产镜像+对应数据库恢复点的真实配对回退；现场 host 工具、权限、Compose、资产和 ingress 隔离基线；此精确 RC 的 off rehearsal 与生产 authorizer proof；最终线上 API/计费/静态资源验收。现场状态新鲜度为 UNKNOWN。
- 下一次最终审批包必须绑定上述 RC commit/tree/digest/binary/runtime inventory，并附真实 backup manifest/encrypted SHA、独立 restore 比较结果、旧镜像与数据回滚点及回退记录、现场 host baseline、off rehearsal/授权 receipt、维护窗/责任人、逐步停止条件和验收标准。缺项或任一项 UNKNOWN 时不得部署。
- PG03、账号池和 Executor 调度继续关闭。正式 RC digest 是候选身份，不是生产批准或可部署授权。

## 2026-10-22 — 复核完成与最终本地 off snapshot build（最新）

### 变更与独立复核

- 持续使用指定工作区，没有新建 worktree。复用此前对 `/opt/clearapi/runtime/new-api`、Compose 只读 bind、`/run/clearapi` 容器目标、缺失父目录创建顺序和 runtime-assets v2 inventory 绑定的修复；保留冻结事故工具、旧 inventory 和原失败证据。独立静态复核未发现冷启动修复或生成 installer 的新阻塞；该 reviewer 未运行测试。
- 当前 off rehearsal 会执行普通 Provider lifecycle、pool-disabled、备份/恢复工具合同和 safe-rollback 本地测试；off proof 不需要 PG03 Executor pairing。Production authorizer 对 off 仍有 fail-closed 拒绝。该拒绝因真实现场处于 failed/isolated，且 host mode-exit、真实备份恢复和配对回退没有证据而保留，不为“打通发布链”移除。
- 早先本地候选 `fcb733e9...` 的 source tree 与当前工作区不等（差异是总控文档）。该候选不再作为本次精确快照。WSL 内没有 Docker CLI，第一次构建命令在 Docker 检查前失败；Windows Python 运行同一受控 builder 后成功。`local-off-rc-20261022T-build2/` 是失败尝试创建的空目录，没有产物。

### 新快照身份

- Base commit：`f2e179ebc8e2d0f29b06603376a49a1058f9d496`。精确 Git tree snapshot：`526c76e682e39b9a0816005b986d6b6ae7189d31`；本地 run `local-20261021T231507Z`，不是 Git commit、GitHub run 或正式 RC。build3 已被此最终总控 tree 的 build4 取代。
- 本地镜像 `clearapi-off-rc:526c76e682e39b9a`，平台 `linux/amd64`，OCI manifest digest/image ID：`sha256:2923ea8fa0940c319be8c8e206957a2b14c4771115635dbdfec1b5ed0d602119`。Gateway `new-api` SHA256：`979713027c986fbd3d4f2e95bf74414f0f3db26481d19bc10c4753162bd9c394`。
- Source tar SHA256：`5f562f4b4b6e08a1dd3ad4f7f798f00847bf129eb1aab27b807c39ff1e7b7de5`；OCI save archive SHA256：`c782fe243deba13a5cb55f5c402923e9b5986f85508606460db993ce8d828563`。Gateway manifest SHA256：`9be474e4851dfc39e907ab54f1948ce47c520e2e3ce5f00c486a5503220e2899`。
- Runtime inventory SHA256：`24a26bef2929cbda58354ce19bfbbde295473312562cb7845ff89b70f56e6381`；generated installer SHA256：`34ba3aca46def45d80cd001401b1599baa1761ce75655b97d58f86e581a37f39`；boot helper SHA256：`7c205fa4861ecbfca344f8289472be9ecd6e52abc7008f3ece4ffe3ec012b8c2`；identity helper SHA256：`afcefce7dcf05bf853a1c44244ddf834dd9e2d6fb023f6cb75495410fc7b7b8e`。Runtime 为 host `/opt/clearapi/runtime/new-api` -> container `/run/clearapi`。
- Build directory：`evidence/non-pg03-20261021/local-off-rc-20261022T-build4/`。19 项 build artifact hash 全部匹配；当前候选 tree 与以隔离 Git index 重算的工作区 tree 一致。Runtime inventory 的 tree、RC manifest SHA、image digest、off mode 和 runtime path 与该产物一致。
- Manifest 明确 `source_commit=null`、`production_ready=false`、CI authentication、migration validation、registry roundtrip、production acceptance 均 `not_run`。它是精确的本地复验快照，不可作为生产部署 digest；不得把本地 OCI digest 等同 GHCR digest。

### 本轮验证

- Linux/WSL `test-runtime-boot-recovery.py -v`：16/16 PASS，含 cold boot parent、危险父目录、mount/image drift、冻结事故拒绝、停止态不启动。
- Linux/WSL `test-boot-recovery-install.py -v`：34/34 PASS，含新 tree/manifest bundle、旧 inventory/改写 manifest/helper 拒绝、安装失败关闭、journal/archive 校验和隔离 rollback。
- Build manifest/tree 与 runtime inventory 身份核验 PASS；根 artifact-hashes 19 项零 mismatch；`git diff --check` PASS。
- Release authorizer/rehearsal producer/backup/tool/rollback 已有同一源代码快照的近期结果：44/44、9/9、28/28、4/4 PASS（详见上节）；本轮没有重复运行。这些仅是本地软件合同，不能证明生产操作。

### 生产决策

**仍不具备提交可执行生产审批或部署的条件。** 正式 commit、保护分支 required CI、GitHub off RC artifact、GHCR exact digest pull roundtrip 均 NOT RUN；真实准确生产备份、独立真实恢复、真实旧镜像+数据库成对回退、host 工具合同现场核验/升级、boot bundle 实际安装及线上验收均 NOT RUN。现场新鲜状态 UNKNOWN，不能以历史只读记录代替。

没有提交/推送/PR、没有触发 GitHub workflow、没有访问生产、没有操作生产数据库或服务、没有真实恢复/部署。PG03、账号池和 Executor 调度保持关闭。仅当正式候选、真实备份/恢复/回退证据和现场 baseline 齐全后，才生成替代本包并申请一次覆盖具体步骤的生产授权。

## 2026-10-22 — durable runtime、新资产绑定与 local snapshot build

### 本地修复

- 所有非冻结事故候选路径统一指向宿主 /opt/clearapi/runtime/new-api；Compose 只读 bind source 为同一路径，容器目标 /run/clearapi。boot recovery 从准确 Docker inspect 校验唯一 bind source，并只在该持久目录上读写身份文件。冻结事故 c50a 与 sha256:22ec 被拒绝，不修改历史事故状态或旧恢复工具。
- prepare-directory 安全检查 /opt/clearapi 祖先，只在缺失时创建固定的 /opt/clearapi/runtime root:root/0755，校验已存在 parent 后运行精确 tmpfiles 配置建立 runtime root:root/0750。若现存目录 owner/mode/symlink 不符则失败关闭，不做权限归一化。
- 保留固定旧 inventory 与其来源历史。build-runtime-assets.py 只接受 exact Git tree、相符 Linux/amd64 off RC manifest、相符 source_tree/commit，或显式标记 local tree snapshot；绑定 installer template SHA、manifest SHA、image digest、runtime path、identity helper SHA 与实际六项资产内容。旧 supplemental inventory、改写 manifest、错误 source/tree/pool mode/run 与 helper 字节漂移均 fail-closed。
- 新 bundle 只包含 runtime boot helper、identity-refresh helper 与四个生成的 tmpfiles/systemd 文件；不包括冻结的 PG03 failed-recovery 工具。生成 installer 通过隔离事务验证安装、identity/metadata/archive、systemd ordering、rollback；本地演练不执行真实 systemctl 或服务启动。

### 定向验证

- test-runtime-boot-recovery.py：16/16 PASS。
- test-boot-recovery-install.py：34/34 PASS。
- test-automatic-release.py：44/44 PASS，含 off production authorization 拒绝。
- test-rehearsal-producer.py：9/9 PASS；off gates 对应真实本地测试命令。
- test-backup-identity.py：28/28 PASS；临时 fixture/fake Docker/GPG，不是生产备份/恢复。
- test-production-deploy.py -k rollback：4/4 PASS（复用本地结果）。YAML、Python compile、Bash -n、相关 git diff --check PASS。

### 本地候选镜像与产物身份

- Base commit f2e179ebc8e2d0f29b06603376a49a1058f9d496；源身份是 Git tree snapshot f943440be1bab83712f7103c38ec8287c8d7498a，不是 commit。source.tar SHA256 e6e89671318ac8ca88834a69a10c945240945b7fb63f09c663c0f9597da5671d。
- Local run local-20261021T211111Z，image clearapi-off-rc:f943440be1bab837，linux/amd64 OCI manifest digest/image ID sha256:fcb733e9761b084c9721279d4e20270ab5cc29db0218e9a90b6c39d490496aad。OCI save archive SHA256 504a60b274e857adc57e1a8728f5dc372da8d335a8d7b8ef83cdd74c146b61b6；OCI manifest/config/layer digests 在 build 时逐项校验。new-api SHA256 979713027c986fbd3d4f2e95bf74414f0f3db26481d19bc10c4753162bd9c394，与镜像提取二进制一致。
- Runtime inventory SHA256 e0fe84c51d60cda8c6249050f4db34c2c02582eea0bd8dd22787944946e1507f；generated installer SHA256 699a40ab8e53c89d6a480099b0371a68e479ebbf0d5aa26686fc03d811bad96f；runtime helper SHA256 7c205fa4861ecbfca344f8289472be9ecd6e52abc7008f3ece4ffe3ec012b8c2；identity helper SHA256 afcefce7dcf05bf853a1c44244ddf834dd9e2d6fb023f6cb75495410fc7b7b8e。artifact hashes见同目录 artifact-hashes.json 与 runtime-assets/artifact-hashes.json。
- 全部文件在 evidence/non-pg03-20261021/local-off-rc-20261022T-build1/。manifest 明确 source_commit=null、production_ready=false、registry_roundtrip=not_run、ci_authentication=not_run、migration_validation=not_run。这是可复验的本地构建产物，不是正式 RC，不是 GHCR digest，也不是生产部署身份。旧候选 f93 不代表本次工作树；本地 fcb digest 不能部署。

### 最终生产审批包状态

本地启动路径和资产旧 inventory 阻塞已解决；不过在正式 source commit、受保护 main CI、GitHub RC artifact、registry pull digest roundtrip 产生前，无法提供可批准部署的生产候选 digest。现场当前状态新鲜度 UNKNOWN。真实准确备份、独立环境真实恢复、生产数据比对与旧镜像/数据回退、host 工具合同升级及实际 boot bundle 兼容检查/安装、Gateway/Executor 普通 API/TLS 验收均 NOT RUN/BLOCKED。

未提交、推送、触发 CI/RC workflow、访问/修改生产、操作真实数据库或服务、进行真实恢复或部署。PG03 持续关闭。后续唯一可提供完整审批的路径：先形成正式 commit/CI/main/RC/registry digest，再按该 RC 重生成哈希绑定启动包；随后负责人批准具体维护窗与生产操作序列。

## 2026-10-22 — off gate 证据来源修正与独立审查

独立只读审查发现 off rehearsal 的 `backup_restore` / `safe_rollback` receipt 过去可由人工填写 JSON 字段生成，不能证明命令实际执行。现工作流在 off 模式改为调用 `test-backup-identity.py` 与 `test-production-deploy.py -k rollback`；off Provider 与 pool-disabled gate 继续以实际 Go 测试产出。移除了已不可达、构造人工成功 JSON 的 `RehearsalGateEvidenceTest` 及对应 verifier 分支。PG03 的操作员证据门禁保持不变。生产 authorizer 仍在 host admission 前拒绝 off。

- `wsl python3 -B scripts/ci/test-automatic-release.py -v`：44/44 PASS。
- `wsl python3 -B scripts/ci/test-rehearsal-producer.py -v`：9/9 PASS；新增合同断言确保 off 的备份/回滚、普通 Provider 和 pool 关闭步骤使用真实本地测试命令。
- `wsl python3 -B scripts/ci/test-backup-identity.py -v`：28/28 PASS；使用临时数据库/替身/临时密钥，仅验证工具行为，不证明生产备份内容可恢复。
- `wsl python3 -B scripts/ci/test-production-deploy.py -k rollback -v`：4/4 PASS；仅本地模拟部署回滚合同。
- rehearsal workflow YAML 解析 PASS；相关文件 `git diff --check` PASS。
- **独立审查确认的残余启动资产问题**：当前 helper 可依据 inspect 接受 durable mount source，但候选 Compose 的 `/opt/clearapi/runtime/new-api` 与旧启动资产只创建 `/run/clearapi` 不兼容；installer/inventory 又绑定旧 source/tree/RC 的固定 hash，当前修改不能由旧包安装。需在有效候选身份确定后生成、绑定并独立审查新资产包及启动目录合同；本轮没有通过更换路径或弱化 installer 门禁规避。
- **生产证据状态**：准确新备份、独立真实恢复、数据和旧镜像回退 NOT RUN；failed→off 的生产 host contract BLOCKED；生产现场新鲜性 UNKNOWN。当前旧候选 `sha256:f93f02ea…fa5b06`不包含本轮修改，不能作为该修改的发布候选。未连接生产、未操作生产数据/服务、未触发 workflow 或部署，PG03 保持关闭。

## 2026-10-22 — 本地隔离复验（不含生产操作）

本轮在既有 boot-recovery 实现上做了一个本地修正：check/reconcile 先从准确 Gateway inspect 投影核验唯一 `/run/clearapi` 只读 bind 的宿主源，再对该实际路径校验/刷新身份；允许候选的 durable `/opt/clearapi/runtime/new-api`，但冻结的失败事故仍必须保持 volatile `/run/clearapi`，其他mount漂移失败关闭。新增隔离用例覆盖 durable 路径重启后重建身份及mount漂移拒绝。未改写 Compose/mount 或触碰生产；未重复前端、三库、完整发布链测试。备份实现复用现状。

- `wsl python3 -B scripts/ci/test-runtime-boot-recovery.py -v`：14项PASS。包括 durable/volatile mount身份核验、tmpfiles 重建丢失目录后由新进程 reconcile、systemd 启动顺序、停止态 Gateway 不启动及mount漂移拒绝；均为临时目录/替身演练，不证明生产资产已安装或现场已恢复。
- `wsl python3 -B scripts/ci/test-boot-recovery-install.py -v`：31项PASS。覆盖隔离安装事务、字节/元数据归档、锁、冲突/竞态拒绝、systemd加载校验及回滚；替身测试不覆盖生产主机已安装工具的兼容性，现场既有helper/version冲突仍需授权只读核对后解决。
- `wsl python3 -B scripts/ci/test-backup-identity.py -v`：27项PASS。包括身份/镜像绑定、失败退出码保留、显式停止态合同与writer隔离复核、加密备份和隔离恢复fixtures；使用本地替身/临时数据，不是生产备份，也不证明生产备份内容可恢复。
- **当前结论**：本地相关保护合同为PASS；Gateway有效宿主挂载、boot资产与兼容helper现场状态、准确备份、独立真实恢复、数据及旧镜像回退仍分别为UNKNOWN/BLOCKED/NOT RUN。off自动发布虽有本地mode-aware证明合同，但真实off gate、failed→off现场模式转换合同和自动部署链验收仍未完成。候选 `f93f02ea…fa5b06` 没有部署；PG03保持关闭。生产写操作仍需单独、具体批准。

## 已批准有界只读诊断结果（最新权威；2026-10-21 23:52 上海）

**一次诊断已实际完成，两个故障点已确认；生产修复/恢复/发布仍BLOCKED。** 本节取代以下历史节中的“等待诊断批准/首错UNKNOWN”，但不取代其恢复门禁。用户本轮原话：“我已批准上述一次有界生产只读诊断。请按既定范围执行……不得扩大授权范围。”没有将历史其他chat的权限当作本轮授权，也没有套用旧helper安装/恢复批准。

本次实际1次SSH会话，client记录2026-10-21 `15:52:05.661707–15:52:09.402008 UTC`，服务器自身记录`15:52:09.197469–15:52:09.481131 UTC`；两端时钟独立，不据微小偏差作跨机时间排序。复用原Windows OpenSSH私钥**路径**，只核查文件存在/ACL，未读取或输出私钥内容；严格固定历史ED25519 host fingerprint `SHA256:kVqtsCzwAanVxCoVB5OA4b1lvMVdF9bpxYhy8xjNuLc`。本机默认目录无key、agent不可用后，从既有“检查并复用 VPS SSH 连接”chat找回准确历史路径，没有新建身份、扩大磁盘扫描或要求用户重贴文件。

唯一远端入口为`LC_ALL=C sudo -n /usr/bin/python3 -I -B -`，脚本通过stdin输入，未落盘。内部固定调用仅systemctl show、指定service/6分钟窗口journalctl、准确CID的docker inspect，以及日志尾部/指定工具hash/目录元数据读取；没有Docker exec/run/start、SQL、HTTP或现场helper执行。stdout为脱敏JSON，stderr为空、SSH exit0。日志250条上限，实际6条，未触顶；backup.log尾部最多65536 bytes，实际文件13799 bytes，只返回相关时间窗口3条固定生命周期消息。未白名单化的3条journal消息只回传hash，不带原文；不采集Env/命令参数/秘密内容。

### 1. 备份失败首因已确认

| 时间（UTC） | 同一invocation的实际发现 | 判定 |
| --- | --- | --- |
| 2026-10-20 19:31:16（上海10-21 03:31:16） | service真实ExecStart为`/opt/clearapi/backups/bin/clearapi-backup.sh`；InvocationID=`3f1a8b289e1e46e4b27d5752bfdfff19`，本次start/exit均在该秒，仍failed，ExecMainStatus=1。 | PASS：现场元数据 |
| 19:31:16.415721 | PID264682，匹配该InvocationID：`backup identity verification failed: expected exactly one running new-api container`。 | CONFIRMED：首个明确应用诊断为running Gateway身份前提失败 |
| 19:31:16.465799 | PID264707，同一InvocationID：`chmod: cannot access '/opt/clearapi/backups/encrypted/20261020T193116Z/*': No such file or directory`。 | CONFIRMED：次生空目录cleanup错误，晚约50ms |
| 同秒backup.log | `START backup_id=20261020T193116Z`→`FAIL ... rc=1`→`END ... rc=1`，相关窗口无OK行。 | BLOCKED：该次不能作为成功备份 |

结合已绑定的真实脚本顺序，identity capture位于pg_dump之前，故这次在数据库dump前已被拒绝。**现场首因不再是猜测；原identity helper本来也以1退出，因此不能声称本次chmod把某个已观测到的17/23改成1。** 上轮17/23→1是另一个已复现的本地缺陷，恰好也在现场出现次生chmod错误；原始失败、两类问题和修复边界均保留。

现场版本新鲜hash及root:root/0750，均精确匹配Git事故source `c50a53c1b89e121bcade7dac41d3f11e53a9832a`，未安装本地新代码：

| 工具 | 实际SHA256 |
| --- | --- |
| clearapi-backup.sh | `0d70b1e1228827d3f275e2e56b15772f560dfc95d9be10bdb9ad0992d1726023` |
| backup-identity.py | `fd6f4fe5bdb4c125dc286df7e80c71e6bcab2285f713f14e74feef543ae1696b` |
| pg03-backup-state.py | `1b974d1de8fc0a9af6477474d34375203c5eb5a41b61c34e69857dfa5df5f43f` |
| runtime-identity-refresh.py | `afcefce7dcf05bf853a1c44244ddf834dd9e2d6fb023f6cb75495410fc7b7b8e` |

安装中的identity helper不支持`--stopped-gateway`。因此**不能在退出Gateway状态下反复启动旧backup service，也不能为了备份先强行启动Gateway**。候选停止态合同仍要求准确CID、restart=no、双failed、全writer窗口及真实completion，不能省略。

旧备份`20261020T002138Z`的密文/manifest hash、旧镜像在场和合成恢复CI按原范围复用。本轮未解密、读取备份payload、进行真实restore或新增恢复点，也未遍历服务器恢复材料。**真实可恢复性仍UNKNOWN；准确新备份/隔离恢复/数据与旧镜像回退门禁仍BLOCKED/NOT RUN**，不能把首因查明或密文存在判为恢复PASS。

### 2. Gateway 127 已确认是 OCI 初始化挂载失败

准确CID仍为`f8f963a7df5f5114a02a7660e18121411c6aa5f284924d60b514a638dc10d24f`，image仍为`sha256:22ecaa468c784cd41ee2604dd8f3ac189960744516a508fc8c9cee3ee1724e02`，不是拟发布f93f。状态exited127、running=false、restarting=false、OOMKilled=false；restart policy仍unless-stopped/0。StartedAt=`2026-10-20T00:25:59.270480344Z`、FinishedAt=`2026-10-20T00:46:42.804107636Z`。它们是Docker保留的状态时间，不能当作本次诊断发生了启动。

新鲜完整State.Error：

```text
failed to create task for container: failed to create shim task: OCI runtime create failed: runc create failed: unable to start container process: error during container init: failed to fulfil mount request: open /run/clearapi: no such file or directory
```

有效Path和entrypoint首项均为`/entrypoint-secure.sh`，entrypoint共2项。本轮只取可公开的执行路径，不取任意Args或Env；第二项/最终应用参数没有额外采集，不能冒充完整effective Compose验收。OCI在初始化阶段无法满足mount，本次失败早于进入应用，不存在已证实的二进制缺失或应用崩溃证据。

| 挂载/目录 | 新鲜结果 | 含义 |
| --- | --- | --- |
| host `/run/clearapi`→container `/run/clearapi` | bind、RW=false、rprivate；host源目录不存在 | CONFIRMED：实际OCI报错的直接条件 |
| host `/opt/clearapi/runtime/new-api` | 目录存在，root:root/0750；父runtime也root:root/0750 | 与仓库durable模板一致的目录在场，但它不是当前容器mount source；不静默切换 |
| `/run` | root:root/0755目录 | 不把父目录存在误认为子路径存在 |
| runtime-boot-recovery.py | 缺失 | 现场boot资产缺口仍在；其余systemd/tmpfiles沿用旧缺失证据，未扩大到重新扫描 |
| 四个secret挂载 | sql_dsn、session_secret、crypto_secret、token_encryption_key均有只读bind元数据 | 仅证明配置的挂载元数据，未打开文件或验证secret有效性 |

**故障诊断PASS；恢复BLOCKED。** `/run/clearapi`为何消失（重启、删除或其他原因）仍UNKNOWN；本次未批准扩展读取boot/删除历史，未补查。新建空目录、切换到durable路径或直接启动仍会触及现场身份/迁移/后台写入及事故恢复合同，均未执行。

### 完成、证据与剩余最短路径

执行记录`bounded-diagnostic-execution.json`、脱敏原始JSON `production-bounded-diagnostic.json`、本地归因及Git字节绑定`bounded-diagnostic-findings.json`、实际stdin脚本`bounded-production-diagnostic.py`均保留。本地仅做collector AST和脱敏allowlist校验，以及新鲜工具hash与既有Git对象的比对；前轮31项本地修复验证和应用/前端/三库/RC证据继续按原范围复用，未重复运行。

本轮没有修改任何生产代码、配置、数据、权限或PG03状态；没有备份重跑、reset-failed、解密恢复、安装、启动/重启、部署、推送或workflow触发。SSH/sudo本身可能产生正常系统审计记录，不将这等同业务写入。未重查PG03调度开关、current-main、自动开关或公网，它们的新鲜性仍UNKNOWN。

**后续必须另行批准受控修复/备份恢复阶段**：固定准确已审查的backup+identity及兼容host工具版本，在完整writer隔离与身份条件下建立真实新恢复点，完成独立恢复及旧镜像/数据回退证明；再按兼容boot/恢复合同处理当前挂载前提和failed/isolated→off转换。上轮off证明补丁保持待审且生产authorizer继续拒绝off，不能以本次只读批准安装或放宽护栏。目标f93f的未来main/control资格仍须重新核实。**本次一次性诊断已完成，到此停止，不追加SSH或扩大授权。**

---

## 本地最小修复（最新权威；2026-10-21）

**已实际完成两处本地修复；生产发布仍 BLOCKED。** 本节取代以下历史续办节的当前实施状态；原始证据与失败均保留。目标仍是source`75b0f09e2a527c5173c1179a6efe697de4f64f39` / RC`39619413994/1` / image`sha256:f93f02ea737721e621681771424d3b0fca4fd8d1c8d6e3b7804097b793fa5b06`。补丁是未提交的本地control变更，**不是这个RC已构建/验证过的新源码**。未改变应用、迁移、依赖和锁文件，未扩大到新工作区/主线。

### 1. 备份：修复掩盖原始错误的清理缺陷

旧`production-backup.sh`在`STAGE`/`ENC`创建后若身份捕获或dump失败，EXIT cleanup对空`ENC`执行`chmod 600 "$ENC"/*`；Bash保留未匹配通配符，chmod失败后把原退出码改成1。本轮严格本地替身实际复现原身份exit17→1、dump exit23→1（`local-backup-diagnostic-before.log`两项subtest失败）。通过事故c50a Git原字节SHA256`0d70b1e1228827d3f275e2e56b15772f560dfc95d9be10bdb9ad0992d1726023`证明留存现场安装版本也包含该缺陷，**所以service exit1不能唯一定位首个错误**。

修复只处理密文目录实际存在的普通文件；新增固定阶段名`identity_capture/database_dump/encryption/...`的FAIL日志，不输出BASH_COMMAND、参数或秘密。失败不发布新manifest；清理失败仍返回失败。单个定向用例的两个subtest现分别保持exit17/23并记录准确phase、清理staging、无成功marker。另复核成功加密备份/旧reader和已发布legacy manifest不能掩盖cleanup失败。全程是临时目录、严格fake Docker、临时GPG测试密钥，不使用生产dump/密钥。

**判定**：本地错误掩盖缺陷FIXED/PASS；现场19:31:16的首个故障仍UNKNOWN。补查本工作区剩余backup/restore/failure命名材料后，未找到该invocation journal或backup.log具体诊断，不能靠推测补出。原备份hash/镜像在场记录复用，真实独立恢复及数据+镜像回退仍BLOCKED/NOT RUN。停止态backup的restart=no、双failed、独占writer与completion receipt合同全部保留；本地修复未安装到生产，不能直接重跑备份。

### 2. Gateway：保留有证据的挂载诊断，不虚构修复

复用上节所列同CID `f8f963a7…` 的missing-runtime-mount=true、后续`/run/clearapi`不存在/只读bind/exit127、缺失boot资产与控制工具不兼容事实。最新完整State.Error/时间/effective entrypoint仍未采集。没有新增二进制缺失证据，也没有理由把现场volatile `/run/clearapi`改成仓库durable模板。现有boot/恢复安装方案及其STOP记录继续有效，本轮不改这些文件、不重跑原成功RC/boot测试，不把“本地可以建目录”写成“现场已恢复”。**现场恢复BLOCKED，最新错误细节UNKNOWN**。

### 3. 发布链：off证明不再依赖Executor，生产端继续失败关闭

本轮仅修改7个原先干净的代码文件并新增1个共享合同模块：

- `scripts/ci/release_proof.py`集中模式/检查集合与receipt契约。PG03原11项集合保持完整；off保留Runtime、计费、真实backup_restore、三库升级幂等、重复扣费、状态隔离、safe_rollback共9项，替换的两项为`pool_disabled`与`ordinary_provider_usage_billing`。off不能使用controlled延后普通Provider验收。
- `produce-gate-receipt.py`接受显式`--pool-mode off`，拒绝该mode不适用的PG03 gate；输出v2和mode，真实命令非零仍产生failure receipt。
- `assemble-release-proof.py`可以不提供Executor参数组装off/v2，强制每份receipt为off/v2并保留原source/digest/RC/attempt绑定。缺任一共同gate、错误mode、失败或旧receipt均拒绝。此处只是合同准备，**没有生成真实生产off成功proof**。
- `automatic-release.py`本地validator能验证off/v2；v1只按历史PG03解释，不能贴off标签。返回已验证mode，同时保留已部署controlled/v1 reader所需的精确授权对象字段。真实authorizer仍验证可信workflow/当前main/CI/RC/artifact/熔断，随后对off明确返回`off production execution blocked: recovered baseline and approved host contract required`。不能以本地proof替代现场准入。
- `deploy-production.yml`去掉授权步骤硬写`pool_mode=pg03`，改用Python authorizer已验证输出。原off部署分支拒绝rehearsal ID、健康Runtime/Executor存在性/模式退出/事故隔离等host护栏没有放宽；当前rehearsal也没有接通真实off专用gate producer。**完整off生产路径仍BLOCKED，不能dispatch测试或绕过护栏**。
- 回归集中于原`test-automatic-release.py`和`test-backup-identity.py`，未散建测试文件。可审阅差异`local-minimal-repair.patch`不含原有CI/observation/host等用户修改；已用`git apply --reverse --check`验证它与本次工作区改动匹配，仅检查，未实际apply/revert。

### 实际验证、失败保留与停止点

| 实际执行 | 结果和边界 |
| --- | --- |
| 新off回归在旧代码上 | 7项，报告2 failures/4 errors（含同一方法的subtest）；实际暴露旧assembler强制四项Executor参数、validator缺off合同。工具输出保留在本聊天，不冒充生成了完整原始日志文件。 |
| Linux `python3 -B scripts/ci/test-automatic-release.py NonPG03ProofTest ReleaseProofTest -v` | 最终24项PASS，exit0；含7项新增off测试和受影响PG03合同/controlled/CI链/熔断兼容回归。日志`local-mode-proof-final-tests.log`；前一版Linux通过日志也保留。 |
| Windows同一受影响集合 | 23项通过，1项因WinError1314不能创建symlink报错；`local-mode-proof-tests.log`原样保留。换Linux是明确针对平台权限假设，同一symlink用例通过，没有删除或skip安全用例。 |
| Linux backup定向集合 | 新失败阶段/退出码用例、成功加密备份/旧reader、legacy marker+cleanup失败，共3项PASS；`local-backup-diagnostic-after.log`。修复前2个subtest失败日志保留。 |
| Linux producer/assembler兼容 | 失败命令仍failure、缺gate拒绝、controlled仅延后Provider、另一RC/变化schema拒绝，共4项PASS；`local-producer-compatibility-tests.log`。 |
| 静态和差异检查 | 6个Python AST、Bash语法、PyYAML解析修改workflow、定向diff空白和补丁reverse-check PASS；actionlint/shellcheck未运行。没有重跑未变的前端2379项、应用三库矩阵或真实RC/恢复验收。 |

31项Linux定向方法全部PASS，均属本地控制/替身合同验证。原历史CI、exact RC/schema-only启动和合成恢复CI继续只按原范围有效。原`inventory.sha256.json`及`continuation-inventory.sha256.json`不改写；更新前RESULT原字节另存`RESULT.before-local-repair.md`，本轮源/补丁/日志hash记录在`local-repair-receipt.json`。

**剩余最小真实阻塞**：需用户另行明确批准一次有界生产只读诊断，范围仅该失败backup invocation的脱敏首个错误/phase、准确ExecStart和helper版本摘要，同CID Gateway的State.Error/StartedAt/FinishedAt、非秘密entrypoint与runtime/secret挂载元数据。不得输出Env、secret内容或带凭据参数，不启动/重启任何服务、容器或workflow。本轮未连接，不要求用户重新提供已有文件。获得诊断后才能确定现场工具修复的准确对象；真实备份+独立恢复/旧镜像回退和failed/isolated→off模式合同还须分别闭环。f93f不能自动取得未来main/control资格；PG03、公网、生产配置/数据和自动开关均未变更，现场新鲜性UNKNOWN。

---

## 续办核查（最新权威；2026-10-21，仅本地）

**本轮调查完成；安全非 PG03 生产发布仍 BLOCKED。** 目标保持 source `75b0f09e2a527c5173c1179a6efe697de4f64f39` / RC `39619413994/1` / image `sha256:f93f02ea737721e621681771424d3b0fca4fd8d1c8d6e3b7804097b793fa5b06`。本节取代下文对“下一步”的描述，下文保留为上一轮原始回执。没有连接生产、SSH、启动/重启容器、触发 workflow、调整生产配置/数据/PG03状态、提交/推送或安装依赖。没有将旧现场快照写成新鲜生产事实；**当前远端 main、自动开关、现场状态新鲜性 UNKNOWN**。

按备份→启动恢复→自动链顺序检查。证据与源码字节记录：`continuation-local-audit.json`；可审阅的离线探针：`continuation-local-audit.py`。原始28项 `inventory.sha256.json` 在更新前全部匹配；原回执原字节保存于 `RESULT.before-continuation.md`，其摘要仍等于原 inventory 的 `RESULT.md` 条目。该 inventory 是历史快照，不应重写为本轮清单。

### 1. 备份失败与可用恢复点

| 判定 | 已核实事实及边界 |
| --- | --- |
| PASS（历史资产身份） | `production-rollback-readonly.json` 的观察时间为 2026-10-21 22:55:48 上海时间。备份 `20261020T002138Z` 的 manifest SHA256 `a9b9c32cd168c5a68740e20f1c949cff41da665b14d7aeb5790404dd0d795166`、密文 SHA256 `0c8757c8d204cddd7cc5b3b9a7835823e376093b9ee344f5a8bd8c219d3b3b31` / 12323351 bytes及旧镜像 `sha256:51ddf6df…c863d24a5` 在场记录可复用。这里只在本地复核留存证据，未重新访问服务器。 |
| BLOCKED（备份服务） | 同记录保存 `Result=exit-code / ExecMainStatus=1 / ActiveState=failed`，最后退出 2026-10-20 19:31:16 UTC；timer active/enabled 不能证明产生过成功恢复点。 |
| PASS（失败机制候选的源码定位） | 只读检查 `39619582548/1` 记录已安装 backup script SHA256 `0d70b1e1228827d3f275e2e56b15772f560dfc95d9be10bdb9ad0992d1726023`，本轮证明它精确匹配事故 source `c50a53c1…` 的 Git 原字节；它在 dump 前调用默认 running identity capture，且不支持 `CLEARAPI_BACKUP_STOPPED_GATEWAY`。当前候选脚本为 `f6aa450c…`，不能把本地新增能力当作现场已有能力。 |
| UNKNOWN（本次 exit1 的确切根因） | 目前检查的证据没有该次 invocation 的 journal、backup.log诊断、准确 ExecStart/非秘密条件或已安装 identity helper 完整字节绑定。Gateway 已退出与默认 running 契约冲突，是**有代码支持的假设**，不是已证实的19:31:16故障原因；也未排除路径、权限、归档、加密、存储或 cleanup 失败。 |
| BLOCKED（停止态备份） | 候选 `backup-identity.py:21–97` 的停止态契约要求准确CID、exited、restart=`no`、双failed清单、其他Gateway writer不存在；已存现场 restart=`unless-stopped`，因此即使换上新脚本也不能直接调用停止态备份。本地合成探针证实默认无running容器会拒绝、停止态对unless-stopped会拒绝。未改变restart策略或启动Gateway来满足契约。 |
| BLOCKED / UNKNOWN（真实恢复） | 已检查材料没有该准确备份的解密成功、源/恢复完整数据与schema/index/constraint/sequence对比、旧镜像+运行配置启动/身份验证及实际数据回退回执。`pg03-offline-restore-ci-39578824035.json`明确 `production_backup_used=false`，只可复用为合成恢复工具验证。旧备份不能据其ID推断精确created_at或当前数据新鲜性；现行恢复审计还要求真实manifest创建时间距验收不超过86400秒、v2停止态身份及completion receipt（`scripts/pg03-restore-acceptance.py:284–300`），这些材料未取得，不能倒填时间或把旧备份改签成当前恢复点。 |

**最小下一步**：先由管理员提供/另行批准采集该失败 invocation 的脱敏诊断和固定元数据，确认失败发生在哪一步；在此之前不重跑backup service、不reset-failed、不升级backup脚本。随后才可能在另批全writer受控窗口内产生当前准确备份、证明真实exit0和明文staging清理，交给专用离线环境完成真实恢复与旧镜像回退验收。只传递非秘密hash、时间、状态和聚合比较结果；本任务不取得私钥、明文业务行或生产凭据。

### 2. 启动资产与 exit127 的实际证据

- **PASS（历史诊断支持）**：`../boot-recovery-preflight-followup-20261020T074134Z.json:21–35` 对同一Gateway `f8f963a7…` 明确记录 `missing_runtime_mount_error=true`、exit127；同日独立review也记录缺少`/run/clearapi`的mount error。`scripts/pg03-failed-recovery.py:170–185` 的准入同时核对精确旧CID/image、exit127、非OOM、`State.Error`含 `open /run/clearapi: no such file or directory`、准确只读bind mount；**这是源码接受条件，不是新采集的原始错误日志**。本地探针证实仅exit127及 `/new-api` 不存在错误不能通过此恢复入口。
- **PASS（后续缓存事实）**：`production-readonly-v2.json` 于2026-10-21 22:50 上海时间再次记录同CID/image、exited127、host `/run/clearapi`不存在、bind source和destination均`/run/clearapi`且RW=false。此记录未收集完整`State.Error`、StartedAt/FinishedAt、最终Path/Args或effective Compose entrypoint。历史挂载诊断可解释启动前失败，不能据此断言本次新增了二进制缺失、动态链接器缺失或脚本解释器故障。**最新精确OCI错误和进程是否曾进入entrypoint仍 UNKNOWN**。
- **PASS（候选依赖定位）**：候选Dockerfile使用CGO_ENABLED=0，复制`/new-api`和`/entrypoint-secure.sh`、chmod0755，image默认ENTRYPOINT为`/new-api`（`Dockerfile:12–13,54–64`）；仓库生产Compose模板使用`[/entrypoint-secure.sh,/new-api]`，launcher通过`/bin/sh`读取`/run/secrets`后exec（`scripts/production-entrypoint.sh:1–37`）。新RC已有exact digest拉取/binary roundtrip与两次schema-only启动成功证据，复用即可；它不证明生产容器的最终entrypoint/secret mounts已正确。
- **BLOCKED（host runtime路径与启动顺序）**：生产缓存mount是volatile `/run/clearapi`；仓库`docker-compose.production.yml:91–97`模板却是durable `/opt/clearapi/runtime/new-api`→容器`/run/clearapi`且`create_host_path=false`。两者不能混称。当前非PG03 overlay只固定image和两个false，不改变mount，因此不会自动修复缺失目录。`runtime-identity-refresh.py:40–46,88`要求精确只读mount、running容器及该目录内身份文件；单纯创建空目录仍不能证明Runtime通过。
- **BLOCKED（恢复资产兼容）**：缺少boot/recovery两helper、tmpfiles及三项systemd资产；预期顺序为tmpfiles准备root:root0750目录→Docker前目录校验→Docker后身份reconcile。候选image不包含host systemd安装。`../boot-install-compatibility-stop-20261020T075513Z.json`记录create-only安装尚未开始：已安装V2/control/legacy/schema依赖与获审恢复source不匹配；旧wrapper无recovery入口。后续只读检查也确认V2/control/backup三项不匹配。**只补七项boot资产不能修复整条恢复契约**，不能静默扩大旧安装清单、套用旧RC授权或绕过failed/isolated状态。

**最小下一步**：在备份诊断之后，由管理员提供同一CID的脱敏 `State.{Status,Error,ExitCode,OOMKilled,StartedAt,FinishedAt}`、不含凭据的Path/entrypoint/命令路径、runtime/secret **挂载元数据**与host目录权限/存在性、准确effective Compose相关非秘密投影；不输出Env/secret内容或带凭据参数。不启动容器进行探测。本轮不安装helper、不执行tmpfiles/systemctl/reconcile/recover、不改变mount。真正恢复启动会执行迁移和后台写入，必须先有真实恢复/回滚能力及明确现场写入授权。

### 3. 自动链为何强制 PG03；最小安全 off 方案

**PASS（静态原因定位）；BLOCKED（可执行off路径尚不存在）**。本轮将16个相关工作区文件与候选Git原字节比较，CRLF规范化后全部一致，避免把既有脏工作区的其他control修改冒充此候选。具体限制：

1. `deploy-production.yml:163–185`在proof认证后硬写`pool_mode=pg03`；`automatic-deploy.yml`调用同一reusable workflow。`automatic-release.py:24–28,145–238`的checks与executor身份验证无off分支，且proof未绑定pool_mode。`assemble-release-proof.py:12–30,94–119`同样无mode参数、强制Executor字段/PG03 Provider检查；rehearsal执行这些gate（`deployment-rehearsal.yml:165–210`）。增加`pool_mode:off`字段不会取消Executor依赖，本地探针实际收到`KeyError('executor')`，未发起远程调用。
2. `deploy-production.yml:241–245`的off分支还显式要求`REHEARSAL_RUN_ID`为空；因此只改硬编码为off仍会在deploy阶段拒绝合法自动proof。当前controlled入口也只允许PG03，不能用其延后Provider验收语义冒充已完成普通业务线上验收。
3. `production-deploy.sh:823`要求健康running identity/Runtime baseline；`1202–1203`要求所有Executor/route-chain容器为0（包括停止容器），`1248–1249`要求所有匹配pool option rows为0。现场保留运行Executor，且用户要求不改变PG03生产状态，因此手动off同样不可直接使用。不能通过删除容器/配置行或移除gate使它通过。
4. V2 `pg03_configure()`（`production-deploy-v2.sh:1010–1021`）明确拒绝current manifest为PG03时未经批准的off退出；controlled hold也有独立拒绝条件（`1346–1354`）。旧PG03失败清单、事故隔离和PG03开发成果必须保留，不能把它们改写成普通健康off基线。

**最小安全改动边界（设计结论，未实施）**：复用现有rehearsal→proof→authorizer→deploy链，在同一合同中显式绑定枚举`pool_mode=off|pg03`，缺失/未知/交叉mode一律拒绝。mode必须绑定producer/receipt/source/control/RC attempt/digest和现场前置/后置断言，不能来自可任改的输入或孤立false常量。off仅替换两项PG03专用验收：Executor配对和Gateway→Executor→Provider链；改为可核验的普通Channel1→Provider usage/扣费/拒绝与重复请求证据及关闭pool的实际运行断言。PG03模式的原有checks保持完整。

共同门禁继续必需：可信producer、当前main及CI/RC/source/OCI/binary身份、artifact完整性、迁移/三库与重复启动、Runtime、业务计费、重放/重复扣费隔离、准确backup+真实restore、safe_rollback、前任身份、环境排队后复核和事故熔断。off需要证明管理开关false、调度master有效false、无启用pool source/route/bound channel/chain/candidate；不得自动改写现有模型来源。现场Executor及PG03数据/资产保留，以真实不可调度/不可走pool路由的证据替换旧“必须不存在”假设；这项合同变更必须定向验证，不能直接删除存在性gate。若涉及由failed PG03清单恢复到off，必须增加明确、一次性、获批的恢复/模式转换合同，保留旧failed/isolated审计，不复用手动off或旧七资产安装批准。

至少需要mode错配/缺失、伪造关闭、残留可调度pool路径、保留但不可用Executor、无真实restore/rollback、旧proof/RC attempt、main变化及失败事故重入的负向回归；不得重跑未受影响的2379前端及已通过三库矩阵来代替这些缺口。源码/控制main变化后重新核实本候选资格；**不得将现有f93f候选视为自动符合未来current-main**。本轮没有修改workflow/control/application实现，也未打开自动开关或清除circuit。

### 本轮实际验证与停止点

- `python -B evidence/non-pg03-20261021/continuation-local-audit.py`：exit0，5项定向合成probe PASS；新RC ZIP SHA及四项payload inventory/manifest-schema一致性PASS；16个相关源码与精确candidate一致（规范化CRLF），旧backup脚本来源精确匹配事故Git blob。全部是本地字节/契约验证。
- 原28项证据inventory全部PASS；旧CI、exact RC启动及合成恢复CI结果按其原范围复用，未重跑前端/三库/旧36或92项测试。真实生产restore、旧镜像回退、恢复启动、线上业务验收 **NOT RUN / BLOCKED**；现场/远端当前状态 **UNKNOWN**。actionlint/shellcheck未运行，本轮未修改对应实现。
- 只读检索中，直接向rg传递Windows通配路径出现os error123，猜测的`test-pg03-failed-recovery.py`路径不存在；改为明确路径/rg glob及直接检查既有guard。两者均未执行生产操作或测试，也未作为“证据缺失/测试失败”结论的依据。
- 下一步最小请求仅是**管理员脱敏证据包/受限生产只读采集授权**：先失败backup invocation，随后同CID的完整mount/runtime诊断及准确工具版本；不得将此授权解释为备份重跑、容器操作、安装或部署授权。本轮已抵达需要生产证据的边界，停止，不发送新提示词或创建新工作流。之后仍须分别批准受控备份/离线恢复、兼容资产修复及一次性off恢复/发布；**PG03生产状态不变**。

---

结论：**新候选及本轮 CI 验证完成；生产发布 BLOCKED，未上线。** 用户指定的“回滚条件不足则停止生产变更”已触发。没有启动/替换 Gateway、停止 Executor、修改生产配置或数据库、安装工具、改动 Caddy、公网恢复或触发 Deploy Production。PG03 成果与全部历史失败证据保留。

## 本轮精确身份

- 远端 main：`75b0f09e2a527c5173c1179a6efe697de4f64f39`，开始与收尾均经 GitHub API 核实。工作树 HEAD 是该 main 的第二父提交 `f2e179ebc8e2d0f29b06603376a49a1058f9d496`，与 main 的 tracked 内容没有差异；已有未提交修改未用于构建、未清理。
- 新 CI：[39619405764/1](https://github.com/a3225865526-wq/Clear-API/actions/runs/39619405764)，SUCCESS。7 jobs SUCCESS；仅限定 pull_request 的 PG03 package-read job 按 workflow 条件 SKIPPED，不能说八项全部通过。
- 新 RC：[39619413994/1](https://github.com/a3225865526-wq/Clear-API/actions/runs/39619413994)，SUCCESS。在审阅当前 11 个 workflow 的精确 main 字节和触发链后，通过既有 `release-pg03-gateway.yml` 的手动入口从当前 main 重新构建。workflow 名称及 OCI version 保留 PG03 字样；它是 Gateway 构建入口，不是启用账号池的操作。
- 新镜像：`ghcr.io/a3225865526-wq/clearapi@sha256:f93f02ea737721e621681771424d3b0fca4fd8d1c8d6e3b7804097b793fa5b06`。
- binary SHA256：`37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5`。构建使用精确 checkout、build context 清洁检查、新 OCI revision、registry digest 拉取及 binary roundtrip。应用代码未变时二进制摘要相同是允许的；本次不是把旧 digest 改名为新候选。
- Artifact `12353708716`；原 ZIP SHA256 `a4eb052737e76a0bbc561456419b6eb73ed0a7e30d0e52363b4d748093c785af` 与 GitHub API digest 一致；四项 payload inventory、source/candidate/OCI/workflow commit、Dockerfile hash 和 schema receipt 均已本地核验。原件 `rc-original.zip`、`rc/`；回执 `candidate-verification.json`。
- RC manifest 仍为 `production_ready=false / production_acceptance=not_run`。本机额外 pull 被现有 Docker registry 凭据 scope 拒绝，没有读取、导出或更换 Token；镜像获取及二进制核验的实际成功证据来自本轮 RC runner，未声称本机镜像启动通过。

## PG03 关闭与普通服务依赖

- `common/pool.go`：`CLEARAPI_POOL_ENABLED=false` 关闭 owner 管理入口；`middleware/pool.go` 在关闭时返回 404。**该环境变量单独不等于关闭调度**。
- `model/pool_master.go`：调度 master 只接受精确字符串 `true`；缺省、false、非法值均关闭。`model/pool_routing.go` 在 master 关闭时拒绝 pool 来源模型；普通未配置模型或 `source=api` 的非 pool 渠道独立通过。已有 `source=pool` 的模型不会自动回落 API，不能通过改来源来隐式绕过既有策略。
- `service/accountpool/policy_snapshot.go` 对非 pool 绑定渠道提前返回，不要求 PG03 签名密钥或 Executor。`main.go` 初始化不以 Executor 健康为前提；普通用户、API Key、钱包/计费及普通路由不要求开启 pool master。普通路由仍使用保留的准入与路由链代码，并非删除 PG03 模块。
- 2026-10-21 22:50–22:54 Asia/Shanghai，生产 `BEGIN READ ONLY` 查询：master 行不存在（代码有效值 false），pool route=0、pool source=0、pool-bound channel=0、enabled chain=0、enabled pool candidate=0。Channel 1/type 1/status 1，唯一启用 ability=1；Channel 2/3/status 2，无启用 ability。没有更改渠道或其数据。
- 生产 Gateway 容器原配置仍是 `CLEARAPI_POOL_ENABLED=true`；容器处于退出状态，未把它谎报为已改 false。`compose.non-pg03.candidate.yaml` 是**尚未安装的审核配置**，精确固定新 digest，并显式设 `CLEARAPI_POOL_ENABLED=false`、`CLEARAPI_POOL_CLIPROXY_DEDICATED=false`。使用前必须重新验证有效 master=false，不能只验证该环境变量。现有 Executor 保持运行，本轮未部署或停止它。
- pool schema 迁移、普通启动清理及后台账务任务不会因关闭管理入口而跳过。现有数据与 pool 表/配置应保留，不删除记录来满足关闭条件。

## 本轮验证与边界

| 检查 | 命令/实际结果 |
| --- | --- |
| 普通业务 HTTP 生命周期，真实三库 | 本轮 CI：`go test -race -count=1 -timeout=5m -v ./e2e/commercial`；SQLite 3.50.4、MySQL 8.4.6、PostgreSQL 17.6 全部 PASS。覆盖注册、登录、admin、API Key、兑换与重放、定价、usage、精确扣费、拒绝退款、零余额阻断。上游为本地合成 provider，不是生产商业上游验收 |
| 迁移错误与重复启动 | 本轮 CI：`go test -count=1 -timeout=5m -v ./internal/migrationproof ./model -run 'TestMigrationProof|TestRetiredOptionsCannotPublishCompletion'`；三库 PASS。MySQL/PostgreSQL 同主日志库和独立日志库均两次启动，SQLite 同库两次启动；原始日志 `mysql-ci.log`、`postgres-ci.log`、`sqlite-ci.log` |
| 精确候选关闭 pool 启动 | 新 RC 使用 `scripts/pg03-schema-rehearsal.py`，显式 `CLEARAPI_POOL_ENABLED=false`，独立 PG17.6、network none/container namespace、tmpfs，无 Executor，schema-only 恢复后同库两次启动/健康/正常退出成功。该 RC 步骤未要求 challenge 迁移完成凭证；它不是生产业务数据升级、恢复或全启动写入验证 |
| 生产 schema 只读核对 | 新鲜 schema-only dump 在服务器内计算规范化 hash，仅回传摘要 `3c1c77f355a2f9376f7cd4133267c99856cd4905b6116ad2ba615dbf84305035`，与新 RC target 相同。没有导出生产行。相同 DDL 不证明数据回填、后台写入和真实回滚通过 |
| 后端及 relaykit | 本轮 CI 的 root/relaykit build/test、独立 relaykit build、migration binary build SUCCESS；backend baseline-aware test 实际退出码 0，没有借 baseline 接受失败 |
| 前端 | 本轮 CI `bun run test`：220 files / 2379 tests PASS，production build SUCCESS。job 名含 typecheck，但 workflow 未运行独立 tsgo，不声称独立 typecheck PASS |
| 本地部署护栏 | WSL：`python3 -B scripts/ci/test-production-deploy.py` 92 项，91 PASS / 1 SKIP（未请求 real producer artifact）；`python3 -B scripts/ci/test-automatic-release.py` 36 PASS。均为隔离替身/合同验证，不是现场恢复 |
| 关闭配置 | `docker compose -f evidence/non-pg03-20261021/compose.non-pg03.candidate.yaml config --format json` 成功，精确 digest 与两个 false 一致；只解析候选 overlay，没有调用生产 compose |
| 线上 | 本轮首页与 `/api/status` HTTPS 均 521，TLS verify result 0；服务器 localhost 3000/80 均连接失败。登录/API Key/实际扣费/真实上游请求等线上功能验收 NOT RUN |

认证验证按 [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) 及 [ASVS 5.0.0 stable](https://owasp.org/projects/asvs) 检查范围约束：服务端认证/授权、会话校验及传输、失败拒绝、重放和敏感信息排除。未修改认证流程，也未执行完整 ASVS 审计，不声明 ASVS 合规或所有认证控制已验收。

## 真实生产状态与停止依据

- 原 Windows OpenSSH 私钥路径只检查存在和 ACL，复用原 known_hosts 严格校验；clearadmin 与原 sudo 权限确认成功。未读取或输出私钥内容，未创建新身份。
- 新鲜现场 Gateway：`f8f963a7…`，实际 image/digest `sha256:22ecaa468c784cd41ee2604dd8f3ac189960744516a508fc8c9cee3ee1724e02`，OCI source `c50a53c1b89e121bcade7dac41d3f11e53a9832a`，Exited 127。**当前没有运行中的网站 Gateway**，该 digest 是现存停止容器的实际镜像。
- PostgreSQL17.6 与 Executor `sha256:cfad1b51928813cc6b932deb31adc71d5d23ae76fd2ea9d899bc740d1a06ae58` 运行；Caddy `54c8550b…` exited 0 / restart=no，隔离保持。5 个用户、6 个 token 的聚合只读核查，用户及 token 负余额计数均 0；没有读取用户身份或凭据。
- `/run/clearapi`、boot helper/recovery helper、tmpfiles 与三个 systemd 资产仍缺失。不能仅替换候选绕过启动身份校验。
- 生产发布清单 current=上述失败 c50a/22ec、status failed、pool_mode pg03；previous=eb67/51dd、status success、pool_mode off。controlled-launch journal 仍为 isolated，target 要求 public_traffic blocked，历史 edge 快照按隔离前状态保留。
- 官方只读检查 [39619582548/1](https://github.com/a3225865526-wq/Clear-API/actions/runs/39619582548) 因 wrapper/control/backup script 三项与当前 main 不同而 FAILURE；未调用 Runtime。管理员另行确认旧 `production-deploy.sh` 的 hash 与新 RC 的 off wrapper 相同，但它要求现有健康 Gateway/Runtime baseline，且末尾要求所有 pool option row 为 0；不能拿它跳过事故隔离/恢复或清除已保存 pool 配置。当前恰好无 pool 配置行也不能弥补健康/恢复条件。
- 新鲜备份检查：timer active/enabled，但 backup service **Result=exit-code / ExecMainStatus=1 / ActiveState=failed**，最后退出为 2026-10-20 19:31:16 UTC。没有清除失败、启动新备份或把 timer active 当备份成功。
- 原备份 `20261020T002138Z` manifest SHA `a9b9c32c…`、加密件 SHA `0c8757c8…` 与既有资产相同，旧 image `51ddf6df…` 在场。**没有解密/恢复，生产数据+镜像的真实恢复仍未验证**；本轮 CI 合成数据库恢复不能替代它。
- 最近真实部署失败 [39347012653](https://github.com/a3225865526-wq/Clear-API/actions/runs/39347012653) 的原始日志新鲜回读：完成替换及 localhost health 后，被 Gateway→Executor authenticated health 拒绝，未自动回滚。投影保存在 `latest-deployment-failure-projection.log`，不改写历史失败。
- 首次只读采集遇到无 Health 字段的 Go template 失败与误用 ORM 字段名 `other_settings` 的 SQL 错误。修正探针后以 `production-readonly-v2.json`、独立 DB/route receipt 得到实际结果；最初失败 JSON 保留，未把失败查询当作数据缺失。

## 真实自动发布链与剩余最短路径

1. 当前 chain 是 CI(push) → `PG03 reviewed main Gateway RC` → `Deployment rehearsal` → `Automatic production release` → `deploy-production.yml`。自动 authorize 硬写 `pool_mode=pg03`，proof 强制 Executor 配对和 Gateway→Executor→Provider usage/billing。**当前自动流程没有可直接用于非 PG03 的 release proof/自动分支**。手动 off 入口存在，但不是自动 off 上线就绪证明。
2. 两个 `CLEARAPI_AUTO_*_ENABLED` 开关在首尾均 false；main strict required checks/enforce_admins、production branch policy 保留。收尾自动下游均 SKIPPED；没有打开开关、削弱门禁或 dispatch 生产部署。
3. 生产前必须先排除备份失败，完成准确备份的真实隔离恢复/数据及镜像回退验收，再按现有受控恢复流程闭环 boot 资产和失败 baseline。当前回滚不足已触发用户停止条件，本轮到此不进行生产写入。
4. 恢复条件具备后，以最小 mode-aware 自动流程承载 off：关闭 pool 的实际状态断言、当前 main/新 RC/CI/镜像/迁移/备份恢复/普通渠道 usage-billing/rollback 证据继续必需；只在 off 分支不要求 PG03 专有 Executor/provider 路径，不能用手动入口绕过失效熔断或删除隔离状态。若源码/控制 main 改变，必须重新核实当前 main 门禁及候选资格，不能直接声称本候选仍满足未来 current-main。
5. 生产切换之后才能进行真实用户系统、API Key、余额与扣费、Channel 1 普通模型请求、首页/静态资源/TLS 和错误路径验收。现有未启用渠道不应为验收而自动启用。
6. PG03 后续独立处理候选资格与指定 control 的审查问题、全 writer 观察窗口、真实备份恢复、生产 adapter 现场验收与 Gateway→Executor Provider 计费链。没有将这些开发工作删除，也没有因为它们暂缓而放弃普通服务的数据与恢复门禁。

本轮只修改总控和本目录准备/证据文件，未提交或推送源码。报告中的未来步骤是阻塞解除条件，不表示已执行或已上线。
