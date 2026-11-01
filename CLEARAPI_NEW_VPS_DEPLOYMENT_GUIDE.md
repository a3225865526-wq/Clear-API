# ClearAPI 新 VPS 现场安装指南

日期：2026-11-01（北京时间）。仅适用于 release-v3-financial-fix 新 VPS 候选。旧 VPS 持续营业，本指南不授权迁移、生产部署或 DNS 修改。

## 当前现场状态

未提供可独立核验的新 VPS IP、SSH 身份、端口、known_hosts 指纹或 root 登记配置。仓库 target 模板仍为占位符；此前 localhost 隔离靶机不是新 VPS。未连接任何 VPS，以下全部为待执行现场步骤，不计验收通过。

## 到场只读预检

由负责人提供新机的真实 host/user/port、私钥安全路径、独立核验过的主机指纹及目标 daemon 身份。不得复用旧生产目标配置，不允许 StrictHostKeyChecking=no。

使用明确的新目标及 pinned SSH 执行只读检查，保留脱敏回执：

```sh
id
uname -m
cat /etc/os-release
nproc
free -h
df -h / /var/lib/docker /opt
lsblk
swapon --show
docker version
docker compose version
docker info --format '{{.ID}} {{.Architecture}}'
python3 --version
gpg --version
ss -lntup
sudo -n nft list ruleset
```

按系统实际防火墙选择 nft/ufw/firewalld 的只读命令，不自动安装/改规则。检查公网出口 DNS/HTTPS 和时间同步；仅对负责人确认的目标站点执行无凭据 GET/TLS 握手，不向收费模型发请求。CPU 架构须 amd64；保留六服务低资源配置及其全部限额。此前只验证过 2GB/50GB 受限模拟、4 并发 Mock，不能推断实际吞吐。Helper 至少要求可用内存 512MiB、空闲磁盘 8GiB；还须额外计算真实备份、恢复临时目录、WAL/AOF、双份镜像及回滚峰值，不能仅满足下限便认定容量足够。

确认 Python 的 file_digest/data 解包过滤器能力（建议3.12+）、Docker Compose、GPG、sudo/visudo、logrotate 均符合现有安装器要求。核对 SSH 身份权限及专用部署用户，私钥不能进入部署包。

## 离线材料与安装

完整交付目录包含安装包 release-v3-financial-fix.tar、四份本轮报告、来源回执和 SHA256SUMS。使用外部独立保存的 SHA 核验外层交付归档，再展开；按 SHA256SUMS 核验各文件。安装包内 SHA256.json 校验准确文件集合。

```powershell
Get-FileHash .elease-v3-financial-fix.tar -Algorithm SHA256
```

在全新受保护目录展开安装包。只复用该包中的 SIX_SERVICE_RUNBOOK.md、install-stack-target.sh、五个 Helper 和 target 模板；不使用旧 VPS upgrade 适配器，不绕过 activation、daemon、root 配置哈希和前驱检查。目标模板已绑定本轮应用提交；真实包 SHA/bytes、源码归档 SHA、Helper SHA 从交付回执填写，不猜测。

按包内 SIX_SERVICE_RUNBOOK 第3节登记 cleardeploy、新机公钥、独立 root 配置、GPG备份公钥和私有 env/secrets。Owner 用户、条款和隐私文件必须为负责人真实批准的材料。空库初始化只用于未来授权的新机隔离验收，不把其初始用户数据覆盖生产数据。既有生产 token/session/crypto 等密钥应在正式迁移时原样安全恢复，不能重新生成。

```sh
sh /protected/package/install-stack-target.sh /protected/package \
 /protected/server.json /protected/production.env /protected/secrets \
 /protected/cleardeploy.pub /protected/backup-public.asc
```

登记后，使用包内 Windows Helper 先 DryRun；实际安装仍留待现场授权和条件满足：

```powershell
pwsh -NoProfile -File .\deploy-local.ps1 -StackTarget -DryRun -ConfigPath C:/PROTECTED/new-target.json
# 未来新机安装获准后：
pwsh -NoProfile -File .\deploy-local.ps1 -StackTarget -ConfigPath C:/PROTECTED/new-target.json
```

不要手动 compose up 替代安全 Helper。保留 production-bootstrap、Finance/Billing/Token 加密等既有门禁，不关闭自动安全控制。新包没有本轮数据库结构迁移。

## 网络与 Sub2API

六服务保持 new-api、gateway-postgres、sub2api、sub2api-postgres、sub2api-redis、edge。两套 PostgreSQL、Redis、内部应用端口不得公网发布；按现有配置只允许 edge/管理回环入口经受控 TLS 代理接入。新机业务域名、TLS证书来源/续期、可信代理和防火墙均待现场验证，不修改现有 DNS。

Sub2API 正式管理域名 sub2api.clear999.com，管理员邮箱 3225865526@qq.com，保留原生管理员登录。Cloudflare Access 策略、指定邮箱登录、未授权/绕源拒绝等验证完成前，公网管理入口保持关闭。admin_access=false、账号池默认关闭；不把域名或邮箱当作已配置凭据，不发送真实收费请求。

## 现场验收与停止

核对六镜像 config 身份、六服务健康/重启/OOM、两套前端、TLS和来源链、内部端口、日志轮转、备份加密异地解密及真实数据量下的恢复空间。财务测试只使用隔离合成数据，不能为验收在生产补扣或退款。新机安装验收、生产数据迁移、DNS切换、启池及真实厂商收费验收分别记录，未执行均标待验。

现场发现身份、容量、资金摘要或健康不符即停止并保留回执。数据恢复、最终停写切换及回滚按 CLEARAPI_NEW_VPS_MIGRATION_AND_ROLLBACK.md 单独批准执行。
