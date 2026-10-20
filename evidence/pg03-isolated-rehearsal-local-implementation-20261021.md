# PG03 独立隔离演练工作流 — 本地实现记录

日期：2026-10-21（Asia/Shanghai）。范围：本地实现、离线替身回归与静态检查。

**本地实现完成，已运行的定向检查 PASS；PG03 仍 BLOCKED。真实镜像读取 NOT RUN，第一次启动 NOT RUN，第二次启动 NOT RUN，真实 schema/迁移结果 UNKNOWN。** 未注册或执行工作流，未生成真实成功 receipt。此前设计和安全接入审查是实施依据；本记录只更新其本地实现阶段，不将历史只读快照当作当前远端事实。

## 固定输入与证据含义

| 输入 | 固定值 |
| --- | --- |
| 候选镜像 | `ghcr.io/a3225865526-wq/clearapi@sha256:07fd58272b4fc62bcc52a3ad1d20e0dac357ba93de9e91a6a797d5e8fe3e8e7a` |
| 镜像源码 | `69a66431646b5cc8912bf97a739e08de07316443` |
| RC / attempt / artifact | `39357150402 / 1 / 12235966400` |
| 原始 ZIP SHA256 | `ac7c42fc5a3549773f2c59080b503d6d5201f7442c9c20a955eb542f97ffb09b` |
| 基线 schema SHA256 | `3c1c77f355a2f9376f7cd4133267c99856cd4905b6116ad2ba615dbf84305035` |
| `/new-api` SHA256 | `40ebc70665bf7f45c7a201c4fdd82a90913a1873b2c7ca0debda1829983eee4b` |
| 测试数据库 | `postgres@sha256:00bc86618629af00d2937fdc5a5d63db3ff8450acf52f0636ec813c7f4902929`；期望 `170006`（17.6） |

基线是固定 RC 的无行数据目标 DDL，与历史事故后摘要一致；不是本轮新鲜生产 schema。即使后续运行成功，也仅证明该固定目标 schema 上的两次隔离启动，不证明当前生产等价、完整数据恢复、SQLite/MySQL 兼容性或业务/账务验收。本轮没有修改应用、ORM、迁移或数据库配置行为，没有执行三数据库矩阵；本项本来只覆盖 PostgreSQL 的隔离测试编排。

## 文件变更

| 路径 | 本轮内容 |
| --- | --- |
| `.github/workflows/pg03-isolated-rehearsal.yml` | 新增唯一独立手动入口；30 分钟 hosted Ubuntu job，无生产后继调用 |
| `scripts/ci/pg03-isolated-rehearsal.py` | 新增固定 RC 下载/验证、临时认证、精确拉取、本地 daemon 绑定、最终清理和脱敏产物适配；双启动仍复用 successor |
| `scripts/pg03-successor-plan.py` | 在已有未跟踪实现上增加可选观测、nonce 和命令注入；保留原离线契约及旧调用方式 |
| `scripts/pg03-schema-rehearsal.py` | 补充逐次观测、启动前身份验证、禁止隐式拉取和失败后逐项清理；旧 receipt 必需字段保持兼容 |
| `scripts/ci/test-pg03-successor-plan.py` | 在已有集中测试中增加隔离、退出、清理、权限和触发图回归；未散落测试文件 |
| `CLEARAPI_MASTER_STATUS.md` | 前置最新本地状态；原有字节保留 |
| `evidence/pg03-actions-isolated-rehearsal-design-20261020T155803Z.md` | 追加本阶段实施状态，保留原方案 |
| 本报告 | 实物差异、验证、边界与后续审批 |

现有 9 份 workflow（含此前已修改的 `ci.yml`）与本轮开始时逐字节一致。没有更改现有生产发布/恢复配置、自动开关、保护、Secrets、包 ACL 或依赖。未新增旧方案的另一份 workflow 文件。

## 实物安全审查

- 事件仅 `workflow_dispatch`，名称独立为 `PG03 isolated exact-image schema rehearsal`；同仓库、main、owner 首次 actor 和 triggering_actor 均校验。并发组独立且 `cancel-in-progress: false`。本地解析现有全部消费者，均不匹配此名称，也没有通配监听。
- workflow 和 job 均仅 `contents: read`、`packages: read`、`actions: read`。`actions: read` 只用于读取固定 RC 的元数据和原始 ZIP；代码没有 dispatch/rerun、包写、部署或权限写 API。无 production Environment、仓库 Secrets、SSH、self-hosted、可复用部署任务或 `secrets: inherit`。
- 只用两个既有完整 SHA 固定的 Action：checkout `9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0`，upload-artifact `043fb46d1a93c77aae656e7c1c64a875d1fc6a0a`。沿用已有受审配置中的 pin，未重新联网解析版本或独立审计其实现。checkout 禁用持久凭据；控制提交使用 `github.workflow_sha`，要求等于事件 SHA、实际 HEAD 且 checkout 无改动。记录控制脚本/工作流/测试 hash，与候选源码明确分开。
- RC 必须为固定路径、仓库、源码、成功状态和 attempt 1；artifact ID/run/名称/未过期/大小须匹配。原 ZIP 先校验外部固定 hash，再复用现有全部条目和 payload/manifest/schema 校验；过期、缺失或任何不匹配均停止，不替换为最新 artifact 或重建 RC。
- 候选和数据库均只按固定 digest 各拉取一次，无 tag、build、push、换凭据或拉取失败重试。先验证 containerd image store；需要时仅合并可丢弃 hosted runner 的 Docker feature 并重启本机 Docker。此操作未在本轮执行，未来运行审批须明确包括它。
- 短期 job token 仅出现在 baseline/pull 步骤环境；registry login 走 stdin，认证位于本次私有临时目录。成功或失败均删除本次 `config.json` 和目录，测试启动前再次确认不存在。Docker 子进程使用受限环境和固定 `unix:///var/run/docker.sock`，不继承 token、生产 DSN、Docker 远端地址或 GH_DEBUG；候选容器只接收明确列出的合成环境。
- 启动前验证完整 RepoDigest、linux/amd64、OCI revision/source、RC image ID、固定二进制 hash；binary probe 无网络且只读。所有 create 使用 `--pull never`。数据库为 network none、tmpfs、无端口/宿主数据挂载，Gateway 仅共享该测试 DB namespace 的 loopback；没有生产连接参数、Provider 或 Executor 凭据。
- 同一测试 DB 连续供两个新 Gateway 使用。每次分别 dump 实际启动前/停止后 schema，均须等于固定基线；记录健康、容器 ID、create/start/stop 命令退出码、进程最终状态、ExitCode、OOM 和错误是否存在。失败命令记录阶段、可取得的返回码或 timeout，未知不填成功。SIGTERM 后要求 exited/Running=false/ExitCode=0/非 OOM/无 Error；依据当前 HTTP graceful shutdown 契约，真实镜像实际表现仍未验证，不接受 137/仍运行的默认 ExitCode。
- 清理按本次 nonce、已记录 ID 和复核标签执行，逐项失败继续尝试其余资源；仅 `docker rm -f -v <owned-id>`，不全局 prune。always 最终步骤再按本次唯一标签查残留、逐项复核和删除、复查为空，同时独立清理认证。任一步失败、取消或清理未知均不能发布成功凭证。
- 只上传两条精确白名单：`public/result.json` 和成功时的 `public/success.json`，保留 7 天。逐次证据只有 schema 摘要和受限状态，不上传原始容器日志、环境 dump、原始 inspect、二进制、认证或签名下载 URL。原始错误均捕获，不回显潜在秘密。底层暂存 receipt 在私有目录，只有全部验收与最终清理成功才对外发布。

正常退出清理容器及匿名卷后，tmpfs 数据随之销毁。runner 上的缓存镜像、私有 RC/暂存证据和其他临时文件随 GitHub hosted runner 丢弃；不在共享机器执行。强制终止/平台故障可能跳过 finally/always 或上传，此时不能保证清理观测，状态保持 UNKNOWN，依赖 hosted runner 销毁，不声称成功。未提供生产环境清理或恢复入口。

## 实际本地验证

| 命令/检查 | 实际结果 |
| --- | --- |
| `python -B scripts/ci/test-pg03-successor-plan.py` | 31 tests，OK，无 skip；保留原 19 项并集中补充 12 项。所有 Docker/网络/数据库相关新增用例使用确定性替身，没有真实启动 |
| Python `ast.parse` 四份受影响脚本 | PASS；测试导入和运行也通过 |
| PyYAML 6.0.3 `BaseLoader` 加重复键拒绝，解析 `.github/workflows/*.yml` | 10/10 PASS；权限/唯一手动事件/完整 Action SHA/生产边界及消费者断言通过 |
| `C:/Program Files/Git/bin/bash.exe -n <临时提取脚本>` | 新 workflow 全部 7 个 run 块语法 PASS，仅解析、未执行 |
| 现有 9 份 workflow SHA256 对比本轮开始值 | 全部 MATCH，包括既有 `ci.yml` 修改 |
| UTF-8、尾随空白、`git diff --check`（本次路径） | PASS；未跟踪文件另作逐行检查 |
| actionlint / shellcheck | NOT RUN：PATH 和 Git 工具目录未安装；未安装任何依赖 |
| 完整 CI、候选/数据库拉取、真实数据库、双启动、远端 Actions | NOT RUN；严格限本地静态授权 |

首次定向测试失败源于 Windows `write_text` 将合成 SQL 夹具转换为 CRLF，使字节断言不符；改为 `write_bytes` 后通过。新增回归覆盖任一次启动/任一次 schema 漂移、异常退出/OOM/未退出、错误二进制、停止与清理失败、清理失败仍继续其余资源、认证失败擦除、不重试、继承秘密/远端 daemon 排除、成功凭证最终门禁及触发图。替身结果不是镜像或真实迁移证据。

收尾独立重建本轮前总控 SHA256 `8d9251356b03c809aa938d7e57e1587aa5deca19159b3989c50caf7893e53e3a`、阶段设计报告 SHA256 `eb7e1a4a57f985378627d18a9cc8276fddc728154a62820caeb0b3e21fcb5e3a` 均 MATCH。首次重建检查错误假定原总控标题后的空行是 CRLF；查看原始分隔字节后改用其实际 LF，通过原 hash 核验，未为满足检查重写历史内容。

实现文件 SHA256（本报告编写时）：

| 文件 | SHA256 |
| --- | --- |
| workflow | `9720cab3c6ad6a8999b39509afca5b14c507ed81d0d61cfca627098549132910` |
| Actions 适配脚本 | `c2d5a0e5be8f2ae8532c04a8f0da164ba19aeaa259da34b489f6b9c5bec314ae` |
| schema helper | `54b7024681f801ca3a726b97ab41e1d4d9528eadb523ee8b18147145c0a8d61b` |
| successor | `098c222e035ba9f015e1b1fbf5c7de006326122f7941a9a0965116352c34745c` |
| 集中测试 | `fc2390e1399fe8684959ca95873aa94f7b84c8457142cb2ba3c4ffe458397b12` |

schema helper 原 hash 为 `0b594a4fb054bf6c921d70d66aee1bbb63687ad6c564719664c62b37ce08d517`。它也是现有 RC/部署审计共享工具；本轮只改本地源码，没有安装到生产或更新远端工具。未来任何涉及该文件的生产字节比对/安装须另行评审，不能将新 hash 冒充旧工具或旧 RC 的 hash。

## 剩余风险、审批和停止条件

本地静态实现不证明 Actions 包读取实际有效、固定 artifact 仍可用、runner Docker/containerd 可配置、候选真实健康/退出或数据库 schema 幂等。actionlint/shellcheck 仍待具备工具的环境运行；实际 GitHub schema 校验与 required CI 也未发生。

最小下一步是负责人审阅上述 8 个文件的实物差异。本轮到此停止；不提交、推送、PR、合并、默认分支注册、dispatch/rerun、真实镜像读取、真实数据库、生产 SSH/部署/重启或数据库写入。

后续注册须单独明确授权：保留正常 PR、required checks 和管理员保护，合并前重新读取确认两个自动开关仍 false、触发图/生产消费者/并发边界未改变。旧审查的 false 值仅为历史观察，不据此直接放行；不能通过改开关、skip-ci 或改保护注册。合并后仅观察正常 CI 与后继 skipped，不能重跑现有发布链。

再后续一次独立运行仍需另批固定控制 ref、执行窗口和 hosted runner 写入范围（包括可能的 runner Docker 重启、临时拉取/认证、测试 DB DDL/合成写入和资源删除）。不得重建候选以匹配新控制提交，不得转用现有生产恢复或演练入口。只有真实精确镜像、两次健康/schema/退出与清理证据齐备，才能重新评定这项隔离演练；PG03 整体和生产验收仍有独立门禁。


## 2026-10-21 — 独立只读审查：BLOCKED

本节为本地实现后的独立复审结论，优先于前文“定向检查 PASS”推导出的任何注册/运行就绪理解；原实施记录不改写。负责人已明确允许本轮**仅追加本报告与总控**，代码、workflow、测试及其他文档保持只读。

**总体：BLOCKED；当前不建议将此实物版本提交“注册工作流”审批。** 触发/权限/隔离路径的静态审查通过，但发现两项 P2 验收正确性问题，尚不能满足本轮“失败不误报 PASS、迁移失败可识别”的要求。未发现可证实的 P0/P1 生产直达路径或真实秘密泄漏。问题未修复，不能以 31 项测试通过覆盖它们。注册本身的事件隔离可行性与本版本的验收就绪性分别判断。

### 1. 审查对象与事实基准

已检查指定 8 文件实际内容、跟踪差异及未跟踪文件全文；不能用 `git diff` 未列出新文件代表没有变更。总控相对 HEAD 的 437 行 diff 含早前多阶段记录，本轮仅审查其与当前阶段的表述、历史保留和授权边界，不把历史上线/SSH事实重新认证为现在的事实。另检查 `.github/workflows/ci.yml` 的已有两行接线、boot 测试已有差异、全部工作流解析结果、successor 导入的 proof gate 清单和 schema helper 的 RC/legacy/recovery 调用方。

受审代码 SHA256 与前文 5 项实物清单完全一致。本轮检查前后，全部 10 份 workflow、四份 Python、阶段设计及两份拟追加文档均未被测试修改；追加后仅这两份审查文档改变。工作区已有其他未提交/未跟踪资产保留，暂存区为空；没有审查或整理无关代码。

本轮 GitHub GET 重新确认（不是沿用作者结论）：

- 默认 main=`69a66431646b5cc8912bf97a739e08de07316443`；已注册工作流 9 份，尚无新独立任务。
- `CLEARAPI_AUTO_RC_ENABLED=false`、`CLEARAPI_AUTO_PRODUCTION_ENABLED=false`；updated_at 分别为 `2026-10-19T15:19:02Z`、`2026-10-19T14:16:55Z`。main strict required checks 仍为 Backend vet, build, test, migration binaries / Frontend typecheck and test / Secret scan and forbidden files，enforce_admins=true；没有降低保护。
- 9 份远端 workflow 均按该固定 main Contents API 读取并完整解析；8 份与本地忽略 CRLF 后一致，CI 与本地已有两行差异分开记录。唯一 job 级 reusable 为 automatic-deploy→deploy-production，没有新任务名的消费者或通配消费者。
- 查询时 in_progress/queued/waiting 的 total_count 各为 0；只代表这三种状态的当前采样，不声称持续排除所有人工或外部调用。
- 仓库默认 workflow 权限为 read，不能由 Actions 批准 PR；production Environment 只有 branch_policy，无人工 reviewer/timer，可手动进入的现有生产入口不能当作审批隔离屏障。
- 固定 RC `39357150402/1` 仍 completed/success、main、指定 head_sha/path。artifact `12235966400` 未过期，25180 bytes，API digest=`sha256:ac7c42fc5a3549773f2c59080b503d6d5201f7442c9c20a955eb542f97ffb09b`，expires_at=`2027-01-18T02:06:56Z`。本轮只读元数据，没有下载 ZIP 或候选镜像，不将元数据当作本轮 payload 或镜像实物校验。
- checkout/upload 两个固定完整 SHA 均经官方仓库 commit API 确认存在；没有仅凭 SHA 字符串长度下结论。未审计其完整依赖供应链。

记录时点：2026-10-21 00:28:32 Asia/Shanghai（2026-10-20 16:28:32 UTC）；以上 API 是本轮各次 GET 的采样，不是未来注册窗口的永久保证。

### 2. 具体发现（只记录，不修复）

**F1 — P2：成功状态先于成功凭证落盘，证据写入失败时仍上传“success”。**

- 位置：`scripts/ci/pg03-isolated-rehearsal.py:160–171`；上传消费者 `.github/workflows/pg03-isolated-rehearsal.yml:82–90`。
- 实际行为：先设置 `conclusion=success`、`success_receipt_present=true` 并写 `public/result.json`，随后才创建/写 `public/success.json`。如果后一步出现 I/O 错误，finalize 会非零退出，但 result 保持已发布的成功状态；always 上传允许仅有 result 时成功，无法由该文件真实判断凭证是否已发布。正常成功路径的公开 steps.finalize 也仍停留在 started，之后 passed 只写入私有 state。
- 已复现：不调用 Docker/网络，复用测试中的合成成功状态，mock `Path.open` 仅令临时 `public/success.json` 创建失败，结果为 `result_conclusion=success / reported_receipt_present=true / actual_receipt_exists=false`。这证明产物可误报；**没有证明 GitHub job 会变为 success**，该 job 本身仍会失败，外部验收必须同时核对 run/job 最终状态。
- 影响：失败运行可留下一份可上传但自报成功的结果，违反失败证据必须保持 BLOCKED 的要求；人工或后续工具单看产物会被误导。
- 最小修复建议：在全部成功产物可靠完成后，才发布完整成功摘要；发布错误路径保留明确 failure/unknown，不能在出错前声明 success_receipt_present。补充“success 文件创建/写入失败、最终 state 写失败”的定向回归，并验证公开阶段状态与实际一致。不在本轮修改。

**F2 — P2：健康、退出和 schema 摘要不能识别候选中被吞掉的迁移错误。**

- 位置：`scripts/pg03-schema-rehearsal.py:143–182`，以及 `scripts/ci/pg03-isolated-rehearsal.py:145–157` 的最终成功门禁；固定候选源码对照 `model/main.go:338–340`、`model/main.go:401–408`、`main.go:378–381`。
- 源码事实：主迁移通常将错误返回并阻止 HTTP 启动，但 options 主键迁移、部分 best-effort 回填及 retired options 迁移存在只 `SysError` 记录后继续的路径。已从固定远端源码读取，忽略 CRLF 后与对应本地文件一致，不是推测候选一定会失败。
- 静态影响：当这些路径报错但最终 schema 不变时，应用仍可能健康并在 SIGTERM 后 exit 0；演练没有任何日志或结构化迁移结果采集，所有当前成功条件仍能成立。`pg_dump --schema-only` 也无法判断 DML 回填失败或行数据残留。不能把它扩大解释为“所有迁移/数据幂等性通过”。
- 验证强度：确定性替身路径可发布两次健康/同 hash 的 receipt，且没有迁移结果/日志查询；这只证明控制层没有对应检测，不代表本轮在真实候选中注入或观察到了迁移错误。真实行为仍 NOT RUN。
- 最小修复建议：在隔离控制层增加有界、脱敏的迁移失败状态采集或可靠结构化结果校验，保持原始日志不上载；补充“健康=true、schema 不变、exit=0，但迁移错误存在”时必须拒绝成功的回归。或者由负责人另行明确缩窄验收标准并修订证据声明后再复审；本轮不自行把用户要求降为仅 hash 相等。行数据业务验收仍需独立范围，不能要求本项证明生产数据安全。

P0/P1：本轮未发现。两个 P2 是普通代码/验收正确性问题，不是已确认的生产事故；因直接影响本任务的成功判定，当前保留 BLOCKED。

### 3. 六项关键要求判定

| 要求 | 判定 | 依据与限制 |
| --- | --- | --- |
| 工作流安全性 | PASS（静态、当前配置） | workflow:3–28 仅手动、owner/main、三项 read；35/84 两个有效完整 Action SHA；无生产 Environment/SSH/reusable 调用。当前远端链 main push→CI→RC→演练→自动生产仍存在，两项 false 使对应自动 job 不执行；注册前必须重查，手动生产入口不受此统一关闭 |
| 镜像身份 | PASS（控制逻辑）；实物 UNKNOWN | adapter:68–79 固定 RC/ZIP/候选，93–103 仅 digest 拉取一次，无 tag；232–244 与 helper:43–77 再验 RepoDigests/平台/source/image ID/二进制。Gateway 由已验 image ID create，--pull never；缓存不能通过不同 ID/摘要替代。拉取被缓存满足仍可接受内容寻址身份，但不等于本轮证明远端字节传输 |
| 两次启动真实性 | PASS（静态）；两次 NOT RUN | helper:81 单建 DB、112 连续循环、124 每次新建 Gateway、129 同一 loopback DSN/DB namespace；每次新的 health exec、stop/inspect、前后 dump。第一实例退出后才创建第二实例，非日志重放。run/attempt 独有目录与 nonce 避免旧 receipt 重用；真实 image/start/health 仍未验证 |
| 数据库隔离与异常 | PASS（静态和替身故障探针） | network none、Gateway container:DB namespace、无端口/宿主目录/socket 挂载，合成 SQL_DSN、空 LOG_SQL_DSN；适配器固定本地 daemon、受限子进程环境。分别在第一/第二次 start 注入 TimeoutExpired 均 no receipt、timed_out=true、cleanup_complete=true。强制 job/runner 终止仍可能无清理证据，必须 UNKNOWN 并依赖 hosted runner 丢弃，不宣称绝对无资源残留 |
| schema 与迁移判定 | PASS WITH FINDINGS（F2） | 每次真实 pg_dump 前后摘要，均需等于固定 RC 目标；第二次仍用同一 DB，DDL 改变即拒绝。仅固定 RC 无行数据目标 DDL，非新鲜生产 schema；非致命迁移错误与数据行语义无检测 |
| 失败处理与证据 | BLOCKED（F1） | 通常异常/清理失败会阻止 receipt，逐项清理失败继续其余对象；短期 auth stdin/0700目录、finally+always 清理、仅白名单上传、State.Error只记录存在性。但成功文件写入失败的摘要可误报，当前不满足完整失败判定 |

网络隔离判定依赖受信任的 GitHub hosted runner 与正常 Docker namespace/内核隔离边界，未执行真实实例验证；没有将静态参数检查写成生产数据库可达性实测。测试数据库会执行合成 DDL/启动写入，这属于未来运行审批，当前没有执行。

### 4. 测试审查与实际验证

- 重新运行 `python -B scripts/ci/test-pg03-successor-plan.py`：31 tests OK，无 skip，耗时约 0.8 秒。原有 19 项主要是离线 successor/基线契约，新增 12 项检查编排与部分失败边界；不能把所有 31 项都称为真实双启动测试。
- 实际阅读测试替身：create/start/health/dump/inspect 都由 Python 返回合成值，核心两次使用同 DB namespace、不同 Gateway ID、schema 漂移、未退出/OOM/137、停止失败、清理失败、身份二进制拒绝有有效断言。现有 fixed-RC 测试并不覆盖完整网络下载/校验/拉取/main 多模式串联，名称也不等于覆盖证明。
- 10 份 YAML 重复键拒绝解析、4 份 Python AST、新 workflow 全部 7 个 run 块 `C:/Program Files/Git/bin/bash.exe -n`、定向 `git diff --check` 通过。已有静态测试检查权限、手动事件、SHA pin 和当前消费者；没有将 PyYAML 当作 Actions 表达式/actionlint 验证。
- 补充三个不写仓库的临时探针：success 产物创建失败可复现 F1；两次 start 分别注入 timeout 均关闭成功路径；同 schema/健康的替身没有非致命迁移状态检测，支持 F2 的静态边界判断。探针只使用临时目录和 fake Docker，不是候选或数据库演练。
- actionlint / shellcheck 未安装，NOT RUN；未安装或下载工具。现有完整 CI、需 Linux/root 的 legacy/recovery/systemd 测试、三数据库矩阵均未重跑：本轮不改其实现，且真实实例/演练超出授权。共享 helper 的 validate_receipt 接口未改，相关调用方已静态检查，不宣称所有既有回归均通过。
- 本地缺少指定 merge commit object，`git show <候选>:model/main.go` 返回 128；未 fetch 或写 Git object，改用固定提交 Contents API 获取 main.go/model/main.go 对照成功。首次失败不计作源码身份验证通过。
- 未下载真实 RC ZIP、未请求 GHCR、未读取 Docker image/容器实物、未启动任何真实容器/数据库、未访问生产 SSH。实际镜像读取 NOT RUN，第一次/第二次启动 NOT RUN，schema/迁移结果 UNKNOWN，PG03 BLOCKED。

### 5. 注册和运行建议、停止

**当前不建议进入注册审批。** 先由负责人决定 F1/F2 的最小修复范围，完成本地修复和针对性只读复审，再固定具体控制提交及完整 diff 申请正常 PR/注册；不能为让测试通过修改本轮受审代码，也不在本轮展开修复。

从事件隔离角度，独立手动任务的注册路线仍可行：保留现有自动开关、三项严格 required checks 和管理员保护，注册前重新核对当前远端配置/消费者/并发状态；合入 main 会运行正常 CI，并可能产生 skipped 后继记录，不能承诺“零 Actions 记录”。本轮的安全快照不授予未来合并或运行许可。

**即使后续批准注册，实际运行仍必须单独审批。** 运行审批需固定已修复的控制 ref、唯一候选 digest、RC/原 ZIP hash、执行窗口及 hosted runner 上的 Docker feature/重启、临时认证/拉取、测试 DDL/合成写入与资源清理范围。注册不等于允许拉取/启动，隔离演练通过也不等于生产验收。

本轮仅按明确补充授权追加两份审查文档。代码/workflow/测试/设计文档未改；无提交、推送、PR、注册、dispatch/rerun、生产操作或下一阶段执行。审查完成后停止，等待负责人决定。


## 2026-10-21 — 两项 P2 本地修复证据：P2-1 PASS / P2-2 BLOCKED

本节追加修复结果，不改写上方原始发现与独立审查结论。本轮仅限两项缺陷及直接测试/产物接线；**PG03 仍 BLOCKED，不是 READY，不申请注册或真实运行。** 可以申请新一轮独立审查，审查对象包括修复代码及 P2-2 仍不可证明成功的边界，不能将该建议解释为注册建议。

### P2-1 — 成功摘要提前发布：PASS（Linux 本地回归）

修复前顺序：清理/校验 → 公开 result=success、receipt_present=true → 写 success.json → main finally 再写私有 state。后两步失败可留下错误的成功摘要。

修复后顺序：

1. 完成验收/清理；若 public 已存在，按本次 run 根目录边界复核后改名保留到 `retained-public-<nonce>`，拒绝本轮成功，不覆盖或删除旧证据。
2. 在本次 run 下的新私有 `evidence-*` 目录写中间观察（pending_publication）并 flush/fsync；再写 success.json（仅满足全部条件时）、最后写 result.json，每份文件都 flush/fsync。
3. 同步暂存的目录，将整份 public 目录原子改名发布，再 fsync 本次 run 父目录。半写入和未完成同步的暂存文件不在上传白名单。
4. 发布错误必须退出非零。若能写失败证据，则撤下本次公开目录并发布 `failure_or_unknown`、receipt_present=false；连失败结果也无法写入时直接非零，不输出成功。保留部分私有证据供检查，不对未知工作区做删除/覆盖清理。
5. finalize 模式在发布后直接返回，移除原来发布后的 state finally 写入；公开 steps.finalize 与实际成功/失败一致。新增独立 `verify-public` 回读步骤，同时核对 finalize 的实际 outcome：失败 producer 只允许上传失败 bundle；结果/凭证必须完整可解析。只有回读步骤自身 success 且生成 evidence_ready，才允许现有白名单上传。即使文件系统拒绝移动旧成功目录，也不能由其获得上传许可。

实现位于 `scripts/ci/pg03-isolated-rehearsal.py` 的 sync_directory / persist_json / quarantine_public / publish_evidence / verify_public / finalize，以及新独立 workflow 的 finalizer ID、回读步骤和上传 if。没有修改任何现有生产 workflow。GITHUB_OUTPUT 只是回读步骤的传递通道；即使其写入失败，回读 step outcome 不是 success，上传条件仍关闭。

新增 7 项 publication 方法覆盖：完整成功文件顺序；凭证/中间证据部分写失败；结果文件始终写失败；父目录同步失败后的撤回；旧成功结果保留且本次失败；CLI 非零且无成功输出；旧目录无法改名时回读拒绝。成功场景中的 migration=success 是**仅测试发布机制的合成前置状态**，并非当前候选能提供此信号的证明。

### P2-2 — 迁移错误检测不足：BLOCKED（拒绝门禁已实现，成功证据缺口未闭合）

先重新读取固定提交 `69a66431646b5cc8912bf97a739e08de07316443` 的 main.go / model/main.go / common/sys_log.go，经 Contents API 核对与对应本地源码忽略 CRLF 后一致，未拉取镜像或下载二进制。确认：

- 主迁移多数错误会返回，但 options 主键、部分回填/种子、retired options，以及 price_amount 元数据/DDL 的某些错误只记日志后继续；某些 warning 甚至经 SysLog 输出。
- `SysLog` 与 `SysError` 都输出 `[SYS] <时间> | <消息>`，不带结构化迁移步骤/状态，两者写入错误被忽略。logger 的 MultiWriter 分别关联 stdout/stderr 和可选文件；仅捕获 stdout 还会漏掉 SysError。
- 只有“database migration started”等一般日志，没有覆盖全部迁移/回填、与实例和 schema 绑定的可靠完成记录。健康/exit 0/无已知错误文本均不能补齐这个缺口。固定 digest 不变，不能在控制脚本里补写候选从未产生的成功事实。

最小实现的边界：

- 只在新隔离模式启用 require_migration_evidence；successor 的 observation 路径显式开启，最终摘要也要求每次 migration.status=success。既有默认 RC helper/validate_receipt 语义不借此升级为完整迁移验收。
- 每次停止实际 Gateway 后按新建容器 ID 获取带 Docker 时间戳的日志，保留 stdout **和 stderr**。新模式显式使用 blocking json-file、无轮转；收集后解析上限为 2 MiB，过大、获取失败、非法编码/无法解析均 UNKNOWN。原始日志只在控制进程内解释，不写入产物、CI 输出或报告。
- 解析 Docker 时间戳、SYS 时间和完整记录边界，按固定源码的精确迁移事件语法映射成有限错误类别；不按任意子串、业务日志中的引用或一般 error 字样认定迁移失败。产物只含实例/source、日志 hash、记录数量、错误类别和 reason，不反射 SQL/日志 payload。
- 已识别迁移错误 → failure；实际前后 schema 与证据冲突 → failure；没有可靠迁移完成契约 → unknown，即使日志采集完整、健康且 exit 0，也不得产生成功 receipt。日志读取失败仍进入原来的逐对象清理。
- `capture_complete` 仅描述本次 Docker 捕获/语法是否完整，不代表应用的每一次日志写入都成功，更不代表迁移完成。当前固定候选的解析器**没有任何返回 success 的路径**；这是明确阻塞，不是已修好正向验收。它会在首次迁移证据不足时停止，不通过重复第二次健康检查弥补缺口。

新增 8 项 migration 方法覆盖：完整正常旧日志仍 UNKNOWN；已知迁移错误分类与 payload 脱敏；普通业务错误/嵌入引用不误判；缺失、损坏和超限日志；schema 冲突；exit 0 且 schema 相同但迁移报错时不出凭证；无法取得日志时清理并 UNKNOWN；stderr 保留。

**未满足的正向要求明确保留：** “正常启动且迁移证据完整 → 证明迁移成功”的真实候选用例当前无法提供。现有候选没有这种可验证证据，不能用合成 status、无错误字符串或 schema 相等代替它。进一步闭合需要另行评审可靠的候选侧迁移完成/错误证据生产方案，且必须与允许测试的镜像身份相容；本轮没有修改 Go 业务/迁移代码、构建新镜像、更换 digest 或扩大执行授权。因此本项最终判定是 BLOCKED。

### 验证与文件范围

| 检查 | 实际结果 |
| --- | --- |
| `wsl.exe -d Ubuntu --cd /mnt/d/AI/API/worktrees/clearapi-controlled-launch-20261020 -- python3 -B scripts/ci/test-pg03-successor-plan.py` | 最终 46 tests OK，无 skip；原 31 项加 15 项新方法。Linux /tmp 使用真实文件 flush/fsync、目录 fsync/rename，Docker/数据库/网络全部替身 |
| `python -B scripts/ci/test-pg03-successor-plan.py`（Windows） | 46 tests OK，9 项 POSIX 持久化方法明确 skip，37 项通过；不把跳过计作 Linux 持久化 PASS |
| 四份 Python AST、10 份 YAML 重复键拒绝解析、独立任务触发/权限与回读上传条件、8 个 shell run 块 Git Bash -n、UTF-8/空白与 git diff --check | PASS |
| 原有 9 份 workflow 的本轮前字节 hash | 全部 MATCH，包括 ci.yml 的既有两行改动；不声称工作区相对 HEAD 无差异 |
| actionlint / shellcheck | NOT RUN，未安装；没有安装依赖 |
| 实际 Docker 日志、候选启动、真实 DB、crash/power-loss、GitHub runner 文件系统/表达式执行、完整 CI/数据库矩阵 | NOT RUN；超出本轮授权或没有对应环境，不推断通过 |

首次 Windows 回归因 `os.O_DIRECTORY` 不可用而报 8 个错误；该目录持久化契约的目标是 Linux，未在生产逻辑跳过 fsync。随后改为明确的非 POSIX 拒绝、相关用例标注平台限制，并在 WSL Linux 真实执行全部持久化测试通过。WSL 显示现有 localhost 代理转发提示；测试没有网络调用。初始日志文件名定位与 PowerShell glob 读取失败后，按 rg 实际命中路径读取；没有执行迁移或应用入口。

本轮仅修改下列 7 个文件：

| 文件 | 修改 |
| --- | --- |
| `.github/workflows/pg03-isolated-rehearsal.yml` | 仅新增结果回读门禁及上传条件，不改事件、权限、候选或运行入口 |
| `scripts/ci/pg03-isolated-rehearsal.py` | 安全持久化/目录发布、旧结果隔离、公开结果回读、避免发布后再写私有状态、保留日志 stderr |
| `scripts/pg03-schema-rehearsal.py` | 新隔离模式日志采集、已知迁移错误分类、无可靠成功证据时 UNKNOWN |
| `scripts/pg03-successor-plan.py` | 将新隔离 observation 调用接到迁移证据门禁 |
| `scripts/ci/test-pg03-successor-plan.py` | 集中新增 15 项，保留原行为回归，区分 Linux/Windows 能力 |
| 本报告 | 追加修复证据，原始发现不覆盖 |
| `CLEARAPI_MASTER_STATUS.md` | 追加 P2-1 PASS / P2-2 BLOCKED 与停止边界 |

最终实现 SHA256：workflow=`b74067842820f5f1ee5855506954ef841b92e80e91e41f864e2fae5ca10801c8`；Actions adapter=`c1ddc5a5b4a49d838f14313dd0032c01c898dd353b1fc4f20ca0cca2845227ad`；schema helper=`b68b6dee7e7929a78ab231e3f990afbbb21b476f062e7bd034e31e0a118a8ab9`；successor=`bd13cab469cacda12233171ce448aadcc720934635644dfa70aed752d4c3e1c5`；测试=`62729e3c9df4978dd1edb1111901ae83302d58ef910898e7f47c52d29a68f76c`。

本地可申请新一轮独立审查；尚不具备注册/运行审批就绪条件。固定候选读取 NOT RUN，两次真实启动 NOT RUN，真实 schema/迁移 UNKNOWN，PG03 BLOCKED。未提交、推送、合并、注册/触发 Actions，未改生产 workflow/配置/Secrets/包权限/部署开关，未访问生产 SSH 或启动数据库。到此停止，等待负责人审阅；任何下一阶段均不自动执行。


## 2026-10-21 — 本轮自主推进后的状态（追加）

候选侧专用迁移凭证、控制校验和独立审查已形成待验证差异，详见 `pg03-migration-proof-source-review-20261021.md`；源码/测试 hash 与未构建的新候选身份见 `pg03-migration-proof-preparation-20261021.json`。历史 P2 发现及旧候选契约不改写。

最终离线结果：Linux 51/51；Windows 42 通过、9 跳过。独立审查发现的 uptime 两处遗漏已修复并复核。P2-1 继续为 Linux 本地 PASS；P2-2 及 PG03 仍 BLOCKED：Go/gofmt 未运行，真实三数据库及候选镜像/两次启动未执行，无真实成功凭证。原固定镜像不含新凭证代码，必须新构建；当前 adapter 仍绑定旧 RC，未注册或运行。后续最小条件与逐项外部操作授权见新报告；本轮无生产操作。

收尾新增阻塞（经固定 GORM v1.25.12 上游源码和独立审查确认）：Row/Rows 后置 Next/Scan 错误无法由 Trace 完整证明。现已在 proof 模式对所有此类查询保守锁存失败，避免误发成功；此护栏没有解决正常迁移的正向证明，真实路径预计仍拒绝。源码修复尚未闭环，应先在可测试环境使相关错误显式传播，不能删除护栏或直接把此差异称为已修复候选。详见新报告的独立审查和最小后续顺序。
