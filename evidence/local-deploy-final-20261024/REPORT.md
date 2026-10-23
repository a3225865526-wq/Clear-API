# ClearAPI Windows 本地一键部署最终隔离验收

- 结论：本轮本地工具验收 **PASS**。生产未连接、未修改；GitHub 未推送、未改工作流/变量/Environment。
- 时间：2026-10-24T04:57:44.341026+08:00（Asia/Shanghai）
- 正式工作区：`D:/AI/API/worktrees/clearapi-local-deploy-integration`；分支 `codex/clearapi-local-deploy-integration`。
- 基线/测试应用源码：`d9cd1299f6cb0442ba9d43af1a1ac368c94816c5`。本轮只改部署工具、测试、说明和状态，Go/web 业务代码未变。最终工具身份由下列文件 SHA256 及每例记录绑定，不把旧证据改绑为新版本。
- 全部 30 个真实场景 PASS（完整运行加受影响项定向重跑）；另有 16 个本地 helper/切换准备模拟回归和 12 个现有旧 GHCR 备份身份定向回归 PASS。

| 验收合同 | 结果 | 主要证据 |
|---|---|---|
| Windows 完整成功入口（真实 Buildx/save/SSH/load/Compose/PG dump/Runtime/健康） | PASS | full-entry-success.log、success-backup-proof.json、success-runtime-check.log、success-production-backup-identity.json |
| 前端启动失败自动恢复旧镜像，文件字节与健康恢复 | PASS | frontend-crash-rollback.log、results.json 前后快照 |
| 实际 HTTP TimeoutError → 有界就绪失败 → 前端安全回退 | PASS | health-timeout-rollback.log |
| 两次真实 receive 竞争同一锁；第二个拒绝，容器/配置不变 | PASS | concurrent-deploy-blocked.log、concurrent-lock-holder.log |
| 实际 pg_dump 不存在的 DB 失败，更新前阻断 | PASS | backup-failure.log |
| 归档 SHA256、大小、config image ID、revision 错配阻断 | PASS | archive-*.log |
| loopback/生产目录/daemon/模式合同/路径穿越/符号链接/URL userinfo/测试证明与身份/真实 sudo 边界 | PASS | isolation-*.log、sudo-*.log、sudo-policy.log |
| 后端替换后失败保留 pending/UNKNOWN，不自动回退 | PASS | backend-failure-manual-recovery.log；显式测试操作者恢复单独记录 |
| Windows PowerShell 5.1 真实 pinned SSH preflight 和二进制传输拒绝错配 | PASS | powershell51-live-preflight.log、powershell51-real-archive-identity-block.log |

成功镜像 OCI config ID：`sha256:a34b84f437efbacd174b20d0b54dd1b19a4dc3c5d63f40b75a7de79798731545`；归档 SHA256：`76de8c04d0b4f76434b9aa0421352f29d7376e28f2a2995867db456a0595596c`；真实隔离备份 ID：`20261023T180450Z`。测试 receipt 明确 `target_mode=isolated-test-v1`、`public_url=null`，不冒充生产健康。

## 实际执行与结果

- `python tests/local_deploy_e2e.py --runtime C:\Users\Chen\AppData\Local\Temp\clearapi-e2e-runtime --evidence evidence/local-deploy-final-20261024`：实跑矩阵，最后在 sudo 拒绝文本断言处停止（实际已拒绝）；修正测试断言后，`--case isolation` 全部 PASS。此前已通过且工具指纹相同的 A–E 保留；未把整条失败命令写成 exit 0。
- 同一命令分别加 `--case concurrency`、`--case backend`、`--case full-entry`：受影响项定向重跑及完整入口均 exit 0。最终完整矩阵中的并发项也已通过。
- PowerShell 5.1 经 `tests/local_deploy_transport.ps1` 真正调用相同正式传输函数，成功 preflight，归档错配按预期非零退出；前后容器/文件/DB 标记一致。
- `docker exec clearapi-e2e-host python3 -m unittest discover -s /tmp/local-deploy-unit/tests -v`：16 tests / OK。
- `docker exec clearapi-e2e-host python3 /tmp/local-deploy-unit/backup-regression.py`：12 个现有 `scripts/ci/test-backup-identity.py` capture/check 定向用例 / OK；具体用例见 backup-identity-regression.log。
- Windows PowerShell 5.1.26100.9711 / PowerShell 7.6.5 AST：PASS；`python -m py_compile`（修改的 Python 工具/测试及切换准备器）：PASS。
- `visudo -cf /etc/sudoers.d/clearapi-local`：parsed OK；实际 clearadmin 用户仅具有固定 preflight/receive 的免密入口，测试配置和通用 shell 被拒绝。
- `python scripts/prepare-local-cutover.py`：PREVIEW ONLY，未 apply；本地触发链和哈希见 cutover-preview.patch / environment.json。未声称远端 Actions 已停用。
- `git diff --check`：PASS。最终提交前另核对暂存文件、源代码 Git blob 哈希与证据匹配。

环境：外层 Docker 29.7.2、独立 DinD 29.9.2、真实 PostgreSQL 17.6；固定 daemon ID `f931987c-f3aa-4151-a0bf-ad2b7d45fd8d`，SSH 仅 `127.0.0.1:2222`。共享服务保留容器是 sleep 标记，不当作 Executor/Caddy 业务验收。测试只观察独立测试数据，不使用生产数据或密钥。

## 工具字节身份

- `scripts/deploy-local.ps1`：`cbbd7575f48cb95bdc2f15dec2931d8e51152abc662760acef8cf9c52a435d73`
- `scripts/local-deploy-vps.py`：`08cea48657e48985953a957f63bf519ee809b18285f50f917ed9bdeb3dbcc139`
- `scripts/runtime-identity-refresh.py`：`34e473ca30e79c5f30b66c49e360009a5492a5cc98cbd9702f52925d15e6f998`
- `scripts/backup-identity.py`：`6ef4b2ab8aece259e7ecbe7e4ac150116575b489d93f5bd0ce53976f9c95cdfe`

## 首次生产切换前仍需的最少事项

具备申请首次生产切换的**本地工具技术条件**，不等于生产可直接部署。生产当前健康、恢复主线、正式加密备份与恢复前提、旧发布链在途状态及真实业务验收本轮未核实，必须在授权阶段按既有合同处理。

1. 授权并执行旧 GitHub 自动/手动生产入口停用及在途运行核对；源码保存是否推送另按用户授权，本轮不推送。
2. 单独授权明确的 VPS 切换窗口：先只读核实当前状态；必要恢复必须遵循原恢复合同；按精确哈希安装新 helper/Runtime/backup-identity、root 配置、哨兵及受限 sudoers，再执行精确候选首次部署与验证。旧安装器固定哈希未擅自改写，新资产不能冒充旧批准版本。
3. 公网开放、Provider/PG03 商业使用和账务处置仍按各自授权与验收范围；本轮全部不执行。

本任务完成后停止，不触发生产切换。历史生产恢复记录完整保留。前期失败与修正见 execution-notes.md。

Runtime 资产字节说明：执行矩阵时工作区副本为 CRLF，原始执行哈希 34e473ca30e79c5f30b66c49e360009a5492a5cc98cbd9702f52925d15e6f998 原样保留。程序逐字节确认仅 CRLF→LF 后等于已暂存源码，规范源码/最终安装资产 SHA256 为 8c7b8362e819fdf20703eca2df70162afdfc2470a4a28d0ac947fc14923d671e；LF 资产随后真实 Runtime --check PASS，见 runtime-final-lf-check.log。规范 Git 哈希单独记录在 canonical_git_source_sha256，不改写历史执行身份。诊断日志和 unified diff 的上下文空白保留，不应用源代码空白规则。
