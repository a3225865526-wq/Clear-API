# CLEARAPI — 全自动 CI/CD 与无人值守生产部署改造报告

日期：2026-10-15（Asia/Shanghai）

**总体状态：PARTIAL / PRODUCTION AUTOMATION NOT ENABLED。** 已实现并验证严格 CI、受控自动合并、自动 RC 入口和自动部署证明消费端；真实部署演练证明生成端、PG03 生产合同及恢复闭环尚未完成。不能宣称已经建立完整无人值守发布系统。

## 工作区、提交与安全边界

- 仓库：`a3225865526-wq/Clear-API`，远端名 `production`；`origin` 是上游 QuantumNous/new-api，未向其推送。
- 独立 worktree：`D:/AI/API/worktrees/clearapi-auto-cicd-20261015`。
- 分支：`codex/clearapi-auto-cicd-20261015`。
- 创建及交付前核对的 main：`0406a48abd6c723575e5c54e1423e91db10eb926`；PR #38、#39 的合并提交已在历史中。
- 实现提交：`7963fb58abf1e296fd2f641fab2d4800a3d0e3b1`；本报告单独提交。
- 基线 CI：run `38751555259`，真实 GitHub 查询为 completed/success；这不是本次分支的 CI 结果。
- Codex A 工作目录未写入、切换、提交或清理；其已有和新增未跟踪报告均保留。
- 未连接 VPS、未执行生产部署、未更改 production 设置、未合并任何 PR、未推送 main；未安装或升级项目依赖、未修改锁文件。
- 临时及证据目录：`D:/AI/API/runtime/clearapi-auto-cicd-20261015/{tmp,artifacts}`。
- 测试使用独立容器和网络 `clearapi-auto-cicd-20261015`；两个测试数据库容器已停止并保留，不删除用户环境。前端检查只读取已有依赖，生成文件留在本 worktree。

## 1. 原有全部已识别人工确认点

| 确认点 | 原行为 | 本次结果 |
|---|---|---|
| 合并 PR | 人工 Merge | 新增受控自动合并工作流，默认未启用 |
| RC 构建 | 两个 RC 工作流仅 workflow_dispatch | 正式 PG03 RC 增加成功 main CI 的自动入口 |
| 生产作业触发 | 人工 Run Workflow / Deploy Production | 新增真实演练完成后的自动调用入口，默认未启用 |
| 部署参数 | 人填 digest、commit、migration_compatible | 自动入口从认证产物和版本化证明推导；拒绝可变标签 |
| 输入确认 | 人工键入 DEPLOY_PRODUCTION | 仅手动应急入口保留；自动入口必须通过证明校验 |
| production 审批 | required reviewer 为仓库所有者 | 保留；列出后续一次性正式调整 |
| 脚本安装/身份 | 服务器安装的受审脚本必须与源码一致 | 未绕过、未自动覆盖；继续阻断不匹配 |
| 账务迁移批准标志 | 迁移程序要求单独批准及 DSN | 未伪造或取消；自动策略目前仅接受无 schema 变化 |
| Legal/Product 等批准记录 | 现有业务合同中的人工批准 | 未改写；不属于技术发布成功常量 |

production 当前没有发现 Wait Timer 或自定义审批规则，只有 required_reviewers 和 branch_policy；不表示其他组织级策略已被审计。

## 2. 已改造成自动化的代码

- `automatic-merge.yml`：仅处理同仓库成功 PR CI；只执行受信 main 中的策略代码，不执行 PR 代码。仅接受所有者提交的 `codex/` 分支，绑定当前 head、当前 main 祖先关系、完整文件列表和最新 CI attempt。冲突、过期分支、未知路径及敏感文件均阻断。
- 合并通过 GitHub App 短期令牌调用正常受保护 merge API，使用 `--match-head-commit`，没有 `--admin`、force push、保护绕过或人工自批。不排队保留可沿用至未来提交的自动合并授权。仓库原生 `allow_auto_merge=false` 可以保持不变。
- 初版低风险范围仅包括非敏感公共展示组件、文档页面、语言资源和文档。后端、账务、权限、迁移、运行身份、部署、依赖和政策等变更不能自动证明安全，明确阻断；更强自动证明策略尚未实现。
- `release-pg03-gateway.yml`：成功 main push CI 后自动构建的入口，源码取完整 SHA，拒绝过期 main，检查 CI 中全部 job 成功以及 DB qualification；保留真实 linux/amd64 构建、OCI revision、镜像 push/pull digest、二进制回读校验及 Manifest。发布前再次核对 main，并增加产物 SHA256 清单。
- `automatic-deploy.yml`：等待名为 `Deployment rehearsal` 的真实演练工作流成功，再调用原生产部署流程。**该演练工作流生成端尚未实现，当前不会形成可用生产链路。**
- `deploy-production.yml`：凭据之前独立完成证明认证；保留手动应急入口。自动部署检查当前 main、旧运行版本、脚本完整性，再复用现有 Deploy V2；没有删除原 Runtime/Billing/Backup/Manifest 断言。

## 3. 一次性 GitHub 管理员配置清单

本轮身份具有 admin 权限，但以下配置**均未执行**。必须先完成第 8、11 节，再正式启用；不能只改开关。

1. 在 PG03 首次正式上线完成后，按现有分支保护正常审核本次政策与工作流变更。不得由本自动化合并自身。
2. 保留 main 当前三个 required checks：`Backend vet, build, test, migration binaries`、`Frontend typecheck and test`、`Secret scan and forbidden files`；保留 strict、enforce_admins、conversation resolution，以及禁止 force push/删除。Rulesets 当前返回空数组。
3. 创建仅安装到 Clear-API 的 GitHub App，不设置 branch/ruleset bypass。最小权限：Contents write、Pull requests write、Actions read、Administration read、Metadata read。Administration read 用于核查保护规则。App 不能取得 production 密钥。
4. 仓库变量 `CLEARAPI_MERGE_APP_ID`，仓库 secret `CLEARAPI_MERGE_APP_PRIVATE_KEY`。使用固定 SHA 的官方 create-github-app-token action 签发并自动撤销短期令牌，避免 GITHUB_TOKEN 合并后不触发 main CI 的问题。
5. 确认 squash merge 可用（本轮已查为 true）。不必打开原生排队 auto-merge。
6. 设置经过真实核验的仓库变量 `CLEARAPI_EXECUTOR_IMAGE_REPOSITORY`，只允许所有者的 GHCR 仓库；证明中的 Executor 源仓库必须为 `a3225865526-wq/CLIProxyAPI`。不得填单元测试 fixture 镜像。
7. 完成可信 `deployment-rehearsal.yml` 生成端及隔离环境配置，产物名为 `deployment-proof-<run_id>-<attempt>`。下节列出合同；禁止把本地合成测试数据上传为发布证明。
8. 演练通过后，正式调整 production：reviewers `[]`、wait_timer `0`、can_admins_bypass `false`、deployment_branch_policy 保持 protected_branches `false` / custom_branch_policies `true`；保留现有唯一的 **branch 类型 main** 策略，禁止其他分支和标签。当前 reviewer 为所有者、can_admins_bypass=false、唯一 main 分支策略均已通过 API 核对。
9. 保留 `PROD_SSH_PRIVATE_KEY`、`PROD_KNOWN_HOSTS` 在 production environment 内，以及原 `PROD_SSH_HOST/PORT/USER`。不转移生产凭据到普通 PR 作业；不关闭主机指纹校验。
10. 在仓库 Variables 中按阶段设置 `CLEARAPI_AUTO_MERGE_ENABLED=true`、`CLEARAPI_AUTO_RC_ENABLED=true`；只有完整生产前置条件通过后才设置 `CLEARAPI_AUTO_PRODUCTION_ENABLED=true`。默认未设置即关闭。紧急停止设回 false，并检查失败熔断。

对应设置页面：Repository Settings → Apps / Secrets and variables → Actions / Branches / Environments → production。修改环境时仅提交已核对字段，保留其他现存设置；重新 GET 核对分支限制和保护项。

## 4. AGENTS.md 政策变更

新增 `Proposed unattended release policy v1`：明确受保护的精确 head 合并、严格 CI 后 RC、版本化发布证明、真实门禁、失败停止，以及禁止绕过和自批。现有保护政策和项目身份归属未删除。

新条款只有正常审核合并、真实隔离演练通过及管理员独立激活之后生效。本轮不将提出的政策视为已经审核生效。

## 5. CI 验证缺口与实际结果

| 项目 | 实际验证/代码改动 | 状态 |
|---|---|---|
| 根模块 Go vet | `GOWORK=off go vet ./...`；修复六处标签缺空格、财务测试四处未具名字段 | PASS |
| 独立 relaykit vet/build | 独立 vet 被离线缓存缺少 `golang.org/x/text v0.35.0`、`github.com/tidwall/pretty v1.2.0` 阻断；未安装依赖、未改变版本 | BLOCKED；独立 build 未完成 |
| 根模块全包测试 | `GOWORK=off GOFLAGS=-p=2 go list -e ./...` 排除根 main 包，再 `go test -count=1`，全部包退出 0 | PASS；可选测试内的 skip 不等于真实外部环境已验证 |
| 前端 typecheck | 已有 native tsgo `-b web/tsconfig.json`，版本 `7.0.0-dev.20260707.2`，退出 0；CI 改为真实 `bun run typecheck` | 本地 PASS；非 fresh frozen-lock install 复现 |
| 前端 vitest/build | 本轮未运行；工作流保留并增加完整 typecheck | NOT_RUN |
| 历史豁免 | CI/RC 不再调用放行包装器；旧包装器改为原样传递退出码；注入历史测试名的 exit 7 实际返回 7 | PASS，历史名单仅保留为记录 |
| 三个旧豁免测试 | controller HTTP health、router video 直接通过；relay 转换测试补预约夹具后通过，不关闭实际 dispatch guard | PASS |
| 标签兼容性三数据库 | `go test -count=1 -v ./model -run 'TestMigrationSchemaStability/.*/billing_auth_snapshot_tag_compatibility'` | PASS |
| Gateway/Route Chain/Mock Provider/Usage/Billing | `go test -count=1 ./e2e/routechain/...`；当前 HTTP mock harness | PASS；不等于真实 Executor 镜像联调 |
| 完整已发行版本升级/启动幂等 | 尚未接入通用真实发行版本升级证明 | NOT_COMPLETE |
| 自动发布策略 | `python scripts/ci/test-automatic-release.py -v`，13 项，含篡改、缺门禁、错误配对、过期 head、失败熔断和拒绝重跑 | PASS；全部为明确合成的离线策略测试 |
| Deploy V2 离线回归 | Linux 容器内 `python3 scripts/ci/test-production-deploy.py`，79 项，77 通过、2 跳过（真实 producer artifact 未请求） | PASS_WITH_SKIPS |
| Backup Identity | `python3 scripts/ci/test-backup-identity.py`，15 项，13 通过、2 加密用例失败 | FAILED：容器 GnuPG 2.2.40 不支持 `--force-ocb`；未弱化加密 |
| YAML/Python/shell | 全部工作流 YAML 解析、嵌入 shell `bash -n`、Python AST 检查 | PASS；未运行 actionlint |
| Secret scan / diff | staged 文件执行 `bash scripts/ci/secret-scan.sh`、`git diff --cached --check` | PASS |

本地 Go 为现有 builder 中的 **Go 1.26.1 linux/amd64**；CI 使用 go.mod 声明的版本。本轮没有独立证明 Go 1.25.1 的完整运行结果。

真实数据库版本：SQLite **3.50.4**、MySQL **8.4.11**、PostgreSQL **17.6**。测试网络不发布数据库端口。新增测试对六处标签修复覆盖旧 main `0406a48a` 的精确标签形式、新建/升级各两次迁移、无重复 DDL、原数据、唯一约束和 JSON 语义；这不是整站从最新发行版本升级的替代品。该模型不属于独立日志库路径。

另以真实 GitHub API 对旧 main CI 执行新 `source` 策略，最终明确返回 `RELEASE BLOCKED: missing release DB qualification`，证明旧的三个绿色检查不会被冒充为新的完整发布资格。此前一次网络请求失败未计为该行为通过。

## 6. RC 构建自动化结果

自动入口代码已实现，关闭状态；本分支未生成或发布新的镜像、digest 或 Release Manifest。已有其他聊天生成的 main RC 不能作为本改造的通过证据。

要求 main push CI 所有实际 job 都成功、不接受跳过/取消作业，并包含 DB qualification；本次将 race、真实三数据库账务测试及 Route Chain mock E2E 接入 CI 的 reusable release validation。RC 的源码、镜像、OCI revision 和二进制身份校验沿用真实构建路径。

## 7. Deploy V2 自动化结果与证明合同

实现的是**可信证明消费端及原部署执行器复用**，不是已经完成的生产自动部署。

- 证明版本：`clearapi.automatic-release/v1`；单门禁证据版本：`clearapi.gate-evidence/v1`。
- 证明必须绑定 source SHA、RC run ID/attempt、Gateway digest/binary SHA256、Executor 源仓库/镜像仓库/source SHA/digest/binary SHA256、配对 Gateway SHA、精确前一生产提交和迁移模式。
- 必需证据：Runtime Integrity、Billing、Backup Restore、Executor pairing、SQLite/MySQL/PostgreSQL upgrade/idempotency、Gateway→Executor→Provider→Usage→Billing、重放/重复扣费、状态隔离、安全回滚。
- 每份证据绑定 gate 名、同一源码和 Gateway digest、成功结果及文件 SHA256。GitHub API 验证产物生产者为同仓库受信 main 的成功完整工作流，拒绝旧 attempt、旧 RC、旧演练、旧 main、跨仓库产物及不完整差异列表。
- 消费端不生成 `approval=passed`，不把业务批准记录替换成技术成功；真实证明生成端缺失时不能取得发布许可。
- 当前自动策略仅允许 `no_schema_change` 和已知低风险差异；破坏性迁移和敏感业务修改一律阻断。
- 原 Deploy V2 的固定脚本身份、Runtime/Billing/Backup/Manifest 和旧版本检查均保留。脚本目前要求 Executor 不存在、Pool 关闭，这与 PG03 配对上线合同不一致，必须等待主线正式明确，不能删除断言解决。

## 8. 无人值守部署演练结果

**NOT_COMPLETE / NOT_RUN（真实镜像部署链路）**。

已经完成离线授权链和既有部署回归，不能等价为真实 Gateway/Executor 镜像、完整数据库升级、备份恢复、生产镜像替换和健康验收。尚无受信 `deployment-rehearsal.yml` 为当前 SHA 生成真实证明；自动生产工作流等待该生成端，不能仅开启变量即部署。

## 9. 故障、回滚与告警

- 自动发布失败/取消后，通过 GitHub 历史 run 状态持续熔断，拒绝下一次自动发布和同一 run 重跑；不会无限重试。恢复尚需受审维护变更和全新真实证据，没有无人值守自动复位机制。
- 授权失败日志保留为 Actions artifact，作业摘要明确 BLOCKED；失败部署保留原 Deploy V2 audit 和 Actions 日志。
- 当前 legacy Deploy V2 不能可靠区分普通健康失败与 Runtime/Billing 身份异常。因此自动调用保守传入 `migration_compatible=false`，**所有失败停止，不自动回滚**；原手动受控入口的回滚机制仍保留。分类后安全自动回滚尚未完成。
- 没有自动恢复数据库。账务/身份异常不能通过重试放行。
- 已加入 GitHub 失败摘要，可使用 Actions 原生失败通知；未验证所有者通知偏好和实际收件，也未接入或测试独立维护者告警渠道，不能声称告警送达已通过。

## 10. 生产自动部署是否启用

**否。** 没有修改 GitHub repository settings、branch protection、rulesets、environment、reviewers、wait timer、variables、secrets、VPS 或生产进程；没有触发任何发布/部署作业。

## 11. 剩余阻塞及解决方式

1. **真实演练证明生成端未实现。** 需要按受审 PG03 Gateway/Executor 合同构建隔离环境，实际执行所有门禁并产出版本化证据，不能使用测试 fixture 或历史桥接二进制代替当前镜像身份。
2. **PG03 部署合同与旧脚本不匹配。** 等 Codex A 首次正式上线完成并形成可信合同后，再开发兼容的真实演练及部署校验；当前不修改其工作区或 VPS，不合并本分支。
3. **整站发行版升级/幂等证明未闭环。** 需要可信已发行版本数据库快照，在三个真实引擎上升级及重复启动，并验证数据、约束、索引以及适用的日志数据库；本次标签兼容测试只覆盖本次模型改动。
4. **备份恢复与独立模块验证受环境限制。** 在已批准的、具有支持现有加密选项的 GnuPG 和完整锁定模块缓存环境重跑；不改变锁文件、不禁用加密、不接受默认 PASS。
5. **安全自动回滚和告警送达未闭环。** 需要故障分类、兼容性证明、真实回滚/恢复演练和接收渠道验证；现阶段选择停止。
6. **GitHub App 与生产一次性设置未做。** 按第 3 节实施，必须在真实演练之后才启用生产开关。敏感变更更强策略仍未实现，不能声称所有开发改动都可自动合并。
7. **未创建 PR。** 当前没有与本次方向性改造匹配的 Issue；现有 Issue #37 是 PG03 功能整合，不冒用它作本政策调整的批准记录。项目所有者普通模板 `.github/PULL_REQUEST_TEMPLATE.md` 要求“改动较大或方向性变更，请先在关联 Issue 中与维护者达成一致，再提交 PR”。本轮允许 Commit/Push/PR，但未明确要求新发 Issue，因此不代发、不伪造事实、不打开不满足门槛的 PR。实现以独立分支和报告交付，后续需完成该正式审核入口。

## 证据与复核入口

证据目录 `D:/AI/API/runtime/clearapi-auto-cicd-20261015/artifacts` 中包含：`go-vet.log`（修复前）、`go-vet-root.log`、`go-vet-fixed.log`（独立模块离线阻塞）、`snapshot-three-db.log`、`legacy-regressions.log`（修复前）、`converted-adaptor-fixed.log`、`root-full-tests.log`、`routechain-e2e.log`、`frontend-typecheck.log`、`policy-tests.log`、`deploy-wrapper.log`（夹具适配前）、`deploy-wrapper-fixed.log`、`backup-identity.log`、`live-source-policy.log` 和各退出状态记录。

测试中的合成 receipt 和 image 名只存在临时测试目录，从未作为生产证明上传。所有结果按本轮实际观察区分通过、跳过、失败、未运行及未完成；本报告不宣称生产发布成功。
