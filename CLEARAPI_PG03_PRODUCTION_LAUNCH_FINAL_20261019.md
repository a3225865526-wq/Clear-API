# ClearAPI PG03 生产发布 — 最终报告（2026-10-19）

## 结论

**PARTIAL — 真实生产部署未完成，线上验收未执行。**

不是 `PRODUCTION DEPLOYED — ONLINE ACCEPTANCE PASS`。

本轮把发布链路推进到**门禁全部真实通过**：schema 发布契约矛盾已修复，三份运营方证据已绑定并通过真实 verifier，手动 rehearsal `39189031358` **11/11 gate 全绿并真实生成 release-proof.json**。自动发布授权链的三处 `event` 断言也已修复并实测生效。

现在只剩两个**都必须由 VPS root 完成**的前置：重装两个已修改的部署脚本、生成 Runtime Integrity 探针会话（见 §8）。此外自动发布路径还被一个**持久熔断器**拦下——它由前两次授权器缺陷导致的失败触发，而那两次失败都停在授权步骤、部署步骤为 `skipped`，从未接触生产（见 §4.4）。

---

## 1. 关键 SHA 与身份

| 项目 | 值 | 状态 |
| --- | --- | --- |
| 本轮开始时远端 `production/main` | `536f56517e15d60bdae0ab54bfad07797afb3d10` | 实测 |
| **当前正式主线 `production/main`** | **`fd8f27641d49f6c5a5b5b6c30fac230d3c074cd1`** | 实测 |
| 本轮合并 PR | #59 schema 契约、#60 Executor 重钉、#61/#62 链路触发、#63 manual rehearsal、#64 部署 event、#65 授权器 event | 已合并 |
| Gateway Required CI（main push） | run `39177329530`，6/6 success | 实测 |
| **最终 Gateway RC** | **run `39177706410`，success** | 实测 |
| **最终 Gateway image digest** | **`sha256:9606af0f5c7e8b002cd80f4f7f108992304abbe663af89eb9e363114d054d6d0`** | 实测 |
| 最终 RC manifest SHA256 | `e6aa32f5b760f476b2d377583aafbacfb420cf70b937cf4f95a921dacf3b4b70` | 实测 |
| **全绿 Rehearsal** | **run `39189031358`，11/11 success，proof 已生成** | 实测 |
| Executor 源码提交 | `a45358ed…`（导入隔离 + 路由放行）；分支 tip `945fc3ee` 内容与之完全相同 | 已推送 |
| Executor 正式 RC | run `39143951438`，digest `sha256:722915a67271b8404ae6411c5763d3ff6a4f5bcadee1a14025e888441f03a4f8`，binary `b9222051f1275d0da003c04dfe20f9cc38180273d69d4ff1c0715cb4e05218f5` | 实测（按 digest 拉取并校验） |
| 部署时间 | 无 | 未发生 |

Gateway RC manifest 的 `source_commit`、`candidate_commit`、`oci_revision`、`workflow_commit` 四者均等于 `24f9bd48`，且 `source_ci_runs` 指向同 SHA 的成功 push CI `39150191907`。**产物身份与源码一致。**

---

## 2. 本轮修改（已合并）

### Gateway（PR #54，已合并进 main）

1. **`admitted()` 被写死为 `false`。** 任何导入账号都不可能被调度，`ResumeAccount` 永远返回 `ErrPoolNotAdmitted`。改为只信任 `SyncAccount` 从 Executor 账本投影出的持久 `run_status`。
2. **过期账本记录被当作授权。** `SnapshotHasTrustedCapability` 原本只比对 credential digest。现在要求 revision 一致、provider 一致、generation 非零，且身份与能力的 generation 相等；新增 `SnapshotAccountAdmitted` 提供按账号/平台过滤。
3. **重复导入被报成 Executor 故障。** `ImportCredential` 现将 HTTP 409 映射为 `ErrPoolConflict`。
4. **网页导入不接受 Codex CLI 凭证形状。** 现接受嵌套 `tokens` 的 `auth.json`，并归一化为 Executor 原生顶层 OAuth 形状。API key 字段、嵌套凭证集合、未知 provider 仍然拒绝；不会把 token 复制成 API key。

### Executor（另一仓库，已推送）

1. **`/v0/management/pool/import` 不在 service 角色白名单内。** 用有效 management key 实测：旧 RC `9ba0e5ff…` 返回 `403 pool_service_management_restricted`；新 RC `722915a6…` 返回 `400 incomplete credential`（说明请求已进入 handler）。仅此一项就使网页导入功能在 service 角色下完全不可用。
2. **导入的凭证落盘时没有写管理隔离栅栏。** 只有内存记录被标记 disabled，文件仍是原始字节，重启后可能重新引入未标记的可调度账号。现在先改写文件再登记记录。
3. **（已撤回，见下方更正）** 我曾声称"重启后 Executor 不再发布任何池模型"并加了启动时重新推导。**该诊断是错误的，改动已 revert**。真实结论是：启动时认证加载路径本来就会为每个已启用凭证调用 `registerModelsForAuth` → `PublishPoolModels`，重启后发布状态正常。详见 §2 的更正说明。

### 关于第 3 项与"`PoolDispatchFencing` 未转发"的更正

本轮我提出过两个后来被自己推翻的判断，均已在仓库中留痕、未用于任何结论：

- **`poolExecutor` 未转发 `PoolDispatchFencing` 导致派发被拒**：错误。`begin()` 断言的是内层 `ProviderExecutor`，它早已实现该方法。保留的转发方法无害，但不是解开 PG03 的关键。
- **重启后池模型不发布**：错误。在**同一份全新状态、同一份已围栏凭证、同一条运维序列**（发布策略 → promote → 授予模型 → 显式 enable）下做 A/B，然后重启同一容器：

  ```text
  带启动重新推导   : eligible=1, verified capabilities=[gpt-6-luna]
  不带该调用       : eligible=1, verified capabilities=[gpt-6-luna]
  ```

  两者完全一致。此前的"空注册表"证据无效，因为那次夹具里的凭证处于 disabled 状态，任何变体都只会返回空表，被误读成了缺陷。该提交已 revert，分支 tip `945fc3ee` 与已发布 RC 的源码 `a45358ed` **内容完全相同**（`git diff --stat` 为空），因此 RC `722915a6…` 仍是正确的配对产物。

  排查过程中还澄清了两个约束（都是我的测试脚本写错，不是产品缺陷）：`matchPoolGrantReason` 要求 `expires_at <= now+1800`，我最初用了 3600；promote 需要带上 pending 记录的 `generation`。

### 需要明确纠正的一点

我最初在 PR #54 正文中声称"`poolExecutor` 未转发 `PoolDispatchFencing` 导致每次派发被拒"——**这是错误的**。`begin()` 断言的是内层 `ProviderExecutor`，而它早已实现该方法。我保留了该转发方法和对应测试（无害且有价值），但它**不是**解开 PG03 的关键。已在 PR 中公开更正。

---

## 3. 实际执行的验证

### 构建与测试（真实执行）

Gateway，`golang:1.25.1` linux/amd64，`GOWORK=off`：

```text
go build ./...                                                  OK
go test -count=1 ./service/accountpool                          ok
go test -count=1 ./service/routechain                           ok
go test -count=1 ./router                                       ok
go test -count=1 ./controller -run 'TestLegacyRetirement|TestRuntimeIntegrity'   ok
```

Executor，`golang:1.26-bookworm`：

```text
go build ./...                                                  OK
go test ./internal/api/... ./sdk/cliproxy/...                   与未修改的 228ecbaa 完全相同的 4 个既有失败
```

那 4 个既有失败（`TestPoolGuardOrdinary400StopsBeforeSecondAccount`、`TestPoolGuardStreamConcurrencyAndSingleAttempt`、`TestPoolIdentityLeaseAndSnapshotBindCanonicalDigest`、`TestPoolStage1BActualProcessTrustRecovery`）在未修改的 `228ecbaa` 上逐一复现。**未修复，也未加入白名单掩盖。**

### 我在本轮造成并修复的一个真实回归

Required CI 正确拦下了 `TestPoolControlPlaneMasterChannelsAndAvailability`：`PUT /owner/pool/master` 返回 `409 POOL_NOT_ADMITTED`。根因是 router 测试夹具的 executor snapshot 为 `Revision:0 / Generation:0 / AuthorizationVersion:0`，被新的（有意的）revision 绑定规则正确判定为"非证据"。我**修夹具**（让它建模一个真实 revision），而不是放宽规则；`go test ./router` 恢复通过，未删除任何测试、未新增 baseline 条目。

### 完整链路实测（隔离环境，真实 OAuth，真实上游）

环境：`pg03-chain-20261019`，全新 PostgreSQL 17.6 + 新 Executor RC + 由**已验收源码**构建的 Gateway（二进制 `94090f3a…e4dd`，镜像内校验一致）。

- **Gateway → Model Routing → Pool → Executor → Provider** 全链路成功：两次真实 `POST /v1/responses` 返回 `200 completed`，上游 usage 为 `input 15 / output 8 / total 23`。
- **网页导入链路**：通过真实 `POST /api/owner/pool/accounts/import` 上传两个运营方提供的 OAuth 文件，返回 `imported:2`，两个账号均落为 `not_admitted`；Executor 侧文件确认 `disabled=true`、`management_disabled=true`（栅栏生效）。
- **计费**：单次请求恰好 1 条 consume log、1 次 `request_count` 递增，钱包与 token 增量与记录的 `quota` 完全一致；故意构造的失败请求**不扣费、不写日志**。
- **双账号与 fallback**：把 A 显式禁用后，Executor preflight 返回 `eligible_count=1`，请求由 B 完成；账号计数器 A=1（禁用前）/ B=2（禁用后两次），失败计数 0，`in_flight=[]`，无遗留租约。
- **重启持久化**：重启 Executor 后 revision 5、trust 5、两个 identity 的 generation 3、capability 均保持，栅栏状态保持，`in_flight` 为空。
- **导入 quarantine 恢复闭环（§C）**：账号在被栅栏期间一直 `not_admitted`；只有执行显式 `PATCH /v0/management/auth-files/status` 后才变 `available`；未被恢复的账号继续隔离。**隐式重新启用路径不存在。**

证据目录：`D:/AI/API/runtime/pg03-chain-20261019/`（含 `chain-acceptance-evidence.json`）。

### 正式 RC 与 rehearsal（本轮最终状态）

- Gateway RC `39177706410` 成功，manifest 身份自洽（见 §1）。
- **Deployment rehearsal `39189031358` 全部 11 个 gate 通过，并真实生成 `release-proof.json`：**

| Gate | 结果 |
| --- | --- |
| runtime_integrity | success |
| billing | success |
| executor_pairing | success |
| sqlite / mysql / postgres_upgrade_idempotency | success |
| replay_and_duplicate_charge | success |
| state_isolation | success |
| backup_restore | success |
| safe_rollback | success |
| gateway_executor_provider_usage_billing | success |

proof 内容（实测）：

```text
schema       clearapi.automatic-release/v1
source_commit fd8f27641d49f6c5a5b5b6c30fac230d3c074cd1
rc_run_id     39177706410
gateway_digest sha256:9606af0f5c7e8b002cd80f4f7f108992304abbe663af89eb9e363114d054d6d0
migration     in_place_startup_migration, startup_passes=2
              source 8922081b…  target 3c1c77f3…
```

### 三份证据是怎样"真实重做"的（不是复制旧 receipt）

- **backup**：用离线 GPG provider（`C:/Users/Chen/Documents/ClearAPI-Infra-Work/gpg`，Git-bundled GPG 需 MSYS `/c` 路径）解密真实生产备份 `20261016T193254Z`，解密后 SHA256 `c1ebff73…` 与历史记录一致；在隔离 tmpfs PostgreSQL 17.6 中 `pg_restore` 成功，**75 张表**，`schema_digest` 精确等于 RC 的 `source_schema_sha256`（`8922081b…`）；不变量检查：负余额 0、无效索引 0、未验证约束 0。
- **rollback**：同一恢复快照上用候选二进制启动两次。第 1 次收敛到 RC 的 `target_schema_sha256`（`3c1c77f3…`），第 2 次 schema **逐字节不变**（幂等）。`rollback_proven=false`、`production_restore_executed=false` 如实保留。
- **provider**：在 RC 源码上跑真实 Gateway → 路由 → 账号池 → Executor → 上游请求：HTTP 200 `completed`，上游 usage `15/8/23`，**恰好 1 条** consume log，钱包与 token 增量均等于记录 quota，`request_count` +1，9 项校验全为 true。

三份证据在**安装为 secret 之前**先经真实 `verify-rehearsal-input.py` 校验（backup/rollback/provider/executor 四项全 PASS），再触发 rehearsal。
| gateway_executor_provider_usage_billing | **failure：`provider rehearsal evidence is not configured`** |

因此 `assemble-release-proof.py` 拒绝生成 proof，`Automatic production release` run `39151614827` 被 skipped。**这是 fail-closed 的正确行为。**

---

## 4. 阻塞的演变（原本三个，本轮解除两个）

### 4.1 已解除：schema 发布契约矛盾

原契约自相矛盾：`assemble-release-proof.py` 硬编码 `migration.mode=no_schema_change`，随后又要求 `source_schema_sha256 == target_schema_sha256`；而 PG03 的 RC 排练**总是**执行一次真实的 legacy→target 升级，因此 `source != target` 恒成立，任何候选都无法产出 proof。

根因是这条断言对 PG03 的迁移方式判断错误：`production-deploy-v2.sh` 明确**禁止**外部迁移标志（`DEPLOY_MIGRATION_FLAGS` 非空即 BLOCKED），迁移由**候选镜像自身启动时**完成；RC 排练正是用"同一数据库上启动两次、第二次 schema 不再变化"来证明这次启动迁移是幂等的。

修复方式（PR #59，已合并）是**从 manifest 推导模式**，而不是假定一种：

- `source == target` → `no_schema_change`
- `source != target` → `in_place_startup_migration`，且仅在同时满足 PG03 排练契约时接受：`schema_version=pg03-schema-rehearsal-v1`、`startup_passes=2`、`source_commit` 绑定本候选，并且 proof 必须复现 manifest 的精确 source/target 哈希

**没有放宽任何门禁**。10 个手写负向用例全部仍被拒绝：

```text
proof claims no_schema_change but schema changed   rejected
proof omits migration                              rejected
proof migration source mismatch                    rejected
proof migration target mismatch                    rejected
only one startup pass                              rejected
startup passes missing                             rejected
wrong schema rehearsal contract                    rejected
rehearsal not bound to candidate                   rejected
target schema missing                              rejected
source schema missing                              rejected
```

### 4.2 已解除：证据无法绑定到 RC 的顺序死锁

运营方证据必须绑定到 RC manifest，而 manifest 只在 RC 完成后才存在；rehearsal 又由 RC 完成**自动触发**，所以它总在证据上传之前运行，必然报 `evidence is stale`。重跑也不行——发布控制要求 `run_attempt == 1`，而任何重跑都会递增它。实测两次（`39167097193`、`39169399416`）。

修复（PR #63、#64、#65，已合并）：为 rehearsal 增加 `workflow_dispatch` 入口，可以**对已完成的同一 RC** 重跑同一套 11 个 gate 与同一 assembler；部署侧三处 `event` 断言同步接受该入口。放宽的**只有**触发方式，所有身份断言不变：同一 workflow 路径、`main` 分支、精确候选 commit、`attempt == 1`、completed+success、同仓库、RC 仍必须是 `workflow_run`。入口本身也不信任输入：它先经 API 解析该 run id 并断言上述全部条件，之后才取值下载 manifest。

### 4.3 本轮实测结果：11/11 通过

证据绑定到当前 RC `39177706410`（`main = fd8f2764`），三份证据在安装前先经真实 verifier 校验：

```text
backup rehearsal identity   PASS
rollback rehearsal identity PASS
provider rehearsal identity PASS
executor rehearsal identity PASS
```

随后手动 rehearsal `39189031358` **全部 11 个 gate 通过并生成 release-proof.json**：

```text
backup_restore success   billing success   executor_pairing success
gateway_executor_provider_usage_billing success
mysql_upgrade_idempotency success   postgres_upgrade_idempotency success
sqlite_upgrade_idempotency success
replay_and_duplicate_charge success   runtime_integrity success
safe_rollback success     state_isolation success

migration = {"mode":"in_place_startup_migration", "startup_passes":2,
             "source_schema_sha256":"8922081b…", "target_schema_sha256":"3c1c77f3…"}
```

### 4.4 剩余的两个硬阻塞

**(1) 自动发布熔断器已永久打开，且无法自愈。** `automatic-release.py` 的熔断规则是：取 `automatic-deploy.yml` 最近一次**非 skipped** 的运行，其结论必须是 `success`，否则 `release circuit open`。当前历史是：

```text
39189520883 failure
39176488931 failure
（更早均为 skipped）
```

这两次失败都发生在**授权步骤**，真正的部署步骤是 `skipped` —— 即它们从未请求生产凭据、从未接触生产环境：

```text
failure  Authenticate automatic proof before production credentials
skipped  Controlled production deploy (exact digest)
```

其中 `39176488931` 的失败原因是我的授权器缺陷（`unexpected run event`），已由 PR #65 修复。但熔断器按设计是**持久**的：它只统计 `automatic-deploy.yml` 自身的运行，因此新一次运行读到的 `previous[0]` 永远是那条失败记录，条件在构造上无法满足；重跑又因 `GITHUB_RUN_ATTEMPT != 1` 被直接拒绝。我没有绕过它——绕过生产发布熔断器属于硬边界。

**(2) manual 路径被 VPS 前置条件挡住。** manual `workflow_dispatch` 不经过 proof、也不受上述熔断器约束，但仍需：`/opt/clearapi/secrets/runtime-probe-session`（只能由 root 经真实交互式登录 + MFA 生成），以及 VPS 上已安装脚本与本次 commit **逐字节相同**。本轮我修改了 `production-deploy-v2.sh`（重钉 Executor 到修复后的 `722915a6… / a45358ed…`）和 `pg03-release-control.py`，因此：

```text
production-deploy-v2.sh   repo b03679284eac818dd69ef50a537277939435fb06016546fff734b91161b19c72
pg03-release-control.py   repo b3389dc40d66fedb287d56304260178035ee817660d8af61d00aec84a50e0a07
```

这两个值必须与 VPS 上 `/opt/clearapi/bin/` 的已安装副本一致；`deploy-production.yml` 只做**校验**（`LOCAL_SHA == REMOTE_SHA`），不负责安装。重装必须以 root 完成。本环境对 `103.42.181.219` 无可用凭据：TCP 22 可达，但 `root@` 与 `clearapi-deploy@` 均 `Permission denied (publickey)`，本地无私钥，`PROD_SSH_PRIVATE_KEY` 只存在于 GitHub `production` environment。

### 4.1 第二条独立阻塞：Runtime Integrity 探针（manual 路径同样被挡住）

`assemble-release-proof.py` 只被 **automatic**（`workflow_run`）路径消费。理论上手动 `workflow_dispatch`（`pool_mode=pg03` + `DEPLOY_PRODUCTION`）不经过 proof。但实测最近 4 次手动部署：

| Run | 结果 |
| --- | --- |
| `39051474037` | `Runtime Gate {"status":"BLOCKED","code":"PROBE_UNAVAILABLE"}` → `FAIL: Runtime Integrity Gate BLOCKED before deployment` |
| `39050053489` | 同上 `PROBE_UNAVAILABLE` |
| `39044452129` | 同上 `PROBE_UNAVAILABLE` |
| `39043761386` | `FAIL: launch baseline: missing or invalid commercial pricing` |

`verify_runtime_gate` 需要 `/opt/clearapi/secrets/runtime-probe-session`，该文件只能由 `runtime-probe-session.py` 在 VPS 上以 root、通过**真实交互式 root 登录 + MFA** 生成（脚本强制 `os.geteuid()==0` 且 `sys.stdin.isatty()`）。因此手动路径同样需要 VPS root 交互，我无法在本环境完成。

**结论：两条发布路径各自被一个独立的、都需要 VPS root 交互的阻塞挡住，且 automatic 路径还额外被 schema 断言挡住。**

---

## 5. 为什么没有继续强行推进

以下三项都超出"在授权范围内做合理修复"的边界，因此按规则停止而不是绕过：

**(a) 绕过发布熔断器属于硬边界。** 熔断器的语义是"先调查上一次失败的自动部署，再恢复发布"。我已经调查清楚并给出结论（两次失败都是授权器缺陷、部署步骤被 skipped、生产从未被触碰），但**清除熔断需要一个明确的恢复动作**，而这个动作不由我决定——它要么是运维在确认无影响后接受一次成功运行，要么是修改熔断规则本身，两者都改变发布安全语义。

**(b) VPS 侧脚本重装需要 root，而我没有该访问权。** `pg03-release-control.py` 与 `production-deploy-v2.sh` 本轮被修改（Executor 重钉到修复后的 RC），必须在 VPS 以 root 重新安装，才能与 commit 逐字节一致。实测：`ssh clearapi-deploy@103.42.181.219` 与 `ssh root@…` 均返回 `Permission denied (publickey)`。

**(c) 生产凭据不在本地。** `PROD_SSH_PRIVATE_KEY` / `PROD_KNOWN_HOSTS` 只存在于 GitHub `production` environment，本地无法导出，也不应导出。

因此我没有"直接改生产数据库""手工篡改账号状态"或"绕过正式流程"去制造上线结果。

---

## 6. 已实测通过 / 仅静态审查 / 未验证 / 失败

### 已实测通过

- Gateway `service/accountpool`、`service/routechain`、`router`、`controller` 定向测试。
- Gateway 全链路一次真实请求成功，含 Usage 解析、余额扣减、单次结算。
- 失败请求不扣费、不写 consume log（**仅限非池通道**，见下方"新发现"）。
- 双账号 fallback：A 被排除后 B 完成，无重复结算、无遗留租约。
- Executor 重启后 revision / trust / capability / 栅栏全部保持。
- 导入 quarantine 的显式恢复闭环（无隐式复活路径）。
- Executor 导入路由在 service 角色下由 403 变为可达（新旧 RC 对照）。
- Gateway Required CI 6/6 通过；正式 RC 构建成功且 manifest 身份自洽。
- Rehearsal 中 8 个 gate 真实通过。

### 新发现（本轮补充实测，属既有设计而非本轮引入）

**池绑定通道的上游失败会保留预扣费、不退款。** 受控对照实验：

```text
池绑定通道 + 上游报错(400 unknown provider):
  token remain 499991 -> 499989 , used 9 -> 11 , consume log 不变      (扣了 2，无日志)
非池通道 + 上游不可达(400):
  token remain 499989 -> 499989 , used 11 -> 11 , consume log 不变      (未扣费)
```

用户侧 `used_quota` 在这两次都没变，只有 token 余额被扣。日志显示预扣费发生后既没有"返还"也没有"结算"，即**预扣费悬空**。

根因在 `relay/request_billing.go`（**本轮未改动此文件**，`git diff 353cd0a2..HEAD -- relay/` 为空）：早退退款分支带 `!channel.PoolChannelBound()` 条件，注释明确写着池/路由链要保留"保守核对路径"。所以这是**既有且有意的设计**，不是我的回归。

#### 本轮把这一项查清了：它是"待核对状态"，不是账务错误

我按任务要求只针对这一已知问题做最小核查，结论是**没有未解释的扣费差异**，实测链条如下（durable wallet 真实启用后）：

1. 失败请求后 `billing_reservations` 出现一行 `state=active, reserved=2, applied=2` —— 即预扣费被**显式记录**为待核对，而不是凭空消失。
2. 该笔对运营方**可见**：`GET /api/owner/finance/billing-reservations?scope=pending` 返回该条记录（`state: active, reserved: 2, applied: 2`）。
3. 提交退款决策 `POST /api/owner/finance/billing-reservations/1/decision`（`expected_state/applied/target` CAS + `execution_ended` + `refund_quota`）返回 `200`，`token_refund: credited`。
4. 余额**精确恢复**：`499987 → 499989`，`applied: 2 → 0`，状态 `active → reviewed`。

也就是说：扣费是**可追溯、可对账、可退款**的保守核对路径，而不是"扣了钱无记录"。这符合 `relay/request_billing.go` 的明文设计意图。

**仍需人工承接的部分**：这是**运营流程**要求——上线后必须有人定期处理 `scope=pending` 的待核对项，否则这些预扣费会一直悬着。**这属于流程约定，不是代码缺陷**，也正因如此我在 §11 把它记为 PARTIAL 而不是 PASS：不是"未解释的差异"，而是"需要运营流程兜底"。

同一路径下，**非池通道**的确定性失败实测**不扣费**（`499989 → 499989`），行为正确。

### 仅静态审查

- Gateway 与 Executor 的镜像 digest ↔ 源码对应关系（RC manifest 自证 + CI 绑定，我无法拉取网关私有包独立复核；Executor 已按 digest 实拉并比对二进制）。
- 生产 VPS 当前实际安装的脚本字节。

### 未验证

- 生产环境上的 PG03 启用、Route Chain 真实请求、账号池派发。
- 生产备份/回滚在生产路径上的实际执行。
- 生产侧 Usage / 余额 / 结算核对。

### 失败

- Deployment rehearsal `39151149989`：`backup_restore`、`safe_rollback`、`gateway_executor_provider_usage_billing` 三个 gate 失败。
- `assemble-release-proof.py`：因 `no_schema_change contradicts the RC schema transition` 拒绝出 proof。
- `Automatic production release` `39151614827`：skipped。

---

## 7. 风险评估

- **不需要回滚任何东西。** 生产未被触碰：没有部署、没有迁移、没有重启、没有改动生产数据库或账号状态。`https://www.clear999.com/` 全程保持可访问。
- **`production/main` 已前进到 `24f9bd48`**，其中包含 PG03 代码改动。这些改动在池功能未启用时不影响现有转发路径（属于 `service/accountpool`、`router` 池测试、`middleware` 池中间件作用域）。
- **旧 Executor RC 的导入缺陷仍然存在**于当前钉定的 `9ba0e5ff…` 上；在重钉之前，网页导入在 service 角色下不可用。这属于"未上线功能仍不可用"，不影响已在运行的业务。
- **既有 4 个 Executor 测试失败**与本轮无关，仍为未解决项。

---

## 8. 解除阻塞所需的最小动作（需人工决策/授权）

**只剩两步，且都必须在 VPS 上以 root 完成。** 原有的 schema 契约与证据绑定两个阻塞已在本轮解除。

1. **在 VPS 以 root 重装两个脚本**（使其与 `main = fd8f2764` 逐字节一致）：

   | 脚本 | 目标路径 | 期望 SHA256 |
   | --- | --- | --- |
   | `production-deploy-v2.sh` | `/opt/clearapi/bin/production-deploy-v2.sh` | `b03679284eac818dd69ef50a537277939435fb06016546fff734b91161b19c72` |
   | `pg03-release-control.py` | `/opt/clearapi/bin/pg03-release-control.py` | `b3389dc40d66fedb287d56304260178035ee817660d8af61d00aec84a50e0a07` |

   重钉内容是 Executor 从 `9ba0e5ff… / 228ecbaa` → `722915a6… / a45358ed…`（含 `EXECUTOR_BINARY`）与随之更新的 `V2_SHA`。安装时保持 root 属主、`0750`。

2. **生成 Runtime Integrity 探针会话**：以 root 通过官方交互式登录 + MFA 运行 `runtime-probe-session.py`，写入 `/opt/clearapi/secrets/runtime-probe-session`。该脚本强制 `os.geteuid()==0` 且 `sys.stdin.isatty()`，无法代跑。

完成 1、2 后即可运行 manual 部署路径（`deploy-production.yml`，`pool_mode=pg03`）完成上线与线上验收：

```text
commit  fd8f27641d49f6c5a5b5b6c30fac230d3c074cd1
digest  sha256:9606af0f5c7e8b002cd80f4f7f108992304abbe663af89eb9e363114d054d6d0
RC      39177706410（success）
proof   rehearsal 39189031358（11/11 通过，release-proof.json 已生成）
```

说明：自动发布路径除上面 1、2 外还需要**清除已打开的发布熔断器**（见 §4.4）。由于失败运行从未触碰生产，人工确认后接受一次成功运行即可恢复；这一步属于运维决策，我没有代为清除。

---

## 9. 本轮未做的、也不应做的

- 未伪造 `source_schema_sha256 == target_schema_sha256`；schema 契约改为**从 manifest 推导模式**并保留全部负向拒绝。
- 未清除或绕过自动发布熔断器（§4.4）。
- 未把失败的 gate 加入白名单，未删除或弱化任何测试；新增回归测试只增强约束。
- 未修改生产数据库、未手工篡改生产账号状态。
- 未绕过正式发布流程直接替换生产镜像。
- 未手工编辑任何 receipt 或 proof；所有证据由本轮真实执行产出，安装前均先通过真实 verifier 校验。
- 未执行 `git reset --hard`、`git clean`、强推或未经核实的工作区切换；两个工作区的未提交现场与验收证据均保留。

---

## 10. 证据位置

| 内容 | 路径 |
| --- | --- |
| 最终 RC manifest（`fd8f2764`） | `D:/AI/API/evidence/pg03-rebind-20261019/rc-39177706410/` |
| 全绿 rehearsal receipts + proof（11/11） | `D:/AI/API/evidence/pg03-rebind-20261019/rehearsal-39189031358/` |
| 真实备份解密 / 恢复 / 两次启动排练 | `D:/AI/API/runtime/pg03-rebind-20261019/` |
| 三份绑定证据（backup / rollback / provider） | `D:/AI/API/runtime/pg03-rebind-20261019/ev6/` |
| Provider 真实链路测量原始结果 | `D:/AI/API/runtime/pg03-rebind-20261019/provider-measurement.json` |
| 完整链路验收（隔离环境） | `D:/AI/API/runtime/pg03-chain-20261019/` |
| Executor RC manifest | `D:/AI/API/evidence/pg03-full-launch-20261018/executor-rc-39143951438/` |
| 历史 full-launch 证据 | `D:/AI/API/evidence/pg03-full-launch-20261018/` |

## 11. 最终分类

**PARTIAL —— 生产部署未执行，线上验收未执行。**

| 项目 | 结论 |
| --- | --- |
| PG03 代码修复与集成 | **已实测通过**（`fd8f2764`，Required CI 6/6） |
| 完整链路真实验证 | **已实测通过**（隔离环境、真实 OAuth、真实上游） |
| 双账号 / fallback / 无重复结算 | **已实测通过** |
| 计费（成功请求按上游 usage 精确扣费、单次结算） | **已实测通过** |
| 计费（失败请求）——非池通道 | **已实测通过**（未扣费） |
| 计费（失败请求）——池绑定通道 | **PARTIAL**：扣费但不写 consume log；实测确认为**设计上的待核对状态**（`billing_reservations.state=active`，运营方 `/api/owner/finance/billing-reservations` 可见，退款决策实测可将余额精确恢复 `499987→499989`），因此不是"未解释的扣费差异"，但需要运营流程承接 |
| 重启持久化 | **已实测通过**（未发现重启缺陷；先前错误结论已撤回） |
| 导入 quarantine 恢复闭环 | **已实测通过** |
| 正式 RC 产物身份（Gateway + Executor） | **已实测通过** |
| schema 发布契约 | **已修复并实测通过**（真实 RC 生成有效 proof；10 个负向用例仍全部拒绝） |
| 三份运营方证据绑定 | **已修复并实测通过**（三份均 PASS 真实 verifier，11/11 gate 全绿） |
| Rehearsal | **已实测通过**（`39189031358`，11/11） |
| Release proof | **已实测通过**（`in_place_startup_migration`，真实生成） |
| 自动发布授权链 | **PARTIAL**：三处 event 断言已修复并实测生效；但被持久熔断器拦下（§4.4） |
| 生产部署 | **未执行**（缺 VPS root 前置，§8） |
| 线上验收 | **未执行** |

**最终成功条件 `PRODUCTION DEPLOYED — ONLINE ACCEPTANCE PASS` 未达成。**
