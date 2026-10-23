# ClearAPI — Windows 本地一键部署

本工具采用 **Windows Buildx 构建 → docker save → 固定主机密钥的 SSH 直传 → Linux helper 校验/load → 单服务 Compose 更新**。不执行 Git push，不使用 GHCR 作为新镜像传输依赖，不触发 Actions。本轮不连接生产，首次生产切换必须另行授权。

## 本地入口

安装 Docker Desktop（Linux 容器模式及 Buildx）、Git、Windows OpenSSH Client、tar、PowerShell 5.1 或以上。复制 `scripts/local-deploy.config.example.json` 为已忽略的 `scripts/local-deploy.config.json`，填写受保护的专用私钥路径和由可信渠道核实的 known_hosts 路径。配置中不放私钥内容或密码。

```powershell
.\scripts\deploy-local.ps1 -DryRun
.\scripts\deploy-local.ps1
```

普通 `-DryRun` 仍会连接已经授权切换的正式目标执行只读 preflight；本轮禁止对生产使用。离线预演使用 `-DryRun -FixturePath <可信隔离preflight.json>`，不会建立 SSH 连接。正式部署要求干净工作树、可追溯前驱提交及明确的前后端变更分类；仅工具或元数据修改不会构建 Gateway。

正式模式固定目标为配置中的既定生产主机和 clearadmin:22；只使用 helper 默认的 root 配置，不能传入测试配置。`-TestTarget` 必须同时提供 `testTarget=true`、`127.0.0.1` 及 `/opt/clearapi-test/` 内的 root-only 配置路径。测试配置还必须满足 `isolated-test-v1` 合同、固定 daemon ID、loopback 健康地址和规范化后的隔离目录边界；不能直接改指生产。

## 部署和失败处理

- helper 同时校验归档字节数、SHA256、OCI config digest、镜像 revision/架构、现有容器/Compose/manifest 身份，以及 PostgreSQL 和 HTTP 健康。Docker Desktop 的 manifest ID 与 config digest 分别处理。
- 与旧生产链共用 `/run/lock/clearapi-production-deploy.lock`；锁内重新核实前驱。竞争部署立即拒绝。备份失败、镜像身份错配均在配置或容器替换之前终止；失败归档可能留在 incoming 目录作调查，不能当作成功部署。
- 后端更新调用既有正式加密备份服务并验证新证明及密文；隔离模式只做真实 pg_dump 测试备份，明确标注测试身份，不能替代正式加密备份。现有 `backup-identity.py` 支持带明确归档来源的本地镜像，并保留旧 GHCR 精确前驱身份合同。
- 仅更新现有 `new-api` 服务。Compose 预览必须证明其他配置完全相同；不会重建 PostgreSQL、Executor、Caddy，不修改业务功能。
- 前端候选失败时恢复原 override、manifest 和精确旧镜像，刷新真实 Runtime identity，重新验证健康。Compose 启动后的 HTTP 就绪检查有界重试，最终错误保留实际原因。返回失败退出码表示候选部署失败，即使回退已成功。
- 后端候选替换后失败，迁移状态视为 UNKNOWN，保留 pending，要求人工恢复；不会自动覆盖数据库或自动切回旧后端。
- 已知 migration、schema、PG03、accountpool、routechain、SQL 变更直接阻断，须另行走评审流程。PG03 开发成果和所有业务功能保留。

健康检查覆盖 PostgreSQL 和 `/api/status`，不代表真实登录、支付、计费、Provider 或 PG03 商业业务已完成生产验收。隔离测试使用开发模式 Gateway、独立 PostgreSQL 17.6 及共享服务保留标记，不把替身当作真实 Executor/Caddy 验收。

## 验收证据与复现

本轮原始输出、前后快照和工具 SHA256 位于 `evidence/local-deploy-final-20261024/`。旧 `evidence/isolated-compose-*` 只证明历史 substrate；旧 15 项 mock 结果不适用于当前归档实现。修正后的聚焦测试位于 `tests/test_local_deploy_vps.py`、`tests/test_cutover_prepare.py`。

真实负向测试入口为 `tests/local_deploy_e2e.py`，通过 `tests/local_deploy_transport.ps1` 加载正式脚本中的 SSH 二进制传输函数，不模拟 helper、Docker 或 Compose。测试只允许本任务拥有且 ID 固定的 DinD 主机；私钥、连接配置、镜像归档、数据库 dump 留在 Git 外的临时目录，不进入证据提交。

```powershell
python tests/local_deploy_e2e.py --runtime C:\Users\Chen\AppData\Local\Temp\clearapi-e2e-runtime --evidence evidence/local-deploy-final-20261024
```

Linux 聚焦模拟回归（Windows 无 fcntl，须 Linux/Python）：

```bash
python3 -m unittest discover -s tests -p test_local_deploy_vps.py -v
python3 -m unittest discover -s tests -p test_cutover_prepare.py -v
```

## 首次生产切换清单（尚未执行）

1. 单独授权切换窗口、精确源码/镜像身份、必要 root 工具安装及首次部署。首先只读核实实际生产状态和现有恢复主线；本轮未连接生产，历史总控不是当前现场证明。故障前驱必须先按已有恢复合同恢复，不能用本工具绕过。
2. 检查 GitHub 实际 workflow/变量和在途运行。先经授权在 GitHub 停用旧自动及手动生产入口，再审查 `python scripts/prepare-local-cutover.py` 的本地预览；未确认旧链不可写生产前不得推送停用文件或启用新入口。本轮只保留预览，未更改远端。
3. 按源码哈希安装 root-owned `scripts/local-deploy-vps.py` 到 `/opt/clearapi/bin/clearapi-local-deploy.py`，以及配套 Runtime identity 和 `/opt/clearapi/backups/bin/backup-identity.py`。Linux 文件必须 LF，所有父目录不可由部署用户写入。旧发布安装器仍有旧版本固定哈希，不能把新工具冒充其批准资产；安装这些精确新资产属于本次切换授权范围。
4. root 配置 `/opt/clearapi/config/local-deploy.json` 严格采用 `scripts/local-deploy.vps-config.example.json`，权限 0600。启用文件同目录 `local-deploy.enabled`，root:root 0600，内容 `LOCAL_DEPLOY_CUTOVER_APPROVED_V1`。不可放入测试字段或测试备份证明。
5. 审查并用 `visudo -cf` 验证 `scripts/local-deploy.sudoers.example`；仅允许默认配置的 `preflight` 和参数校验严格的 `receive`。不授权 `--config-path`、通用 shell 或从用户可写路径执行 root 程序。
6. 真实核实生产架构、Compose 有效配置、运行镜像/manifest、Runtime mount、健康及备份服务。既有固定 GHCR 前驱可识别；新镜像均以归档直传，不需要 GHCR write/pull token。首次正式加密备份、恢复前提和生产业务验收必须在授权窗口按现有合同确认，不能拿隔离测试证明替代。
7. 授权后先预演，再执行本地入口并核对精确 SUCCESS receipt。公网开放、Provider/PG03 商业使用、账务处理按各自授权和验收合同处理。本轮结束后不自动切换或开放流量。

## 2026-10-24 首次生产交接只读补充

当前生产 Gateway 已运行 a032/ed6 且 loopback200，但 Caddy仍隔离、公网521，manifest仍旧FAILED，实际四层Compose和备份/boot基线尚未闭环。普通本地helper要求成功前驱和公网健康，不能直接接管这个现场；不要仅安装旧验收分支资产后运行正式命令。验收分支落后正式main138提交，备份/Runtime/boot须保留main新增合同后整合。

唯一切换顺序、精确停链清单、root资产条件与集中授权申请见 `evidence/local-cutover-preparation-20261024/CUTOVER_PLAN.md`。本轮仅准备，没有远端或生产写入，也没有重跑30个真实测试。
