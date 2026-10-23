## 2026-10-20 09:33 Asia/Shanghai — 新会话续办：恢复代码与条件开口已准备，生产仍521/未写入；等待最终CI/RC/排练

- 只读09:14实核：原Gateway f8f963a7/22ec退出127，/run为tmpfs、/run/clearapi缺失；deploy.env显式覆盖为/run/clearapi，现有机制仅部署时prepare，无boot创建。PG/精确Executor健康；原Caddy54c8550b停止/restart=no，80/443无监听。公网末次复测521，未恢复网站。
- 原FAILED/target/hold/overlay哈希、schema3c1c77f3、账务[2 live review,3历史review settled,10finance review,0outbox]均匹配。未改账/退款/删记录/改DB或配置，未启动服务/开放入口/清熔断。
- PR77新增必要修复：授权stdin FD生命周期；严格原停止容器恢复、append-only attempt及失败停回；tmpfiles→Docker前目录检查→Docker后真实identity reconcile；独立owner restore在完整门禁及原Caddy身份/configSHA通过后才开口，独立result如实区分页面可用与真实登录未测。当前head ff6e0f4b2f5096d12717a01afbb3f5d90d28b801，最终CI39354033185 pending；未合并、未生成新RC。此前c439及中间head CI仅保留历史，不冒充当前绿。
- 本地35恢复/开口tests PASS；boot9旧聚焦cases+Docker inactive拒socket-activation新增1 PASS；bash/shellcheck/diff check PASS。同镜像既有双启动/账务证明复用。此次精确新备份20261020T002138Z真正本机隔离恢复PASS：9项成员hash、schema578bc6f8、账务[2,3,10,0]匹配，owned容器清理；key未导出、无明文文件。初次metadata adapter失败保留，修正后独立结果PASS。
- 生产写入仍必须单独批准。可审查集中方案：runtime/clearapi-unified-final-20261019/launch-current/production-39347012653/RECOVERY_APPROVAL_PLAN.md。范围拟包括审计安装root+4boot资产、同Gateway恢复、隔离下单次VPS reboot验证、全门禁后原Caddy条件开口；当前均NOT EXECUTED。未来unsafe runtime将阻断Docker启动，明确纳入影响；失败不自动恢复DB或清latch。
- 新硬阻塞已查明：原controlled hold无opening入口（PR77已补独立restore）；最终部署仍需incident-successor消费FAILED predecessor/新receipts，不能复用authorization-only自动熔断合同。网站恢复不等于最终PG03正式部署成功。最终部署及入口开放仍需第二次明确生产授权；Provider完整业务验收按用户决定后置，不填PASS。

---

# ClearAPI — 权威总控（MASTER STATUS）

## 2026-10-20 09:07 Asia/Shanghai — 紧急只读诊断：公网521，重启后Gateway因/run/clearapi缺失退出127；未恢复或开放

- **当前状态取代此前running基线：** VPS08:46:30重启，Docker08:46:44尝试启动Gateway失败。State.Error与Docker日志均为`failed to fulfil mount request: open /run/clearapi: no such file or directory`；Gateway f8f963a7/22ecaa46已STOPPED，exit127/OOM=false。/run为tmpfs，配置身份目录=/run/clearapi且目录不存在。
- Caddy54c8550b仍按原controlled journal停止，restart=no，无TCP/UDP80/443监听。源站loopback443与Gateway3000均curl7/HTTP000；外部首页、登录、后台overview、api/status均Cloudflare521且有CF-Ray。UFW44条Web ALLOW，IPv4 Web ACCEPT计数5包/300bytes；IPv6链查询exit1，不声称已验证其内部规则。
- Executor1081a1/cfad与PostgreSQL08:46:43重启后healthy；Executor实际binary e99b4dce、源码b6e5a90f与target一致，网络/alias正确。host-bridge健康与真实认证均200，配置凭据仅比较布尔均匹配。Gateway已停止，当前其DNS/认证路径不能执行，docker exec1不能解读为DNS或认证失败。
- DB schema3c1c77f3、finance9、review2/3/10、outbox0及严格账务保护均保留；manifest6c308146、target521e60ce、hold57a7152a、overlays及FAILED attempt原字节未变。原加密备份12323351bytes/SHA0c8757c8及manifest a9b9c32c完整，旧51dd/eb67镜像仍在。
- 回滚前置未满足：正式wrapper排除PG03自动回滚；原真实回滚证明依赖旧DB+旧镜像恢复，当前仅切旧镜像对新schema的兼容性未证明；schema不同本身不证明不兼容。未覆盖DB、未换镜像、未解除隔离或强清熔断。
- 原始PG03失败仍为Wget管道config exit2、HTTP发出前失败；重启前真实常规私有config认证成功且原健康200。PR77/c439f9bfb本地105项、CI39349733643六项PASS，仍OPEN/未合并；用户收紧只读后未继续merge/RC，未安装新生产资产。PR当前恢复前提是Gateway运行，不能原样用于此重启后停止现场。
- 下一恢复阶段需审计/验证“既有身份工具重建受保护/run目录→启动同一精确Gateway→刷新实际新进程身份→完整门禁”，并单独批准生产写入；启动可能触发现有启动迁移/后台处理，失败应停回新启Gateway并保持隔离。该步骤本身不恢复公网；旧版公网恢复另需证明不覆盖DB的回退路径和审核入口恢复。当前没有生产恢复执行。
- 详细报告：`runtime/clearapi-unified-final-20261019/launch-current/production-39347012653/HOST_ERROR_DIAGNOSIS.md`。最新机器状态为该目录`recovery-live-status-latest.json`，优先于旧readiness/plan快照。

---

## 2026-10-20 08:46 Asia/Shanghai — FAILED恢复开发已授权；公网521，原自动回滚不适用，最小恢复候选定向验证中

- 用户授权新增最小审计FAILED恢复流程及必要提交/PR/CI/RC/Rehearsal；新机制任何生产写入须另行批准。未安装新资产、未执行恢复或开放入口。
- 08:34—08:35只读复核：公网HTTPS521；Caddy仍stopped/restart=no；Gateway f8f963a7/22ecaa46与Executor1081a1/cfad运行、Gateway restarts0，root隔离校验PASS；review2/3/10与outbox0、严格账务配置不变。冻结manifest SHA6c308146、target521e60ce、hold57a7152a、overlay及原备份a9b9c32c。
- 实际DB schema已为3c1c77f3（新候选目标），旧版为578bc6f8；已验证真实回滚需数据库+旧镜像。现有wrapper禁止PG03自动回滚，因此不执行未支持的image-only回滚，也不直接恢复生产数据库。已保留原失败记录和现场哈希。
- 本地分支codex/clearapi-failed-recovery-20261020：Wget0600临时配置/清理修复；新增只读failed-postflight模式，复用原完整基础门禁；新增严格绑定这次incident的owner-authorized恢复流程，仅追加独立恢复receipt，FAILED原字节/目标/隔离日志不改，不换镜像、不改账或清熔断/开放入口；无sudoers变更。
- 最小验证：新增真实Wget/认证失败/重定向拒绝/无凭据argv/清理及恢复拒绝测试20项PASS；原V2定向49项PASS；原PG03合同36项PASS；bash语法、shellcheck PASS。一次初始PG03执行cwd错误导致4个路径失败，改正确仓库cwd后通过；一次新增fixture先触发WSL挂载目录安全拒绝，修正仅测试fixture后通过，未放宽生产权限检查。
- PR77已创建并关联本任务，head c439f9bfb13fa3f1a5ce59597e6e83b007bccd6b；必需CI39349733643运行中。7个聚焦文件（wrapper已有17/18步骤提取复用），无应用/依赖/锁文件修改。中断后核实PR及附件已成功创建，未重复创建；生产恢复未执行。
- 下一步：正常PR/必需CI合并→一次必要新main RC（仅资格审查变更后的恢复资产，其Gateway镜像不由此恢复操作部署）→同源正式FAILED恢复Rehearsal，原运行候选RC39339871064/排练39340908922/备份回滚证据原样复用。全部可审查后集中申请安装3份root资产及执行既有实例postflight恢复的生产批准，保留公网隔离。

---

## 2026-10-20 08:33 Asia/Shanghai — 正式部署39347012653 FAILED且已隔离；新版Gateway运行，Wget验收命令缺陷待审计恢复

- **如实状态：** 正式authorize SUCCESS，production job FAILURE/attempt1。08:26已替换为指定Gateway22ecaa46/c50a53c1，实际container f8f963a7df5f、binary40ebc706、running/restarts0；不能声明部署成功。release manifest及attempt9e37b9c3-9987-453f-bd49-f4a28fa77c81均保持FAILED/blocked。原旧镜像保留，未执行不受支持的PG03自动回滚。
- **已经通过：** 正式部署前Runtime、加密备份20261020T002138Z（12323351bytes，SHA0c8757c8d204cddd7cc5b3b9a7835823e376093b9ee344f5a8bd8c219d3b3b31）、候选digest/revision/binaries/bootstrap；替换后localhost健康、商业配置和Runtime Gate PASS(after) 08:26:17。已有CI/RC/backup_restore/真实rollback/rehearsal证明直接复用，无无必要重建或重复验收。
- **实际失败原因：** PG03 Gateway→Executor验收把GNU Wget --config指向管道/dev/stdin。镜像GNU Wget1.21.3对空配置管道也exit2（Exiting due to error in /dev/stdin），/proc/self/fd/0同样失败；普通0600临时配置则成功。原命令在发出认证HTTP请求前退出。健康原命令HTTP200；限定只读真实认证请求改用安全临时常规文件后exit0，文件已清理、无凭据/响应导出、无Provider业务请求，不能据此手改原FAILED为PASS。
- **安全/账务现场实核08:30：** root TrafficHold.verify PASS（target521e60ce绑定原run39347012653），Caddy stopped/restart=no，Gateway仅loopback、无TCP/UDP80/443、PG无发布端口；Executor1081a1/cfad实际身份与健康PASS。严格预付/钱包/outbox/单实例保护均true，实例1，无Redis/批写；2 live needs_review、3历史review-marked settled、10 finance sync needs_review、outbox0全部原样。未改账、退款或删除记录。
- **硬阻塞与范围：** 现有pg03_load_pair(current)拒绝FAILED；root predecessor校验拒绝FAILED/未解决attempt；controlled journal禁止普通部署/restart/reconcile，且无正式failed-recovery入口。不得重跑原失败run、伪造success、删除journal或强清新发布熔断。原生产授权覆盖精确RC部署及既有支持的失败停止/回滚，不等于新增失败恢复机制或新候选授权。
- **最小合规修复草案：** Wget使用umask077/mktemp常规文件与退出清理，凭据不入argv/log；更新V2固定哈希并正常PR/CI。按当前仓库发布合同，合并发布脚本变更必须必要新main RC；旧证据身份不可改绑，已有效证据保留。另需对该FAILED现场建立经审计的恢复方案，继续入口隔离，验证剩余必要基础门禁。当前仅保存review patch/scope，未改仓库或安装脚本、未恢复或dispatch第二次。
- **证据：** launch-current/production-39347012653/{failure-summary.json,paired-connection-diagnostic.json,wget-stdin-reproduction.json,authenticated-connection-regular-config.json,recovery-scope.json,proposed-wget-probe-fix.patch}；controlled-postflight-39347012653.json。下一步是单独确认失败恢复范围，不重复原部署授权或要求Runtime重新登录；五家Provider业务验收及开放流量继续后置。

---

## 2026-10-20 08:23 Asia/Shanghai — 正式受控部署39347012653运行中；公网入口已持久化隔离

- authorize job SUCCESS：真实controlled proof与原调查熔断恢复资格由正式入口认证；recovery-audit.json已下载归档，无强制清熔断。生产job已通过root工具身份及指定配对prepare，正在V2部署步骤。
- VPS只读实核：Caddy running=false/restart=no，controlled-launch-state.stage=isolated，公网隔离已执行并落盘；旧Gateway51dd仍running、manifest仍eb67成功基线，尚未替换。不开放流量。
- 同一授权范围连续推进；RC/排练/已有效证明不重跑，账务记录保留。不得把运行中标为成功。

---

## 2026-10-20 08:20 Asia/Shanghai — 已授权生产准备完成；本人登录后Runtime恢复PASS，进入正式controlled部署

- 一次生产部署授权已收到且持续有效，不再请求重复授权；不含客户流量开放。
- 指定Executor已真实启动，container1081a1b576ad、digest cfad1b51、binary e99b4dce、revision b6e5a90f、内部网络及健康门禁PASS；旧Gateway2c28b454/51dd仍未替换。
- 旧过期target SHA61e720已原字节归档至/opt/clearapi/app/.release-authority-archive-61e720348762/pg03-target-manifest.json；原路径已移走，审计/归档落盘，未改证据身份。
- 08:17实际401后仅请求本人认证；用户完成登录，08:19安装版V2 --verify exit0、identity MATCH、Runtime PASS，无会话内容导出。此前401历史保留，不重复已有效证明。
- 正式production workflow已dispatch：39347012653，workflow_dispatch/main/attempt1，授权校验运行中；尚未声明部署成功。
- main c50a53c1未变化，现进入正式deploy-production.yml controlled dispatch，沿用RC39339871064/排练39340908922及全部有效证明。尚未替换Gateway、尚未停止Caddy；正式V2必须在替换前持久化阻断入口并保持关闭。不得改账或强清熔断。

---

## 2026-10-20 08:14 Asia/Shanghai — 已获一次明确受控生产部署授权；准备配对Executor，客户流量不得开放

- 用户已明确回复“授权受控生产部署（不开放流量）”，授权覆盖RC39339871064/排练39340908922、指定Executor启动与实核、旧target原样归档、正式controlled Gateway部署及既有备份/必要迁移/受支持失败停止或回滚。此授权持续有效，无需再次申请同一范围授权；不含客户流量开放/退款改账/强清熔断。
- 候选/证明沿用main c50a53c1、Gateway22ecaa46、Executorcfad，已有CI/备份/真实回滚/受控排练不重跑、不重建。07:37 Runtime PASS之后工作曾中断，恢复先核实真实状态；只有实际401才通知本人再认证。
- 第一次只读Executor预检误假设运行标签只有一份Compose文件而断言失败，未发生服务变更；实际标签为docker-compose.prod.yml＋历史compose-candidate.yaml。正式V2明确使用生产base＋准备好的compose-pg03.yaml，已按该精确配置重新预检，无门禁放宽。
- 当前尚未执行production workflow dispatch，未归档旧target、未替换Gateway、未停止Caddy。后续在部署锁内启动指定Executor并核验实际身份/健康，再原样归档过期target，随后正式controlled dispatch；失败保留证据并停止或按已有方案回滚。

---

## 2026-10-20 07:37 Asia/Shanghai — 本人登录后Runtime真实PASS；复用全部有效证明，现仅待一次受控生产部署授权

- **用户决策确认：** 最精简验收；有效且身份匹配的CI/备份/回滚/Runtime直接复用，不重复测试；仅实际401才请求本人认证。五家Provider真实请求/usage/billing/账号切换由用户部署后自行验收，部署后不开放真实客户流量。
- **Runtime当前真实PASS：** 用户完成官方登录后，07:36—07:37安装版V2 --verify exit0，运行/Compose/manifest identity MATCH、Runtime Gate PASS；会话有效，未要求再次登录、未导出认证内容。此PASS属于当前旧生产的部署前门禁；新进程上线后按现有流程验证其实际身份和健康。
- **有效证明直接复用：** main c50a53c1b89e121bcade7dac41d3f11e53a9832a；CI39338987624、RC39339871064、controlled Rehearsal39340908922及新候选真实backup/rollback87693f14均沿用，不重建、不重跑。仅只读核实main/latest RC/latest rehearsal/最新production记录未变化，原proof哈希一致；生产最新deployment仍7166252463。
- **受控条件：** 满足进入分阶段受控部署的前置条件，尚未获生产授权；Executor live health仍NOT_STARTED，作为授权后第一阶段硬门禁，必须在Gateway替换前通过。正式controlled合同已支持Provider后置，保留其余十项门禁及商业入口拒绝；无需再修改流程或提供虚假Provider PASS。
- **集中授权范围（待用户确认，未执行）：** 启动精确cfad Executor并核验真实身份/健康；核对后原样归档过期target61e720；通过正式owner controlled入口部署RC39339871064/Gateway22ecaa46…1724e02（含既有部署前备份、必要迁移及受支持的失败停止/回滚）；在替换前持久化公网隔离，部署后继续关闭。授权不包括真实客户流量开放、退款改账、删除记录、强制清熔断或sudoers修改。
- **生产/账务保持：** Gateway旧51dd/eb67未替换，Executor未启动，Caddy未停止，旧target未归档。2 live needs_review、3历史review-marked settled、10 finance sync needs_review、outbox0的已核基线保留；账务强保护由部署流程继续强制检查。熔断只读资格PASS且原有效期仍今天12:22，circuit_recovered=false。
- **下一步/最少用户操作：** 仅集中确认一次“授权受控生产部署（不开放流量）”；获授权后按上述精确计划连续执行，Provider业务验收及对外开放仍后置。部署计划和readiness已更新为READY_AWAITING_PRODUCTION_AUTHORIZATION，未执行production dispatch。

---

## 2026-10-20 07:30 Asia/Shanghai — 发布准备完成；正式受控排练及消费端PASS，等待本人Runtime认证，未部署

- **已完成：** PR75/76均正常合并，无admin/bypass。当前main=`c50a53c1b89e121bcade7dac41d3f11e53a9832a`；同源CI39338987624六项SUCCESS。正式RC39339871064 SUCCESS/attempt1，Gateway=`sha256:22ecaa468c784cd41ee2604dd8f3ac189960744516a508fc8c9cee3ee1724e02`，manifestSHA=`4103d5d1c25457181d5ac5e8000734ccc377e95f4c0752162694563f6b977028`；Executor仍精确cfad1b51…ae58。新候选真实加密备份恢复/两次启动/实际SIGKILL137/旧DB+旧镜像恢复PASS，rollback_proven=true；旧证据原样保留。
- **正式受控合同已通过：** Rehearsal `39340908922` SUCCESS/attempt1，实际十项receipt全部SUCCESS，Provider如实deferred而非PASS。新消费端实读artifact的receipts/目录、逐一hash及source/digest/RC/attempt/schema绑定PASS；商业消费端明确拒绝该controlled schema。proofSHA=`26b4a3a6d2ad9026739fc721ebaa1996d0fd44ecec7ee2cc400e2acc3c72a3e7`；latest RC/rehearsal及manifest source CI绑定均复核PASS，predecessor精确eb67、compare116文件完整。正式Runtime合同92例OK/2skip，不当作VPS实际Runtime PASS。
- **剩余硬阻塞：** 07:26 VPS安装版V2 --verify实际exit1；运行/Compose/manifest identity MATCH，但HTTP401 AUTH_REQUIRED。会话文件安全且无内容导出，已请求用户本人在VPS私有终端官方登录；05:43 PASS已过期不得沿用。用户只授权发布准备（不部署），尚未取得生产部署授权。
- **受控部署条件：** 离线发布准备就绪；当前仍不具备可执行生产部署条件。本人登录→新鲜V2 verify PASS后，集中一次生产授权，范围为先启动指定Executor并通过真实health/image/binary/network门禁、原样归档旧target61e720、通过官方owner controlled dispatch部署精确新RC。Executor尚未启动，不能声称其live health通过。V2在替换前持久化入口隔离、停Caddy并禁重启；当前Caddy仍运行旧生产，隔离方案未执行。不得开放商业流量。
- **生产及账务：** Gateway仍旧51dd/eb67、未替换；十二份root资产与main MATCH无需再装。原2 live needs_review、3历史review-marked settled、10 finance sync needs_review保留，outbox0，严格账务保护/单实例/无Redis/loopback/schema9均只读通过；无退款、改账、删除或强制清熔断。原熔断资格只读PASS，circuit_recovered=false，截止今天12:22未延长；生产最新deployment仍7166252463。
- **最少用户操作：** 先在VPS执行 `sudo /opt/clearapi/bin/runtime-probe-session.py --login`，完成仅回复“已登录”，不发送任何认证内容。验证通过后再集中申请一次生产部署授权；部署后用户自行真实Provider usage/billing验收，另行确认后才可开放商业流量。
- **审计：** `D:/AI/API/tmp/rc-39339871064/controlled-proof-consumption.json`；`launch-current/controlled-readiness-c50a53c1b89e-final.json`；`controlled-production-plan-c50a53c1b89e.json`。计划为PREPARED_NOT_AUTHORIZED，精确dispatch参数已留档，未执行。当前工作树detached main c50，Git clean；不再增加代码/重建/重跑成功门禁。

---

## 2026-10-20 07:20 Asia/Shanghai — 最终新候选真实恢复/回滚PASS；正式controlled Rehearsal39340908922运行中，未部署

- **新main/RC：** main c50a53c1b89e121bcade7dac41d3f11e53a9832a；同源CI39338987624六绿。RC39339871064 SUCCESS/attempt1，Gateway=`sha256:22ecaa468c784cd41ee2604dd8f3ac189960744516a508fc8c9cee3ee1724e02`，manifestSHA=`4103d5d1c25457181d5ac5e8000734ccc377e95f4c0752162694563f6b977028`，binary40ebc706…83eee4b，schema578bc6f8→3c1c77f3。产物原字节哈希/仓库/来源/平台/CI绑定正式校验PASS。一次本地assert误按int比较实际字符串run字段而失败，已按原始合同精确字符串校验；未修改任何manifest或重建。
- **真实执行PASS：** `D:/AI/API/tmp/rc-39339871064/isolated-recovery-result-87693f14.json`；复用原加密备份，新候选两次启动、账务及schema不变量通过；实际SIGKILL exit137后恢复数据库及旧Gateway51dd/eb67健康，rollback_proven=true。自建容器全部清理，原两轮证据及raw结果哈希保持。
- **精确镜像传输：** 非主线operator分支9910fe710（只改6处候选常量，未合main），run39340561989 SUCCESS且production job SKIPPED；归档SHAad281a0f…ce7d7e、141236009字节，manifest绑定及本机Docker image/revision通过，生产最新deployment仍7166252463。没有再次尝试失败的私有registry认证路径。
- **证据配置：** 新鲜实际执行来源的backup/rollback evidence经正式verifier PASS后更新两项Secret；payloadSHA分别d81b41b7…af56、cd710a00…38e4。Provider未伪造PASS，合同明确deferred。正式manual controlled Rehearsal `39340908922` 绑定此RC运行中；两次automatic入口正常SKIPPED已完成。
- **生产只读复核：** 07:11仍旧Gateway51dd running/restarts0、单实例/loopback/无Redis/严格账务保护通过；schema578bc6f8、finance schema9/owner有效；live needs_review2、历史review-marked settled3、finance sync needs_review10、outbox0。未改账。旧target61e720保持、无消费维护state/backup/reissue、无部署锁；Executor compose容器0、Caddy仍原restart policy运行。
- **仍未生产部署：** 12份发布资产已与新main MATCH，无需重装；熔断原历史只读资格PASS但circuit_recovered=false。正式排练运行中；07:26 VPS安装版V2 --verify exit1、identity MATCH，但实际HTTP401 AUTH_REQUIRED；会话文件安全，未导出内容。已在收尾窗口请求用户本人官方登录，不能沿用05:43的过期PASS。通过后再集中申请生产部署授权；不开放商业流量。

---

## 2026-10-20 07:10 Asia/Shanghai — main CI六绿；必要RC39339871064运行中，熔断资格只读复核通过，未部署

- main c50a53c1b89e121bcade7dac41d3f11e53a9832a 的push CI39338987624六项SUCCESS；正式policy latest_main_ci身份/必需jobs核验PASS，原始元数据已归档。
- 自动RC39339790361因配置false正常SKIPPED并完成后，唯一手动RC39339871064/attempt1已启动；尚未生成新digest，不声明生产验收。AUTO_RC=false/AUTO_PRODUCTION=false/predecessor eb67原样复核。
- 新main熔断资格只读核验PASS：四项原授权失败的run/job/log哈希、一次只读inspection完整状态链匹配、无后续manual介入；circuit_recovered=false，原调查截止12:22，未延长。审计circuit-readonly-c50a53c1b89e.json。
- `tmp/rc-39339871064/` 已准备新RC专属隔离验证脚本（保留原断言/本地engine约束），复用原真实加密备份；原39275789802和39334166644证据/结果哈希已冻结，尚未执行新候选。
- 十二份VPS发布资产与main精确一致，无需再安装。生产Gateway仍旧版本；Executor、旧target、公网入口、账务和熔断均未操作。Runtime登录仍待收尾实核，生产部署另行一次授权。

---

## 2026-10-20 07:02 Asia/Shanghai — PR76 六绿并正常合并；新main同源CI运行中，12份VPS资产精确匹配

- PR76最终head8111b59b，CI39338020103六项SUCCESS，经严格分支保护复核后正常merge，无admin/bypass。API保护查询一次unexpected EOF，第二次独立连接返回并通过断言后才合并。
- 当前main=`c50a53c1b89e121bcade7dac41d3f11e53a9832a`，与已通过CI的PR head树完全一致；push CI `39338987624` 运行中。尚未构建该main的新RC，旧RC39334166644及失败排练仅保留历史。
- 只读VPS资产复核：12份与新main精确MATCH，固定V2/helper pin一致；无需重复安装。生产应用服务没有启动、替换或停止。
- 后续：main CI成功→等待自动禁用RC正常结束→一次手动新RC→复用原加密备份验证新候选真实身份及恢复→新鲜证据配置→正式controlled Rehearsal。Runtime登录留在收尾窗口；生产部署和商业开放仍未授权。

---

## 2026-10-20 06:55 Asia/Shanghai — 两处排练对接修复已通过定向验证；PR76 必需CI运行中，未部署

- PR #76 / `8111b59b16ed6133f160af6163aed6891eee3bab` 已提交推送并关联本任务；CI `39338020103` 运行中，尚不声称通过。范围3文件39行新增/1行替换，无应用、账务、依赖及root发布脚本变更。
- 原失败环境下 `RELEASE_STAGE=controlled python3 scripts/ci/test-production-deploy.py` 实际92例OK/1skip；proof消费回归36项PASS，git diff检查PASS。失败Rehearsal39336491135保留原状。
- 用户已授权发布准备（不部署），继续必需CI→正常合并→main同源CI→一次必要新RC→候选真实验证和正式controlled Rehearsal；旧证据身份不改绑。
- Runtime官方认证待用户，安排排练收尾窗口复核；当前未启动Executor、替换Gateway、停止Caddy、归档旧target、改账或清除熔断，AUTO_PRODUCTION=false。

---

## 2026-10-20 06:49 Asia/Shanghai — 正式受控排练被真实门禁拒绝；两处最小对接修复验证中，未部署

- **状态：RELEASE BLOCKED；未生产部署。** Rehearsal `39336491135` FAILURE，assembler明确 `gate failed: runtime_integrity`。其余9项receipt真实SUCCESS；Provider按受控合同SKIPPED/deferred，未伪造PASS。
- **已定位具体原因：** controlled job的`RELEASE_STAGE`继承到商业V1测试fixture，导致34处`unknown argument: --controlled-launch`（实际91例/2skip）；不是上游业务PASS。另实际artifact将receipt置于`receipts/`子目录，旧消费端只查根目录，不能据此放行。
- **最小修复：** 当前工作树分支 `codex/clearapi-rehearsal-consumption-fix-20261020`，基于main0848；仅3文件39行新增/1行改动：明确商业fixture stage；消费端支持真实nested和既有flat布局并拒绝目录symlink/歧义副本；新增针对性回归。36项policy测试PASS，原失败的完整命令在`RELEASE_STAGE=controlled`下重跑验证中。V2/control/helper和任何账务、依赖、锁文件不改。
- main0848/RC39334166644的真实恢复/回滚结果3291afcf继续保留，不能把失败排练改成成功，也不能改旧证据身份。修复合并后，现有current-main合同**必须再生成一次必要新RC**及对应候选验证；仅此真实对接缺陷导致，不构建中间候选。
- Runtime最新401仍待用户本人认证；登录可放在后续排练收尾窗口，避免CI等待期间再次过期。生产Executor未启动、Caddy未停、旧target未归档、账务及熔断未处理，AUTO_PRODUCTION仍false。
- 下一阶段：针对性验证→在既有发布准备授权内提交PR并经必需CI正常合并→新main CI/必要RC→真实新候选证据→正式controlled Rehearsal；不得绕过失败receipt或直接部署。

---

## 2026-10-20 06:42 Asia/Shanghai — 新RC真实恢复/回滚PASS；有效证据已配置，正式controlled Rehearsal运行中

- **冻结候选不变：** main0848ce7a、RC39334166644/attempt1、Gateway84f69ec1…cac5f1、manifest8553ec41…626a6；没有第二次构建。
- **实际新执行PASS：** `D:/AI/API/tmp/rc-39334166644/isolated-recovery-result-3291afcf.json`。同一原加密备份完成新候选两次真实启动、源/目标schema和账务核验；实际SIGKILL exit137后重新恢复数据库，精确旧Gateway51dd/eb67健康、核心账务不变量保持；rollback_proven=true。所有本轮自建容器经标签核验后移除；原RC39275789802证据及原始c1036ccd结果SHA不变。
- **真实证据配置：** 新operator-evidence/backup_restore.json和safe_rollback.json均经正式verifier身份校验PASS后，才更新对应BACKUP/ROLLBACK Secret。payload SHA分别534b970d…e885、bc6a9ef3…943。Provider Secret未改写，受控合同仍如实deferred。
- **正式排练：** `39336491135` workflow_dispatch/main/controlled，绑定新RC39334166644，正在运行；旧自动排练39334891001、39334096781因入口条件SKIPPED，不当作通过。前任eb67→main0848真实ahead87/behind0，精确diff116文件未触及GitHub截断上限。
- **镜像传输已核验：** 非生产导出run39335684981成功；归档SHA5936daa3…bec3、141232689字节，加载到本机后ImageID/revision完整匹配。生产最新deployment仍7166252463，无新增记录；helper分支742e52fa未合主线。
- **生产仍未部署：** 仅已授权的三份发布脚本更新和predecessor配置已完成；Executor未启动、Caddy未停、账务及熔断未改。已实核旧credential job没有Compose service label，不会算作生产Executor，无需删除它。
- **剩余：** 等正式controlled Rehearsal；Runtime最新401仍待用户本人官方登录（6:19已请求）。通过后复核运行态/原熔断资格，并集中申请一次明确生产部署授权，覆盖旧target原样归档、准备指定Executor及受控Gateway替换；不开放商业流量。

---

## 2026-10-20 06:35 Asia/Shanghai — RC39334166644成功；通过非生产包读取任务取得精确候选，未部署

- 当前main=`0848ce7a4ac157cc2ac8411b617bceeba55366ad`，main CI39333198873六项SUCCESS。正式RC `39334166644` SUCCESS/attempt1，Gateway=`sha256:84f69ec152aed7f820b610fcf65f2b8adbea80e89c4539b6d8911bda46cac5f1`；manifestSHA=`8553ec417f50c681ab23e29ddec370d6272513ba6577211bd1447de5b09626a6`，binary=`40ebc706…83eee4b`；schema578bc6f8→3c1c77f3，artifact原字节hash均通过。
- 本地Docker有scope错误，匿名也被拒绝；VPS既有Docker登录unauthorized。均未读取Token、未启动服务或改release manifest，不继续重复该认证路径。
- 已在**非主线operator分支** `codex/clearapi-rc-image-export-20261020` / `742e52faa9a06b54f7de1d44f2cd09585ad1785c` 给既有GHCR工作流增加严格owner/ref/RC身份的非生产导出job；仅contents/actions/packages READ，没有production environment/SSH，复用Actions短期凭据。**不合入main，不重建RC，不改变生产合同。** 当前工作树暂位于此operator分支，不能把它当候选main。
- 导出 run `39335684981` SUCCESS，生产job明确SKIPPED；实时生产deployment最新仍7166252463，无新增生产记录。已认证并导出同一84f候选，归档下载到 `D:/AI/API/tmp/rc-39334166644/image-transfer/` 进行中。
- VPS三份差异脚本已审计安装；Gateway仍旧51dd，Executor未启动、Caddy未停止、账务/熔断未处理。Runtime最新401，待用户本人再次官方登录；没有用05:43过期PASS替代。
- 下一步：校验导出archive/receipt并仅加载到本机Docker→对新RC实际执行隔离恢复/故障注入/旧DB+镜像恢复→生成新执行来源证据（不改旧证据）→正式controlled Rehearsal。生产服务仍无部署授权。

---

## 2026-10-20 06:21 Asia/Shanghai — main CI 六绿；正式 RC39334166644 运行中，差异脚本已审计安装，未部署

- **main/CI：** `0848ce7a4ac157cc2ac8411b617bceeba55366ad`；同源push CI `39333198873` 六项SUCCESS（含三库）；PR75已正常合并。PR最终head CI39332051470亦六项SUCCESS。
- **唯一新候选构建：** 自动RC禁用导致39334073734正常SKIPPED并已完成后，手动触发正式RC `39334166644`，来源严格为当前main；尚在运行，无候选digest或生产PASS声明。AUTO_RC=false、AUTO_PRODUCTION=false；没有构建中间RC。
- **root资产：** 已安装V2/control/controlled-traffic-hold三份差异，原件及审计保留 `/opt/clearapi/bin/.release-assets-0848ce7a4ac1-1792448346582538094/`。V2 SHA=`1bd21eae376dd8316d1cfabe558133b4e0390965c2f13b7005e4deb656eac109`，helper SHA=`b75475f8ec195c17791bd00acab41ae777fb629901617595fa78374974277b90`；release manifest原字节不变，服务未重启、未部署、sudoers未动。
- **Runtime当前硬阻塞：** 新安装V2身份验证OK，但probe BLOCKED/PROBE_UNAVAILABLE；独立最小HTTP诊断确认06:19返回401 AUTH_REQUIRED（会话文件权限安全、未输出会话）。05:43的PASS已过期，不能沿用；已请用户在VPS私有终端官方登录。此项待用户，RC/隔离验证继续独立推进。
- **候选恢复准备：** 新目录 `D:/AI/API/tmp/rc-39334166644/` 的operator脚本保留原有恢复/失败注入/账务/旧镜像校验，仅换真实新RC/schema-helper路径并增加本地Docker端点防护。确认desktop-linux、Linux本地engine；尚未执行恢复。原RC39275789802所有证据和原始结果保持原样。
- **生产边界：** 没有启动Executor、替换Gateway、停止Caddy、处理账务或清除熔断。旧target归档仍仅计划，纳入最终生产部署授权。下一步：等待RC→真实候选必要恢复证明→新鲜证据Secret→正式controlled Rehearsal；Runtime由用户本人认证后实核。
- **记录设置：** CI元数据归档首次因Windows默认GBK无法解码step名称失败，未写入状态；明确UTF-8后归档成功。

---

## 2026-10-20 06:12 Asia/Shanghai — PR75 正常合并；受控合同进入 main，等待同源主线 CI，未部署

- **状态：PARTIAL / RELEASE PREPARATION；生产服务未动。** 用户已明确授权发布准备，不包含生产服务启动/替换/开放。AUTO_RC=false、AUTO_PRODUCTION=false。
- **main：** PR #75 已正常 merge（未使用admin/bypass），`main=0848ce7a4ac157cc2ac8411b617bceeba55366ad`；最终PR head `67b0002389a367d3cd7c5ab2b48251532640e78e`，CI `39332051470` 六项SUCCESS。主线push CI `39333198873` 运行中；正式RC尚未构建，旧RC39275789802仅历史，不能视为新main候选。
- **必要权限修正：** 去掉工作流新增helper的重复sudo hash查询，沿用V2固定哈希root内核验；测试SSH探针按现有sudoers白名单拒绝越权命令，8项workflow定向用例PASS。sudoers未修改。旧head CI39331527507随既有并发规则取消，不当作放行。
- **精确配置：** predecessor已从da0dcfcd校正为实时生产 `eb67c5f21639716eed7489d32ca322c249e5fdf3`，VPS当前仍旧Gateway51dd/release success。审计 `predecessor-correction-20261020.json`。
- **脚本准备：** 已从合并main的Git LF原始字节准备 `candidate-assets-0848ce7a4ac1/`，12份资产及V2/helper固定pin一致；9份MATCH，仅V2/control需替换、traffic helper需新增。尚未安装；无服务、账务或公网入口变更。
- **旧target：** 只读确认固定路径pg03-target-manifest.json仍为过期维护候选0a5884fd，SHA256=`61e7203487624765f38ba4adac0914078adc88cbe7301f54cb4b4357588e6d66`；无维护state或backup receipt。保留不覆盖合同，已准备按哈希原样归档方案，尚未执行，纳入最终生产部署授权。
- **下一阶段：** 同源main CI成功→一次必要正式main RC→针对新候选的真实恢复/回滚身份验证（复用原加密备份，不改旧证据）→精确差异脚本审计安装及Runtime复核→有效backup/rollback Secret配置→正式controlled Rehearsal。当前无生产部署授权；Executor仍不得启动，公网入口仍不得停止。

---

## 2026-10-20 06:00 Asia/Shanghai — 发布准备已获明确授权；PR75 CI 运行中，未部署

- 用户明确授权提交/推送/创建PR、必需CI通过后正常合并、校正predecessor、真实证据配置更新、审计安装发布脚本、必要新RC/Rehearsal；不包含生产服务启动/替换/开放流量。
- 本地发布合同提交 `e51b2374aadcaf2e0e901ced142ba52d747bd118` 已推送到指定 `production` 远端 `a3225865526-wq/Clear-API`，Draft PR #75，CI `39331527507` 运行中。默认origin误指向上游QuantumNous，首次推送被403拒绝且未产生远端变更，已纠正使用指定远端。
- predecessor 经VPS当前成功manifest再次实读后已精确更新为 `eb67c5f21639716eed7489d32ca322c249e5fdf3`，审计 `predecessor-correction-20261020.json`。没有放宽校验或变更生产服务/账务。
- Windows工作树的WSL Git指针不能直接识别；首次secret-scan错误输出后产生的PASS不采信，显式GIT_DIR/GIT_WORK_TREE后实际扫描通过。其余125项定向用例记录保持。
- 下一阶段：CI完成后正常合并→一次手动正式main RC（AUTO_RC/PRODUCTION仍false）→新候选必要身份验证与真实证据更新→差异脚本审计安装→正式controlled Rehearsal。旧RC证据不改绑，生产服务仍不操作。

---

## 2026-10-20 05:55 Asia/Shanghai — 受控发布合同已本地实现并验证；待远端发布准备授权，未部署

- **状态：PARTIAL / CONTROLLED DEPLOYMENT NOT YET READY；生产未部署。** main 仍 `db7f2571e321da26de8b89c3f81b9833e66859ee`，最终 RC 仍 `39275789802`；AUTO_RC=false、AUTO_PRODUCTION=false。未修改生产配置/数据库、未启动 Executor、未停止公网入口、未清除熔断。
- **用户最新决策：** 五家 Provider 真实 usage/billing 验收后置，由用户在受控部署后完成。Provider 不得伪造 PASS；商业流量须保持阻断直到验收与明确开放确认。D 暂缓。
- **机制核实及本地实施：** 原 Rehearsal 商业合同仍硬要求 Provider；manual 入口没有分阶段隔离合同，不将其用作绕过。旧 legacy maintenance 专为退役商品/数据库操作，不复用。新工作树 `D:/AI/API/worktrees/clearapi-controlled-launch-20261020` / 分支 `codex/clearapi-controlled-launch-20261020` 从最终 main 创建，本地变更尚未提交/推送。controlled proof 保留十项门禁、Provider=deferred、public_traffic=blocked；商业 consumer 拒绝；fresh owner dispatch、精确授权 artifact、RC/CI/熔断/前任/Executor 校验；V2 写 durable journal 后停止 Caddy并禁用自动重启，Gateway只绑loopback，80/443无监听，失败保留隔离，无自动开放入口。
- **验证：** 125 项不同定向用例 PASS（automatic34、PG03原35+新增parser1、producer7、V2 modes/workflow48）；shell语法、warning级shellcheck（既有SC2034/SC2163排除）、YAML/Python语法、V2/helper pin MATCH。中途 fixture缺id / WSL cwd / Windows GBK验证设置错误均已有明确修正及通过结果。新增变更正式CI/正式Rehearsal/生产受控链路均 NOT RUN，不能以本地结果替代。
- **原证据复用：** backup_restore / safe_rollback 原件对 RC39275789802 身份校验 PASS，未改写；没有重复恢复测试或构建 RC。复用原正式排练8项成功receipt及两份有效恢复证据，十项受控合同本地组装/消费 PASS，Provider仍deferred；`controlled-rehearsal-local-20261020/LOCAL_ONLY.json` 明确 local_only / production_authorization=false。更改流程合并后，现有 current-main 身份合同要求一次新 RC；旧证据不得换身份，必要候选验证需独立刷新，原加密备份可复用。
- **生产实核：** 05:40—05:42 只读确认旧 Gateway `51ddf6df…` running/restarts=0，schema仍 `578bc6f8…8333fd`，账务保护标志/单实例/无Redis均保持；2条live needs_review、3条历史review-marked settled、10条finance sync needs_review、outbox=0。未作运营处置。生产无running Executor；旧凭据生成容器仍Exited，不能当实例。
- **Runtime 已解除当前认证阻塞：** 用户本人完成官方登录后，05:43 安装版V2 `--verify` exit0，running/Compose/manifest身份一致、Runtime Gate PASS (before)。仅对旧生产有效；候选上线后仍必须自己通过Runtime。原过期legacy维护授权保留，不为无关退役续签。
- **熔断：** 原四项授权失败及只读inspection 7166252463的run/job/原日志hash/完整状态链实时复核匹配；无其他manual介入，生产record分类匹配。只读分类已完成，circuit_recovered=false；原调查截止2026-10-20 12:22 Asia/Shanghai，未延长。
- **新增配置硬阻塞：** `CLEARAPI_PREVIOUS_SOURCE_COMMIT=da0dcfcd…` 与实际生产 `eb67c5f21639716eed7489d32ca322c249e5fdf3` 不同，必须精确审计更新，不能跳过predecessor校验。
- **下一最少授权：** 用户全局AGENTS要求提交/推送/外部写入须单独明确授权；先授权本地发布合同变更提交/推送/PR/必需CI后正常合并、必要RC/Rehearsal及精确配置/有效证据更新，服务仍不部署。满足受控条件后，集中申请一次明确生产部署授权（覆盖配对Executor启动及Gateway受控替换）；验收后另需用户Provider结果与开放确认。
- **审查与证据：** `D:/AI/API/runtime/clearapi-unified-final-20261019/launch-current/CONTROLLED_RELEASE_REVIEW_20261020.md`、`controlled-policy-validation-20261020.json`、`controlled-readiness-20261020.json`、`circuit-readonly-20261020.json`。下方历史记录保留原样。

---

## 2026-10-19 23:30 Asia/Shanghai — 最终自动 RC 已生成；Rehearsal 因三份外部证据过期失败

- **状态：PARTIAL / RELEASE BLOCKED；未生产部署。** 当前生产仍运行旧 Gateway `sha256:51ddf6df27129df0fe7ab7205c1e8bef0da6e2ca9b7bb9ede93ab77c863d24a5`，未重启、未改账、未清熔断；`CLEARAPI_AUTO_RC_ENABLED=false`、`CLEARAPI_AUTO_PRODUCTION_ENABLED=false`。
- **main/CI：** `main=db7f2571e321da26de8b89c3f81b9833e66859ee`；同源 push CI `39271850900` 六项 SUCCESS。PR #73、#74 均已 MERGED；PR #74 合并主线 CI 真实 SUCCESS。
- **最终 RC：** 自动链 `39275789802` SUCCESS，来源严格绑定上述 main，唯一候选 manifest 下载至 `launch-current/final-rc-39275789802/`。manifest SHA256=`e6d9f4acd259682f2487ce9d21455a2564394b5e54f7ad3739f33704b521cdba`；Gateway digest=`sha256:41b4699b1e70974c58fae70d5cddb6695bfaaf9e25c98a433166b2196b26a51b`；binary SHA256=`40ebc70665bf7f45c7a201c4fdd82a90913a1873b2c7ca0debda1829983eee4b`；schema source=`578bc6f8…8333fd`、target=`3c1c77f3…35035`。此前手动 RC `39273487409` digest=`sha256:8e212b…` 不得复用。
- **正式 Rehearsal：** `39276589237` 绑定最终自动 RC；Runtime/Deploy V2、billing、backup/restore gate、Executor pairing、三数据库幂等、Provider/usage gate、replay、state isolation、safe rollback 的本地测试步骤均完成，但 `assemble proof` 失败。失败原因是 GitHub Secret 中 backup/provider/rollback 三份操作证据仍绑定旧源码/旧 digest/旧 manifest，verifier 明确返回 `*_ rehearsal evidence is stale`；不得改写为 PASS，不得以历史 `rollback_proven=false` 替代真实恢复。
- **VPS 实时只读复核：** `current-vps.json` 已更新至 `15:26:42Z`；旧 Gateway running/restarts=0；Runtime HTTP `401 AUTH_REQUIRED`；部署锁无人持有；旧 maintenance request `7f426779…` 已过期，state/backup receipt/reissue 均不存在；生产账务 durable/outbox/reconciliation 配置为真、单实例、无 Redis、schema v9，pending reservation=5、outbox=0，5 条记录仍需运营核对。deployment log 79 条记录，最近成功仍是旧镜像 `eb67c5f2…`；没有本候选 production record。
- **当前硬阻塞：** 需要新鲜且真实操作来源的 backup、Provider usage/billing、safe rollback evidence，全部绑定最终 RC `39275789802`、digest、manifest SHA、schema 和 target `clearapi-production`；其中 rollback 必须实际经历候选失败后恢复 DB+旧镜像并验证，不能由双启动或声明字段代替。还需重新官方 Runtime 登录取得 Runtime PASS，并在正式部署前重新核对过期熔断/未决操作。

## 2026-10-19 22:39 Asia/Shanghai — PR73 已合并；商业 Runtime 策略真实缺口待 PR74

- **状态：PARTIAL / RELEASE BLOCKED；未生产部署。** 最新用户要求保留：生产部署全部硬门禁通过后必须执行前明确确认。AUTO_PRODUCTION=false；AUTO_RC=false（暂停中间RC，避免已知错误版本构建），未改sudoers、熔断或重启生产服务，D暂缓。
- **main：** PR #73 已于2026-10-19T14:25:43Z正常合并，`86ab6cbb6cc1131d33920ee7b53566284e0350d4`；PR head CI39267675730、main CI39268853772均6项SUCCESS。自动RC39269982444、Rehearsal39269990802、自动发布39270000453均SKIPPED，没有构建重复RC。
- **必须追加修复：** main86脚本已按Git LF字节安装4个不匹配资产（V1/V2/control/backup）并补正式V1安装器，原件/audit保存在`/opt/clearapi/bin/.release-assets-86ab6cbb6cc1-1792420033434276765/`，服务/数据库未动。新V2身份验证OK，但真实Runtime被RUNTIME_CONFIG_MISMATCH拒绝。已查明生产商店开启且root deploy.env明确`DEPLOY_LAUNCH_BASELINE=commercial`、`DEPLOY_STORE_POLICY=commercial_catalog`；原服务器旧V2支持这项策略，仓库V1/V2缺失。不得关闭商店或假称Runtime通过。
- **PR #74：** `550c2b2e9a8a33a4c28d96bcd5ed1770c5766c3d`，分支`codex/pg03-runtime-store-policy-20261019`，工作树`D:/AI/API/worktrees/clearapi-runtime-store-policy-20261019`，Draft，CI39270428270进行中。修复仅明确的root商业目录策略，closed/未知/调用者覆盖拒绝，支付/pool master/enforce/referral/审批/产品/身份/时效/前后不变校验保留，相关pin全部MATCH。新字节未安装。
- **验证实况：** 两wrapper新增商业策略成功与负向条件已实际通过；原无效配置断言1改为更早拒绝2后V1/V2单例PASS。初轮V1 Runtime8项、V2共享28项各含同一旧断言FAIL/一个真实producer SKIP，不冒充整套PASS；修正后的完整回归待本head CI。PG03 root28、InstallerFault14 PASS；bash-n PASS。shellcheck V1三条/V2十条既有诊断逐项与main一致，全文仍FAIL，无新增；安装器PASS。初次WSL路径插值错误导致setup失败已记录并纠正为明确Linux路径。
- **Runtime：** 用户本轮官方登录+旧V2验证PASS已记录；安装main86字节后的新验证失败。后续直连HTTP401 AUTH_REQUIRED；最终验证窗口需再次官方登录，不能假定先前会话持续有效。没有修改/输出/复制会话。
- **恢复准备已真实完成：** 复用正式备份20261018T193241Z（当前生产eb67/51dd，PG03恢复范围完整，encrypted hash MATCH），离线GPG流式仅恢复数据库至隔离tmpfs PostgreSQL，无明文secrets归档导出/DB文件落盘。82表schema=`578bc6f8…8333fd`精确匹配当前生产，账务/索引/约束零异常。精确旧镜像已通过固定SSH传输、SHA/OCI校验并在隔离数据库启动健康，冻结账务hash未变。未运行最终候选，尚无safe rollback PASS。
- **镜像传输边界：** 初次未压缩旧镜像传输180秒超时，部分tar保留且禁用；压缩传输完成、hash记录、docker load及旧镜像身份核对PASS。没有重建旧镜像。
- **隔离目标：** `launch-current/isolated-recovery-target.json`保存本任务PostgreSQL容器/随机label，必须校验label后使用；旧Gateway健康容器已停止保留。不要触碰其他本地运行/停止容器或旧链路数据。
- **仍需：** PR74必需CI/正常合并→冻结修复后的main→一次最终RC→重算必要安装pin并安装差异→正式Runtime/当前账号资格→最终候选失败后DB+旧镜像真实恢复、Provider usage/切账号/账务→正式Rehearsal/熔断实核→用户确认→正式部署与线上验收。生产Executor尚无运行实例；不能拿已退出凭据生成容器或本地隔离Executor当生产实例。pending reservation=5，outbox=0，未替运营决定退款。
- **证据：** `D:/AI/API/runtime/clearapi-unified-final-20261019/launch-current/`；下方为本轮此前阶段和历史，保留原样。

---

## 2026-10-19 22:19 Asia/Shanghai — 本地继续上线 / 当前阶段执行中

- **状态：PARTIAL / NOT DEPLOYED。** 用户已报告在正式 VPS 私有终端完成 `runtime-probe-session.py --login`，随后旧安装版 V2 `--verify` 输出 identity OK、Compose/manifest/current digest 一致、Runtime Gate PASS (before)。作为本次用户现场证据记录，未重复该成功检查；它仅适用于当前旧生产 `eb67c5f2` / `sha256:51ddf6df…`，不是新版放行。
- **最新授权：** 生产部署必须全部硬门禁通过后、执行前再次明确取得用户确认。已将 `CLEARAPI_AUTO_PRODUCTION_ENABLED` 从 true 改为 false（2026-10-19T14:16:55Z），防止正式 Rehearsal 成功后自动上线；未清除熔断、未改 sudoers、未重启生产服务。D 暂缓。
- **候选：** PR #73 仍 Draft；当前 head `f1a75fb9c92908e46e73b806a53571d2de1635df`，Required CI `39267675730` 运行中。main 最近独立查询仍 `d0c20d6c46d86845805e4a9d35b067b0f6b20be4`。旧 head `0d1c26e1` 六绿只作为历史；中间 head CI 按仓库既有 concurrency 自动取消，未人工取消。
- **本轮代码：** `e01e3d619` 为熔断恢复增加精确只读记录分类：重新检查 record/run/job/源字节/原日志 hash/完整状态链，并保留真实 production 记录计数；新记录或任一变化拒绝。31 项定向测试 PASS，历史 inspection 7166252463 实时匹配。原调查 expiry 未延长；尚未执行恢复或生成生产许可。
- **新增真实阻塞及修复：** 生产82表 schema=`578bc6f8…8333fd`，RC旧75表 fixture=`8922081b…9da0ef`，不匹配。新增7张 Route Chain/fallback 表、无删表。已从生产只读导出无行数据 schema，更新同一排练 fixture（`f1a75fb9c`）；在隔离 PostgreSQL 17.6 导入/再导出摘要完全相同。最终新 RC 必须使用该输入；不能复用旧 schema 证明。
- **生产账务只读实测：** durable/outbox/reconciliation=true，单实例1、无 Redis、批更新=false；finance schema v9、owner 已配置且为有效 root，reconciliation 所需列和唯一索引存在，日志库共用主库。pending reservation=5、outbox pending=0；未改账或作退款决定，运营承接尚待最终确认。
- **维护与资产：** 旧 request `7f426779…` 已过期，仍绑定 `0a5884fd` 及另一旧容器；plan/authorization/target 内部匹配，仅 review audit，未见 state/backup receipt/reissue、锁无持有者。旧授权不能用于新候选。11份资产核对：V1/V2/control/backup 4份不匹配，legacy/runtime helpers/backup helpers等6份匹配，V1正式安装器指定路径不存在。候选 LF 原始 Git 字节包与固定哈希已准备，尚未安装。
- **备份：** 已找到实际完成备份 `20261018T193241Z`，绑定当前生产 eb67/51dd，PG03恢复范围完整、加密文件hash实测匹配；优先复用，不生成重复备份。最终恢复与安全回滚、最终候选 Provider usage/切账号/账务证据、正式 Rehearsal 仍待执行。
- **证据目录：** `D:/AI/API/runtime/clearapi-unified-final-20261019/launch-current/`。本节以下为先前只读审计及历史快照，原样保留。

---

## 2026-10-19 22:00 Asia/Shanghai — VPS 生产只读审计完成 / RELEASE BLOCKED

- **CURRENT STATUS: PARTIAL / BLOCKED，新版未上线。** 当前任务仅生产只读审计，已完成并停止；D 继续暂缓。既有后续发布授权不改变本轮禁止安装、修复、部署和熔断恢复的边界。
- **主线：** `production/main=d0c20d6c46d86845805e4a9d35b067b0f6b20be4`，收尾再次 ls-remote 核实；尚未合并统一分支。
- **统一候选：** `0d1c26e1d0834f48b1aec8882d0c911dac81b19b`，工作区 `D:/AI/API/worktrees/clearapi-unified-integration`；Draft [PR #73](https://github.com/a3225865526-wq/Clear-API/pull/73)。A/B 已有历史保留，C 两个批准提交精确整合，E 选择性整合并解决 V2 pin 冲突，PR #72 全部审阅代码已合入；其他工作线未提交内容未动。
- **实际 CI：** [39259388054](https://github.com/a3225865526-wq/Clear-API/actions/runs/39259388054) 六项 SUCCESS，绑定上述统一 SHA；前后端构建、全量测试、三库商业回归和部署门禁完成。后端原始 `go test exit code: 0`，没有 baseline acknowledged failure；未修改基线名单、CI 合同或锁文件。
- **定向验证：** A 88 frontend tests PASS，TypeScript PASS，24 个前端差异文件格式 PASS；定价清理真实三库 PASS；C 六包 PASS；V1 89 tests（88 PASS、1 real producer artifact 未提供而 SKIP）；V2 48、backup 22、PG03 root 28、legacy 25、automatic 28、producer 6 全 PASS。
- **必要新修复：** rollback evidence 必须 `rollback_proven is True`；历史 evFinal 材料虽 identity/conclusion 匹配，但只证明候选双启动，现已实际拒绝并产生 failure receipt。未重签旧证据、未修改 Secret 或放宽门禁。
- **CURRENT BLOCKER 1：** 最终源码尚未冻结到 main，无最终 RC/digest/proof。已下载原始 GitHub 审计产物：三份 Secret 输入哈希均精确匹配 `runtime/pg03-rebind-20261019/evFinal/` 原件去掉末尾换行后的字节，身份均为旧 RC 39197505434 / cd6f719e，不适用统一候选。Provider 原始测量没有源码/镜像/二进制身份；回滚原件明确 false，仅候选双启动，真实失败后 DB/旧镜像恢复证明仍缺。
- **CURRENT BLOCKER 2：** 已复用指定历史私钥、固定 ED25519 主机密钥及 Windows OpenSSH 成功连接 `clearadmin@103.42.181.219:22`，`sudo -n -l` 证明既有 NOPASSWD: ALL；本轮 root 只读采集成功，**无需再询问管理员通道**。实测 V1 对当前 main MATCH、对 PR73 MISMATCH；V2/control 对两者均 MISMATCH，服务器仍固定旧 Executor e5f33dc6/0838072c；指定安装器不存在，已查三个目录直接文件也未找到替代名，实际路径 UNKNOWN。Runtime 会话存在、root:root 0600、mtime 2026-10-18 03:18 Asia/Shanghai；未读取内容，是否有效及 Runtime PASS 均 UNKNOWN/未验证。
- **正式运行：** Gateway `clearapi-new-api-1` running/restarts=0，digest `sha256:51ddf6df27129df0fe7ab7205c1e8bef0da6e2ca9b7bb9ede93ab77c863d24a5`，OCI revision 与 Manifest current 均为 `eb67c5f21639716eed7489d32ca322c249e5fdf3`、pool_mode=off、status=success；不是当前 main 或 PR73。没有运行中的 Executor 容器/匹配 systemd 服务；仅旧生成凭据容器 Exited(0)/restarts=0，不能当成服务实例。未做公网/业务请求验收。
- **CURRENT BLOCKER 3：** production records 最新实读仍为 29，最新 7166252463 为 inspection 39203542451 的 failure；GitHub in_progress/waiting/queued 均为 0。VPS 未发现匹配部署进程、systemd jobs 或部署锁持有者；锁文件未清理。旧 PG03 target 与维护 request/authorization/plan 存在，但 transition state 不存在，授权消费/业务闭环 UNKNOWN，不能宣称无所有未决事项。现有恢复合同仍拒绝事故后的 production record；未恢复熔断。现有调查截止 2026-10-20 12:22 Asia/Shanghai。
- **池失败账务结论：** relay/service/model 定向及 SQLite/MySQL 8.4.11/PostgreSQL 17.6 reconciliation PASS，覆盖 unknown marker、pending 查询、幂等退款及钱包/token 一致。非 durable 保守路径仍不能保证消费日志；必须核实生产 durable wallet + reconciliation schema/单实例/无 Redis/无批更新 + 运营审计承接，否则发布 BLOCKED，不能假定上游 usage=0 或无条件退款。
- **检查限制：** 全仓 lint 178 errors / 64 warnings（均在本次差异外）和全仓既有格式问题保留；不声称全仓 lint/format PASS。MySQL 首次 DSN 缺 parseTime 与第二次旧 fixture 重复失败已留档；正式 DSN+全新隔离 DB 后 PASS。
- **部署与线上：** 最终 RC / 正式 Rehearsal / production deployment / 真实请求 / 切账号 / Usage / 消费日志与钱包/token 线上对账全部 NOT RUN；未以 CI 或隔离结果替代上线。
- **NEXT MINIMUM ACTION（建议，未执行）：** 先核对旧 PG03/维护授权材料的有效期、消费与业务闭环，保持熔断；冻结最终源码后准备精确资产/安装器来源清单，另按受审正式流程处理安装、Executor 配对及真实 Runtime。当前权限通道已验证，不再把恢复 SSH 当作阻塞；本轮只读任务到报告交付即停止。
- **本次续办：** 最新 PR/main/六项 CI 状态未变；未重跑有效 CI、三库测试、构建或备份，未改源码/依赖/Secret/生产配置/账务数据，未合并、部署。只更新来源核验、root 资产比对和报告。
- **证据：** [最新 VPS 只读审计](D:/AI/API/runtime/clearapi-vps-readonly-audit-20261019/REPORT.md)、[来源续办报告](D:/AI/API/runtime/clearapi-unified-final-20261019/continuation-20261019/REPORT.md)、[原执行报告](D:/AI/API/runtime/clearapi-unified-final-20261019/REPORT.md)。审计目录保留脱敏远端状态、完整容器清单、GitHub 状态、Git blob 哈希/常量对照及完整性清单。历史总控保留只读，统一工作区已提交总控是 CI 前快照。

## 2026-10-24 — 本地部署成果保存

- Windows 本地构建及 SSH 直传已验证；正式 PowerShell → SSH → Linux helper → Compose 成功部署 E2E 已真实 PASS。
- 后端不安全自动回退阻断已验证；前端自动回退、健康超时和剩余负向矩阵尚未完整 PASS。
- 本地部署成果已保存到正式仓库独立分支 `codex/clearapi-local-deploy-integration`；GitHub 自动生产工作流尚未停用，生产 VPS 尚未切换。
