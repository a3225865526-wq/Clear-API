# 最小生产接管准备与一次维护授权申请

本轮只有本地修复、定向验证和只读生产核对。**尚未解除隔离、启动 Caddy、安装 root 资产、改文件归属、调用生产备份、停远端工作流或 Push。** 本文件中的操作均为待批准方案。

## 1. 原隔离可以通过最小兼容方案正式解除，但未执行

旧 `Recovery.gateway/snapshot` 固定要求 f8f963a7/c50/22ec 及 `/run/clearapi`，旧 `WebsiteRestoration.restore` 还要求旧 GitHub 恢复运行和 receipt。当前是 0a9a93c0/a032/ed6、持久 Runtime；伪造旧 receipt 或替换回旧 Gateway 均不成立。

兼容入口复用既有 TrafficHold、Runtime、boot、正式备份、Caddy 校验/开启/失败停回。只新增一份共享元数据读取模块，未新增服务、工作流、审批平台或 checkpoint：

- root `adopt-check/adopt-running` 核对当前实例；`adopt-running` 只追加 `local-deploy-state.json` 和 `compose-local.yaml`，不运行 Compose up/start。状态明确为 **adopted**，不把历史 FAILED 写成 success。
- 原 `release-manifest.json`、`pg03-target-manifest.json`、`controlled-launch-state.json` 字节保留；原四层 Compose 内容也保留。第五层只承接后续本地镜像覆盖，读取现有 root-only `config/pg03/pg03.env`，不新建凭据来源、不复制生产凭据到本地。
- 备份、boot、本地部署使用同一接管状态。Docker engine ID 与 OCI config digest 分开记录；首次接管仍指向正在运行的原镜像。
- `restore-adopted` 在原恢复工具内增加明确 root 维护入口，复用原 Caddy 开启和异常停回函数；先验证原隔离、当前身份、boot 与一次真实正式备份，再开原 Caddy。
- 网站全部健康后，沿用原 append-only website-restoration result 记录实际开放结果。Caddy 只读探针识别该结果为历史 hold 之后的有效状态，保留原 journal 的 `stage=isolated` 历史事实，不覆盖它。原 PG03 GitHub 恢复入口的认证和失败合同仍保留。

**现场阻断未绕过：** 09:25 的候选只读函数执行到原 Compose 文件权限检查时 BLOCKED。`/opt/clearapi/app/docker-compose.prod.yml` 当前 UID/GID=1000、0644、SHA256 `c5fc8ca1ecd1711aa2c68efe5a582a081ed7dd70df20ce85cde51c0782d482fa`。root 工具不能信任普通用户可改写的 Compose。最小必要条件是经授权后仅改为 root:root、保持0644和全部内容。没有放宽读取校验。详见 `actual-adoption-readonly.json`。

这份 BLOCKED 是真实生产前置结果，不是部署失败或可无条件执行的 PASS。其记录保存执行当时完整源码哈希；之后只为 adopt/open 复用既有 TrafficHold 检查，未改变该只读 snapshot 函数的权限语义。

## 2. 不需要重新部署健康 Gateway

09:03 现场：Gateway 0a9a93c0/ed6 运行、loopback HTTP200、已安装 Runtime --check PASS；PostgreSQL 与 Executor healthy，原容器 ID 未变，pool=false。09:04 使用已有 root `pg03.env` 解析四层 Compose 成功，Gateway 配置哈希 **a2ab7e6c…** 与运行容器标签完全一致。此前 Compose 报错是调用方未加载既有环境文件，不是必须更换 Gateway。

最小接管不调用 Gateway Compose up/start/recreate，不跑迁移，不改数据库或账务，不动 PostgreSQL、共享 Executor、业务配置及镜像。只有原 Caddy 是后续待启动对象。Windows 脚本对仅工具/元数据差异会拒绝构建 Gateway；这属于“不需要构建”，不能冒充首次镜像部署 SUCCESS。

仅 PG03 账号池暂缓商业使用；注册登录、普通 API、渠道路由、定价、充值支付、余额计费保持原样。本轮不追加 Provider、PG03 或商业财务专项验收。

## 3. 还需的生产安装条件

精确路径、SHA256、字节数和模式见 **asset-plan.json**。7项候选资产：

| 源码 | 安装目标 |
|---|---|
| local-deploy-vps.py | /opt/clearapi/bin/clearapi-local-deploy.py |
| local-deploy-state.py | /opt/clearapi/bin/local-deploy-state.py |
| runtime-identity-refresh.py | /opt/clearapi/bin/runtime-identity-refresh.py |
| runtime-boot-recovery.py | /opt/clearapi/bin/runtime-boot-recovery.py |
| pg03-failed-recovery.py | /opt/clearapi/bin/pg03-failed-recovery.py |
| backup-identity.py | /opt/clearapi/backups/bin/backup-identity.py |
| production-backup.sh | /opt/clearapi/backups/bin/clearapi-backup.sh |

均为 Linux LF、root:root 0750；安装前按现场哈希/归属记录保存旧文件及原本不存在的目标，先装共享读取模块再装其调用方。旧 RC 不包含这些新字节，不能借用旧 RC 的资产批准；本次维护授权需绑定交付中的本地完整提交与此清单。

- 原 Compose 只做上文一项归属修正，业务内容和四层配置不变。现有 `pg03.env` 为 root:root 0600，继续使用；不导出或重建密钥。
- 四个现有 boot systemd/tmpfiles 资产内容和顺序不变，不需要重启 Docker/Gateway 或新增 unit；兼容脚本就位、元数据发布后执行现有 `check-installed` 和 live `check`。
- 正式备份 timer active，但 service 目前 failed；旧停止态备份不是当前 Gateway 的运行态证明。**本轮未调用生产备份，也未宣称它已修复。** 新备份身份读取支持当前状态及五层配置；配置包会保存这9项实际输入/历史/状态文件，既有加密、明文清理、停止态/schema2、严格完成证明合同保留。restore-adopted 内只调用一次正式备份，必须真实成功并绑定当前 CID/源码/config digest/状态哈希，之后才允许开 Caddy。
- 新本地入口配置和启用哨兵仅在网站健康后写入既定两个 root:root 0600 路径。当前 clearadmin 已是管理员 sudo 通道，继续复用；额外安装受限 sudoers 片段不会撤销其既有 ALL，因此不虚称已有管理员被收紧，也不把新增账号或权限改造作为恢复条件。示例受限规则仍供独立部署身份使用，本次不扩大管理员权限。
- 已有 root CONTROL/LIFECYCLE/TRAFFIC 依赖继续使用现场已核实版本。仓库中的 V2/CONTROL 固定哈希同步仅用于保持旧模式源码及 CI 一致；它们不属于本次7项生产安装，不调用旧发布器，不要求重建/推送 GHCR 镜像。

## 4. 旧工作流可以按4项精确停用，尚未执行

最新 `github-readonly.json`：main仍 `a032815cb6244a7ee5e812de82f8300c02554e99`；两个 AUTO=false，queued/in_progress/waiting/pending/requested均0；11项仍active。

授权后依次执行下列精确操作，每项后核实状态；若出现旧生产在途运行，停止切换，不盲目取消已进入生产的任务：

```powershell
gh workflow disable 387486994 --repo a3225865526-wq/Clear-API
gh workflow disable 378101050 --repo a3225865526-wq/Clear-API
gh workflow disable 390042545 --repo a3225865526-wq/Clear-API
gh workflow disable 378227091 --repo a3225865526-wq/Clear-API
```

分别是自动发布调用者、正式部署、失败恢复、SSH/pull排练。普通 CI、分支保护、离线/只读流程保留；RC 自动开关继续false，不再承担生产镜像发布。已有四入口源代码门禁预览仍可用于后续源码归档，不将“停用所有8项”作为前提。任何后续源码 Push/PR/main 合并仍按授权及现有必需 CI 执行，本次未做。

## 5. 最短合法路径与集中授权范围

**请求批准一个不替换 Gateway 的最小维护窗口**，范围仅为：停4个旧生产入口 → 保存现场/旧资产 → 1项 Compose 归属修正 → 按精确哈希安装上述7项资产 → 当前实例元数据接管 → 现有 boot 与一次正式备份 → 原 Caddy 恢复及最小健康 → 本地入口配置/哨兵激活与只读 preflight。

在核实源码完整提交、资产清单与当前 Gateway/冻结文件均未漂移后，管理员入口的核心调用顺序为：

```text
python3 -I /opt/clearapi/bin/clearapi-local-deploy.py adopt-check --source-commit <本次批准的完整提交>
python3 -I /opt/clearapi/bin/clearapi-local-deploy.py adopt-running --source-commit <同一提交>
```

随后通过私有 stdin 将 `{"source_commit":"同一完整提交","scope":"existing-gateway-original-caddy-v1"}` 传给：

```text
python3 -I /opt/clearapi/bin/pg03-failed-recovery.py restore-adopted
```

该入口自行执行 boot 检查和一次正式加密备份，避免重复备份；然后仅更新原 Caddy restart policy 并启动原容器。验证源站 HTTP308、HTTPS证书与 Gateway JSON、Cloudflare HTTPS状态及首页/登录/overview应用壳。网页壳健康不冒充真实登录或支付交易验收。通过后再激活本地配置/哨兵并经同一 Windows SSH 通道执行正式 helper preflight；不触发 Gateway 构建或镜像更新。

**风险及失败停止：**

- 安装/归属/身份/Compose/备份/boot任一前置失败：不开 Caddy，不动健康 Gateway/PG/Executor/DB。adoption尚未发布时，可按现场归档原样恢复本次更换的资产与归属；若已发布则保留真实接管记录，不盲目换回不识别它的旧脚本。
- Caddy启动、源站TLS/转发、公网/页面或后置身份失败，以及可捕获的中断：沿用原机制停止同一 Caddy并恢复restart=no，核实隔离，追加真实失败结果。旧FAILED/target/hold、数据库和镜像不回写，不自动重试或恢复数据库。若停回也失败，明确UNKNOWN并停止，不能报网站恢复。
- 本机此前Cloudflare GET为403；若授权窗口仍403，不能当200或绕过WAF，按失败停回并报告。当前 Gateway restart=no 保持原样，本轮不声称已验证主机重启自愈，也不追加重启测试。

**本次授权申请不含：** Gateway重新部署、数据库修改/恢复、Executor操作、PG03商业开放、Cloudflare策略变更、Push或main合并。源码保存可独立走现有流程，不成为原网站必须重建镜像的理由。

## 已执行的必要验证与边界

- 新接管7项回归OK；既有adapter14、boot18、候选boot包3、Caddy探针20、恢复38项OK。最后追加TrafficHold复用与信号处理后只重跑受影响的7+4项，均OK；执行命令及真实退出结果见各JSON/log。
- 真实隔离Docker五层Compose等价、archive backup身份/config digest、boot与Runtime读取、9项归档成员验证PASS；三个既有容器、状态及历史文件字节不变，未重部署。此夹具显式替换合成incident常量，不是生产解除证明；日志明确 `production_acceptance=false`。
- 原有停止态/schema2真实GPG夹具备份与严格完成证明回归OK；没有执行生产dump或调用生产备份服务。
- Python编译、修改shell的bash/shellcheck、差异检查已执行；提交前核对固定哈希和secret scan。Go/web业务、生产Compose内容和workflow文件未改，不追加无关全量CI/数据库矩阵，不重跑30项E2E。
- 初始恢复回归因旧固定哈希未同步失败、备份夹具因新增归档路径未映射fixture root失败，均修正并定向通过。真实五层夹具初次误把9项成员断言为10而失败，修正断言后通过；初始失败日志均保留。生产只读候选因UID1000的BLOCKED仍真实保留，未伪造通过。
