# PG03 P2-2 — 候选侧迁移凭证与独立审查（2026-10-21）

状态：**P2-1 PASS（Linux 本地回归）；P2-2 BLOCKED；PG03 BLOCKED**。本轮已形成候选源码和控制脚本的待验证差异，并确认仍有 Row/Rows 后置读取错误无法完整观察的正向证明缺口；新护栏会保守拒绝成功。未取得真实迁移成功证据，不能称为源码正向闭环或候选验收完成。原始阶段记录、固定 RC 和历史证据保留。

## 真实执行路径与方案

实际入口为 `main.go: InitResources`：加载环境 → `model.InitDB`（master 主库迁移）→ 既有授权/设置初始化 → `MigrateRetiredFrontendOptions` → 选项加载 → `model.InitLogDB`。主库迁移包含 schema、种子和回填；同库日志模式仍执行 audit migration，独立日志库另有迁移路径。

原候选的 options 主键、VIP 种子、订单来源回填、price/model_limits 元数据、price DDL、旧前端选项坏值等路径可能记录错误后继续。普通 SYS 日志丢弃 writer 错误；健康、进程退出 0 和 DDL 不变均不能证明这些步骤完成。

采用候选内专用结构化凭证。仅设置 `CLEARAPI_MIGRATION_PROOF_CHALLENGE` 时启用；普通启动不创建文件，也不改变原 best-effort 返回语义。

- 开库前确认 master 和 64 位十六进制随机 challenge，独占创建 `.started` 文件；拒绝已有凭证、重复尝试或不新鲜路径。生产调用的路径固定为 `/tmp/clearapi-migration-proof.json`，没有新增 HTTP 接口或配置写入。
- 按主库 → 旧选项 → 日志库记录顺序完成；所有返回错误仍走原失败路径。对 best-effort 非 SQL 错误显式锁存失败；启用期间的 GORM logger 直接观察 Trace 的结构化 error 参数（排除正常 RecordNotFound），但 Trace 不涵盖 Row/Rows 返回后的 Next/Scan 错误；为避免误报，proof 模式在主库及独立日志库开库后注册 Row/Rows 回调，遇到任何此类查询无条件锁存失败，即使初始查询正常也拒绝凭证。Silent/Debug 不会关闭这些护栏；保留 ParamsFilter 委托。
- 只有所有阶段完成且没有失败锁存时才独占创建凭证，记录实际可执行文件 SHA256、hostname/PID、challenge、主/日志库类型和阶段。写、文件 Sync/Close、父目录 Sync/Close 任一失败均使 InitResources 失败，HTTP 尚未启动；保留残留文件，不删除旧证据。结束初始化时解除内存观察。
- 控制脚本在每次新 Gateway 启动前生成独立 challenge；健康后、停止前从该活容器有界读取凭证（tmpfs 停止后不可依赖其内容）。停止、取 schema 和日志后，严格校验 JSON 唯一键/字段/大小、challenge、默认容器 hostname、PID 1、已核验二进制 hash、三阶段和 PostgreSQL 主/日志类型。只在原有隔离单 PostgreSQL profile 接受此契约。
- 日志仅可否决成功；缺日志/缺凭证/损坏/重放/二进制不符均 UNKNOWN，已识别迁移错误或 schema 冲突为 failure。schema 不变不是凭证的替代品。原固定源码明确不能用新凭证分支放行。
- successor 的 observation 和普通 CLI 路径现在都强制使用迁移证据、候选身份和目标 schema；省略 observation 不再省略这些门禁。通用 schema helper 的旧 RC 默认调用语义没有升级为本次验收。

## 实际文件差异

候选生产源码：`main.go`、`model/main.go`、`model/frontend_option_migration.go`、`model/gorm_logger.go`，新增 `internal/migrationproof/proof.go`。

定向 Go 回归：新增唯一测试文件 `internal/migrationproof/proof_test.go`；扩展原 `model/gorm_logger_test.go`、`model/frontend_option_migration_test.go`。覆盖顺序/失败锁存/不新鲜路径/发布失败/真实二进制 hash/默认关闭、Silent 结构化错误、健康 Row 也必须拒绝证明和关闭连接后的吞错查询、坏 ApiInfo/缺 slug/非法 uptime URL。**这些 Go 测试尚未运行或编译。**

控制与集中离线回归：`scripts/pg03-schema-rehearsal.py`、`scripts/pg03-successor-plan.py`、`scripts/ci/test-pg03-successor-plan.py`。复用已有 46 项，再增加 5 个测试方法（含参数表），覆盖正常逐次凭证、重放/缺阶段/错误身份/重复键/损坏/超限、日志缺失及冲突、任一次缺凭证后的停止清理；保留 P2-1 发布失败覆盖。所有 Docker/数据库调用都是替身。

本轮没有修改任何 workflow、adapter 固定候选常量、Go/前端依赖或锁文件。完整源码/测试 SHA256 与未选定的新候选身份见 `pg03-migration-proof-preparation-20261021.json`；其 working_head 是工作区 HEAD，不是修复后的候选源码提交。

## 运行记录与限制

| 检查 | 实际结果 |
| --- | --- |
| `python -B scripts/ci/test-pg03-successor-plan.py` | 最终 51 tests OK：42 通过、9 项 Linux 持久化相关 skip |
| `wsl.exe -d Ubuntu --cd /mnt/d/AI/API/worktrees/clearapi-controlled-launch-20261020 -- python3 -B scripts/ci/test-pg03-successor-plan.py` | 最终 51 tests OK，无 skip；P2-1 本地发布回归继续通过 |
| 3 份改动 Python AST；11 份源码/测试 UTF-8、尾空白；`git diff --check` | PASS；git 仅提示既有 CRLF/LF 规范化警告 |
| `Get-Command go,gofmt -ErrorAction SilentlyContinue`、常用 Windows/WSL 路径核查 | 未发现可用工具链；Go 编译、测试、gofmt NOT RUN。没有安装工具或依赖 |
| `docker image ls --format '{{.Repository}}:{{.Tag}} {{.ID}}'` | 失败：Docker Desktop Linux named pipe 不存在；未启动/重配 Docker 服务或切换远端 context |
| SQLite/MySQL/PostgreSQL 真实数据库矩阵、独立日志库、完整启动/升级 | NOT RUN，实际引擎版本 N/A；不声称数据库兼容或迁移安全已验证 |
| 镜像读取/身份核验、新候选构建、第一次/第二次真实启动 | 全部 NOT RUN；真实 schema/迁移 UNKNOWN，无真实成功 receipt |
| actionlint/shellcheck、完整 CI、真实 runner/崩溃或断电测试 | NOT RUN；workflow 本轮未变，沿用原静态结果，不重复声称真实执行 |

初次新增 Python 回归有 3 个错误：测试替身把 binary probe 的 `/new-api` 参数误当 Gateway entrypoint，读取不存在的 challenge 导致 StopIteration。改为精确检查 entrypoint 值后通过。收紧 successor CLI 后再次运行两平台回归，结果如上；没有无新假设重复失败命令。

## 独立审查

由独立子代理 `review_migration_proof` 只读审查实际差异。发现一项 P2：uptime 迁移缺 URL/slug 及校验失败两处 return nil 未锁存失败。已补两处 Fail，并增加坏值回归；断言完成文件不存在，避免 Windows 目录 Sync 不支持使测试误通过。审查者随后确认修复、Begin/End 生命周期、ParamsFilter 结构接口及 successor CLI 收紧，未发现剩余确定缺陷。

收尾继续核查时，主代理指出并经独立审查确认第二个实质缺口：GORM v1.25.12 的 RowQuery 可把 *sql.Row 返回给调用方，其 Trace 当时没有 error；HasTable/HasColumn/HasIndex/HasConstraint 后续 Scan 的 error 又可能被布尔接口吞掉。检查 Row.Err 也不能证明未来 Next/Scan 不出错。已增加上述无条件 Row/Rows 拒绝护栏，独立复核未发现护栏新增问题；**正向完成证明仍未闭合，不因护栏存在而认为此缺口已解决**。

固定版本上游源码已通过 agent-reach 的 gh API 只读核实：[RowQuery](https://github.com/go-gorm/gorm/blob/v1.25.12/callbacks/row.go)、[迁移布尔检查](https://github.com/go-gorm/gorm/blob/v1.25.12/migrator/migrator.go)、[Row callback](https://github.com/go-gorm/gorm/blob/v1.25.12/callbacks.go)。没有修改/升级 GORM。由此可推断正常真实迁移也可能触发保守拒绝；尚未实际运行，不把该推断称为运行结果。

审查没有执行 Go 或真实数据库测试，不能替代这些门禁。未将主代理的替身测试记为真实迁移验收。

## 新候选与真实演练准备

原候选保持原样：source `69a66431646b5cc8912bf97a739e08de07316443`，image `ghcr.io/a3225865526-wq/clearapi@sha256:07fd58272b4fc62bcc52a3ad1d20e0dac357ba93de9e91a6a797d5e8fe3e8e7a`，RC `39357150402/1`。它不含上述 Go 凭证生产代码，**必须使用新源码提交重新构建候选**；禁止在原 digest 下替换二进制或把控制脚本生成的数据归给旧候选。新 source/digest/image ID/binary/RC/artifact/ZIP hash 仍 UNKNOWN（清单使用 null），没有伪造占位 hash。

最小后续顺序（不是执行授权）：

1. 提供已获准的 Go >=1.25.1 验证环境，或另批安装工具链；先在可测试环境中让相关迁移查询的读取错误显式返回并传播，解决 Row/Rows 正向证明缺口，不能删除护栏求通过或靠 Row.Err 假定完整。不要直接把当前差异构建成“已修复候选”。随后格式化 8 个改动 Go 文件。待运行定向命令：`go test -count=1 ./internal/migrationproof ./model -run 'TestMigrationProof|TestRetiredOptionsCannotPublishCompletion|TestMigrateRetiredFrontendOptions|TestGormLoggerEndToEndSanitizedOutput'`。
2. 在已证明隔离、仅合成数据的 SQLite/MySQL/PostgreSQL 执行真实矩阵，记录版本和命令；包括默认关闭与凭证开启、真实主库及独立日志库失败、fresh/代表升级库、连续两次启动。可复用 `TestMigrationSchemaStability`，但它不是完整启动凭证证明，不能单独替代本项。缺环境不作 PASS。
3. 上述通过并复审后，分别核实/取得提交、推送和 RC 构建的明确授权。构建身份以最终提交和新 RC 为准，保留旧 RC。现独立 adapter 仍固定旧候选；取得新 RC 后才核验并改固定常量，重新检查差异、触发图、开关和 required checks。
4. 默认分支注册及一次真实隔离运行仍分别待明确批准，窗口须固定控制 ref、runner、包读取路径、精确镜像和合成 DB 写入范围。按现有 nonce 所有权、network none/独立 tmpfs/不发布端口/无生产挂载的隔离契约执行，身份核验后才能启动两次。原 RC 目标 DDL 只是历史基线；新候选目标和本次 schema 语义仍须单独核实。

未提交、推送、合并、注册或触发 workflow，未获取镜像/创建数据库或容器；未读取凭据值、修改生产配置/权限/Secrets/开关、连接生产 SSH、开放入口或执行生产部署。当前停止于无法可靠证明全部迁移读取结果、缺少验证环境及后续外部操作授权边界；不扩大数据库驱动/SQL重写来猜测修复，不自动进入生产。
