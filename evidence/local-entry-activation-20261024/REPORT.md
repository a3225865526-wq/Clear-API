# 2026-10-24 Windows本地部署入口正式激活回执

**结果：入口已正式激活，实际preflight PASS，网站正常，未执行任何应用部署。** 本次无失败停回或激活回滚。

## 1. 授权及执行前核对

本轮用户明确批准 `evidence/local-entry-preparation-20261024/activation-plan.json` 的最小生产写入。开始时正式工作区 `D:/Codex/worktrees/clearapi-local-formal-integration/clearapi-controlled-launch-20261020`、分支 `codex/clearapi-local-deploy-formal-integration`、干净HEAD **133336060a650be577d618a046f001a124f379aa** 均匹配。安装资产只从计划中的 **ce84487c4d1838e3f911c826f1dd4d781d6b5e20** Git blob读取；helper实现提交14d7c09582，三项源字节、LF、大小和SHA全部匹配，不安装工作区临时字节。

实际使用现有受保护身份及专用known_hosts，BatchMode/IdentitiesOnly/StrictHostKeyChecking，可信ed25519指纹 `SHA256:kVqtsCzwAanVxCoVB5OA4b1lvMVdF9bpxYhy8xjNuLc` 与服务器公钥匹配。没有读取、输出或复制私钥/密码/Token。

只读实核：原helper66ec和其他6资产匹配，两个目标配置文件确实不存在；原Caddy/Gateway/PG/Executor的CID/镜像/运行/启动时间/restart/mounts无漂移。四层Compose、现有root环境哈希、adopted状态cdcea866及website_restored结果d2bfd7f6一致；五层有效Compose的ps仍识别原Gateway和PostgreSQL。Runtime、boot installed/live、既有sudoers/visudo正常；原FAILED/target/hold不改。网站公开状态API为200、success=true。

备份前置单独只读验证：已有正式备份20261024T033125Z，manifest SHA48cf9630、密文SHAf0fbffaa及当前Gateway/部署状态身份均匹配，backup service Result=success/exit0，timer active。未新触发备份、解密或恢复演练，不把这些补充检查说成preflight内执行了一次备份。

GitHub初次经本机代理访问出现TLS超时，**当时未写VPS**；随后仅在gh子进程中去除网络代理变量直连完成GET，未修改持久代理设置、防护或认证。精确4项生产workflow仍disabled_manually，普通CI及其余7项保留；两AUTO=false，queued/in_progress/waiting/pending/requested均0。成功门禁见github-gate.json，失败诊断保留而未冒充PASS。

## 2. 真实保全与写入

Asia/Shanghai **21:59:13–21:59:27**，在原生产部署锁内重新核对现场后执行。保全目录：

`/opt/clearapi/backups/local-entry-activation-20261024-133336060a`

目录root:root0700；旧helper、原配置缺失状态、必要release/target/hold/adopted/overlay/网站恢复证据及前后回执均root-only保留。除下列三个入口文件外，仅写计划已批准的必要保全/回执和自动清理的临时校验文件，没有新增部署平台或其他资产。

| 实际目标 | 操作 | owner/mode | bytes | SHA256 |
| --- | --- | --- | --- | --- |
| /opt/clearapi/bin/clearapi-local-deploy.py | 保全旧66ec后原子更新 | root:root0750 | 31111 | b9c11350b7d8d3aef15165bd7a5ea2a5a1ab0f731c39d1f8983bad672e45417f |
| /opt/clearapi/config/local-deploy.json | 原本不存在，独占创建 | root:root0600 | 388 | 22833e916fc368b208959d55442f006e5b551e81dc5261be40a92803bb188286 |
| /opt/clearapi/config/local-deploy.enabled | 最后发布 | root:root0600 | 33 | 00d09becb7e5b07482cf917cf361befd457a12e664d193645454e0e6a4904f16 |

三文件均普通单链接、非symlink；严格使用既有正式配置和 `LOCAL_DEPLOY_CUTOVER_APPROVED_V1` 合同，不含测试模式或自定义目标字段。配置内容经正式config()读取并与固定生产values匹配，见final-state.json。其他6项root资产及sudoers全部不变。

root执行回执：上述目录的 `activation-receipt.json`，root:root0600、12172 bytes、SHA256 **558886f95653bc58683ca848c71192093a1b19463ee49b09feff717c167fa5c1**。独立读回与本地完整结果一致，明确 result=ACTIVATED、rollback=false、application_deployment_executed=false。

## 3. 正式preflight实测PASS

首先在安装事务内，实际调用已安装程序的默认生产入口：

```text
/opt/clearapi/bin/clearapi-local-deploy.py preflight
```

完整CLI退出0，读取真实root配置/哨兵，经真实Docker/Compose/manifest及健康检查返回healthy=true。随后 **22:01:55**，从Windows按本地 `scripts/local-deploy.config.json` 的真实OpenSSH选项及固定sudo命令再执行一次，以验证实际操作者路径，而不是只验证root内调用：

```text
sudo -n /opt/clearapi/bin/clearapi-local-deploy.py preflight
```

同样exit0，stderr为空，返回完全相同的正式身份：

- arch=amd64、service=new-api、healthy=true。
- commit=`a032815cb6244a7ee5e812de82f8300c02554e99`。
- container_id=`0a9a93c02646b1885323f6d4255277c1253e06794205d061f3fdbfca2cc1b56c`。
- image_ref=`ghcr.io/a3225865526-wq/clearapi@sha256:ed6f93a91285abab0f8730c7c14a36b5e52bfaed521ce8c4e798d2815da286b1`。
- image_id（归档OCI config digest）=`sha256:48df93b31f69cf7e3597130c8be86e2c6e97f046de789ccc505257276183b71f`；与容器的engine/manifest ID ed6分开核对，不能混为两个不同应用镜像。

没有FixturePath、TestTarget、config-path覆盖或内存候选代替完整检查。既有preflight为核对真实OCI config两次读取Docker save归档，未docker load/导入/构建或更新镜像；所有 `clearapi-image-*` 临时目录最终为空。正式preflight之外，已另核备份前置、Runtime/boot、权限/sudoers和原恢复证据，未为了激活重新执行30项隔离E2E或开展应用发布。

## 4. 网站、核心服务及边界

激活后正常原生curl读取 `https://www.clear999.com/`：HTTP200、CF-Ray **a4f9808e9ff70987-HKG**，应用壳有效，body SHA8112a695与网站恢复时一致。两个正式preflight的本地及公网API均200/success=true。没有伪称重新完成浏览器人工、登录、支付或Provider验收。

独立后置读取确认全部四个服务的CID/镜像/运行状态/StartedAt/restart/mounts均未变化：

- Gateway仍0a9a93c0/ed6/a032，StartedAt=`2026-10-23T14:07:58.383082222Z`。
- PostgreSQL仍99299db0/ef257d85，StartedAt=`2026-10-20T00:46:43.870944528Z`。
- Executor仍1081a1b5/cfad1b51，StartedAt=`2026-10-20T00:46:43.85551099Z`。
- 原Caddy仍54c8550b/f2a1290d，StartedAt=`2026-10-24T03:31:58.532588899Z`，restart=unless-stopped。

原FAILED、target、hold、adopted元数据、overlay、网站恢复result均原字节保留；Runtime/boot PASS。没有执行deploy-local.ps1（包括DryRun）、receive、Docker build/load、Compose up、服务重启、数据库/计费/用户数据写入、Cloudflare/DNS/裸域跳转修改、GitHub Push/main合并/旧生产链重启，PG03没有商业开放。

## 5. 交付与下一步

本轮入口激活无剩余阻塞，到此停止应用侧操作。当前相对线上只有工具/记录差异，**不需要为了证明入口而补发Gateway**。

今后有真实应用变更时，在正式工作区保存本地提交、完成适用检查并保持干净，使用已有PowerShell7：

```powershell
.\scripts\deploy-local.ps1 -DryRun
if ($LASTEXITCODE -ne 0) { throw '预检未通过，停止部署' }
# 确认本次应用更新已经授权后：
.\scripts\deploy-local.ps1
```

只读预演不是部署授权。当前仅工具差异会按合同提示no Gateway build needed；未来SQL/迁移/真实PG03敏感变化仍须单独审阅，后端仍先正式备份且数据库状态未知时不自动回退。裸域方案继续独立待授权，本次没有处理。

证据索引：authorization.json、production-before.json、github-gate.json、activation-execution.json、windows-preflight.json、final-state.json。冻结计划历史不被改写成执行凭证，本目录回执记录本次真实激活。
