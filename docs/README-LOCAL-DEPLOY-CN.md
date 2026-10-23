# ClearAPI — 本地一键部署原型（未获生产授权）

**状态（2026-10-23 23:14 上海）：原包已导入独立本地分支。真实 Windows Linux 镜像构建、隔离 SSH save/load、Compose 更新和测试脚本控制的回退通过；生产助手完整端到端尚未通过，不可直接切换生产。**

## 本轮实际验证与限制

- 本机 PowerShell 7.6.5、Windows PowerShell 5.1、Docker Desktop 4.87.0 / Engine 29.7.2、Buildx 0.36.1 可访问；初检 C/D 盘可用约 97/202 GiB。5.1 默认执行策略阻止 `.ps1`；测试仅给该进程指定 `RemoteSigned`，未更改系统或用户策略。两版均完成 fixture DryRun 和真实 config digest 提取。
- 从 GitHub main `a032815cb6244a7ee5e812de82f8300c02554e99` 的干净独立 worktree，使用原 Dockerfile 成功构建 `linux/amd64`。这是隔离主机实测架构，并未连接生产探测架构。manifest digest `sha256:1afe191f8549dc0f03c8cc3dfe320fbc92bd56ec5909ed26e419ca6348fbf3a9`；config digest `sha256:96dfd337e209a88d3733c78f00c737dff4668a003ae2f416cacdeddceaa07777`；二进制 SHA256 `979713027c986fbd3d4f2e95bf74414f0f3db26481d19bc10c4753162bd9c394`。不是正式 RC，也不声明与历史 CI 产物字节相同。
- 142598144 字节 Docker save 归档经 Windows OpenSSH 传到新建、仅监听 `127.0.0.1` 的隔离测试容器；主机密钥从本任务拥有的容器直接取得并固定，使用独立临时测试 ED25519 身份，保持 `StrictHostKeyChecking=yes` 和 `IdentitiesOnly=yes`。远端先校验归档 SHA256，再 load，并核对 config digest、revision 和所有文件层。未读取或使用生产私钥，未连接生产 IP。
- 加载到 classic daemon 后 `RepoDigests=[]`。这证明传输可行，同时证明它不满足现有 `runtime-identity-refresh.py`、`backup-identity.py` 和本包 apply 的 GHCR RepoDigest 契约。**本次没有把直传伪装成 GHCR，也没有改这些契约；直传尚未接入生产一键 apply。** 现有 GHCR 实现保留为待验收选项，未来应优先完成直传身份/备份契约适配，而非默认要求 GHCR。
- 独立 daemon 使用真实 ClearAPI Gateway、PostgreSQL 17.6。基线健康→不同 image ID 的同后端测试更新→退出失败镜像→测试脚本恢复上一合格镜像，均有实测；Gateway HTTP、本次测试表数据、schema 哈希、配置字节和共享容器身份/启动时间保持一致。Executor/Caddy 是 sleep 测试替身，仅证明未重建，不能当作真实业务或 TLS 验收。
- **回退由隔离测试脚本控制，未运行生产助手 apply。** 未测试生产模式启动、真实登录、授权 API、支付/计费、真实 Executor、HTTPS、生产备份恢复、跨数据库版本迁移或生产自动回退。首次 release 模式 fixture 因缺少安全 HTTPS 配置被应用正确拒绝；测试随后使用开发模式，未修改应用保护。
- 原包运行结果为 **13 项 VPS helper mock + 2 项切换准备 mock = 15 项通过**。包内“VPS helper 15 项”的描述不准确；没有发现可支持“11 项”的本地部署测试集或执行记录，不能将其他发布线的 11 项门禁与本包混算。增加配置保护回归后，只重跑受影响 helper 测试：14 项通过；未重跑未变化的 2 项切换测试。
- 最小修复：PowerShell `${phase}` 语法；识别 `production` 或 `origin` 中明确匹配 Clear-API 的 remote；通过 Docker save 配置原字节提取规范 config digest，避免 Desktop manifest ID 与 classic config ID 混比；真实 Compose 合并预检拒绝 override 替换导致任何非镜像配置变化。最后一项同时通过 mock 回归与真实 Compose 预览验证。
- 只读核对 GitHub main 和本地源码一致。`CLEARAPI_AUTO_RC_ENABLED=false`、`CLEARAPI_AUTO_PRODUCTION_ENABLED=false`，但 CI→PG03 RC→Rehearsal→Automatic production 的触发定义仍存在，手工生产入口也仍存在；不是永久切换完成。`prepare-local-cutover.py` 对当前工作流的预览成功，未使用 `--apply`，未改工作流、变量、Secrets，未 commit/push 到远端或 dispatch。
- 证据与可复核的隔离测试脚本见 `evidence/local-deploy-isolated-20261023/`。测试 host/daemon 的固定身份是防止误跑的约束，不能改指生产后执行。

## 与真实仓库核对的依据（2026-10-23）

- GitHub `a3225865526-wq/Clear-API` main：`a032815cb6244a7ee5e812de82f8300c02554e99`；本环境未挂载 `D:\AI\API\...` 工作树，不能核对本地未提交文件。
- 真实 Dockerfile 含 Bun + Go 构建，OCI `org.opencontainers.image.revision` 可用于身份验证。`docker-compose.production.yml` 是含占位符的模板，**不是**已运行的 Compose：现有正式部署脚本使用 `/opt/clearapi/app/docker-compose.prod.yml` + `/opt/clearapi/app/compose-candidate.yaml`。
- 既有部署锁 `/run/lock/clearapi-production-deploy.lock`；运行时身份工具 `/opt/clearapi/bin/runtime-identity-refresh.py`；备份服务 `clearapi-backup.service`，备份证明在 `/opt/clearapi/backups/manifests`，加密包在 `/opt/clearapi/backups/encrypted/<id>`。
- `main` 自动链：`ci.yml` push → `release-pg03-gateway.yml` (`CLEARAPI_AUTO_RC_ENABLED` 控制) → `deployment-rehearsal.yml` → `automatic-deploy.yml` (`CLEARAPI_AUTO_PRODUCTION_ENABLED` 控制) → `deploy-production.yml`。是否启用 GitHub 变量并未从运行环境实测，**不能据此认定 push 安全**。

## 文件

- `scripts/deploy-local.ps1`：Windows PowerShell 5.1+ 本地入口；预检查、前驱提交差异分类、Dry Run、Docker Buildx `--platform linux/<实际探测架构> --load`、GHCR 精确 RepoDigest 推送、受控 SSH apply、结果检查。
- `scripts/local-deploy-vps.py`：**切换授权后**才安装的 root-owned VPS 助手；前驱校验、同一部署锁、镜像校验、现有备份、只更新 `new-api`、基本健康和限定安全回退。
- `scripts/local-deploy.config.example.json`：本机配置模板；复制为 `scripts/local-deploy.config.json`（只在本地，必须 gitignore）。不放密码、密钥内容。
- `scripts/local-deploy.vps-config.example.json`：VPS root-only 配置模板；字段必须与已核实的正式 Compose / GHCR 拉取身份一致；模板不包含 Token 明文。
- `scripts/prepare-local-cutover.py`：**待切换、未应用**的 Actions 停用准备器；默认仅输出真实工作树的 unified diff，`--apply` 仅在本地改动，**不会提交或推送**。
- `tests/test_local_deploy_vps.py`：mock 隔离测试。

## 当前可做（不触碰生产）

在本地**独立 worktree** 把本压缩包文件按相对路径复制到仓库（不得覆盖有修改的文件）；把 `.gitignore.append` 附加到当前 `.gitignore`。PowerShell 本地 dry-run：

```powershell
Copy-Item .\scripts\local-deploy.config.example.json .\scripts\local-deploy.config.json
# 编辑 config，仅填写 SSH 私钥路径及经过独立核验的 known_hosts 文件路径。
# 真正 live dry-run 必须先在已授权的目标上安装受控只读 preflight。
.\scripts\deploy-local.ps1 -DryRun
# 不连接生产的模拟 dry-run（fixture 必须由独立隔离环境的可信只读 preflight 结果生成，包含仓库中存在的前驱 commit；本包不伪造）：
.\scripts\deploy-local.ps1 -DryRun -FixturePath C:\tmp\verified-preflight.json
```

`-DryRun` **不 build、不推送 GHCR、不修改 VPS**；若未提供 fixture，仅通过 SSH 执行只读 `preflight`，要求已切换的 helper 存在，故当前恢复阶段不应这样调用。如果未能取得准确前驱提交或状态，预演将正确阻断。

安装 Docker Desktop（Linux 容器模式 + Buildx）、Windows OpenSSH Client、Git；`ssh` 必须启用 `BatchMode=yes`、`IdentitiesOnly=yes`、`StrictHostKeyChecking=yes`，且 known_hosts 指纹必须由可信渠道预先核对，不能直接接受 `ssh-keyscan` 的结果。

模拟测试（独立执行器，不需要实际 Docker）：

```bash
python3 -m unittest discover -s tests -v
```

## 未来首次生产切换（需要单独授权）

1. **先完成现有生产恢复**：Gateway、Caddy、PostgreSQL 正常，当前镜像与 release-manifest、一致性健康且有实证；PG03 保持现有状态。本工具拒绝对故障前驱部署。
2. 在独立 Windows 工作区检查 `git status`、旧 workflow 的完整触发范围和仓库变量；确认没有进行中的生产恢复及可疑 Actions 运行。
3. **先使用隔离 VPS/Docker 环境**做原生 PowerShell、真实 Buildx、GHCR push/pull、最小 Compose、备份、健康和失败回退的端到端测试，不能拿生产 VPS 当测试机；完成源码和 sudoers 安全审查。
4. 在明确切换窗口内先停用旧 GitHub Actions 生产触发器（先由 GitHub UI 停用相关工作流；再运行 `python scripts/prepare-local-cutover.py` 预览及授权后 `--apply`；必要时同步停用旧变量及部署 Secrets），确认没有旧链可写生产之后，才开启本地部署入口。**停用文件不得在仍自动生产部署时贸然 push。** 可用 GitHub UI 先停用实际 workflow，再审查改动推送。
5. 经人工授权，仅此时把 root-owned VPS helper 安装到 `/opt/clearapi/bin/clearapi-local-deploy.py`（0755）、配置 `/opt/clearapi/config/local-deploy.json`（root:root 0600）、启用哨兵 `/opt/clearapi/config/local-deploy.enabled`（root:root 0600，内容 `LOCAL_DEPLOY_CUTOVER_APPROVED_V1`）、现有 GHCR 只读拉取 Token 文件权限、受限 sudoers 规则。审查文件哈希和命令参数，禁止通用免密 root shell；不能从用户上传目录运行脚本。
6. 首次部署以前核对正式 Compose `compose-candidate.yaml` 与 Gateway 容器镜像、运行时身份及 DB 当前状态；根脚本只允许现有单服务 `new-api`，不支持 PG03/Executor/数据库迁移或全新服务器初始化。
7. 授权后的日常命令：

```powershell
.\scripts\deploy-local.ps1
```

此命令与 `git push` 完全分离：脚本本身**不会执行 `git push`**，但会执行 `docker push ghcr.io/...`，需要操作者预先配置 **GHCR write:packages 登录**，VPS 需已有安全的 GHCR pull token，但直到旧 workflow 实际停用之前，`git push main` 仍有触发风险。

## 仍有限制

- **不支持数据库迁移**：已知 schema、PG03、accountpool、routechain、迁移命令等修改直接阻断。后端其他变更需正式加密备份证明；若替换后的后端健康失败，不擅自回退，因为启动时迁移是否发生不能仅靠文件路径证明。
- 只支持现有 Compose 配置中的 `new-api`（前端内嵌在 Gateway 镜像）；不会重建 PostgreSQL、Executor、Caddy。
- 采用本地 Docker **image ID（OCI config digest）**、被动 GHCR 仓库提供的 **RepoDigest（镜像清单摘要）**，两者分别验证，VPS 按精确 RepoDigest 拉取。GHCR 仅作既有可信镜像仓库，不使用 Actions/RC；这比“GitHub 只存源码”的字面范围多一项镜像存储依赖，如果必须完全脱离 GitHub 镜像仓库，就需要另行改造现有身份与备份合同，不能直接切回 `docker save`。
- 健康检查只覆盖本地/公网 `/api/status` 与 PostgreSQL 健康状态；**真实登录、授权 API、支付和账务验收尚未自动覆盖**，因此不可宣称已经完成全部用户要求的生产验收。
- 助手实际会原子更新 `release-manifest.json` 与 `compose-candidate.yaml`，没有实现单独 `local-deploy-state.json`；前端失败时恢复原文件，后端替换后失败保留 pending 并停止自动恢复。新增 Compose 预检若发现非镜像配置变化会在修改这些文件前阻断，不覆盖业务配置。
- 原包生成时缺少 Windows/Docker 的环境限制已由上方本轮实测记录取代；原包历史模拟测试不得当作生产验收。
- **不要提交、上传或分享**真实 private key、known_hosts 以外的敏感配置、`.env`、备份文件；已配置的 known_hosts 本身可保存在本机。
