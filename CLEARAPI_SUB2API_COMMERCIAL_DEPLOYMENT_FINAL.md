# CLEARAPI — Sub2API 商业部署准备最终交付

2026-10-31，Asia/Shanghai。**本地商业功能差异核对、资源组合Mock、备份恢复及非敏感离线包已交付；仅可保留默认关闭的部署候选。2GB可靠商用容量、目标VPS原安全helper适配及真实厂商验收仍BLOCKED/UNVERIFIED，不能宣称已完成生产一键部署或可收费启池。** 本轮不连接新旧VPS、不写生产、不迁正式库、不调用真实收费接口、不推GitHub、不改变生产分支。PG03保持退出/关闭和全部历史。

## 基线、复用与实际改动

先读指定唯一总控最新23:52记录、七份原报告及相关固定配置/源码；以PG03退出后的应用92edfc345d9b9526379ed25363bb79abf9839874为基线，GitHub备份8e26646021cd966299e59041dd173f9878d533b6仅复用历史，不作远端写入。新工作区 `D:/AI/API/worktrees/clearapi-sub2api-commercial-deployment-20261031`、分支 `codex/sub2api-commercial-deployment-20261031`。原主工作区大量dirty修改、原Sub2API工作区未提交报告均保留，无覆盖/合并。

新增deploy/sub2api-low-resource的六服务Compose、固定images.lock、空secret模板、nginx配置、离线打包/校验工具、停写备份脚本、日志轮转配置、OPERATIONS.md/MIGRATION.md。未修改Gateway/Sub2API应用、鉴权、核心Billing、ORM、schema/迁移实现、前端或PG03。复制的provider-plan.historical.json明确为历史规划，created=false/423字段不能当当前状态。代码/配置与报告在本地提交，准确SHA以本工作区Git及最终回执为准，不制造应用缺陷修复或空提交。

原16例双服务Mock、账务/取消/缓存/重试、人工核查并发防重、两轮恢复、客户端等证据复用；21份规范化SHA逐项匹配，没有重跑完整历史矩阵、13家逐模型验证、全量CI或三库兼容矩阵。三CLI原解析/模拟派发有效；Cherry实机/真实模型及真实支付/外部交付/邀请返奖仍维持原未验状态。

## 用户要求的逐项答案

| 项目 | 最终事实与状态 |
|---|---|
| 1. 13平台/31档案 | 固定Sub2API支持范围的认证、原生平台/组/Key、模型发现/映射及禁用规划已整理；原合成实例13组/13Key已创建。**CONFIG_READY**不等于生产配置已导入或13家可售。12个常规协议模板关闭；TypeSafe仅System One，ClearAPI无入口/DTO/计费适配，**PROTOCOL_GAP/CLOSED** |
| 2. 合法凭据 | OpenAI/Codex、Anthropic、Gemini、Antigravity、Grok、Kimi、GLM、DeepSeek、MiniMax、OpenCode Go/Zen、Command Code、Cline、TypeSafe全部没有本轮获授权的真实凭据和收费验收。OAuth成功也不是商业转售许可；API Key/订阅/Bedrock/Vertex按档案分开，不假造认证 |
| 3. 运营后台 | 复用官方完整后台；既有API实证覆盖创建/分组/停恢复/Key吊销/Usage/错误，源码支持编辑、并发、健康/配额、冷却、原生批量与对应刷新机制，足够首版范围。未重写第二套后台/数据库，未宣称全部页面和每种凭据刷新均实测 |
| 4. 映射/限流/隔离 | 原模型别名与C/R/M及SSE已验；本轮受限池4并发有新Usage。simple同平台组不能硬隔离账号来源，不支持Composite/高级字段保证；固定账号范围须真实执行边界。关闭账号用POST /schedulable，不把PUT成功当已停调度。401/403/429依原机制区分，重试/费用硬界按拟用厂商验证。普通渠道独立 |
| 5. 唯一最终收费 | ClearAPI唯一客户钱包与结算；Sub2API simple内部Usage，原专用执行用户余额0无客户扣费。原正常/重试唯一结算、outbox/receipt幂等与退款证据保留。新受限环境24普通+4池成功，成功费用392 units；另4个503保留40未知units，最终毛消费/Token/applied432，钱包净减少432，无重复日志 |
| 6. UNKNOWN/7天核查 | 已有Owner API和唯一decision/CAS路径可执行，流程/实际参数见OPERATIONS.md。每日09/17全分页、24h取证、72h升级/停来源、从可信首次预留起7天逐笔批准符合条件退款；晚到超预留平台承担、不追补。执行未结束不假确认，无自动批量退款。必须落实真实Owner/值守；通用可信跨服务唯一关联未实现，不宣称未知费用自动精算 |
| 7. 2核2GB/50GB | 六服务+嵌套Docker父cgroup1.5GiB/1.7CPU，另预留宿主512MiB/0.3CPU；OOM0，但回收压力明显。不是完整2GB虚拟机，50GB未强制限盘。详见低资源报告 |
| 8. 可承受范围 | 实测4并发短Mock：普通24次、池4次，未得出真实长SSE或可收费QPS。2GB可靠商用验收BLOCKED；建议最低候选2核4GB，磁盘按库存/备份余量决定50/80GB，升级配置也未实测 |
| 9. 部署包 | **离线镜像/源码/配置/备份与迁移资产齐备，校验通过；生产一键执行入口尚未闭合。** 原deploy-local.ps1明确绑定旧host/user，原helper只部署new-api，不能安装本六服务布局。新目标helper安装/身份绑定与布局适配仍是实际工具阻断，不能把Compose演练冒充原安全工具已支持新机，也不以TestTarget绕过 |
| 10. 新机条件 | 购买/目标SSH指纹、Linux/amd64兼容、原helper目标适配、受控TLS/来源链、凭据安全导入、正式库/secret清点、当前一致备份和隔离恢复、单写切换围栏、指定Owner、真实出口及拟用厂商/模型/费用批准。无须买齐13家，无须重复询问已定政策 |
| 11. 必修代码阻断 | 未发现新的业务应用/Billing代码缺陷；**新VPS安全helper的目标/六服务支持尚未验证/完成，不能承诺以后完全不需部署工具适配。** TypeSafe为明确未实施协议范围，保持关闭。若要求全自动未知Usage精算或simple内部Key硬USD预算，现版本不满足；本轮沿已批准人工政策，不重写账务 |
| 12. CODE_READY | 原92应用/官方Sub2API及既有商业/客户端/type59能力保持；新增离线打包/停写备份和受限Compose准备完成。Code-ready不代表所有厂商/目标部署工具/容量ready |
| 13. LOCAL_ACCEPTANCE_PASS | 21份原证据hash、六服务健康、24普通Mock+4池并发、钱包/内部Usage、两PG全表恢复摘要、两轮服务重启、11项备份SHA、Redis AUTH拒绝、无Key401、离线15文件校验等，具体范围见下节 |
| 14. REAL_PROVIDER_ACCEPTANCE | 所有真实厂商未验；需合法商业凭据、准确模型/协议/售价、单次及累计预算、输出/思考/attempt硬界和来源支出限制、真实Usage/钱包关联与Owner核查，另行授权后仅拟用范围有限调用。普通Token/渠道额度、simple内部Key/窗口配置不能编造成供应商硬成本上限 |
| 15. 商用部署候选 | **默认关闭源码/配置候选：可以保留。2GB正式商用部署/公开账号池：不可批准。** 新目标部署工具与容量/安全/恢复门禁仍须完成，真实厂商先有限验收再由运营批准公开模型 |
| 16. 真实改动/验证 | 见本报告下节、低资源报告、evidence/sub2api-commercial-deployment-20261031及本地Git；无应用源码变更，不重跑无关完整矩阵，不产生生产或付费请求 |

## 实际命令与结果

- `python .../lab.py setup`：首轮空库因保护性schema门禁未完成；使用已有CLI后，修正Compose三处真实配置问题，六服务最终healthy。没有把首次尝试写PASS。
- `python .../migrate.py`：独立本地PG finance v1–v9、billing reservations/auth snapshots/outbox/receipts/reconciliation/task settlement现有CLI成功；初次结尾edge因IP冲突失败，之后修正；不冒充生产迁移或新的三库验证。
- `python .../business.py`：前期4次Mock连接错误保留原退款事实；修正挂载更新后4并发24成功，336 units、24唯一消费；result为business.json。
- `python .../recovery.py`：首次活跃system_instances摘要漂移；停写后独立恢复PG17.6/83表、PG17.11/105表全行摘要匹配；两轮重启账务快照不变；result为recovery.json。
- `COMPOSE_PROJECT_NAME=low sh /pkg/backup.sh /pkg/lab.env /private-backup-2`及`sha256sum -c SHA256SUMS`：两库/globals、Redis及app/env备份成功，11项OK；首份CRLF失败备份保留。未做生产GPG解密/异地恢复，未独立验RDB导入；敏感合成备份不出私有runtime。
- `python .../pool.py`首4请求503（账号PUT没有开启schedulable），保留40未知units；续验`pool-resume.py`使用原POST /schedulable，4并发成功/内部Usage新增4/最终钱包999568、Token/applied/毛消费432，消费32/request32、outbox pending0。原确认条款合成库只读克隆至lab，不对新生产实例伪造ack。
- `docker exec ... redis-cli ping`先移除REDISCLI_AUTH：NOAUTH；缺Key `/v1/models`401；最终inspect六容器无OOM。资源父memory.peak约1536MiB、触限回收，并非通过真实商用容量验收。
- `python deploy/sub2api-low-resource/package.py build .../final-package`：六镜像固定身份、源码tar，15项SHA校验PASS；镜像514803200 bytes、源码45936640 bytes；不依赖GHCR/Actions。`sh -n backup.sh`、Python编译、`git diff --check`通过。空secret环境模板应拒绝启动；实际合成env config --quiet通过。
- 21份原证据规范化hash全匹配。未重跑全量Go/前端/三库/16协议矩阵；没有新的ORM/schema实现变化。宿主logrotate调度/实际TLS代理链、全机2GB/50GB、真实VPS恢复/支付回调、真实厂商与费用硬界**未验收**，不写通过。

详细首轮失败和修正见attempts.json；驱动在仓库外 `D:/AI/API/runtime/commercial-deployment-20261031/`，文件读取仅本地合成或固定源码，不引用生产DSN。期间未读取正式客户数据、生产配置secret或访问生产。

## 交付路径和关闭状态

- 本工作区 `deploy/sub2api-low-resource/OPERATIONS.md`：部署材料、固定版本/镜像、初始化、网络、预算、Owner核查、备份/升级/回滚与真实启池清单。
- `deploy/sub2api-low-resource/MIGRATION.md`：实际PG引擎一致快照、用户/钱包/订单/兑换码/邀请/日志/密钥完整性、双写围栏、回调/任务去重、DNS窗口和新写入后回退。
- `CLEARAPI_SUB2API_LOW_RESOURCE_DEPLOYMENT.md`：预算/实测/适用边界/OOM与磁盘风险和BLOCKED结论。
- 非敏感可搬运目录 `D:/AI/API/runtime/commercial-deployment-20261031/final-package/`，包内15项SHA索引；没有测试数据库、test.env、可用Token或真实secret。完整运行镜像和固定源码都在包内，不依赖其他本机tar路径。

最后只停止本轮新建lab，保留其数据/失败记录/私有备份；原Sub2API三服务继续原状态。clone内部Key inactive、mock账号不调度，所有本轮lab进程已停且无外部公开端口；生产PG03/master及普通渠道未查询或改动，不把历史生产现场写成当前实时核实。唯一权威总控追加本轮结论并验证原字节前缀保留，不更新其他工作区的旧总控副本。没有新远端提交、部署、迁库、DNS切换或收费调用。
