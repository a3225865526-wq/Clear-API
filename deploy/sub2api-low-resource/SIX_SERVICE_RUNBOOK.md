# 六服务安全部署与恢复操作单 v2

本包扩展同一套 `scripts/deploy-local.ps1` → SSH → `clearapi-local-deploy.py`，新增显式 `-StackTarget`/`stack-*` 模式。旧VPS入口、配置、sudo规则及单Gateway方法保留。不要在旧VPS安装或激活本目标；安装器和新模式都会拒绝已有旧部署activation/state的主机。PG03不继续开发，历史仍由迁移清单保护。

原OPERATIONS.md保留6f92278ad历史，此处是新目标当前操作入口。实际验收结果、版本、摘要和未验边界以 `CLEARAPI_NEW_VPS_SIX_SERVICE_DEPLOYMENT_FINAL.md` 及其证据为准。应用仍是new-api，原QuantumNous归属、版权、许可证与嵌入前端均保留。

## 1. 材料与目标

六服务固定为new-api、gateway-postgres、sub2api、sub2api-postgres、sub2api-redis、edge。Gateway源92edfc345d9b9526379ed25363bb79abf9839874；Sub2API源d851a38477597157e78450d30af2d8504764d085；PG分别17.6/17.11，Redis7.4.11。images.lock.json同时记录本机engine身份和离线tar内的经典config_id；不能将containerd manifest ID当经典config ID。Gateway源码tar的Git pax comment、文件SHA及OCI revision均核验，所有镜像必须linux/目标架构。固定前端已嵌入，不在VPS构建。

生产目标需Docker/Compose、Python（需hashlib.file_digest和tarfile data过滤器）、OpenSSH、sudo/visudo、GPG、logrotate及标准Linux工具。版本兼容性在真实目标预检；本地已测版本见最终报告，不能推断所有发行版都已验。数据库和Redis不发布端口；edge/管理仅回环，公网TLS和来源链在新VPS另验。新目标目前只准备amd64离线镜像，不伪造arm64验证。

## 2. Windows本地制作和核验

在包含已提交工具的工作区：

```powershell
python deploy/sub2api-low-resource/package.py build D:/RELEASE/clearapi-six-service
python deploy/sub2api-low-resource/package.py bundle D:/RELEASE/clearapi-six-service
```

生成自包含目录和同名.tar，输出完整SHA/bytes；保存独立受信SHA索引。构建仅收集明确允许的源文件和五个helper文件，不枚举ignored生产env、密钥、测试库或dump。工具输入必须已提交且无差异。operator-source.json记录工具commit与各helper实际SHA；应用源归档独立固定92。未变应用镜像复用既有已验本地产物，不依赖GHCR/Actions。

已有获准镜像归档时可使用 `build <全新目录> --image-archive <原images.tar> --image-archive-sha256 <独立核实的SHA>`；不需要重新导出或连接本机Docker。仍须验证完整六镜像集合、Linux/amd64及固定config_id，不能以本机Docker故障为由跳过源/镜像校验。

`package.py verify <目录>`验证准确文件集合和全部SHA；不能单靠随包校验和认证包来源。Windows配置使用target.windows.example.json，填真实host/user/port、受控私钥/已独立核实的known_hosts、准确包SHA和服务器root配置SHA。私钥不能被其他普通主体读取，三个受控输入不能被其他普通主体写入；不要把配置放在不受控共享目录。不得用TestTarget/FixturePath运行六服务模式。

## 3. 新目标首次登记（须在未来授权的新机上做）

1. 独立核实新IP、SSH指纹、Linux/架构/磁盘/实际RAM/出口、时钟与Docker daemon ID。不要盲信ssh-keyscan；新主机还未购买时不能预填有效指纹或声称SSH已验。
2. 通过发行版标准方式准备专用cleardeploy身份和公钥登录，禁用该身份的密码及keyboard-interactive登录；保留现有运维登录通道。sudo只给本包stack.sudoers.example中的固定模式，不给shell、任意config path或旧目标模式。
3. 负责人在root私有暂存区准备独立production env、固定token-key（64 hex）、管理员密码文件、经其批准的terms/privacy文件、Sub2API管理员/JWT/TOTP/PG/Redis凭据，以及GPG备份公钥。迁移已有库必须保留原SESSION/CRYPTO/token/JWT/TOTP密钥，不生成替代值。私钥和恢复解密材料加密异地保存；目标日常备份只需GPG公钥。
4. 填target.server.example.json为server.json：实际target_id、daemon_id、架构、准确包SHA/bytes、源归档SHA、三个已审helper SHA、备份公钥完整fingerprint、bootstrap输入与真实Owner引用，以及五个protected_inputs的SHA。它们绑定env、token-key、密码文件和legal文件，变化会拒绝部署。placeholder不是生产凭据或授权。
5. 在独立校验包SHA后，以root执行（以下路径均为受保护暂存材料）：

```sh
sh /protected/package/install-stack-target.sh /protected/package \
  /protected/server.json /protected/production.env /protected/secrets \
  /protected/cleardeploy.pub /protected/backup-public.asc
```

安装器只登记新目标的root文件、固定sudo和公钥，检查实际sshd策略、visudo、logrotate语法及helper preflight；不修改/重载全局sshd，不购买主机、不部署服务。输入应由root持有，不允许其他主体写入。启用文件是root600的 `STACK_TARGET_APPROVED_V1:<target_id>`。根目录、配置和模块不可被部署用户改写，三个helper实际字节必须匹配root独立配置。新目标不得与旧/测试目标混用activation或daemon ID。

## 4. 正式部署路径

```powershell
pwsh -NoProfile -File scripts/deploy-local.ps1 -StackTarget -DryRun -ConfigPath C:/PROTECTED/new-target.json
pwsh -NoProfile -File scripts/deploy-local.ps1 -StackTarget -ConfigPath C:/PROTECTED/new-target.json
```

两步都使用原SSH参数BatchMode/StrictHostKeyChecking/IdentitiesOnly与原二进制stdin传输。DryRun仅读目标，检查config/daemon/helper/env/key/已部署镜像、健康、实际生产readiness和前驱generation；不构建、上传或改服务。execute必须仍匹配前驱，root独立批准的包SHA/bytes必须相同；中断/超长/损坏上传、文件集合/源码/镜像/platform/配置不符均拒绝。

目标安装位置是 `/opt/clearapi/stack`：releases/incoming/backups/receipts，data下Gateway/主PG/logs，sub2data下Sub2API/app/PG/Redis。所有服务内存有界，合计1504MiB；SQL10/idle2、两PG缓存32MB/20连接、Redis48MB/noeviction+AOF、Go目标320/288MiB等沿上轮结果。新包显式保留实际CLEARAPI_PRODUCTION与钱包/任务/Token加密/Owner安全门禁。GOMEMLIMIT不是硬RSS保证，8连接代理上限不是供应商USD支出限制。默认无Gateway Redis/batch、PG03关闭、初始无公开池模型。

空库顺序：验证/加载固定镜像→PG/Redis健康→空状态新鲜加密备份→原版production-bootstrap --execute→应用健康/实际readiness→edge→真实六服务/两前端身份→状态和回执。bootstrap的Finance v1–v9、Billing/Token加密/Owner/legal门禁不绕过；中断后只允许原包重入原工具，不清库或重置管理员。对已有目标，先新鲜停写备份，再切换；内部Key/账号/模型和任何收费启池不由部署自动完成。

重复同包先重新核验，返回already_applied，不重建用户、不再次迁移或动钱包。镜像tag不可用相同名字改成不同config ID；数据库/Redis引擎版本变化须单独审迁移，不能当普通应用升级。应用验证在实际Gateway内exec，不用另一个占用同固定IP的compose run。

## 5. 备份、正常恢复和日常轮转

新鲜备份覆盖两PG逻辑dump/globals、Redis稳定RDB、app/logs/env、固定key/legal等配置，经GPG公钥加密并保存完整proof（target/daemon/env/镜像、数据和schema/索引/约束摘要、密文SHA）。父进程流式计算行摘要，不在2GB机器内聚合整库JSON。密文和proof须加密异地保留，私钥另存。目录中的旧明文恢复材料不递归打入后续备份。

维护窗口先禁止新入口/回调/计划任务并排空在途，执行：

```sh
sudo -n /opt/clearapi/bin/clearapi-local-deploy.py stack-backup --keep-fenced
```

这会停止edge/Gateway/Sub2API写入，保持两PG/Redis运行。不要假定stop等于所有外部执行免费结束；未决Usage依既定Owner政策保留。普通 `stack-backup` 备份后恢复同一配置并验证健康；keep-fenced留在维护状态供恢复。

Owner独立解密选定密文并核对密文/proof/明文SHA，将明文tar安全放在root600 `/opt/clearapi/config/stack-restore.tar`；root600 `stack-recovery.json`填target_id、backup_id、plaintext_sha256、observed_data_digest和真实approved_by。observed_data_digest来自当前停写后的fresh backup proof。若与选定恢复点不一致，必须先核查新写入，不能覆盖为旧余额/订单。

```sh
sudo -n /opt/clearapi/bin/clearapi-local-deploy.py stack-recover --backup-id <准确backup_id>
```

恢复只写新目录：两套新PG目录用同版本工具恢复，逐表行数据和schema/索引/约束摘要必须一致；Redis先以RDB/appendonly no导入，再启用并等待AOF重写，之后才恢复正常启动。所有旧目录/数据库保留，不用down -v/删库。新env文件由root生成并绑定状态，后续部署沿恢复后的实际数据路径。新数据摘要不同，即使提供通用批准，也拒绝旧快照覆盖。

logrotate.conf对应新ROOT的两处实际日志位置，需加入目标宿主的标准logrotate调度；安装器验证语法，调度是否已启用在新机验收。Docker local轮转每服务10MiB×3；应用日志、DB/WAL、AOF和备份另计。copytruncate有瞬时文本丢失边界，资金审计留在持久数据库和备份中。严禁为了省内存/磁盘关闭账务或删除审计；磁盘不足8GiB或备份空间不足就停止新发布。

## 6. 故障、关入口和回滚边界

- 上传/校验/容量/前驱失败：不切服务。损坏包保留在私有incoming供调查，不假写成功回执。
- received/backup_ready且尚未触及新状态：`stack-recover`可恢复原配置/服务；从未启动的初始容器只移除容器记录，不移除数据。
- switching后：默认拒绝自动回数据库。journal、加密备份、旧镜像及数据全部保留，入口关闭；SSH中断要等待原持锁操作终结，再检查状态，不并发重发业务请求。
- 确认Gateway镜像config完全相同、资金快照未变，仅配置故障时，Owner可批准恢复前一配置/镜像并保留最新数据库：root600 stack-recovery.json填action=resume_previous_images、target_id、candidate_bundle_sha256、observed_data_digest及approved_by，随后 `stack-recover --resume-previous`。这是保留当前数据的配置恢复，不是完整业务回库。应用镜像变了或资金不一致就拒绝，交Owner核查。
- 有新钱包/订单写入、未知迁移或关键账务差异：停入口、保留最新库和全部证据。不能恢复旧快照清空差额；必须先人工取证/对账/制定保持新交易的恢复方案。工具不自动退款、补扣或重发未知请求。

OOM、Redis拒写、DB启动/迁移、健康失败都要核对服务State/OOMKilled/RestartCount及journal；失败不能仅靠Docker自动restart认定已恢复。容器内存/代理并发限制和50GB预留只是本地已测条件，不证明任意真实长SSE/流量下可靠商用。

## 7. 新VPS到位后的最少现场工作

核实真实IP/指纹/daemon/权限和资源、包SHA与密钥保管；安装新目标配置并DryRun；核对公网TLS/证书续期/可信代理来源及SSE超时、端口不公开DB/管理；使用真实库存大小证明备份和恢复临时空间；按MIGRATION.md完成停写一致备份、隔离恢复/钱包订单核对、回调/任务去重与单写切换，经另行授权后迁移。当前主机和DNS不因离线验收而变化。

真实启池另需拟用厂商商业授权/合法凭据、模型/协议/售价、输出及物理attempt/单次和累计预算的可执行边界、实际新机出口、真实Usage与客户钱包核对、Finance Owner值守。simple组不是同平台账号来源硬隔离，内部Key配置不保证USD硬预算；无可执行成本边界的平台继续关闭。无需买齐13家，TypeSafe专用协议缺口保持关闭。
