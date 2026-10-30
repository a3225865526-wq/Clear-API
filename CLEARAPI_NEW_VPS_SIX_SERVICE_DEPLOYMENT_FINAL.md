# CLEARAPI 新 VPS 六服务部署工具与离线验收交付

记录日期：2026-10-31（Asia/Shanghai）。本轮只操作 Windows 本地隔离环境；未连接新旧 VPS、上传生产文件、迁移真实数据、操作 DNS/Cloudflare、启用正式账号池、调用收费厂商或推送 GitHub。

**结论：六服务适配代码和固定离线包已交付；多项真实部署、故障和恢复演练通过，但本轮不能认定 `OFFLINE_DEPLOYMENT_READY`。** 最后本机 Docker Desktop 控制面返回 500，继而请求超时、隔离 SSH banner 超时；最新运行时配置核验和最终封装版本的完整重放、同包重复部署及测试环境停机核验尚未完成。不得拿较早 helper 的成功回执证明最后版本已经通过。

## 1. 版本、工作区和包

- 基线：`6f92278ad3caed5dfcafb12c2561cfb99756182b`，既有成果保留。
- 本轮工具提交：`669c5691b4c2fc408c9a4e3d70d873dd19954551`，本地分支 `codex/new-vps-six-service-helper-20261031`，未推送。
- 工作区：`D:/AI/API/worktrees/clearapi-new-vps-six-service-helper-20261031`。原工作区的无关改动未覆盖。
- Gateway 应用/源码归档固定：`92edfc345d9b9526379ed25363bb79abf9839874`；Sub2API 固定：`d851a38477597157e78450d30af2d8504764d085`。本轮没有修改 Go 应用、核心 Billing、ORM/schema、前端、PG03 或平台接入档案，没有重建这些未变应用镜像。
- 离线目录：`D:/AI/API/runtime/six-service-helper-20261031/release-final`。
- 单文件部署包：`D:/AI/API/runtime/six-service-helper-20261031/release-final.tar`，`560998400` bytes。
- 部署包 SHA256：`5df0ea860d9ae1f65c640a3e95924bb1619b1fe7938035b696e5002290f16180`。
- `images.tar`：`514803200` bytes，SHA256 `b3f148e24329dddf7e1177347bac005e64b3382dd25aab5b0c91fff1a1d2775c`。
- `gateway-source.tar`：`45936640` bytes，SHA256 `d41cf9c4d86da20ee54b55b00a3a0c4db4ce5eba290d8bb159155f08f8bdb3cb`。

`operator-source.json`记录确切工具提交、应用提交及五个 helper 摘要；`SHA256.json`列出 26 个文件，目录及最终 tar 的全部成员/摘要均核实。构建复用上轮已独立核实的镜像归档，同时重新验证六镜像 config ID、平台和精确清单，不依赖运行中的 Docker、GHCR 或 Actions。包内不含测试数据库、dump、私钥、运行 env、可用 Token 或管理员密码。已检查本轮 23 个已知合成运行凭据字面量，包和证据未命中；不将此描述为全面凭据审计。

## 2. 实际六服务与持久化

清单来自实际 Compose，不从旧 helper 的单 Gateway 假设推导。

| 服务 | 固定镜像 | 启动依赖 | 内存硬上限 | 初始持久化目录/端口 |
|---|---|---|---:|---|
| gateway-postgres | postgres:17.6-alpine | 无 | 256 MiB | `/opt/clearapi/stack/data/gateway-postgres`；不发布 PG 端口 |
| sub2api-postgres | clearapi-sub2api-postgres:17.11-266d3c257594 | 无 | 256 MiB | `/opt/clearapi/stack/sub2data/postgres`；不发布 PG 端口 |
| sub2api-redis | clearapi-sub2api-redis:7.4.11-858f009f9709 | 无 | 96 MiB | `/opt/clearapi/stack/sub2data/redis`；不发布 Redis 端口 |
| new-api | clearapi-local:92edfc345d9b9526379ed25363bb79abf9839874 | Gateway PG healthy | 448 MiB | `data/gateway`、`data/logs`；内部 3000 |
| sub2api | clearapi-sub2api:d851a384 | Sub2 PG、Redis healthy | 384 MiB | `sub2data/app`；`127.0.0.1:18882` 管理入口 |
| edge | clearapi-edge:nginx-8541484afbc9 | new-api healthy | 64 MiB | 固定只读 nginx.conf；`127.0.0.1:18080` |

恢复后的 PG/Redis/app/logs 使用独立 `/opt/clearapi/stack/recovery/<backup-id>/...` 目录和受保护 env；保留旧目录，不假称仍指向初始位置。数据网络分别为 internal 的 `gateway-data`、`data`；应用网 `172.30.81.0/24`，edge `.2`、Gateway `.3`，动态范围 `.128/25`，避免固定 IP 碰撞。Sub2API 信任 Gateway `.3`，Gateway 信任 edge `.2`。公网 TLS、真实反向代理链、证书及端口防护待新机现场验收。

经典 Docker config ID（完整 manifest/repo digest 同时保存在 images.lock.json）：

| 服务镜像 | config ID（sha256） |
|---|---|
| new-api | a70bf2ccf6da4442666175236754d14bdf2ded5e92c4ffbfcf5756fdeada848c |
| gateway-postgres | d741b376874687de90374fd34f55c6b2760e8f7bd7e4ae5cd47f50757fc08cf8 |
| sub2api | 62cb3769069c6a9c3c7fa708defd71229fe2befe396fc7cda32720ec69631f82 |
| sub2api-postgres | ab20e934b27ba3ff927c1b4c9ff8cf6c32f315f743efe68dd3441182a87fb085 |
| sub2api-redis | f84b0c4678011602b9b98c227a4dcd5468bf8b088b02fdd4165cb7758bad8058 |
| edge | 5253dc86cc93ac6249902934655c6f7c959d8caa45a8c2ecc0b95953834d8ee8 |

只交付 Linux/amd64 镜像；不声称 arm64 已验。基础镜像以固定上游 digest 标识，Sub2API 以既有源码提交和镜像标识，不声称本轮重新编译/审计了上游源码。

## 3. 原 helper 的最小适配与安全门禁

正式链路保持 Windows 本地构建/固定包 → 原 SSH 二进制流上传 → 原 `clearapi-local-deploy.py` → 健康/回执。没有第二套生产部署入口。

- `scripts/deploy-local.ps1` 只增加显式 `-StackTarget` 委托；原默认分支和全部既有 Python 方法（除必要 main 分派）逐项源码/AST 比对不变。新模式不能组合 `TestTarget` 或 `FixturePath`。
- `scripts/local-deploy-stack.ps1` 校验 Windows 配置/私钥/known_hosts 权限、固定目标/端口/身份、StrictHostKeyChecking、包 SHA、root 配置 SHA、应用 commit、generation。DryRun 不上传、不修改远端。
- 新 `scripts/local-deploy-stack.py` 只能由原 root helper 的固定 `stack-*` 命令使用；配置固定于 `/opt/clearapi/config/stack-target.json`，需 root 私有 activation、文件/目录权限、Docker daemon ID、架构及全部 helper 摘要吻合。
- 新旧 activation/state 互斥；sudo 仅允许固定 stack 模式，不允许 shell、任意 config path 或旧目标命令。新目标独立 target ID、配置、密钥、目录和回执。
- 源归档 Git pax commit、OCI revision、镜像 config ID、包清单/长度/SHA 全部核对；拒绝重复成员、非普通文件、越界路径、非本地镜像依赖、非预期六服务、额外公网端口和不受控持久化。
- 沿用部署锁和原子状态；写 journal 后切换。先关闭 edge/停止写服务，做新鲜 GPG 加密备份，再启动状态服务、原生 bootstrap/迁移、应用及健康校验；原生 `production-bootstrap` 必须明确返回 `status=verified` 才放行流量。
- 保留真实 production、钱包/任务结算、Token 加密、Owner、条款和隐私门禁。初始数据经现有 CLI 合法初始化，未伪造 migration/proof/条款确认。
- 回执绑定目标、root 配置/运行 env、包、helper、六镜像、服务健康、备份。最终版本还核对运行容器 Compose hash、内存/Swap/CPU/PIDs、env、mount、网络和端口；**这组最后新增核验只有静态验证，尚未完成真实重放**。

已直接修复演练暴露的工具问题：Compose 子进程抢读 SSH 上传 stdin（改 DEVNULL，并有回归）；固定 IP 下 compose run verifier 冲突（改实际 Gateway 内 exec）；sudo 无参数恢复匹配；中断后的空初始化状态处理。没有改变认证/Billing 应用逻辑。

旧 VPS 兼容性的证据范围：原分支和方法保留，未对旧 VPS 安装新模块/配置，也没有接触旧 VPS。本轮 `-DryRun -FixturePath` 实际 exit1，仍被旧敏感路径门禁正确拒绝 `deploy/sub2api-low-resource/MIGRATION.md`；不得把这批新目标运维变更直接作为旧 Gateway 发版，也不得删除该门禁。历史已验旧应用部署能力保留，本轮不声称重新完成旧生产 SSH/部署验收。

## 4. 已完成的真实本地演练

证据根目录：`evidence/new-vps-six-service-20261031/`，`final-install/` 为第二套新建目标，`prototype-phase/`保留前期结果；不同文件记录真实 helper SHA，不混成同一版本。

1. 两套独立嵌套 Docker/SSH 目标，Windows 仅连 `127.0.0.1:18961/18962`。实际 root 安装器、sshd key-only 生效配置、visudo、logrotate debug、新目标预检与 Windows DryRun 通过。
2. 第二目标从空数据库/新受保护配置开始，经原 Windows SSH 流上传完整固定包，六服务 healthy，ClearAPI/Sub2API HTML 均可访问并记录摘要；初始 type59 启用数、Sub2API 账号/Key 均为 0，管理员钱包 0。
3. 独立批准的测试专用 Mock 配置：4 并发普通请求 4 次、4 并发池请求 4 次、SSE 2 次，每次 input10/output2、quota14。合成钱包 `999860`、Token/applied/消费日志合计 `140`，10 条唯一请求、outbox 待投递 0。内部 Usage 增量和内部钱包断言通过。业务驱动随后使用错误 PUT 关闭端点失败，已改为原生 POST status 并实际关闭，未重复收费请求凑结果；该批逐请求时长没有完整落盘，不虚构数据。
4. 池 fixture 只读克隆以前负责人已确认条款的合成 Sub2API 库到新测试数据库；未创建/伪造新的合规确认，不含厂商真实 Key/账号。测试库和凭据只留仓库外私有 runtime。
5. 停写后新鲜加密备份 `20261030T223842-c57b5c9b`：PG17.6、PG17.11 逻辑 dump/globals、Redis RDB、应用/env/受保护配置。密文 SHA `082644105fe1e6794a20e4a52bdae5f4663767c9d9b2f7192b74b861c96d8309`，207570 bytes（小型合成数据，不能推断生产备份大小）。
6. 两套 PG 恢复到新目录；全表行、schema/索引/约束摘要与停写快照吻合，旧目录保留。Redis 非空合成键通过 RDB 导入，再生成 AOF；值与 TTL 均恢复，观察 TTL3539 秒。两轮重启后钱包、预留、唯一日志和 outbox 与恢复前完全一致。
7. 恢复后的加密 relay Key 仍可完成额外 SSE（8.921 秒）；新增第 11 条唯一日志，最终合成钱包 `999846`、Token/applied/log quota=`154`、outbox0。随后再次关闭所有测试池渠道、内部 Key/账号调度和 relay Token。
8. 实际宿主 logrotate debug/force 通过，21 个已轮转文件，覆盖恢复目录；Docker local 日志每服务 10MiB×3。新机的定时执行仍须现场确认。

最终同包重复执行的 `already_applied` 实测及**最后封装版本**从空目标到恢复的完整重放未完成。实现具备 generation 和同包分支，但本报告不以代码分支替代幂等实测。

## 5. 故障注入与恢复边界

以下为实际隔离注入结果，详见同名 JSON；成功恢复记录包含当时 helper SHA，不能冒充最终 SHA 的回归。

| 故障 | 实际方式及观察 | 恢复/边界 |
|---|---|---|
| 镜像缺失/摘要错误 | 删除离线镜像成员/改错 config identity，exit2 | 切换前停止 |
| 上传损坏 | 完整损坏流、实际 SSH 仅上传4KB后断开 | 拒绝，无 generation/数据变更 |
| SSH 主机错误 | 换错误 pinned host key | SSH255，未进入 helper |
| 磁盘不足 | 真实 ext4 临时占位至剩余低于8GiB | 预检拒绝；仅移除本次占位文件 |
| 内存不足预检 | 配置要求高于实际 available | 拒绝；这是阈值注入，不是全机 OOM |
| 实际 OOM | 测试 edge 12MiB 上限内分配 | OOMKilled=true；故障停止，批准后配置恢复；未故意 OOM 共享宿主 |
| PG 启动失败 | 无效 PG 启动参数 | switching journal/备份保留，默认人工恢复 |
| 迁移失败 | 实际执行故障迁移命令返回失败 | 不放行入口；不声称覆盖任意真实 DDL 半迁移 |
| Redis 不可用 | 故障配置导致 Redis 无法健康启动 | 停止发布，保持数据，批准后恢复 |
| 单服务健康失败 | edge 健康探针故障 | 拒绝成功回执，保留停写状态 |
| 服务重启/SSH中断 | 应用切换期间杀掉 SSH 客户端 | 检查 durable journal；禁止盲目并发重试；按所处阶段恢复 |
| 关键账务不一致 | 隔离合成钱包额外 +1 | 检出并关闭入口；拒绝旧快照覆盖新钱包，未自动退款或补扣 |

`received/backup_ready` 且未触及新状态可恢复前驱配置。进入 `switching` 后默认拒绝自动回库；只有 Gateway config ID 完全相同、资金摘要不变、负责人 root 私有批准绑定当前完整数据摘要，才能恢复旧配置并**保留当前数据库**。该配置恢复实际通过，不等于有新交易后的完整业务回滚。

完整备份恢复同样要求明确 root 批准、精确 backup/目标/daemon/解密材料 SHA/当前数据吻合，使用新目录而非删卷。账务发生新写入/差异、应用镜像/迁移未知时，负责人必须取证、对账并批准保留新交易的方案；工具不会用旧库抹掉新交易。账务 +1 试验只在合成 fixture 上修复后才继续，未改真实钱包。

## 6. 2GB / 50GB 的实际结果和限制

- 实际约束：父 cgroup 1.7 CPU、1536MiB RAM、memswap 同上限（无额外 Swap）、pids1024；六服务与嵌套 dockerd 一并受限，另为宿主预算 0.3CPU/512MiB。这不是完整 2GB VM；共享宿主内核/OS，不把预算等同真实新 VPS 验收。
- 实际 ext4 稀疏块盘为 **50,000,000,000 bytes**，Docker data-root 和 `/opt/clearapi` 都在盘上；格式化后可用容量约45.5GiB。OS 根盘在外，另以至少8GiB预留约束；不冒充全系统真实50GB磁盘。恢复后样本已用4499042304、可用44375552000 bytes，尚无真实客户库/长历史。
- 六容器硬限额合计1504MiB；CPU总额2。Gateway/Sub2 Go目标320/288MiB、GOMAXPROCS1；SQL open10/idle2；两PG shared_buffers32MB、work_mem1MB、maintenance16MB、connections20、max_wal256MB；Redis48MB/noeviction、AOF everysec、连接池16/idle2。沿用已有低资源方向，未开展大规模调优。
- 正向 final-install 样本父 peak `1610829824` bytes（约1536.2MiB，已触限回收）、memory.max 事件非0、父/服务无业务阶段 OOM；恢复后 SSE 样本 current `1165746176` bytes。不能据此宣称内存宽裕或任意并发可靠。
- 首版仅保留已证实的 **4 并发 Mock** 范围；edge 连接上限8只是拒载保护，不能当8并发真实厂商承诺。SSE 已测约9秒，不是300秒真实厂商/长时间稳态验证；代理关闭重试、buffering off，超时360秒，Gateway streaming300秒。
- 无额外 Swap 的小数据演练通过，不将 Swap 当资金安全或内存容量保证。真实新机的 Swap、CPU steal、page cache、日志/WAL/AOF/备份增长与长期流量待现场测。
- helper 最低剩余空间8GiB且不少于上传包4倍。这是固定底线，**不是按真实数据库大小计算的完整备份预算**；备份同时存在 dump、snapshot tar、密文，恢复还保留旧库和新库。真实库到位后应按最坏临时空间上调 root 配置 `minimum_free_bytes` 并实测，不得在50GB大库条件下仅满足8GiB就宣称安全。
- logrotate copytruncate 存在瞬时文本丢失边界，资金事实由持久数据库/备份保留。不得删历史账务/PG03数据节约空间。2GB可靠商用及长期容量目前仍未获证明。

## 7. 验证命令、未完成项及 Docker 故障

实际执行并观察：

- `python scripts/ci/test-local-deploy-stack.py`：2项回归 PASS（SSH stdin、原生 status 精确判定）。
- `python deploy/sub2api-low-resource/package.py build .../release-final --image-archive .../final-package/images.tar --image-archive-sha256 b3f148e...2775c`：PASS，固定 archive全量身份核验。
- `python deploy/sub2api-low-resource/package.py bundle .../release-final`：26项 PASS，生成上述包；独立 tar成员/摘要复核 PASS。
- 最终包3个 Python模块 `python -m py_compile`、两个 PowerShell脚本 Parser.ParseFile、`git diff --check`：PASS。仅生成的 syntax cache 已清除，不作为包成员。
- 实际 root安装、Windows `-StackTarget -DryRun`/SSH部署、备份/restore/restart/故障命令由 `evidence/.../reproduce/` 驱动快照和 JSON记录，失败 attempts保留；不以日志里写 PASS 代替回执。
- 未重复无改动的历史16例完整矩阵、全前端构建、三数据库兼容矩阵/真实厂商；本轮无应用数据库实现变更。恢复实测仅针对本轮部署实际 PG17.6/17.11，不外推到所有MySQL/SQLite或其他版本。

最后观察：Docker Desktop client29.7.2/API1.55 对 version/ps/本轮父容器 inspect/exec 返回500，随后 `docker version` 20秒超时；18962 SSH banner超时。Docker Desktop/backend进程存在不表示 engine健康，根因未确认，不编造“Docker是被本轮OOM/磁盘打满”的结论。未全局重启 Desktop、WSL、停止Dify或其他项目。

重启 Docker Desktop 会中断本机其他容器，超出本轮仅隔离目标的授权范围，已单独请求明确批准；**未收到批准就不执行**。最终已验证的业务关闭状态见第4节，但故障后的两个测试父容器是否已停机无法再次确认；合成材料/旧数据保留，不能报告已清理。

剩余本地闭环（必须完成后再升级离线状态）：恢复健康Docker控制面；核实既有隔离目标/journal并保全数据；安装最后包内确切helper及更新root hash；最后版本新空目标部署与原生ready；同包两次执行核实already_applied且数据不变；实际运行配置漂移拒绝/复原、受影响备份恢复/故障路径回归；核实关闭测试Key/渠道和停机，再记录最终包SHA对应的真实回执。无需重跑无变化的13平台/全Billing/全部历史测试。

## 8. 独立状态与最终十项回答

| 状态 | 本轮判定 |
|---|---|
| CODE_READY | 既有应用/默认关闭配置已准备；新helper实现已提交、定向静态回归通过，最新真实验收仍待闭环 |
| OFFLINE_DEPLOYMENT_READY | **暂不能认定；BLOCKED**：Docker控制面及最后版本重放/幂等/漂移检查未完成 |
| NEW_VPS_ACCEPTANCE_PENDING | 保持；无真实新IP/SSH/资源/出口/TLS现场验收 |
| REAL_PROVIDER_ACCEPTANCE_PENDING | 保持；真实厂商收费调用0 |
| PRODUCTION_LAUNCH_PENDING | 保持；未授生产迁移/数据切换/正式启池授权 |

1. 原Windows helper是否完整适配：实现已完成并部分实测；最终验收尚未完整闭环，不能回答“已全部通过”。
2. 六服务能否安全离线部署：较早固定版本的真实空库部署/健康通过；最后交付包未完成完整重放，不能将二者混同。
3. 旧VPS流程是否兼容：默认分支/原方法/门禁完整保留，新目标隔离；未重验旧生产，不可把包含新目标迁移文档的整批变更直接走旧单服务发布。
4. 2GB/50GB：受限模拟的小数据/4并发Mock/恢复通过且存在触限；不是完整VM或长期商用容量通过。
5. 备份恢复/回滚：两PG全量逻辑及Redis恢复、两轮重启、同应用配置故障恢复通过；新交易后业务回滚明确人工对账，不自动回库。
6. 应用阻断：未发现需修改的应用/Billing代码阻断；当前已知阻断在本机Docker和最后工具验证，未完成项不能视为无风险。
7. 包位置与SHA：见第1节，确切工具版本669c5691；应用镜像/源92edfc345。
8. 新机到位：见下一节；实际SSH/公网资源验收不可提前完成。
9. 厂商收费：仍缺拟用厂商合法凭据/商业授权、具体模型/协议/真实Usage、可执行单请求与累计/物理attempt费用及输出上界、真实出口与Owner值守/调用授权。无需重买13家；存在协议缺口或无硬费用边界的平台继续关闭。两项既定未知Usage/退款政策不重新设计、不自动退款。
10. `OFFLINE_DEPLOYMENT_READY`：**否，保留明确阻断**，不能用离线阶段部分通过升级生产READY。

## 9. 新 VPS 购买后最少步骤

1. 负责人授权后核实真实IP、独立SSH指纹、amd64/Linux/真实2核2GB/50GB、CPU共享情况、时钟、出网、磁盘和Swap；准备Docker/Compose、Python、sudo、GPG、logrotate和key-only专用用户。本地已测Alpine3.24.3、Python3.14.8、Compose5.6.1、GPG2.4.9、logrotate3.22.0，不推断所有发行版兼容。
2. 用包内 target.windows/server.example.json 登记真实daemon、helper/包SHA和独立受保护配置；保管GPG私钥及迁移必须保留的SESSION/CRYPTO/Token/JWT/TOTP密钥，管理员/Finance Owner、条款隐私只能由负责人提供/批准。按 SIX_SERVICE_RUNBOOK.md 安装固定target并DryRun。
3. 核实公网TLS/证书续期/可信代理/真实SSE、DB/Redis和管理端口不公开、logrotate调度；用真实数据量证明备份/异地加密恢复空间与资源余量。全机长期容量不因本地成功自动放行。
4. 取得独立迁移授权后，依MIGRATION.md停写一致备份、隔离恢复/钱包订单核对、回调任务去重和单写切换；保留旧机与历史数据。DNS切换另按授权执行。
5. 少量合法厂商/具体模型/预算硬界实测，核对真实Usage、客户钱包和平台差额；Owner核准上线及启池。未知仍按既定核查/退款政策，未获授权不收费、不启池。

唯一总控仍是 `D:/AI/API/worktrees/clearapi-controlled-launch-20261020/CLEARAPI_MASTER_STATUS.md`；本轮只向其追加，不改历史或其他同名副本。详细操作为包内 `SIX_SERVICE_RUNBOOK.md`，原OPERATIONS历史保留。new-api/QuantumNous项目身份、归属和许可证保持。
