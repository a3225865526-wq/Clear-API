# 2026-10-24 最小生产维护执行结果

**结论：已在公网健康失败处停止，原 Caddy 安全停回；网站未恢复，本地入口未激活。** 不是完整生产接管成功。

授权源码：`1aa591f4bb230c4fce5770114e45e9311beb87a4`。执行前确认工作区为 `D:/Codex/worktrees/clearapi-local-formal-integration/clearapi-controlled-launch-20261020`、分支正确、HEAD精确匹配且干净；7项Git源blob与已审核清单SHA256一致。后续只新增操作证据和状态说明，未改冻结代码、未Push或合并main。

## 1. 四项旧生产入口已停用

10:42 Asia/Shanghai依次停用：387486994 automatic-deploy、378101050 deploy-production、390042545 failed-production-recovery、378227091 ghcr-pull-rehearsal。逐项核对为 `disabled_manually`；其余7项仍active，普通CI及分支保护未改。

最终只读核实：两个AUTO变量仍false，queued/in_progress/waiting/pending/requested全部0，远端main仍a032815c，未Push。证据：github-before.json、workflow-disable-actions.json、github-after-disable.json、github-final.json。

## 2. 七项root资产、sudoers及元数据接管已完成

- 安装前再次比对生产CID、镜像、启动时间、挂载、原四层Compose、既有资产、环境文件、Caddy配置与boot系统文件，无漂移。原Runtime、boot安装及TrafficHold检查通过。
- 现场及旧资产保存在生产root-only目录 `/opt/clearapi/backups/maintenance-20261024-1aa591f4bb`，记录原属主、权限、哈希和原本不存在的目标。额外保全20261020T002138Z及20261023T102102Z的原manifest/密文，未为追求回退删除历史记录。
- 仅将 `/opt/clearapi/app/docker-compose.prod.yml` 的UID/GID1000改为root:root；0644和SHA `c5fc8ca1…`、内容不变，没有递归改权限。
- 7项资产按审核SHA逐项安装为root:root0750；共享模块先安装。另按明确授权安装 `/etc/sudoers.d/clearapi-local-deploy`，root:root0440，SHA `843e4777…`；独立与完整visudo检查均通过。原管理员ALL权限未被撤销，未声称该片段消除了既有管理员权限。
- 冻结入口的adopt-check PASS后执行adopt-running；真实生成local-deploy-state.json和compose-local.yaml，状态为adopted，没有Compose up/start Gateway。状态文件SHA `cdcea866…`、终端层SHA `aee0bbc3…`。原FAILED/target/hold及四层Compose内容逐字节不变。
- 当前Gateway OCI源码仍a032815c、容器0a9a93c0，Docker engine ID为ed6f93a9，OCI config digest为48df93b3；没有把工具源码提交当作运行镜像提交。

证据：production-before.json、install-root-assets.json、adopt-check.json、adopt-running.json、final-production-state.json。

## 3. 正式备份、Runtime和boot PASS

只调用一次正式备份服务，时间10:51:43—10:52:07，备份ID **20261024T025143Z**。systemd Result=success / ExecMainStatus=0；oneshot完成后inactive为正常完成状态，timer仍保留。新证明为schema2 / running identity v1，绑定当前0a9a93c0/a032/config48df及接管状态SHA，未使用旧停止态证明替代。

- 密文 **12,377,896 bytes**，SHA256 **7e77bb3aba44e22bc3be0be2b272f454ce122811e2676cac44bec3cfa732f54a**，服务和只读复核均匹配；明文staging已清理。
- 已确认既有离线恢复密钥可用、原真实隔离PG17恢复成功证据保留。新密文通过同一受保护密钥认证解密，9项归档SHA全部匹配，本地隔离、无网络PG17客户端对新dump执行pg_restore -l成功。没有导出私钥或将明文写入文件，没有恢复生产数据库；本次也没有声称执行了新备份的完整数据库恢复演练。
- 加密副本保存在Git外 `D:/AI/API/runtime/clearapi-local-maintenance-20261024/`，未把密文、明文、密钥加入Git。Docker读取客户端端点为本机named pipe，没有使用生产Docker端点。
- 接管后及停止后，Runtime --check、boot check-installed、live check均PASS。持久目录、挂载和运行身份current；四项boot系统资产未改，未重启主机或Docker。

证据：restore-adopted.json中的真实backup_id、post-stop-diagnostic.json、fresh-backup-readability.json、recovery-preconditions.json、local-readability-endpoint.json、final-production-state.json。

## 4. 原Caddy启动过，公网合同失败后已停回

按冻结restore-adopted入口执行，10:52:19.167启动原54c8550b/f2a1290d容器。Caddy日志有server running/serving initial configuration，没有启动崩溃证据；它在规定公网健康窗口内未通过检查。

10:52:49追加 `website-restoration-failure-39347012653.json`，状态website_restoration_failed、containment_verified=true、public_traffic=blocked。原Caddy已停止，restart恢复no，退出码0、State.Error为空。失败文件SHA `42eca429…`，没有成功result，没有重试、删除attempt或清原隔离。

源站HTTPS/转发/HTTP308及全部网页后置验收在公网前置失败后未完整走完，不能标PASS。已合法完成的元数据接管及新备份保留，未盲目换回不识别接管状态的旧资产。

## 5. 网站没有恢复实际访问

只读复核的www.clear999.com/api/status持续收到 **Cloudflare HTTP403 / error code1010**，带CF-Ray；对应维护nonce未出现在已读取的原站access记录中。只读VPS解析裸域clear999.com得到gaierror / No address associated with hostname，不能据此声称已完成裸域访问恢复。

这说明公网健康合同未满足；不把“进程启动”写成“网站恢复”，也不在缺乏证据时把本次失败归咎于Gateway镜像。没有改Cloudflare防护规则、DNS、Caddy配置或绕过检查。剩余处理应先核实1010与维护客户端/公网防护的关系及裸域解析；本轮已按停止条件终止，不继续扩大生产修改。

证据：restore-adopted.json、post-stop-diagnostic.json、public-error-classification.json。

## 6. Windows本地部署入口没有激活

网站未通过，不生成 `/opt/clearapi/config/local-deploy.json` 或local-deploy.enabled。相关资产、sudoers和adopted状态已安装/登记，但不等于正式入口激活。没有执行后续Gateway更新，也未把Windows脚本的无应用变更拒构建当作部署成功。

## 7. 剩余阻塞

公网403/1010、VPS裸域解析未取得地址、网站健康未验收。原Caddy已安全停回，当前失败结果必须保留；不能重跑原一次性恢复入口或删除失败凭证来追求表面成功。需要在明确审阅和授权下解决剩余公网/检查兼容问题，之后才可重新评估恢复和激活。

## 8. Gateway、PostgreSQL、Executor及业务数据没有被本轮改写

前后证实三者CID、镜像、StartedAt、restart policy、挂载均一致；Gateway仍HTTP200，PG/Executor healthy，PG03仍false。未执行Gateway重启/重部署、PostgreSQL写入/恢复/迁移、Executor启停或渠道/余额/计费/其他业务配置操作。数据库操作仅为已授权正式备份读取，未用不安全自动回退。

本轮生产写入仅为已授权的4个工作流停用、现场保全、指定文件属主、7个资产及sudoers、接管/备份/恢复审计记录、正式备份文件与原Caddy尝试启动/停回及其正常运行缓存。原FAILED/target/hold、Runtime身份数据及原Caddyfile字节保留。

停止后只有只读核对和本地备份可读取性/证据整理，没有激活、重试Caddy或继续修改生产。一次本地终端GBK无法显示Caddy日志emoji的输出错误未影响远端诊断；完整远端JSON已先写入证据，随后只从该证据读取，未重复生产操作。未重跑30项隔离E2E、未开展无关CI。
