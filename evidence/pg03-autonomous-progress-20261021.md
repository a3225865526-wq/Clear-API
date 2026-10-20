# PG03 自主多阶段推进 — 2026-10-21

任务范围：负责人授权本地修复、CI、功能分支/提交/推送/PR，以及满足全部门禁后的独立工作流注册和隔离运行；仍禁止任何生产 SSH、配置/数据/容器、发布和公网操作。历史停止指令由本轮授权取代，历史证据不改写。

## 阶段 A/B — 进行中

已用 driver 层观察替换“所有 Row/Rows 都拒绝”的过渡护栏。观察真实查询、事务、准备语句、结果 Next/Close 和 context 取消错误；保留 GORM Trace 与 Row.Err 初始错误回调，以及非 SQL best-effort 错误锁存。默认启动不启用观察连接；ClickHouse proof 明确拒绝。没有更改 Go 依赖版本或锁文件。

定向测试已增加：正常读取允许完成、driver 读取/关闭/提交延迟失败、读取后 context 取消、closed pool 吞错；CI 三库中使用专用 pg03_proof_main/log 做真实迁移、连续两次启动、合成数据保留及日志库失败凭证否决。测试不能使用生产 DSN，显式限定 loopback 与固定测试库名。

独立子代理 driver_proof_review 已读 billing gate 并核对固定 GORM/MySQL/SQLite/pgx 接口，未发现阻止合入的源码或新增 CI 安全缺陷；该结论不代替真实测试。

Windows 便携 Go1.26.1 来自官方归档，SHA256 9b68112c913f45b7aebbf13c036721264bbba7e03a642f8f7490c561eebd1ecc；Linux 归档 SHA256 031f088e5d955bab8657ede27ad4e3bc5b7c1ba281f05f245bcc304f327c987a。归档已按官方清单校验，仅解压任务目录，不改系统 PATH；恢复 go.sum 固定模块到任务缓存，没有版本升级。9 个改动 Go 文件已实际 gofmt。

实际命令：便携 go test -mod=readonly -count=1 ./internal/migrationproof ./model -run 'TestMigrationProof|TestRetiredOptionsCannotPublishCompletion|TestMigrateRetiredFrontendOptions|TestGormLoggerEndToEndSanitizedOutput'，Windows exit 0，两包 OK（Linux 专用持久化测试有平台跳过）。Linux 同一定向命令正在执行，尚不记录通过。Python 集中离线回归 51 项 OK（Windows 42 通过、9 skip）；git diff --check exit 0，仅既有换行提示。

当前远端 main 69a66431646b5cc8912bf97a739e08de07316443；两项自动发布开关实际读取为 false，保留三项 strict required checks、enforce_admins 和当前审查规则。9 份 workflow 的最新固定触发图取证另存 pg03-autonomous-preflight-20261021.json。新 package-read CI job 仅同仓 owner PR、packages:read/contents:none，只读取历史完整 digest、无 checkout/容器启动/发布；它不是新源码候选验收。

本地 origin 指向 QuantumNous/new-api，production 指向用户指定 Clear-API。已明确从 Clear-API fetch main，核实其 tree 与原本地 HEAD 相同后，在当前工作区创建 codex/pg03-migration-proof-20261021 分支，保留所有未提交改动。没有创建额外 worktree。

P2-1 继续 Linux 本地 PASS；P2-2 正向实现待实际 Linux/三库验证，PG03 仍 BLOCKED。新 RC/镜像尚未构建；真实隔离重跑轮次 0/2。生产操作未执行。

## 第一份 PR / CI 实际记录

已创建草稿 PR #78，首次提交 ac3996b10858457d28e7dcb64ddc3c439556a3bd，正常 CI run 39465768745 正在执行。新增 packages:read/contents:none 能力 job 已 success，证明该次同仓 token 可读取历史完整 digest；仍不等于新候选验收。必需检查未全部完成，不合并。

GitHub workflow validation run 39465661995 失败（未执行 job）：job-level env 不允许 runner.temp 上下文。已把目录绑定移到 runner 分配后的首个 shell step，通过 GITHUB_ENV 传递；新增静态断言禁止 job env 使用 runner 上下文。此项为注册前配置修复，不是真实演练，演练修复重跑仍 0/2。

WSL 临时目录在后续调用中不可用，改稳定任务目录后 tar 提取在 DrvFS 慢 I/O；中断该提取并保留半成品，改由 Windows 原生提取已验 hash 的 Linux SDK。此前两次失败发生于环境准备，未执行 Go 测试。新的 Linux 定向进程已启动，尚不声称通过。

## 首次真实三库结果及定向修复

CI 39466058635：MySQL 三库新增步骤 success；SQLite 在 InitDB 返回 sqlite: invalid driver.Value type int；PostgreSQL 实际17.6完成首次shared迁移，但凭证被锁存失败。均保留失败，未记PASS。

已按固定依赖和 Go 标准库定位两项兼容性：Go1.25 的 database/sql 对 NumInput=-1 且存在 ColumnConverter 的路径会跳过默认转换，包装层现在显式保留无原生 converter 驱动的默认参数转换；pgx Exec 返回 driver.RowsAffected，LastInsertId 是不支持的可选能力，GORM 在 RETURNING 模式会正常忽略该能力错误，不能把它当SQL执行失败。结果能力保持原样返回，SQL执行、Trace、Row.Err、延迟读取/关闭、事务错误观察继续保留。集中 driver 夹具增加原生 int 转换和“不支持 LastInsertId 仍为健康执行”回归，随后重跑实际矩阵。

Linux 本地 Go setup 曾因 WSL 网络无法下载两个固定 Linux 依赖失败；通过 Windows 网络恢复同一 go.sum 版本并执行 Linux -deps 加载，增加固定 numcpus 依赖缓存。没有改版本或锁文件；再次验证使用 GOPROXY=off，避免重复网络超时。尚未将该次执行记录为通过。

## 新矩阵结果与既有集成夹具修正

9ef82f0ac5b359fda2246268a49dae6bfa8f56ca / CI39467424629：真实 SQLite、MySQL、PostgreSQL 矩阵 job 全部 success，包读取能力与前端 success。Linux 定向两包 Go exit0，SQLite3.50.4 真实重复启动通过。独立审查对本次默认参数转换及 Result 能力转发复核通过。

Backend required job 在既有 test-legacy-transition.py 的 schema 夹具失败：共享 helper 现先创建受所有权清理保护的 binary probe，并在两次启动前分别取 schema/启动后核对 process，旧夹具仍模拟旧 run 路径。更新夹具保留原真实合同（首次 schema 升级、第二次幂等、失败不发布与全部清理），Linux 单个相关用例 exit0（内部3场景）。不修改门禁降低要求。完整 CI 需在该差异后重新通过。

## 阶段D安全本地准备

新增停止态Gateway备份模式（默认running不变）、保留历史策略和 successor_completion 校验，详见 pg03-successor-production-readiness-20261021.md。WSL定向验证6个独立测试方法通过：两项停止态身份拒绝/漂移、新停止态真实GPG合成备份/完成校验、EXIT清理失败即使legacy marker存在也拒绝、默认running全加密备份、历史保留。测试中Docker/dump/TOC为替身，未验真实数据库恢复。完成校验覆盖未知/布尔/非零退出、残留staging、密文篡改、hash时manifest原地修改/cipher路径替换、staging父链接。

独立审查先发现文件稳定性、路径和inventory缺口，已修复并复核通过。完整生产successor仍缺新鲜生产配置/写入范围、停止态全writer独占证明、真实备份恢复和新的生产一次性审批协议，不能把该本地组件标成完整执行器或生产PASS。现有生产身份配置清单已复用，无生产SSH或账号/权限修改。

CI package-read同时在同仓main push运行，防止source CI的skipped job触发既有release-control拒绝；权限仍contents:none/packages:read，只pull旧完整digest，无容器启动/发布。与旧schema夹具修复均经独立复核通过。

## CI39469656112结果与备份测试执行身份修正

f23c6839777214c3beae1be46e2f5ed30bbc54a4：Backend、Frontend、三库及包读取均success；Secret scan job在26个备份测试的新增stopped全备份完成校验失败（unsafe backup directory）。该校验刻意要求根目录链由当前root持有，本地WSL root执行通过，而原CI以runner非root调用；不放宽生产文件资格，改该备份测试命令为sudo，与既有部署/Runtime安全文件测试一致。其余检查和断言完整保留，最新head仍须正常CI通过。

## PR78正常合并及注册

CI39509230715/1a57f501a9414abfdaaf3c865e90bf4501600ff2全部7个job success。新鲜 premerge-boundary-20261021T2242Z 取证九workflow原字节/两false开关/strict三required checks/enforce admins，required approving reviews=0；ready后使用--merge --match-head-commit正常合并，未admin bypass。PR78 merge799a608d1b5a79df0f2ef8031a7885f414125778，2026-10-20T22:44:15Z（北京时间21日06:44）。已注册新workflow，不运行旧固定RC。当前main push CI39510261549进行中，新RC尚未触发；postmerge-rc-boundary记录10份workflow与同样false/strict控制。

当前工作区在同一目录创建后续控制分支 codex/pg03-exact-rc-acceptance-20261021，保留未提交/未跟踪旧工作。备份26项Linux root回归exit0；新增successor kernel及reader9方法通过，独立审查先发现lease尾部/重启证据时序/布尔类型，均修复并复核；另保护请求对象与基线不被adapter误改。测试是合成IO/实际临时文件持久化，不是生产执行器或真实超时截止验证，实际adapter仍NOT IMPLEMENTED。


## 新RC实际构建与控制验收增量

main push CI39510261549全部7项SUCCESS后，按新鲜false开关/严格保护/触发图，仅手动dispatch release-pg03-gateway.yml，RC39511197302/1 SUCCESS。source799a608d1b5a79df0f2ef8031a7885f414125778，完整镜像sha256:729fdd37591fb333c9cefe202d7c2b7ff4486df31a2689a9b2152abdf7f6965f，binary37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5。原始ZIP25194bytes/SHA b02e4189c742e50f2d62312dcb80b60033380ae00c3f442bda2990b948e358a7与GitHub artifact12305532882 digest一致，5个受限条目/4个payload hash及source/OCI/CI/schema/binary核验通过。原始ZIP及verification清单保留。

RC的真实pull/legacy双启动已成功，但未启用新challenge凭证，不当专用P2-2验收。专用控制脚本改固定新身份；增加空白PostgreSQL17.6只读库（SHOW实际on）探测真实SQLSTATE25006、非零明确退出、无OOM/Docker错误、无成功凭证资格及独立两容器清理。负向日志只证明迁移拒绝，tmpfs退出后凭证不可采集明确记unknown；不是成功依据。finalize和回读同时要求负向完整合格。

实际命令：WSL root python3 scripts/ci/test-pg03-successor-plan.py，62项全PASS/exit0；Windows python scripts/ci/test-pg03-successor-plan.py IsolatedActionsTest MigrationEvidenceTest SuccessorRequestTest，24项中22PASS/2 POSIX skip/exit0。新RC固定后发现4个旧夹具失败，按legacy source语义修复3处，不改旧日志/凭证拒绝合同，重跑上述受影响回归通过。三份Python AST及git diff --check exit0。successor CI改sudo运行，确保9项root kernel方法真实执行；不把替身IO视作生产操作或实际超时中断。

独立driver_proof_review复核当前固定值/负向隔离与清理/finalize及回读/CI身份/legacy夹具，未发现阻断缺陷；未代跑容器/测试。successor_readiness_review复核kernel和完成reader通过；生产adapter、只读完整投影和连续全writer门禁仍NOT IMPLEMENTED，不能声称只剩部署审批。真实专用演练0次、修复重跑0/2。
