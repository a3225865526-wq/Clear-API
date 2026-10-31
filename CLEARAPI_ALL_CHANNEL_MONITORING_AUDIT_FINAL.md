# 全渠道监测专项审计与修复

交付时间：2026-10-31T23:59:03+08:00。仅 Windows 本地隔离实验；实验分支 `codex/shared-infrastructure-optimization-20261031`。应用源码 `fa96a74df2b46c0f3617068e0f77fbdc2fae67fe`，离线操作配置源码 `dd68a49afb63fc66433f5b54b2e4eb4a8ada4ef4`。源码基线和原 release-v2 均保留；未推送 GitHub、未操作生产、未修改 DNS、未发送真实收费请求。

## 核心结论

已修复公共诊断和操作入口缺陷，没有删除安全门禁，也没有证据证明公共 Probe 执行器或调度器导致所有渠道同时失效。现有告警含义为：Allowlist、Published、Enabled、非 Archived 是独立条件，另须有效费用合同和启用的预算策略。普通 API 成功不等于监测费用授权完成。全局策略默认暂停、监测表明确 opt-in 初始化，均为有意的安全设计。

现有前端缺少合同/策略编辑入口和每个门禁的解释，后端管理列表缺少 allowlist 就绪诊断；这些是本轮修复的公共可观测性/可配置性缺陷。尚不能把负责人网站的“全部暂停”归因于单一生产根因：本轮禁止操作生产，且本地可读库存没有 channel16。未伪造生产全渠道扫描结果。

## 实际渠道矩阵

已有 Windows 本地 SQLite `D:/AI/API/runtime/new-api/data/one-api.db` 只读扫描，完整模型清单在 `local-monitor-inventory.json`：

| 渠道ID / 名称 | 厂商/协议能力 | 模型 | Published / Enabled / Archived / Allowlist | 合同 | 全局策略 | 测试/后端原因 |
|---|---|---|---|---|---|---|
| 3 / 桥 | type1，OpenAI兼容；不能仅据此确定真实厂商 | codex-auto-review及该行JSON全部模型 | 不适用：无监测表/条目 | 未发现监测合同 | 未初始化 | MONITOR_NOT_INITIALIZED；先明确执行监测迁移，默认暂停 |
| 4 / Lunora | type1，OpenAI兼容；实际监测协议未配置 | 该行JSON全部模型，含gpt-image等 | 同上 | 同上 | 同上 | 同上；不是自动启用依据 |
| 16 / 负责人确认的DeepSeek | 普通调用有效是负责人提供的事实；预期openai_chat需核对 | 未从生产读取，不猜测 | 未验证 | 未验证 | 未验证 | 负责人给出的门禁错误不能精确推出全部字段值 |

最终新隔离栈的另外4条合成渠道 `six-ordinary/six-pool/six-sse/six-vip` 全部关闭、无 monitor 条目、无费用合同；监测存储由明确选择的 helper 迁移初始化、全局策略暂停。逐字段机器矩阵见 `evidence/commercial-shared-20261031/monitoring-state-matrix.json`（null表示未配置/未检查，不能当作false）。本地实际调度器为running、master_node=true、周期86400秒；这不代表生产调度器已验收。完整行级返回和调度器现场状态见 `local-stack/final-monitoring-catalog.json`，不与上述已有本地库或生产库存混用。

## 公共链路检查及修复

1. 全局开关/Allowlist/发布/Enabled/Archived：保留现有独立语义；发布不自动启用。Allowlist接口的 enabled 表示成员资格，属于既有契约，不误改成监测执行开关。
2. 新增只读 readiness：按 monitor/channel 返回具体未满足条件；直接调用同一个当前费用授权逻辑，核对渠道状态、模型/协议、Endpoint/凭据绑定、期限和最坏费用上限，不联网、不写合同、不预留费用。所有未建配置渠道单列；可批量复制矩阵。配置就绪不等于动态日预算或并发额度此刻仍可用，执行前仍事务校验。
3. 增加单条费用合同 JSON 编辑及全局预算策略 JSON 编辑，复用既有 Root 接口、写保护和明确确认。合同保存不自动启用；全局策略启用的确认明确说明可能允许授权范围内定时探测。未填造假合同、未批量发布/启用、未全量加入Allowlist。负责人可批量审阅矩阵与共同策略，实际目标授权仍逐项确认。
4. 前端按服务端 readiness 禁用测试按钮，逐项显示渠道、原因和对应配置位置；API错误集中映射并支持七语言，初始化未完成明确指向部署管理员迁移。未完成租约、预算/频率/并发限制、失效合同仍拒绝执行。
5. 调度器/系统任务/执行器：现有监测由系统任务的每日流程触发，各目标 interval 是最小间隔，并非保证按该秒数独立轮询。增加本节点心跳及 master 标识，区分运行、未在本节点启动、心跳陈旧；不能把非主节点无心跳误判成全局停机。保留队列、任务去重、租约和同一 Probe 执行器。Mock 验证暂停/恢复/不重复执行；另用测试overlay明确验证未启动、心跳陈旧、恢复运行三态（heartbeat-observation-final.log），没有改动生产源码或二进制。前端验证陈旧心跳提示。本地实际running不等于生产运行。未发现公共调度代码故障。
6. 数据库/缓存：readiness直接读取当前数据库；真实 admission、预算预留、租约防重、实际Usage和结算逻辑保持。三数据库实际回归及迁移幂等通过。初始化默认不建目标、合同或预算授权。
7. DeepSeek对照：测试数据库明确以16号合成条目验证独立门禁。原生Probe继承渠道认证和Endpoint，合同绑定当前配置；没有凭普通调用成功自动签发独立监测凭据。模型和协议由明确监测配置约束。合成16号不等于真实16号已探测。

## Mock 回归

SQLite3.50.4、MySQL8.4.11、PG17.6 的 `TestConsoleMonitorControlMatrix`、`TestConsoleProbeConfiguredExecutionMatrix` 通过；`TestConsoleProbeArbitraryModelsAndNativeProtocols` 和现有控制器/执行器套件、前端管理/交互/历史套件通过。覆盖：全未配置的阻断诊断、单/多目标成功、单项禁用隔离、未发布/归档/非Allowlist拒绝、缺预算及超预算拒绝、频率/并发/未决租约、费用合同失配/过期/超限、暂停/恢复、前后端一致与无假Usage/无越权费用。调度停止信息仅本节点观察，不虚构跨节点集群健康验证。

原生协议 Mock 覆盖 Chat/Responses/Anthropic/Gemini及已有图像/音频/视频等协议路径；不表示40+厂商每个渠道类型已真实验证。group/pool 等未具备受支持监测执行适配的目标继续显示 integration pending，不能通过Mock或合同绕过。D图像部署未批准的边界不变。

验证日志：`monitoring-three-database.log`、`monitoring-backend.log`、`monitoring-controller-final.log`、`frontend-mapping-final.log`、`monitoring-frontend.log`。七语言同步的 missingCount=0；本次新增文本均补译，仓库既有其他页面未翻译项未冒称已清零。

## 未完成的真实验收

不能回答“生产全部渠道现在可以监测”：生产行级矩阵、channel16 当前门禁值、真实调度器运行状态均未读取。所有拟启用的真实渠道都仍需负责人提供明确监测授权、真实费用合同、单次与每日预算和频率；无需监测的渠道应继续关闭。负责人可用新增矩阵导出逐项审阅，然后在新VPS隔离阶段确认迁移、主节点心跳和权限，最后在明确非生产 scope/费用预算下做真实供应商探测。本轮真实厂商监测未验收，真实收费请求数为0。
