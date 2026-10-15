# CLEARAPI_PG03_FULL_GATEWAY_INTEGRATION_FINAL_REPORT

日期：2026-10-15（Asia/Shanghai）

## 1. 总体结论：PARTIAL

P0 Gateway Runtime Integrity 缺口已在独立开发分支完成真实后端整合并通过核心合同测试，但还不能标记 READY_FOR_RC：controller/router 全量回归仍有两个既有失败，且新源码尚未经过正式 CI/RC 镜像构建。生产环境没有改变。

本地提交：`5199f5a4b67faf5b63cac2c00f4f9307ccf182ba`。

## 2. 实际整合内容

### Runtime Integrity

恢复稳定实现中的：

- `model/runtime_integrity.go`
- `service/runtime_integrity.go`
- `controller/runtime_integrity.go`
- `middleware/auth.go` 的 root live-session 只读鉴权
- `/api/clearapi/runtime-integrity` 路由注册
- `controller/runtime_integrity_test.go`

端点依旧 fail-closed：身份、运行二进制、OptionMap/数据库一致性、审批视图、产品视图和时间窗口任何一项不可信都会拒绝；没有硬编码 PASS，也没有允许 PAT 走诊断路由。

### Billing Reserve Mode

恢复稳定 `setting/operation_setting/billing_reserve_policy.go` 及 Billing reservation/reconciliation 兼容结构。模式是真实配置状态，默认仍为 `off`；Runtime Integrity 测试在 enforce 场景验证报告读取。没有改变计价公式、价格倍率、余额定义、扣费精度、退款幂等或支付/兑换码逻辑。

### 商品审批与 Referral

恢复 Runtime 查询所需的 `CreditCatalog`、`CreditApprovalEvent`、`RedemptionBatch` 证据绑定及 `ReferralConfig`/只读校验。有效商品视图复用稳定 `getEffectiveCreditProducts` 和 legacy snapshot 校验；数据库读取失败仍返回阻塞错误，不转换为空数据。

### 迁移与兼容性

`model/main.go` 增加必要的 Approval/Referral 表模型和幂等初始化入口；未加入 BillingAuthSnapshot 到启动自动迁移，该迁移仍由已有 production-bootstrap 显式入口控制。保留目标 RC 现有 route-chain、Pool 和前端结构，没有合入会删除它们的稳定分支提交。

为兼容目标 RC 的旧商城读取函数，`getStoreProductsForUser` 改为返回 `(products, error)`，调用方保留旧响应形状；有效商品错误不再伪装成空列表。

## 3. 阻塞及处理记录

| 阻塞 | 根因 | 处理 | 结果 |
|---|---|---|---|
| Runtime endpoint 缺失 | RC 删除 model/service/controller/middleware/route | 按稳定提交恢复并保留 fail-closed 合同 | 已解决，核心测试 PASS |
| Billing/Approval/Referral 编译缺口 | RC 不含稳定数据模型和策略注册 | 文件级恢复必要后端依赖，未合入 UI/无关业务 | 已解决，包编译 PASS |
| 整套提交产生 route-chain/前端冲突 | 稳定提交范围过宽 | 中止 cherry-pick，改为文件级提取 | 已解决 |
| Runtime 测试初次缺少 provider handler | 使用较早 cd051 文件版本 | 对齐稳定 `da0dcf…` controller/service 实现 | 已解决，Runtime 测试 PASS |
| Controller 全量失败 | `TestResponsesHTTPHealthCountsFinalResult/retry_succeeds` 实际只请求一次、500；独立重跑复现 | 与 Runtime 整合无关，未改重试合同 | P1 保留 |
| Router 全量失败 | `TestGetOpenAIVideoRouteRendersJimengTask` fixture channel admission 返回 503；独立重跑复现 | 与 Runtime 整合无关，未改 channel 语义 | P1 保留 |

## 4. 测试与构建

- `go test -mod=readonly -run '^$' ./controller ./service ./model ./router`：PASS。
- `go test -mod=readonly -count=1 ./controller -run TestRuntimeIntegrity`：PASS。
- `go test -mod=readonly -count=1 ./model -run TestMigrationSchemaStability`：PASS。
- `go test -mod=readonly -count=1 ./model -run 'TestPoolMasterFailClosed|TestBillingReconciliationDatabase'`：PASS。
- `go test -mod=readonly -count=1 ./model`：PASS，完整 model 包回归通过。
- `go build -mod=readonly -o /tmp/new-api .`：PASS；使用已有本地 `web/dist` 只读挂载，未将生成资产纳入提交。
- `go test -mod=readonly -count=1 ./controller`：FAIL，仅 `TestResponsesHTTPHealthCountsFinalResult/retry_succeeds`；期望 2 次/200，实际 1 次/500。
- `go test -mod=readonly -count=1 ./router`：FAIL，仅 `TestGetOpenAIVideoRouteRendersJimengTask`；期望 200，实际 503 channel admission failed。
- 既有 Deploy V2 离线回归：59 项，58 PASS、1 conditional SKIP、0 FAIL；脚本未修改，本轮复用已确认证据。
- 既有 Account Import、Executor、三数据库 Mock PG03/Billing 证据继续有效；没有真实 OAuth、真实 Provider 或生产调用。

没有新 Gateway 镜像 digest；旧 `sha256:3262afd...` 不代表本次新源码。

## 5. 发布准备

- 新源码需要重新走 Git Push → CI → 人工触发 RC 构建，不能复用旧 digest。
- Runtime Integrity 端点和核心依赖已经真实可编译、可测试；Runtime 合同测试通过。
- Deploy V2 对成对 Gateway/Executor、Runtime、Backup、Billing、回退保护的逻辑未改，已有离线合同继续可复用。
- 目前仍有两个阻止 RC 全量绿灯的 P1 测试失败；因此暂不具备申请正式 RC 的完整条件。
- 未执行三数据库真实迁移；仅 schema stability 和历史三数据库证据可用。生产迁移仍需人工审批和正式 bootstrap 流程。

## 6. 生产状态

本轮严格 Host Key SSH 只读复核结果：

- Gateway 仍运行 `sha256:3fbbd768d71d8e2d4ea96ebd0ef010bc75f7a7c3e34e9b788df334d88ed6a8f8`。
- Executor 仍运行目标 `sha256:0838072c3fd61165ba20f78084dafe678114dd5c8b19a1e8b4acd31eee2ad821`，healthy。
- 生产 Manifest 仍为旧 commit `da0dcfcd7062875163bff4faa6bc70377b1db890`、旧 Gateway digest、`pool_mode=off` 缺省。
- Deploy V2 SHA256 仍为 `2d7fee67a57a5c5e408067a44ceb3f5b29399dd7aa62edc7c845bd36b7ae9408`。
- Account Import SHA256 仍为 `1737eafe22a21361490b9ca28a7b0dbdcd69d6f3b8d5e24369400d515a9a5bd3`。
- Backup timer active；Channel 2 只读事务返回 status `2`，随后 ROLLBACK。

生产 Gateway、Executor、Billing 数据、Runtime、数据库和 Channel 均未修改；未上传/导入真实 OAuth，未创建 Grant/Trust/Capability，未 push、触发 RC 或部署。

## 7. 唯一下一步

请人工批准下一步 **P1 修复范围**：只修复 `Responses retry_succeeds` 和视频路由测试 fixture/真实 admission 根因并重新跑 controller/router 全量；完成后再申请正式 RC 构建。生产部署和真实凭据操作仍需另行审批。
