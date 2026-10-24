## 2026-10-24 11:12–11:17 Asia/Shanghai — 公网只读排障：探针签名1010、裸域NODATA、Caddy停止为独立问题

- 基于正式集成分支22f85af11f68a9de62807332f62953dd0b2385b9及干净工作区，仅采集网络/Docker只读状态；未改Cloudflare/DNS/NS/服务/root资产/业务数据，未激活入口或重跑30项E2E。
- **1010范围已收窄：** UTC03:14:12同一VPS原生curl/8.5.0为521（Ray a4f5cf5a5f85df9d-HKG），Python-urllib/3.12为403/1010（a4f5cf5aeb77107b-HKG）；Windows原生curl/8.21.0为521（a4f5cf621b61a425-LAX）。VPS curl仅改显式UA为该Python值也复现1010（03:16:46，a4f5d31faa871483-HKG）。指向探针客户端签名拒绝，不能写成整个VPS IP或所有普通用户被封禁。官方1010/BIC语义吻合，但具体安全功能/规则未由Security Events确认。
- **裸域确实缺少可用地址：** .com父区委派至drew/khloe.ns.cloudflare.com；两台权威均aa/NOERROR，对clear999.com的A/AAAA/CNAME均NODATA、SOA serial2416861397、负缓存TTL1800。VPS默认及1.1.1.1/8.8.8.8/9.9.9.9一致，Windows默认最终也返回无A；不是仅VPS解析器故障或刷新缓存可解决。www权威A/AAAA正常指向Cloudflare、TTL300，公开CNAME NODATA不能排除面板有展平CNAME。Windows公共DNS/DoH通路超时/重置与域名缺记录分开记录。
- **源站仍未恢复：** 只读确认原Caddy stopped/restart=no，Gateway原CID/镜像/启动时间保持且loopback200，PG/Executor身份/启动时间未变。原生curl的521与Caddy关闭一致，不证明源站TLS/转发健康；面板内源站目标仍待核实。正式恢复public_health默认urllib UA是已定位的兼容点，不能原样再次启动Caddy后假定探针会过。
- **明确未验证项：** 浏览器工具无法可靠识别当前URL而安全停止，已停止UI操作；未获得普通浏览器网站结果、Cloudflare登录态、Security Events/BIC开关或DNS面板内容，不猜测具体规则或全用户影响。未读取cookies/Token、未索要凭据。
- **最短后续路径：** 先只读补齐Security Events/面板源站/TLS及普通浏览器；最小修正正式探针客户端签名且保留原健康/失败停回合同，经精确差异/SHA和单独维护授权后仅恢复原Caddy。裸域另需有效代理DNS与规范跳转/证书条件；原源站只配置www，不能只补@记录就声称裸域HTTPS恢复。不需要替换健康Gateway，不关闭全站防护。上述写入均未授权执行，本地正式入口仍未激活，PG03暂缓商业及其他业务保持原样。
- 证据及最小操作建议：evidence/public-access-diagnostic-20261024/REPORT.md和同目录6项JSON。下方生产维护、备份、Runtime/boot及历史恢复主线记录原样保留。

---

## 2026-10-24 Asia/Shanghai — 最小生产维护已执行至公网失败停止；Caddy停回，入口未激活

- **最终结果：网站未恢复，正式入口未激活，不宣称整套接管成功。** 用户明确批准的源码为1aa591f4bb230c4fce5770114e45e9311beb87a4；执行前核实正确独立工作区/分支、精确HEAD及干净状态，7项源blob/SHA和实际现场无漂移。以下为本轮真实执行结果，下方准备/历史记录保留。
- **旧发布链：** 10:42精确停用387486994 automatic-deploy、378101050 deploy-production、390042545 failed-production-recovery、378227091 ghcr-pull-rehearsal，均disabled_manually；其他7项仍active，CI/分支保护/两AUTO=false均未改。最终五类在途计数0，远端main仍a032，未Push/合并。
- **Root与元数据：** 现场及旧资产保存在/opt/clearapi/backups/maintenance-20261024-1aa591f4bb。只将指定docker-compose.prod.yml由1000:1000改为root:root，0644和c5fc8ca1内容SHA不变，无递归修改；7个批准资产逐项root:root0750/SHA匹配，受限sudoers root:root0440及全局visudo通过（原管理员ALL仍保留，不虚称被收紧）。adopt-check PASS后adopt-running完成，新增状态adopted，state SHA cdcea866、local overlay aee0bbc3；原FAILED/target/hold和四层内容原字节保留，未改成success。
- **备份/Runtime/boot PASS：** 一次正式备份20261024T025143Z，10:51:43启动、10:52:07正常完成，Result=success/exit0；密文12377896bytes，SHA7e77bb3aba44e22bc3be0be2b272f454ce122811e2676cac44bec3cfa732f54a。绑定当前0a9a93c0/a032/config48df及state SHA；staging清理。新密文认证解密及9项归档SHA全部匹配，本机隔离无网络PG17客户端pg_restore -l PASS；无私钥导出、无本地明文备份文件、无生产DB恢复，不冒称新备份完整DB恢复演练。原恢复密钥及既有真实PG17恢复证据可用，关键两份旧备份另保全。接管后/停止后Runtime及boot installed/live均PASS，未改4份boot系统文件、未重启Docker/主机。
- **Caddy：** 原54c8550b/f2a于10:52:19成功启动，日志server running；规定公网检查未通过，于10:52:49由冻结恢复入口安全停回，restart=no、exit0、State.Error空，containment_verified=true。失败凭证website-restoration-failure-39347012653.json SHA42eca429保留，无成功result，未清attempt/隔离或重试。源站HTTPS/转发/HTTP308/全页面后置验收未完整走完，不能报PASS。
- **剩余阻塞：** 维护客户端持续收到www.clear999.com HTTP403 / Cloudflare error1010；VPS解析裸域clear999.com未获得地址。未擅改Cloudflare防护/DNS、未绕过健康合同。到达停止条件后只有只读检查及本地加密备份可读取性验证，未再写生产或激活入口。/opt/clearapi/config/local-deploy.json与local-deploy.enabled均不存在；7资产/元数据完成不等于入口激活，恢复需后续明确审阅和授权，不能删除真实失败记录来重试。
- **业务及容器：** Gateway仍0a9a93c0/ed6（OCI源码a032、config digest48df），StartedAt仍2026-10-23T14:07:58.383082222Z，HTTP200；PG99299d、Executor1081a1的CID/镜像/StartedAt/restart/mounts一致且healthy。未重部署/重启Gateway、未启停PG/Executor、未执行数据库或渠道/余额/计费等业务写入，仅正式备份读取。PG03=false维持，其他业务配置原样。
- 执行事实、逐阶段日志和完整SHA：evidence/production-maintenance-20261024/{REPORT.md,RESULT.json,final-production-state.json,fresh-backup-readability.json}。未重跑30项E2E或无关CI；提交仅保存本轮状态/证据，冻结源代码及安装资产不改，后续Git HEAD仅为本地记录提交。

---

## 2026-10-24 09:34 Asia/Shanghai — 最小接管兼容已本地完成；生产仍隔离，待一次维护授权

- **网站主线：未启动Caddy、未解除隔离。** 当前0a9a93c0/a032/ed6 Gateway、持久Runtime、本机HTTP200正常，PG/共享Executor healthy。只读实核既有root pg03.env可解析四层Compose，其a2ab7e6c配置哈希与运行标签完全一致；不需要重新部署Gateway、换镜像或改业务配置。
- 旧恢复合同具体不适配：固定f8f963a7/c50/22ec、/run/clearapi挂载、旧GitHub恢复receipt。已复用原TrafficHold/Runtime/boot/备份/Caddy恢复函数补兼容入口，另存local-deploy-state.json的真实adopted状态和compose-local.yaml终端层；不制造旧receipt、不改原FAILED/target/hold，不用测试证明接管生产。原四层内容保留，后续新本地版本只写本地状态/第五层。
- **真实前置尚未执行：** 候选只读snapshot在原docker-compose.prod.yml权限处BLOCKED：UID/GID1000、0644，SHA c5fc8ca1…未变。经授权后仅需root:root归属修正，不能放宽root读取校验。7项精确资产安装/旧资产保全及当前真实正式备份、boot/网站健康均待批准；生产备份service仍failed、timer active，本轮没有调用生产备份或伪称其修复。
- 新restore-adopted在root明确范围下先验原隔离、当前身份、boot和一次正式备份，再仅开启原Caddy；失败/可捕获中断沿用stop+restart=no，真实记录UNKNOWN/失败，保留健康Gateway及历史。既有Caddy探针读取成功恢复结果为有效状态，历史isolated原文件不覆盖。4份boot系统资产和服务顺序不变，未重启Docker/主机，也未追加重启验收。
- 本地兼容验证：接管7项OK，adapter14、boot18、候选包3、恢复38、探针20等受影响回归OK；最后仅重跑新增接管7和相关恢复/固定哈希4项均OK。真实隔离Docker五层配置等价、备份身份/config digest、boot/Runtime及9项配置归档成员PASS，3个既有测试容器/文件不变；非生产验收，未重跑30项E2E。初次pin/fixture路径/9误写10的失败均保留并定向修正。Python编译、bash/shellcheck、diff及提交前secret scan检查；无Go/web/依赖/生产Compose或workflow内容改动。
- 旧生产入口仍4项，最新GitHub只读main=a032、两AUTO=false、五类在途0、11项active。本轮未停用、未Push/PR/main合并；普通CI与分支保护保留。现有管理员clearadmin权限不变，不把新增sudoers片段描述成撤销其ALL；不新建账号或发布平台。
- **一次明确授权申请（未生效）：** 停4旧生产入口→保全现场/旧资产→1项归属修正→按asset-plan.json安装7项精确资产→只做当前实例元数据接管→现有boot检查及一次正式加密备份→原Caddy HTTP/HTTPS/转发/页面检查→本地配置/哨兵激活与只读preflight。不含Gateway重部署、DB修改、Executor操作、PG03商业开放、Cloudflare策略、Push或main合并。公网403或其他异常按原停回合同停止，不假装恢复。
- 仅PG03账号池商业使用暂缓，其余功能/配置/商业逻辑原样。报告、具体命令、源哈希与风险：evidence/minimal-production-handoff-20261024/REPORT.md、asset-plan.json。代码提交SHA由Git HEAD及交付消息给出；下方其他主线完整保留。

---

## 2026-10-24 08:54 Asia/Shanghai — 网站优先恢复只读核实；独立 main 集成（非生产切换）

- **网站恢复结果：未恢复、未启动 Caddy。** 用户只批准满足现有安全条件后的原 Caddy 最小恢复，未批准整套生产切换。现场原容器54c8550b、Caddyfile SHA6b124666保持原样，由现有 edge Compose 管理；代理127.0.0.1:3000，Gateway0a9a93c0/ed6运行、HTTP200，证书www.clear999.com有效至2027-01-04。Caddy exited/restart=no，80/443无监听，源站HTTPS000；08:55本机公网首页及api/status均403/CF-Ray，不能当源站恢复。未执行启动后的页面或TLS握手验收。
- **具体停止点：** frozen hold SHA57a7152a仍stage=isolated，target SHA521e60ce仍public_traffic=blocked，release SHA6c308146仍旧c50/22ec FAILED。正式 WebsiteRestoration 绑定旧失败 Gateway，必须存在 failed-recovery-39347012653.json 等门禁；现场该凭证及pg03-failed-recovery.py安装入口缺失，当前Gateway已为a032/ed6。不能套用旧恢复证明或直接docker start绕过隔离。未改hold/FAILED、未安装资产、未变更任何生产容器或业务配置。该阻断独立于本地部署接管。
- **本地部署切换进度：** 创建独立工作区 D:/Codex/worktrees/clearapi-local-formal-integration/clearapi-controlled-launch-20261020，分支codex/clearapi-local-deploy-formal-integration，以正式main a032815cb6244a7ee5e812de82f8300c02554e99合入原验收分支8a8b36ab。main的138个后续提交全部保留，未覆盖其他工作区。仅两处文本冲突：总状态保留main完整历史并加本地主线；backup-identity合并归档支持与main停止态/schema2/严格successor完成证明。业务源码和workflow文件相对main完全不变。
- Runtime语义集成：恢复旧GHCR image_digest输出；归档继续docker_image_id，增加tag/revision绑定；boot允许有明确success/method/archive SHA的本地归档，实核config digest与Docker engine ID分离；候选helper/boot/安装器/清单固定哈希同步，仅为本地新资产，不继承旧生产安装授权。
- 定向结果：boot18、安装器14、候选包3、备份16（含真实GPG停止态备份与严格完成证明）、停链预览2均OK；原RuntimeIdentityTest21个用例完成，但该组合命令误选不存在的InstallerTest导致exit1，后续仅正确InstallerFaultTest14项OK，未隐藏初次错误。真实隔离Docker新boot/config digest/Runtime只读检查PASS，既有manifest及identity原字节不变；未重跑30项矩阵，不改绑旧验收证据。
- 工作流范围修正：08:49复核main仍a032、两个AUTO=false、五类在途计数均0。仅automatic-deploy、deploy-production、failed-production-recovery(recover/restore)、ghcr-pull-rehearsal(SSH/pull)列为4个生产可达入口；保留普通CI、RC、离线排练/审计。上轮8项整套停用方案未获批准、现已被4项计划取代。prepare-local-cutover仅preview，未apply，远端11项仍active。
- 首次生产接管尚未就绪：需按当前健康Gateway解决原隔离恢复合同、四层Compose/受控环境与普通helper两层配置交接、当前正式备份/manifest/boot基线，并在未来授权Push后满足必需CI。此处不重写FAILED为success、不删除历史证据，不申请或执行整套生产切换。
- 仅PG03账号池暂缓商业；注册登录、普通API、模型渠道路由、共享Executor、模型定价、充值支付、余额计费全部保持原样。证据及正式集成报告：evidence/caddy-and-formal-integration-20261024/REPORT.md；下方历史完整保留。

---

## 2026-10-24 05:04–05:12 Asia/Shanghai — 首次本地切换只读准备；现场已变化，生产执行仍 BLOCKED

- 本轮仅 GitHub GET/git fetch、merge-tree预览和固定SSH只读检查；没有停用远端workflow、Push、生产安装/配置修改/备份触发/服务启停/开放公网，未重跑30场景。验收提交49f23003a054e7b156e9f7c6a25589aa56500adc继续保留。
- 正式main=a032815cb6244a7ee5e812de82f8300c02554e99；验收分支独有4提交、落后138提交。三方预览仅master与backup-identity冲突；必须正常整合，保留main完整恢复/停机备份成果和本地两项合法shop修复，不覆盖其他worktree/PR。Runtime/boot存在无文本冲突的pin/引用语义差异，禁止直接复制旧分支资产。
- GitHub自动RC/生产变量仍false；所有旧workflow仍active；queued/in_progress/waiting/pending/requested均0。已列出先切断4个生产可达入口、再退役RC/排练/授权来源的8项顺序。现有4文件停用准备器覆盖不足，不代表已安全退役；CI/分支保护保留。
- **本次现场取代旧退出127记录：** Gateway0a9a93c02646/ed6f93a9已运行main a032约7小时，loopback3000=200、pool=false、restart=no；durable runtime已挂载，静态identity --check PASS。原Caddy54c8550b仍stopped/restart=no，公网API521。原Executor1081a1与PG99299d/17.6健康，不启停、不开放PG03商业功能。
- manifest仍旧c50/22ec FAILED、hold isolated，当前Gateway由四层Compose含website-recovery-39957948359 overlay运行；39957948359是RC成功编号，不是生产恢复成功证明。普通local helper两层Compose/成功manifest/公网健康前置均不适用当前隔离交接，换来源不会自动恢复网站；禁止伪造success、删hold或TestTarget用于生产。
- 备份timer active，service failed；最近20261023T102102Z为旧c50停止态schema2，密文12330812bytes/SHA995100eb…匹配，但不是当前a032运行态备份。root备份入口实际clearapi-backup.sh（main7edb9ce9）；不得装回旧602db567或丢弃main停机/严格证明/append-only备份能力。
- 现有boot资产/加载顺序check-installed PASS，live check BLOCKED；新helper未安装、本地启用配置/哨兵不存在。必须先闭环最新main整合、合法首次接管/有效Compose及受控环境、当前真实备份/Runtime/boot身份；本轮不修改实现、不新增发布系统、不重测已验收矩阵。
- 唯一顺序与一次集中授权申请（含具体范围、前置停止点、失败/安全回退）已写入 evidence/local-cutover-preparation-20261024/CUTOVER_PLAN.md；当前申请未生效，不能声称生产切换已可直接执行。下方所有历史原文保留，不覆盖生产恢复主线。

---

## 2026-10-24 Asia/Shanghai — Windows 本地一键部署隔离验收 PASS（非生产状态更新）

- 本节仅记录 `codex/clearapi-local-deploy-integration` 的本地交付；下方生产恢复主线原文保留，不能据此认定生产已恢复或切换。本轮未连接生产、未使用生产密钥、未推送 GitHub。
- 基线 `d9cd1299f6cb0442ba9d43af1a1ac368c94816c5` 精确匹配正式独立 worktree；初始只有未跟踪 __pycache__，现已忽略。最终本地提交 SHA 以 Git HEAD 和交付消息记录为准。
- 30 个真实验收场景 PASS：完整 Windows 入口、前端失败回退、真实 TimeoutError、安全并发锁、实际 pg_dump 失败前置阻断、归档 SHA/大小/image ID/revision 错配、测试模式生产隔离（含路径穿越/符号链接/userinfo/测试证明与身份/真实 sudo）、后端 UNKNOWN/pending 禁止自动回退、PowerShell 5.1 实际传输。完整/定向重跑的退出结果均如实保留，不把早期失败写成通过。
- 成功测试应用源码 d9cd1299；镜像 config ID `sha256:a34b84f437efbacd174b20d0b54dd1b19a4dc3c5d63f40b75a7de79798731545`，归档 `76de8c04d0b4f76434b9aa0421352f29d7376e28f2a2995867db456a0595596c`，隔离备份 `20261023T180450Z`；Runtime check 与归档模式正式备份身份读取 PASS。最终工具字节哈希及逐例前后快照见 `evidence/local-deploy-final-20261024/results.json`，未复用不匹配的旧 PASS。
- 直接修复：LF Linux 资产、正式/测试配置分离、严格隔离目录和 URL、加载后 revision/架构、保留固定 GHCR 前驱兼容、Compose 启动 HTTP 就绪等待、早期拒绝保留 SSH 错误、现有备份身份适配归档；既有后端不安全回退禁令保留。业务应用和 PG03 开发成果未改。
- 定向回归 16+12 PASS；PS 5.1/7.6 AST、Python 编译、sudoers 解析、diff check PASS。切换准备器仅预览，旧 Actions 未停用。中文说明和首次切换清单已按 SSH 直传更新。
- 具备申请首次切换的本地工具技术条件；生产现场恢复/健康、正式备份与恢复前提、远端旧链停用和精确 VPS 安装/首次部署仍需单独授权与现场核实。不得使用隔离证明替代生产验收，不自动开放流量。
- 交付报告：`evidence/local-deploy-final-20261024/REPORT.md`；旧主线如下完整保留。

---

# ClearAPI — 权威总控（MASTER STATUS）

## 2026-10-23 — OFF RC 与 Deployment Rehearsal 完整通过；停在生产审批边界

- PR #87 已合并，merge commit `60ea62441e4a628517e8d3e3c8762d30d0a1556b`，tree `6127ddc5e2a3d2c3f20bbf2c7ff27470f410a36c`；其 8 项保护 CI 全部 PASS（run `39806922042`）。
- 最新正式 OFF RC `39808120659/1` 成功，基于上述最新 main；artifact `clearapi-off-rc-39808120659-1`，source/tree、run/attempt、manifest/schema receipt、镜像和二进制身份均经独立 validator PASS。镜像 digest `sha256:0ae055c5ab47a20b27a850491b2de0b7931c73393b3ccbaef891c9b062f561fa`；二进制 SHA256 `37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5`。
- OFF isolated schema evidence：PostgreSQL 17.6 disposable instance 两次启动成功；首轮 schema `578bc6f83dfafb47056c74324d13833a6333152685f4500316b0cd67f88333fd` -> `3c1c77f355a2f9376f7cd4133267c99856cd4905b6116ad2ba615dbf84305035`，第二轮保持目标值，cleanup 完整；production schema 明确 `unverified`。
- OFF Deployment Rehearsal `39810841937/1` 成功，严格绑定 RC `39808120659/1`。最终 proof SHA256 `d17a8bd...`（本地下载文件实际 SHA256 `d17a8bdfaf873d7a947f2ae2739b8741a39897ce8e23de854d195735845f26c2`），proof artifact `deployment-proof-39810841937-1`，11/11 OFF gates PASS：runtime_integrity、billing、backup_restore、sqlite/mysql/postgres upgrade idempotency、ordinary_provider_usage_billing、pool_disabled、replay_and_duplicate_charge、state_isolation、safe_rollback。PG03-only `executor_pairing`、`gateway_executor_provider_usage_billing` 与 PG03 operator backup/rollback gates 按 OFF 模式不执行，未被伪装为 PASS。
- `CLEARAPI_AUTO_PRODUCTION_ENABLED=false`；对应 automatic-deploy workflow runs skipped。没有 SSH、生产数据库连接、真实生产备份/恢复、部署、Gateway 替换/重启或公网流量切换。PG03、账号池和 Executor 保持关闭。
- authorizer 修复：本 PR 按 proof mode 分路验证 OFF `release-candidate.yml`、`clearapi-off-rc-{run_id}-{attempt}` 与 OFF manifest/schema/runtime artifact hashes；PG03 继续走原 `release-pg03-gateway.yml` 合同和校验。真实 OFF proof 在本地 validator 独立通过；合并后仍须为最新 main 重建 RC 并重跑 rehearsal。

## 2026-10-22 — OFF RC 成功；Rehearsal dispatch 被 choice 定义阻塞

- PR #85 已合并，merge commit `98a3a1d32ddd078bb0b446ea3b47dab39923c91e`；其 8 项必需 CI 全部 PASS（Actions run `39794014652`），含 SQLite/MySQL/PostgreSQL matrix、backend、frontend、secret scan 与 PG03 isolated restore。
- 新正式 OFF RC run `39797527304/1` 基于上述 main commit/tree `accd82ccf715e4b33574cce499288bf8ce36e07f` 成功；artifact `clearapi-off-rc-39797527304-1`。镜像 `ghcr.io/a3225865526-wq/clearapi@sha256:740d50a5062565d300635dd25bd9bb5223a2bff6a037dea981a06d50a134f759`，二进制 SHA256 `37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5`。本地 validator 对仓库/source/tree/run/attempt/artifact 身份 PASS，artifact hash 与二进制哈希匹配。
- OFF 隔离 schema receipt 与诊断 observation 均真实存在：PostgreSQL 17.6 隔离实例两次启动成功；第一次从 schema SHA256 `578bc6f83dfafb47056c74324d13833a6333152685f4500316b0cd67f88333fd` 迁移到 `3c1c77f355a2f9376f7cd4133267c99856cd4905b6116ad2ba615dbf84305035`，第二次保持一致；绑定迁移 proof 成功。生产 schema 明确仍为 `unverified`。
- 已确认仓库变量 `CLEARAPI_AUTO_PRODUCTION_ENABLED=false`。OFF Rehearsal dispatch `39797527304` 被 GitHub 以 HTTP 422 拒绝：`pool_mode=off` 不在允许选项内。实际 workflow choice 写作未加引号的 `off`，按 YAML 解析为布尔值；未尝试 PG03 替代值，也未启动 Rehearsal。已修复为字符串选项并新增 regression test；WSL producer 11/11、automatic-release 45/45 通过，workflow YAML parse 与 `git diff --check` 通过，待 PR/CI。修复合并后需为最新 main 再建 RC，因为 Rehearsal 严格要求 RC commit 等于当前 main。
- OFF Deployment Rehearsal `39804761415/1` 已运行并严格绑定 RC `39802315993/1`。10 个 receipts 通过：runtime_integrity、billing、SQLite/MySQL/PostgreSQL upgrade idempotency、ordinary_provider_usage_billing、pool_disabled、replay_and_duplicate_charge、state_isolation、safe_rollback。PG03-only backup/restore evidence 与 Executor pairing 按模式跳过。唯一失败为 OFF `backup_restore` 回归：28 项中 `test_stopped_incident_encrypted_backup_preserves_failed_identity_and_history` 因绝对路径祖先 `/`、`/home` 等 root-owned 系统目录被误拒而失败。Proof assembler 正确以 `gate failed: backup_restore` 拒绝最终 proof。artifact `deployment-proof-39804761415-1` 保留全部 receipts；不得将其视为 rehearsal 成功。
- 已修复祖先目录检查：允许 root-owned 或当前用户拥有的目录，仍要求非组/他人可写（sticky root-owned 临时目录除外）；目标备份目录自身、manifest、staging/encrypted 目录和 artifact 的严格 owner/mode/link/hash 校验保持不变。WSL backup identity 28/28、producer 11/11 PASS。待 PR/CI；合并会改变 main，必须再次构建最新正式 RC 并重新演练。
- 未执行 SSH、生产数据库操作、真实生产备份/恢复、部署、Gateway 替换/重启或流量切换。OFF 模式的 backup gate 仅是离线合成 fixture 回归，不是生产备份/恢复证据。生产现场操作与最终授权仍是阻塞；PG03、账号池、Executor 保持关闭。

## 2026-10-22 — PR #84 已合并；OFF RC 首轮 schema 迁移合同阻塞

- PR #84（`faf1a34e78c8aef5dba55ac040e062abf8ea242e`）已合并，merge commit `45c819d15855d4c7cbfce3c56d8739b2dd920052`。8 个必需 CI checks 全部 PASS。
- 基于该 main commit 触发正式 RC run `39754699152`。前端构建、竞态测试、SQLite/MySQL/PostgreSQL matrix 均 PASS；Docker job 已构建并推送镜像 digest `sha256:4db763f26ab72b4f80732c34a48f4c57603084e59f9ef9ce9b5a4ee319c40a2d`，但随后 OFF isolated schema rehearsal 阻塞，无 RC artifact / schema receipt 发布。该 digest 仅为失败 run 的诊断身份，不得作为候选或 rehearsal 输入。
- 已确认代码缺陷：共享 helper 原本拒绝 OFF 首轮 schema 变化。修复明确限定 `pool_mode=off`，PG03 的 schema conflict 门禁保持不变；仍要求候选完成 proof 和两次启动收敛。失败 run 只有通用 BLOCKED 日志，尚不能确认其实际失败点；新 RC 将始终保留脱敏 observation 供定位。当前本地 schema helper 84/84、producer 10/10、validator 45/45 PASS，Python compile、YAML parse、`git diff --check` PASS。未用本地回归替代生产证据。
- 当前最短路径：保护 PR/CI 后合并；以新的 main 构建 RC，核对真实 schema observation、artifact、image/binary 身份；再按本任务已授权范围执行 OFF Deployment Rehearsal。触发前必须核对自动生产发布开关关闭；生产操作继续单独审批。
- 真实生产备份、恢复、回退、现场验收、SSH/生产数据库操作、部署与公网流量切换仍未执行且未授权。PG03、账号池和 Executor 继续关闭。旧 RC `39698686281` 及失败 RC `39754699152` 均不可复用。

## 2026-10-22 — OFF 发布链合同修复完成；旧 RC 不可复用

- 修复 `deployment-rehearsal.yml` 的模式分支：OFF 仅接受 `release-candidate.yml`、artifact `clearapi-off-rc-{run_id}-{attempt}` 与 `clearapi.release-candidate/v1`；PG03 继续使用原 `release-pg03-gateway.yml`、artifact 和 receipt 合同，未降低 PG03 门禁。
- OFF RC 现在必须携带 exact-candidate 两次启动的 PostgreSQL schema rehearsal：source/target schema SHA256、source commit、image digest、Gateway binary SHA256、run/attempt、candidate image ID、`production_schema=unverified` 和完整 artifact hash inventory；缺失、错配或 UNKNOWN 不会生成 proof。
- OFF proof 使用 `off_isolated_startup_migration`，明确是非生产隔离证据；不声明生产 schema 已验证、不依赖 Executor、不启用 PG03、账号池或 Executor。deployment rehearsal 对 manifest、runtime bundle、schema receipt、binary 和 digest 做独立绑定校验。
- 定向验证：WSL `test-rehearsal-producer.py` 10/10 PASS；WSL `test-automatic-release.py` 45/45 PASS；Python compile、workflow YAML parse、`git diff --check` PASS。Windows 端 Bash heredoc 测试不作为结果，Linux/WSL 结果为准。
- 旧正式 RC `39698686281/1`（commit `f5751ffba116048551312d6ffbb3473fd83e63ad`、digest `sha256:5f7ddd26258a2fa411e55cd5e13ccf36e43ec4b20506cc44dcb001ce97903bf7`）仍是历史身份，但不含本修复后的 OFF schema rehearsal，禁止沿用其 digest 或 artifact 作为新 rehearsal 身份。必须在修复合并后的最新 main 上重新构建 RC。
- 当前仍未创建 PR、未触发 CI/RC/rehearsal、未连接生产；真实生产备份、恢复、回退、现场验收和部署审批继续 BLOCKED/NOT AUTHORIZED。PG03、账号池和 Executor 保持关闭。

## 2026-10-22 — 非 PG03 正式 RC 已构建；生产仍未授权

本节更新并取代下方“正式 commit/CI/RC/registry 尚未运行”的旧状态。该旧记录保留为历史，不再代表当前状态。

- PR #83 已合并至 main：merge commit f5751ffba116048551312d6ffbb3473fd83e63ad，源码 tree 39d06ca31f4b19eb30c1555221d2c1318b24b12e。PR required checks 与合并后 main CI run 39697652261 全部 PASS（后端、前端、SQLite/MySQL/PostgreSQL、Secret scan、隔离 PG03 restore）。
- 正式 Release Candidate workflow run 39698686281 成功；GHCR digest 为 sha256:5f7ddd26258a2fa411e55cd5e13ccf36e43ec4b20506cc44dcb001ce97903bf7。RC 中 pool_mode=off、production_ready=false。
- RC job 对 GHCR digest 执行 pull 后提取 /new-api，验证镜像 revision 为精确源码 commit，并生成 runtime bundle。下载 artifact clearapi-off-rc-39698686281-1 后，本地逐项复算 7 个内容文件哈希全部匹配；Gateway binary SHA256 37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5 与 RC manifest/artifact 一致。Runtime inventory 绑定相同 commit/tree、RC run/attempt、manifest SHA、镜像 digest、helper 和六项资产哈希。详见 evidence/non-pg03-20261021/RESULT.md 与 evidence/non-pg03-20261021/formal-rc-39698686281/。
- 自动 RC 与自动生产发布仓库开关均为 false；仅触发 Release Candidate 构建/镜像推送。PG03 RC、deployment rehearsal、automatic production release 的关联 workflow-run 均 skipped；没有生产 SSH、配置/服务/数据库访问或部署。隔离 PG03 restore CI 是 disposable runner 验证，不代表启用 PG03。
- 生产仍 BLOCKED/NOT AUTHORIZED：真实生产备份、独立真实恢复、旧镜像与数据库配对回退、现场 host/tool/assets baseline、经批准的 off rehearsal/生产 authorization proof、线上非 PG03 验收和恢复公网入口均未完成。生产现场最新状态仍 UNKNOWN。正式 RC 身份不等于可部署授权；PG03 与账号池继续关闭。

## 2026-10-22 — 非 PG03 本地启动资产复核完成；生产仍未授权

续办复核确认：冷启动时 `/opt/clearapi/runtime` 缺失的创建顺序、Compose durable bind source、容器 `/run/clearapi` 目标和 inventory/manifest/helper 身份绑定已在本地形成一致合同。独立只读审查未发现当前冷启动修复或生成 installer 的新阻塞；审查没有重跑测试。更早报告中“路径不一致/旧 inventory 无法安装新资产”的结论保留为历史，已由 2026-10-22 的新实现和隔离证据取代。

- 既有隔离验证继续有效：runtime recovery 16/16、candidate installer 34/34、automatic release policy 44/44、rehearsal producer 9/9、backup tool 28/28；独立 review 确认生成包绑定 tree/RC manifest/image digest/helper/资产 hash。无生产访问或变更。
- 已完成与当前工作树源码相符的本地 off snapshot build；最终 tree、二进制、OCI 与 runtime bundle 哈希以 `evidence/non-pg03-20261021/RESULT.md` 中本节为准。该 build 仍不是 GitHub RC、GHCR digest 或生产部署身份。
- off rehearsal 的普通 Provider 和 pool-disabled gate 使用实际本地行为测试；proof 不依赖 PG03 Executor 配对。生产 authorizer 仍拒绝 off：当前 failed/isolated 生产基线、host 模式转换合同以及真实备份/恢复/镜像-数据回退均未具备。不得移除此门禁来绕过现场恢复要求。
- 正式 commit、受保护 main CI、GitHub RC、GHCR exact-digest roundtrip、真实生产备份与独立恢复、成对回退、现场 boot bundle 安装、线上验收均为 NOT RUN/BLOCKED；现场状态新鲜度 UNKNOWN。未提交、推送、触发 workflow、访问生产或部署。PG03 与账号池保持关闭。

## 2026-10-22 — durable runtime 与候选资产绑定修复；本地镜像已构建

候选 runtime 目录统一为 /opt/clearapi/runtime/new-api，与生产 Compose bind source 一致；容器内挂载目标仍为 /run/clearapi。启动资产先安全创建缺失的 /opt/clearapi/runtime，再由 tmpfiles 确保 runtime 目录 root:root/0750。旧 installer/source/inventory 未被覆盖；新增 bundle builder 从明确的 Git tree 与 RC manifest 生成新 inventory/installer，绑定 manifest SHA、OCI digest、tree、runtime/helper 和六项资产 SHA。旧 inventory、manifest/tree/helper 改动均在隔离测试中被拒绝。历史事故工具/记录保留，并拒绝用新 helper 处理冻结事故镜像。

- test-runtime-boot-recovery.py：16/16 PASS，覆盖冷启动父目录缺失创建、危险父目录拒绝、tmpfiles、身份重建、停止态和镜像/mount 漂移 fail-closed。
- test-boot-recovery-install.py：34/34 PASS，覆盖新 tree/manifest bundle、旧 inventory/错 RC/helper 哈希拒绝、隔离安装/归档/安装回滚，不启动服务。
- Release/off gates：自动授权 44/44、rehearsal producer 9/9、backup tool 28/28 PASS；workflow YAML、Python/Bash 语法及 git diff --check PASS。上述不构成真实生产备份、真实恢复或现场兼容证据。
- 本地 build：source tree f943440be1bab83712f7103c38ec8287c8d7498a（base f2e179ebc8e2d0f29b06603376a49a1058f9d496；不是 commit）；local run local-20261021T211111Z；linux/amd64 digest sha256:fcb733e9761b084c9721279d4e20270ab5cc29db0218e9a90b6c39d490496aad；Gateway binary SHA256 979713027c986fbd3d4f2e95bf74414f0f3db26481d19bc10c4753162bd9c394。source tar/OCI image archive 与 runtime bundle hashes 已记录；资产包绑定同一 digest/tree。产物位于 evidence/non-pg03-20261021/local-off-rc-20261022T-build1/。
- 不是正式 RC，也不可部署：没有 Git commit、GitHub RC run/artifact、GHCR push/pull roundtrip、已认证 CI 或 main 合并；manifest 明确 source_commit=null、production_ready=false。不能把该本地 digest 提交为生产部署目标。
- 生产审批包仍不可执行：最新生产状态 UNKNOWN；真实新备份、独立恢复、真实数据库/镜像回退、host 四工具合同升级、boot bundle 实际兼容安装及线上验收均 NOT RUN/BLOCKED。off authorizer 仍 fail-closed；PG03 保持关闭。未访问或变更生产、未触发 workflow/发布/部署。现场执行前需先形成正式 commit/CI/RC/registry identity，然后以该身份重做资产 bundle。

## 2026-10-22 — off gate 证据来源修正；上线仍 BLOCKED

复用独立只读审查发现，将 off rehearsal 的备份/回滚步骤由操作员 JSON 自述改成实际本地合同测试；清除已不可达的自述 Provider/pool-disabled verifier 测试。PG03 对应外部证据门禁未改变，off 生产授权仍在 host admission 前 fail-closed。

- `wsl python3 -B scripts/ci/test-automatic-release.py -v`：44/44 PASS；包括 off 生产授权拒绝。
- `wsl python3 -B scripts/ci/test-rehearsal-producer.py -v`：9/9 PASS，断言 off backup/rollback、Provider 与 pool gate 调用实际测试。
- `wsl python3 -B scripts/ci/test-backup-identity.py -v`：28/28 PASS，均为本地临时数据/替身；不代表生产备份或真实恢复。
- `wsl python3 -B scripts/ci/test-production-deploy.py -k rollback -v`：4/4 PASS，限本地回滚合同用例。rehearsal YAML 解析及相关 `git diff --check` PASS。
- **仍 BLOCKED**：旧 installer/inventory 固定于旧 RC/source，不能安装当前修改后的 runtime helper；候选 Compose 使用 durable `/opt/clearapi/runtime/new-api`，但启动资产只准备 `/run/clearapi`，两者尚未形成兼容、获审的新绑定包。真实备份、独立恢复、数据与镜像回退均 NOT RUN；失败→off host contract 未验收。当前 `sha256:f93f02ea…fa5b06`不包含本地改动，不能作为本轮候选。生产状态新鲜性 UNKNOWN；未连接或更改生产、未触发 workflow、未部署，PG03 维持关闭。

## 2026-10-22 — 本地隔离复验更新；生产仍 BLOCKED

修正本地boot恢复检查：从准确Gateway inspect读取并核验实际只读bind源，候选允许durable runtime路径，冻结事故仍锁定原volatile路径，未知路径/mount漂移拒绝；仅依据该源处理身份文件，不擅自改Compose。新增对应隔离回归。没有修改生产配置，没有连接生产、操作数据库或容器/服务、安装boot资产、触发workflow或部署。完整逐项状态见[evidence/non-pg03-20261021/RESULT.md](evidence/non-pg03-20261021/RESULT.md)的“2026-10-22”节。

- `wsl python3 -B scripts/ci/test-runtime-boot-recovery.py -v`：14项PASS。验证mounted runtime源绑定、tmpfiles目录恢复、启动顺序及停止态不启动Gateway；不代表生产资产已安装。
- `wsl python3 -B scripts/ci/test-boot-recovery-install.py -v`：31项PASS。验证本地隔离安装事务/回滚和systemd合同；不代表生产主机已有工具与安装器兼容。
- `wsl python3 -B scripts/ci/test-backup-identity.py -v`：27项PASS。验证身份、停止态合同、失败码和临时fixture备份/恢复行为；不代表真实生产备份或恢复通过。
- **生产发布保持 BLOCKED**：Gateway挂载根因为缺失宿主`/run/clearapi`已确认，但生产boot/helper兼容资产、准确新备份、独立真实恢复及旧镜像/数据回退尚未闭环。failed→off模式转换与off真实自动发布链也未验收。远端当前状态未重新读取，保持UNKNOWN；不得将本地演练或旧候选证据表述为当前线上成功。
- RC `39619413994/1` / `sha256:f93f02ea…fa5b06`仍仅为未部署候选；PG03维持关闭。下一阶段生产操作须另行取得覆盖准确目标和步骤的负责人批准。

## 2026-10-21 23:52 上海 — 获批一次有界只读诊断完成：备份首错与 Gateway 挂载故障已确认；发布仍 BLOCKED（最新权威）

用户已明确批准上轮约定的一次诊断。本轮实际仅1次SSH会话，复用历史私钥路径及严格ED25519 known_hosts；非交互执行stdin只读Python，不在服务器安装脚本。没有重试采集、重跑备份、reset-failed、服务/容器启停、配置/数据写入、解密恢复、部署或workflow触发。PG03未启用，原现场状态未改动。目标上下文仍为source75b0 / RC39619413994/1 / digest`f93f02ea…fa5b06`。

- **备份首个失败 CONFIRMED / 诊断PASS**：同一systemd invocation `3f1a8b289e1e46e4b27d5752bfdfff19`于2026-10-20 `19:31:16.415721 UTC`首先报`backup identity verification failed: expected exactly one running new-api container`。运行中Gateway身份前提不满足，在pg_dump前失败。约50毫秒后再报空密文目录`chmod .../20261020T193116Z/*`不存在；它是次生清理错误，不能取代首因。service仍failed/exit1；本轮没有重跑。
- **实际工具版本 PASS（绑定事故c50a）**：现场backup script=`0d70b1e1…`、identity helper=`fd6f4fe5…`、pg03-state=`1b974d1d…`、runtime-refresh=`afcefce7…`均精确匹配事故c50a Git原字节，root:root/0750。现场identity helper不支持停止态；上轮本地清理/阶段日志修复未安装。真实新备份/独立恢复与数据+旧镜像回退仍BLOCKED/NOT RUN；旧备份hash证据复用，不把只读诊断写成恢复通过。
- **Gateway故障 CONFIRMED / 诊断PASS**：准确旧CID `f8f963a7…`、image`22ecaa46…`，exited127、非OOM、非restarting。完整OCI错误为初始化期间`failed to fulfil mount request: open /run/clearapi: no such file or directory`；现场该host路径确实不存在，容器仍只读bind该路径。`/opt/clearapi/runtime/new-api`实际存在且root:root/0750，但不是该容器的mount source。有效启动路径为`/entrypoint-secure.sh`；本次失败发生在OCI初始化、进入应用之前，不是已证实的二进制缺失。目录为何消失仍UNKNOWN，未越界读取重启/删除历史。
- **剩余阻塞**：缺boot helper与既有兼容工具/恢复门禁仍未闭环；不得凭durable目录存在静默改mount。需另批受控writer窗口/准确停止态备份及工具修复，真实隔离恢复+旧镜像回退验收，再完成failed/isolated→off的现场合同。off本地证明修复及31项验证按原范围复用，生产执行护栏保留。当前main、自动开关、PG03调度值及公网未在本次范围重查，保持UNKNOWN（新鲜性），不沿用旧值冒充新观测。

执行/原始脱敏回执/本地归因分别为`evidence/non-pg03-20261021/bounded-diagnostic-execution.json`、`production-bounded-diagnostic.json`、`bounded-diagnostic-findings.json`；详细结果见[RESULT最新诊断节](evidence/non-pg03-20261021/RESULT.md)。**本次一次性诊断已完成并停止，不继续连接生产，不沿用此批准执行修复或恢复。**

## 2026-10-21 — 非 PG03 本地最小修复已落地：31项定向验证 PASS；生产仍 BLOCKED（最新权威）

继续使用现有工作区、source75b0 / RC39619413994/1 / digest`f93f02ea…fa5b06`作为目标上下文。最新结果见[回执本地修复节](evidence/non-pg03-20261021/RESULT.md)；仅本轮8个代码文件的可审阅差异为`evidence/non-pg03-20261021/local-minimal-repair.patch`。既有用户修改全部保留；未提交/推送、安装依赖、连接生产、执行容器/服务操作或触发workflow，未启用PG03。

- **备份本地缺陷 FIXED / PASS；现场首个失败 UNKNOWN**：已复现身份捕获exit17、dump exit23均被空密文目录的`chmod .../*`清理覆盖为exit1；留存现场安装脚本的c50a精确Git原字节也含该缺陷。改为只处理实际存在的文件，保留原始退出码；新增固定phase失败日志，不记录命令/参数/秘密。不是宣称现场19:31:16的首个失败已确认，也没有在生产安装补丁或重跑备份。
- **Gateway现场恢复 BLOCKED**：复用同CID的历史missing-runtime-mount诊断和后续`/run/clearapi`缺失/exit127；不把仓库durable路径模板套到现场volatile挂载。现有boot/恢复资产方案保留，不凭缺失新鲜错误、备份恢复和兼容工具证据擅改挂载/启动。
- **off证明合同本地 FIXED / PASS；完整off自动发布仍 BLOCKED**：producer→assembler→validator新增显式off/v2及所有receipt的mode绑定；保留共同11项集合中9项，将PG03专用2项替换为`pool_disabled`和`ordinary_provider_usage_billing`。旧PG03/v1仍受原全部门禁，不能改标签变off；controlled/v1授权对象保持已部署reader所需精确字段。自动workflow读取已验证的mode输出，去掉硬写PG03。**off真实gate producer/现场恢复与模式转换合同未闭环，authorizer明确拒绝off生产执行，旧host护栏未放宽**；没有新生产授权或启用开关。
- **实际验证**：Linux证明合同与受影响PG03兼容24项PASS；备份失败/成功加密读取/清理失败3项PASS（Docker为严格替身、GPG为临时测试密钥）；producer/assembler兼容4项PASS；Python AST、Bash语法、YAML解析、diff及补丁反向可应用检查PASS。Windows受影响24项中23通过、1项WinError1314符号链接权限错误，原记录保留；Linux同项通过。没有重跑未变前端/应用/三库/真实RC验收。详细日志及hash见同目录`local-repair-receipt.json`。

**下一个真实阻塞**：需要单独批准有界生产只读诊断，取得该失败backup invocation的脱敏首个错误/phase和准确helper版本，同CID Gateway完整State.Error/时间与非秘密mount/entrypoint元数据；本轮不连接。不要求重新粘贴已有文件。准确当前备份的真实独立恢复+旧镜像回退、兼容资产修复和failed/isolated→off现场合同仍BLOCKED/NOT RUN。f93f的旧成功证据按原范围保留，不能作为本轮新control/未来main资格PASS。生产、PG03及公网状态没有变更；最新现场/远端新鲜性UNKNOWN。

## 2026-10-21 — 非 PG03 续办：三个阻塞本地调查完成；等待准确现场证据，生产仍 BLOCKED（最新权威）

本节取代下方各历史“下一步/最新权威”的当前动作；原记录和已有工作区修改全部保留。仍以 source `75b0f09e2a527c5173c1179a6efe697de4f64f39` / RC `39619413994/1` / digest `sha256:f93f02ea737721e621681771424d3b0fca4fd8d1c8d6e3b7804097b793fa5b06` 为目标。详细结论及精确缺失项：[非 PG03 发布回执续办节](evidence/non-pg03-20261021/RESULT.md)。本轮只更新总控及该证据目录；无生产SSH、容器操作、配置/数据/PG03状态变更、workflow触发、依赖安装、提交或推送。

- **备份：BLOCKED；确切失败原因 UNKNOWN。** 复用22:55上海时间的service exit1/failed与旧备份密文/旧镜像在场记录。本轮证明已安装backup脚本hash精确匹配事故c50a源码，dump前要求running Gateway、不支持停止态；Gateway退出与该契约冲突是有代码依据的假设，缺该次journal/backup.log诊断，不能写成已证实根因。候选停止态契约又要求restart=no，而留存现场为unless-stopped；不能直接重跑备份或改restart。未找到该准确备份的真实隔离恢复/数据与旧镜像回退证明；合成恢复CI不能代替。
- **启动恢复：BLOCKED；历史挂载诊断有证据，最新完整错误 UNKNOWN。** 2026-10-20同CID回执明确missing_runtime_mount_error=true；10-21缓存仍为exit127、host `/run/clearapi`缺失、该目录只读bind。不能把127单独归因为缺少二进制。仓库Compose模板用durable `/opt/clearapi/runtime/new-api`，生产缓存用volatile `/run/clearapi`；当前off overlay不修复mount。候选entrypoint/binary已有RC限定验证，不替代生产effective entrypoint/挂载检查。两helper、四systemd/tmpfiles资产缺失且旧V2/control依赖不兼容，单补七资产不能闭环恢复。
- **自动off路径：BLOCKED / NOT IMPLEMENTED；静态原因定位 PASS。** 不仅authorize硬写PG03、proof/rehearsal强制Executor及Provider路径，deploy的off分支还要求无rehearsal ID。旧off wrapper要求健康Runtime、零Executor容器和零pool options；保留现场Executor及PG03资产时不可直接使用。V2明确拒绝未经批准的PG03→off退出。最小安全方案是同一链的mode绑定proof/receipt/authorizer/部署合同，保留所有共同CI/身份/迁移/Runtime/计费/备份真实恢复/回滚/熔断门禁，用实际关闭与普通Channel1业务证据承载off；PG03原合同不降级。设计已记录，未改实现或门禁。
- **本轮本地 PASS（限定范围）**：5项合成拒绝/错误分类探针；新RC ZIP及四项payload完整性、16个相关源码与精确candidate的CRLF规范化一致性、原28项证据inventory。回执：`evidence/non-pg03-20261021/continuation-local-audit.json`。旧RESULT原字节保存为同目录`RESULT.before-continuation.md`，旧inventory保留为历史快照。旧成功CI/启动/合成恢复按原范围复用，未重跑前端/三库或旧整套测试。

**当前最小下一步及停止点**：由管理员提供脱敏失败backup invocation诊断及工具字节绑定，随后提供同CID完整State.Error/时间/非秘密entrypoint与runtime/secret挂载元数据；若需代理现场采集，须另批受限生产只读访问。此请求不包含重跑备份、改变restart、安装、恢复启动或部署。准确当前备份及真实独立恢复/镜像回退、兼容资产修复、failed/isolated→off的一次性恢复授权仍分别必需。本轮不越界取得这些证据，停止于此；当前远端main/自动开关/生产状态新鲜性UNKNOWN，不能沿用历史值冒充本轮新鲜核验。**PG03生产状态不变；非 PG03尚未上线。**

## 2026-10-21 23:00 — PG03 暂缓；非 PG03 新 RC 完成，生产因恢复/回滚门禁停止（最新权威）

当前目标更新为：**PG03 暂不部署，先上线其余网站功能；当前尚未上线。** 历史开发成果、失败记录及并行任务/已有工作区修改全部保留。完整结果、精确命令/版本/限制：[非 PG03 发布回执](evidence/non-pg03-20261021/RESULT.md)。本轮只更新总控和该证据目录，未提交/推送源码，未进行生产变更。

- **新候选真实构建**：远端 main/source `75b0f09e2a527c5173c1179a6efe697de4f64f39`；新 RC `39619413994/1` SUCCESS；新镜像 `ghcr.io/a3225865526-wq/clearapi@sha256:f93f02ea737721e621681771424d3b0fca4fd8d1c8d6e3b7804097b793fa5b06`；binary SHA256 `37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5`。通过既有精确 Gateway RC workflow 构建，workflow 的 PG03 名称不代表启用账号池；未用旧镜像冒充新候选。Artifact `12353708716` 原 ZIP/API digest、四项 inventory、source/OCI/Dockerfile/schema 身份已核验；manifest 仍为 production_ready=false / production_acceptance=not_run。本机额外 pull 因 registry scope 被拒；本轮 RC runner 的真实拉取及 binary roundtrip 成功，未声称本机启动通过。
- **本轮新验证**：CI `39619405764/1` SUCCESS，7 jobs SUCCESS、1 个仅 PR 使用的 PG03 package-read job 条件 SKIPPED。真实 SQLite3.50.4/MySQL8.4.6/PostgreSQL17.6 商业 HTTP 生命周期、迁移及重复启动 PASS；注册/登录/API Key/usage/精确扣费/拒绝退款/零余额阻断使用隔离合成上游，非线上验收。前端220 files/2379 tests及生产构建 PASS，未运行独立 tsgo。后端 root/relaykit 测试实际exit0。本地 wrapper 91PASS/1SKIP（未请求real producer artifact），automatic-release 36PASS。
- **关闭 PG03 可分离**：普通非pool渠道不要求Executor；管理入口 `CLEARAPI_POOL_ENABLED=false` 和调度master有效值false须分别验证。候选 `compose.non-pg03.candidate.yaml` 显式false，尚未应用。新RC在显式pool=false、无Executor的独立PG17.6完成schema-only两次启动/健康/正常退出，不冒充生产数据升级/恢复。关闭pool不跳过迁移/后台任务，已有source=pool模型不自动降级API。
- **新鲜生产只读事实（22:50–22:55 Asia/Shanghai）**：复用原Windows OpenSSH私钥路径/既有known_hosts严格校验成功；master行缺省（有效false），pool route/source/bound channel、启用路由链及池候选均0；仅Channel1启用且1条enabled ability，Channel2/3保持禁用。5个用户/6个token聚合检查负余额计数均0，未读取用户凭据。生产schema hash `3c1c77f3…` 与新RC target一致，不以相同DDL代替数据/恢复证明。
- **实际生产镜像/线上**：没有运行中的Gateway。现存 `clearapi-new-api-1` 为image/digest `sha256:22ecaa468c784cd41ee2604dd8f3ac189960744516a508fc8c9cee3ee1724e02`、source `c50a53c1b89e121bcade7dac41d3f11e53a9832a`，Exited127。原容器管理开关仍true，未谎报已改false；调度master/发布状态保持关闭。Executor `sha256:cfad1b51…` 与PG17.6运行，Caddy exited/restart=no。公网首页及/api/status本轮均HTTP521；真实登录/Key/扣费/上游渠道线上验收 NOT RUN。
- **生产停止条件已触发**：/run/clearapi和boot/recovery资产仍缺失；backup timer active/enabled，但backup service当前failed、ExecMainStatus=1。原加密备份20261020T002138Z的manifest/加密件hash与旧镜像在场已新鲜复核，真实生产数据+镜像恢复仍未验证。官方只读检查39619582548因V2 wrapper/control/backup脚本三项与main不同而FAIL，未调用Runtime。最近真实部署39347012653原日志新鲜回读，确认为切换后PG03 Gateway→Executor门禁失败、未自动回滚。按本次用户要求停止生产写入，不盲目切换或开放Caddy。
- **当前自动发布链**：11个workflow已按远端main原字节核验；CI(push)→GatewayRC→Deployment rehearsal→Automatic release的自动authorize仍强制pool_mode=pg03及Executor/provider proof，尚无可直接使用的自动off分支。手动off入口存在且旧wrapper字节匹配，但要求健康baseline，不能绕过事故恢复/隔离/回滚门禁。两个自动开关首尾均false；本轮自动下游SKIPPED，strict保护未改，未触发Deploy Production。

剩余最短条件：先解决备份服务失败，完成准确备份的真实隔离恢复/数据与镜像回退，再按受控流程闭环boot资产和失败baseline；随后以最小mode-aware自动流程承载off，保留当前main/CI/RC/镜像/迁移/普通渠道计费/备份恢复/回滚门禁，再上线并验收。若main改变，重新核实候选资格，不能沿用本轮PASS冒充新源码验证。**PG03独立后置**：候选/control资格审查、全writer窗口、真实备份恢复、adapter现场及Executor→Provider usage/billing验收；不删除其成果或启用账号池。非PG03生产发布仍BLOCKED，本轮在真实恢复/回滚阻塞处停止。

## 2026-10-21 — 当前授权收尾：P1 本地修复与独立针对性审查通过；新 control 未提交，生产仍 BLOCKED

本节为本轮最新权威，取代下方“尚需批准本地修复”的当前动作；历史原始 control、失败和验收记录全部保留。报告：`evidence/pg03-control-race-20261021/REPORT.md`；精确候选：同目录 `candidate-manifest.json`；独立最终复审：`independent-final-review.json`。

- 已确认本地 HEAD `f2e179ebc8e2d0f29b06603376a49a1058f9d496` 与历史 control75b0 同 tree617d16；已有脏工作区修改未清理。仅变更 observation、host 和集中 observation 测试三文件，adapter 原字节不变。监视器先完成首轮有效 before 采样；action 前同步核查；实际 host 入口在授权/journal/命令前执行 one-use live guard；失败锁存、部分启动有界收敛且不可重试。
- 原实现新增回归26项产生9个失败（含subtest），原始日志/代码已保留。第一版27项虽通过，独立调度探针仍确认“已锁存失效后调用action”的P1；该失败版本及独立BLOCKED记录保留。最终入口guard版本经独立AI针对性复审无新增阻断；不冒充完整control/策略来源审查、GitHub人工review或签发。
- 实际本地验证：WSL observation29、successor-plan82、host12，共123项OK/0skip；Windows observation28通过/1明确fcntl平台skip。三文件AST及定向diff检查通过。fixture不作生产证据；此前Windows独立入口探针的fcntl ERROR如实保留。应用/source/RC/digest及原隔离控制未变，固定真实隔离39517646061/1的有效范围直接复用，未重跑。
- 新本地control内容身份：`pg03-control-local-sha256:76f32e2d44b7e59e1936a8ce021328b92bc85459443657c6ba194343314e1019`。它是base tree加三文件overlay，**不是Git commit**；实际工作区其他已有修改不属于该候选。observation SHA256 `2c5ab3be2d9067ec71ac409f59bfe4ab0efd7c85c529d5542e2ab10287e8b5be`，host `b6086ec92c60d2cac741073e739fb38fb93faf29f64fd4ddaa2504e046d70924`，tests `1621353991c3a0d3ac3d6cef35363a3b8febf48b34f17882a355c8101dc3fd29`。
- 当前授权任务完成后停止：**无提交、推送、CI/Actions触发、外部配置写入或生产操作**。新control的CI NOT RUN、真实资格 BLOCKED/NOT ISSUED；可信策略来源完整独立审查/真实authority与签名根、生产身份/实时独占窗口、真实备份恢复、adapter现场验收及正式部署/线上回滚准备仍未通过。合作guard准入后的在途fencing与远端原子撤销未获现场验证，不能由本地测试代替。

下一步最小操作：负责人另行批准选定本轮三文件及必要证据的正常提交/推送/必需CI（不夹带既有修改），形成真实control commit后绑定新候选并判断资格；随后落实可信来源与真实签发。生产相关权限继续分别审批，不能沿用本轮本地授权。无需重做身份未变且验收范围未受影响的既有成功验收。


## 2026-10-21 — Production Readiness 自主补齐（本轮最新，生产仍 BLOCKED）

本节取代历史“完整adapter尚未实现”的当前开发状态，历史记录与失败证据保留。正式待批包：`evidence/pg03-production-approval-package-20261021.md`；候选/观测/恢复协议与管理员清单分别见同目录新增报告。

- 固定 source799a / RC39511197302/1 / digest729fdd 的真实隔离39517646061/1继续PASS，本轮未重跑；应用/迁移/依赖/Dockerfile未改，不需重建应用RC。生产资格不自动继承，旧current-main门禁保留。
- 已新增只读候选资格协议、实际source/control差异与控制字节绑定、到期/撤销/重放拒绝；独立grant工作流只产审批artifact，无生产步骤，本轮未dispatch。
- 已新增两阶段新鲜采集/60秒连续观察和五阶段ExecutionWindow、独立grant与六类证据协调器、固定ProductionHost动作及每phase持久journal、同CID fresh challenge启动器；默认无现场transport或执行权限。
- 已新增离线备份材料审计及默认plan的签名授权独立恢复runner。真实备份/密钥/生产DB均未接触；独立日志库若存在而缺dump即拒绝。真实生产producer/源指纹、采集身份、全writer窗口、backup/restore和adapter现场验收仍BLOCKED/UNKNOWN/NOT RUN。
- 本轮本地测试、独立审查、PR/CI及最终控制commit以待批包末尾最终记录为准。开发变更不能冒充生产验收；未连接生产SSH、安装权限/配置、生产helper/DB/容器操作或正式部署。

## 2026-10-21 - 固定候选资格策略独立复审（BLOCKED）

可信策略来源实现、证据绑定与操作依赖清单见 `evidence/pg03-policy-source-delivery-20261021.md`；固定候选原始绑定见 `evidence/pg03-fixed-candidate-binding-20261021.json`。负责人已批准候选限定策略边界，无需重复申请同一边界；固定control差异独立内容审查发现P1，可信来源原型最终独立复审未完成，故不得签发policy或作候选PASS。

- 负责人已批准的范围仅为仓库 a3225865526-wq/Clear-API 中 source 799a608d1b5a79df0f2ef8031a7885f414125778、RC 39511197302/1、镜像 sha256:729fdd37591fb333c9cefe202d7c2b7ff4486df31a2689a9b2152abdf7f6965f 与 control main 75b0f09e2a527c5173c1179a6efe697de4f64f39 的一次候选资格判断；不包含生产执行授权。
- GitHub Compare 实际结果：ahead 11、behind 0、merge-base/base 为 candidate source、head 为指定 control main；52 个文件均为 scripts/、.github/workflows/、evidence/ 或总控文件下新增/修改，无删除、重命名或应用源码/依赖/Dockerfile 路径。规范化 compare projection SHA256：265b4fa0b7ec3d963563b7962623c314f58c5bd00befa56983450da8c988fcd3。
- 独立代码复审完成但结论为 BLOCKED：差异路径范围满足 control-only；策略来源、独立审查到精确 diff 的可信绑定及持久原子防重放不通过。该复审不是 GitHub 人工 review，也不冒充审批签名。
- 真实来源检查：仓库仅有 production environment（ID 23693288049），仅 branch policy、没有 required reviewers；pg03-qualification environment 不存在；Actions variable PG03_QUALIFICATION_STATE 查询 404。实际 policy、独立签名、review 和资格结果均未签发。
- 新资格判断仍 BLOCKED。tools/pg03 候选原型已完成本地修复与 27 项定向回归；最终独立审查仍发现指定 control 差异中的 P1 竞态：`scripts/pg03-observation.py` 启动异步监视后直接调用受保护 action，`scripts/pg03-successor-adapter.py` 继承该风险。完整 52 文件内容/字节绑定报告见 `evidence/pg03-fixed-control-independent-review-20261021.json`，结论 BLOCKED；不能通过修改本地副本冒充修复指定 control commit。当前原型字节 hash 为 authority `2bc49018d0bef2fa7c467d2c9e09bb0d9485fe522ce32800f3beca0c4bb5eb8f`、policy source `51c959d1630bde04186f38c7f8a55fc9c026093a315506e722e5964693426235`、tests `257d33ee4afe388d0cf81a94c93c5ae028a7a9b756c2e5f46ce6cf6f6f253f84`。生产 adapter 保持指定 control 原状，不将候选批准扩大成生产集成授权。实际可信服务、独立信任根和真实签发均未提供；未 dispatch workflow，未更改 GitHub environment/variables/权限，未连接生产。

- 开发PR82已正常合并：main `75b0f09e2a527c5173c1179a6efe697de4f64f39`；head f2e 的CI39578824035/2八jobs SUCCESS（只重试frontend一次，首次失败保留）；合并后CI39580985299/1也八jobs SUCCESS；自动RC/演练/生产release三个下游run均SKIPPED，两自动开关仍false。最终回执：`evidence/pg03-readiness-final-receipt-20261021.json`。最终本地交付回执见待批包第11/12节，未为回写结果重复触发CI。

剩余最小事项：候选策略边界批准已取得；但固定control独立审查有P1，且策略原型独立复审尚未完成，禁止签发或资格PASS。负责人需明确是否批准仅本地修复P1并另行形成新control；该决定不自动授权提交、推送或触发CI。随后需独立审查/CI、新control候选范围审批、非生产可信authority和签名根、一次性真实资格判断；再分别取得生产只读身份/观察、准确写入投影、真实备份恢复、adapter现场验收、生产grant/部署及公网恢复的具体授权。当前不具备正式生产部署审批条件；不得自动部署或恢复公网。



## 2026-10-21 07:55 — 真实隔离验收PASS；生产就绪仍BLOCKED（最终最新权威）

本节取代下方历史“当前/待运行”状态，全部旧工作和失败记录保留。最终报告：evidence/pg03-final-acceptance-20261021.md；生产准备：evidence/pg03-successor-production-readiness-20261021.md。

- **P2-1 PASS**：Linux原子发布/失败回读回归及真实runner最终写入/fsync/finalizer/verify-public/白名单上传均通过；不扩大为断电验收。
- **P2-2 PASS（固定候选真实隔离）**：新source799a608d1b5a79df0f2ef8031a7885f414125778 / RC39511197302/1 / image ghcr.io/a3225865526-wq/clearapi@sha256:729fdd37591fb333c9cefe202d7c2b7ff4486df31a2689a9b2152abdf7f6965f / binary37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5。候选侧权威迁移凭证、driver错误锁存及三库定向回归已通过；旧69a/07fd不当新对象。
- **PG03真实隔离 PASS**：专用run39517646061/1 SUCCESS，control f02528c351d4812336980fd56810658a6339b05c；新digest packages:read实际拉取及运行身份匹配。同一独立PG17.6数据库两次完整启动/健康/正常退出，逐次前后schema和独立challenge/CID的原始9字段迁移receipt通过；另空白只读PG真实迁移拒绝SQLSTATE25006/exit1。6个owned容器全部移除、final无剩余/auth清理、持久化与回读成功。
- 原始成功artifact12308671421及ZIP SHA816b0127874f3a77d50b33e52353742300c8d01ce4d3e3c0d43ea8779589136b、result/success/verification保存于evidence/pg03-isolated-39517646061/；API来源/digest/control文件hash/全部receipt已本地回验exit0，并经独立只读复核通过。仅schema-only/master/共享main-log单PG及该固定镜像，非生产Runtime/业务数据升级。legacy日志capture_complete=false与负向tmpfs proof不可采集均保持原值，不伪造PASS。
- PR78/79/80/81正常合并，相关完整CI每次7 jobs SUCCESS；最终main f02528的CI39517515354也已7 jobs SUCCESS，三库实际版本SQLite3.50.4/MySQL8.4.6/PostgreSQL17.6。最新PR81日志集中67项OK/0skip；本地Linux73项PASS（含adapter6项）、R2 gate14项PASS、OneShot3项PASS，独立审查通过。原始失败run39514525897和39516153502全部保留；总3次真实演练、修复重跑2/2结束，不再自动追加。
- **生产就绪/PG03整体 BLOCKED**：现生产current-main资格不接受旧source799a直接套用新control；新鲜只读身份和全writer连续窗口、准确获准启动写入投影、真实停止态加密备份/离线恢复均未获真实验收。successor kernel/备份组件与R2/adapter本地基础已完成对应范围验证；完整production adapter NOT IMPLEMENTED，未安装/激活，不能称“只剩部署审批”。

最小下一步：负责人先确定生产source/control资格协议（最终main冻结新RC验收，或另批分离协议）、只读采集/连续全writer窗口通道和获准写入范围；管理员另批并核实身份/权限/期限与非秘密现场事实；随后完成完整adapter绑定、获准备份/真实恢复与独立一次性生产审批。现行current-main/身份/迁移/权限门禁不降低。生产SSH/配置/数据/容器、正式部署和公网恢复均未执行。任务按情况B收尾，停止在真实授权/协议/证据边界。



## 2026-10-21 — 最后第2轮隔离重跑已触发，结果待完成（最新权威）

PR81/head1fc0158e7433e8671541ac4775f850d084dd6fd7的CI39516682473七jobs SUCCESS，日志实证集中67项OK且0skip。独立审查通过，新鲜保护/main/两false开关核验后正常合并control f02528c351d4812336980fd56810658a6339b05c；未绕过必需检查/人工要求。完整10-workflow固定字节再次核验后，仅dispatch专用run39517646061/1。

真实总共已触发3次（初次+2轮修复），当前修复重跑2/2，禁止第三轮自动修复重跑。候选仍source799a / RC39511197302 / digest729fdd；最终真实结果待完成。两份原始失败artifact全部保留；未发布成功凭证前P2-2镜像验收/PG03仍BLOCKED。

生产current-main/身份与连续全writer窗口/准确获准写入/真实备份恢复/完整production adapter阻塞不变；安全本地准备已形成待审差异，没有安装/激活或生产执行。



## 2026-10-21 — 第1轮重跑迁移凭证校验失败；最后hostname修复待CI（最新权威）

- 专用run39516153502/1 FAIL：新镜像/PG17.6/启动前schema通过；第一次Gateway start/health/SIGTERM正常退出均通过且DDL不变，但结构化迁移凭证invalid_structured_proof，不能以健康/退出/schema判PASS；第二次/负向NOT RUN。3个owned容器/auth清理成功，失败证据持久化回读上传，原始artifact保留。
- 已确认原控制假设hostname==Gateway CID[:12]不适用于Docker container:网络：Moby会继承donor hostname。原raw不在失败artifact，具体失败字段未实测。最小控制修复改为运行中精确CID的inspect Config.Hostname与exec hostname双源相等验证，再将该期望值用于专用receipt与回读；CID+fresh challenge+pid+binary+阶段/DB门禁均保留。两次不同CID/challenge仍必需，hostname允许因同网络相同。
- 相关Windows15项与Linux root集中73项PASS（含本地adapter6项）；独立审查通过。此次仍仅控制修复，应用799a/RC39511197302/digest729fdd不变，不需重建。
- 修复重跑已经1/2，最后第2轮尚未触发；须先正常CI/保护/当前触发链与两false开关复核。P2-2真实镜像验收和PG03仍BLOCKED。

生产current-main资格、身份/连续writer窗口、获准写入投影、真实备份恢复及完整adapter缺口保留，不生产部署。



## 2026-10-21 — 日志修复已正常合并，真实隔离第1轮修复重跑进行中（最新权威）

- 修复PR80/head5024c6f4f65756aff128e71df95fcded71a3aac0，完整CI39515073600七jobs SUCCESS，独立审查通过。正常merge97765d27f98889eda43484d813ed146b1f4dc823，未绕过保护/required checks。新鲜10-workflow/两false开关/strict保护再次通过后，仅手动dispatch专用run39516153502/1。
- 第1轮修复重跑已触发（1/2），本次仍为候选source799a / RC39511197302 / digest729fdd；仅控制日志参数/回读修复，未变更应用。首次run39514525897 FAIL及完整原始失败artifact保留；新run未完成前不判迁移或隔离PASS。
- R2 gate/OneShot与successor adapter基础合同已完成安全本地代码、针对性测试与独立复核，详见production-readiness报告及local-components字节清单；无生产安装/激活/凭据或执行入口。完整production adapter NOT IMPLEMENTED。
- 已明确额外生产资格阻塞：旧生产release-control仍要求candidate/RC source为实时main，799a与控制merge分离的隔离验收不自动满足该门禁。不得降低current-main检查；最终源码冻结重建/验收，或另批并实现source/control分离的一次性successor审批协议。

当前继续收集实际演练结果；生产身份/全writer连续窗口/准确获准写入/真实备份恢复及最终adapter仍BLOCKED，不进入生产部署。



## 2026-10-21 — 首次真实隔离失败已保留；日志配置修复待CI（最新权威）

- PR79/head41c58069ae77914a09e757176d65e30d92651958 的CI39513279138全部7 jobs SUCCESS，独立审查通过后正常合并为control1e155bc49c942ec72a30995670a720101fdaf7c8。两false开关、10 workflow原字节/触发图及strict保护已新鲜复核，未绕过人工或CI门禁。
- 专用真实run39514525897/1（唯一workflow_dispatch）FAIL：packages:read已实际获取新精确digest729fdd…；RC ZIP、image ID/OCI/binary37bb…、PostgreSQL17.6及启动前schema3c1c…一致。Gateway第一次create成功、docker start退出1，尚未进入迁移；第二次启动与负向探针NOT RUN。全部3个owned容器清理成功、registry auth已移除、finalizer拒绝success且失败回读/上传成功。无成功receipt。原始artifact及验证元数据保留在evidence/pg03-isolated-39514525897/。
- 已发现显式max-size=-1违反Docker logger的正数参数解析，符合start阶段失败的源码解释；因原始stderr未保留，不伪造实测报错。拟删除该非法参数，并在每次正/负容器start前严格核验实际日志配置只有json-file+blocking、无继承轮转；错配置/inspect失败即拒绝并清理。候选应用源码未改，不需重建RC。
- P2-1本地PASS；P2-2源码机制/三库回归PASS，但真实镜像验收仍BLOCKED。真实初次演练1次FAIL，修复重跑尚0/2；修复正常CI/独立复核通过后最多再2次专用演练，不使用rerun旧控制。
- 阶段D继续本地R2 gate/单次observer/adapter基础和严格scope/restore合同准备；门禁未安装或激活，完整生产adapter及新鲜身份/全writer窗口/获准写入/真实备份恢复仍BLOCKED。生产操作均未执行。



## 2026-10-21 — 新RC身份核验完成；专用真实隔离验收待运行（最新权威）

本节取代下方历史“当前”状态；原记录及失败证据全部保留。PR78正常合并，main/source 799a608d1b5a79df0f2ef8031a7885f414125778 的完整 CI39510261549 七个job全部success，三库实际版本见 evidence/pg03-source-ci-39510261549.json。

- P2-1：Linux本地PASS。P2-2：候选源码机制和真实三库定向回归PASS；新镜像专用迁移凭证双启动验收仍NOT RUN，不由日志、退出码或schema相同替代。
- 新RC39511197302/1构建SUCCESS。source 799a608d1b5a79df0f2ef8031a7885f414125778；image ghcr.io/a3225865526-wq/clearapi@sha256:729fdd37591fb333c9cefe202d7c2b7ff4486df31a2689a9b2152abdf7f6965f；binary SHA256 37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5。原始artifact ZIP/API digest、payload hashes、OCI/source/CI/二进制/目标DDL一致性均已核验，见 evidence/pg03-rc-39511197302/verification.json；旧69a/07fd候选不用于修复验收。
- RC runner已实际拉取该新digest并执行legacy schema两次启动；该步骤未启用本次challenge迁移凭证，不能当专用真实验收PASS。下一次专用runner的packages:read精确获取、凭证双启动、空白只读库迁移拒绝及证据持久化仍NOT RUN，真实演练修复重跑0/2。
- 控制增量已固定新RC并增加独立只读测试库负向探针；完整Linux root集中回归62项PASS，Windows受影响24项中22PASS/2平台skip；独立复核无阻断项。9项successor本地kernel合同/事务方法包含在62项中，无生产adapter/mutation CLI，不冒充完整执行器。生产只读资格、全writer窗口、列级写入投影、真实备份恢复及adapter仍BLOCKED。
- 当前控制分支 codex/pg03-exact-rc-acceptance-20261021 待提交/正常CI/条件合并；只改变控制脚本、测试和证据，候选应用源码/Dockerfile/依赖不变。控制commit与候选source分别记录，不把控制变更冒充镜像源码。

继续正常PR及必需CI，动态复核触发链/两个false开关/保护后，仅运行专用隔离workflow。生产SSH、配置/权限/数据/容器、正式部署及公网操作未授权、未执行。PG03整体仍BLOCKED。


## 2026-10-21 06:50 — 阶段A/B：PR78已合并，等待main CI后构建新RC（当前）

- PR78 已在 strict required checks 与管理员保护下正常合并，未绕过审查。head 1a57f501a9414abfdaaf3c865e90bf4501600ff2 / CI39509230715 的7个job全部success；merge source 799a608d1b5a79df0f2ef8031a7885f414125778。三库实际版本/覆盖和P2修复证据见下文，P2-1/P2-2本地机制验证PASS。
- 新鲜10-workflow图/原始字节、两个false发布开关和strict保护核验通过；专用隔离workflow已注册，但仍绑定旧RC，绝不dispatch旧候选。main push CI39510261549仍进行中，须成功后才手动触发专用RC构建；生产操作未授权、未执行。
- 备份26项Linux root完整回归通过（真实GPG/归档，DB/Docker替身）。新增successor严格request/观测/结果合同、一次性持久claim/固定阶段/异常收敛内核形成，9项Linux root方法通过并独立审查修复闭环；该增量当前仅本地，生产adapter/server认证/有界调用/全writer和列级范围未实现，不能冒充完整生产执行器。
- 报告已修正现有只读helper能力：它不采DB/schema/金融/完整配置/epoch；R2 gate/observer接线尚无实现，不能让管理员安装不存在的实物。真实生产资格/连续独占窗口/备份恢复和最终apply仍BLOCKED。
- 新RC/完整digest/新镜像获取、专用真实双启动均NOT RUN，真实演练修复重跑0/2；PG03整体BLOCKED。

继续main CI→新RC→精确身份→更新控制固定值/正常CI合并→仅专用隔离workflow。现有工作区/失败记录全部保留，不执行生产SSH/部署/恢复公网。


## 2026-10-21 — 多阶段最新状态：三库迁移凭证通过，完整CI/新RC/真实隔离待完成

本节为当前状态，以下历史证据保留。PR #78 / codex/pg03-migration-proof-20261021 正常验证中，不绕过required checks；本轮非生产提交/推送/合并注册/隔离运行授权按最新用户任务执行，生产权限不扩大。

- P2-1：Linux本地 PASS，原子发布/失败回读门禁不降低。
- P2-2：机制定向验证 PASS：Linux Go两包通过，真实 SQLite3.50.4、MySQL8.4.6、PostgreSQL17.6 三库CI通过，主/独立日志库重复启动及数据保留覆盖；CI Go1.25.1、本地Go1.26.1。driver错误、Row.Err/延迟读取/Close/事务/非SQL吞错、坏旧选项、凭证发布失败和严格控制端解析均有针对性覆盖。新RC真实镜像验收仍NOT RUN，不继承旧digest成功。
- 独立审查：源码/驱动转换/Result能力、旧schema夹具适配、包读取CI无skip接入均复核通过。完整CI曾因旧夹具失败，已修复且Linux单项通过；新head全部required CI尚待，不能合并。
- 包读取：Actions packages:read 已实际读取旧精确镜像，证明该次仓库token能力，不是新候选验收。新source/digest/RC、两次真实隔离启动仍NOT RUN，真实演练修复/重跑0/2。
- 阶段D：停止态failed Gateway备份及完成校验本地增量完成，6个定向测试方法通过，独立审查发现后修复并复核通过；真实GPG加密/归档但DB/Docker替身，不冒充真实备份/恢复。生产启动写入面、身份/有效期/审计、部署前后/回滚及最小管理员动作记录于 evidence/pg03-successor-production-readiness-20261021.md。
- 完整successor生产执行器仍BLOCKED：现只有离线admission及备份组件，缺新鲜生产资格/有效配置与准确允许写入投影、全writer独占窗口、真实停止态加密备份和离线真实恢复、独立的一次性生产审批协议。不能把failed改success或直接调用旧V2/恢复入口替换镜像。

继续完成当前CI、正常PR注册、新RC精确身份及专用隔离演练；其他缺生产证据的步骤停于其边界。PG03整体仍BLOCKED，不执行生产部署/SSH/数据库或容器操作。详情见 evidence/pg03-autonomous-progress-20261021.md。


## 2026-10-21 — 自主多阶段推进中：阶段 A/B 验证与 PR 准备（当前）

本轮最新授权覆盖安全分支/提交/推送/PR、必要 CI，以及满足全部门禁后的隔离工作流注册和运行；不再沿用历史“这些操作一律待批”的停止条件，生产禁止项完整保留。详见 `evidence/pg03-autonomous-progress-20261021.md`。

- P2-1 继续 Linux 本地 PASS；P2-2 已以 driver 读取/事务错误观察补齐 Row 后置错误路径，独立源码审查未发现阻断项，仍待 Linux/真实三库结果，不能提前 PASS。
- 便携 Go1.26.1 官方归档 SHA 已核验，任务目录运行、不改系统 PATH/依赖版本/锁文件；gofmt 已执行，Windows 定向两包 go test exit 0。Linux 定向验证进行中；CI 专用三库/日志库步骤已准备。原 Python 51 项集中回归通过（Windows9 skip）。
- 已新鲜读取 main、两个 false 发布开关、三项 strict required checks 与保护；原生产 workflow 未修改。当前分支 codex/pg03-migration-proof-20261021 基于实际 Clear-API main，原工作区改动保留。
- 新候选、精确镜像和真实两次启动尚未执行；真实演练修复/重跑 0/2。阶段 C 仍依赖 P2 与 CI、包 Read 和触发链门禁；阶段 D 继续本地准备，生产证据不使用模拟替代。

当前继续验证和安全 PR，不执行生产操作。


## 2026-10-21 — PG03 自主推进：候选侧迁移凭证差异已形成，P2-2/真实验收仍 BLOCKED（当前权威）

本节取代历史章节中的“等待普通步骤确认”作为当前执行状态，历史证据全部保留。依据本轮授权已完成本地源码差异、离线验证和一次独立审查；详见 `evidence/pg03-migration-proof-source-review-20261021.md` 与身份/哈希清单 `evidence/pg03-migration-proof-preparation-20261021.json`。

- **P2-1 PASS（Linux 本地）**：现有原子发布/失败撤回/回读上传门禁保持；本轮集中回归再次覆盖。没有真实 runner/断电验收，不扩大 PASS 含义。
- **P2-2 BLOCKED**：新增 opt-in 候选侧专用凭证，直接跟踪真实 InitDB→旧选项→InitLogDB 完成顺序、结构化 GORM 错误和非 SQL 吞错；失败不发布可用凭证。凭证绑定本次 challenge、容器 hostname/PID、实际 binary hash 和主/日志库类型，文件/目录持久化失败阻止启动。控制脚本逐次从活容器读取并校验；缺失/冲突不出成功 receipt，日志/exit 0/schema 相等不能代替它。另已核实 GORM v1.25.12 的 Row/Rows 后置 Next/Scan 错误不能靠 Trace 完整观察；proof 模式现对这类查询无条件拒绝成功，正常迁移预计也会 BLOCKED。正向源码闭环尚未完成，不能删除护栏求通过；Go 与真实数据库未跑，不能判 PASS。
- **修改范围**：`main.go`、`model/main.go`、`model/frontend_option_migration.go`、`model/gorm_logger.go`；新增 `internal/migrationproof/proof.go` 和唯一新 Go 测试文件 `proof_test.go`，扩展原 `model/gorm_logger_test.go`/`model/frontend_option_migration_test.go`；schema/successor/集中 Python 测试及本轮证据。successor CLI 也强制迁移证据，不能通过省略 observation 跳过。未修改任何 workflow、adapter 固定候选常量、依赖或锁文件。
- **实际验证**：WSL Linux 最终 51 项全部通过；Windows 51 项中 42 通过、9 项平台相关跳过。3 Python AST、11 源码/测试 UTF-8/尾空白、diff 检查通过。原 46 项作为受影响集中回归复用和扩展，没有重跑无关 boot/全量 CI。Go 编译、Go 测试和 gofmt NOT RUN：当前未找到工具链，未安装依赖。Docker Linux named pipe 不可用；真实三数据库矩阵/独立日志库、引擎版本和启动验证均 NOT RUN/N/A。
- **独立审查**：子代理只读审查发现 uptime 两处吞错未锁存的 P2，已补 Fail 和坏值/凭证不生成回归；复核确认修复。随后双方确认 Row/Rows 观察缺口，已增保守拒绝护栏并复核，但正向证明仍未解决；不声称无剩余缺口。审查不代替尚未执行的 Go/数据库验证。
- **必须新候选**：原 source `69a66431646b5cc8912bf97a739e08de07316443` / digest `sha256:07fd58272b4fc62bcc52a3ad1d20e0dac357ba93de9e91a6a797d5e8fe3e8e7a` / RC `39357150402/1` 不含凭证生产代码，保留原样并继续 BLOCKED。修复需新源码提交/重新构建，不能替换旧 digest 的含义。新 source/digest/RC 尚未产生；本地 HEAD `ff6e0f4b2f5096d12717a01afbb3f5d90d28b801` 不冒充修复候选。现 adapter 仍绑定旧 RC，不具备注册/运行就绪条件。
- **真实状态**：镜像获取/新候选身份核验 NOT RUN；新候选构建 NOT RUN；第一次/第二次真实隔离启动 NOT RUN；真实迁移/schema UNKNOWN；没有真实成功 receipt。新候选身份、验证命令、隔离条件和后续授权顺序已写入阶段证据，不能当作实测结果。
- **边界**：未提交/推送/PR/合并/注册/触发 workflow；未取镜像或启动真实 DB/容器，未改生产配置/权限/Secrets/发布开关，未连接生产 SSH、写生产数据、开放入口或部署。旧修改和历史证据保留。

最小下一步：提供已有获准 Go 与隔离数据库验证环境（或另批工具链安装），先让迁移查询读取错误显式返回/传播，解决 Row/Rows 正向证明缺口，完成 gofmt/定向 Go 回归和真实 SQLite/MySQL/PostgreSQL 主/日志库矩阵；当前不应直接构建成已修复 RC。通过后再分别取得提交/推送/新 RC 构建授权，核验新完整身份并重固定 adapter；注册与一次真实演练仍另批控制 ref、runner、窗口和合成 DB 写入范围。当前停于无法可靠证明迁移安全、缺少验证环境及外部操作授权边界，**PG03 仍 BLOCKED，不自动进入生产部署**。


## 2026-10-21 — PG03 独立隔离工作流本地实现完成；真实演练仍 BLOCKED（当前）

本节更新当前主线；历史状态和工作区已有修改保留。依据负责人本轮明确授权，已按既有设计与发布链审查完成本地实现和静态验证，详见 `evidence/pg03-isolated-rehearsal-local-implementation-20261021.md`。

- **本地实物**：新增 `.github/workflows/pg03-isolated-rehearsal.yml` 与固定 RC/认证/证据适配脚本；复用已有 successor 双启动入口，扩展 schema helper 和集中测试，未修改任何既有 workflow。本轮开始时 9 份现有 workflow 的字节哈希全部保持一致（包括 ci.yml 已有修改）。
- **固定边界**：仅手动、同仓库 main/owner、独立任务名与并发组；contents:read/packages:read，actions:read 仅下载固定 RC。checkout/upload 使用完整 SHA，无 production Environment/仓库 Secrets/SSH/部署调用。候选固定为 `ghcr.io/a3225865526-wq/clearapi@sha256:07fd58272b4fc62bcc52a3ad1d20e0dac357ba93de9e91a6a797d5e8fe3e8e7a`，源码 `69a66431646b5cc8912bf97a739e08de07316443`，RC `39357150402/1`。
- **未来执行契约**：原始 RC ZIP 外部 hash 与身份先验证；精确拉取后清除临时认证，再核验 RepoDigest/平台/OCI/image ID/二进制。独立 PostgreSQL 17.6 的无外联 tmpfs 数据库连续供两个新 Gateway 使用，分别记录启动前后 schema、健康、命令与最终进程退出。异常/清理失败不发布成功凭证；always 复核 nonce 所有权清理，仅上传脱敏白名单，7 天保留。
- **实际验证**：离线定向 31 tests OK（无 skip）；10 份 YAML 唯一键解析、权限/触发图断言、4 份 Python AST、新工作流 7 个 shell run 块 Git Bash -n、UTF-8/空白及差异检查通过。actionlint/shellcheck 未安装、未运行；未安装依赖。测试替身不代表真实启动或迁移。
- **真实状态**：本轮未读取/拉取候选、未启动容器或数据库。镜像读取 NOT RUN，第一次/第二次启动均 NOT RUN，真实 schema UNKNOWN，无真实成功 receipt。schema 是固定 RC 目标 DDL，不是新鲜生产快照；PG03 与生产验收仍 BLOCKED。
- **未执行边界**：未提交/推送/PR/合并/注册/触发 Actions，未改自动开关、Secrets、保护、包权限、依赖或生产发布配置；未执行生产 SSH、部署、重启或数据库写入。本地共享 schema helper 的新 hash 仅属于本地控制实现，不冒充旧 RC/生产已安装工具。

最小下一步：负责人审阅本地 8 文件差异。默认分支注册和一次真实运行仍须分别明确批准；注册前重新核查两项开关仍 false、当前触发图/并发与 required checks，保留正常 CI 和保护；运行再固定控制 ref/窗口及测试 runner 写入范围。此轮完成后停止，等待审阅。


## 2026-10-21 — PG03 Actions安全接入只读审查：总体BLOCKED，静态安全路径已确认（当前）

详见 `evidence/pg03-actions-safe-integration-review-20261021.md`；以下历史记录原样保留。本轮唯一任务为发布链审查和文档更新，未实施新workflow。

- **关键新增事实**：仓库默认main仍为69a66431646b5cc8912bf97a739e08de07316443；实际API读取 `CLEARAPI_AUTO_RC_ENABLED=false`（updated_at 2026-10-19T15:19:02Z）和 `CLEARAPI_AUTO_PRODUCTION_ENABLED=false`（2026-10-19T14:16:55Z）。此前“开关未核实”在当前快照中已解除；无需临时关开关、改发布权限或申请PAT。包Actions Admin沿用负责人确认，本地CLI scope不代表Actions拒绝。
- **完整配置覆盖**：固定远端9份workflow及唯一job级reusable引用automatic-deploy→deploy-production均检查。main push/PR merge→CI→专用PG03 RC→Deployment rehearsal→Automatic production release→reusable deploy；监听completed与job真正执行分开。CI无路径过滤，新增workflow注册仍触发正常CI；当前false使自动RC/生产job跳过，可能留下skipped后继记录。新独立名称任务完成不匹配任何现有workflow_run消费者。
- **生产路径区分**：手动RC不受自动RC开关限制；手动Deployment rehearsal若成功可被自动生产caller消费（仍受生产开关）；直接手动Deploy Production、恢复和GHCR VPS入口不受两个开关统一控制，本轮均禁止调用。production Environment仅main分支策略，无required reviewers/timer，不能当成人工审批屏障。
- **安全注册建议**：审批后的新路径为 `.github/workflows/pg03-isolated-rehearsal.yml`（替代先前方案路径），仅手动、独立name，显式contents:read/packages:read及固定RC下载所需actions:read。正常PR保留main严格三项required checks和管理员保护，两个现有false状态保持不变；合并前复核触发图/开关/并发约束，合并后只观察CI和skipped后继，再另批一次独立运行。不绕过默认分支注册，不skip-ci，不擅改权限或生产配置。
- **当前判定**：触发图/过滤/当前开关只读核查PASS；保持当前设置的条件安全路径PASS。具体workflow尚未实现/审阅，PR注册及运行未获批，因此整体接入与PG03仍BLOCKED。只有负责人后续批准的实物差异和执行窗口才能推进；不能把当前快照视为永久安全保证。
- **执行事实**：本轮仅新增审查报告、追加阶段报告和更新本总控。没有workflow/脚本变更、提交/推送/PR合并、Actions触发、权限/Secrets/保护变更、生产SSH/部署/服务重启或数据库操作。镜像读取NOT RUN，两次启动均NOT RUN，schema UNKNOWN，无新成功receipt。

最小下一步：负责人审阅报告后，仅批准独立任务的本地实现和定向验证；PR发布/注册和一次运行继续分别待批。本轮收尾后停止。


## 2026-10-20 23:58 — PG03 同仓库Actions隔离演练方案：BLOCKED / 待批（当前）

负责人确认包Manage Actions access已列Clear-API且为Admin；此前Actions包授权UNKNOWN的假设由此更新。本地CLI缺read:packages不代表Actions缺权；不再以该scope为同仓库job不可用的结论，也不申请PAT或扩大包ACL。

- **静态检查完成**：9个远端workflow固定源码均只读核查；发布job保留packages:write，新任务拟contents:read/packages:read，另加仅下载固定RC所需actions:read。现有生产SSH入口不调用；独立名称不会匹配现有workflow_run消费者。main仍69a66431646b5cc8912bf97a739e08de07316443，RC39357150402/1成功。
- **推荐方案**：独立手动 `PG03 isolated exact-image schema rehearsal`，GitHub hosted ubuntu-24.04，一次固定digest读取；临时认证在启动测试前清理；固定RC目标DDL在无外联tmpfs PostgreSQL17.6中连续两次真实启动，逐次健康/schema/退出/清理留证。完整镜像继续固定sha256:07fd58272b4fc62bcc52a3ad1d20e0dac357ba93de9e91a6a797d5e8fe3e8e7a；历史目标schema不称新鲜生产快照。
- **发布风险已明确**：successor工具/集中测试仍仅本地，不能checkout旧候选源码假定工具存在。新手动workflow需默认分支注册；发布main可能触发CI→RC→演练→自动生产链，开关未核实，不准直接推送/注册。实施、发布、运行须分别获批，保持必需checks，不擅改开关或发布权限。
- **文件差异与验证**：本轮仅新增 `evidence/pg03-actions-isolated-rehearsal-design-20261020T155803Z.md`、更新本总控及阶段记录；工作流/脚本差异为该方案内拟议清单，尚未实施。9份本地YAML解析和触发链检查通过；actionlint未安装且未运行，新工作流和定向回归未运行。
- **真实状态**：Actions读取未执行，未取得镜像，第一次/第二次启动均NOT RUN，schema结果UNKNOWN；无新成功receipt。不提交、不推送、不dispatch，不改ACL/PAT/现有发布权限，不连接生产、不部署、不重启生产服务、不写生产数据库。

下一步限于审阅该具体方案和批准后续本地实现范围；安全默认分支注册与一次运行仍需各自明确授权。**PG03隔离演练与整体状态仍BLOCKED，不能报告PASS。**


## 2026-10-20 23:46 — PG03 精确镜像只读交接待批：BLOCKED（当前）

本节为本轮最新状态；以下历史记录及全部既有修改保留。唯一候选仍为 `ghcr.io/a3225865526-wq/clearapi@sha256:07fd58272b4fc62bcc52a3ad1d20e0dac357ba93de9e91a6a797d5e8fe3e8e7a`，源码 `69a66431646b5cc8912bf97a739e08de07316443`。

- **限定核查结论**：未发现同时具备批准、访问证明及精确镜像交接能力的只读隔离环境；并非断言所有外部环境不存在。本地只有named-pipe Docker contexts，desktop-linux中精确镜像仍不存在；仓库Environment列表仅production。最近10条run均completed，in_progress/queued各为0；RC现存两项artifact仍仅小型证据归档和build record，没有镜像归档。均为只读查询，不触发workflow。
- **沿用权限阻塞**：CLI缺少read:packages及GHCR拒绝为上轮已确认结果；本轮未重复被拒绝的包接口/拉取，不修改凭据，不推断当前包ACL或Actions授权。现有GHCR Pull Rehearsal涉及生产SSH，Deployment rehearsal可能接自动生产链，均不转用。
- **具体待批事项**：`PG03-PACKAGE-READ-20261020-R1` 已准备，详见 `evidence/pg03-package-read-approval-20261020T154609Z.md`。限定上述仓库/完整digest、管理员指定的非生产隔离读取环境、一次只读获取及经批准交接到本地desktop-linux；需填写执行人、环境ID、UTC窗口和批准ref。最低为有效包Read及适用的read:packages（或另批独立job的packages:read），无包Write/Admin要求；先核对已有授权，不自动改ACL或创建凭据。scope/包ACL不能声称限到单digest，由执行和交接校验落实。
- **真实验收**：镜像尚未取得；第一次启动NOT RUN、第二次启动NOT RUN，退出码均N/A；schema幂等性UNKNOWN，无新成功receipt。只有获准取得并独立核验精确镜像后，才进入已有真实目标DDL的连续两次隔离启动；历史schema不冒充新鲜生产观察。
- **本轮边界与验证**：仅更新本地总控和阶段证据，未执行生产SSH、workflow dispatch/rerun、生产部署、任何数据库写入或容器启动。历史19/19与baseline PASS未重跑。原文按字节保留，进行文档/哈希及差异检查；本轮不宣称PG03通过。

唯一下一步：管理员审阅上述具体交接事项并提供有效批准与可校验镜像交接；在此之前 **PG03 = BLOCKED**，生产及其他既有门禁不变。本事项尚未向外部发送，不是执行许可。


## 2026-10-20 23:37 — PG03 精确候选读取权限定位：BLOCKED（本轮最新）

本节更新当前下一步，保留以下全部历史记录。本轮只做本地/GitHub只读核验和本地证据更新；没有拉取重试、登录/凭据或权限变更、workflow dispatch/rerun、SSH、数据库/容器启动、生产写入、提交、推送或部署。

- **精确候选已核实**：`ghcr.io/a3225865526-wq/clearapi@sha256:07fd58272b4fc62bcc52a3ad1d20e0dac357ba93de9e91a6a797d5e8fe3e8e7a`；源码 `69a66431646b5cc8912bf97a739e08de07316443`；RC `39357150402/1` API为completed/success。归档 `12235966400` 未过期，API/本地SHA256均为 `ac7c42fc5a3549773f2c59080b503d6d5201f7442c9c20a955eb542f97ffb09b`；本轮重新运行baseline inspector退出0，四项payload/目标schema校验PASS。清单仍为production_ready=false / production_acceptance=not_run。
- **读取阻塞已精确定位**：现有CLI身份 `a3225865526-wq` 的响应头仍仅 `gist, read:org, repo, workflow`，无 `read:packages`；包接口退出1并明确返回 `You need at least read:packages scope to get a package.`。仓库admin/pull不能替代包scope。包visibility/ACL及当前Actions包授权仍UNKNOWN；未假定Docker使用同一凭据。本地Docker可用，精确候选inspect退出1（No such image），固定PostgreSQL已缓存；未重复历史失败拉取。
- **已有Actions入口不能直接转用**：核对四份固定源码workflow与本地一致（忽略CRLF）。GHCR Pull Rehearsal虽然packages:read，但pull路径进入production并使用SSH/sudo；Deployment rehearsal不是本地successor两次真实启动入口，其成功还可能触发自动生产链。未触发任何workflow或获取其短期token，没有已核实可直接使用的额外隔离包读取环境。
- **真实演练状态**：镜像实物身份UNKNOWN；第一次启动NOT RUN，第二次启动NOT RUN，退出码均不适用；隔离schema/迁移幂等性UNKNOWN，无隔离实例或成功receipt。目标schema `3c1c77f3…` 来自已核实RC目标及历史证据，不能声称是本轮新鲜生产观察。PG03仍不具备进入下一阶段的已验证条件。
- **最小待批事项**：优先由已有获准包读取环境提供/预载上述精确镜像；若没有，管理员另批隔离的只读包读取运行环境（packages:read及该包有效Read权限，先核对现有包授权）与必要的镜像交接。任何新workflow/dispatch或本地read:packages授权变更均未执行，不创建长期凭据，不转用生产SSH。源码/镜像读取完成后才运行已有目标schema两次启动入口。
- **证据与剩余门禁**：详见 `evidence/pg03-exact-image-access-20261020T153750Z.md` 和原阶段报告新增附录。生产执行授权、完整successor执行器、停止写入方备份/恢复与启动写入范围、新候选连接/路由/账务验收及新鲜生产schema仍待分别核实；本轮没有PG03或上线PASS。


## 2026-10-20 23:31 — 最新 PG03 本地修复与验收准备（当前主线）

本节取代下方历史记录中的当前目标与下一步：**推进最新版 PG03 验收和正式上线；不以恢复10月17日旧版或仅恢复事故同一容器为最终目标。** 保留原 failed 清单、隔离记录、全部既有工作区改动及旧正式镜像回退引用。本轮没有 commit/push/dispatch 或任何生产写入。

- **已核实候选（GitHub只读）**：当前 main `69a66431646b5cc8912bf97a739e08de07316443`；最新成功 RC `39357150402/1`；候选 digest `sha256:07fd58272b4fc62bcc52a3ad1d20e0dac357ba93de9e91a6a797d5e8fe3e8e7a`。归档 `12235966400` 实测 SHA256 `ac7c42fc5a3549773f2c59080b503d6d5201f7442c9c20a955eb542f97ffb09b`，四项payload哈希及基线输入校验通过。清单仍为 `production_ready=false` / `production_acceptance=not_run`，不是最终生产选定版本。
- **阶段一结论（复用已有证据）**：缺失目录根因是易失 `/run/clearapi` 无开机创建/排序；现有 boot/identity 实现与哈希未改，已有12项boot验证及安装审查沿用、不重跑。生产安装、实际配置/挂载和新实例身份仍未现场复验，不能标记阶段验收PASS。
- **阶段二结论**：原 Wget管道配置导致认证请求发送前失败的修复已存在；现有恢复入口仍只恢复事故原容器，普通发布拒绝failed前驱/hold。生产 Gateway→Executor 认证仍未验证，Executor healthy不等于连接通过。完整successor替换/授权消费/新清单执行链尚未实现，不声称已解除PG03阻塞。
- **新增有效发现**：最新RC schema演练是旧 `578bc6f8…` → `3c1c77f3…`；事故后历史数据库已经是后者。该演练不能替代“当前schema → 当前schema”的两次启动证明；本轮未取得当前生产schema/数据投影，不能由preflight-only推断无写入。
- **本地增量**：用户已批准本地successor设计/实现。新增 `scripts/pg03-successor-plan.py`，实现失败前驱/候选/RC/挂载/隔离/完整receipt离线契约与“固定RC目标schema上再启动两次”的独立演练入口；复用现有schema helper和gate清单。结构匹配永不授予生产许可；缺少生产事实不伪造观察。它是验收准备工具，**没有生产执行器或特权入口**。新增集中测试 `scripts/ci/test-pg03-successor-plan.py`，在已有修改的CI中只增加该测试一行，其他既有修改保留。
- **实际验证**：最终19/19新增定向测试通过；真实RC ZIP/四项payload/基线输入校验通过；Python AST、新文件空白、UTF-8 CI YAML接线检查和 `git diff --check` 通过。首次YAML检查因Windows默认GBK解码失败，显式UTF-8后通过。没有重跑已通过且未变的boot/全量CI/数据库矩阵，也没有把startup替身测试记为真实候选启动。
- **本地真实演练阻塞**：Docker 29.7.2/containerd可用、固定PostgreSQL已缓存；最新版候选镜像未在本地。精确digest拉取失败 `permission_denied: The token provided does not match expected scopes`；既有GitHub CLI响应头不含 `read:packages`。未读取/输出token值、修改凭据/权限或重试。真实候选启动/schema演练及新候选配对、route、billing/失败请求验收 **NOT RUN**；未生成成功演练receipt。
- **生产SSH BLOCKED**：默认既有身份连接在认证阶段返回 `Permission denied (publickey)`，远端命令未执行。用户随后明确选择保持BLOCKED、继续本地工作；未切换身份、绕过认证或修改SSH。当前boot安装、schema/待处理账务、备份可恢复性、容器和公网状态均无本轮新鲜验收证据。
- **执行计划边界**：详见 `evidence/pg03-latest-successor-local-20261020.md`。已列出只读资格→本地验收/执行器补齐→精确资产归档安装→受支持的停止写入方备份→独占后继尝试→仅Gateway替换和新身份→隔离验收/新receipt→另行Caddy开放的逐项写入/容器范围。当前backup/启动写入契约与执行器未完成，计划是**有条件草案，不是可执行审批清单**；不能直接用现有要求running identity的备份机制支持stopped Gateway。
- **未完成与状态**：未达到整体 `LOCAL FIX PASS`；`CANDIDATE ACCEPTANCE PASS` / `PRODUCTION DEPLOYMENT PASS` / `WEBSITE UPDATED — CORE ACCEPTANCE PASS` 均未达到。最终生产镜像/源码未批准；旧正式digest `sha256:51ddf6df27129df0fe7ab7205c1e8bef0da6e2ca9b7bb9ede93ab77c863d24a5` / source `eb67c5f21639716eed7489d32ca322c249e5fdf3` 仅作保留的回退参考。
- **唯一下一步**：通过已获准的包读取环境提供/预载精确候选镜像，执行准备好的当前schema两次真实隔离启动并保留补充证据；不扩大token权限或自行触发外部workflow。随后仍需候选验收、恢复获准只读现场渠道、确认备份/启动写入契约并补齐successor执行器；生产SSH及全部生产变更继续BLOCKED。

## 2026-10-20 Production Incident

- Previous production image:
  sha256:51ddf6df27129df0fe7ab7205c1e8bef0da6e2ca9b7bb9ede93ab77c863d24a5
- Candidate image:
  sha256:22ecaa468c784cd41ee2604dd8f3ac189960744516a508fc8c9cee3ee1724e02
- Candidate source commit:
  c50a53c1b89e121bcade7dac41d3f11e53a9832a
- Controlled deployment run: 39347012653 — FAILURE
- Automatic production release run: 39341639498 — SKIPPED
- Local Gateway health and post-deploy Runtime Gate: PASS
- PG03 Gateway-to-Executor health/authenticated connection: BLOCKED
- Public ingress: ISOLATED; public HTTPS acceptance NOT RUN
- Automatic rollback: NOT PERFORMED
- Post-reboot defect: /run/clearapi missing; Gateway exited 127
- Caddy: exited 0; restart policy no
- Current overall status: WEBSITE DOWN / PG03 BLOCKED
- Next action: inspect deployment audit and release manifest,
  then choose the official recovery path; repair boot-time runtime
  identity initialization without weakening identity validation.
- Guardrail: no blind redeploy, manual image replacement, or
  public ingress re-enable before recovery validation.

### READ-ONLY OBSERVER R2 — READY FOR APPROVAL（DESIGN ONLY）/ COLLECTION BLOCKED

本轮为方案审查与本地文档修订，**不是生产操作授权**。未连接VPS、未运行或修改helper、未创建生产用户/key/SSH/sudoers/状态文件、未恢复采集；现有管理员登录能力保持原边界。

- **当前设计**：现行 `CLEARAPI_READONLY_IDENTITY_ADMIN_CHECKLIST.md` 第3节推荐A：单独300秒授权中，helper最多启动1次、合法采集最多1次；不保证成功一次。nonce仅关联响应，无重放保护；key expiry和MaxSessions不能提供调用总次数限制。B被明确区分为未选择的3次/1次/300秒方案，同样需服务端持久门禁，不可仅靠客户端或人工撤key。
- **P0处理**：拟新增独立root gate；固定持久目录、稳定inode非阻塞锁、O_EXCL claim及文件/父目录fsync先于任何helper启动。claim存在即已用（含损坏/失败/超时/UNKNOWN/断连/崩溃），不可退款、重置或通过回退重新激活；observer仅sudo无参gate，不能保留直接sudo helper旁路。全部新代码/哈希审阅/状态记账写入均尚未实现、未批准。
- **验证分类**：P类只在获批管理员渠道本地做元数据、语法、有效投影及sudo -l权限查询，不执行gate/helper；E类为真实认证/会话/命令/subsystem/TTY等可能进入门禁的测试。ForceCommand会替换原请求；负向测试也可能消耗额度，不是免费测试。A仅提议一个正常生产请求，其余优先隔离fixture或单独新授权。
- **四层证明分离**：文件语法、磁盘有效配置、运行daemon加载、真实认证授权端到端分别列PASS/FAIL/UNKNOWN，不能互相替代。全版本/启动参数/Include/Match/key与CA/动态授权/sudo外部策略/组/间接提权/执行链安全仍待现场核验。
- **备份/回退设计**：拟用 `/var/backups/clearapi-observer/observer-isolation-r2-20261020-01/`（root:root/0700，文件0600），保留原字节/身份/ACL等、外部seal及恢复演练；未创建。保留独立管理员会话/控制台；仅恢复本事务且身份仍匹配的对象，不覆盖并发变更，不恢复旧ARMED状态；clearadmin NOPASSWD: ALL收敛单独审批。
- **现有现场事实**：仍仅引用负责人确认的2026-10-20 21:53左右Asia/Shanghai证据，详见下方R1原文；本轮无新增现场结果。本地helper哈希继续固定 `b36f2e99c29cd85e1eec4baeee75426f09f931dc4f20907b56e71e44ef3c451e`。没有把设计或已有全局查询改记为实际链PASS。
- **历史与审查**：原方案原始字节保存于 `evidence/readonly-observer-identity-plan-v1-20261020.md`，SHA256 `89d5cac0981117b6cad2c34de673a89a73b2bb31bcda9fd98ce53adbaea382bf`；新增 `evidence/readonly-observer-review-20261020-r2.md`。现行VPS方案替代R1直接sudo helper链；下方历史记录及其余已有修改保留。
- **未解决的执行阻塞**：A/B未获负责人选择批准；gate及集中测试未实现、无真实gate哈希；原observer元数据仍绑定直接sudo helper，需未来独立适配审查；管理员现行渠道、现场版本/完整策略/持久化与恢复能力、L1-L4证明均未取得。A也不能支持原60秒连续采样，因此没有恢复原观察/归档流程的资格。
- **下一步仅审批**：负责人审阅R2设计，决定是否单独批准本地gate实现/测试/接线适配；之后再分别审批管理员预检、备份、精确生产安装/配置/reload/回退、UNARMED到带真实T0/T1的激活及一次E2E请求。设计批准不等于任何生产批准，旧clearadmin一次授权不转用。
- **本轮本地检查**：原方案历史副本哈希一致；原第1/2节逐字符保留；移除R2新增总控前缀后重建的原总控哈希仍为 `d3e1eb503cee18590ae1a834a2cd019301e9e491ae199669dbf119886e697bc5`；helper/reader/observer三份代码哈希不变。方案no-index差异检查已完成（返回1表示有差异、无空白报错）；总控 `git diff --check` 退出0，四份Markdown空白/围栏检查PASS。没有运行gate测试或生产验证。

**设计P0已有明确可验证规格，故标记READY FOR APPROVAL — DESIGN ONLY；不是READY FOR EXECUTION。READ-ONLY COLLECTION仍BLOCKED，helper本轮调用0、生产变更0、60秒观察未启动。已完成本地差异/哈希检查，停止并等待负责人审阅。**

### R1历史方案（以下原文保留；现行方案以R2为准）

### READ-ONLY COLLECTION — BLOCKED / OBSERVER 权限隔离方案待负责人批准

**当前任务仅制定方案并更新本总控；不得连接生产、执行 helper 或恢复采集。** 本节取代下方历史记录的当前事实判断与下一步安排，不删除历史证据，不扩大或续期历史授权。

现场证据基准：**2026-10-20 21:53 左右 Asia/Shanghai（13:53 左右 UTC）**。来源为负责人在本次《CLEARAPI — READ-ONLY OBSERVER 权限隔离方案设计》请求中明确提供的已确认现场审计结果；本轮仅核对本地源码和资料，**没有重新连接 VPS 或独立复测现场**，不虚构精确秒数、原始回执或审计签名。

| 项目 | 最新已核实事实或尚未证明项 | 结论 |
| --- | --- | --- |
| 目标与 helper | `103.42.181.219`；`/usr/local/libexec/clearapi-readonly-state` 存在，root:root，0755，现场 SHA256 与 `b36f2e99c29cd85e1eec4baeee75426f09f931dc4f20907b56e71e44ef3c451e` 一致 | 已核实（负责人提供的现场证据） |
| 专用 observer 身份 | `clearapi-observer` 用户不存在 | 已核实；专用身份前置条件不满足 |
| SSH 全局查询 | `forcecommand none`、`allowtcpforwarding yes`、`allowagentforwarding yes`、`x11forwarding yes`、`permittty yes` | 已核实；不能代替特定用户/来源 Match、公钥选项及运行 daemon 的生效证明 |
| 管理身份 sudo | `clearadmin` 存在 `NOPASSWD: ALL` sudoers 规则 | 已核实；不能用该身份冒充最小只读身份，也不能仅加窄规则声称宽授权已撤销 |
| 专用受限通道及完整有效权限 | 用户/组与所有 sudo 策略来源、SSH Include/Match/密钥/CA 来源、环境与运行配置尚未完成完整核验 | UNKNOWN / NOT QUALIFIED |
| helper 与生产采集 | helper 尚未执行；生产采集尚未开始 | BLOCKED；不自动恢复 |

- **本轮本地核对 PASS**：完整静态读取 `scripts/recovery-tool-vps-helper.py`，实测 SHA256 与上述现场/预期值一致；核对现有 `scripts/recovery-tool-vps-reader.py`、`scripts/recovery-tool-observer.py` 和管理员清单。helper 的 shebang 是 `/usr/bin/python3 -IB`，入口要求有效 UID 0、`len(sys.argv)==1`；stdin 只接受 nonce 请求，子命令为固定路径/参数且 `shell=False`。既有 reader 拒绝 root/clearadmin/clearapi-deploy，绑定 clearapi-observer；现有元数据固定强制命令，不能通过修改接线绕过权限门禁。
- **最小待批方案**：新增独立无附加特权组用户及 root 拥有且不可由 observer 写入的 home；唯一短期专用公钥使用 `restrict`、UTC expiry 和固定 command；用户级 SSH Match 同时固定命令、禁用转发/TTY/用户 rc/其他认证来源。固定命令仍为 `/usr/bin/sudo -n -- /usr/local/libexec/clearapi-readonly-state`。sudo 仅 `(root)`、`NOPASSWD`、`NOSETENV`、绝对 helper 路径且末尾明确 `""`，核验完整有效规则和环境；不授权通用 Python、shell、docker 或 systemctl，不授予 docker 组。已匹配 helper 只复核，不重复安装、覆盖或改模式；0755 也是现有本地资格门禁要求。
- **sudo 语义已核对**：官方 sudoers 文档和上游 `command_args_match` 均明确：只写路径且不写参数约束会允许任意参数；末尾唯一 `""` 才匹配无参数。窄规则不会普遍抵消其他适用规则，须审查 include 顺序、别名、组和外部策略并用 `sudo -l -U` 做正反向查询，不能仅以单文件 `visudo` 成功放行。参考 https://raw.githubusercontent.com/sudo-project/sudo/main/plugins/sudoers/match_command.c 及 https://manpages.debian.org/bookworm/sudo/sudoers.5.en.html 。生产安装版本仍待批准后核验。
- **root 兼容性与残留风险**：保留 root 执行是为读取受保护的 proc/cron/部署锁及固定 Docker socket 投影；NOEXEC 会阻断必需子进程，不作为默认隔离方案。SSH ForceCommand 会替换客户端 shell/参数/subsystem 请求，而不是保证拒绝请求或不执行 helper；禁止把试连、错误命令或拒绝测试当作零 helper 调用。公钥 expiry 只约束新认证，MaxSessions=1 仅限制每连接，nonce 不是服务端一次调用授权；既有通用 root 管理通道仍属可信管理边界。helper 没有显式业务写入，不保证操作系统 atime、SSH/sudo 审计等零写入。
- **实施与回退门禁（均未执行）**：负责人分别批准管理员只读预检、受保护备份、精确用户/公钥/sudoers/SSH 配置写入、必要 SSH reload 及有界回退；保留既有管理员连接并确认控制台/第二管理员连接。先备份实际 Include 图及目标 absent 状态，核对恢复能力；先完成静态语法/完整策略检查，最后激活新密钥和配置。失败先拒绝 observer 新连接并撤销其专用 sudo 权限，再按对象身份恢复本事务文件并检查/reload；不解除限制作为补救，不删除未知资产，不改 clearadmin 宽权限或业务服务。活跃 observer 会话终止须在回退批准范围内。
- **后续验证及非秘密证明（未运行）**：用户 UID/组/密码状态、helper 哈希/类型/链接/父目录/ACL、完整 sudo 策略来源及允许/拒绝查询回执、具体来源上下文 SSH Match 投影、唯一公钥指纹/选项/期限、可信 host key、生效 daemon/启动参数/reload 记录、配置备份与变更哈希、审阅人/UTC/退出码。真实认证、禁用能力测试及任何 helper 调用另行限定次数/输入/时间审批；静态查询不代替端到端证明。不得提交私钥、凭据值、完整公钥正文或原始敏感日志。
- **本轮验证边界**：只做本地源码哈希、文档/调用链静态核对和总控差异/空白检查；未运行 helper、测试、CI、RC、演练或生产检查。认证/授权设计参考 OWASP ASVS 5.0.0 的 8.1.1、8.2.1、8.3.1、16.2.1/2/5、16.3.1/2、16.5.3 及 Authentication / Session Management Cheat Sheets；仅设计对照，未完成生产控制验证，不声明合规。

**唯一下一步：负责人审阅本次七项权限隔离方案并明确批准具体后续操作。方案交付后停止；READ-ONLY COLLECTION 保持 BLOCKED，60秒观察未启动，归档/升级/恢复/部署/公网操作均未获本轮授权。** 原 clearadmin 一次只读授权不能转用于新 observer、验证批次或配置变更，也不得因本节记录自动续跑。

### 历史记录：2026-10-20 21:43 — READ-ONLY COLLECTION BLOCKED

- 审计时间：2026-10-20 21:43:21 Asia/Shanghai
- 授权有效期：2026-10-21 21:39:17 Asia/Shanghai
- 本地源码 SHA256：PASS
- 生产配置检查：七项均 UNKNOWN
- 生产连接：未执行
- Helper 调用：0
- 生产变更：0
- 证据报告：`evidence/readonly-collection-blocker-evidence-20261020T134321Z.md`

阻塞原因：缺少经过核实的管理员审计渠道及当前生产权限证据。历史权限记录不能替代现场核验。

唯一下一步：获取带时间戳的脱敏管理员审计证据，覆盖 helper 文件身份、SSH 强制命令实际生效情况及完整有效 sudo 权限。

禁止自动恢复采集、启动 helper、执行归档或扩大授权。授权到期后不得继续使用本次授权。

### 2026-10-20 — 精确版本一次性只读授权已取得；执行前门禁 BLOCKED（未连接生产）

**APPROVED — LIMITED READ-ONLY COLLECTION / READ-ONLY COLLECTION: BLOCKED / helper 调用 0 次。** 来源为本次 ChatGPT 对话中负责人身份用户的明确授权；范围仅 `clearadmin@103.42.181.219:22` 通过已核实受限通道无额外参数调用 `/usr/local/libexec/clearapi-readonly-state` 一次。批准源码 SHA256 `b36f2e99c29cd85e1eec4baeee75426f09f931dc4f20907b56e71e44ef3c451e`，本轮本地实测一致。首次实测记录签发时间为 **2026-10-20 21:39:17 Asia/Shanghai（13:39:17 UTC）**，到期 **2026-10-21 21:39:17 Asia/Shanghai（13:39:17 UTC）**；不推断消息原始发送时间，不追认历史安装/执行。

- 完整授权边界、时间证据、逐项门禁及执行记录见 `evidence/readonly-collection-authorization-execution-20261020T133917Z.md`。已完整静态读取本地 helper：未发现显式生产写入，固定只读命令、root 无参数入口、4秒总期限及256KiB成功输出限制存在；不替代远端版本和实际权限核验。
- **停止原因**：已有 `evidence/recovery-tools-vps-entry-audit-20261020T082550Z.json` 第130、157–163、199行记录历史 clearadmin 通用sudo、key无forced command及sshd forcecommand=none；旧SSH审计记载 `NOPASSWD: ALL`。没有当前证据证明这些限制已收紧，不能声称当前实测失败或通过。既有 `SshTransport` 还明确拒绝 clearadmin；未修改或绕过该限制。
- **实际结果**：未连接VPS、未取得远端helper SHA256/属主/权限，未现场核验SSH/sudoers，未发送nonce或运行helper，采集执行时间不适用。没有生产输出；本授权下采集未开始。仅新增本地报告并更新本节，保留已有修改；未运行测试/CI/RC/演练，无安装、服务/容器、数据库、部署、配置、监控、60秒预观察或归档操作。
- **唯一下一步**：管理员提供针对clearadmin的当前非秘密文件身份/哈希及受限SSH/完整有效sudo门禁证明；需要改变身份、通道或生产配置时先单独审批。保持暂停，不自动续跑。下面旧记录中的授权UNKNOWN、候选未安装及身份方案是历史记录，不代表本次已确认生产安装或满足执行门禁；原有观察/归档/恢复门禁继续保持。

### 2026-10-20 — 管理员身份配置清单已交付（未执行）

新增本地 `CLEARAPI_READONLY_IDENTITY_ADMIN_CHECKLIST.md`：列明单仓库GitHub App只读权限/受限签发及非秘密证明G、VPS专用用户/helper/无参sudo/SSH限制的配置与静态验证命令及证明V。新核对的语法要点：sudoers末尾 `""` 禁止附加参数；`PermitUserEnvironment`只能核对全局有效设置，不能放入`Match User`；`sshd -T`不能单独证明运行daemon已加载新配置。所有管理员操作及可能需要的SSH配置生效操作均待独立授权，本会话未执行。未修改采集器、未运行测试/CI/RC/演练、未读取真实凭据或连接生产；无新增生产资格证据，**LIVE QUALIFICATION UNKNOWN / 60秒观察未启动 / 归档NO-GO** 不变。下一步仅由管理员交回清单G/V证明，再由负责人另行授权一次生产只读身份资格核验。

### 2026-10-20 — 受限身份采集接线完成；生产资格仍待管理员证据

**LOCAL BINDING PASS（隔离验证） / LIVE QUALIFICATION UNKNOWN / 60秒预观察 NOT STARTED / 生产归档 NO-GO。** 负责人已选择专用受限身份，不再使用宽权限 `gh` 或 `clearadmin` 作为只读采集身份。本轮只修改本地采集接线/测试与本总控，保留其余未提交工作；未连接目标仓库 API 或 VPS、未加载真实凭据、未创建/轮换凭据或修改生产文件，未开始观察或归档。仅查询了公开官方权限/安全文档。

- **本地接线**：新增 `scripts/recovery-tool-observer.py`，提供 `build_driver(config, approval, window_module=UPGRADE.W, ...)`，绑定升级器所用的同一个门禁模块/类；固定 source/repository、代码SHA、两个已审阅inventory指纹、VPS运行身份指纹、四工具哈希及管理员权限/有效期声明。默认无凭据加载、无transport启动、无观察或归档调用；未来真实接线必须显式 `enable_live=True` 且匹配独立的只读授权引用。每次HTTP/SSH调用前后均重新检查凭据租期，过期即不再发送后续请求；整个窗口必须落在两个身份有效期内。权限元数据是待管理员核验的声明，不是程序已经证明了生产实际权限。
- **GitHub 实际覆盖**：固定目标仓库的GET端点；全refs六类Actions队列、非空run的jobs/pending deployments、独立deployments及其status完整分页、main ref、自动发布变量、workflow列表。根据实时读取的repository/main/auto/workflow(id,path,state)规范JSON重新计算指纹，采集前后重读配置拒绝期间漂移，不再回传静态预期值伪装为实时指纹。分页重复/不完整、未知部署状态、权限拒绝、超时、断线或畸形数据均UNKNOWN；活动队列/部署、main/auto/指纹不符均FAIL。令牌只发往固定仓库的GET白名单；禁重定向/继承代理，响应上限2MiB，单请求≤5秒、采集预算≤25秒，迟到结果拒绝。Socket读取使用单次底层读取并检查期限；既有门禁另以30秒TTL关闭失效源，不宣称可即时取消内核调用。当前main提交标识绑定其完整源码；其他ref/特权/外部事件仍需负责人冻结承诺，不能将采样覆盖写成技术上排除所有未来调用。
- **VPS 实际覆盖**：新增本地候选 `scripts/recovery-tool-vps-helper.py`（尚未安装），固定无参数入口，仅接受32位hex nonce；读取固定工具/配置目录清单的内容哈希及元数据、相关进程名称投影（不输出完整cmdline）、标准cron/at入口、systemd jobs、FLOCK的device/inode/PID、Docker已运行状态和四容器最小状态投影。Docker未运行即停止，不触发socket激活；不执行任何项目工具、服务启动、迁移、数据库或恢复命令。关键代码父目录必须root拥有且不可被组/其他用户写入；清单前后漂移/读取异常关闭采集。配置清单输出只有路径/哈希/元数据，不输出文件正文。helper总期限4秒、输出上限256KiB；超出覆盖数量/体积限制为UNKNOWN。
- **VPS transport/验证**：显式专用用户/密钥文件引用/known_hosts和SSH绝对路径；`-F none`、strict host key、IdentitiesOnly、无agent/proxy/复用/转发/TTY/口令认证，不继承用户SSH配置。固定远端命令 `/usr/local/libexec/clearapi-readonly-state`；有界stdout、超时kill并回收子进程，不暴露stderr。reader核对nonce、coverage_complete、重新计算inventory指纹、target与inventory的一致性及真实锁PID；接线再核对四项工具、已安装helper代码身份和服务/四容器运行身份基线。失联/重放/未知字段/到期/漂移使真实WindowDriver进入不可恢复关闭状态。未观察到的改名/已加载调用、控制台及外部凭据调用仍不是可排除项。

**管理员需提供或单独批准的最小身份与用途（只提供引用/非秘密证明，禁止聊天或日志提供值）：**

| 身份引用 | 范围与用途 | 有效期/约束 |
| --- | --- | --- |
| `CLEARAPI_OBSERVER_GITHUB_TOKEN`；建议专用App身份名 `clearapi-pg03-observer`（未创建） | 仅 `a3225865526-wq/Clear-API` 的GitHub App installation token。`Actions:read` 读取队列/workflows/jobs/pending deployments；`Deployments:read` 读取部署/status；现有完整门禁还必须有 `Contents:read` 读取main ref、`Variables:read` 读取 `CLEARAPI_AUTO_PRODUCTION_ENABLED`；`Metadata:read` 为基础只读权限。不给任何write、Secrets、Administration、其他仓库或组织权限。只有Actions/Deployments两项不能满足现有main/auto门禁，不自动扩大权限。 | 管理员在外部提供已签发token，通过仅本次进程环境注入上述引用；本程序不读取App私钥、不签发/刷新token。安装token官方期限1小时，代码拒绝超过1小时的租期；记录签发/到期UTC、单仓库及精确权限证明、审核人/引用，剩余期限须覆盖资格核验、60秒预观察及窗口结束。 |
| `CLEARAPI_OBSERVER_SSH`；专用 `clearapi-observer@103.42.181.219`（未创建/未现场确认） | 管理员提供专用identity_file及严格known_hosts的绝对路径引用，不提供私钥值。不加入docker组、不赋予通用sudo、不使用root/clearadmin/clearapi-deploy。root拥有且不可写的受限入口仅执行固定无参helper，专用账号禁止其他密钥、口令、TTY/转发/user rc及可写shell初始化/环境入口。 | SSH key或证书由管理员设置≤1小时的UTC到期，并记录non-secret权限审查引用；代码租期同样≤1小时且必须覆盖窗口。专用账号的强制命令为 `/usr/bin/sudo -n -- /usr/local/libexec/clearapi-readonly-state`；sudoers候选仅 `clearapi-observer ALL=(root) NOPASSWD: /usr/local/libexec/clearapi-readonly-state ""`（空参数限制，不含通配符）。`restrict`、`expiry-time`、force-command及有效sudoers/sshd/父目录状态须管理员核验，不凭本地声明认定实际生效。 |

如无现成受限入口，管理员须先另行批准专用账号/受限key配置、精确sudo规则及helper安装；本轮不执行这些外部写入。helper候选目标root:root/0755，其父目录不可由该账号写入。未来认证/查询可能产生系统正常SSH/sudo审计日志或读访问时间更新；固定探针不请求业务/配置/数据库写入。权限资料依据：[Actions](https://docs.github.com/en/rest/actions/workflow-runs#list-workflow-runs-for-a-repository)、[Deployments](https://docs.github.com/en/rest/deployments/statuses#list-deployment-statuses)、[main ref](https://docs.github.com/en/rest/git/refs#get-a-reference)、[Variables](https://docs.github.com/en/rest/actions/variables#get-a-repository-variable)、[安装token期限](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app)、[OpenSSH限制](https://man.openbsd.org/sshd.8)。已参考OWASP Authentication/Session Management的最小权限、凭据保密及期限要求；本轮未修改应用认证流程，不声称生产ASVS合规。

**本次验证（仅新增或受影响路径，未重跑既有CI/RC/恢复演练）：**

- Windows Python **3.12.10**：`python -B scripts/ci/test-recovery-tool-readers.py` **11/11 PASS**；随后仅新增 `CollectorTests.test_http_body_limit_and_deadline` **1/1 PASS**（最终12个不同方法分组通过，不声称最终全量suite重跑）。`python -B scripts/ci/test-recovery-tool-vps-reader.py` **12/12 PASS**。
- WSL Ubuntu/root临时目录：新 `scripts/ci/test-recovery-tool-observer.py` 首批9项为8 PASS / 1夹具错误（Caddy初始running已为true），修正夹具后定向接线用例PASS；新增SSH限时测试初因mock了全局Event.wait干扰Thread.start而ERROR，改为同步thread fixture后该项PASS。现有 **13个不同新方法均已分组通过**；只在helper父目录/HTTP读取/逐请求租期变更后重验直接受影响的接线、期限或相关新增用例。最后一组 `QualificationTests.test_expiry_during_read_closes_driver_without_retry`、`test_real_helper_contract_binds_both_readers_to_existing_window`、`test_default_has_no_credentials_transports_or_started_observation` **3/3 PASS**。所有新接线测试显式禁止真实socket/Popen；helper只使用临时目录、合成状态、fake commands，未启动真实观察线程或等待60秒。
- 七个变更/新增Python文件AST语法、空白检查PASS；静默门禁/升级器/boot安装器源码与原固定SHA保持一致，未重跑它们的25/19/31项。代码身份：observer=`c5933c1a15ab9612aea35baaec37fd7bb5b3d85d2d0e35f89a76ef3e6f91f27d`；GitHub reader=`544ee7efb53bbb387119422850484f1b2fefdfb4815c5e382e85928e730cb405`；VPS reader=`1228db9195355862a8a329a2ecb0ceeb2bbbe528e6a1d917a1c00b2f46ae0fbc`；helper=`b36f2e99c29cd85e1eec4baeee75426f09f931dc4f20907b56e71e44ef3c451e`。observer内部固定上述reader/helper及原window哈希；它们不是历史RC的发布资产。

**剩余硬阻塞 / 启动60秒预观察的最小前置：** (1) 管理员提供上述已审阅专用身份、非秘密权限/到期证明和远端helper实际安装/隔离证据；(2) 另行授权一次真实只读资格核验，取得并审核GitHub配置、VPS入口inventory/运行身份、四工具和helper的新鲜基线，填入 `clearapi.restricted-observer/v1` 配置（三个指纹、target_sha256及身份声明），不能首次自动信任旧JSON或用预期哈希替代实时指纹；(3) 两源真实连通、覆盖与时效全部PASS后，确认固定UTC起止≤15分钟、唯一执行者/PID、事务及全部入口冻结，且两种身份有效期覆盖窗口，才可授权启动60秒实时观察。任何FAIL/UNKNOWN继续拒绝。生产归档、工具替换、boot安装、Gateway/数据库/公网操作均仍不在本轮范围内；未取得这些前置条件前停止扩展实现。

### 2026-10-20 — 中断续接核对与 VPS reader 验证收尾（本地）

**当前结论：中断留下的 VPS 接口验证已收尾；生产采集接线未完成，预观察 NOT STARTED / 归档 NO-GO / 网站与 PG03 上线未完成。** 以下取代上一节笼统的 `LOCAL WORK COMPLETE`，不将历史在线快照或旧测试结果当作当前生产 PASS。本轮未连接 GitHub/VPS、读取秘密值、写入生产、提交或推送。

- **Git 现场**：当前分支 `codex/clearapi-failed-recovery-20261020`，HEAD `ff6e0f4b2f5096d12717a01afbb3f5d90d28b801`；三项已跟踪差异为本总控、`.github/workflows/ci.yml` 和 `scripts/ci/test-runtime-boot-recovery.py`，另有既有未跟踪脚本/证据/缓存，不能把“上一会话四文件”理解为整个工作区只有四项差异。无暂存差异，未重置、清理或覆盖已有工作。只在已有 VPS 测试中续写，并在本总控追加本节。本地 `production/main` ref 为 `c50a53c1b89e121bcade7dac41d3f11e53a9832a`，是本地缓存，不代表远端当前 main；RC/source `39357150402/1` / `69a66431646b5cc8912bf97a739e08de07316443` 仅沿用历史身份，不更新为新鲜核验。
- **确认中断缺口**：`scripts/recovery-tool-vps-reader.py` 已要求 `inventory_sha256`、锁 `pid` 并返回门禁所需的平面字段，但其测试尚未同步。定向命令 `python -B scripts/ci/test-recovery-tool-vps-reader.py ReaderTests.test_ok_and_fixed_command` 实际报 `ValueError: invalid_inventory_sha256`（1 ERROR）；因此之前 7/7 PASS 不覆盖中断后的源码。
- **本次修复与验证**：同步显式指纹/锁 PID fixture，扩展锁归属、缺失字段、指纹漂移、调用/进程/jobs 拒绝、迟到响应和真实 `QuietWindow.sample()` 接口验证。`python -B scripts/ci/test-recovery-tool-vps-reader.py` **11/11 PASS**，全部使用 fake transport / 合成批准和时钟，未启动 `WindowDriver` 或真实60秒观察。只重验中断后改变且旧 PASS 不再适用的 VPS 测试；GitHub 6项、门禁25项、原事务19/安装31项、CI、RC、演练与线上验收均未重跑。
- **文件身份**：保留的 VPS reader SHA256 `d2c44946295ca73d6962f7adbdab21374068d98fcfe182b8250298b4ec4adc8a`；本次测试 SHA256 `465278862a393a5d2a2031e4482d9379cfc8f133a9b965e42cf5c021591bdb59`。静默门禁 `284125307fca2814b84f659d0470c41d133612de9d400362f888908b69fc3973`、升级器 `77ba66604746da9bd7ee41783dc40cc21e3184608185c1decce534a9365f25ed`、boot 安装器 `6bf1c77e6ce1c162e22328239badbaf67b77bde21f100fbbcf3a5747812d01d5` 与既有审阅 pin 一致，无须重跑其既有测试。
- **仍阻止预观察的具体缺项**：没有生产 driver 将两个 reader 绑定到升级器导出的同一个 `W.QuietWindow`；VPS transport 未绑定，固定远端 `/usr/local/libexec/clearapi-readonly-state` 的实现、安装状态、字段覆盖和受限 SSH 身份均未验证；GitHub 显式 token 引用、读取范围和有效期未绑定。GitHub 当前 `inventory_sha256` 由调用方传入，不能据此声称实时检测了 workflow/config inventory 漂移；VPS helper 的 fingerprint 来源亦未核验。必须先审阅真实采集/指纹计算与接线，不能仅填一个预期哈希或读取历史 JSON 获得资格。继承交接中负责人对唯一执行者/不另行触发生产操作的承诺；固定 UTC 窗口及特权/外部凭据等入口覆盖仍须明确。当前无新生产信息可使这些 UNKNOWN 变为 PASS。
- **下一次最小确认（集中一次，不要求提供秘密值）**：明确真实只读采集路径及凭据引用、权限/有效期。可提供已有受限身份；若选择复用历史 `gh`/`clearadmin` 宽权限身份，须明确接受并另行审阅固定 GET/只读命令边界，不能称其为最小只读权限。任何新 helper 安装、SSH/sudo 限制或凭据配置都属于另行列明的外部写入；本轮未授权实施。接线资格确认后再固定不超过15分钟的归档窗口、全入口冻结、唯一执行者/PID与事务，执行60秒实时只读预观察。
- **预备归档边界（供后续精确清单使用，尚非可执行批准）**：只调用已固定升级器 `Upgrade.prepare(window)`，不调用 `apply`/`rollback` 或直接运行无 CLI 的脚本。需另行批准私有输入目录、`/run/lock/clearapi-production-deploy.lock`（0:0/0600）与新的 `/opt/clearapi/.tool-upgrade-rc39357150402-1-<唯一UTC尝试>`（0:0/0700）。按历史 OLD 基线，归档实际含 **9个文件**：四个 `backup-{production-deploy-v2.sh,pg03-release-control.py,pg03-legacy-transition.py,pg03-schema-rehearsal.py}`、两个 `stage-{pg03-release-control.py,production-deploy-v2.sh}`（0:0/0750）及 `baseline.json`、`baseline.sha256`、`journal.jsonl`；其余文件0:0/0600。输入目录的精确名称/内容/哈希、独立外部 seal 留存位置及现场 NOREPLACE/fsync 验证写入对象须在最终审批清单固定，不能遗漏或假称已执行。四项已安装工具均不在归档步骤中被替换。失败或失效时停止后续写入、保留部分归档和锁文件、释放持有FD，不自动删除、重试或继续工具替换。
- **剩余主线**：采集接线资格 → 新鲜只读预检/60秒观察 → 获批归档与独立 seal 核验 → 独立窗口下 control→wrapper 升级 → boot archive/install → 明确启动写入范围后恢复 Gateway → 最新合格版本部署与 Runtime/PG03 验收 → 独立批准 Caddy/公网恢复。Gateway 的迁移/后台写入仍 UNKNOWN；Provider Usage/Billing 按交接由负责人上线后验证，当前 NOT VERIFIED。可独立准备后续精确审批清单和启动只读投影；本轮不再扩展本地功能，不以历史快照解除任何生产门禁。

### 2026-10-20 — 最短路径盘点与本地采集器完成（生产未执行）

**当前结论：LOCAL WORK COMPLETE / PRODUCTION NO-GO / WEBSITE NOT RESTORED.** 本轮未连接生产、GitHub API 或读取凭据值；未创建生产锁/归档、替换工具、安装 boot 资产、启动服务、修改数据库/账务、解除隔离或开放公网。

- 已完成本地硬阻塞盘点：生产旧 Gateway 仍为历史 `Exited(127)`；RC `39357150402/1`、来源 `69a66431646b5cc8912bf97a739e08de07316443` 及七项 boot 来源链沿用已核验记录；生产实时状态不是当前在线证据。
- 已新增并隔离验证两个 fail-closed reader：GitHub `scripts/recovery-tool-readers.py`（显式 token 引用、GET-only、六类 Actions 队列、活动 run jobs、pending deployments、main ref、自动发布变量、分页/超时/权限/字段校验）；VPS `scripts/recovery-tool-vps-reader.py`（固定 SSH 到受限 helper，不使用 sudo，校验服务/容器、调用/进程、systemd jobs、部署锁和目标文件身份）。生产 transport/身份未绑定，故生产资格仍为 **UNKNOWN**。
- 受影响验证：`python scripts/ci/test-recovery-tool-readers.py` **6/6 PASS**；`python scripts/ci/test-recovery-tool-vps-reader.py` **7/7 PASS**；四个 reader/门禁模块 `py_compile` 通过。未重跑既有 19/31、CI、RC、恢复演练或线上验收。
- Gateway 启动副作用静态审查确认 `InitResources()` 先于 readiness 检查，可能执行主/日志库迁移、选项迁移、auth cleanup；后续可能启动 finance sync、订阅重置、credential refresh、task runner、billing protection 及 route-ledger recovery。有效开关、schema 差异、待处理 workload、外部调用和启动写入范围均未由生产只读证据确认，不能安全恢复 Gateway。

**依赖排序：** (1) 负责人批准并提供有界静默窗口、唯一执行者、全入口冻结和两个最小只读身份；(2) 只读预检及 60 秒观察；(3) 单独批准并执行工具四文件归档/外部 seal；(4) 单独批准 control→wrapper 工具升级；(5) 单独批准 boot archive/install；(6) 单独批准 Gateway 恢复并确认启动写入范围；(7) 最新合格 Gateway/Executor 部署与 Runtime/PG03 验收；(8) 单独批准 Caddy/公网恢复。任一关键条件为 `FAIL/UNKNOWN` 即停止对应生产写入。

### 2026-10-20 — Local constrained VPS reader adapter (no production binding)

- Added `scripts/recovery-tool-vps-reader.py` and isolated fake-transport tests at `scripts/ci/test-recovery-tool-vps-reader.py`. The adapter emits a fixed SSH invocation for a constrained remote helper, validates strict JSON and required state fields, and maps timeout/disconnect/denial/schema/UNKNOWN to fail-closed `UNKNOWN`. No production connection, credential lookup, or provisioning occurred; transport remains unbound, so production state is **UNKNOWN**.
- Local isolated fake-transport tests: 7/7 PASS (`python scripts/ci/test-recovery-tool-vps-reader.py`). This is not live-source qualification and does not change archive/upgrade authorization.

### 2026-10-20 — 实时只读采集源资格与负责人确认（仅本地核验）

**结论：两个生产实时采集源均未具备启动观察的资格；不连接生产/GitHub，不读秘密值，不开始60秒观察。** 本节是对现有采集实现/历史审计的核对，不是新鲜在线证据。静默窗口门禁文件 `scripts/recovery-tool-window.py` SHA256 `284125307fca2814b84f659d0470c41d133612de9d400362f888908b69fc3973` 只实现读取回调接口和拒绝条件；没有可执行生产 reader、命令绑定或凭据绑定。历史源代码与记录见 `evidence/recovery-tools-window-gate-local-review-20261020.md`、`evidence/recovery-tools-quiescent-window-review-20261020.md`。

| 源 | 命令/来源及覆盖 | 凭据、权限与有效期 | 副作用及资格 |
| --- | --- | --- | --- |
| GitHub Actions reader（尚不存在为可执行实时reader） | 门禁要求回调返回六队列 `in_progress/queued/waiting/pending/requested/action_required` 的完整分页与零计数、当前 main SHA、自动发布开关和配置/inventory指纹。历史审计脚本 `evidence/recovery-tools-github-entry-audit-20261020.py` SHA256 `efbe080…` 使用 `gh api --method GET <repos/a3225865526-wq/Clear-API/...>` 读取 repo/ref/workflows、六队列、活动run的jobs/pending deployments及main workflow/blob；该脚本不是门禁reader，历史运行曾有 API EOF，不能视为完整实时源。 | 历史使用本机 `gh` 已登录身份；记录显示对仓库有 admin/maintain/push 等权限，而不只是只读。凭据具体引用/后端名称、最小有效权限与到期时间均未在不读秘密的条件下核实，保持 **UNKNOWN**。不得假设PAT/App/会话仅有GET权限。 | 已审历史API方法均为GET，按设计不改仓库/Actions；但缺少已审定的实时绑定与调用封装，不能证明将来命令不含其他动作。可用性、权限最小化、错误/超时、字段映射与来源指纹 **UNKNOWN**；资格 **不通过前置核验（UNKNOWN）**。 |
| VPS生产状态reader（尚不存在为可执行实时reader） | 门禁要求返回SSH连通、匹配生产调用、进程、systemd jobs、部署锁及相关inventory/来源指纹；历史探针使用 `/proc/*/cmdline` 名称投影、`/run/lock/clearapi-production-deploy.lock` 与 `/proc/locks`、`systemctl list-jobs`，以及相关定时器/服务/入口清单。历史采集代码 `evidence/recovery-tools-vps-entry-audit-20261020.py` SHA256 `2c8d980…` 声明 stdin root inventory/no tool execution；较早的 production preflight 脚本/摘要记载命令形式为现有 `ssh` 身份 + strict existing `known_hosts` + `sudo -n python3 -B -` 执行审阅过的stdin只读探针。它们不是当前窗口采集器，也没有持续观察集成。 | 历史SSH身份为 `clearadmin@103.42.181.219`，复用本机现有SSH key；仅引用身份/主机，不读取或输出私钥。历史审计报告 `clearadmin` 具有 `NOPASSWD: ALL`，为广泛特权而非只读限定；远端授权密钥还存在root及clearapi-deploy入口。密钥轮换/有效期及 sudo 规则当前状态 **UNKNOWN**。不得将 `sudo -n` 等同于只读授权。 | 探针预期只读，但以root运行且其身份权限允许任意写命令；stdin代码审阅和命令参数绑定未对当前持续reader完成，间接调用/审计采集副作用亦未验证。SSH可达性和现时进程/调用/jobs/lock均未检查，资格 **不通过前置核验（UNKNOWN）**。 |

**范围与静默窗口契约（不是绝对技术锁定）：** 需纳入 Actions 全refs的六种队列/dispatch、rerun、approval、upstream events、GHCR核验及九份现存 workflow/action 定义；VPS侧已识别的 deploy/recovery/restore/inspection/manual SSH、systemd/cron/at/timer及相关工具调用。门禁的显式路由为 `actions_all_refs`、`dispatch_rerun_approval`、`upstream_events`、`ghcr_verify`、`manual_clearadmin`、`manual_clearapi_deploy`、`console_root`、`external_credentials`、`systemd_cron_at`、`other_agents`。唯一执行者须由负责人在本次归档专用窗口明确实名/身份并固定 executor PID、事务与归档目标；现有记录仅列历史owner标识 `a3225865526-wq`，不等于本次确认。窗口须先有负责人正面确认，批准固定UTC起止（不超过15分钟）、全部入口覆盖、两个合格只读源及单一执行者；两源均取得完整新鲜PASS后，连续观察满60秒且到达T0才可申请后续归档写入许可。任何新调用/队列、SSH失联、采集异常/超时/过期、UNKNOWN、字段/指纹漂移、监控线程停止、覆盖未知、锁异常、时钟偏差或窗口到期均不可恢复地关闭窗口；停止后不再推进写入，不自动重试/续期/回退，保留现场并报告。任意root/控制台命令、改名或已加载调用、外部凭据持有人、其他agent及未来其他ref等无法由快照技术证明排除，必须由负责人明确覆盖承诺；不能声称已被锁定。

**负责人确认清单（本轮未授权、未执行）：**
1. 确认仅归档窗口的UTC起止（<=15分钟）、唯一执行者及executor身份/PID；确认上述正常入口冻结范围，并明确接受未能技术锁定入口的人工覆盖责任；否则不启动观察。
2. 明确批准的只读采集用途/字段：GitHub只查询目标仓库全refs Actions六队列、当前main与自动发布/配置指纹；VPS只读取SSH连通、调用/进程/jobs、部署锁与入口清单。指定可审计的最小权限身份及凭据引用名称、存放/注入方式、权限范围、到期时间；禁止在聊天/日志中披露值。VPS不得用 `NOPASSWD: ALL` 冒充只读授权，须先提供可证明的只读命令范围或接受并另行审阅其风险。
3. 单独明确生产写入边界：允许在归档专用窗口内一次创建/持有指定 `/run/lock/clearapi-production-deploy.lock` 部署锁、指定 `/opt/clearapi/.tool-upgrade-...` 归档目录及其中四个归档/manifest/seal文件；列明确切目标、root属主/模式、内容与哈希验证方式、外部seal留存位置，以及归档部分失败时停止并保留现场的处理。此项不授权工具替换、boot资产安装、服务/数据库/账务/公网变更或第二个替换窗口。

在采集器代码/哈希、只读凭据范围/有效期、真实连通与字段覆盖均得到独立核验，且负责人确认上述清单前，开始60秒实时预观察的条件仍为 **NO**。本次仅更新本总控；既有未提交内容原样保留。
## 2026-10-20 — 当前恢复与 PG03 上线状态

**CURRENT STATUS: LOCAL WINDOW STOP GATE IMPLEMENTED / 25 NEW GATE TESTS PASS / LIVE COVERAGE & OWNER WINDOW UNKNOWN / PRODUCTION ARCHIVE NO-GO.** 本轮仅本地实现并单独只读复审静默窗口失效停止门禁：归档/替换独立、各最长15分钟、双有效采集后先观察60秒；任何新调用、失联、UNKNOWN或到期都锁定失败，逐项阻止后续逻辑写入，包括日志和回退，不自动续期或重试。25项新增门禁测试分组20+3+1+1通过，原19项事务测试及既有验收未重跑。未连接生产/GitHub、创建生产归档/输入/锁、替换工具或执行服务/数据库/账务/公网操作。生产真实采集源资格、全入口覆盖与负责人窗口承诺仍未取得，归档及替换未获本轮授权；boot/Gateway/上线阻塞保持。

### 2026-10-20 — 静默窗口失效门禁：本地实现与复审

- **本地实现/只读复审 GO；生产归档 NO-GO**。完整记录：`evidence/recovery-tools-window-gate-local-review-20261020.md`。既有入口清单/08:23–08:30快照仍是历史证据，没有作为当前实时PASS输入；原设计记录 SHA256 `45ebb3c7ff1bad77b09bd180c03dc8db6bfcf1d08010d7a37a910ab193310d8f` 保持不变。
- 新 `scripts/recovery-tool-window.py`：SHA256 `284125307fca2814b84f659d0470c41d133612de9d400362f888908b69fc3973`；`QuietWindow` / `WindowDriver` 实现一次性、固定UTC与单调时钟期限、60秒预观察、双监测线程、缺源/覆盖不完整拒绝及失效不可恢复。GitHub名义15秒/VPS2秒采集，TTL与最大观测间隔30/5秒；无默认PASS读源，不提供生产CLI或凭据。真实读源必须是单独审阅的实时只读回调，不能读取旧JSON伪装新观察。
- 更新 `scripts/recovery-tool-upgrade.py`：SHA256 `77ba66604746da9bd7ee41783dc40cc21e3184608185c1decce534a9365f25ed`，固定新门禁模块及原boot基础函数哈希。`prepare(window)`、`apply(seal,window)`、`rollback(seal,window)` 均必需相符窗口，绑定phase/action/精确事务。部署锁、归档/备份/暂存/manifest/seal、日志、quarantine/publish/retire/restore逐项检查许可；门禁失效不追加成功/失败生产receipt，也不自动继续回退写入。已进入的durable原语完成自身fsync/close，随后变更一律重新检查；不声称能取消已进入的系统调用或实现零延迟全局互斥。
- 新归档 schema `clearapi.recovery-tool-upgrade/v2` 封存归档窗口ref/期限/事务标识；prepared日志必须绑定该封存窗口，replace begin记录独立窗口。旧无窗口v1归档不能自动升格为新流程证据。新的生产候选必须固定以上新代码哈希，不能沿用旧操作API `7759004a...` 的批准身份；原RC/发布payload/停止receipt不变。
- 在原 `scripts/ci/test-recovery-tool-upgrade.py` 增加25项 `WindowGateTest`，文件 SHA256 `5652f4e396dc4da52e8cbb819c907385083a833dc6f9a476ac5500b1f37b6150`。分组通过20/20、3/3、1/1、1/1；覆盖正常独立窗口、60秒/到期/不续期、新队列/调用/进程/jobs、SSH/监控异常、UNKNOWN/缺源、漂移/锁/时钟、归档及替换中途停止、回退停止、FD释放、最后发布后禁止成功receipt与重试。首次新增16项中的3个参数冲突已修复；通过结果及分组边界见审计。不把分组结果声称为最终全量suite跑过。原19项仅调整合成窗口fixture调用，不执行；无CI/RC/boot/演练/备份验收重跑。
- **未覆盖/未验证**：任意控制台/特权命令、改名/已加载调用、外部凭据/其他agent及未来其他ref调用不能由进程快照直接证明静默；需要负责人覆盖冻结承诺。两个真实实时读源的凭据/字段映射/规范inventory指纹、全入口覆盖和采集时效尚未现场只读资格验证，不能判PASS。申请归档窗口前须确认owner、UTC起止<=15分钟、唯一executor及全部入口覆盖；固定新代码后，完成允许的60秒实时只读预观察并另行批准归档/input/lock写入。替换仍需实际归档独立验seal后再单独申请，现场NOREPLACE/fsync仍未实测。

### 2026-10-20 16:23–16:34 — 上一轮静默窗口与调用入口证据（历史）

本轮已实现下列历史记录中缺少的API门禁，见上方最新实现记录；历史入口/配置与空队列快照不因此成为当前窗口PASS，真实读源资格及owner承诺仍待取得。

- 完整清单、窗口方案及证据哈希：`evidence/recovery-tools-quiescent-window-review-20261020.md`。GitHub 全为 GET，现有 gh 凭据；VPS 复用原 SSH 身份，仅内存只读探针。所有观察记录独立保存，不改写16:17停止证据或旧 RC 身份。
- **PASS（采样范围）**：Actions in_progress / queued / waiting / pending / requested / action_required 六类在08:23与08:29 UTC两次采集中均 total_count=0、分页完整；main 始终为 `69a66431646b5cc8912bf97a739e08de07316443`。9项 workflow 全部 active，精确源定义补取完成。`CLEARAPI_AUTO_PRODUCTION_ENABLED=false`（既有状态，非本轮暂停）；production 环境仅允许 main、只有 branch_policy，无人工 reviewer/wait_timer 门禁。只列出1名仓库协作者（owner/admin），不能由此推断其所有 PAT/App/外部自动化持有者已静默。
- **入口链**：main push→CI→Gateway RC→Deployment rehearsal→Automatic production release→可复用 Deploy Production（最后自动步骤当前受 false 变量阻断）；直接 deploy/recovery workflow_dispatch 与 rerun/approval 仍可构成调用。GHCR inspection 经 `inspect-vps-authorization.py:69` 调用 VPS V2 `--verify`，处于独立并发组且不受正常部署锁路径覆盖；不能将“只读检查”排除出调用冻结清单。所有refs、上游触发及人工/凭据API调用都需纳入窗口。
- **VPS证据**：284项唯一 unit/config、15项标准 cron/anacron/periodic/user-spool 文件已作有界静态检查；backup/edge timer服务脚本及备份两项leaf依赖进一步核对，未发现四项工具的定时调用；标准at入口未发现。相关进程采样为空、部署锁不存在、systemd jobs为空。clearadmin为 unrestricted sudo；clearapi-deploy受限sudo白名单仍可调用四项工具，两种身份各有1条无 forced-command 的公钥记录；root直连SSH禁用。未读取/复制私钥或输出公钥值，未执行备份/工具/服务。
- **方案（未激活）**：归档与最终替换各开一个最长15分钟的独立窗口，负责人明确UTC起止和唯一执行者，覆盖两类SSH、控制台、其他agent及外部凭据调用；窗口内不push/dispatch/rerun/approve/改配置。开始前60秒排空观察，GitHub至少15秒检查一次（证据TTL30秒），VPS进程/锁/jobs至少2秒检查一次（TTL5秒）；每次目标变更前检查身份和新鲜permit。检测新调用、漂移、失联、UNKNOWN或到期即停止推进，按已审阅身份规则回退或保留现场。等待负责人审批期间关闭窗口，替换前重新进入新窗口并核对封存基线，不无限占锁。
- **仍缺最小条件 / NO-GO**：负责人尚未确认覆盖所有正常入口的有界维护承诺；现有 `Upgrade.apply()` 不是静默监控器，没有外部活性permit回调，最终小范围执行驱动必须先审阅其失效停止/回退联动。之后才可另行授权归档窗口、独立核验四项归档seal，并提交两项替换清单申请最终授权。现场NOREPLACE/fsync行为仍未实测，需在获批私有文件范围验证，不能由符号存在推断PASS。本轮没有擅自禁用工作流/调度、改权限或部署强制门禁。

### 2026-10-20 16:17 — 工具升级现场预检：UNKNOWN 停止归档

- 只读原始记录：`evidence/recovery-tools-production-preflight-20261020T081749Z.json`，SHA256 `aaa4eafb998f8426cb51b5f44b731ce711ce9baba4cd79e019e7896c75fb9203`；判定与缺失证据见 `evidence/recovery-tools-preflight-decision-20261020T081749Z.md`。探针仅经 SSH stdin 运行，未上传远端文件。
- **PASS（当前观测）**：四项工具及 runtime-probe-session / controlled-traffic-hold 规范旧版字节、root:root 0750、普通单链接/无 xattr；控制 pin 匹配旧 wrapper。目标/归档父目录 0:0/0755、同文件系统、挂载非只读，可用 24713482240 bytes / 4966845 inodes，候选归档路径不存在。原 Gateway stopped/127、Executor/PostgreSQL running、Caddy stopped/no、Docker active/running/NeedDaemonReload=no 的基线保持。
- **在途调用：采样未观察到，持续静默 UNKNOWN**。08:17:49.801352 与 .986064 UTC 的 `/proc/*/cmdline` 投影无已知工具/Actions runner 匹配且无读取错误；部署锁不存在、systemd jobs 为空。直接 systemd/cron 名称引用未检出；这些有限投影不能排除已加载模块、改名/间接入口、远端队列或采样后的新调用。未将采样结果写成完整窗口 PASS。
- **硬阻塞**：缺少明确时限、责任人及禁止新人工调用/dispatch 的窗口约束，并缺少相关远端部署/恢复/维护队列与间接调度入口的排除证据。共享锁不足以阻止加锁前的模块导入/校验。libc renameat2 符号存在，但现场文件系统 NOREPLACE/持久性行为未做写入实测，不冒称 PASS。
- **归档 NOT CREATED / 完整性 NOT RUN / 替换 NOT RUN**。目录与容量可行不等于已满足写入门禁；出现必要 UNKNOWN 后未调用 prepare、创建锁或暂存新代码。故本轮不签发最终生产替换清单、不申请将 UNKNOWN 豁免为 PASS。下一最小动作是补齐静默窗口/队列/入口证据；若需要暂停调度或修改访问控制，先列明最小动作另行批准。门禁解除后重新核对现场，才可在授权范围内生成四项归档、独立验 seal，并提交仅两项替换的最终待批清单。

### 2026-10-20 — 四项旧工具本地复审：两项替换、两项保留

- **本地方案 GO / 生产执行 NO-GO（未另行授权）**。完整方案及历史证据更正：`evidence/recovery-tools-upgrade-review-20261020.md`。现有 HEAD 的根 tree 恰为 `7231fa53f2a1bb8cebb7ab314f9f038442ac3de6`，与既有精确源提交审计一致；本轮直接比较该本地 tree 与事故提交 `c50a53c1b89e121bcade7dac41d3f11e53a9832a` 的原始 Git blob，没有修改发布脚本或重建 RC。
- 最小生产候选仅两项：先将 control 从 `08f457e0...` 替换为 `2c14d16d...`，再将 wrapper 从 `1bd21eae...` 替换为 `dd4e2fbe...`，均固定 `0:0/0750`。新 control 原字节已固定新 wrapper SHA；新 wrapper 已固定 recovery helper SHA，不现场修改常量或放宽哈希门禁。legacy-transition 的规范 SHA 为 `c078b3c8...`，schema-rehearsal 为 `5bb73efc...`，旧→新均相同，作为不写入的身份护栏。
- 更正上一轮证据中的两项 `recovery_source_sha256`：`7eef8104...` / `0b594a4f...` 是 CRLF 工作区字节，不能安装为规范恢复源码。原停止 receipt SHA 仍为 `535918ade2d0c7b887b705b3f561fa3c3a91093eb4c5ceee34f96070d8739df5`，不覆盖原件；更正不撤销真实 wrapper/control 停止原因，也不改变七项来源 PASS、原 RC `39357150402/1` 或其历史。
- 新本地事务 API：`scripts/recovery-tool-upgrade.py`，SHA256 `7759004a21e68b03b2ece8ae9300364043a7adc2625a8c8075177abf1d320d42`。复用哈希固定且未修改的 boot 安装器安全读写/部署锁/日志基础函数；独立封存四项原件与元数据、外部 seal 校验后，仅两项通过 NOREPLACE 逐项替换。日志 intent 与 dev/inode/内容/权限约束回退；未知文件、护栏漂移或回退失败即停止并保留现场。无生产 CLI，无服务/网络/数据库调用，不属于原 RC 产物。
- 新增一个定向测试文件 `scripts/ci/test-recovery-tool-upgrade.py`，SHA256 `7d8bf8613612db963cd89742857fb935b088aa0bd89cd37f1cc292acd8fdcf28`；WSL Linux root 临时目录 **19/19 PASS**，含8个替换失败边界、旧拒绝/新兼容、CRLF与哈希拒绝、归档校验、部分回退失败及身份约束恢复、冲突/权限/链接/日志/幂等检查。未重跑既有31项 boot 安装器测试、CI、RC、恢复演练或备份验收。
- 只读复审 PASS；生产最新状态/调用静默窗口/依赖身份/容量与文件系统前提 **UNKNOWN / 本轮未检查**。共享部署锁不能阻止所有入口在锁前导入或校验模块；未来需先排除在途调用并禁止新调用，否则不得升级。下一步只可单独申请“两项替换、四项归档、固定新工具输入、独立校验、受控回退”授权；不能自动续跑 boot 安装或 Gateway/Caddy 恢复。

### 已完成

- 恢复源代码 `ff6e0f4b2` 已合并为 main `69a66431646b5cc8912bf97a739e08de07316443`（PR #77）。原 CI `39354033185` SUCCESS；合并后 main CI `39354908193` SUCCESS，SQLite/MySQL/PostgreSQL 检查均通过。
- Gateway RC `39357150402` SUCCESS，源码与 RC 身份一致；Gateway digest `sha256:07fd58272b4fc62bcc52a3ad1d20e0dac357ba93de9e91a6a797d5e8fe3e8e7a`，binary SHA256 `40ebc70665bf7f45c7a201c4fdd82a90913a1873b2c7ca0debda1829983eee4b`，manifest/schema rehearsal 均通过。RC 明确标记 `production_ready=false`、`production_acceptance=not_run`。
- 失败恢复演练 `39358088019` SUCCESS；同源 `recovery-rehearsal.json` 绑定 RC `39357150402`、source `69a66431646b5cc8912bf97a739e08de07316443`、wrapper SHA256 `dd4e2fbedff508588b203d3736b6104e1fcd5557c05182f5ef47db8a3ed34d85`。演练没有生产凭据或生产写入。
- 授权后的只读门禁复核完成：RC `39357150402` 和恢复演练 `39358088019` 仍为 SUCCESS；未触发 recover workflow，未产生生产 job、receipt 或运行状态变化。
- SSH 只读审计 PASS：`clearadmin@103.42.181.219:22` 通过默认 `known_hosts` 的严格主机密钥校验，实际 sudo 权限与历史记录一致；未创建新密钥或扩大权限。完整证据：`evidence/controlled-recovery-ssh-audit-20261020.md`。
- 现场只读结果：Gateway `clearapi-new-api-1` 为 `Exited (127)`；Executor 与 PostgreSQL running/healthy；Caddy 为 stopped 且 `restart=no`；公网 localhost health probes 均未连接成功，未执行外部开放检查。
- 备份/回滚资产核对：manifest SHA256 `a9b9c32cd168c5a68740e20f1c949cff41da665b14d7aeb5790404dd0d795166`、加密备份 SHA256 `0c8757c8d204cddd7cc5b3b9a7835823e376093b9ee344f5a8bd8c219d3b3b31` 均匹配；旧 Gateway image 在场。未执行恢复，真实回滚仍为 UNVERIFIED。
- Caddy reconcile 已在本地实现：只调用 `systemctl is-active` 门禁和固定 Docker socket 的最小 `docker inspect --format` 投影；Docker daemon 不活跃、容器缺失、JSON/状态无效或探测异常均为 `UNKNOWN`，不会因异常默认 `PASS`。持久化 journal 的 `edge` 被按隔离前原始快照解释，推导出的 stopped/no 才是隔离期望；不改写 journal、不执行 start/stop/update/systemctl 写操作。
- reconcile 支持 `--stdout-only` 零审计写入探测；使用 `--audit-log` 时只允许在本地 `evidence/` 目录追加 JSONL，记录 UTC 观测时间、observed/persisted/expected、差异、错误和建议动作；拒绝生产路径、UNC、符号链接、硬链接和状态文件别名。针对正常隔离、运行中、身份/镜像/暂停/restart policy 漂移、容器缺失、Docker 不可用、超时、坏 JSON、重复字段和审计追加已覆盖测试。
- boot recovery 正式实现已从当前恢复分支和历史提交 `7a491c3ff`、`ff6e0f4b2` 核对：`scripts/runtime-boot-recovery.py`、四项 tmpfiles/systemd 资产定义及 `scripts/ci/test-runtime-boot-recovery.py` 均存在；缺失的是生产安装状态，不是本地源码。新增重启恢复/幂等安装测试，未安装到生产。
- 本地验证：Windows `python -B scripts/ci/test-caddy-state-reconcile.py` 共 19 项，18 PASS、1 项 POSIX symlink 用例跳过；WSL root 下 Caddy 19/19 PASS、boot recovery 12/12 PASS（含真实 tmpfiles 与 systemd-analyze 验证）；Python 语法检查与 `git diff --check` 通过。完整根因、资产 SHA256、启动触发条件与前置检查见 `evidence/controlled-recovery-local-review-20261020.md`。
- RC `39357150402` 的缓存发布原件已找到并核对：`artifact-hashes.json` 中 4 个条目全部匹配（gateway manifest `68bbc50ff0703cfc172eff77d1c56b45bf79ef03cad7556f592836e7c8d46fa0`、binary info `c5a9fae198f220074302eec01da247ffecc5eb5f627811508adb45edf6b5add1`、schema JSON `75f09b27fe2330ce5481ea1ea01fbf8a676bc1c3308e9840702a4b72ad755eb4`、target SQL `79d553767b10ed9edc45c37e56a2a82bcc1dce661e0ddd469b306b43d732f6a4`）。Manifest 的 source/candidate/OCI/workflow commit 均为 `69a66431646b5cc8912bf97a739e08de07316443`，RC run/attempt 为 `39357150402/1`，Gateway digest 与 binary SHA256 均匹配既有记录；`production_ready=false`、`production_acceptance=not_run`。
- Recovery rehearsal 缓存原件也匹配：source `69a66431646b5cc8912bf97a739e08de07316443`、RC `39357150402`、wrapper SHA256 `dd4e2fbedff508588b203d3736b6104e1fcd5557c05182f5ef47db8a3ed34d85`，与当前 `production-deploy-v2.sh` 一致。
- boot recovery 来源链 **PASS**：2026-10-20 06:40 UTC 通过现有 `production` 远端对应的 GitHub API 读取精确提交 `69a66431646b5cc8912bf97a739e08de07316443`，根 tree 为 `7231fa53f2a1bb8cebb7ab314f9f038442ac3de6`；三个 Python 文件（Git 模式均 `100644`）与本地 `ff6e0f4b2` 资产逐字节一致，SHA256 分别为 `354a69bccfeb3ad7ba631a5b28dce27b2f2989d7315d597f8b53d82d13a5cdd5`、`afcefce7dcf05bf853a1c44244ddf834dd9e2d6fb023f6cb75495410fc7b7b8e`、`860914fd36624296f6177dcf127a2aa5e1c1ab82c95b98760b79c9818ad99ff1`。四项 systemd/tmpfiles 资产来自该 tree 的 `scripts/runtime-boot-recovery.py:35-68` 中 `ASSETS` 字面量，UTF-8 内容也全部逐字节一致；不是四个独立 Git 文件。原本因本地缺失 merge object 而留下的 UNKNOWN 已解除，没有替换资产或改变本地 HEAD/main。
- RC `39357150402/1` 在线原件核对 PASS（沿用上一轮结果）：head/source/candidate/OCI/workflow commit 均为上述提交；artifact `12235966400` ZIP SHA256 `ac7c42fc5a3549773f2c59080b503d6d5201f7442c9c20a955eb542f97ffb09b` 与 GitHub digest 一致，四项内部 inventory 哈希全匹配。恢复源码明确将 boot/helper 列为 recovery assets，并在执行前对照该提交逐字节校验。**原 RC 未包含独立 boot inventory**：其四项 inventory 不包含这七项资产，Gateway 最终镜像也未打包它们；本轮以独立本地补充归档记录该交付清单缺口，不改变原 RC 身份或历史。原始来源审计保留于 `evidence/controlled-recovery-local-review-20261020.md`，本轮未覆盖该证据。
- 七项资产的独立补充 inventory 已保存至 `evidence/boot-recovery-assets-supplement-rc39357150402-attempt1-20261020.json`；归档 SHA256 为 `b25e5a9c3deee05b51c7aaf3d0c74167c0be223929e2f1191d01ebc5492917d2`，校验文件为同路径后缀 `.json.sha256`。归档包含 source commit/tree、原 RC `39357150402/1`、artifact/manifest 身份、三项源码路径/Git 模式/blob/哈希及四项内嵌资产定义位置、目标路径、请求安装模式和哈希；未知生产模式明确标为未验证。七项本地哈希均与既有审计一致；归档字节、JSON 内容、SHA256 与校验文件回读核验 PASS。此为事后本地补充清单，不是原 RC workflow 产物，未上传外部系统；原始审计文件保持不变。
- 四个 systemd/tmpfiles 内容的本地期望 SHA256：`/etc/tmpfiles.d/clearapi-runtime.conf`=`61c0985511f716a07407f3f27ee8b81a43cf34ccd9c5d9a906641380f4985932`；`clearapi-runtime-directory.service`=`f1ded6ec46c2906f0173459ab5e0768de37e4bf4c63a387f0eaa9e8b6b83ca93`；Docker drop-in=`b327be816c5c2a7bcef7a8378be6af5eb91cd9689ac04515766dad38e8d2d630`；`clearapi-runtime-identity.service`=`81dfc61b3d6b118dac38f846827bd49d0f4dcfd9f3c874a796b534eb4e8972aa`。安装目标、权限和回退说明已写入本地审计记录；生产目标未读取或修改。
- 独立审阅与生产只读预检（2026-10-20 15:39–15:43 Asia/Shanghai）完成：安装器固定哈希 `6bf1c77e6ce1c162e22328239badbaf67b77bde21f100fbbcf3a5747812d01d5` 的事务边界、封存校验、权限、受控回退、重复执行及路径约束审阅 PASS；七项目标中 helper 正确、其余六项缺失且无未知内容冲突；Caddy 隔离与归档目录/容量只读前提 PASS。详见 `evidence/boot-recovery-independent-review-20261020.md`。本轮读取了生产目标，取代上条“生产目标未读取”的旧阶段状态；仍无资产写入。
- 已收到限定生产安装授权；执行前兼容性只读门禁 **FAIL / STOP**。新 helper 的 `authority()` 必须让 RC manifest 的 `v2_wrapper_sha256` 等于所加载 `CONTROL.V2_SHA`；现场旧 control 仍固定 `1bd21eae376dd8316d1cfabe558133b4e0390965c2f13b7005e4deb656eac109`，而 RC/恢复源码要求 `dd4e2fbedff508588b203d3736b6104e1fcd5557c05182f5ef47db8a3ed34d85`，旧 wrapper 同时缺少 `--recover-failed-controlled`。负责人本次第6条将此类兼容性风险明确列为停止条件，因此在归档/暂存/锁创建之前停止，不更新旧工具。

### 当前阻塞

- 当前硬阻塞：Gateway 仍为原容器 Exited(127)，/run/clearapi 缺失；六项 boot/recovery 安装目标仍缺失。Caddy 现场隔离已只读核验 PASS，原始 running/unless-stopped 快照与当前 stopped/no 的差异符合隔离流程，不再作为 journal 损坏或来源不明阻塞。没有修写 journal、创建运行目录或启动服务。
- 恢复执行依赖 **FAIL / BLOCKED（本轮16:17重新只读确认）**：四项工具均精确匹配事故提交，但只有 `production-deploy-v2.sh`、`pg03-release-control.py` 与恢复 tree 不同。另两项规范 Git blob 完全相同，先前“不匹配恢复源提交”的四项泛化已纠正。旧 wrapper 缺少 `--recover-failed-controlled`，旧 control 的 V2_SHA 仍为 `1bd21eae...`，恢复 RC 要求 `dd4e2fbe...`；七项 boot 安装不能修复该差异。两项替换、四项独立归档/身份护栏的本地方案已 GO；现场工具身份/目录可行性已 PASS，但持续排除新调用仍 UNKNOWN，因此未进入归档，生产替换仍须单独批准。不夹带入 create-only 七项安装，不重建 RC。
- 启动写入风险仍 UNKNOWN：生产环境存在 finance、billing outbox/protection/reconciliation 和 task-settlement 控制变量，但有效迁移/后台写入开关无法从现场安全确认。恢复文档明确记录 Gateway 启动可能执行 AutoMigrate、auth cleanup、system updates、finance sync、completion/outbox processing 和 route ledger recovery；按授权边界停止在任何生产写入之前。
- 本地 boot/helper/recovery/wrapper 到 RC 精确源码提交的字节身份已核实为 PASS，boot 实现未改写；独立 boot inventory 已完成本地补充归档，该项不再是本地归档阻塞。新 Caddy 工具/测试仍未纳入发布身份。历史 CI/RC 成功保留，不冒称新增代码已通过远端门禁。
- 安装入口改为新本地 `scripts/boot-recovery-install.py`：必须先封存并独立验证七项原状态、enable 链接与目录基线，再在部署锁下安装；无立即 start/restart 调用。旧 boot 脚本内置 `install` 保持原字节，不得作为后续生产安装入口绕过事务归档门禁。
- 安装前归档、模式及回退的**本地修复 GO / 独立审阅 PASS** 保留；负责人限定批准已经取得，但新增的兼容性停止条件已触发，安装执行 **BLOCKED**。真实生产归档及其完整性均 NOT RUN，不冒充 PASS；本次没有进入生产写入，也没有可验证的生产安装 receipt。
- 网站恢复和 PG03 正式上线仍未执行；Provider、真实 Usage/Billing、登录及商业验收仍未验证。CI/RC/演练通过不等于上线成功。

### 需要负责人执行

- **历史限定 boot 安装批准已按第6条停止，当前任务不授权任何生产归档、替换、boot 安装或 Gateway 启动**：原提案及 SHA256 `9ad6fe8b0aa0db20087409fae2a75be5e0379aefd7b6480c65a2ce312cccab05` 保留。新鲜permit门禁已本地实现/复审，下一最小条件是负责人确认15分钟窗口/全入口冻结、固定新版操作代码，并绑定审阅合格的真实实时只读源与规范指纹，完成60秒预观察；缺失/UNKNOWN即门禁拒绝。再另行授权生成/独立校验四项归档，提交 control→wrapper 两项替换/受控回退清单供确认。快照为空和既有auto=false都不是替换批准，不自动续跑七项安装。
- 新事务入口只有在独立归档校验、原状态回读和精确来源/权限检查全部通过后才可创建缺失资产；不覆盖任何既有版本，不自动规范化权限异常。回退仅撤销本次日志可证明且身份未漂移的对象；来源不明、目录创建身份无法证明、加载状态异常或回退 reload 失败均保留证据并停止，禁止以 start/restart、删除目录树或覆盖未知文件补救。
- `mode=recover` 前仍必须明确启动写入范围：master 执行迁移/初始化；finance 需 enabled=true 且 owner ID 有效；protection 或 durable-wallet 开启即触发 reservation completion，outbox 只控制额外日志交付；settlement 在 polling 被调用且相关开关/前置 schema 满足时恢复；route-ledger recovery 和 instance reporter 不受 master 角色统一禁止。生产门禁在 InitResources 之后，可能晚于部分写入。已与事件镜像对应源码核对这些路径未变；任何有效配置、schema、pending workload 或写入影响 UNKNOWN 时保持 BLOCKED，需另行明确授权已审阅的启动写入范围。
- 仅在 `recover` 成功且新 receipt、备份/回滚和全量 Runtime 检查通过后，再单独授权 `mode=restore` 恢复原 Caddy；随后仍需独立完成 PG03 线上验收，不能将 website restore 视为 PG03 上线完成。

### 2026-10-20 — 上一轮 boot 授权执行尝试：兼容性停止（历史记录）

以下保留上一轮停止过程；其中“四项均不匹配恢复源码”的判断已由本轮原始 Git blob 审阅更正为“两项不匹配、两项保留”。原 receipt 不改写，生产停止事实不变。本轮新增本地代码与测试的结论以上方最新复审为准。

| 检查/阶段 | 结果 | 说明 |
| --- | --- | --- |
| 授权接收与本地安装器身份 | PASS | 负责人批准限定 boot 安装并附七项约束；安装器仍为已审阅 SHA256 `6bf1c77e6ce1c162e22328239badbaf67b77bde21f100fbbcf3a5747812d01d5`，未改写。 |
| 执行前旧工具/新资产兼容性 | FAIL / STOP | 15:55:13 Asia/Shanghai 经原 SSH 身份只读确认四项旧工具仍不匹配恢复源码；旧 wrapper 无恢复入口，旧 control 不接受恢复 RC wrapper 哈希。按本次第6条停止，不沿用上一轮“boot-only 可分阶段申请”的范围判断绕过此新停止门禁。 |
| 生产输入目录、安装前归档、部署锁 | NOT CREATED | 三个授权候选路径均仍不存在；没有调用安装器 archive/lock，也没有任何现场归档可供校验。 |
| 归档完整性 / 安装 / reload / enable / check-installed | NOT RUN | 停止发生在任何生产写入前。不能把本地方案验证或旧31项测试视为这些生产步骤已通过。 |
| 回退与生产 receipt | NOT NEEDED / NOT CREATED | 无本次生产变更；不调用回退，不虚构生产安装成功 receipt。已生成独立本地停止 receipt，保存授权文档及安装器哈希、只读实测和停止原因。 |
| 现场容器与隔离 | 保持原状态 | 精确 Gateway 仍 stopped/127；原 Executor/PostgreSQL running；原 Caddy stopped、未暂停、restart=no/0。未执行任何 start/restart、Docker/主机重启、数据库、账务或公网动作。 |

本地停止 receipt：`evidence/boot-install-compatibility-stop-20261020T075513Z.json`；SHA256 `535918ade2d0c7b887b705b3f561fa3c3a91093eb4c5ceee34f96070d8739df5`，已重新读取核验。它是本地执行尝试/停止记录，不是生产安装完成 receipt。观测时间为 `2026-10-20T07:55:13.753503+00:00`。

停止原因已足够明确，本轮不扩大为旧工具修复或追加生产权限申请；不继续归档/安装，也不重跑既有测试、RC 或验收。后续恢复仍须解决四项旧工具合同及 Gateway 启动写入范围，Caddy 恢复和正式部署仍未授权。

### 2026-10-20 — 上一轮独立审阅与生产只读预检

| 检查 | 状态 | 本轮依据及范围 |
| --- | --- | --- |
| 安装器独立审阅 | PASS | 重新读取完整代码，核对事务/路径边界、封存完整性、权限、原子 no-replace 发布、日志对象归属回退及无立即 start/restart 调用。相同事务重放被拒绝且不追加目标变更；新事务可保留已正确资产。不是任意重试都不运行 reload/enable。 |
| SSH 身份与 root 只读权限 | PASS | 复用历史明确密钥；公钥指纹与旧审计一致、严格 known_hosts、IdentityAgent=none。未读取/复制私钥，未创建凭据或扩大权限。默认身份失败后仅改为正确的原身份。 |
| 七项目标、属主/权限与链接 | PASS（仅安装兼容性） | identity helper SHA256 与 inventory 一致，0:0/0750/nlink=1/无 xattr；其余六项缺失。预期 enable 链接及额外相关 alias/mask 不存在；docker.service.d 缺失，wants 目录及其它必需父目录为 root:root 0755。缺失不等于已经安装。 |
| 当前 systemd 前提 | PASS | Docker active/running，NeedDaemonReload=no，无待执行 job；两个新 boot unit 的 not-found/inactive/dead 投影字段完整、无覆盖。只执行 show/list-jobs/is-active，没有加载或 enable。 |
| Docker、Gateway、Executor、PostgreSQL | PASS（保留现场身份）；Gateway 可用性 FAIL | 原 Gateway f8f963a7... 停止/127，缺失运行目录的 mount 错误未变；精确 Executor 1081a1b5... 与 PostgreSQL running/healthy。Docker 活跃后才读取固定 socket 投影，无 Docker exec 或数据库查询。 |
| Caddy 与隔离 | PASS（观测范围） | 原 Caddy 54c8550b... / f2a1290d... 停止、未暂停、restart=no/0；与冻结日志推导的隔离期望一致；TCP/UDP 80/443 无监听。未做公网访问或入口操作。 |
| 原事故记录与备份信息 | PASS（身份/元数据） | manifest/target/hold 三份冻结 SHA 匹配。备份 manifest SHA a9b9c32c... 匹配；加密文件 root:root 0600、12323351 bytes 在场；未重跑加密文件全量哈希、解密、恢复或旧备份验收。当前加密内容新鲜验证不声称 PASS，既有验收按原范围保留。 |
| 归档可行性 | PASS（只读条件） | 候选 `/opt/clearapi/.boot-preinstall-rc39357150402-1-20261020T073800Z` 及 input 兄弟路径不存在；父目录0:0/0755无 xattr，root可写/可搜索，挂载非只读；根与七目标最近父目录同 device=64769。可用24713674752 bytes、4966845 inodes，超过16 MiB/64 inode计划余量。未做任何创建/写入/fsync探针。 |
| 真实归档、生产安装及回退实证 | UNKNOWN / NOT RUN | 本轮明确禁止。只能在后续单独批准后生成归档、独立验 seal 和原状态，再决定是否进入安装；不可从只读可行性推断已经成功。 |
| 完整恢复工具合同 | FAIL | 四项 root 工具为精确可追溯旧版；旧 wrapper 没有恢复入口，旧 V2 pin 与恢复 RC 不符。后续最小四项升级需单独规范和审批，不因此放宽七项安装器对既有资产的拒绝覆盖规则。 |
| Gateway 启动及上线写入影响 | UNKNOWN / BLOCKED | 本轮不查账、不执行 schema/pending-workload 验证；旧启动写入范围阻塞保持。boot enable 的未来启动影响须在有限清单中明确接受，不等于授权当前重启/恢复或开放。 |

- 原始只读证据：`evidence/boot-recovery-independent-preflight-20261020T073948Z.json`、`evidence/boot-recovery-preflight-followup-20261020T074134Z.json`、`evidence/boot-recovery-dependency-preflight-20261020T074500Z.json`。最后一份以 JSON 内观测时间 07:43:41 UTC 为准，不以文件名充当时间。首次 Docker 健康字段投影对停止容器失败，原 UNKNOWN 记录保留；缩小投影后的补采已解析真实容器状态，未修改原记录。
- 独立审阅及阻塞根因完整记录：`evidence/boot-recovery-independent-review-20261020.md`。只读复审未改安装器或测试；原31/31结果保留，未重复运行。
- 具备**仅 boot 七项安装**的限定申请条件，待负责人审阅 `evidence/boot-recovery-install-authorization-20261020.md` 后另行批准；不具备 Gateway 恢复、网站开口或 PG03 正式上线的执行条件。实际生产归档完整性未知是批准后执行阶段的硬门禁，不能绕过；现场四项旧工具与启动写入未知仍阻塞后续恢复。

### 2026-10-20 — 上一轮三项修复及只读复审

**本地结论：GO。生产结论：NO-GO。** 完成本轮授权的归档、精确安装模式、可审计失败回退三项修复；生产连接、现场归档与安装未授权也未执行。下面保留的历史 NO-GO 记录已由本节对应实现与测试结果取代，不作为本地修复仍未完成的结论。

| 修复 | 实现及复审结果 |
| --- | --- |
| 七项安装前归档 | 新 `scripts/boot-recovery-install.py` 封存现存原始字节、数值 UID/GID、完整权限位、SHA256/长度、链接数及 dev/inode，明确记录 absent；保存 enable 链接目标/权限/属主/哈希、两项可能新建目录和只读 systemd 投影。root:root 0700 归档目录、0600 manifest/seal、文件和目录 fsync、独立外部 SHA 校验及安装前再次比较；归档失败不得安装。 |
| 三项 Python 安装策略 | 三个 `/opt/clearapi/bin/*.py` 目标固定 root:root `0750`，四项配置固定 root:root `0644`；显式 fchown/fchmod，不依赖 umask。依据现有 root 运维安装器的 0750 策略、recovery 对 identity helper 的直接执行要求和 boot unit Python 调用，非推测现场权限。原 inventory 的 null 安装模式字段不回写，独立策略记录于新审计。 |
| 部分失败及冲突 | 完整暂存后原子 no-replace 发布；部署锁、预期前置状态和 hash-chain JSONL 日志绑定逐项对象身份。已存在者必须内容/精确权限都匹配，否则停止；从不替换未知旧资产。 |
| reload/enable 失败回退 | enable 的唯一预期 WantedBy 链接先由事务创建、记账；真实适配器仅允许显式 reload、`--no-reload enable` 和只读投影，无 start/restart/disable/Docker。失败仅撤销日志中本次创建、身份仍匹配的对象；核对完整原文件/权限/链接及 systemd 基线。回退 reload 最多一次，失败记录 incomplete，不自动重试。 |
| 崩溃及未知状态边界 | 回退可只依赖封存归档、外部归档 SHA 与持久日志，不依赖源码包。不能证明的目录创建、未知 inode/内容/权限、意外 enable 链接、ACL/xattr、额外 mask/alias/覆盖及跨文件系统布局均 fail-closed；保留未知对象，不伪报恢复 PASS。 |

- 新安装器 SHA256：`6bf1c77e6ce1c162e22328239badbaf67b77bde21f100fbbcf3a5747812d01d5`；新定向测试 `scripts/ci/test-boot-recovery-install.py` SHA256：`93f601d9eb3bd4955176cd64c332f30e4e0c4dc732b083563e5dafe662dbeb2b`。两者都是新本地资产，不冒充源提交 `69a66431646b5cc8912bf97a739e08de07316443` 或 RC `39357150402/1` 的文件。
- 仅运行新增测试：现有 WSL Ubuntu root / Python 3.14.4 下 `python3 -B .../scripts/ci/test-boot-recovery-install.py` **31/31 PASS**。全部目标、归档及变更只位于临时目录，systemd 使用替身，真实 subprocess 调用被禁止；已验证归档截断/篡改/漏项、archive 路径冲突、不同 umask、权限/属主/链接异常、部分发布、同字节 inode 替换、reload/enable 失败及回退失败、进程重建后回退、互斥和加载顺序漂移。未执行旧 boot/Caddy 测试、完整 CI/RC/备份验收或真实 systemd 集成测试。Python 内存语法解析及空白检查通过。
- 最终只读复审：归档校验与回退入口独立；公有 archive/install/rollback API 均获取部署锁；既有正确资产及原 enable 链接不变；未知对象不能以内容相同替代 inode 归属判断；所有 incomplete 分支均阻止成功结论和自动再次回退。旧 `runtime-boot-recovery.py install` 不再列入待执行方案。
- **剩余生产阻塞**：新安装器独立身份及策略尚待负责人审阅接受；七项目标、相关链接/覆盖/权限、无待执行 systemd 作业和同文件系统归档条件需要另行授权的现场只读预检；之后还须现场生成并独立验证生产归档，再单独授权安装。归档阶段也可能创建部署锁和归档目录/文件，不能当作只读步骤。Gateway 启动写入、Caddy/隔离、数据库及业务验收仍属后续独立门禁。
- 本轮停止在本地修复与复审完成，不请求或执行生产写入；在上述必要生产前提齐全前，不整理可执行的生产安装授权清单。完整依据及失败语义见 `evidence/boot-recovery-install-local-review-20261020.md`。

### 2026-10-20 — 历史安装前归档与回退静态审阅（修复前）

**当时结论：NO-GO，仅完成本地审阅。该阶段记录保留，最新本地修复复审结论以上节为准。** 七项资产的来源与目标路径一致，不代表已具备可执行、可恢复的生产安装事务。当时只改本总控文档；未改源码、inventory、校验文件或其引用的原始审计，未运行既有测试。RC 在线状态沿用既有证据，未重新访问 GitHub。

| 必要检查 | 本轮结果 | 证据或缺口 |
| --- | --- | --- |
| 补充 inventory 完整性及来源身份 | PASS | 实测 SHA256 仍为 `b25e5a9c3deee05b51c7aaf3d0c74167c0be223929e2f1191d01ebc5492917d2`；sidecar 匹配；其引用原始审计 SHA256 `e1ad09c894f4efb8970ea68ab5a17091ee09cb3be14eb09087830d3ae0118b9d` 未变；source/tree 与 RC `39357150402/1` 均匹配既有记录。 |
| 七项内容和安装目标 | PASS | 三个本地源码文件的 SHA256/Git 模式/blob 匹配 inventory；四项 UTF-8 ASSETS 内容的 SHA256、字节数、目标路径全部匹配，无重复目标。 |
| 明确安装权限和属主 | 未通过 | 三项脚本的 `installation_mode` 均为 null；Git `100644` 不能当作生产安装模式。四项配置的 `0644` 只是 `os.open` 请求值，受 umask 影响；尚无统一的七项 root:root/精确模式与安装后回读方案。helper 被恢复代码作为可执行路径直接调用，必须明确其执行权限。 |
| 保存七项目标的安装前内容、权限、属主、哈希、缺失状态 | 未通过 | `scripts/runtime-boot-recovery.py:211-230` 只生成四项配置，不安装三个 Python 文件、不归档旧内容或缺失状态；文档仅要求存在安装前归档。补充 inventory 是新资产清单，不含生产旧文件 payload，不能替代现场基线。 |
| 生产归档完整性 | 未验证 | 未连接生产、未创建或读取现场归档；现有资料没有覆盖全部条目的 manifest/payload/元数据回读验收证据。 |
| 部分失败与 systemd 回退 | 未通过 | 无跨七项文件及 enable 链接的事务日志、失败恢复状态机与验收步骤；文件 fsync 不等于父目录项已持久化；配置创建中断、reload/enable 失败均可能留下部分结果。 |
| 无立即启动或重启调用 | PASS（静态） | `install()` 及已核对 helper 导入调用链无 start/restart、Docker 操作或运行时目录创建；不代表生产现场的作业队列和有效配置已被检查。 |

**安装前归档方案必须补齐的内容（审阅要求，不是生产命令）：**

1. 采用固定七项目标 allowlist，并另列 `enable` 的预期链接 `/etc/systemd/system/multi-user.target.wants/clearapi-runtime-identity.service`、实际相关既有 enable/mask 链接及可能新建的 `docker.service.d`/wants 父目录。记录既有 unit enable/mask 状态、有效 FragmentPath/DropInPaths 与顺序；不得假定链接或目录原先缺失，也不得遍历或归档无关生产数据。出现额外覆盖来源时先停下并分类。
2. 对七项目标逐项保存明确的 `present`/`absent` 状态；存在者保存原始字节、SHA256、字节数、数值 UID/GID、完整权限位、文件类型和链接数；相关目录/链接保留类型、属主、模式和链接目标。ACL/xattr 等额外权限元数据若存在，须明确可保存和恢复，否则阻塞。符号链接、硬链接、非普通文件、异常父目录或来源不明资产只记录并停止，不能通过跟随链接、chmod/chown 或强制覆盖“修复”现场。
3. 明确 root-owned 归档目录、唯一事务 ID、与 source/tree、原 RC、补充 inventory 哈希的绑定、部署互斥范围及不可覆盖的保存方式；同一锁下以安全文件描述符读取，核对读取前后的 inode/元数据，防止归档时状态变化。已知旧资产还需有受信旧版本身份依据，单纯给未知旧文件计算哈希不等于证明其来源。
4. 在任何安装目标变更前落盘全部旧 payload、缺失条目与元数据 manifest，对文件及目录项完成持久化；重新打开逐项核对 payload 哈希、长度、类型、属主/模式和缺失条目，确认无漏项/多项。再封存归档 manifest/容器自身 SHA256 并保留独立校验记录；任一项失败即不安装。此处尚无实现或实际验收，不能标为归档完整性 PASS。

**回退覆盖要求与现有缺口：**

| 失败场景 | 必须遵循的回退边界 | 当前方案状态 |
| --- | --- | --- |
| 第 N 个文件创建/写入或进程中断 | 用持久化事务日志区分未触碰、完整新建、部分新建、既有文件；原先缺失者只处理可证明由本事务创建且身份仍匹配的对象。无法确认当前部分文件来源或遭并发更改时停止并保留证据。不能仅按“安装前 absent”就删除当前路径。 | 无逐文件创建意图、inode/哈希、完成标记及中断恢复流程；NO-GO。 |
| 既有文件内容冲突或来源不明 | 四项配置预检会拒绝当时存在的冲突；七项统一安装也必须先拒绝未知版本/目标漂移。回退只允许恢复可信归档中的原件，且先确认当前目标属于本事务允许撤销的状态；无此证据不得覆盖。 | 三项脚本安装尚无审定流程；预检与写入间也无整体锁/最终复核；NO-GO。 |
| 权限、属主、链接或目录异常 | 首次写入前停止；中途异常只对日志证明由本事务改变的对象恢复已归档数值 UID/GID 和权限/链接状态，验证哈希及元数据。不得用统一 chmod/chown 掩盖原始异常。 | 缺七项精确模式和元数据恢复验收；NO-GO。 |
| `daemon-reload` 失败/超时 | 认为加载状态未知，停止后续 enable；按可信基线和事务日志恢复文件/链接，再在另行授权的回退范围内 reload 并回读验证。若回退 reload 仍失败，记录恢复未完成、停止重试，保留隔离和证据，禁止以启动/重启服务补救。 | 现有代码只抛错退出，不撤销已创建配置，也不恢复管理器加载状态；NO-GO。 |
| `enable` 部分成功或失败 | 记录所有本事务实际创建的链接；仅撤销仍与事务记录一致的新链接，保留原有 enable/mask 链接和状态。不能粗暴执行 disable、删除整个目录或强制解除 mask。恢复后验证磁盘链接与加载状态；目录只在证明本事务创建且为空、未漂移时处理。 | 原回退原则未覆盖 enable 链接及目录；NO-GO。 |

现有 `scripts/clearapi-install-approved-script.sh` 只针对旧 `production-deploy.sh` 和 identity helper 两件套，硬编码旧 wrapper SHA256；不覆盖本次七项目标和 systemd 链接，且恢复路径会固定 chmod/chown。不能将该脚本当作本轮七项归档/回退能力的证明，也不得在本轮调用它。

**未来启动行为：** `enable clearapi-runtime-identity.service` 不带 `--now`，会建立持久化的 multi-user.target 启动关系，本身没有立即启动该服务的请求。未来启动进入该 target 时，identity unit 在 Docker/目录服务之后执行 reconcile，可能写 `/run/clearapi` 的身份文件；Docker drop-in 的 Requires/After 使未来 Docker 启动依赖目录服务，后者运行 tmpfiles 准备目录。identity unit 的 After 本身不拉起 Docker；独立的主机/Docker 启动仍可能按现有容器 restart policy 启动 Gateway，并触发既有启动写入。这些持久配置影响必须列入后续授权，不能把“本次没有 start”当作未来无行为变化。

**下一步最小本地清单（补齐条件，不是生产操作授权清单）：**

1. 明确七项目标的安装数值 UID/GID、精确模式、已知旧版本允许列表和互斥方式；若提出脚本 `0750`/配置 `0644`、root:root 等策略，应另行审阅记录，不能修改既有 inventory 后沿用旧校验值。
2. 补齐可审阅的现场归档与完整性验收步骤，以及上述五类失败的逐对象回退、链接/目录处理、加载状态验证和一次回退失败后的停止条件。本轮只记录缺口，不新写安装器或回退实现。
3. 本地方案必要检查全部通过后，再提交供负责人审阅的生产操作授权清单。现场只读预检、生产归档创建、资产安装、daemon-reload/enable 都仍需后续明确范围；Gateway 启动、数据库、Caddy、解除隔离与公网开放不并入 boot 安装授权。

## 2026-10-19 — 统一整合 A/B/C/E + PG03

- **CURRENT STATUS: PARTIAL / RELEASE BLOCKED。** 用户已授权全部正式门禁通过后自动生产部署，D 继续暂缓；此前冻结约束已被本次任务替代。尚未执行最终生产部署或线上计费验收。
- **基线：** `production/main=d0c20d6c46d86845805e4a9d35b067b0f6b20be4`（本轮 ls-remote/fetch）；工作区 `D:/AI/API/worktrees/clearapi-unified-integration`，初始干净 HEAD `64aaa5ed8755ccac7cdd72a5eab5cb853efcbbc8`。
- **整合：** A/B 保留既有合并；修复 A 三处格式；C 的 `f2878fe59`/`88e935b93` 已按共同祖先核对后选择性整合；E `33fcb21d5` 已整合，冲突保留最新 Executor 身份并重新计算 V2 固定哈希；PR #72 审阅分支已合入统一分支（尚未合并到远端 main）。
- **有效验证：** A 前端 7 文件/88 tests PASS；完整 TypeScript PASS（复用现有依赖）；定价清理真实 SQLite/MySQL 8.4/PostgreSQL 17.6 PASS；C 六包测试 PASS；PG03 root 测试 28 PASS。V1、identity refresh、清单及安装器三项固定哈希 MATCH；V2 `be01cd79e22e0e68f10253edce7520606751fe65cac526ca1e6d007282c5e549` 与 control MATCH。
- **检查边界：** 全仓格式检查 FAIL（包含既有问题）；本次 A 三处已定向修复。全仓 lint 178 errors/64 warnings，全部位于本次 main 差异以外，不能称全仓 PASS。锁定依赖 CI、统一构建与其余门禁仍待完成。
- **CURRENT BLOCKER：** 最终源码/RC/镜像证据尚未生成；历史三份证据不适用于最终候选；原始验收适用范围、VPS root 安装与 Runtime/MFA、当前未决操作、熔断恢复及池失败账务处理仍待核实。现有 RC 39205228315 仅历史候选，不作当前发布凭证。
- **NEXT MINIMUM ACTION：** 完成 E/PR72 回归、池失败账务结论、当前授权/原始证据只读核验，再执行统一 Required CI。仅在外部硬依赖可闭环后冻结最终 RC、正式 Rehearsal、生产部署与线上验收。
- **证据索引：** `D:/AI/API/runtime/clearapi-unified-final-20261019/`；历史总控 `D:/AI/API/worktrees/clearapi-master-status-unification/CLEARAPI_MASTER_STATUS.md` 保留只读。

### 当前增量（统一 CI 前）

- E/PG03 回归：V1 89 tests（88 PASS，1 real producer artifact 未提供而 SKIP）；backup identity 22 PASS；PG03 root 28 PASS；legacy transition 25 PASS；automatic release 28 PASS；producer 6 PASS（新增缺失/false/字符串/整数 rollback_proven 拒绝与失败 receipt 断言）。V2 同路径测试仍运行中。
- 新增必要修复：正式 rollback verifier 必须 `rollback_proven is True`。原始 evFinal/RC39197505434 材料在身份完全匹配时被明确拒绝；其仅证明双启动幂等，不是安全回滚，历史“11 gates 全绿”不能作本次放行依据。
- 原始 provider / restore measurement 已能独立读取；仅支持旧候选的有限结论，无最终源码绑定；未改写证据或 Secret。
- 池失败账务：relay/service/model 定向 PASS，真实 SQLite/MySQL 8.4.11/PostgreSQL 17.6 的 reconciliation 用例 PASS，覆盖 unknown marker、pending 查询、幂等退款、钱包/token 一致及审计回滚。非 durable 保守路径仍不能保证消费日志；最终生产必须核实 durable wallet + reconciliation schema/单实例/无 Redis/无批更新 + 运营审计承接，否则保持 BLOCKED。本轮未将未知 usage 伪装为零并退款。
- 当前环境最小阻塞：正式检查器的 `clearapi-vps` 无法解析，本机无 SSH config；未获取 VPS 身份、root 安装或 Runtime/MFA 结果。保留既有 GitHub 授权边界，不创建高权限凭据。
- 本次 24 个前端差异文件定向格式 PASS；全仓 lint 178 errors / 64 warnings 均在差异外。MySQL 初次检查的 DSN 缺 parseTime 导致时间字段读取失败，补为工作流原有 DSN 并用全新隔离 DB 后 PASS；两次失败记录保留。

---

以下为历史记录，不代表当前发布授权或候选状态。

# ClearAPI — Master Status

## 2026-10-19 — READ-ONLY AUDIT COMPLETE / RELEASE BLOCKED（最新阶段）

- **CURRENT STATUS:** BLOCKED；GitHub 只读调查已完成。main 仍为 `d0c20d6c46d86845805e4a9d35b067b0f6b20be4`；RC `39205228315` 保留。本轮无新 RC、Rehearsal、VPS 操作或熔断恢复。
- **CURRENT BLOCKER:** 原始运营验收材料尚未独立取得；VPS 当前未决操作、Runtime/商业基线未现场确认；现有熔断规则仍拒绝已识别的只读 environment record。不是读取权限缺失，也不再是 production records 不可读。
- **CURRENT PRIORITY:** 固定已取得的证据绑定和事故记录调查，确认原始验收适用范围，并限定后续恢复机制仅能分类精确、经审计的只读记录；未知 VPS 状态继续阻止恢复。PR #72 保持 Draft，避免在范围尚未收敛时产生新候选。
- **NEXT MINIMUM ACTION:** Cloud 完成当前必要 CI 与本阶段状态归档；后续需取得原始验收材料及真实服务器状态，才可最终冻结源码、一次生成必要 RC、重新签发真实证据并恢复发布。现在不需要用户操作手机/VNC或提供凭据；自动只读 push 通道已成功，无需手机 fallback。root 安装仍独立待管理员通道具备后执行，不能照搬旧清单。

### 阶段 1：已有授权通道核查完成，读取边界明确

PR #72 原 head `33bea6ce40d7b27c18133624e9b4430ae81e4b82` 的 CI `39207544711` 六项全部 SUCCESS，PG03 root 测试 28/28；前驱修复仍待合并。当前 main CI/RC 与候选身份沿用下表，未重复执行。部署记录仍 UNKNOWN，不恢复熔断。

### 阶段 2：新增正式只读调查模式，待实际运行

复用既有 `ghcr-pull-rehearsal.yml` 的 workflow_dispatch，新增 `release_audit_only` 分支：

- 仅 owner 的初始触发/重跑、仅 main 或上述审阅分支；独立 job，没有 production environment、SSH 凭据、sudo、服务容器、Provider 请求或构建。
- job token 只申请 `contents:read`、`actions:read`、`deployments:read`。不创建高权限凭据、不申请管理/写权限，不改变生产 sudoers。若 Actions 中仍被拒绝，报告失败阶段与这组最小读取权限，保持 BLOCKED。
- 下载既有 RC artifact，复用原 inventory/hash 校验；核对 main/source/run/attempt。仅输出三份证据中格式受限的发布 SHA/digest/run/manifest/schema 身份和字段匹配结果，不输出原始 Secret JSON、target_id 值、任意 provenance 文本/URL、token、会话或 API 错误响应。
- 读取 production deployment records/statuses，以及正式部署 workflow 的未完成或事故期间非 skipped 运行；保留记录 ID、状态与 Actions 关联，逐项待调查。VPS 上是否有未决操作仍必须现场核实，不能从 GitHub 空列表推定安全。
- 所有输出固定为 `clearapi.release-state-audit/v1`、`status=BLOCKED`、`not_release_evidence=true`；不会重签证据、生成 release-proof、恢复熔断或部署。原 VPS job 在该模式被跳过，且 GHCR 工作流不会触发 Automatic production release。

另提供独立 `PG03 release state audit`：push 触发仅限本 PR 分支且只监听审计源码/工作流文件，owner/triggering owner 双检查；同样只有 read 权限、无 production environment 或 SSH。它不会触发 RC、Rehearsal 或自动部署。使用现有仓库写入身份尝试自动调查，不创建高权限凭据或替代 root 登录方式。当前有效 CI 完成后才推送该必要补丁，不取消正在运行的工作流。

实际本地验证：automatic-release/audit 28 tests PASS（含 Secret payload/target/provenance 不反射、异常日志脱敏、未知部署状态拒绝及 job 权限隔离）；PG03 28 tests（26 PASS、2 root-only skipped）；rehearsal producer 6 PASS；YAML/py_compile/diff 检查 PASS。新增补丁改变 PR 身份，旧 PR CI 不能冒充新 head CI，更新后的必需 CI 另行核实。

### 阶段 3：审计结果回来后的边界

**首次实际自动审计已完成运行：** [39210043117](https://github.com/a3225865526-wq/Clear-API/actions/runs/39210043117)，job `117921166702`，源码 `62119ac189288e8b7694e8c91fd1b1ea0b7d2d6f`，Artifact `12166868531`（`release-state-audit-39210043117-1`，SHA256 `f08dcf40d41e221e9af7736bc3449853ef07e9c5f1ea83ac593cf881356bf4f3`）。它不属于 Rehearsal 或生产部署。

- 原始日志的三项 `public_history` 均精确匹配 `passed_cd6f_RC39197505434` 的 source_commit / gateway_digest / rc_run_id，即当前三份 Secrets 仍绑定 cd6f719e79de432de6219561b04502784cb58930、Gateway bdbb535b...fdc6、RC 39197505434；这不再只是历史文档推测。
- 与当前 RC 39205228315 不匹配的字段均为 source_commit、gateway_digest、rc_run_id、manifest_sha256；rc_run_attempt、source/target schema、target_id、kind/schema/运营方 success 声明均匹配。身份匹配结果不等于原始验收来源已经核验，不改写任何 Secret。
- Actions token 已成功读到 deployments 与 status API，阶段错误为 `Unknown deployment status`，不是权限拒绝。调查工具应保留新状态与记录并继续盘点，不能把未知状态改成 success 或据此恢复熔断；本次补丁仅修正诊断的前向兼容处理，固定 BLOCKED/逐项待调查不变。
- 尚未取得生产部署、VPS 未决操作或原始运营验收已闭环的证据。暂无需用户手机/VNC/凭据操作。

先读取真实报告并关联原始验收来源，逐项调查 production records；未知/失败维持 BLOCKED。调查收敛后才决定 PR #72 合并及一次必要的新 RC，按最终 manifest 重新取得真实证据。VPS 安装仍独立待办，control 待审阅哈希 `2c3b1d22e4109aef1a075a46e42b127830515ab3f3161830f91df114d5948422` 未改变；旧 PR #70 清单不能用于修复后的候选。

### 阶段 4：自动调查实际完成（2026-10-19 06:36 UTC）

[只读审计 39210813131](https://github.com/a3225865526-wq/Clear-API/actions/runs/39210813131) SUCCESS，job `117923538503`、源码 `62cd14181ae967edb3137157a153847713a07aa3`。`audit_complete=1` 仅表示读取完成；报告始终 BLOCKED / not_release_evidence。Artifact `12167820751`，`release-state-audit-39210813131-1`，SHA256 `56590c3e2acee912fa26e7a6e4199f3e770ae83e93028d2b6bd453141621329a`。无需用户触发、无需 root、无生产 environment；没有新增高权限凭据。

该代码 head 的必需 CI [39210818694](https://github.com/a3225865526-wq/Clear-API/actions/runs/39210818694) 六项全部 SUCCESS；之后仅归档本总控的实际调查结论，不改变审计、控制脚本或生产逻辑。归档后的 head CI 以 PR #72 实际结果为准，不将旧 head 的检查冒充新 head 检查。

三份当前 Secrets 都匹配已知公开历史 cd6 / RC39197505434 / bdbb digest，与当前 RC 的四项不匹配仍如上。受检的标准来源引用字段均未发现引用（`source_reference_present=0`），`original_acceptance_verified=0`；这不能扩展为“JSON 中绝对没有任何其他来源字段”，更不能把历史文档当作独立验收原件。Windows 原始测量与备份恢复材料仍是待取得项，未改写 Secrets。

GitHub production records 全量盘点 **29 条**；正式生产 workflow 的跨分支运行历史中，事故起点之后只有四次已完成的自动授权失败，无未完成的正式生产 workflow，也无插入手动部署运行。**GitHub 无运行中任务不等于 VPS 没有未决操作。**

事故起点 `2026-10-18T23:25:57Z` 之后只有 deployment **7166252463**：source=`29b6e6ff1b0d1d380b0b8940b1ac4104808f0904`，created=`2026-10-19T05:14:53Z`，所有 status 的 Actions 链接均为只读 inspection **39203542451**。API 按新到旧的状态为 `failure,in_progress,queued,queued,waiting`；waiting 是历史队列状态，不能当成当前仍在等待管理员。原 job 已 completed/failure，pull 两步 skipped，失败点为资产身份不匹配的只读 inspection。

该记录已人工逻辑核查对应源代码和原始日志（未改任何记录）：

- run attempt=1，workflow_dispatch，main，actor/triggering_actor 均为仓库 owner；唯一 job `117901393251`。
- 原日志规范化 SHA256：`d896e78cca99cb9c980d0f71214335ade746138f1053e3a0a8dc68fd6052ce23`。
- 原 inspection 源码 SHA256：`70d2f048f6f61dbc63af5b8111030c6be6f5acb60ef3714d0a175a5a3e3d17bc`；原 GHCR workflow SHA256：`afeed524d4ee00d202b513393c670e87426f2ad682f425d8770693aa16618907`。
- 已确认仅 SSH 身份/sudo list/固定资产 hash 读取；资产不匹配后未执行 Runtime/V2、安装、拉取或部署。不能将其记为“生产部署成功”，也没有删除/重写 failed record。

还复核了事故前较近的失败记录，不能把它们误写成“全部只在授权阶段”：

| 运行 | 实际失败边界（原始日志） |
| --- | --- |
| `39051474037` / `39050053489` / `39044452129` | 已进入 VPS wrapper，step 3 后 Runtime `PROBE_UNAVAILABLE`，`BLOCKED before deployment`；未到备份/切换/迁移步骤 |
| `39043761386` | wrapper 的 launch baseline 拒绝 `missing or invalid commercial pricing`，在备份/切换前停止 |
| `38911511937` | wrapper 的 launch baseline 拒绝 `unapproved enabled abilities`，在备份/切换前停止 |

这些是历史结论，不证明当前 Runtime、商品计费配置或渠道基线已经修复。历史日志观察到旧镜像 `sha256:3fbbd768...a8f8`，不能作为今天实际运行 digest。

**恢复机制边界已明确：** `.github/release-circuit-recovery.json` 的四次授权失败调查仍有效至原截止时间；但现有规则要求事故后完全没有 production record，因此会继续被只读记录 7166252463 拦截。后续如实现精确的只读记录分类，必须同时核对上述 record/run/job/源码/日志/状态链并拒绝任何新增或变化记录，保留真实数量和失败历史；不得将该记录伪装成 success 或无条件忽略所有 inspection。还必须确认 VPS 当前状态及其他门禁。本轮未修改恢复规则、未产出恢复许可、未清除熔断。

## 2026-10-19 — MOBILE COORDINATION（上一阶段记录，以顶部实际审计结论为准）

**本节取代下方所有“门禁全绿 / 仅剩 VPS root”结论。生产部署与线上验收均未取得完成证据。**
本次修改在 `codex/pg03-predecessor-compat-20261019` 审阅分支；未将修复分支当作已部署源码，未触发新 RC 或 Rehearsal。

### 当前已核实候选

| 项目 | 身份 / 真实结果 |
| --- | --- |
| main（本次查询） | `d0c20d6c46d86845805e4a9d35b067b0f6b20be4` |
| main CI | [39204691662](https://github.com/a3225865526-wq/Clear-API/actions/runs/39204691662) SUCCESS |
| 当前已构建 RC | [39205228315](https://github.com/a3225865526-wq/Clear-API/actions/runs/39205228315) SUCCESS；不重复构建 |
| Gateway 候选 digest | `sha256:98e615c08fd0697506340c2150c6e2a7e5aa53c543741ce61ebe4a190424ca65` |
| Executor 正式配对 digest | `sha256:cfad1b51928813cc6b932deb31adc71d5d23ae76fd2ea9d899bc740d1a06ae58`；不代表本次已核实生产运行值 |
| Rehearsal | [39205679007](https://github.com/a3225865526-wq/Clear-API/actions/runs/39205679007) FAILURE；未生成可用全绿 proof |
| 自动发布 | [39206211930](https://github.com/a3225865526-wq/Clear-API/actions/runs/39206211930) SKIPPED |
| VPS 只读检查 | [39203542451](https://github.com/a3225865526-wq/Clear-API/actions/runs/39203542451)：SSH 成功；两份脚本哈希不符；Runtime 未执行 |
| 实际生产 SHA / 镜像 / PG03 状态 | 本轮未取得完整运行证据，UNVERIFIED；历史旧版本健康不作为当前验收 |

### 三份 stale 证据：身份不匹配，不是时间过期

用户已取得原始 Artifact `deployment-proof-39205679007-1` 并提供三份真实 stderr；本轮已对照 verifier 的错误分支：

- backup_restore：`backup rehearsal evidence is stale`
- gateway_executor_provider_usage_billing：`provider rehearsal evidence is stale`
- safe_rollback：`rollback rehearsal evidence is stale`

`scripts/ci/verify-rehearsal-input.py` 只有在证据 `source_commit` 或 `gateway_digest` 不等于 RC manifest 时报告该 stale。该结果不是 Runtime、Provider 或备份操作本身失败的证明，也不是它们对新候选已通过的证明。

**当前三份 Secrets 的实际身份与来源引用：尚未取得可回读的非敏感元数据，UNKNOWN。** GitHub Secrets 不支持读取明文；不得从 stale 推断究竟哪个字段不符，也不得假定 Secrets 从上一次验收后从未更改。需要运营方提供各证据的 source_commit、gateway_digest、rc_run_id/attempt、manifest_sha256、schema 指纹、target_id 及原始结果引用；不需要私钥、密码、会话或 Provider token。

现有历史来源（不是当前 Secrets 的独立实测）：

- 仓库 `CLEARAPI_PG03_PRODUCTION_LAUNCH_FINAL_20261019.md` 记录三份证据曾绑定 `9a3e6585737d823896137346faf94c2e1f4d1b04` / RC `39193373708` / Gateway `sha256:95f7e50f1894171aef3eaf5c94b9e123491430c8f17d4caab3c107aa0ae79b6c`，manifest SHA `898b7b15648799b7d1b2f5e168526eb0e905c93066343e37eb05ca59517061e4`。
- 详细来源记录：备份 `20261016T193254Z` 的隔离解密/恢复；75 张表与 schema/不变量核验；候选二进制双启动；真实 Provider 链路的 usage、钱包/token 与消费日志一致性。原始文件位于 Windows `D:/AI/API/runtime/pg03-rebind-20261019/`，其中 `ev6/`、`provider-measurement.json`；当前 Cloud 无法读取该本地目录和离线解密身份。
- 后续 `cd6f719e79de432de6219561b04502784cb58930` / RC `39197505434` / Gateway `sha256:bdbb535b8826aa243eee7922a7a5e2d0fb8646f69a34653b4251ca60c488fdc6` 的 Rehearsal `39198350006` 曾成功；这是那次运行的验收，不是对现在 Secrets 或新 RC 的许可。
- 历史 rollback 记录明确 `rollback_proven=false`、`production_restore_executed=false`；不得改写为生产恢复/回滚已实测。

重新签发条件：先固定最终 main/RC/manifest；取得原始结果及可验证来源，判定与新候选的适用范围。可复用真实备份文件、已验证的固定 Executor 和无关测试结果；对受源码/二进制/schema/运行目标变化影响的验证，只补必要项目。rollback 的精确候选双启动和 Provider 当前候选真实请求结果不能仅靠替换身份字段得到。重新签发必须记录真实操作、来源和判断依据，精确绑定 source/digest/run/attempt/manifest/schema/target，并通过现有 verifier；不修改 verifier，不编造成功，不无条件复制旧 JSON。

### 前驱身份读取：修复已准备，尚未安装或合并为 main

现场 clearapi-deploy 的 sudo allowlist 不允许通用 `sudo python3 -c`。旧 workflow 在读取生产前驱 SHA 时使用了该命令，是独立权限兼容缺陷。

本审阅分支将 PG03 的相同前驱校验移到已有 allowlist 的 `pg03-release-control.py prepare`：main() 持现有部署锁，在发布任何目标/维护授权文件前，使用既有安全 reader 读取固定 root-owned manifest（拒绝 symlink、非安全属主/权限和 duplicate JSON），要求精确 40 位前驱 SHA，并拒绝 pending/failed 状态。保留原有 GitHub 授权与 RC 校验；没有新增 sudo 权限、任意路径参数或通用 root 命令。非 PG03 路径不受本修复影响。

新增回归覆盖身份错误/缺失、未知 manifest、pending/failed、固定读取路径及失败时禁止准备写入。实际本地：PG03 28 tests（26 PASS / 2 root-only skipped），automatic-release 24 PASS；完整 PR CI 另行记录，不能将本地跳过项写成通过。

### Release circuit：保持 BLOCKED

- 原始失败 `39176488931` 为 `unexpected run event`：历史策略仅接收 workflow_run，而实际手动 Rehearsal 是 workflow_dispatch。后续 `39189520883`、`39195457908`、`39198826181` 被持久失败历史拦截。
- 四份原始授权日志已复核，规范化 SHA256 全部与 `.github/release-circuit-recovery.json` 相符；四个部署 job 全部 skipped。因此可确认这四次运行没有进入生产部署 job，不能据此声称整个 VPS 无其他操作。
- PR #69 已提供受审计恢复：精确失败列表、原日志、job inventory、main 祖先关系、24 小时时效、无插入手动部署、无待调查 production deployment record；仅在当前全部 proof/RC/CI 校验完成后产出恢复审计。未删除状态、重写失败或执行恢复。
- 当前连接器拒绝 deployments API，公开网页查询也未得到记录；实际 production records 与未决操作仍 UNKNOWN。只读检查使用 production environment，其可能产生的记录也必须核查分类，不能直接豁免或当成业务部署。
- 恢复记录有效至 **2026-10-20 04:22 UTC / 12:22 Asia/Shanghai**；过期必须重新调查，不修改时间伪造新调查。

### VPS root：单独待办，不阻止上述 Cloud 工作

实际只读检查已确认两个文件不匹配：V2 为 `10d12caa...0e45d`、release-control 为 `bf6bc8cb...3189d`；其余四份资产匹配。部署账号没有安装权限，Cloud 没有其他已授权 root 身份，不能访问 Windows 本地密钥；不扩大 sudoers，不绕过 MFA。

PR #70 安装方案对当前 main d0c20d6c 的两份文件仍适用（V2 `490af9ab...18468fe`；control `e24259b8...abcb7`）。**本次修复会改变 control 哈希，故旧方案不能当作修复后最终候选的安装清单。** 最终代码冻结、审阅通过后一次性更新正式安装 pin/hash；然后由正式管理员/Server.HK 支持代装并保留备份/审计。官方工单草稿已在 PR #70，尚未提交；不假设支持方一定可代装或免费。

安装后重跑原只读 inspection 读取真实哈希和 Runtime 结果。Runtime 当前 UNKNOWN，不预先断言需要新登录；如实际 PROBE_UNAVAILABLE，仅走官方管理员交互登录/MFA。命令执行成功不等于探针 PASS。

### 最小剩余阻塞与执行顺序

1. 取得三份运营方证据的非敏感历史身份、原始验收记录与重新签发依据；只补真正受影响的验收。
2. 完成本分支前驱权限修复的必需 CI/审阅；同时取得并调查 production records。未知状态继续 BLOCKED，不提前放开熔断。
3. 一次性确定最终源码与正式资产哈希；只有最终代码改变候选身份时才产生一个必要的新 RC。现有成功 RC 保留，不能作为修复后源码的候选，也不重建相同 RC。
4. 三份真实证据绑定最终 RC 后，按既有入口验证；不无变化重复 Rehearsal、不 rerun 旧 attempt、不复用不同 source/digest 的全绿 proof。
5. root 两份资产安装、真实 Runtime/运行身份验证与熔断恢复核验分别完成；全部门禁通过后才继续正式部署。
6. 实测生产 SHA、Gateway/Executor digest、PG03 启用、真实请求/切账号/fallback、usage/钱包/token/消费日志、失败请求退款或待核对闭环、健康与回滚能力。历史隔离失败请求待核对/退款结果不等于当前生产闭环。

**最终状态：BLOCKED。没有脚本安装、Runtime PASS 或生产已部署的新增证据。**

## 2026-10-19 — 历史快照（已被上方协调状态取代）

**以下仅为历史候选记录，不能作为当前 main/RC 的验收结论。** 其中“仅剩 root”“均需 root”已失效，以本文顶部协调状态为准。

### 本次执行身份

| 项目 | 当前值 | 状态 |
| --- | --- | --- |
| 生产域名 | `https://www.clear999.com` | HTTP 200（全程未被触碰） |
| **远端 main（最新）** | **`9a3e6585737d823896137346faf94c2e1f4d1b04`** | VERIFIED |
| 本轮合并 PR | #59 schema 契约、#60 Executor 重钉、#61/#62 链路触发、#63 manual rehearsal、#64/#65 event 授权 | MERGED |
| **最终 Gateway RC** | Run `39193373708`, digest `sha256:95f7e50f1894171aef3eaf5c94b9e123491430c8f17d4caab3c107aa0ae79b6c`, Manifest SHA `898b7b15648799b7d1b2f5e168526eb0e905c93066343e37eb05ca59517061e4` | SUCCESS |
| **Rehearsal** | Run `39194957755`：**11/11 gate success，release-proof.json 已生成** | PASS |
| schema 发布契约 | 改为从 manifest 推导 `in_place_startup_migration`；10 个负向用例仍全部拒绝 | FIXED |
| 三份运营方证据 | backup / rollback / provider，均先过真实 verifier 再安装；绑定 RC `39193373708` | PASS |
| Executor 分支 / commit | `codex/pg03-executor-mvp` / `b6e5a90fce6c0b3fa231d43ab07a65622f981be2`（tip `945fc3ee` 内容相同） | PUSHED |
| Executor digest | `sha256:cfad1b51928813cc6b932deb31adc71d5d23ae76fd2ea9d899bc740d1a06ae58` | VERIFIED |
| Executor binary SHA256 | `e99b4dceee9b857c67e7b2b5101552511dbce60c789b2bec3c8fdd6bb5a33523` | VERIFIED |
| 自动发布熔断器 | `39176488931` / `39189520883` / `39195457908` 三次 failure（均在授权步骤，部署步骤 skipped）后**持久打开** | BLOCKED |
| VPS 已安装脚本 | 与当前 commit **不一致**（本轮改过 `production-deploy-v2.sh` / `pg03-release-control.py`） | NEEDS ROOT REINSTALL |
| PG03 生产启用 | 未执行 | NOT RUN |
| 线上验收 | 未执行 | NOT RUN |
| 熔断 / 双账号故障转移 | 隔离环境实测通过（本轮及上轮） | PASS (ISOLATED) |

### 剩余阻塞（均需 VPS root）

1. 重装 `production-deploy-v2.sh`（`490af9ab74b354de17075f9c01b66a4b88fa63ca376c3730da550d18818468fe`）与 `pg03-release-control.py`（`9dce204701e835c68633ed27859f90c40b8c43ccf75b7f83e12b006e4bb666f4`）到 `/opt/clearapi/bin/`，root 属主、`0750`。
2. 以 root 经真实交互式登录 + MFA 生成 `/opt/clearapi/secrets/runtime-probe-session`。
3. （仅自动路径额外需要）确认无影响后恢复发布熔断器。

详见 `CLEARAPI_PG03_PRODUCTION_LAUNCH_FINAL_20261019.md` §4.4 与 §8。

## 2026-10-18 — RESUMED — FULL PG03 PRODUCTION LAUNCH（历史）

**PG03 = RESUMED — FULL PG03 PRODUCTION LAUNCH.** 本节为 2026-10-19 之前的状态，历史正文原样保留。

### 本次执行身份

| 项目 | 当前值 | 状态 |
| --- | --- | --- |
| 生产域名 | `https://www.clear999.com` | HTTP 200（本轮公网只读核对） |
| 远端 main（最新） | `353cd0a23b723f86c6b21e759a6d691ce1ba5457` | VERIFIED |
| Gateway 工作树 | `D:/AI/API/worktrees/clearapi-pg03-full-launch` | ACTIVE |
| Gateway branch commits | PR #52 `1c2c555a`, PR #53 `fe3cf9cb` + integration test `f235c089` | MERGED/PUSHED |
| Executor 工作树 | `D:/AI/API/worktrees/pg03-executor-mvp` | ACTIVE |
| Executor 分支 / commit | `codex/pg03-executor-mvp` / `228ecbaa6f71ade146be789c9a3c42522a7f92a8` | PUSHED |
| Executor RC | Run `39079462722` | SUCCESS |
| Executor digest | `sha256:9ba0e5ffb0ccbcf4f1f23d272e4c1b7bb0fe86af93429c823928358ff99d34e5` | VERIFIED |
| Executor binary SHA256 | `6dff0b07e6b231abbe5a4845f2f4e588d9b3252c3d84b33f75b3ed79d336240b` | VERIFIED |
| 自动部署 | final main `353cd0a2`; Rehearsal attempt 2 blocked only on Provider evidence; AUTO variable absent | BLOCKED / NOT READY |
| 网站 JSON 导入 | 前端、Gateway API、Executor 原子导入已开发；multipart route test PASS；未部署 | LOCAL PASS / PRODUCTION NOT RUN |
| Gateway RC | Run `39083968671`, digest `sha256:bfdf5b842f9a9344cfb1eff1186be4962520bfbaceec72b5bd547432a3ac374d`, Manifest SHA `896c8c2731fe43a4b3068ec7d656a06975c4c9ecdd602ee86a7ad4b4b602a02d` | SUCCESS |
| Backup / rollback evidence | actual encrypted backup `20261016T193254Z`, 75 tables, exact RC twice, 0 invariant failures | PASS ISOLATED |
| Rehearsal | Run `39084286400` attempt 2: all non-Provider gates PASS; proof blocked on Provider | BLOCKED |
| PG03 production enablement | Not executed | NOT RUN |
| Post-main test branch | `codex/pg03-rehearsal-fix-20261018`; head `0023088c`; no production switch | NOT MERGED |
| 真实 Provider Usage/Billing | 尚未执行 | NOT RUN |
| 熔断 / 双账号故障转移 | 尚未执行 | NOT RUN |

### 网站 JSON 导入

- 支持格式：顶层字段 Codex OAuth JSON，`type=codex`，必需 `access_token`、`refresh_token`、`account_id` 或 `chatgpt_account_id`、`email`。
- 安全边界：服务端解析与暂存、单文件 256 KiB、最多 10 文件、批次 1 MiB、重复 key/嵌套 collection/API key 拒绝、固定错误码、Token 不进入响应/日志/浏览器持久化。
- 流程：Web 上传 → Gateway 校验 → Executor 原子上传 → Pool 登记 → trust/capability 回读；导入不会自动标记 verified。
- 本地验证：Gateway `service/accountpool` 单测通过；Router 控制面测试通过；Executor 原子导入测试通过；前端 `tsgo -b` 与生产构建通过；本次改动定向 lint 通过。
- 未部署到生产；需要新的 Gateway RC + Executor RC 配对和正式 Deploy V2。

### 当前待完成顺序

1. Executor RC `39079462722` 与 digest/binary/manifest 已完成并写入上方。
2. Gateway PR #52、#53 已完成 Required CI 并合并；额外 multipart route 测试在修正分支 `f235c089`。
3. main `353cd0a2` Required CI `39083507403` PASS；正式 Gateway RC `39083968671` PASS。
4. Rehearsal backup/rollback/executor/migration/contracts PASS；自动 proof 仍缺真实 Provider evidence，Deploy V2 尚未执行。
5. 使用获准测试凭据完成网站导入、Pool、Route Chain、Executor、Usage/Billing 生产验收；个人 Codex OAuth 未被确认可用于商业中转时不得作为商业上游启用。
6. 无法确认商业用途或缺少第二个合格账号时，记录为 BLOCKED，不得伪造全通。

### 最终分类 — 2026-10-18

**PARTIAL — BLOCKED ON REAL PROVIDER EVIDENCE. Production was not switched.**

- 最新 main `353cd0a2`、Required CI `39083507403`、Gateway RC `39083968671`、Executor RC `39079462722` 均 PASS。
- 网站 JSON 导入代码、Executor 原子导入、multipart route test 与前端构建均 PASS；生产未部署。
- 真实备份 `20261016T193254Z` 隔离恢复及精确 RC 双启动 PASS；生产未执行恢复/切换。
- Rehearsal `39084286400` attempt 2 除 Provider 外全部 gate PASS；Assembler 拒绝生成 proof。
- 两个上传账号均为 `free` 计划且未确认商业用途；不得作为已获准商业上游。
- 自动部署 Run 因 proof 未生成而 skipped；PG03 生产 enablement、Route Chain 生产请求、Executor 部署、真实 Usage/Billing 均为 NOT RUN/UNVERIFIED。
- 完整报告：`CLEARAPI_PG03_FULL_LAUNCH_FINAL_STATUS_20261018.md`。`CLEARAPI_MASTER_STATUS.md` 为工作区副本，已随 `0023088c` 推送。
- 证据：`D:/AI/API/evidence/pg03-full-launch-20261018/`。

### 历史记录

以下为 2026-10-16 冻结及更早历史，原样保留，不代表本次状态。

## 2026-10-16 — OFFICIAL FREEZE / HANDOFF

**PG03 = FROZEN — Frozen Integration Candidate。** 本节取代下方历史 BLOCKED 状态及继续上线的 Next Action；历史记录原样保留。冻结原因是战略调整，不是失败，也不删除、回退或重写已完成成果。

- 优先顺序：自动部署 → 商业化 MVP → 恢复并最终整合 PG03 → 统一生产上线。
- 冻结 main：`0a5884fd894b0dac1fae5073e4d37265970df530`。
- 正式 RC：Run `38906184793`。
- candidate digest：`sha256:569de0e04a63fd51a601ad38675b4cb9529ffdeaec5d42c6a3dc8580bf919523`。
- 已完成成果保留：Route Chain、Account Pool、admission / capability、Executor、Runtime Integrity、Billing Reserve、CreditCatalog、RedemptionBatch、Referral、迁移验证、Required CI、RC 身份验证、Executor 健康检查、备份恢复验证。均为既有证据，本轮不重跑。
- 已确认 SSH / sudo / 应用 Root Dashboard 认证及 main / RC / digest 绑定通过；不是认证失败。
- 遗留 maintenance request = EXPIRED，工具无合法 replacement/reissue 命令；旧生命周期文件阻止再次 review。**暂不处理，不再修复 legacy transition，不创建新请求，不续期或复制旧 session。**
- 生产尚未切换 PG03，Channel 2 保持禁用；这是用户确认及历史检查状态，本轮未连接 VPS 或重新核验在线健康。
- 主线工作树：`D:/AI/API/worktrees/pg03-mainline-integration-20261015`；branch `codex/pg03-rc-containerd-20261016`；本轮本地核实 HEAD 与冻结 main 一致。
- 会话工作树：`D:/AI/API/upstream/new-api`；branch `codex/pg03-account-pool-mvp`；HEAD `fd98c9aad0374581e46e5ba13bcf18ec96046559`，与冻结 main 分开记录。
- 冻结交接完整索引：`D:/AI/API/worktrees/clearapi-master-status-20261016/CLEARAPI_MASTER_STATUS.md`（本地总控维护工作树）；本目录既有九份 PG03 报告均保留，未提交文件、代码、分支、工作树及历史不清理。
- 本轮仅更新上述两个本地总控 Markdown；没有业务代码、生产、数据库、Channel 或 Gateway 变更；没有部署、RC 重建、commit、push、force push。

恢复条件：自动部署完成；商业化 MVP 完成；最新 main 稳定；自动 CI / RC / Deploy V2 稳定；能够生成新的完整 RC；再最终整合 PG03；最后统一生产部署。恢复必须另行授权并重新核验维护协议和运行时，不沿用过期请求或历史 RC 作为未来整合版本许可。

**冻结边界：不改 Account Pool / Route Chain / Executor / Runtime Integrity / Billing / Channel / Gateway / legacy transition；不申请生产部署、不启用 Channel 2、不重建 RC、不删除 PG03 代码/分支/工作树、不 squash / reset / force push。冻结完成后停止。**

### PG03 主线本地冻结交接补记（2026-10-16）

- 当前执行工作树为 `D:/AI/API/worktrees/pg03-mainline-integration-20261015`；branch `codex/pg03-rc-containerd-20261016`；HEAD `0a5884fd894b0dac1fae5073e4d37265970df530`，已本地核实。
- 冻结报告：`CLEARAPI_PG03_FREEZE_HANDOFF_REPORT.md`，保存九份既有报告的 SHA256、PG03 工作树/分支入口、历史证据目录及恢复依赖。
- 本地 `refs/heads/main` 仍为 `ad71806fd00518855d4f7d27d72c9d3f846e2cbb`，不存在 `refs/remotes/origin/main`。这与当前分支 HEAD 分开记录；用户确认的冻结 main 为上列 `0a5884fd`，本轮没有联网查询或更新本地引用。
- 上方“本轮仅更新两个总控”和“会话工作树”属于先前冻结记录。本次仅补记当前目录总控并新增冻结报告，不修改总控维护工作树；历史正文、九份既有报告、代码、未提交文件、分支及工作树保留。
- 本次仅作本地状态与文档保全核对；不重跑历史 CI、数据库或恢复验证，不访问生产。Root / SSH / sudo / RC 绑定 PASS、生产尚未切换及 Channel 2 禁用沿用用户确认和已有证据。
- **PG03 = FROZEN；旧 maintenance request 问题暂不处理。** 自动部署和商业化 MVP 优先，达到上述恢复条件后再另行恢复。本次没有 commit、push、force push、RC 重建或任何生产/数据库/Channel/Gateway 变更；交接完成后停止。

---

以下为冻结前历史记录；其即时修复/部署行动已暂停，历史生产信息不视为本轮复核。

更新时间：2026-10-16 22:05:57（Asia/Shanghai；末次只读核对 14:05:57 UTC）

最终状态：**BLOCKED**。生产 Gateway 尚未切换，网站未更新为 PG03 RC。

## A — PG03 Production Launch

| 项目 | 当前真实状态 |
| --- | --- |
| PR #42 | MERGED；Head `fbe21405`，Merge `2302c59c`；发布修复 #43 / #44 已合并 |
| 当前 main | `0a5884fd894b0dac1fae5073e4d37265970df530`，本轮再次核实，未修改 |
| Required CI | `38905305045` SUCCESS，匹配 main；未重跑 |
| 正式 RC | `38906184793` SUCCESS，匹配 main；未重建 |
| Candidate digest / image ID | `sha256:569de0e04a63fd51a601ad38675b4cb9529ffdeaec5d42c6a3dc8580bf919523` |
| RC Manifest SHA256 | `550efc411f3ae8f044d143c3383a8520b411d165dddb827600a6f019862bb0d7` |
| 发布身份 | main / RC / digest / VPS image ID / OCI revision / 二进制摘要均匹配；不需要重建 RC |
| 应用 Root 认证 | 用户已合法登录；真实 Runtime 接口接受会话，旧版本返回 `503 / APPROVAL_VIEW_INVALID`；不是缺少账号认证 |
| Maintenance request | `7f426779efc3cf2fd2be8a157ccba70f26687e03f53a7f747e5e9b6530dc63c5`；13:52:48 UTC 创建、14:02:48 UTC 到期；14:05:17 UTC 核对时已过期 149 秒 |
| Production Run | [38911511937](https://github.com/a3225865526-wq/Clear-API/actions/runs/38911511937)，自动启动，FAILURE，未取消；13:55:33 UTC 被旧渠道基线拒绝，未进入维护/备份/切换 |
| 生产 Gateway | 仍为 `da0dcfcd7062875163bff4faa6bc70377b1db890` / digest `sha256:3fbbd768d71d8e2d4ea96ebd0ef010bc75f7a7c3e34e9b788df334d88ed6a8f8`；未替换或重启 |
| VPS / Executor / 数据库 | VPS 可用；Executor healthy；PostgreSQL 17.6 healthy；未迁移或重启 |
| Website | 末次公网首页及 `/api/status` 均 HTTP 200，TLS 校验 0；属于旧版本健康 |
| Backup | 正式加密备份 `20261016T140214Z` 已验证；原备份保持；timer active |
| Channel 2 / 3 | status=2，启用 abilities 均为 0；全部渠道及路由记录保留 |
| 维护 / 回滚 | 未进入维护、未退役 apply、未暂停 Caddy、未切换 Gateway、未回滚 |

当前最小直接阻塞：有效的一次性 maintenance request 不再存在。原请求已过期，且在用户随后明确授权禁用 Channel 3 后，冻结的渠道指纹不同。Root 登录已经成功；业务配置变化不是 RC 镜像身份变化。

下一步：以当前运行时状态对应的合法有效维护请求，继续同一个 main / RC / digest。现有 helper 没有续期或重绑定入口；不得改写旧请求、延长时间戳、伪造认证或忽略有效期。本轮不再次索取凭据，不触发已知无效的新 Run。用户最新指令后不再变更、删除或重置任何 Channel、商品、路由或历史数据。

## B — Autonomous Deployment

Status: **BLOCKED**（自动发布授权已实际通过，阻塞为维护请求有效性）

- production required reviewers 已取消，仍仅允许 main；保留 Required CI、RC/Manifest、镜像、Runtime、Billing、Backup 和后置健康验证。
- Run `38911511937` 已通过 owner-dispatch 自动授权、脚本完整性和精确 RC/Manifest 绑定，无人工 Approve 等待。
- 本轮无发布代码变更，无新 commit / PR / main / RC。既有 RC 仍匹配，不能因业务配置变化重建。
- 原失败原因：Channel 2 的 9 条 stale enabled abilities，以及启用的 Channel 3 / ability 不符既有商业基线。
- 用户随后明确要求关闭或删除渠道时，采用禁用并保留记录：事务纠正 Channel 2 abilities，正式管理 API 禁用 Channel 3 并刷新缓存；未删除数据。
- 变更前已执行并验证正式加密备份。变更后除 `channels` 指纹外，维护快照中的账务、Schema、Pool 和其他保护字段全部保持。
- 最新只读确认 Channel 1 启用能力=1，Channel 2 / 3=0；不再存在 unapproved enabled abilities。
- 请求时效及冻结快照是维护协议，独立于 Release Artifact 身份。未伪造或绕过协议；未创建额外失败 Run。

## C — Commercial Readiness

Status: **BLOCKED**（首次生产切换尚未完成）

按最小上线路径执行，未新增企业级、复杂商业或逐模型验收。新 Gateway Runtime PASS 和生产更新尚未发生；Channel 2 保持禁用，不为宣布成功强行开启或读取用户 Token 做请求。非关键配置改进后置；不恢复最近网站配置来迎合旧快照。

完整记录见根目录 `CLEARAPI_PG03_AUTONOMOUS_PRODUCTION_FINAL_REPORT.md`。此文件是唯一项目总控状态文件。


## 2026-10-21 — PG03 隔离演练实现独立只读复审：BLOCKED（最新审查）

本节为本轮最新审查结论，历史记录原样保留。报告追加在 `evidence/pg03-isolated-rehearsal-local-implementation-20261021.md` 的“独立只读审查”部分。负责人已明确允许只追加本报告与总控；没有修改实现或测试。

- **判定**：BLOCKED，当前不建议将此实物版本提交注册审批；从事件/权限隔离角度的条件安全路线仍成立，但两项 P2 验收正确性问题需负责人决定处理，未发现 P0/P1 生产直达路径。
- **F1 / P2（已用临时故障探针复现）**：`scripts/ci/pg03-isolated-rehearsal.py:160–171` 先发布 result=success/receipt_present=true 再写 success.json；后一步 I/O 失败时公开结果仍成功、凭证却不存在。job 会失败，但 always 上传可保留这份错误成功摘要；现有测试未覆盖，代码未修复。
- **F2 / P2（源码确认的检测缺口）**：`scripts/pg03-schema-rehearsal.py:143–182` 的健康/退出/schema 门禁没有迁移错误状态采集；固定候选源码存在只记日志不阻断的迁移错误路径，同 hash 不代表所有迁移/数据回填成功。真实候选未运行，不能声称现场已发生此错误。
- **已验证**：31 项离线测试重跑 OK；10 YAML 解析、4 Python AST、7 shell 块语法及差异检查通过；另有两次 start 超时替身均不产成功 receipt。actionlint/shellcheck 未安装、未运行，完整 CI/真实数据库/演练未运行。所有受审代码、workflow、测试、设计文档字节保持本轮开始值。
- **远端只读新鲜事实**：main 仍 `69a66431646b5cc8912bf97a739e08de07316443`，已注册 9 workflow，新任务尚未注册；两个自动开关仍 false，main 三项严格 required checks/enforce_admins 保持；查询时 in_progress/queued/waiting 各 0。固定 RC/artifact 元数据仍有效，两 Action SHA 经官方 commit API 确认存在；未读/拉镜像，未下载 RC ZIP。此快照不代表未来窗口持续安全。
- **真实状态不变**：镜像读取 NOT RUN，两次启动 NOT RUN，schema/迁移结果 UNKNOWN，PG03 BLOCKED。固定 RC 目标 DDL 不代表新鲜生产快照或数据行验收。

下一步仅由负责人决定 F1/F2 修复范围；修复并复审后才能建议注册。即使以后批准注册，实际拉取与双启动仍须另行明确审批；本轮无提交/推送/PR/注册/触发 Actions、真实数据库、生产 SSH/配置/数据操作。此轮到此停止，不执行下一阶段。


## 2026-10-21 — PG03 两项 P2 本地修复：P2-1 PASS / P2-2 BLOCKED（最新）

完整证据追加在 `evidence/pg03-isolated-rehearsal-local-implementation-20261021.md` 的“两项 P2 本地修复”章节；原独立审查发现保留，不以新测试改写历史结论。

- **P2-1 PASS（Linux 本地）**：私有暂存 → 中间观察 flush/fsync → 成功凭证 flush/fsync → 最后结果 flush/fsync → 目录同步/原子发布/父目录同步；失败改发 failure_or_unknown 或直接非零，不留半成品在上传路径。旧 public 只在当前 run 根下改名保留，并使本轮失败。新增独立回读同时核对 finalizer outcome，成功回读才许可上传，旧目录无法隔离时也不能沿用旧 success。发布后不再执行私有 state finally 写入。
- **P2-2 BLOCKED**：固定候选 SysLog/SysError 同格式且忽略 writer 错误，没有可靠迁移完成契约。新隔离模式已采集 stdout+stderr、按实例/source/记录语法识别已知迁移错误；exit 0/schema 不变仍可被拒绝。日志不足/无法解析/缺完成证据均 UNKNOWN，schema 冲突为 failure；不存在以“没搜到错误”放行的分支。当前候选没有可证明 success 的输入，本轮不伪造或更换 digest；仍缺正向迁移成功证据生产方案。
- **验证**：WSL Linux 最终 46 tests OK，无 skip；Windows 46 项中 37 通过、9 项 Linux 持久化明确 skip。四 Python AST、10 YAML、8 shell 块语法、回读上传门禁与空白/差异检查通过；原 9 workflow 字节未变。actionlint/shellcheck 未安装未运行。没有真实 Docker/数据库/迁移/镜像验证，所有此类测试使用替身。
- **范围**：仅新独立 workflow、adapter/schema/successor、集中测试及这两份阶段文档，共 7 文件；保留既有工作区修改，不改生产 workflow、Go 迁移业务、配置/Secrets/包权限/开关。没有提交/推送/PR/合并/注册/触发 Actions、镜像拉取、数据库启动或生产 SSH。

具备申请新一轮独立审查的本地条件，审查须同时确认 P2-2 的未闭环边界；**不具备注册/真实运行就绪条件，PG03 仍 BLOCKED，不是 READY。** 实际镜像读取与两次启动均 NOT RUN，真实 schema/迁移 UNKNOWN。完成本轮后停止，等待审阅，不执行下一阶段。
