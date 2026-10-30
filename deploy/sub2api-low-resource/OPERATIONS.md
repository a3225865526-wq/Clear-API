# 固定版本离线部署包

2026-10-31，Asia/Shanghai。仅准备；没有购买、连接或部署新旧VPS。该包保持账号池关闭，不能直接成为已验真实商用实例。

## 固定材料与生成

Gateway源 `92edfc345d9b9526379ed25363bb79abf9839874`；Sub2API 0.2.19源 `d851a38477597157e78450d30af2d8504764d085`。PG03原历史保留，master关闭，不开发。两套PG分别17.6/17.11，Redis7.4.11。离线镜像经典config/engine身份见images.lock.json；本地tag不是registry digest。前端已内嵌，不在VPS构建。原始new-api/QuantumNous版权和许可证保留在源归档与镜像中。

在Windows运行 `python package.py build <全新输出目录>`，再运行 `python package.py verify <输出目录>`。包包含全部六个运行镜像、固定Gateway源码tar、Compose、空secret模板、代理配置、备份脚本、厂商档案、关闭渠道规划及SHA256.json；不包含任何数据库、测试env、管理员密码、OAuth或Token。独立校验SHA索引真实性须通过受信渠道交付索引，校验和本身不是签名。

## 部署入口与当前实际限制

先验证Linux/amd64、Docker/Compose、磁盘余量、时钟、SSH主机指纹及端口。只使用pinned known_hosts、StrictHostKeyChecking/BatchMode/IdentitiesOnly，经SSH上传。不得使用GitHub Actions/GHCR部署。

Gateway日常更新仍用源码归档内 `scripts/deploy-local.ps1 -ConfigPath <私有配置>`，先加 `-DryRun`。已有helper仅部署new-api，当前Windows脚本明确绑定旧host/user/22；新IP不能只改配置即执行。**新目标的helper安装/身份绑定尚未验证，是实际部署阻断项，不得用TestTarget冒充生产或手拼receive绕过。** 本包Compose是新目标整机布局和本地演练资产，不宣称旧helper会安装六服务。新主机最终绑定须沿原安全helper完成，禁止恢复GHCR或另建第二套发布平台。

经另行授权后，Linux上非敏感预检：

```sh
python3 package.py verify /path/to/package
docker load -i /path/to/package/images.tar
docker compose -p clearapi-stack --env-file /protected/production.env -f compose.yaml config --quiet
```

load后逐项比对images.lock.json的engine_id；不得静默pull。不要打印完整Compose配置或env。参数模板的secret为空，未填写时config必须失败。密码使用足够强的随机hex以避免DSN转义歧义；迁移时保留原SESSION_SECRET/CRYPTO_SECRET及现有token-encryption key文件，不生成替代值。生产若原来启用token encryption，必须将其原环境/只读文件挂载合入并验证，当前新建空库模板不能替代旧栈完整配置。

## 网络、初始化与持久化

`ROOT`下gateway-postgres、gateway、logs；`SUB2_ROOT`下postgres、redis、app。备份单独放受保护目录并加密异地保存。Docker local日志每服务10MiB×3，六服务约180MiB上限；`/app/logs`是独立持久目录，不受Docker日志轮转保护，必须按实际文件设置系统轮转并保持审计保留。WAL上限是检查点目标，并非磁盘硬配额。

PG/Redis无发布端口；两套DB内部网络分开。Sub2API管理仅127.0.0.1:18882，通过SSH隧道访问原版官方后台。gateway网允许厂商出站，不能以网络名宣称没有外联。避免172.30.81.0/24与目标宿主网络冲突；若修改，必须同步固定edge IP与TRUSTED_PROXIES并验证实际来源。Sub2API仅信任实际Gateway代理地址，不信任客户端伪造转发头；正式选定账号时核对固定版server.trusted_proxies及security.trust_forwarded_ip_for_api_key_acl配置。当前模板未将动态Gateway地址固化进Sub2API可信代理，不能据此声称生产代理信任链已验收。

Gateway默认日志路径是/data/logs，即ROOT/gateway/logs（并非挂载/app/logs后就自动改到那里）。logrotate.conf同时涵盖两处路径；正式安装前按实际ROOT/保留政策核对，执行 `logrotate --debug <配置路径>`，再安装宿主调度。本轮没有宿主logrotate二进制验证，未宣称已安装；copytruncate有轮转瞬间丢少量文本的固有限制，资金审计以持久数据库为准。Docker local轮转只覆盖stdout/stderr，应用日志与数据库备份还须监控磁盘、按实际保留政策加密归档，不能宣称50GB硬配额已验证。

edge只监听回环18080；当前是HTTP本地演练入口。公网TLS、证书续期、Cloudflare/源站规则沿实际原反代配置迁移，未配置并验收HTTPS前不得公开。若再增加TLS代理进程，须纳入资源预算。SSE禁缓冲、read/send超时360秒；Gateway streaming timeout300秒。固定edge→Gateway只信任172.30.81.2；新增外层代理时不能直接信任任意XFF，须按真实代理链补充并验证。

初始化顺序：PG/Redis健康→恢复正式逻辑备份/全部原配置密钥→原有bootstrap/显式迁移核验→Gateway/Sub2API→edge。新空库使用已有 `/production-bootstrap`，从源码内 `cmd/production-bootstrap/bootstrap/config.go`取得实际参数：管理员密码文件、负责人批准的terms/privacy文件及版本、approved-by、candidate-commit；先默认dry-run，再在授权窗口execute，最后verify-only。它已有finance v1–v9及billing步骤，不自造SQL建账。不要在迁移库重复创建管理员、补余额或用新条款覆盖正式记录。

本地资源演练曾直接先启动Gateway，因缺显式Billing schema被保护性拒绝；用现有finance-migrate v1–v9及billing reservations/auth-snapshots/outbox/receipts/reconciliation/task-settlement CLI后健康。**不能把直接compose up当作完成所有空库初始化。** 生产迁移恢复的是原完整schema，不盲目重跑bootstrap或任何财务补录命令。新Sub2API的AUTO_SETUP只用于独立空库；生产独立管理员与JWT/TOTP密钥必须新建并加密保存。新实例若有条款门禁，由负责人本人在官方后台确认，不复制测试条款记录。

## 厂商、模型和预算

provider-plan.historical.json保留13平台31档案，其created=false/423是历史规划字段；不是当前状态查询或可导入payload。最新实际13组/13Key和正式条款证据在原final acceptance报告；这些合成实例记录不导入生产。12份渠道保持disabled、models空；TypeSafe只有System One专用协议，无ClearAPI资金入口，保持PROTOCOL_GAP，不能宣传13家全部可销售。

type59常规基址 `http://clearapi-sub2api:8080`，不带/v1；原生OpenAI按已验证路由可采用 `/openai`前缀，适配器追加/v1/responses。只有合法凭据与实有模型才能填写公开模型/mapping/售价，并经运营批准。每平台独立Key/原生组，禁Composite和Antigravity mixed scheduling。同平台来源若必须硬隔离，应有独立执行边界；2GB预算不包括13个独立执行实例。

账号concurrency、Key/用户速率与额度按原后台配置并实测；simple模式会跳过部分额度/窗口逻辑，`SIMPLE_MODE_KEY_RATE_LIMIT_ENABLED=false`，不能声称内部Key有硬USD预算。用户Token余额/预扣也不等于上游成本上限。edge最多8条同时活跃HTTP连接是整机保护，不是账号并发或收费预算，HTTP/2前端多路复用语义仍须目标验证。输出/重试/思考Token界限依实际厂商和协议证明；Gateway RetryTimes=0不能证明Sub2API零attempt。未有可执行厂商支出上限时标记COST_CAP_UNPROVEN，先不启收费调用。

原后台复用账号创建/编辑/停用/恢复、Key吊销、平台分组、并发、健康/配额、Usage、错误/冷却及批量操作。401按失效认证处理，403按权限/出口/授权排查，429按原厂限流冷却处理；不要对全部401/403套无穷重试或把429当永久失效。刷新仅对该账号类型原生支持的机制可用，API Key无OAuth刷新。连续异常先关指定type59渠道/模型，再停账号新调度，保留在途/审计/结算。普通type1渠道可独立工作，勿全站停机。

## 资金运营与关停

单Gateway、无Gateway Redis/batch、DURABLE_WALLET/BILLING_OUTBOX/FINANCE/reconciliation single instance及真实Finance Owner生效后才允许人工核查；Sub2API独立Redis不冲突。Owner必须是已存在且可登录的实际用户，不能照抄模板的空值或测试id。完整Finance UI须schema v9与capability ready。客户只在ClearAPI收费，Sub2API simple只记录内部Usage，不导入终端客户钱包。

每运营日09/17由Owner全分页查询 `GET /api/owner/finance/billing-reservations?scope=pending`；24h取证、72h升级并暂停异常来源。原请求/首次预留时间为7天起点，不用updated_at重置。关联原请求模型/价格、Sub2API账号/Key/时间及所有物理attempt，保存可信Usage证据，不能仅凭时间相近强行认同一请求。目前两服务没有已验证的通用可信唯一关联，未知Usage仍需逐笔核查。

调用现有 `POST /api/owner/finance/billing-reservations/:id/decision`，提交读取到的expected_state/applied/target、`0<=refund_quota<=applied`、证据reason及真实execution_ended。409刷新后核查，不能改快照绕过；唯一decision防重。政策A：晚到超预留差额平台承担，不追补。政策B：执行结束仍无可信Usage，最迟7天Owner逐笔批准符合条件退款，保留UNKNOWN事实；不按年龄批量退款。执行尚未结束须升级事故，不假勾结束。退款是追加调整，毛消费日志不重写；净额=历史扣款−decision退款。实际Owner和日常值守、异地备份职责在启池前落实，本包没有自动值守或超期通知。

## 备份、恢复、升级与回滚

先切断新请求、回调及定时任务并排空在途，暂停所有钱包/订单/执行器写入。只停止本项目edge/new-api/sub2api后：

```sh
sh backup.sh /protected/production.env /protected/backup-NEW
cd /protected/backup-NEW
sha256sum -c SHA256SUMS
```

脚本会拒绝仍运行的写入服务，数据库保持运行；两PG逻辑dump+globals、Redis SAVE/RDB、app/logs/env配置同取，umask077。另有独立log DB/文件/原PG03历史时按MIGRATION.md补齐，本脚本不声称覆盖仓库外未知挂载。备份加密异地后验证解密，设置实际定时任务和监控，不把脚本存在当作已受保护。要保护RDB与原AOF保持一致，恢复停Redis后仅恢复一种权威快照：RDB导入空目录、首次以appendonly no启动核对，再按Redis官方机制启用AOF重写并确认完成；不得将旧AOF与新RDB混用或丢失既有TTL。

在无公网的独立新库使用相同PG主版本：createdb→pg_restore --no-owner --no-acl→全部表/索引/约束与业务摘要校验；globals仅审查后恢复所需角色，不覆盖系统超级用户。恢复app/env/所有加密密钥和Redis，之后启动单写Gateway并verify-only。本轮实际已验证两PG的全表行摘要与两轮服务恢复，未恢复生产数据或外部支付。

升级用原helper：先身份/锁/新鲜加密备份及隔离恢复，再本地构建、SSH、helper健康检查。保留旧镜像、源码、env、helper state与数据库。禁止docker prune/down -v/删库。backend迁移不明时停写人工判断兼容，不能盲目回滚钱包DB；新栈有写入后回退必须带回最新一致数据，不允许旧快照覆盖新余额/订单。

## 正式启池前检查

1. `docker compose ... config --quiet`、`docker compose ... ps`；逐镜像核对lock、健康和OOMKilled/重启数；`free -m`、`df -h`、`docker stats --no-stream`，检查实际日志/备份临时空间。
2. `ss -lntp`确认PG/Redis不公开、管理只回环；通过SSH隧道登录官方后台，不公开18882。
3. 在新VPS对拟用厂商确切域名做DNS/TLS与无凭据目录GET，记录响应码/出口；401仅到鉴权入口，403须排查，不能当真实模型成功或假设美国必可用。不要用生成请求充当免费网络测试。
4. Finance capability ready、schema v9、单实例/no batch/no Gateway Redis、Owner值守和恢复材料到位；所有旧/新写入方已围栏。
5. 确认拟用厂商、商业授权、凭据导入位置、实际模型/协议/售价、输入输出/思考上限、账号并发、Key限额、物理attempt及单次/累计预算、批准人/核查人/停用方法。没有凭据的平台继续CONFIG_READY/REAL_CALL_UNVERIFIED，不要求买齐13家。
6. 经另行费用授权，仅对指定范围做JSON/SSE/必要取消的有限真实调用，逐笔核对真实Usage/钱包/Token/预留/outbox/消费日志及内部Usage；不自动开放客户模型。异常只关相关池入口，保留普通渠道和在途账务。

本地验证不替代以上目标现场检查。2GB仅短时组合验证，无可靠商用容量承诺；真实厂商、目标出口、TLS/helper绑定及生产恢复保持未验。
