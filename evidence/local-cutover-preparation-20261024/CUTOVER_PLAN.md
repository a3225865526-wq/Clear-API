# ClearAPI 首次本地生产切换准备与集中授权申请

**状态：只读准备完成；生产执行仍 BLOCKED / 未授权。** 本地工具的 30 场景验收继续有效，本轮未重跑任何场景、未新增发布系统。只读现场暴露的首次接管/代码整合问题不能由隔离 PASS 替代。

采集时间：2026-10-24 05:04–05:12 Asia/Shanghai；完整原件见本目录 JSON 和远端 workflow 快照。本轮只更新本地准备记录，无远端 workflow/变量/Environment 写入、Push、生产文件写入、安装、服务启停、备份触发或公网开放。

## 1. 最新事实

| 对象 | 只读结果 |
|---|---|
| GitHub main | `a032815cb6244a7ee5e812de82f8300c02554e99`；本轮未改 |
| 自动开关 | AUTO_RC=false，AUTO_PRODUCTION=false，但所有旧 workflow 仍 active |
| 在途运行 | queued/in_progress/waiting/pending/requested 均 0；执行切换前必须再核实 |
| 代码关系 | 验收分支 `49f23003…` 相对 main 本地独有 4 提交、落后 138 提交，不能直接推为 main 或拿旧分支构建最终生产应用 |
| Gateway | `0a9a93c02646…` 正在运行，restart=no、重启计数 0；源码 a032815c，镜像 `ed6f93a91285…`；只绑 127.0.0.1:3000；本地 `/api/status`=200 |
| Runtime | durable `/opt/clearapi/runtime/new-api` 已存在 root:root0750，实际只读挂到容器 `/run/clearapi`；身份文件匹配当前进程；既有 helper 的只读 `--check` PASS |
| Caddy/公网 | 原 `54c8550bbaca…` 已停止、restart=no；原 Caddyfile SHA `6b124666…` 未变；源站443未连通，独立公网 `/api/status`=521 |
| Executor / PostgreSQL | 原 Executor `1081a1b576ad…`、PostgreSQL `99299db0bed7…` 正在运行且 healthy；PG 17.6，无公开 DB/Executor 端口；本轮不重建/启停 |
| PG03 | 当前 Gateway `CLEARAPI_POOL_ENABLED=false`、GIN_MODE=release；Executor 虽仍运行，不等于 PG03 商业开放；保持现状，不作专项商业验收 |
| 发布记录 | manifest 仍为旧 c50a53c1 / 22ecaa46、status=failed、pool_mode=pg03；controlled hold 仍 isolated。不能把当前私有 Gateway 健康改写成原失败部署 SUCCESS |
| Compose | 实际由 base + compose-pg03 + compose-candidate + compose-website-recovery-39957948359.json 四层组成；末层才将 Gateway 指向 a032/ed6、pool=false、durable runtime |
| 备份 | timer enabled/active，但最近 service failed。日志存在 Compose/运行镜像错配及无 running Gateway 的失败；最新证明 20261023T102102Z 为旧 c50 停止态 schema2，并非当前 a032 运行态备份。密文 12,330,812 bytes / SHA995100eb… 实测匹配，仅证明完整性 |
| 开机依赖 | tmpfiles、directory-before-Docker、Docker drop-in、identity-after-Docker 均已安装；`check-installed` PASS，但当前 live boot `check` BLOCKED。不重启主机来试错 |

注：39957948359 是已成功的 Release Candidate run，不能仅因恢复 overlay 文件名含该编号就把它当成成功的生产恢复记录。完整 Runtime 配置/API 门禁、真实登录/支付/Provider 本轮未验证，不用静态身份检查冒充。

## 2. GitHub 最小安全停用顺序

旧自动链仍是 CI → PG03 RC → Deployment rehearsal → Automatic production → Deploy Production；AUTO=false 不是永久退休，也不关闭手动恢复和 SSH pull 入口。

先重核开关及在途运行；如出现活动写运行，立即停止切换并先审查；不盲目取消已进入生产的运行，禁止并行切换。随后依次停用：

1. Automatic production release `387486994`。
2. Deploy Production `378101050`、Failed production recovery `390042545`、GHCR Pull Rehearsal `378227091`。这四个是先要切断的生产可达路径。
3. PG03 RC `384204592`、Release Candidate `378068457`、Deployment rehearsal `387553097`、PG03 successor grant `391551026`，退役旧镜像/排练/授权来源，使 GitHub 后续只承担源码、历史和既有源代码 CI。

具体未来命令和当前源文件 hash 见 workflow-retirement-plan.json；本轮没有执行。保留 CI 和 main 的保护检查，不删除 Secrets、不改 Environment、不绕过审核。停用后再允许任何 Push。集成提交同时保留终端 job 的退休阻断并移除旧 workflow_run 链，不能仅依赖 disabled 可复用 workflow 的状态。现有 prepare-local-cutover.py 仅覆盖四个文件，不足以单独覆盖当前新增的恢复/SSH入口。

## 3. 唯一代码集成方式

保留验收点49f23003；在当前独立分支上正常三方合入固定的 production/main，再经正常 PR 和现有必需 CI 集成，不 reset、不 force-push、不覆盖其他 worktree，不擅自合并/关闭其他 PR。

本轮仅执行 git merge-tree 预览，未改 index/分支：真实冲突只有 CLEARAPI_MASTER_STATUS.md（add/add）和 scripts/backup-identity.py。以 main 的完整生产恢复历史为底追加本地进度；备份工具保留 main 的停止态 schema2、successor_completion、严格路径/新鲜度/密文检查，再合入本地 archive 支持。不要用 6ef4b2ab 的旧分支整文件覆盖 main e1e21e02。

四个本地独有提交还包括两个合法 shop 修复 965f629e/d021bf6f；三方预览虽自动合并 shop 两个文件，仍需核对语义，不能静默丢掉或用旧文件覆盖新业务。Runtime 虽无文本冲突，也有真实语义冲突：本地 helper hash8c7b8362 与生产/main afcefce7 不同，boot 工具固定后者且只认 GHCR。必须将既有身份/boot 合同与归档能力一起整合并冻结资产，不能简单复制。保留 main 的 backup.sh 7edb9ce9，不回退本地602db567旧备份脚本。

现有 main 保护要求分支保持最新及 Backend vet/build/test/migration、Frontend typecheck/test、Secret scan 三个上下文；保持不变。不重跑30场景，不新增 PG03 商业或财务专项；仅执行集成实际涉及的既有必要检查/必需CI。

## 4. 当前三个停止点

1. **候选未整合/未冻结**：138 个 main 提交不能丢；备份和 Runtime/boot 的不同合同需合并，最终候选 commit、资产 hash 目前不存在，root-assets-plan.json 明确 executable=false，禁止把旧验收 SHA 当成最终生产资产。
2. **首次接管不等于普通更新**：本地 helper 只读两层 Compose、要求成功 manifest，且 preflight 必须公网健康。当前四层、旧 FAILED、Caddy隔离/521必然不满足。它不会修复 Gateway、结束旧事故或启动 Caddy。须沿用既有受控恢复/交接机制，保留原 FAILED/hold 原字节和真实前驱证据；完成一次受控接管/Compose有效配置及 root 环境接线，不能伪造 success 或用 TestTarget 跑生产。若不能形成合法接管条件，停在本地准备，不安装、不部署。
3. **备份和 boot 基线尚未闭环**：Compose 无受控环境时缺少既有 KEY/ID/EPOCH 插值；只记录所需变量名，未导出值。必须复用 root 受控环境和 secret 文件引用。备份 service 当前 failed，最新旧停止态包不能当新运行态备份；boot 依赖已装但 live check 未通过。不能通过 reset-failed、删 journal、关闭身份/备份检查或恢复旧数据库来“过门禁”。

因此旧“Gateway退出127、缺/run/clearapi”已被本次现场取代；当前不需再次按旧故障启动原容器，更不能假定换部署来源就能恢复网站。30场景是本地工具合同验收，不能解决这些首次生产交接事实。

## 5. root 资产和允许影响

详见 root-assets-plan.json，候选 hash 必须在整合后冻结：

- `/opt/clearapi/bin/clearapi-local-deploy.py`，root-owned0750/0755；本地 JSON 与启用哨兵 root:root0600，不能放测试字段。
- 同源 Runtime identity helper、现有 runtime-boot-recovery 及四项 tmpfiles/systemd 资产；保留目录0750、身份0400、只读 bind、原有顺序与 hash pin。更新/daemon-reload不包含 Docker restart、reboot、PG/Executor重启。
- `/opt/clearapi/backups/bin/backup-identity.py` 与实际备份入口 `/opt/clearapi/backups/bin/clearapi-backup.sh`；保留 main 的 append-only checkpoint 和 stopped-writer 语义，不做 PG03 商业上线测试。
- 受限 sudoers0440、visudo通过，仅固定默认配置 preflight/receive，不允许 --config-path/test 或 shell。当前管理员已有 NOPASSWD:ALL，不在本轮顺手变更管理通道，也不把新增规则误称为已收紧全部权限。
- 首次受控接管须保持所有非镜像有效配置、secret文件引用、数据卷、路由、充值/支付/余额/计费和 Gateway pool=false。Executor进程、PG数据及原Caddy配置保持；只在最终网站恢复步骤启动原Caddy并恢复隔离前经核实的restart策略。

## 6. 唯一切换顺序（仅授权后的执行方案）

**停用旧 GitHub 生产入口 → 安全整合/现有CI/冻结候选 → root资产安装与合法首次接管 → Windows本地构建 → 校验归档后SSH直传/单Gateway更新 → 最小健康与身份检查 → 原Caddy受控恢复网站。**

步骤3是明确停止点，不是跳过 preflight 的许可。不能在其尚未闭环时照抄 .ps1 命令；也不能为满足这个顺序提前开放 Caddy 绕过公网健康前置。首次受控接管须在既有恢复/部署主线上明确容纳“保持公网隔离直至新候选通过”，不能另造发布系统或弱化正常模式检查。

安装前 under existing deploy lock 复核本目录基线漂移、保留原件，冻结候选源码/镜像/资产；正式 fresh backup 必须真实成功且核对密文与实际运行前驱，恢复/旧镜像兼容性证据优先核对已有适用原件，确无适用证据即停止，不把隔离证明或旧schema证明改绑新身份。

最小健康：PG现有healthy；Gateway loopback /api/status、实际容器/source/image/Compose/Runtime一致；已有配置/身份门禁不降低。最终再核对原Caddy ID/image/config哈希和隔离记录，按明确授权恢复该Caddy；公网HTTPS/api/status和首页/登录页面壳可用。页面壳不代表真实用户登录、支付或Provider验收。PG03保持off，不触碰Executor商业能力。

## 7. 一次集中授权申请（本轮未生效，当前停止点未消除）

申请授权范围：仅指定仓库和该独立工作区，依上述顺序停用8个旧 workflow；正常 Push/PR/必需CI/合并本次整合成果；完成现有工具的最小首次接管/Runtime/备份/boot合同整合并冻结可审查资产；在指定VPS安装这些精确root资产及受限sudoers；创建本次正式备份；Windows构建并SSH直传，只替换Gateway；全部门禁通过后启动原Caddy，恢复普通网站入口。PG03商业开放不在授权内。

**生效前置条件：**执行前用户明确同意该范围；代码/资产/镜像身份已冻结且可核对；旧写链及在途运行均关闭；前述三个停止点真实解决；当前基线不漂移；真实备份与必要恢复前提成立。现阶段不能把此申请当成已可执行或已获批准的生产部署；任何条件未满足就停在本地整合/保持现有隔离，不继续安装或上线。

**影响：**源码历史新增正常提交/PR，旧GitHub发布能力停用；root安装资产与必要接管配置发生审计化更新；Gateway单服务短暂替换，最后恢复网站公网。PG/Executor不重启，业务配置与账务数据不手改，不进行PG03商业开放、OS重装、reboot或Docker restart。

**失败停止/安全回退：**安装失败恢复该次审计归档中的工具/配置原件，保留证据且不启动服务；备份或身份失败在替换前终止。已证明仅前端且前驱/数据兼容、配置和真实身份均可恢复时，按既有合同回到本次真实前驱a032/ed6，重新验健康；不是回到旧c50或旧51dd。后端/迁移或DB状态UNKNOWN则停止新Gateway、保持Caddy隔离，不自动恢复DB或切旧后端。网站恢复失败则停回本次启动的原Caddy并恢复restart=no；如隔离状态不可验证标记UNKNOWN并停止。不得清除原FAILED/attempt/hold或自动重启旧GitHub链；无论哪步失败都不以重复执行掩盖。

本轮结束后不执行本申请。详见本目录原始只读证据；若用户只授权停链/整合而未授权网站开放，则严格停在对应边界。
