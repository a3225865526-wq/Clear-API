# ClearAPI — Master Status

## 2026-10-19 — READ-ONLY AUDIT CHANNEL PREPARED（最新阶段）

- **CURRENT STATUS:** BLOCKED。main 仍为 `d0c20d6c46d86845805e4a9d35b067b0f6b20be4`；RC `39205228315` 保留。本轮无新 RC、Rehearsal、VPS 操作或熔断恢复。
- **CURRENT BLOCKER:** 自动审计已取得三份真实绑定关系，但原始验收来源尚未验证；production records 已可读，审计遇到未识别的 deployment status，完整调查尚未完成。未知状态保持 BLOCKED。Cloud 直接 gh 的 Forbidden 不能等同于仓库 scope 缺失，Actions 只读身份现已实际可用。
- **CURRENT PRIORITY:** 使用现有 Actions 的短期只读身份，在不接触 SSH/root/production environment 的 job 中读取 RC、证据发布身份和部署记录，收敛 PR #72 的最终修复范围。
- **NEXT MINIMUM ACTION:** Cloud 先通过已有仓库写入授权，在限定审阅分支自动执行独立的只读 push audit，取得真实结果；暂无需用户操作。仅若该正式通道确实不可用，才使用 owner 手机单次分支审计作为 fallback（既有 GHCR Pull Rehearsal，branch=`codex/pg03-predecessor-compat-20261019`，`release_audit_only=true`，`audit_rc_run_id=39205228315`）。无需 Windows、VNC、root 或新凭据。工具没有 workflow_dispatch，重跑旧事件也不能替代新增模式；但不能据此跳过仍可尝试的受约束只读 push 通道。

### 阶段 1：已有授权通道核查完成，读取边界明确

PR #72 原 head `33bea6ce40d7b27c18133624e9b4430ae81e4b82` 的 CI `39207544711` 六项全部 SUCCESS，PG03 root 测试 28/28；前驱修复仍待合并。当前 main CI/RC 与候选身份沿用下表，未重复执行。部署记录仍 UNKNOWN，不恢复熔断。

### 阶段 2：新增正式只读调查模式，待实际运行

复用既有 `ghcr-pull-rehearsal.yml` 的 workflow_dispatch，新增 `release_audit_only` 分支：

- 仅 owner 的初始触发/重跑、仅 main 或上述审阅分支；独立 job，没有 production environment、SSH 凭据、sudo、服务容器、Provider 请求或构建。
- job token 只申请 `contents:read`、`actions:read`、`deployments:read`。不创建高权限凭据、不申请管理/写权限，不改变生产 sudoers。若 Actions 中仍被拒绝，报告失败阶段与这组最小读取权限，保持 BLOCKED。
- 下载既有 RC artifact，复用原 inventory/hash 校验；核对 main/source/run/attempt。仅输出三份证据中格式受限的发布 SHA/digest/run/manifest/schema 身份和字段匹配结果，不输出原始 Secret JSON、target_id 值、任意 provenance 文本/URL、token、会话或 API 错误响应。
- 读取 production deployment records/statuses，以及正式部署 workflow 的未完成或事故期间非 skipped 运行；保留记录 ID、状态与 Actions 关联，逐项待调查。VPS 上是否有未决操作仍必须现场核实，不能从 GitHub 空列表推定安全。
- 所有输出固定为 `clearapi.release-state-audit/v1`、`status=BLOCKED`、`not_release_evidence=true`；不会重签证据、生成 release-proof、恢复熔断或部署。原 VPS job 在该模式被跳过，且 GHCR 工作流不会触发 Automatic production release。

另提供独立 `PG03 release state audit`：push 触发仅限本 PR 分支且只监听审计源码/工作流文件，owner/triggering owner 双检查；同样只有 read 权限、无 production environment 或 SSH。它不会触发 RC、Rehearsal 或自动部署。使用现有仓库写入身份尝试自动调查，不创建高权限凭据或替代 root 登录方式。当前有效 CI 完成后才推送该必要补丁，不取消正在运行的工作流。

实际本地验证：automatic-release/audit 28 tests PASS（含 Secret payload/target/provenance 不反射、异常日志脱敏、未知部署状态拒绝及 job 权限隔离）；PG03 28 tests（26 PASS、2 root-only skipped）；rehearsal producer 6 PASS；YAML/py_compile/diff 检查 PASS。新增补丁改变 PR 身份，旧 PR CI 不能冒充新 head CI，更新后的必需 CI 另行核实。

### 阶段 3：审计结果回来后的边界

**首次实际自动审计已完成运行：** [39210043117](https://github.com/a3225865526-wq/Clear-API/actions/runs/39210043117)，job `117921166702`，源码 `62119ac189288e8b7694e8c91fd1b1ea0b7d2d6f`，Artifact `12166868531`（`release-state-audit-39210043117-1`，SHA256 `f08dcf40d41e221e9af7736bc3449853ef07e9c5f1ea83ac593cf881356bf4f3`）。它不属于 Rehearsal 或生产部署。

- 原始日志的三项 `public_history` 均精确匹配 `passed_cd6f_RC39197505434` 的 source_commit / gateway_digest / rc_run_id，即当前三份 Secrets 仍绑定 cd6f719e79de432de6219561b04502784cb58930、Gateway bdbb535b...fdc6、RC 39197505434；这不再只是历史文档推测。
- 与当前 RC 39205228315 不匹配的字段均为 source_commit、gateway_digest、rc_run_id、manifest_sha256；rc_run_attempt、source/target schema、target_id、kind/schema/运营方 success 声明均匹配。身份匹配结果不等于原始验收来源已经核验，不改写任何 Secret。
- Actions token 已成功读到 deployments 与 status API，阶段错误为 `Unknown deployment status`，不是权限拒绝。调查工具应保留新状态与记录并继续盘点，不能把未知状态改成 success 或据此恢复熔断；本次补丁仅修正诊断的前向兼容处理，固定 BLOCKED/逐项待调查不变。
- 尚未取得生产部署、VPS 未决操作或原始运营验收已闭环的证据。暂无需用户手机/VNC/凭据操作。

先读取真实报告并关联原始验收来源，逐项调查 production records；未知/失败维持 BLOCKED。调查收敛后才决定 PR #72 合并及一次必要的新 RC，按最终 manifest 重新取得真实证据。VPS 安装仍独立待办，control 待审阅哈希 `2c3b1d22e4109aef1a075a46e42b127830515ab3f3161830f91df114d5948422` 未改变；旧 PR #70 清单不能用于修复后的候选。

## 2026-10-19 — MOBILE COORDINATION：BLOCKED，剩余不止 VPS root 安装

**本节取代下方所有“门禁全绿 / 仅剩 VPS root”结论。生产部署与线上验收均未取得完成证据。**
本次修改在 `codex/pg03-predecessor-compat-20261019` 审阅分支；未将修复分支当作已部署源码，未触发新 RC 或 Rehearsal。

### 当前已核实候选

| 项目 | 身份 / 真实结果 |
| --- | --- |
| main（本次查询） | `d0c20d6c46d86845805e4a9d35b067b0f6b20be4` |
| main CI | [39204691662](https://github.com/a3225865526-wq/Clear-API/actions/runs/39204691662) SUCCESS |
| 当前已构建 RC | [39205228315](https://github.com/a3225865526-wq/Clear-API/actions/runs/39205228315) SUCCESS；不重复构建 |
| Gateway 候选 digest | `sha256:98e615c08fd0697506340c2150c6e2a7e5aa53c543741ce61ebe4a190424ca65` |
| Executor 正式配对 digest | `sha256:cfad1b51928813cc6b932deb31adc71d5d23ae76fd2ea9d899bc740d1a06ae58`；不代表本次已核实生产运行值 |
| Rehearsal | [39205679007](https://github.com/a3225865526-wq/Clear-API/actions/runs/39205679007) FAILURE；未生成可用全绿 proof |
| 自动发布 | [39206211930](https://github.com/a3225865526-wq/Clear-API/actions/runs/39206211930) SKIPPED |
| VPS 只读检查 | [39203542451](https://github.com/a3225865526-wq/Clear-API/actions/runs/39203542451)：SSH 成功；两份脚本哈希不符；Runtime 未执行 |
| 实际生产 SHA / 镜像 / PG03 状态 | 本轮未取得完整运行证据，UNVERIFIED；历史旧版本健康不作为当前验收 |

### 三份 stale 证据：身份不匹配，不是时间过期

用户已取得原始 Artifact `deployment-proof-39205679007-1` 并提供三份真实 stderr；本轮已对照 verifier 的错误分支：

- backup_restore：`backup rehearsal evidence is stale`
- gateway_executor_provider_usage_billing：`provider rehearsal evidence is stale`
- safe_rollback：`rollback rehearsal evidence is stale`

`scripts/ci/verify-rehearsal-input.py` 只有在证据 `source_commit` 或 `gateway_digest` 不等于 RC manifest 时报告该 stale。该结果不是 Runtime、Provider 或备份操作本身失败的证明，也不是它们对新候选已通过的证明。

**当前三份 Secrets 的实际身份与来源引用：尚未取得可回读的非敏感元数据，UNKNOWN。** GitHub Secrets 不支持读取明文；不得从 stale 推断究竟哪个字段不符，也不得假定 Secrets 从上一次验收后从未更改。需要运营方提供各证据的 source_commit、gateway_digest、rc_run_id/attempt、manifest_sha256、schema 指纹、target_id 及原始结果引用；不需要私钥、密码、会话或 Provider token。

现有历史来源（不是当前 Secrets 的独立实测）：

- 仓库 `CLEARAPI_PG03_PRODUCTION_LAUNCH_FINAL_20261019.md` 记录三份证据曾绑定 `9a3e6585737d823896137346faf94c2e1f4d1b04` / RC `39193373708` / Gateway `sha256:95f7e50f1894171aef3eaf5c94b9e123491430c8f17d4caab3c107aa0ae79b6c`，manifest SHA `898b7b15648799b7d1b2f5e168526eb0e905c93066343e37eb05ca59517061e4`。
- 详细来源记录：备份 `20261016T193254Z` 的隔离解密/恢复；75 张表与 schema/不变量核验；候选二进制双启动；真实 Provider 链路的 usage、钱包/token 与消费日志一致性。原始文件位于 Windows `D:/AI/API/runtime/pg03-rebind-20261019/`，其中 `ev6/`、`provider-measurement.json`；当前 Cloud 无法读取该本地目录和离线解密身份。
- 后续 `cd6f719e79de432de6219561b04502784cb58930` / RC `39197505434` / Gateway `sha256:bdbb535b8826aa243eee7922a7a5e2d0fb8646f69a34653b4251ca60c488fdc6` 的 Rehearsal `39198350006` 曾成功；这是那次运行的验收，不是对现在 Secrets 或新 RC 的许可。
- 历史 rollback 记录明确 `rollback_proven=false`、`production_restore_executed=false`；不得改写为生产恢复/回滚已实测。

重新签发条件：先固定最终 main/RC/manifest；取得原始结果及可验证来源，判定与新候选的适用范围。可复用真实备份文件、已验证的固定 Executor 和无关测试结果；对受源码/二进制/schema/运行目标变化影响的验证，只补必要项目。rollback 的精确候选双启动和 Provider 当前候选真实请求结果不能仅靠替换身份字段得到。重新签发必须记录真实操作、来源和判断依据，精确绑定 source/digest/run/attempt/manifest/schema/target，并通过现有 verifier；不修改 verifier，不编造成功，不无条件复制旧 JSON。

### 前驱身份读取：修复已准备，尚未安装或合并为 main

现场 clearapi-deploy 的 sudo allowlist 不允许通用 `sudo python3 -c`。旧 workflow 在读取生产前驱 SHA 时使用了该命令，是独立权限兼容缺陷。

本审阅分支将 PG03 的相同前驱校验移到已有 allowlist 的 `pg03-release-control.py prepare`：main() 持现有部署锁，在发布任何目标/维护授权文件前，使用既有安全 reader 读取固定 root-owned manifest（拒绝 symlink、非安全属主/权限和 duplicate JSON），要求精确 40 位前驱 SHA，并拒绝 pending/failed 状态。保留原有 GitHub 授权与 RC 校验；没有新增 sudo 权限、任意路径参数或通用 root 命令。非 PG03 路径不受本修复影响。

新增回归覆盖身份错误/缺失、未知 manifest、pending/failed、固定读取路径及失败时禁止准备写入。实际本地：PG03 28 tests（26 PASS / 2 root-only skipped），automatic-release 24 PASS；完整 PR CI 另行记录，不能将本地跳过项写成通过。

### Release circuit：保持 BLOCKED

- 原始失败 `39176488931` 为 `unexpected run event`：历史策略仅接收 workflow_run，而实际手动 Rehearsal 是 workflow_dispatch。后续 `39189520883`、`39195457908`、`39198826181` 被持久失败历史拦截。
- 四份原始授权日志已复核，规范化 SHA256 全部与 `.github/release-circuit-recovery.json` 相符；四个部署 job 全部 skipped。因此可确认这四次运行没有进入生产部署 job，不能据此声称整个 VPS 无其他操作。
- PR #69 已提供受审计恢复：精确失败列表、原日志、job inventory、main 祖先关系、24 小时时效、无插入手动部署、无待调查 production deployment record；仅在当前全部 proof/RC/CI 校验完成后产出恢复审计。未删除状态、重写失败或执行恢复。
- 当前连接器拒绝 deployments API，公开网页查询也未得到记录；实际 production records 与未决操作仍 UNKNOWN。只读检查使用 production environment，其可能产生的记录也必须核查分类，不能直接豁免或当成业务部署。
- 恢复记录有效至 **2026-10-20 04:22 UTC / 12:22 Asia/Shanghai**；过期必须重新调查，不修改时间伪造新调查。

### VPS root：单独待办，不阻止上述 Cloud 工作

实际只读检查已确认两个文件不匹配：V2 为 `10d12caa...0e45d`、release-control 为 `bf6bc8cb...3189d`；其余四份资产匹配。部署账号没有安装权限，Cloud 没有其他已授权 root 身份，不能访问 Windows 本地密钥；不扩大 sudoers，不绕过 MFA。

PR #70 安装方案对当前 main d0c20d6c 的两份文件仍适用（V2 `490af9ab...18468fe`；control `e24259b8...abcb7`）。**本次修复会改变 control 哈希，故旧方案不能当作修复后最终候选的安装清单。** 最终代码冻结、审阅通过后一次性更新正式安装 pin/hash；然后由正式管理员/Server.HK 支持代装并保留备份/审计。官方工单草稿已在 PR #70，尚未提交；不假设支持方一定可代装或免费。

安装后重跑原只读 inspection 读取真实哈希和 Runtime 结果。Runtime 当前 UNKNOWN，不预先断言需要新登录；如实际 PROBE_UNAVAILABLE，仅走官方管理员交互登录/MFA。命令执行成功不等于探针 PASS。

### 最小剩余阻塞与执行顺序

1. 取得三份运营方证据的非敏感历史身份、原始验收记录与重新签发依据；只补真正受影响的验收。
2. 完成本分支前驱权限修复的必需 CI/审阅；同时取得并调查 production records。未知状态继续 BLOCKED，不提前放开熔断。
3. 一次性确定最终源码与正式资产哈希；只有最终代码改变候选身份时才产生一个必要的新 RC。现有成功 RC 保留，不能作为修复后源码的候选，也不重建相同 RC。
4. 三份真实证据绑定最终 RC 后，按既有入口验证；不无变化重复 Rehearsal、不 rerun 旧 attempt、不复用不同 source/digest 的全绿 proof。
5. root 两份资产安装、真实 Runtime/运行身份验证与熔断恢复核验分别完成；全部门禁通过后才继续正式部署。
6. 实测生产 SHA、Gateway/Executor digest、PG03 启用、真实请求/切账号/fallback、usage/钱包/token/消费日志、失败请求退款或待核对闭环、健康与回滚能力。历史隔离失败请求待核对/退款结果不等于当前生产闭环。

**最终状态：BLOCKED。没有脚本安装、Runtime PASS 或生产已部署的新增证据。**

## 2026-10-19 — 历史快照（已被上方协调状态取代）

**以下仅为历史候选记录，不能作为当前 main/RC 的验收结论。** 其中“仅剩 root”“均需 root”已失效，以本文顶部协调状态为准。

### 本次执行身份

| 项目 | 当前值 | 状态 |
| --- | --- | --- |
| 生产域名 | `https://www.clear999.com` | HTTP 200（全程未被触碰） |
| **远端 main（最新）** | **`9a3e6585737d823896137346faf94c2e1f4d1b04`** | VERIFIED |
| 本轮合并 PR | #59 schema 契约、#60 Executor 重钉、#61/#62 链路触发、#63 manual rehearsal、#64/#65 event 授权 | MERGED |
| **最终 Gateway RC** | Run `39193373708`, digest `sha256:95f7e50f1894171aef3eaf5c94b9e123491430c8f17d4caab3c107aa0ae79b6c`, Manifest SHA `898b7b15648799b7d1b2f5e168526eb0e905c93066343e37eb05ca59517061e4` | SUCCESS |
| **Rehearsal** | Run `39194957755`：**11/11 gate success，release-proof.json 已生成** | PASS |
| schema 发布契约 | 改为从 manifest 推导 `in_place_startup_migration`；10 个负向用例仍全部拒绝 | FIXED |
| 三份运营方证据 | backup / rollback / provider，均先过真实 verifier 再安装；绑定 RC `39193373708` | PASS |
| Executor 分支 / commit | `codex/pg03-executor-mvp` / `b6e5a90fce6c0b3fa231d43ab07a65622f981be2`（tip `945fc3ee` 内容相同） | PUSHED |
| Executor digest | `sha256:cfad1b51928813cc6b932deb31adc71d5d23ae76fd2ea9d899bc740d1a06ae58` | VERIFIED |
| Executor binary SHA256 | `e99b4dceee9b857c67e7b2b5101552511dbce60c789b2bec3c8fdd6bb5a33523` | VERIFIED |
| 自动发布熔断器 | `39176488931` / `39189520883` / `39195457908` 三次 failure（均在授权步骤，部署步骤 skipped）后**持久打开** | BLOCKED |
| VPS 已安装脚本 | 与当前 commit **不一致**（本轮改过 `production-deploy-v2.sh` / `pg03-release-control.py`） | NEEDS ROOT REINSTALL |
| PG03 生产启用 | 未执行 | NOT RUN |
| 线上验收 | 未执行 | NOT RUN |
| 熔断 / 双账号故障转移 | 隔离环境实测通过（本轮及上轮） | PASS (ISOLATED) |

### 剩余阻塞（均需 VPS root）

1. 重装 `production-deploy-v2.sh`（`490af9ab74b354de17075f9c01b66a4b88fa63ca376c3730da550d18818468fe`）与 `pg03-release-control.py`（`9dce204701e835c68633ed27859f90c40b8c43ccf75b7f83e12b006e4bb666f4`）到 `/opt/clearapi/bin/`，root 属主、`0750`。
2. 以 root 经真实交互式登录 + MFA 生成 `/opt/clearapi/secrets/runtime-probe-session`。
3. （仅自动路径额外需要）确认无影响后恢复发布熔断器。

详见 `CLEARAPI_PG03_PRODUCTION_LAUNCH_FINAL_20261019.md` §4.4 与 §8。

## 2026-10-18 — RESUMED — FULL PG03 PRODUCTION LAUNCH（历史）

**PG03 = RESUMED — FULL PG03 PRODUCTION LAUNCH.** 本节为 2026-10-19 之前的状态，历史正文原样保留。

### 本次执行身份

| 项目 | 当前值 | 状态 |
| --- | --- | --- |
| 生产域名 | `https://www.clear999.com` | HTTP 200（本轮公网只读核对） |
| 远端 main（最新） | `353cd0a23b723f86c6b21e759a6d691ce1ba5457` | VERIFIED |
| Gateway 工作树 | `D:/AI/API/worktrees/clearapi-pg03-full-launch` | ACTIVE |
| Gateway branch commits | PR #52 `1c2c555a`, PR #53 `fe3cf9cb` + integration test `f235c089` | MERGED/PUSHED |
| Executor 工作树 | `D:/AI/API/worktrees/pg03-executor-mvp` | ACTIVE |
| Executor 分支 / commit | `codex/pg03-executor-mvp` / `228ecbaa6f71ade146be789c9a3c42522a7f92a8` | PUSHED |
| Executor RC | Run `39079462722` | SUCCESS |
| Executor digest | `sha256:9ba0e5ffb0ccbcf4f1f23d272e4c1b7bb0fe86af93429c823928358ff99d34e5` | VERIFIED |
| Executor binary SHA256 | `6dff0b07e6b231abbe5a4845f2f4e588d9b3252c3d84b33f75b3ed79d336240b` | VERIFIED |
| 自动部署 | final main `353cd0a2`; Rehearsal attempt 2 blocked only on Provider evidence; AUTO variable absent | BLOCKED / NOT READY |
| 网站 JSON 导入 | 前端、Gateway API、Executor 原子导入已开发；multipart route test PASS；未部署 | LOCAL PASS / PRODUCTION NOT RUN |
| Gateway RC | Run `39083968671`, digest `sha256:bfdf5b842f9a9344cfb1eff1186be4962520bfbaceec72b5bd547432a3ac374d`, Manifest SHA `896c8c2731fe43a4b3068ec7d656a06975c4c9ecdd602ee86a7ad4b4b602a02d` | SUCCESS |
| Backup / rollback evidence | actual encrypted backup `20261016T193254Z`, 75 tables, exact RC twice, 0 invariant failures | PASS ISOLATED |
| Rehearsal | Run `39084286400` attempt 2: all non-Provider gates PASS; proof blocked on Provider | BLOCKED |
| PG03 production enablement | Not executed | NOT RUN |
| Post-main test branch | `codex/pg03-rehearsal-fix-20261018`; head `0023088c`; no production switch | NOT MERGED |
| 真实 Provider Usage/Billing | 尚未执行 | NOT RUN |
| 熔断 / 双账号故障转移 | 尚未执行 | NOT RUN |

### 网站 JSON 导入

- 支持格式：顶层字段 Codex OAuth JSON，`type=codex`，必需 `access_token`、`refresh_token`、`account_id` 或 `chatgpt_account_id`、`email`。
- 安全边界：服务端解析与暂存、单文件 256 KiB、最多 10 文件、批次 1 MiB、重复 key/嵌套 collection/API key 拒绝、固定错误码、Token 不进入响应/日志/浏览器持久化。
- 流程：Web 上传 → Gateway 校验 → Executor 原子上传 → Pool 登记 → trust/capability 回读；导入不会自动标记 verified。
- 本地验证：Gateway `service/accountpool` 单测通过；Router 控制面测试通过；Executor 原子导入测试通过；前端 `tsgo -b` 与生产构建通过；本次改动定向 lint 通过。
- 未部署到生产；需要新的 Gateway RC + Executor RC 配对和正式 Deploy V2。

### 当前待完成顺序

1. Executor RC `39079462722` 与 digest/binary/manifest 已完成并写入上方。
2. Gateway PR #52、#53 已完成 Required CI 并合并；额外 multipart route 测试在修正分支 `f235c089`。
3. main `353cd0a2` Required CI `39083507403` PASS；正式 Gateway RC `39083968671` PASS。
4. Rehearsal backup/rollback/executor/migration/contracts PASS；自动 proof 仍缺真实 Provider evidence，Deploy V2 尚未执行。
5. 使用获准测试凭据完成网站导入、Pool、Route Chain、Executor、Usage/Billing 生产验收；个人 Codex OAuth 未被确认可用于商业中转时不得作为商业上游启用。
6. 无法确认商业用途或缺少第二个合格账号时，记录为 BLOCKED，不得伪造全通。

### 最终分类 — 2026-10-18

**PARTIAL — BLOCKED ON REAL PROVIDER EVIDENCE. Production was not switched.**

- 最新 main `353cd0a2`、Required CI `39083507403`、Gateway RC `39083968671`、Executor RC `39079462722` 均 PASS。
- 网站 JSON 导入代码、Executor 原子导入、multipart route test 与前端构建均 PASS；生产未部署。
- 真实备份 `20261016T193254Z` 隔离恢复及精确 RC 双启动 PASS；生产未执行恢复/切换。
- Rehearsal `39084286400` attempt 2 除 Provider 外全部 gate PASS；Assembler 拒绝生成 proof。
- 两个上传账号均为 `free` 计划且未确认商业用途；不得作为已获准商业上游。
- 自动部署 Run 因 proof 未生成而 skipped；PG03 生产 enablement、Route Chain 生产请求、Executor 部署、真实 Usage/Billing 均为 NOT RUN/UNVERIFIED。
- 完整报告：`CLEARAPI_PG03_FULL_LAUNCH_FINAL_STATUS_20261018.md`。`CLEARAPI_MASTER_STATUS.md` 为工作区副本，已随 `0023088c` 推送。
- 证据：`D:/AI/API/evidence/pg03-full-launch-20261018/`。

### 历史记录

以下为 2026-10-16 冻结及更早历史，原样保留，不代表本次状态。

## 2026-10-16 — OFFICIAL FREEZE / HANDOFF

**PG03 = FROZEN — Frozen Integration Candidate。** 本节取代下方历史 BLOCKED 状态及继续上线的 Next Action；历史记录原样保留。冻结原因是战略调整，不是失败，也不删除、回退或重写已完成成果。

- 优先顺序：自动部署 → 商业化 MVP → 恢复并最终整合 PG03 → 统一生产上线。
- 冻结 main：`0a5884fd894b0dac1fae5073e4d37265970df530`。
- 正式 RC：Run `38906184793`。
- candidate digest：`sha256:569de0e04a63fd51a601ad38675b4cb9529ffdeaec5d42c6a3dc8580bf919523`。
- 已完成成果保留：Route Chain、Account Pool、admission / capability、Executor、Runtime Integrity、Billing Reserve、CreditCatalog、RedemptionBatch、Referral、迁移验证、Required CI、RC 身份验证、Executor 健康检查、备份恢复验证。均为既有证据，本轮不重跑。
- 已确认 SSH / sudo / 应用 Root Dashboard 认证及 main / RC / digest 绑定通过；不是认证失败。
- 遗留 maintenance request = EXPIRED，工具无合法 replacement/reissue 命令；旧生命周期文件阻止再次 review。**暂不处理，不再修复 legacy transition，不创建新请求，不续期或复制旧 session。**
- 生产尚未切换 PG03，Channel 2 保持禁用；这是用户确认及历史检查状态，本轮未连接 VPS 或重新核验在线健康。
- 主线工作树：`D:/AI/API/worktrees/pg03-mainline-integration-20261015`；branch `codex/pg03-rc-containerd-20261016`；本轮本地核实 HEAD 与冻结 main 一致。
- 会话工作树：`D:/AI/API/upstream/new-api`；branch `codex/pg03-account-pool-mvp`；HEAD `fd98c9aad0374581e46e5ba13bcf18ec96046559`，与冻结 main 分开记录。
- 冻结交接完整索引：`D:/AI/API/worktrees/clearapi-master-status-20261016/CLEARAPI_MASTER_STATUS.md`（本地总控维护工作树）；本目录既有九份 PG03 报告均保留，未提交文件、代码、分支、工作树及历史不清理。
- 本轮仅更新上述两个本地总控 Markdown；没有业务代码、生产、数据库、Channel 或 Gateway 变更；没有部署、RC 重建、commit、push、force push。

恢复条件：自动部署完成；商业化 MVP 完成；最新 main 稳定；自动 CI / RC / Deploy V2 稳定；能够生成新的完整 RC；再最终整合 PG03；最后统一生产部署。恢复必须另行授权并重新核验维护协议和运行时，不沿用过期请求或历史 RC 作为未来整合版本许可。

**冻结边界：不改 Account Pool / Route Chain / Executor / Runtime Integrity / Billing / Channel / Gateway / legacy transition；不申请生产部署、不启用 Channel 2、不重建 RC、不删除 PG03 代码/分支/工作树、不 squash / reset / force push。冻结完成后停止。**

### PG03 主线本地冻结交接补记（2026-10-16）

- 当前执行工作树为 `D:/AI/API/worktrees/pg03-mainline-integration-20261015`；branch `codex/pg03-rc-containerd-20261016`；HEAD `0a5884fd894b0dac1fae5073e4d37265970df530`，已本地核实。
- 冻结报告：`CLEARAPI_PG03_FREEZE_HANDOFF_REPORT.md`，保存九份既有报告的 SHA256、PG03 工作树/分支入口、历史证据目录及恢复依赖。
- 本地 `refs/heads/main` 仍为 `ad71806fd00518855d4f7d27d72c9d3f846e2cbb`，不存在 `refs/remotes/origin/main`。这与当前分支 HEAD 分开记录；用户确认的冻结 main 为上列 `0a5884fd`，本轮没有联网查询或更新本地引用。
- 上方“本轮仅更新两个总控”和“会话工作树”属于先前冻结记录。本次仅补记当前目录总控并新增冻结报告，不修改总控维护工作树；历史正文、九份既有报告、代码、未提交文件、分支及工作树保留。
- 本次仅作本地状态与文档保全核对；不重跑历史 CI、数据库或恢复验证，不访问生产。Root / SSH / sudo / RC 绑定 PASS、生产尚未切换及 Channel 2 禁用沿用用户确认和已有证据。
- **PG03 = FROZEN；旧 maintenance request 问题暂不处理。** 自动部署和商业化 MVP 优先，达到上述恢复条件后再另行恢复。本次没有 commit、push、force push、RC 重建或任何生产/数据库/Channel/Gateway 变更；交接完成后停止。

---

以下为冻结前历史记录；其即时修复/部署行动已暂停，历史生产信息不视为本轮复核。

更新时间：2026-10-16 22:05:57（Asia/Shanghai；末次只读核对 14:05:57 UTC）

最终状态：**BLOCKED**。生产 Gateway 尚未切换，网站未更新为 PG03 RC。

## A — PG03 Production Launch

| 项目 | 当前真实状态 |
| --- | --- |
| PR #42 | MERGED；Head `fbe21405`，Merge `2302c59c`；发布修复 #43 / #44 已合并 |
| 当前 main | `0a5884fd894b0dac1fae5073e4d37265970df530`，本轮再次核实，未修改 |
| Required CI | `38905305045` SUCCESS，匹配 main；未重跑 |
| 正式 RC | `38906184793` SUCCESS，匹配 main；未重建 |
| Candidate digest / image ID | `sha256:569de0e04a63fd51a601ad38675b4cb9529ffdeaec5d42c6a3dc8580bf919523` |
| RC Manifest SHA256 | `550efc411f3ae8f044d143c3383a8520b411d165dddb827600a6f019862bb0d7` |
| 发布身份 | main / RC / digest / VPS image ID / OCI revision / 二进制摘要均匹配；不需要重建 RC |
| 应用 Root 认证 | 用户已合法登录；真实 Runtime 接口接受会话，旧版本返回 `503 / APPROVAL_VIEW_INVALID`；不是缺少账号认证 |
| Maintenance request | `7f426779efc3cf2fd2be8a157ccba70f26687e03f53a7f747e5e9b6530dc63c5`；13:52:48 UTC 创建、14:02:48 UTC 到期；14:05:17 UTC 核对时已过期 149 秒 |
| Production Run | [38911511937](https://github.com/a3225865526-wq/Clear-API/actions/runs/38911511937)，自动启动，FAILURE，未取消；13:55:33 UTC 被旧渠道基线拒绝，未进入维护/备份/切换 |
| 生产 Gateway | 仍为 `da0dcfcd7062875163bff4faa6bc70377b1db890` / digest `sha256:3fbbd768d71d8e2d4ea96ebd0ef010bc75f7a7c3e34e9b788df334d88ed6a8f8`；未替换或重启 |
| VPS / Executor / 数据库 | VPS 可用；Executor healthy；PostgreSQL 17.6 healthy；未迁移或重启 |
| Website | 末次公网首页及 `/api/status` 均 HTTP 200，TLS 校验 0；属于旧版本健康 |
| Backup | 正式加密备份 `20261016T140214Z` 已验证；原备份保持；timer active |
| Channel 2 / 3 | status=2，启用 abilities 均为 0；全部渠道及路由记录保留 |
| 维护 / 回滚 | 未进入维护、未退役 apply、未暂停 Caddy、未切换 Gateway、未回滚 |

当前最小直接阻塞：有效的一次性 maintenance request 不再存在。原请求已过期，且在用户随后明确授权禁用 Channel 3 后，冻结的渠道指纹不同。Root 登录已经成功；业务配置变化不是 RC 镜像身份变化。

下一步：以当前运行时状态对应的合法有效维护请求，继续同一个 main / RC / digest。现有 helper 没有续期或重绑定入口；不得改写旧请求、延长时间戳、伪造认证或忽略有效期。本轮不再次索取凭据，不触发已知无效的新 Run。用户最新指令后不再变更、删除或重置任何 Channel、商品、路由或历史数据。

## B — Autonomous Deployment

Status: **BLOCKED**（自动发布授权已实际通过，阻塞为维护请求有效性）

- production required reviewers 已取消，仍仅允许 main；保留 Required CI、RC/Manifest、镜像、Runtime、Billing、Backup 和后置健康验证。
- Run `38911511937` 已通过 owner-dispatch 自动授权、脚本完整性和精确 RC/Manifest 绑定，无人工 Approve 等待。
- 本轮无发布代码变更，无新 commit / PR / main / RC。既有 RC 仍匹配，不能因业务配置变化重建。
- 原失败原因：Channel 2 的 9 条 stale enabled abilities，以及启用的 Channel 3 / ability 不符既有商业基线。
- 用户随后明确要求关闭或删除渠道时，采用禁用并保留记录：事务纠正 Channel 2 abilities，正式管理 API 禁用 Channel 3 并刷新缓存；未删除数据。
- 变更前已执行并验证正式加密备份。变更后除 `channels` 指纹外，维护快照中的账务、Schema、Pool 和其他保护字段全部保持。
- 最新只读确认 Channel 1 启用能力=1，Channel 2 / 3=0；不再存在 unapproved enabled abilities。
- 请求时效及冻结快照是维护协议，独立于 Release Artifact 身份。未伪造或绕过协议；未创建额外失败 Run。

## C — Commercial Readiness

Status: **BLOCKED**（首次生产切换尚未完成）

按最小上线路径执行，未新增企业级、复杂商业或逐模型验收。新 Gateway Runtime PASS 和生产更新尚未发生；Channel 2 保持禁用，不为宣布成功强行开启或读取用户 Token 做请求。非关键配置改进后置；不恢复最近网站配置来迎合旧快照。

完整记录见根目录 `CLEARAPI_PG03_AUTONOMOUS_PRODUCTION_FINAL_REPORT.md`。此文件是唯一项目总控状态文件。
