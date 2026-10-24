# Windows 本地部署入口激活准备（2026-10-24 晚间，Asia/Shanghai）

**本地非生产准备已完成；VPS入口尚未激活。无需更新或重启Gateway即可激活，但还需另行授权下列三项文件写入及正式preflight。** 本轮未安装生产资产、写配置/哨兵/部署状态、启停服务或更新镜像。

## 1. 现有基线与本地配置

正确工作区 `D:/Codex/worktrees/clearapi-local-formal-integration/clearapi-controlled-launch-20261020`，分支 `codex/clearapi-local-deploy-formal-integration`。开始HEAD为网站恢复记录 `ed78f07a91240462bd617abc49e6877e99d4e599`，干净；正式main的138项后续提交完整保留，没有reset或切换其他工作区。

Windows、Linux helper、配套状态/备份/Runtime/boot工具均完整存在。已创建本机被Git忽略的 `scripts/local-deploy.config.json`：clearadmin、103.42.181.219、22、testTarget=false，引用原已授权受保护私钥。未读取私钥内容，ACL继承关闭，仅原用户、SYSTEM、Administrators可访问；没有复制凭据、新建Token或修改权限。

专用 `D:/AI/API/runtime/clearapi-local-deploy-production/known_hosts` 仅复制原可信known_hosts中的一条ed25519记录，没有ssh-keyscan/TOFU。指纹 **`SHA256:kVqtsCzwAanVxCoVB5OA4b1lvMVdF9bpxYhy8xjNuLc`** 与服务器现有公钥一致。用此配置真实完成只读SSH连接，BatchMode、IdentitiesOnly、StrictHostKeyChecking均启用，UpdateHostKeys=false。ssh-agent停止，无需启动它。

本机Docker Desktop为Linux/x86_64，Buildx可用。PowerShell7.6.5执行策略RemoteSigned；5.1.26100.9711为Restricted。本轮只读检查并保留这些策略，中文命令使用已有PowerShell7，不要求Bypass或改系统设置。

## 2. 已核实生产事实与真正缺口

21:27只读核对无漂移：原Caddy54c8550b、Gateway0a9a93c0、PostgreSQL99299db0、Executor1081a1b5的身份、运行、镜像、启动时间、restart和mounts与网站恢复后记录一致。四层Compose、现有root环境文件哈希、adopted状态cdcea866及website_restored结果d2bfd7f6一致；当前源码a032、engine ed6、OCI config digest48df基线保持。

7项已安装资产均匹配既有实际版本：本地helper仍66ecfffa，恢复helper已是网站恢复时的04b237bd，其他6项未变。既有sudoers root:root0440、SHA843e4777及visudo解析通过；clearadmin原管理员ALL仍存在，不虚称收紧权限。Runtime及boot installed/live PASS。

正式备份20261024T033125Z的manifest SHA48cf9630、密文SHAf0fbffaa及当前Gateway/元数据身份仍匹配；服务Result=success/exit0，timer active。本轮复用已有可读取性/恢复方法证据，没有新触发备份、解密或恢复演练。

21:34 GitHub GET：387486994 automatic-deploy、378101050 deploy-production、390042545 failed-production-recovery、378227091 ghcr-pull-rehearsal均disabled_manually；普通CI及其余7项仍active。两个CLEARAPI_AUTO_*_ENABLED均false；queued/in_progress/waiting/pending/requested均0；远端main仍a032815cb6244a7ee5e812de82f8300c02554e99。未Push/合并/启用工作流。最初一次错误变量名404和网络TLS超时不作为结果，后续有完整成功GET证据。

两份正式入口文件均不存在。实际调用已安装helper的preflight（禁用Python字节码缓存写入）退出2，明确因缺少 `/opt/clearapi/config/local-deploy.json` 而BLOCKED；**没有绕过门禁，也没有正式preflight PASS。**

还发现两个直接影响接管的兼容问题，已在本地最小修复，源码提交 **`14d7c09582e99c3144a76fcbeb06f509f3225489`**：

- 已安装helper仍用默认urllib检查公网，真实收到403，Ray `a4f952650e378a4c-HKG`。只改本地候选probe为原生curl默认UA，加 `X-ClearAPI-Probe: local-deploy-v1`；HTTP200及JSON success、TLS、8秒/64KiB界限保留，不伪装浏览器、不关防护。候选仅在VPS内存中执行这一个状态函数，本机/公网均真实200、success=true；未安装文件，未调用config/preflight/receive/apply或docker save，也未写生产状态。
- Windows累计diff会将两个未打入Gateway运行镜像的恢复工具当成PG03业务变化，导致未来正常应用更新也被阻断。仅排除精确的 `scripts/pg03-failed-recovery.py`、`scripts/pg03-release-control.py`；SQL、迁移、真实PG03及bootstrap阻断保持。未引入新模式或发布平台。

PowerShell5.1/7.6的AST及七项分类合同、Linux两项状态探针定向测试和Python编译通过。历史30项真实隔离验收未重复，不能把历史证据无条件宣称为新代码的完整生产验收。

## 3. 待批准的最小VPS激活范围

精确字节、SHA和前置在 **activation-plan.json**，源码绑定14d7c09582：

| 目标 | 操作 | root模式 | 新SHA256 |
| --- | --- | --- | --- |
| /opt/clearapi/bin/clearapi-local-deploy.py | 保全现有66ecfffa后原子更新一个helper | 0750 | b9c11350b7d8d3aef15165bd7a5ea2a5a1ab0f731c39d1f8983bad672e45417f |
| /opt/clearapi/config/local-deploy.json | 仅当仍不存在时创建精确正式配置 | 0600 | 22833e916fc368b208959d55442f006e5b551e81dc5261be40a92803bb188286 |
| /opt/clearapi/config/local-deploy.enabled | 最后发布既有合同的启用哨兵 | 0600 | 00d09becb7e5b07482cf917cf361befd457a12e664d193645454e0e6a4904f16 |

全部root:root，不变更其他资产、sudoers、元数据、Runtime身份、网站配置或业务数据。配置严格匹配现有helper的固定values合同；两份本地候选不是生产激活凭证。

经单独授权后：先再次核实上述基线、4项生产入口停用及无在途，保全旧helper和必要元数据；更新该helper，创建正式配置，哨兵最后发布；仅执行 `sudo -n /opt/clearapi/bin/clearapi-local-deploy.py preflight` 并核对原前驱身份/健康。现有preflight为核对OCI config digest会临时docker save并自动清理，这项临时文件写入与必要root现场保全也须包含在批准范围；不会docker load、构建、Compose up、接收归档或更新容器。

任一身份/文件SHA/配置或preflight失败立即停止；移除**本次新建的**启用哨兵使入口不可用，按保全副本恢复本次helper及撤销新配置，不碰Caddy/Gateway/PG/Executor或原FAILED/target/hold。没有发生镜像更新，不需要数据库或业务回滚。本轮仅提交该具体范围，**尚未获得也未执行这些VPS写入**。

## 4. 今后每次更新命令

入口正式激活后，在正确工作区保存真实应用变更为本地Git提交，完成适用检查、保持工作区干净，再在PowerShell7执行：

```powershell
Set-Location 'D:\Codex\worktrees\clearapi-local-formal-integration\clearapi-controlled-launch-20261020'
.\scripts\deploy-local.ps1 -DryRun
if ($LASTEXITCODE -ne 0) { throw '预检未通过，停止部署' }
# 审阅版本和scope，并已授权实际更新后：
.\scripts\deploy-local.ps1
```

若从PowerShell5.1启动，使用中文README给出的现有pwsh.exe绝对路径运行同一脚本，无需改执行策略。无需Push、触发旧GitHub发布、手工打镜像或抄SSH命令。`-DryRun`不构建镜像或更新容器，仍需已激活helper才能完成正式预检。

当前源代码相对线上a032只有工具/记录差异，因此正常分类会拒绝无意义构建：`Only metadata/tooling changed; no Gateway build needed`。这不是要补一次Gateway发布，不应创建空应用提交。启用完成由正式helper preflight证明；下一次真正的web/Go等应用变更才进入镜像构建、SSH直传和原单服务部署。迁移/PG03敏感变更仍须单独审阅，后端仍先正式备份且未知数据库状态不自动回退。

本机不缺凭据或软件安装；部署激活唯一待用户决定的是上述VPS最小写入授权，不需要用户手工重启服务或提供私钥/Token。

## 5. 裸域独立计划（不执行）

两台权威drew/khloe仍对裸域A/AAAA返回aa/NOERROR/NODATA，SOA serial2416861397、负缓存1800秒；www仍为正常Cloudflare地址。对实际Cloudflare边缘做SNI=clear999.com的只读TLS握手，链和主机名验证PASS；证书SAN包含clear999.com及*.clear999.com，有效至2027-01-05。该TLS证明不等于DNS或重定向已经生效。

具体候选：

- DNS增加 `A @ → 103.42.181.219`，Proxied=true、TTL=Auto（API ttl=1）；不动www、NS或原有其他记录，不增加未经验证的源站AAAA。
- Single Redirect，phase=http_request_dynamic_redirect，匹配 `(http.host eq "clear999.com")`，目标表达式 `concat("https://www.clear999.com", http.request.uri.path)`，308、preserve_query_string=true。例如 `http://clear999.com/login?a=1` → `https://www.clear999.com/login?a=1`，不匹配www，避免循环。308保留请求方法，设置见[Cloudflare官方说明](https://developers.cloudflare.com/rules/url-forwarding/single-redirects/settings/)。
- 执行时先读现有zone、DNS、边缘SSL状态及已有重定向规则，保留现有规则集；先增加精确边缘跳转，再发布代理A记录。之后核对HTTP/HTTPS及路径/查询参数，www不受影响，考虑旧负缓存最长1800秒。原Caddy仍只服务www，无需为该边缘跳转修改生产Caddy或源站证书。

已有Edge会话没有Cloudflare标签页；只读打开Dashboard的导航未完成，随后页面仍about:blank，未获取面板记录、zone ID和规则ID，**不能判断登录态或宣称已核实规则冲突**。不读取cookies/Token或索要凭据；未来执行前需在可用管理访问下补齐，且DNS/跳转另行授权。精确JSON在apex-dns-redirect-plan.json，未发API写请求，未改WAF/BIC。

## 6. 本轮边界与证据

网站服务、镜像、业务配置/数据未更改，PG03继续暂缓商业；本地配置及专用public host pin是唯一机器配置写入。生产只有只读SSH、Docker/Compose观察、元数据/备份校验、内存候选状态函数、正常网络诊断；没有激活入口、修改生产配置/状态或复制新root文件。

同目录production-readonly.json、candidate-status-readonly.json、github-readonly.json、windows-local-config.json、targeted-validation.json，以及相关明确候选/计划文件保存本轮事实。当前本地配置被Git忽略；私钥、密码、Token及完整机器私钥路径未写入提交。生产恢复主线历史原样保留在总控下方。
