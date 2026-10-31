# ClearAPI 商业功能与基础设施综合交付

交付时间：2026-10-31T23:59:03+08:00。仅 Windows 本地隔离实验；实验分支 `codex/shared-infrastructure-optimization-20261031`。应用源码 `fa96a74df2b46c0f3617068e0f77fbdc2fae67fe`，离线操作配置源码 `dd68a49afb63fc66433f5b54b2e4eb4a8ada4ef4`。源码基线和原 release-v2 均保留；未推送 GitHub、未操作生产、未修改 DNS、未发送真实收费请求。

## 交付结论

分组方案B、Sub2API官方后台独立HTTPS入口及管理员快捷链接、全渠道监测诊断/配置入口与七语言已完成。本地单元/协议Mock、三数据库、完整四并发A/B、备份恢复与故障门禁通过。共享PG未达到收益门槛，最终保留双PG、单Redis、单Nginx；未牺牲账务或恢复能力强行合并。

| 状态 | 判定 |
|---|---|
| CODE_READY | 本轮代码及固定镜像已完成 |
| LOCAL_ACCEPTANCE_PASS | 限已记录的Windows隔离/Mock范围 |
| OFFLINE_DEPLOYMENT_READY | 新独立离线候选可进入新VPS隔离验收准备 |
| NEW_VPS_ACCEPTANCE_PENDING | 真实2GB宿主、网络、磁盘、正式TLS/访问保护、备份恢复待现场 |
| REAL_PROVIDER_ACCEPTANCE_PENDING | 没有真实收费探测；真实授权/合同/预算和厂商验证待补 |
| PRODUCTION_LAUNCH_PENDING | 未上线；PG03、D发布限制保持 |

## 新候选与保留物

新文件：`D:\AI\API\runtime\commercial-shared-20261031\release-v3-commercial-optimized.tar`，684103680 字节。
SHA256：`d7125e8bcb40fd98c62ed575860ab551ee3a67b796f144f1e84e2fa5f3966131`。
应用源码：`fa96a74df2b46c0f3617068e0f77fbdc2fae67fe`；操作包源码：`dd68a49afb63fc66433f5b54b2e4eb4a8ada4ef4`；镜像 config：`sha256:a97ded8dc68021c352f444a86639678ee3e1c9c2ab113c66d4460a97aa8c3855`，Engine ID：`sha256:f79eee256d3f22768a6aa0bfaa6ac5a796d8ac6f4a4ec7c45bfdd8811aff4c2c`。版本清单与SHA256旁置。包内严格清单28个文件加SHA256清单，含源码tar、6镜像、Compose、Windows helper、模板、迁移/恢复说明及备份工具；没有真实密钥、用户库或本地私密测试运行时。

构建使用Dockerfile指定Go1.26.1固定摘要、缓存中已验证模块、CGO=0、GOWORK=off；标准无网络Dockerfile构建因未缓存go mod下载失败后，用锁定Bun1.4.4/Rsbuild2.1.6构建前端并在指定编译器离线重编全部6个程序，复制到原批准运行时基底。没有关闭TLS/校验。此构建路径与标准Dockerfile的Bun1.4.0不同，完整记录在 build-manifest.json，不伪称标准Dockerfile原样构建成功。VERSION源文件为空，二进制保持该原始版本语义，候选标识在OCI标签/清单。

正式源码基线 `c8e4c06fe9bbd282a48deb16d6714b9688f08bf6`、本轮起始HEAD `1b059920ec2eb951c4c2e9205f604ba1f92f5a54` 均保留。原 `release-v2.tar` SHA256仍为 `ed0c3dba06d435852892483ca3647f635d3308f35536673bc62c1a2600a1f01d`；桌面备份、历史工作区、备份分支和正式源码基线未替换。不推送、不改main、不删Docker数据卷。

## 负责人要求逐项答复

| 项目 | 结论 |
|---|---|
| 1–5 分组完整性、老用户、旧Key/缓存/协议、防扣费、重启用 | 已实现，本地覆盖与实际旧Key六协议/在途SSE通过；不宣称数学上穷尽所有第三方自建入口 |
| 6–8 独立域名、快捷入口、访问保护 | 配置/代码/本地TLS、IP、原生登录通过，正式域名和公网验收待现场 |
| 9 PostgreSQL共享 | 不采用；完整应用Mock可运行但收益不足、故障域扩大 |
| 10 Redis共享 | 本来只有一个，保持，不新增 |
| 11 反向代理共用 | 本来一个，继续复用并增加可选受控TLS虚拟主机 |
| 12 实际内存收益 | 最终拓扑节省0；共享实验RSS少72.8MiB但父级工作集多33.4MiB |
| 13 CPU/延迟 | A/B单轮CPU采样3.92/3.54秒，窗口不同；非流中位96.0/101.3ms，无可靠提升结论 |
| 14 备份恢复 | 实际加密双库全表/Redis/TTL/两轮重启/SSE通过；全实例恢复在专用共享数据库实验通过 |
| 15 账务安全 | 最终合成 applied/token/log=378、唯一消费27、待投递0；钱包守恒，没有自动退款 |
| 16 新release-v3 | 已生成独立文件，原v2未覆盖 |
| 17 2GB边界 | 只验到2CPU/1536MiB父级/50GB/四并发Mock，宿主512MiB是规划预算，非真实2GB整机验收 |
| 18 新VPS待验 | 正式秘密/角色、TLS、访问控制、单写部署、实际恢复、资源容量和监测授权 |
| 19 真实厂商缺口 | 生产全渠道矩阵、真实合同/单次日预算、非生产实测授权及真实Usage核对 |
| 20 下一阶段 | 可进入新VPS隔离部署准备，不能直接公开商用上线 |

最终账本：{"quota": 999636, "token_used": 378, "applied": 378, "reservations": 27, "logs": 27, "unique_logs": 27, "log_quota": 378, "pending_outbox": 0}。总合成钱包 + 已结算量保持1100000；全部测试渠道/Relay Token已关闭，Sub2API账户/内部Key关闭，全局监测暂停，无真实合同或监测目标自动创建。最后新鲜加密备份完成后测试栈停止，数据保留。本地合成备份及其恢复私钥另存私有runtime，未进入源码、证据或部署包。完整故障注入中产生的预期OOM单独报告，正常A/B和升级阶段OOM为0。

## 实际验证与文件

- Go：`go test ./middleware ./controller ./service ./model -count=1` 四包通过；固定Go1.26.1定向组/管理/监测测试通过；真实SQLite3.50.4、MySQL8.4.11、PG17.6矩阵通过。`go test ./relay ./relay/channel ./relay/helper -run 'Test(Cancelled|RelayRequestsStopUpstream|NewTaskAPIRequestInherits|StreamScannerHandler_ClientCancel|ResponseModelRetry)' -count=1` 通过取消/重试相关回归。
- Frontend：Bun typecheck、改动文件oxlint、production build通过；分组/入口5测试、监测22测试，最终管理错误映射14测试和公共错误6测试通过。一次因预期仍是旧英文合同错误而失败，修正测试为新的可操作翻译文案后通过；没有把失败算通过。i18n sync完成；既有全站其他翻译缺口不在本轮范围。
- Helper：`python scripts/ci/test-local-deploy-stack.py` 4测试通过；Windows原生helper fresh/upgrade/dry-run/repeat实际验收；TLS配置/原生资源登录、完整A/B、连接/依赖故障、加密恢复、OOM/磁盘/中断/新写入保护均有本轮receipt。
- 原始全包回归出现的两项过时夹具先在初始HEAD复现，再仅修正分数价格预期与503重试夹具；未修改生产计费/429停止语义。保留 baseline-existing-failures.log 与修复后结果。

关键文件与分项报告：分组模型和双层中间件、分组/订阅控制器、管理员入口、监测readiness/调度心跳/控制器、七语言前端、TLS覆盖/渲染器、安装/helper/打包脚本。详见同目录五份专项报告及 `evidence/commercial-shared-20261031/`。唯一权威总控只追加本轮结果并校验原历史前缀哈希，不覆盖历史。

未推送GitHub，因此未触发远端CI；本轮只声明已记录的本地验证。

本轮边界：真实生产监测根因及全渠道矩阵未取证；未把每个厂商/全部渠道类型或未支持group/pool监测适配宣称通过。未运行真实支付、真实厂商调用、公网DNS/TLS、新VPS现场迁移；这些明确列为后续验收，不阻断本轮安全可执行的本地交付。
