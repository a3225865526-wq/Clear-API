# 原 Caddy 恢复与本地正式集成 — 2026-10-24

## 网站：BLOCKED，未启动

本次仅只读生产核实；未修改文件、安装资产、启停服务或解除隔离。Gateway 已运行，无需通过更换镜像恢复它。网站恢复与本地部署接管分别推进。

- 现场时间 `2026-10-24T00:46:07Z`，见 `caddy-readonly.json`。Gateway `0a9a93c0…` / `ed6f93a9…` 仍运行，loopback `/api/status` HTTP200；PostgreSQL、共享 Executor 仍运行，未操作。
- 原 Caddy `54c8550b…` / `f2a1290d…` 为 exited、restart=no、host network。正式管理方式为 `/opt/clearapi/edge/docker-compose.yml` 的 `clearapi-edge/caddy`；没有 Caddy systemd unit。原配置、挂载和 TLS 数据保留，未创建替代服务。
- Caddyfile 为 root:root 0644、只读绑定，SHA256 `6b1246669fbab3287a5c9e99f041cbcff0c44ebbcedb9c0a751310f7b1b05969`。反向代理仍为 `127.0.0.1:3000`，HTTP www 站点为 HTTPS 308 跳转。证书 SAN 为 `www.clear999.com`，Let's Encrypt，2026-10-06 至 2027-01-04；仅检查公开证书元数据，没有读取或导出私钥，未声称 TLS 握手通过。
- 80/443 无监听，源站 HTTPS curl exit7/HTTP000。`00:55:07Z` 从本机重新 GET 公网首页及 `/api/status` 均为 **403、带 CF-Ray**，见 `public-readonly.json`；不能复用上轮521作为本次公网结果，也不能将403当作源站恢复。

**有效停止条件：** `/opt/clearapi/app/controlled-launch-state.json` SHA `57a7152a…` 仍为 `stage=isolated`；target SHA `521e60ce…` 明确 `public_traffic=blocked`；release SHA `6c308146…` 仍是旧 c50/22ec FAILED。这三份冻结记录均未变化。

main 的 `scripts/controlled-traffic-hold.py` 明确没有 opening 入口，原 edge 只有在单独审阅的恢复流程后才可开放。现有 `WebsiteRestoration.restore` 还要求 `failed-recovery-39347012653.json`、同源成功恢复运行和精确旧实例 postflight。现场恢复 receipt 不存在，`/opt/clearapi/bin/pg03-failed-recovery.py` 未安装；当前 Gateway 已为 a032/ed6，不能借用绑定 c50/22ec 的恢复流程。新 main 的 `caddy-state-reconcile.py` 也仅为只读比较，不提供开口权限。

用户明确要求遇到未解除隔离停止，因此没有直接 `docker start`，没有改 restart policy、清 hold、改 FAILED 或安装脚本。没有启动就没有“启动后健康 PASS”，也没有发生需要停回的操作。该阻断不要求先完成本地部署，不要求替换 Gateway 或修改数据库；后续需先审阅适用于**当前实例**的原 Caddy 解除隔离合同，再在明确授权范围执行。当前条件不满足既有最小启动授权，不能据此执行整套切换。

## 正式集成：本地已准备，未推送

独立工作区：`D:/Codex/worktrees/clearapi-local-formal-integration/clearapi-controlled-launch-20261020`；分支 `codex/clearapi-local-deploy-formal-integration`。从正式 main `a032815cb6244a7ee5e812de82f8300c02554e99` 合入本地分支 `8a8b36ab8f9e9c08ca644ff06e829b8784f035ea`，保留 main 的全部138个后续提交和原隔离验收历史。

仅两处文本冲突：

1. MASTER：main 完整内容原样保留，再加本地部署的新状态；未用旧主线覆盖新恢复记录。
2. backup-identity：同时保留 main 停止态/schema2、严格 successor_completion（新鲜性、调用退出码、文件归属/密文哈希/明文清理）和已验收归档身份支持。`production-backup.sh` 与 main 完全相同。

另修复 Runtime 语义集成：GHCR 仍输出 `image_digest`，本地归档仍输出 Docker engine ID；归档 tag 必须等于 OCI revision。boot 对成功本地部署校验 method、archive SHA、ref/revision，以及 manifest 的 OCI config digest；不把 containerd index 当 config ID。boot/helper/旧安装器的候选哈希清单同步；新资产未安装，未继承旧审批。原历史安装模板未改，候选包生成器仍能绑定精确源树并拒绝旧/漂移资产。

相对 main，`.github/workflows/`、生产 Compose、Go/web 业务源码、依赖、原备份 shell 和历史 boot 安装模板均无差异。PG03 开发成果保留，只有其账号池商业使用暂缓；其他业务、配置和商业逻辑保持原样。

## 实际验证

| 检查 | 结果与证据 |
|---|---|
| boot 定向回归（含新归档边界） | 18 tests OK；initial-focused-checks.txt |
| 既有 RuntimeIdentityTest | 21 个用例完成；初始组合命令因误选不存在的 InstallerTest 而 exit1，不能将整条命令标为PASS；详见 initial-focused-checks.txt |
| 正确安装器回归 | InstallerFaultTest 14 tests OK；installer.log |
| main 候选资产包合同 | CandidateBundleTest 3 tests OK；runtime-bundle.log |
| 备份冲突回归 | 16 tests OK；backup.log，含停止态真实 GPG 加密备份与严格完成证明测试（Docker/DB 为隔离夹具） |
| 四入口停链预览 | 2 tests OK；initial-focused-checks.txt、four-entry-retirement-preview.patch；未apply |
| 新 boot 的实际 Docker config digest 与 Runtime check | PASS；real-archive-boot-check.log。仅现有隔离 DinD，engine `2f86fe96…`、config `a34b84f4…` 不同；manifest 与 identity 字节不变，未重新部署 |
| 候选固定哈希 | production-deploy.sh / runtime-identity-refresh.py 均OK；pins.log |
| Python 编译、Git diff、secret scan | 已执行通过；最终提交前再次核对暂存差异 |

Linux 测试使用 WSL Ubuntu 的临时 root 夹具，没有生产 Docker socket、生产数据库或生产密钥。中间一次 runner 因旧 `/tmp` 目录已消失而在测试前失败，修正为同次调用创建并测试；失败未隐藏。**没有重跑30项 E2E**；其原始 PASS 和工具身份在 `evidence/local-deploy-final-20261024/` 保持不变，新集成不能冒充旧矩阵字节。

## 生产入口仅4项；当前均未停用

`00:49:07Z` GitHub 只读结果：main 仍 a032，两个 AUTO=false，queued/in_progress/waiting/pending/requested 均0，11个 workflow 仍 active。

| 顺序 | 工作流 / ID | 生产能力 |
|---|---|---|
| 1 | automatic-deploy / 387486994 | 自动调用可复用生产部署 |
| 2 | deploy-production / 378101050 | 手动/可复用 SSH 生产部署 |
| 3 | failed-production-recovery / 390042545 | recover/restore 可写生产；rehearse 为离线 |
| 4 | ghcr-pull-rehearsal / 378227091 | SSH inspection / 可选 docker pull；release_audit 为只读 |

保留 CI、两个 RC、Deployment rehearsal、release state audit、isolated rehearsal、successor grant 等7项；不因为它们能产出构建/证明就判为直接生产写入口。前四项的远端停用仍需单独授权。未来源码终端 job 的停止门禁已用 `prepare-local-cutover.py` 生成**预览**，保留离线/审计 job，未修改 workflow 文件。详见 `four-entry-retirement-plan.json`。上轮8项整套方案未获批准，已标注为历史方案。

## 尚未满足的首次接管条件

本地 main 集成不等于生产可部署：旧隔离合同未解除；当前四层 Compose/受控环境与普通 helper 两层布局不同；FAILED manifest 与当前 Gateway 不一致；当前运行实例的正式备份及 live boot 基线尚未闭环。不能手改 success、使用旧停止态备份或 TestTarget 绕过。后续源码 Push/PR 和必需 CI 也均未执行。

按用户要求，本轮不安装新 root/helper，不替换镜像，不做首次本地生产部署，不修改 GitHub 变量/Environment 或服务，不开放公网。当前不提交整套生产切换授权申请；先分别闭环当前原 Caddy 恢复约束和上述接管条件，再集中申请明确的首次部署操作。
