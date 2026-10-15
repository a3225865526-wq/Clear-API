# CLEARAPI_PG03_CI_RC_RELEASE_REPORT

日期：2026-10-15（Asia/Shanghai）

## 1. 最终结论：PARTIAL

经过验证的开发分支已成功推送，但正式 CI 尚未运行，新的 Gateway RC 尚未构建。原因有两个明确的人工/流程门禁：

1. 仓库 CI workflow 只监听 `pull_request`，直接 push 不会产生 CI Run。
2. 仓库 agent PR 规则要求真实关联 Issue 及行为/影响/频率证据；当前授权没有包含创建 Issue，也没有现成 PR，因此不能绕过规则直接创建 PR。

此外，现有 `PG03 exact Gateway RC` workflow 的 Source Scope 只允许 wrapper/Bootstrap 文件，而本分支真实修改包含 Runtime Integrity、Billing Reserve、Approval、Referral、路由和测试文件。直接人工触发该 workflow 会在 source-scope 检查处拒绝，不能把旧 wrapper workflow 当成新 Gateway 源码 RC 构建流程。

## 2. Git 状态

- Remote：`production` = `https://github.com/a3225865526-wq/Clear-API.git`
- Branch：`codex/pg03-runtime-integrity-repair-20261015`
- Commit：`587ad4c4189213241043c58d55ba05c6ae554314`
- Remote branch ref：已确认指向同一 SHA
- Working tree：clean
- Push：成功，未 force push，未修改 `production/main`
- PR：未创建，因 agent PR gate 缺少 Issue 事实和 Issue 编号

该提交链包含：Runtime Integrity 依赖整合 `5199f5a4`、P1 修复 `8e66e489`、最终 readiness 报告 `587ad4c4`。

## 3. 本地验证

- `go test -mod=readonly -count=1 ./controller`：退出码 0，PASS。
- `go test -mod=readonly -count=1 ./router`：退出码 0，PASS。
- `go test -mod=readonly -count=1 ./relay ./service -run 'Test.*(Retry|Channel|Pool|Route)'`：退出码 0，PASS。
- Runtime Integrity、Model、Migration schema、Pool OFF、Billing reconciliation：此前均 PASS，源码未被后续发布操作改变。
- `go build -mod=readonly -o /tmp/new-api .`：退出码 0，PASS；使用已有只读 `web/dist` 挂载，仅用于本地构建，不是发布产物。
- Deploy V2 离线证据：恢复 baseline 并使用私有 Linux 临时目录时 59 项，58 PASS、1 conditional SKIP、0 FAIL；脚本未修改。

未运行远程 CI，因为没有 PR；未触发人工 RC workflow。

## 4. 发布身份与阻塞

- 旧 Gateway digest `sha256:3262afd...` 不能用于新源码。
- 本轮没有新镜像 digest，状态为 `NOT_BUILT`。
- Executor 固定身份仍为 `sha256:0838072c...`；本地接口/合同验证通过。
- 当前 RC workflow 的 `PG03_BASE_COMMIT` 仍为 `fd98c9aad...`，且 source scope 允许 wrapper、Bootstrap、Migration 等有限文件；它会拒绝本分支的 Gateway 业务/Runtime 依赖变更。
- 因此必须由维护者决定：扩展现有 RC workflow 的 source scope/构建合同，或建立经过审查的正式 Gateway RC workflow。不得用手工镜像或旧 digest 绕过。

## 5. 生产安全确认

生产 Gateway、Executor、Runtime、Billing、数据库和 Channel 2 未改变；未上传/导入 OAuth，未重启 Executor，未部署，未修改生产 Manifest，未触发 RC 构建。

## 6. 唯一下一步

请完成一个人工流程动作：为该分支建立/关联维护者批准的 GitHub Issue 和 PR，并确认正式 RC workflow 是否允许包含本次 Gateway Runtime/Billing/Approval/Referral 源码范围；确认后再运行 PR CI，并由人工触发 RC 构建。
