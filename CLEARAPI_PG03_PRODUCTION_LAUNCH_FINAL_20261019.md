# ClearAPI PG03 生产发布 — 最终报告（2026-10-19）

## 结论

**PARTIAL — 真实生产部署未完成，线上验收未执行。**

不是 `PRODUCTION DEPLOYED — ONLINE ACCEPTANCE PASS`。

本轮把 PG03 从"代码未集成、链路未验证"推进到"代码已合并进 `production/main`、完整链路已在隔离环境用真实 OAuth 账号和真实上游实测通过、正式 RC 已构建"，但**自动发布契约本身存在一个结构性矛盾**，使 release proof 无法生成，Deploy V2 无法被授权。该矛盾不是代码 bug，而是两条发布规则互相冲突；解除它需要一次发布策略决策（见 §8），不属于可在无授权下自行改写的范围。

---

## 1. 关键 SHA 与身份

| 项目 | 值 | 状态 |
| --- | --- | --- |
| 本轮开始时远端 `production/main` | `353cd0a23b723f86c6b21e759a6d691ce1ba5457` | 实测 |
| 合并后正式主线 `production/main` | `24f9bd481f354409291ef8e72350bc4d5daadabb` | 实测 |
| Gateway 集成 PR | #54，head `01fca228419edf1acfd37b9cd99ae6bbd248c38b` | 已合并 |
| Gateway Required CI（main push） | run `39150191907`，6/6 job success | 实测 |
| Gateway 正式 RC | run `39150754912`，success | 实测 |
| Gateway image digest | `sha256:2e730c638a0d32215de0616b04d8c20a609cc9bd0610cdd14d5443cfaada2c21` | 实测 |
| Gateway 二进制 SHA256 | `003cfd5da46f8a0776c4b718c75d2bc84768f912bc117d59dd0603399127a0e4` | RC manifest 声明 |
| Executor 源码提交 | `a45358ed…`（导入隔离+路由放行）/ `6a85ae78…`（重启发布修复） | 已推送 |
| Executor 正式 RC | run `39143951438`，digest `sha256:722915a6…a4f8`，binary `b9222051…18f5` | 实测（本地按 digest 拉取并校验） |
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
3. **重启后 Executor 不再发布任何池模型。** service 角色的池只从 trust/capability 提交处发布模型，启动时没有任何路径重建发布状态，导致重启后 `GET /v1/models` 为空、Gateway 报 `unknown provider`，直到运维再改一次策略。已加启动时重新推导（只读已提交状态，不提升信任、不授予能力）。

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

### 正式 RC 与 rehearsal

- Gateway RC `39150754912` 成功，manifest 身份自洽（见 §1）。
- Deployment rehearsal `39151149989` **失败**，各 gate 实测结论：

| Gate | 结果 |
| --- | --- |
| runtime_integrity | success |
| billing | success |
| executor_pairing | success（`executor rehearsal identity PASS for 24f9bd48… sha256:2e730c63…`） |
| sqlite / mysql / postgres_upgrade_idempotency | success |
| replay_and_duplicate_charge | success |
| state_isolation | success |
| backup_restore | **failure：`backup rehearsal evidence is stale`** |
| safe_rollback | **failure：`rollback rehearsal evidence is stale`** |
| gateway_executor_provider_usage_billing | **failure：`provider rehearsal evidence is not configured`** |

因此 `assemble-release-proof.py` 拒绝生成 proof，`Automatic production release` run `39151614827` 被 skipped。**这是 fail-closed 的正确行为。**

---

## 4. 唯一真正的硬阻塞（已实测证明）

自动发布契约**在当前形式下不可能被满足**，原因是两条规则互相冲突，而不是缺少某份可补的证据：

1. `scripts/ci/assemble-release-proof.py` 硬编码 `"migration": {"mode": "no_schema_change"}`；
2. 同一脚本随后要求 `manifest.schema_transition.source_schema_sha256 == target_schema_sha256`；
3. 但 PG03 的 RC 工作流（`scripts/pg03-schema-rehearsal.py`）**总是**执行一次真实的 legacy→target 升级排练，因此 `source != target` 恒成立。

我用**真实 assembler + 真实 RC manifest + 全部通过的 receipts** 直接验证：

```text
$ python3 scripts/ci/assemble-release-proof.py --manifest <real RC manifest> --receipts <all-pass> ...
no_schema_change contradicts the RC schema transition
（未生成 proof.json）
```

真实 RC 的实际值：

```text
source_schema_sha256 = 8922081b8513c5d810b4eca8226ec59a2093000be1c54eda10cd3b188c9da0ef
target_schema_sha256 = 3c1c77f355a2f9376f7cd4133267c99856cd4905b6116ad2ba615dbf84305035
```

这意味着：**即使我把 backup、rollback、provider 三份证据全部补齐，proof 仍然无法生成**——因为 schema 断言先一步失败。这个门禁在设计上把"存在真实迁移"和"必须无 schema 变更"同时要求了。

此外，backup/rollback 两份证据现在是 `stale`：它们绑定的是旧 RC `353cd0a2`，而本轮 RC 是 `24f9bd48`。这是正常的重绑定需求，不是缺陷。

---

## 5. 为什么没有继续强行推进

以下三项都超出"在授权范围内做合理修复"的边界，因此按 §7 停止而不是绕过：

**(a) 解除 schema 闸门属于发布策略决策。** 要么允许 `migration.mode` 表达真实迁移，要么让 RC 排练不产生 schema 变更。两条都改变"什么算可发布的证据"这一安全语义，不能在无人决策时由我自行选定。更不能为了让流程通过而伪造 `source==target`。

**(b) VPS 侧脚本需要 root 出站重装，而我没有该访问权。** `pg03-release-control.py` 与 `production-deploy-v2.sh` 当前仍钉在旧 Executor `9ba0e5ff… / 228ecbaa`。`deploy-production.yml` 只**校验**已安装字节（`LOCAL_SHA == REMOTE_SHA`），不负责安装。重钉到修复后的 RC 必须先在 VPS 上以 root 重新安装这两个脚本。实测：`ssh clearapi-deploy@103.42.181.219` 与 `ssh root@…` 均返回 `Permission denied (publickey)`，本地无可用私钥。

**(c) 生产凭据不在本地。** `PROD_SSH_PRIVATE_KEY` / `PROD_KNOWN_HOSTS` 只存在于 GitHub `production` environment，本地无法导出，也不应导出。

因此我没有"直接改生产数据库""手工篡改账号状态"或"绕过正式流程"去制造上线结果。

---

## 6. 已实测通过 / 仅静态审查 / 未验证 / 失败

### 已实测通过

- Gateway `service/accountpool`、`service/routechain`、`router`、`controller` 定向测试。
- Gateway 全链路一次真实请求成功，含 Usage 解析、余额扣减、单次结算。
- 失败请求不扣费、不写 consume log。
- 双账号 fallback：A 被排除后 B 完成，无重复结算、无遗留租约。
- Executor 重启后 revision / trust / capability / 栅栏全部保持。
- 导入 quarantine 的显式恢复闭环（无隐式复活路径）。
- Executor 导入路由在 service 角色下由 403 变为可达（新旧 RC 对照）。
- Gateway Required CI 6/6 通过；正式 RC 构建成功且 manifest 身份自洽。
- Rehearsal 中 8 个 gate 真实通过。

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

1. **决策 schema 闸门语义**（二选一，均需改动 `scripts/ci/` 的发布契约）：
   - 允许 `migration.mode` 表达真实 schema 迁移并配套回滚证据；或
   - 让 RC 排练在"无 schema 变更"候选上运行。
   在此之前，任何 PG03 候选都无法产出 proof。
2. **重新绑定并提供三份运营方证据**：backup、rollback 针对新 RC `24f9bd48` 重做；provider 证据基于已完成的真实链路结果生成并绑定到该 RC 的 run/attempt 与 manifest hash。
3. **在 VPS 以 root 重装两个脚本**：把 `pg03-release-control.py`、`production-deploy-v2.sh` 的 Executor 常量从 `9ba0e5ff… / 228ecbaa` 改为 `722915a6… / a45358ed…`（并同步 `EXECUTOR_BINARY`），或提供一次授权的方式完成该重装。
4. **提供可达的部署通道**：能通过 GitHub `production` environment 正常使用 `PROD_SSH_PRIVATE_KEY`（该链路本身可用，只是我这侧无凭据），或在受控窗口内提供一次性部署执行。

完成 1–3 后，`Automatic production release` 即可在门禁真实通过的前提下继续，随后才可能进入线上验收。

---

## 9. 本轮未做的、也不应做的

- 未伪造 `source_schema_sha256 == target_schema_sha256`。
- 未把失败的 gate 加入白名单，未删除或弱化任何测试。
- 未修改生产数据库、未手工篡改生产账号状态。
- 未绕过正式发布流程直接替换生产镜像。
- 未执行 `git reset --hard`、`git clean`、强推或未经核实的工作区切换；两个工作区的未提交现场与验收证据均保留。

---

## 10. 证据位置

| 内容 | 路径 |
| --- | --- |
| 完整链路验收（本轮） | `D:/AI/API/runtime/pg03-chain-20261019/` |
| Gateway RC manifest（本轮） | `D:/AI/API/evidence/pg03-launch-20261019/gateway-rc-39150754912/` |
| Rehearsal receipts（本轮） | `D:/AI/API/evidence/pg03-launch-20261019/rehearsal-39151149989/` |
| Executor RC manifest（本轮） | `D:/AI/API/evidence/pg03-full-launch-20261018/executor-rc-39143951438/` |
| 隔离 Executor 验收（上一轮，未重跑） | `D:/AI/API/runtime/pg03-oauth-fresh-20261018/` |
| 历史 full-launch 证据 | `D:/AI/API/evidence/pg03-full-launch-20261018/` |

## 11. 最终分类

**PARTIAL。**

- PG03 代码修复与集成：**PASS**（已合并 `24f9bd48`，Required CI 6/6）
- 完整链路真实验证：**PASS**（隔离环境、真实 OAuth、真实上游）
- 双账号 / fallback / 无重复结算：**PASS**
- 计费一致性（成功扣费、失败不扣费）：**PASS**
- 重启持久化：**PASS**（并修复了一个真实重启缺陷）
- 导入 quarantine 恢复闭环：**PASS**
- 正式 RC 产物身份：**PASS**（Gateway + Executor）
- Rehearsal：**FAIL**（11 个 gate 中 8 通过、3 失败）
- Release proof：**FAIL**（结构性 schema 矛盾，已实测证明）
- 生产部署：**NOT RUN**
- 线上验收：**NOT RUN**

**最终成功条件 `PRODUCTION DEPLOYED — ONLINE ACCEPTANCE PASS` 未达成。**
