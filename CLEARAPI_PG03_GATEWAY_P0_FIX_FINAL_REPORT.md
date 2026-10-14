# CLEARAPI_PG03_GATEWAY_P0_FIX_FINAL_REPORT

日期：2026-10-15（Asia/Shanghai）

## 1. 最终结论：BLOCKED

没有提交 Runtime Integrity 产品代码修复。源码对照确认 P0 的直接原因是目标 Gateway RC `29fcafdce455b348863763f7370e418877a78f82` 基于较早源码快照，缺少 Runtime Integrity v1 的 model/service/controller/middleware/route 实现。稳定实现可以定位，但在完整合同不变的前提下，它依赖目标 RC 不包含的 Billing reserve policy、审批型 CreditCatalog 和 Referral 配置读取。Runtime Gate 必须如实报告这些状态；写死 `enforce`、返回空商品视图、跳过审批验证都属于伪造/降级，不能采用。

当前授权禁止修改 Billing 公式、额度、退款或账务语义，也禁止绕过 Gate。因而不能安全地把稳定分支整体或相关 Billing 产品逻辑移植到 PG03 RC 并声称上线门禁可通过。保持 BLOCKED 比创建一个编译成功但不能通过真实 Runtime Gate 的不完整 endpoint 更符合授权和部署合同。

## 2. P0 根因与依赖图

目标 RC 缺少：

```text
GET /api/clearapi/runtime-integrity
  -> DisableCache
  -> RuntimeIntegrityAuth (live root dashboard session, no PAT audit write)
  -> controller.GetRuntimeIntegrity
  -> service.CollectRuntimeIntegrityReport
  -> model.ObserveRuntimeIntegrity
      -> container/process identity + running binary digest
      -> runtime config / DB OptionMap consistency
      -> Billing reserve mode
      -> Pool master state
      -> Referral config
      -> effective CreditCatalog + solo approval evidence
      -> legacy product compatibility snapshot
      -> stable product/approval view digest
```

具体缺项及业务边界：

- 生产稳定代码在 `da0dcfcd7062875163bff4faa6bc70377b1db890` 有 `model/runtime_integrity.go`、`service/runtime_integrity.go`、`controller/runtime_integrity.go`、`middleware/auth.go` 和 router 注册。目标 RC 中这些实现和路由均不存在。
- Pool master 模型/Option 已在目标 RC。它读取 DB `ClearAPIPoolMasterEnabled`；与 `CLEARAPI_POOL_ENABLED` 环境开关不同，不构成 Pool OFF/ON 冲突。
- Runtime Integrity 的 `billing_mode` 依赖 `operation_setting.ReservationHardeningMode()` 和 `billing_reserve_policy.mode`。目标 RC 没有这套版本化策略；Deploy V2 要求报告字段精确为 `enforce`。移植该策略并实际启用会引入新的 Billing reserve/enforcement 行为，因此本轮禁止。
- Referral 检查依赖 `ReferralOptionKey`、`ReferralConfig.Validate` 和 `readReferralConfig`。稳定版本中的 `model/referral.go` 还包含奖励结算/账本行为，直接整体移植会超出最小只读依赖范围；自行复制解析会产生第二份业务配置语义，不能当作同一可信实现。
- 商品报告依赖稳定 `CreditCatalog`、`CreditApprovalEvent`、`getEffectiveCreditProducts`、LegacyStoreProduct 快照及 RedemptionBatch 审批/交付字段。其有效视图会验证已发布产品的审批事件、版本、digest 和证据批次。目标 RC 没有这些数据模型/校验。空列表或仅 hash 原始表不能保留审批保护。
- 稳定来源这些功能分布在 `90549923`、`d8bf1746`、`1a087820`、`cd05132f` 等提交，并包括 schema/模型与产品行为代码；不属于可单独复制的编译辅助文件。

## 3. 本轮实际变更

开发分支 `codex/pg03-runtime-integrity-repair-20261015`，基线仍为目标 RC commit `29fcafdce455b348863763f7370e418877a78f82`。没有源码变更、没有 commit；`git diff --check` 对干净基线通过。

原因是已确认的最小实现仍必须引入目标 RC 不具备的 Billing/Approval/Referral 合同，继续只补路由或 controller 会留下一个不能通过强制 Gate 的假性修复。本轮不改测试预期、不删除任何门禁，也不生成新镜像身份。

## 4. 自动验证与复用证据

- 之前独立 Gate/Deploy V2 回归：59 项中 58 PASS、1 条条件性 real-producer artifact SKIP，0 FAIL；未改 Deploy V2。
- 之前 Account Import 合成测试、Pool management 与 Executor SSE 定向测试通过；Executor 测试夹具修复 commit 为 `f9da050e`，其独立工作树未修改。
- 之前 Gateway/Executor/Mock Billing 的 SQLite、PostgreSQL、MySQL 历史验收均有 8/8 场景 PASS，但对应旧测试源码；它们不证明缺少 Runtime Integrity 的目标 RC 可上线。
- 本轮 `go test -mod=readonly -count=1 ./controller ./router` 失败：controller 包仅 `TestResponsesHTTPHealthCountsFinalResult/retry_succeeds` 失败（期望 2 次 upstream 尝试/HTTP 200，实际 1 次/HTTP 500）；router 包仅 `TestGetOpenAIVideoRouteRendersJimengTask` 失败（期望 200，实际 503 `channel_admission_failed: channel is nil`）。两个用例单独重跑仍失败。它们不涉及 Runtime Integrity；本轮不扩展修复 Responses retry 或视频 channel admission 行为。完整原始输出在 `evidence/gateway-baseline-tests.log`。其余 package 级通过状态不代表这两个包通过。
- 未运行会启动服务、访问生产 DB 或写入状态的 Runtime Gate。
- 没有创建 Gateway 构建或新 digest。

## 5. 发布准备

- 必须重新整合并构建 Gateway；现有 digest `sha256:3262afd71eea69cbe42b0d0c152416bc0f210b0abc822cb02b80df52e852ff36` 只能代表未包含 Runtime Integrity 的旧 RC。
- 当前目标源码无法提供 Runtime Integrity 响应；本轮没有实际 Runtime PASS。
- Manifest pair、Executor identity 和 Deploy V2 的成对 digest 校验设计已有证据，但当前运行 Manifest 的 `pool_mode` 缺省为 off；PG03 target manifest 不存在，需由后续获批的正式流程生成，不可手填 approval。
- Billing、Backup、Rollback Gate 仍保留在现有候选脚本；未执行生产脚本。当前目标源码不能提供这些门禁所需的可信 Runtime 报告。
- RC 审批条件不满足，存在 P0；没有新 P1 结论。

## 6. 生产状态

本轮通过此前验证的 SSH key 与 `known_hosts` 严格只读复核：

- Gateway 仍为 `ghcr.io/a3225865526-wq/clearapi@sha256:3fbbd768d71d8e2d4ea96ebd0ef010bc75f7a7c3e34e9b788df334d88ed6a8f8`，running；没有替换。
- Executor 仍为指定 digest `sha256:0838072c3fd61165ba20f78084dafe678114dd5c8b19a1e8b4acd31eee2ad821`，running/healthy。
- Production Manifest 仍记录旧 Gateway commit `da0dcfcd7062875163bff4faa6bc70377b1db890`、旧镜像和缺省 `pool_mode=off`。
- Deploy V2 与 Account Import VPS SHA256 分别仍为 `2d7fee67a57a5c5e408067a44ceb3f5b29399dd7aa62edc7c845bd36b7ae9408`、`1737eafe22a21361490b9ca28a7b0dbdcd69d6f3b8d5e24369400d515a9a5bd3`。
- Backup timer 为 active；PostgreSQL `BEGIN READ ONLY` 查询 Channel 2 得到 status `2`，随后 ROLLBACK。
- 未修改 Runtime、Billing、数据库、Channel、生产配置或容器；未导入凭据、grant/trust/capability、触发真实 Provider 请求、push、触发 RC 或部署。

## 7. 唯一下一步

请求一个最小范围决策：批准在开发分支中整合稳定版 `billing_reserve_policy` 及 CreditCatalog/solo-approval/Referral 只读模型依赖，保持代码与现有稳定实现一致且不启用生产策略、不迁移生产 DB；否则该 Gateway RC 应保持 BLOCKED。批准后仍需单独完成测试、构建和发布审批，本报告不构成生产授权。
