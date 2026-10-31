# CLEARAPI 新 VPS 六服务部署最终离线验收

最终更新：2026-10-31T13:55:17+08:00。**OFFLINE_DEPLOYMENT_READY：通过，限本报告固定版本、小型测试数据和受限并发范围。** 新 VPS、真实厂商、生产上线仍各自待验，不等同生产 READY。

本轮没有连接任何 VPS、修改生产/DNS/Cloudflare、迁移真实数据、改真实钱包、启用正式账号池、调用收费厂商或推送 GitHub。new-api/QuantumNous 身份与归属保留。

## 交付与十项结论

1. **原 Windows helper 已完成六服务适配及最终实测。** 仍是 Windows→原 SSH→原 Python helper，新增显式 StackTarget/stack 模式；无第二套生产部署系统或 GHCR 依赖。
2. **固定 v2 包能安全离线部署。** 新空目标真实 root 安装、pinned SSH DryRun/完整上传、原生 production-bootstrap status=verified、六服务健康与两后台 HTML 通过；初始池渠道0、Sub2账号/Key0。
3. **旧流程保持兼容。** 原默认分支和全部既有方法源码/AST保持；新旧activation互斥，root配置/daemon/目录/密钥/sudo隔离。本轮未重验或操作旧生产；旧fixture仍正确拒绝敏感MIGRATION路径，不能将整批新目标运维变更直接走旧Gateway发布。
4. **2GB/50GB受限模拟通过，容量有边界。** 父1536MiB/1.7CPU/无额外Swap约束六服务+嵌套dockerd，另预算宿主512MiB/0.3CPU；实际50,000,000,000bytes ext4承载Docker和/opt，OS根在外并预留8GiB。不是完整2GB VM或任意流量商用证明。当前仅证实4并发Mock；存在触限回收，需保留限额、拒载及监控。
5. **双库/Redis恢复、重启及安全配置回退通过。** PG17.6/17.11全表行、schema/索引/约束摘要匹配；恢复至新目录，保留旧库；Redis RDB导入→AOF重建、非空值/TTL通过；两轮重启账务不变。有新资金差异时拒绝旧快照覆盖，必须人工对账，不自动退款/补扣。
6. **无已知应用代码阻断。** 修复的是helper对Compose内存字符串与Docker整数的比较缺陷，两行int转换，未放宽限额。Go/Billing/ORM/schema/前端/PG03/13平台未改；最后版本重放和受影响回归已补齐。
7. **最终包位置**：`D:/AI/API/runtime/six-service-helper-20261031/release-v2.tar`；560998400bytes；SHA256 **`ed0c3dba06d435852892483ca3647f635d3308f35536673bc62c1a2600a1f01d`**。工具commit `8839580ed5989b1b0ce679babf7e3f35f2babd8b`；旧5df0ea包保留作失败历史，不再作为可发布最终包。
8. **新机不可提前核验项**：真实IP/SSH指纹、amd64资源/CPU steal/Swap/磁盘、出口、TLS/来源链、DB/Redis/管理端口防护、logrotate调度、真实库存大小下备份和恢复空间、异地解密恢复。
9. **厂商收费仍缺**：合法凭据/商业授权、实际模型/协议、单次及累计和物理attempt预算/输出上界、真实Usage与钱包核对、Owner值守及调用授权。无需买齐13家；未验协议或无成本边界的平台继续关闭。既定未知Usage/退款政策保留。
10. **可以认定有界的OFFLINE_DEPLOYMENT_READY**；CODE_READY通过；NEW_VPS_ACCEPTANCE_PENDING、REAL_PROVIDER_ACCEPTANCE_PENDING、PRODUCTION_LAUNCH_PENDING独立保持。实际迁移和启池仍须分别授权。

## 实际六服务与参数

清单为new-api、gateway-postgres、sub2api、sub2api-postgres、sub2api-redis、edge。PG/Redis先健康，应用依赖对应状态服务，edge依赖Gateway。初始PG目录分别 `/opt/clearapi/stack/data/gateway-postgres` 与 `/opt/clearapi/stack/sub2data/postgres`；Redis为sub2data/redis；应用为data/gateway、sub2data/app，日志data/logs。恢复使用独立recovery目录及受保护env，旧数据不删除。

三网络分别gateway、internal gateway-data、internal data。edge .2/Gateway .3固定，动态池.128/25；Gateway/Sub2API来源信任明确。只发布回环18080和18882，数据库/Redis不发布端口。公网TLS和真实代理链待现场。

内存上限Gateway448/Sub2API384/PG各256/Redis96/edge64MiB，总1504MiB；CPU总2。Go目标320/288MiB、GOMAXPROCS1；SQL open10/idle2；PG shared_buffers32MB/work_mem1MB/maintenance16MB/connections20/WAL256MB；Redis48MB/noeviction/AOF everysec、pool16/idle2。无Gateway Redis/batch，PG03关闭，生产钱包/Token加密/Owner/条款门禁实际启用。edge连接上限8只是拒载，不是8并发真实厂商承诺；SSE超时360秒/Gateway300秒，最新恢复后SSE实测12.484秒，不冒充长期稳态验证。

最终正向资源原始样本见final-closure.json；cgroup读数前两行为current/peak：`1583902720` / `1610960896` bytes。故障OOM是刻意edge12MiB注入，应与正向阶段分开解释。磁盘样本：

```text
Filesystem           1-blocks       Used Available Use% Mounted on
/dev/loop2           48891371520 12389793792 36484800512  25% /vps
```

最低剩余8GiB且不少于包4倍只是固定底线，不是完整真实库备份预算。dump、snapshot tar、密文和新旧恢复库会同时占盘，必须按真实库存量上调root minimum_free_bytes并实测。Docker日志local 10MiB×3，应用logrotate debug/force在最终环境通过；copytruncate有瞬时文本丢失边界，资金事实保留于DB/备份。Swap不能代替容量保证。

## 证据与恢复边界

最新证据为 `evidence/new-vps-six-service-20261031/final-release-v2/`。Windows仅访问127.0.0.1:18964；测试父容器内存/CPU限额实际生效。所有已用脚本和非敏感JSON在该目录；私钥、env、Token、dump和解密材料只留仓库外私有runtime，包不含这些材料。

- 官方固定包空库部署和立即重复部署通过；实际资源/额外网络/配置文件漂移均被拒绝，复原后健康。原生ready必须明确verified，非0失败和not_bootstrapped不能出成功回执。
- 新一轮4普通+4池+2SSE，共10次，每次input10/output2/quota14；钱包999860，Token/applied/消费日志140，10唯一请求/outbox0；Sub2Usage新增4且内部钱包不变。池测试只读恢复以前Owner确认条款的合成库，未伪造新确认或使用真实厂商账号。
- 新鲜GPG备份 `20261031T053013-5017700d`，密文SHA `76253b00b2dd911dc885bb3973897e816001a27966ed2a789963508d4c02eae6`，两PG及Redis真实恢复通过。恢复后第11次SSE成功，最终钱包999846，Token/applied/消费日志154，11唯一请求/outbox0。
- v2重跑PG启动、Redis启动、迁移命令失败、edge健康失败、真实edge OOM、SSH切换中断、合成钱包+1差异；全部安全停止/保留证据并按规则恢复，资金不变或差异被拒绝。迁移注入是故障命令，不声称覆盖所有真实DDL半迁移。
- 镜像缺失/摘要不符、损坏/4KB中断上传、错误pinnedKey、实际ext4低于8GiB、内存阈值、sudo旧模式/任意config拒绝复用此前未变路径的真实证据；没有再次填满宿主盘。内存阈值不是全机OOM。
- 恢复后重新部署**官方v2包**、再次同包部署通过；ledger、state和容器身份幂等不变。最终测试渠道/Key/账号调度关闭、relayToken禁用；新鲜加密备份完成、六服务有序停止、全部本轮父容器停机核实，数据/卷保留。

切换前可恢复前驱；switching后默认人工恢复。只有同Gateway config ID、资金摘要未变、root批准精确绑定当前数据，才能恢复旧配置并保留当前DB。新写入/未知迁移/资金差异必须取证对账；工具不会用旧快照抹掉新交易。恢复必须精确backup/目标/daemon/解密材料SHA和当前数据一致，落入新目录而不删卷。

## 版本、验证及主机故障历史

Gateway源92edfc345d9b9526379ed25363bb79abf9839874；Sub2API源d851a38477597157e78450d30af2d8504764d085。应用镜像未变，不重跑全部历史/前端/三库；本轮实际数据库是PG17.6/17.11，不外推其他版本/方言。Linux/amd64镜像清单：

| 镜像 | 经典config ID |
|---|---|
| clearapi-sub2api-postgres:17.11-266d3c257594 | sha256:ab20e934b27ba3ff927c1b4c9ff8cf6c32f315f743efe68dd3441182a87fb085 |
| clearapi-sub2api-redis:7.4.11-858f009f9709 | sha256:f84b0c4678011602b9b98c227a4dcd5468bf8b088b02fdd4165cb7758bad8058 |
| clearapi-sub2api:d851a384 | sha256:62cb3769069c6a9c3c7fa708defd71229fe2befe396fc7cda32720ec69631f82 |
| postgres:17.6-alpine | sha256:d741b376874687de90374fd34f55c6b2760e8f7bd7e4ae5cd47f50757fc08cf8 |
| clearapi-local:92edfc345d9b9526379ed25363bb79abf9839874 | sha256:a70bf2ccf6da4442666175236754d14bdf2ded5e92c4ffbfcf5756fdeada848c |
| clearapi-edge:nginx-8541484afbc9 | sha256:5253dc86cc93ac6249902934655c6f7c959d8caa45a8c2ecc0b95953834d8ee8 |

images.tar SHA b3f148e24329dddf7e1177347bac005e64b3382dd25aab5b0c91fff1a1d2775c；gateway-source.tar SHA d41cf9c4d86da20ee54b55b00a3a0c4db4ce5eba290d8bb159155f08f8bdb3cb。所有repo digest/26文件摘要/工具身份详见包内lock、SHA256及operator-source.json；来源须独立核验，不只信随包SHA。

实际执行：两项Python工具回归PASS；package.py build/bundle及26项SHA验证PASS；真实root安装、pinned Windows DryRun/SSH部署/幂等、备份恢复、故障和运行漂移验证见上述JSON；helper Python编译/PowerShell语法及diffcheck通过。已知运行凭据字面量扫描PASS，不把它当全面安全审计。

之前Docker API500/超时阻断属真实失败历史。用户明确批准Docker重启；常规停止失败后只结束Docker进程和docker-desktop WSL，未关闭Ubuntu。启动曾报缺失数据设备与格式化失败，观察C仅约0.6GB；立即停止自动重试，未手动格式化、恢复出厂或删除原Docker数据盘（Docker自动格式化尝试失败，已停止重试）。中断后再次观察engine恢复、磁盘余量变化，未擅自声称由我们的存储迁移修复（本代理未移动Docker数据盘）。原失败证据和初版helper误判均保留，最新v2已补齐闭环。

## 新机到位后的最少工作

独立核实IP/SSH指纹及资源→提供合法Owner/密钥/条款、GPG异地恢复材料→填写包内Windows/server模板→按SIX_SERVICE_RUNBOOK安装及DryRun→现场TLS/来源链/端口/日志调度/真实库备份空间和恢复→另授一致停写迁移、单写切换/订单钱包核对及DNS操作→少量合法厂商/费用Usage验收→Owner最终启池授权。未到位内容不能提前签通过。

唯一总控仍是 `D:/AI/API/worktrees/clearapi-controlled-launch-20261020/CLEARAPI_MASTER_STATUS.md`，本轮追加保留全部历史。原5df0ea初版、prototype/final-install历史证据保持，当前正式交付请只使用v2包SHA。
