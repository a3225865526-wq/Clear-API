# CLEARAPI_PG03_P1_CLOSURE_AND_RC_READINESS_REPORT

日期：2026-10-15（Asia/Shanghai）

## 1. 最终结论：READY_FOR_RC

两个已知 P1 已关闭。Gateway Runtime Integrity 及其 Billing/Approval/Referral 依赖仍通过此前核心合同验证；controller/router 全量回归现在通过；本地 Linux 构建通过。满足申请新的 RC 构建审批的技术前提。

这不是生产上线结论：本轮没有 push、没有触发 CI/RC、没有部署，旧 Gateway 镜像不能代表新源码身份。

## 2. 两个 P1

### P1-A Responses retry

根因是测试把已向 upstream 发送请求后收到 HTTP 500 的情况当成可安全重试。请求已 dispatch，usage 进入 unknown/pending reconciliation；现有 retry 合同明确 `upstream_attempted` 或 `upstream_usage_uncertain` 时停止，避免重复 dispatch、重复 commit 或错误退款。产品代码行为符合安全合同，测试前提过期。

修改：`controller/responses_websocket_test.go` 将场景改名为 `dispatch failure is terminal`，期望一次 upstream、HTTP 500、无成功计数；保留已有握手拒绝后的安全重试覆盖。

定向命令：

```text
go test -mod=readonly -count=1 ./controller -run TestResponsesHTTPHealthCountsFinalResult
```

结果：PASS。

### P1-B Video Channel Admission

根因有两层：只读 task retrieval 路径错误地经过 `middleware.Distribute`，没有模型请求却触发 Channel Admission；测试 fixture 也没有创建 `Ability` 并初始化内存 channel cache。修复没有绕过真实 admission：retrieve 改为只读 TokenAuth 路径，remix 仍经过 Distribute；RelayInfo 缺省 task mode 明确映射为 `RelayModeVideoFetchByID`，避免 nil builder；测试创建真实 channel ability、开启测试 cache 并初始化 cache。

修改：`router/video-router.go`、`router/task-plugin-protocol-router.go`、`controller/relay.go`、`relay/relay_task.go`、`router/video_router_test.go`。

定向命令：

```text
go test -mod=readonly -count=1 ./router -run TestGetOpenAIVideoRouteRendersJimengTask
```

结果：PASS。

## 3. 连续执行记录

- 首次验证确认 retry 失败是 dispatch 后终态，不是缺少 retry 次数；未修改产品 retry 安全判断。
- 视频第一次补 cache 仍失败，定位到 `MemoryCacheEnabled` 默认关闭；随后补齐 cache 开关、Ability 表/记录和 InitChannelCache，仍发现 task relay mode 缺省为 unknown；最终恢复真实 fetch mode 映射并修正 invalid relay mode 的 nil builder 返回。
- 期间 `relay/service` Retry/Channel/Pool/Route 定向回归保持通过。
- Deploy V2 发行目录直接运行时缺少 baseline 测试夹具且严格父目录权限拒绝 `/tmp`；此前恢复 baseline 并使用 `/root` 私有临时目录的结果为 59 tests，58 PASS、1 conditional SKIP、0 FAIL。Deploy V2 脚本本轮未修改，发行合同复用该证据。

## 4. 全量回归结果

| 命令 | 退出码 | 结果 |
|---|---:|---|
| `go test -mod=readonly -count=1 ./controller` | 0 | PASS |
| `go test -mod=readonly -count=1 ./router` | 0 | PASS |
| `go test -mod=readonly -count=1 ./relay ./service -run 'Test.*(Retry\|Channel\|Pool\|Route)'` | 0 | PASS |
| `go test -mod=readonly -count=1 ./controller -run TestRuntimeIntegrity` | 0 | PASS |
| `go test -mod=readonly -count=1 ./model` | 0 | PASS |
| `go test -mod=readonly -count=1 ./model -run TestMigrationSchemaStability` | 0 | PASS |
| `go test -mod=readonly -count=1 ./model -run 'TestPoolMasterFailClosed\|TestBillingReconciliationDatabase'` | 0 | PASS |
| `go build -mod=readonly -o /tmp/new-api .` with existing read-only `web/dist` | 0 | PASS |
| Deploy V2 offline suite with restored baseline and private Linux temp root | 0 | 58 PASS, 1 conditional SKIP, 0 FAIL |

Tests use synthetic fixtures/Mock Provider and isolated SQLite where applicable. No real Provider, OAuth credential, production database, or production Runtime was used. The historical PG03 three-database evidence remains SQLite/PostgreSQL/MySQL 8/8; it was not needlessly rerun because its source and tested contracts were unchanged.

## 5. 新代码身份

- 分支：`codex/pg03-runtime-integrity-repair-20261015`
- P0 integration commit: `5199f5a4b67faf5b63cac2c00f4f9307ccf182ba`
- P1 closure commit: `8e66e489d0d6551b6cc3153428655c0a0b9e69e7`
- 工作区：clean after commit
- 构建：Linux/amd64 Go build PASS，使用现有只读 web asset mount；没有生成或伪造发布 digest

## 6. RC Readiness

- Runtime Integrity：**PASS**，真实端点/身份/状态 fail-closed 合同测试通过。
- Billing Reserve/commit/refund：**PASS** 定向和完整 model/历史 PG03 evidence；未修改 Billing 语义。
- Channel Admission：**PASS**，视频 read-only retrieval 不再错误触发 admission，remix/真实请求仍保留 admission。
- Route Chain：**PASS** 定向回归；Pool OFF/ON 合同未放宽。
- Pool OFF：**PASS** 历史 Deploy V2/off 回归与 Pool master tests。
- PG03：**PASS** 本地依赖/Executor/Mock E2E 历史证据；没有真实 Provider。
- Manifest：**READY_FOR_NEW_RC_GENERATION**；旧 digest 不适用于新源码，需正式流程生成新成对身份。
- Deploy V2：**PASS** 已有 59-case evidence（58/1/0），脚本未改。
- P0/P1：**无已知阻止申请新 RC 的 P0/P1**。正式 CI 仍是人工发布流程的一部分。

## 7. 生产安全确认

- Gateway：未改变。
- Executor：未改变、未重启。
- Runtime：未修改。
- Billing 数据：未修改。
- Channel 2：仍 disabled，未开启。
- OAuth：未读取、上传、导入或刷新真实凭据。
- 未 push、未触发正式 RC、未部署、未修改生产数据库或 Release Manifest。

此前只读生产状态仍有效：Gateway 运行旧 digest `sha256:3fbbd768d71d8e2d4ea96ebd0ef010bc75f7a7c3e34e9b788df334d88ed6a8f8`；Executor 运行指定 digest `sha256:0838072c3fd61165ba20f78084dafe678114dd5c8b19a1e8b4acd31eee2ad821` 且 healthy；Backup timer active；Channel 2 只读 status `2`。

## 8. 唯一下一步

请批准将当前分支 push 并触发正式 CI / 人工 RC 构建流程；本请求不包含生产部署或 Channel 2 开启。
