# PG03 Production Readiness — 开发交付及正式部署待批包

日期：2026-10-21（Asia/Shanghai）。**生产就绪 BLOCKED；正式部署 NOT RUN。** 本文件不签发任何生产权限，所有历史失败和既有工作区修改保留。最终验证与远端集成记录将在本节补充。

## 1. 候选与既有验收

| 对象 | 精确身份 / 结论 |
| --- | --- |
| 仓库 | a3225865526-wq/Clear-API |
| 应用 source | 799a608d1b5a79df0f2ef8031a7885f414125778 |
| RC | 39511197302 / attempt 1，既有 SUCCESS |
| 镜像 | ghcr.io/a3225865526-wq/clearapi@sha256:729fdd37591fb333c9cefe202d7c2b7ff4486df31a2689a9b2152abdf7f6965f |
| image ID | sha256:729fdd37591fb333c9cefe202d7c2b7ff4486df31a2689a9b2152abdf7f6965f |
| binary SHA256 | 37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5 |
| RC ZIP SHA256 | b02e4189c742e50f2d62312dcb80b60033380ae00c3f442bda2990b948e358a7 |
| 隔离 control | f02528c351d4812336980fd56810658a6339b05c |
| 隔离验收 | [39517646061/1](https://github.com/a3225865526-wq/Clear-API/actions/runs/39517646061)，PASS，直接复用 |
| 隔离 artifact / SHA256 | 12308671421 / 816b0127874f3a77d50b33e52353742300c8d01ce4d3e3c0d43ea8779589136b |
| 新生产资格 | BLOCKED；未签发 source/control 分离 policy，不能称该候选已经具备生产资格 |

详见 `pg03-final-acceptance-20261021.md` 及 `pg03-isolated-39517646061/`。两次完整启动、原始迁移凭证、只读负向探针、六容器清理与持久化回读不重跑。此前最终 main 的 CI39517515354 及隔离结论已于本轮重新通过 GitHub 只读接口核对；这不代表本轮新代码 CI。

本轮只增加控制、适配器、受限启动器、离线恢复工具及测试/证据，未改 Go、数据库迁移、依赖、Dockerfile 或已构建镜像。无需重建该应用 RC；新控制源码和已验证镜像 source 必须分别记录。今后任何应用/依赖/镜像行为变化，资格比较器拒绝复用本次候选，要求新 RC 和受影响验收。

## 2. 候选资格与独立部署授权

`pg03-candidate-qualification.py` 复用既有 release manifest、schema 和固定隔离验证器，绑定 source/control 的完整实际差异、历史与当前控制字节、仓库数字 ID、RC/源码 CI/控制 CI/隔离的精确 run attempt、原始 ZIP 与 API digest、二进制和镜像身份、证据时效、撤销与已消费 nonce。缺失、伪造、重放、过期、UNKNOWN、失败/跳过 job、版本漂移均拒绝。旧 release-control 的 current-main 检查保留。

资格 policy 必须由独立可信实时来源签发；输入 JSON 的 approved_by、哈希、历史成功或配置开关不是授权。输出恒为 execution_allowed=false，详见 `pg03-candidate-protocol-20261021.md`。

新增 `PG03 successor independent grant` 工作流只手动运行、只读权限、无 SSH/生产 Secrets/部署步骤；本轮未 dispatch。它在生产 environment 独立审批后产生 exact request 的短租 artifact。适配器仍通过 GitHub API 校验实际 environment ID、main-only 策略、prevent_self_review、经可信 policy 明确批准的独立 reviewer、审批未拒绝、同仓库 owner dispatch、精确 artifact/run attempt 和 control。它不继承旧 owner-dispatch fallback。**当前 environment/独立 reviewer 是否满足新协议 UNKNOWN；未修改其配置。** 缺实际审批/安装资格时不会执行。

## 3. 只读采集、窗口及身份

`pg03-observation.py` 使用稳定 intent 开始观察，四通道新鲜基线连续至少 60 秒；从最新真实采样封装 observation，再绑定 scope v2、最终请求和实际资格。这样避免 future request/observation/qualification 的哈希循环；不通过更新历史时间戳生成新证据。

ExecutionWindow 实现五阶段允许状态变化，动态 Gateway CID 只能从可信 host 的成功结果绑定，candidate 身份必须精确；其余 protected runtime、Executor epoch、有效配置、schema、受保护数据、待处理业务和权限/锁维持固定。未知 writer、失联、超时、采样间断、权限拒绝、重放、漂移均终止，不能重新建立基线自证通过。已进入的 host syscall 只能在有界 deadline 内收敛，失败记录 UNKNOWN 并 containment，不能宣称即时撤销已提交的 Docker 操作。

真实四通道 reader、权限/全 writer 投影、身份 lease、durable observer reservation 与可信 final-authority 是明确的生产安装边界；本轮未加载凭据或绑定现场 transports。旧 R2 A 是单次采集，旧 helper 缺 DB/schema/资金/epoch，不能充当新窗口。实际状态：身份 BLOCKED，现场事实 UNKNOWN，真实连续窗口 NOT RUN。完整管理员步骤见 `pg03-observation-checklist-20261021.md`。

## 4. 真实备份与独立恢复

沿用停止态 failed Gateway/v2 备份完成校验，原 failed manifest 保持原值；新离线审计器核验密文/明文/各 payload 的实际 hash、加密方式和公钥引用、安全嵌套归档、完整 PG03 清单、main/log、全表行指纹及 schema/index/constraint/uniqueness/sequence/migration 对比、真实命令记录与退出结果。

离线 runner 默认仅 plan；execute 还需专用离线主机/空 Docker daemon、固定 root policy、外部 hash、管理员签名、到期和撤销检查、不可覆盖 claim。管理员先在获准环境解密，工具不获取私钥；runner 只恢复已验证 dump 到 network=none/tmpfs/无 host mount 的独立 PG17.6，产生实际恢复与审计材料。过期后无法获准清理时保留 BLOCKED 与 owned CID，须管理员处理，不能宣称已清理。

内容 PASS 与真实来源认证分离。合成夹具、假 Docker 及手写 exit0 不产生真实备份资格。**真实备份/解密/恢复/源指纹与生产者认证 NOT RUN / BLOCKED。** 既有备份仅主库；若现场使用独立日志库，在其真实 dump 与同窗口源指纹齐备前必须拒绝，不拿主库替代。管理员可执行步骤、输入格式、检查点、失败处置见 `pg03-backup-restore-checklist-20261021.md`。

## 5. 生产适配器与主机动作

ProductionAdapter 将资格、独立 grant、六类原始证据、scope/backup/restore 语义、活窗口与现有 Transaction 串联。没有 enable_live 开关或默认 host/凭据。异常、UNKNOWN 或绑定变化使对象失效；事务与 host action journal 持久消费执行权，失败不得自动重试。

ProductionHost 提供固定本地动作：准备 runtime、仅创建 Gateway（不启动依赖）、首次启动验证、保存原始 proof 后停止/换 challenge/再次启动、写独立 successor manifest。旧 failed 清单、Executor、Pool checkpoint/admission/receipts、公网入口及数据库恢复不在这些动作范围内。精确主机动作与可信 site/collector/bounded-runner 接口必须在独立安装审核后绑定；测试替身不是生产 adapter 验收。

`production-migration-entrypoint.sh` 仅从 root:root 600 的非秘密 challenge 文件读取 64hex+换行，沿用 `/entrypoint-secure.sh /new-api` exec 链。必须只读挂载整个 `/run/clearapi` 目录，以便原子轮换文件可被同 CID 下次启动看到；`/tmp` 必须 tmpfs，每次停止清除旧排他 proof，先持久保存原凭证，启动器不删除它。详见 [Docker tmpfs 行为](https://docs.docker.com/engine/storage/tmpfs/)；命令计划采用 `up --no-start --no-deps` 与独立 start，见 [Compose up](https://docs.docker.com/reference/cli/docker/compose/up/)。该附加启动链的真实生产集成 NOT RUN。

## 6. 部署前检查与执行顺序（未获准、未执行）

1. 负责人批准独立 source/control 资格协议及完整 control-only diff；冻结实际 main，保留已验镜像身份。新鲜 CI/RC/隔离 API、控制文件、撤销/时效检查全部通过。
2. 管理员独立批准并证明采集身份/host key/权限/有效期、全 writer 入口冻结、唯一 Executor 和锁；形成真实完整投影，不使用旧 helper 快照填空。
3. 在单独获准窗口完成真实停止态加密备份、独立恢复和源/目标全量验收，持久封存真实 producer；任何数据/配置/身份变化都重新评估恢复点与证据。
4. 独立审核并安装精确资产/Compose/启动器、collector 和 bounded runner；核验无 Gateway 外网监听、无依赖启动、原 Executor/epoch 不变，批准真实列/行/操作写入投影。
5. 建立连续观察 intent/session；最新样本生成 observation 和 scope v2，封装最终 300 秒 request/资格/site policy，再申请一次性生产 grant。审批等待过期即重新收集/审批，绝不续写旧时间。
6. 独占 deployment_lock 下核验 grant、全部证据及活 ExecutionWindow，持久 TX claim 和每动作 intent 后按固定五阶段执行。逐步复核权限/期限/现场状态，保存两次不同 challenge/process 的原始 migration proof、Runtime/认证 Executor、schema/受保护状态/准确写入及 restart 证据。
7. 最终新 manifest/result 持久化、producer 实际退出成功、审计独立回读全部通过，仍保持公网隔离；开放 Caddy、公网业务探测或正式对外恢复必须另行批准。

失败回退：记录 FAIL/UNKNOWN，停止本次 owned successor Gateway并保留隔离，验证 Executor/epoch、旧 failed manifest 和受保护状态。containment 不可验证时报告 UNKNOWN，管理员检查。禁止自动启动旧应用、回灌数据库、回滚 Pool 高水位、重置 receipts 或复用已消费事务。数据库真实恢复只在独立验收环境；生产恢复另行设计审批。

## 7. 安全审查与实际验证

适用参考：[OWASP ASVS 5.0.0 stable](https://owasp.org/projects/asvs)、[Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)。本轮围绕高风险操作独立认证、服务端实际权限和审批、绝对期限/撤销、一次性消费、防重放、失败闭锁及不含秘密的审计进行实现与定向验证。没有修改用户登录/密码/session 机制；没有对未安装的生产认证或整个项目宣称 ASVS 合规。

实际命令、计数、审查结论、控制 commit/PR/CI 结果见本文件末尾最终验证记录。未改数据库模型、ORM、驱动或应用迁移，复用原三库验收（SQLite3.50.4/MySQL8.4.6/PostgreSQL17.6），不将离线恢复替身标成真实 PG 验收。

## 8. 剩余最小授权及能否申请正式部署

**现在不满足正式生产部署审批条件。** 可以审核此开发包，并分别批准以下有界管理员工作：source/control 分离 policy；最小只读身份和全 writer 连续窗口；准确启动写入范围；真实停止态加密备份/离线私钥环境与独立恢复；精确 host/collector 安装与非秘密现场核验；最后独立一次性生产 grant。每项需绑定对象/hash/操作者/权限/UTC期限/撤销/审计目的地，不签发无限期或泛化 sudo/SSH 权限。

状态汇总：既有固定候选隔离 PASS；本地开发/测试以最终记录为准；真实候选生产资格 BLOCKED；现场状态 UNKNOWN；真实备份恢复 NOT RUN；生产身份/安装授权 BLOCKED；生产 adapter 现场验收 NOT RUN；正式生产部署 NOT RUN。已知旧失败保持 FAIL，不覆盖为成功。


## 9. 本轮验证记录（开发控制源码）

- WSL Linux/root `python3 -B scripts/ci/test-pg03-successor-plan.py`：82 tests OK / 0 skip / exit0；包含adapter、grant producer、kernel及启动器定向回归。
- WSL Linux/root `python3 -B scripts/ci/test-pg03-candidate-qualification.py`：13 tests OK / 0 skip / exit0。
- WSL Linux/root `python3 -B scripts/ci/test-pg03-observation.py`：22 tests OK / 0 skip / exit0。
- WSL Linux/root `python3 -B scripts/ci/test-pg03-successor-host.py`：12 tests OK / 0 skip / exit0（含host自身UNKNOWN闭锁与0750运行目录）。
- `sh -n scripts/production-migration-entrypoint.sh`、`shellcheck --severity=warning scripts/production-migration-entrypoint.sh`：exit0；12个新增/受影响Python AST解析及 `git diff --check`：exit0。
- 本机WSL `docker --host unix:///var/run/docker.sock info --format '{{.ServerVersion}}'`：exit1，Docker CLI未安装。没有安装依赖；真实独立PG恢复定向验证改由无生产凭据的CI执行，结果另记。本机不能声称真实恢复通过。
- Windows adapter定向14 tests：13 OK / 1 POSIX启动器skip / exit0。Windows qualifier调用Linux release helper的fcntl不可用；不把错误标PASS，目标Linux的13项已通过。
- 新集成测试首次因fixture代码块放置造成UnboundLocalError，修正测试布局后8项kernel定向和82项集中全通过。没有修改生产门禁求通过。
- 独立AI交叉审查覆盖候选比较/ZIP来源、两阶段窗口、adapter/grant/启动器、host持久claim/动作、恢复材料与runner。已闭环：qualification UNKNOWN/过期结果接受、UNKNOWN动作可重试、scope双向hash、future observation绑定、create/start阶段冲突、同CID challenge轮换、host精确CID containment/传递资产pin、恢复PG03清单及有界stdin等问题。独立AI审查不代替人工生产审批或真实现场验收。

新增CI只运行本地/合成和独立PG定向测试。新审批workflow仅dispatch且名称不匹配既有CI→RC→rehearsal→自动生产消费者；两自动发布变量仍false、strict required checks/enforce_admins/禁止force-delete保留。未修改保护、Secrets、生产权限或开关。


补充最终本地记录：restore集中26项（25 PASS / 1 opt-in真实PG skip）exit0；host最终12项exit0；launcher目录统一0750与原runtime prepare保持一致，未要求扩大生产目录权限。恢复批准支持真实备份完成后签发，拒绝超过24小时备份，避免通过回填issued_at满足循环时间约束。

本地WSL Git不能解析Windows worktree gitdir；其secret-scan打印PASS无有效Git输入，因此该次结果INVALID，不计通过。改用原生Git Bash先 `git rev-parse --is-inside-work-tree` 得true，再 `bash scripts/ci/secret-scan.sh` 实际PASS/exit0；没有读取或输出凭据。


## 10. PR82 第一轮CI失败及定向修正（保留原始失败）

开发head853f6e09fc841b6e9de28670ba58452740fe4dfb，CI39576186446 的独立真实PostgreSQL测试FAIL：pg_isready返回成功后的首次SHOW server_version_num子进程非零。job119142169666原始日志保存在 `pg03-readiness-ci-39576186446-postgres.log`，原GitHub run保留。该错误不是既有候选隔离验收失败，不重跑39517646061。

原日志没有子进程stderr，初始化临时socket server的短暂ready是待验证根因假设，未写成实测事实。修正为精确检查容器PID1 comm==postgres，再判pg_isready；增加“临时bash即使ready也不得进入SQL”的回归，仅合成真实PG测试追加有界stderr诊断。后续CI结果决定是否通过，不能由本地假Docker替代。

补齐真实临时OpenSSL/execute入口门禁验证：有效签名+durable claim、同授权重放、错误签名、过期和越权拒绝（只创建临时合成密钥，Docker/mount/machine ID为显式替身）。恢复集中现在29项：28 PASS/1真实PG opt-in skip；观察窗口23项PASS，均WSL exit0。签名入口不等于真实生产恢复producer认证。

观察GitHub投影改为未授权active/pending集合为空，允许精确预先获准的唯一只读grant控制活动；不能按workflow名或owner泛排除。完整raw活动仍须audit；request_sha256成为dispatch显式输入和run-name，并由固定producer校验canonical输入匹配，最终artifact/actualreview验证成功前不得seal。否则“窗口内禁止所有workflow”会使合法独立审批本身破坏窗口。此项不允许任何生产写入或生产workflow豁免。


第二轮CI39577444624（ebfd132bb）保留FAIL：PID1门禁后已通过SQL/源inventory，真实pg_restore因/restore/main.dump不存在失败，docker cp先前退出0。按 [Docker cp官方tmpfs限制](https://docs.docker.com/reference/cli/docker/container/cp/#corner-cases)，改用固定exec stdin流式写入live tmpfs并独立校验目标SHA256；不扩大挂载/权限。真实PG定向测试移至CI独立只读job，避免等待与其无关的Go构建（三库原有验证均保留）；之后CI为8 jobs。

同轮backend既有未修改测试TestE2EDeadlineInheritanceRealTiming失败：B dispatch expected1/actual0，2.80s。其deadline2s、A延迟1.5s；未改Go/测试断言/应用候选，不将调度解释写成已确认根因。首轮相同应用的backend通过不替代新head检查，后续正常CI仍必须通过。原始backend/postgres日志及hash清单保留。


流式tmpfs修复本地独立复核：同fd预hash/rewind→Popen stdin，精确literal shell+位置参数，不插值命令；文件<=16GiB、nofollow/singlelink、消费完整和inode/size/mode/mtime/ctime复核，目标容器内SHA256独立相等后才pg_restore。SQL字节stdin原1MiB限制保留。新增>1MiB真实文件流、错hash/符号/硬链接/截断及目标hash不符回归。32项本地恢复测试为31 PASS/1真实PG opt-in skip，exit0；不据此声明真实Docker修复PASS。
