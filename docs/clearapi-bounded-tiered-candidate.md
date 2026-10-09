# ClearAPI 受限表达式计费候选

基线：生产 revision `fc564273ff9192b81b0d3f92c996471b9129783c`。
工作分支：`codex/bounded-tiered-protection`。现进入授权的代码审查、PR、CI 及 RC 准备；最终 Commit、PR 和 Run ID 以 GitHub 发布结果为准，生产部署不在本轮授权内。
集成目标 main（2026-10-09 实际远端读取）：`490867c254eda79a477ba33856abfa01a39bdd94`。该 main 只新增备份身份验证相关修改，与六个源码文件无差异或合并冲突，完整保留。

## 最小实现

严格保护只新增 Channel 1 / OpenAI / gpt-6-luna / default / Chat Completions 的受限表达式入口。
精确匹配现有表达式或仅修正长上下文缓存读取的表达式、v1 token 模式和 500000 Quota/USD。
请求计价、钱包准入、渠道验证及派发前预留均保留门禁。
必须启用 enforce/quarantine，使用已批准的 channel/model Profile 和正数上下文、输出、单请求额度上限。
价格及渠道准入读取临时副本，避免改变 controller 的首次渠道选择状态。

预留复用既有表达式区间上界：P/CR/CC/CC1h 各按完整批准输入包络求保守上界，覆盖可见输入缓存写入及未知缓存类别。
类别不能同时占据全部输入，但预留允许保守高估；实际结算仍使用原始 Usage 归一化、冻结表达式及 QuotaRoundChecked。
任意表达式、固定收费、请求倍率、工具、多模态、透传、自动分组、渠道覆盖与非批准模式不获得该入口。
未修改结算或 R7 Zero-chunk 代码、数据库 schema、历史 Reservations。

## 独立价格配置差异（待运营方批准；本文件不执行）

2026-10-09 生产只读查询确认：模型模式 tiered_expr；Channel 1 type=1/status=2/models=gpt-6-luna/group=default/model_mapping=NULL。
该模型没有 ModelRatio/CompletionRatio/CacheRatio/CreateCacheRatio/ModelPrice 显式条目。
供应商公示价格由运营方确认，单位 USD/百万 Token；不是逐请求账单。

仅修改 `billing_setting.billing_expr["gpt-6-luna"]` 的最后一项：

```diff
- : tier("upstream_2", p * 0.04 + c * 0.15 + cc * 0.05 + cc1h * 0.05 + cr * 0.04)
+ : tier("upstream_2", p * 0.04 + c * 0.15 + cc * 0.05 + cc1h * 0.05 + cr * 0.004)
```

批准后的完整字符串：

```text
len <= 272000 ? tier("upstream_1", p * 0.02 + c * 0.1 + cr * 0.002 + cc1h * 0.025 + cc * 0.025) : tier("upstream_2", p * 0.04 + c * 0.15 + cc * 0.05 + cc1h * 0.05 + cr * 0.004)
```

通过现有模型定价编辑/保存机制读取最新版本，只替换目标模型；保留其他模型映射和分组。
无需新增内置价格或修改历史快照。已进行中的请求继续按各自冻结表达式结算。
保存原字符串及配置版本作为恢复依据；恢复封闭运营应先关闭注册和 Channel 1，保持 ENFORCE、保留在途预留和 review。

## 首发配置建议（已接受为候选上限，正式开放仍待最终批准；未写入生产）

| 配置 | 既有快照 | 建议目标 |
|---|---|---|
| CLEARAPI_BILLING_PROTECTION | 未启用 | true |
| CountToken | true | 保持 true |
| billing_reserve_policy.mode / unknown_action | enforce / quarantine | 保持 |
| policy/Profile max_request_quota | 5000 / 5000 | 1000 / 1000 |
| channel::1::model::gpt-6-luna cache_mode | warm | cold（不依赖命中保证） |
| hidden_total_max | 4480 | 保留 4480，作为批准包络而非供应商已保证事实 |
| hidden_input/read/write/write_1h | 640/3840/0/0 | cold 归一化为 4480/0/0/0；预算仍覆盖所有缓存拆分 |
| model_context_max / model_output_max | 400000 / 30000 | 16384 / 1024 |
| margin / floor | 1.10 / 0 | 保持 |
| Profile approved/version | true / 1 | 运营批准后 true / 2；策略版本同步递增 |
| ModelRequestRateLimitEnabled | false | true |
| duration/count/success_count | 1 分钟/0/1000 | 1 分钟/10/10 |
| ModelRequestRateLimitGroup | 无 default 覆盖 | default=[10,10] |
| RELAY_CONCURRENCY_GLOBAL/USER/TOKEN/IP | 4/8/4/32 | 4/1/1/2 |
| RetryTimes | 0 | 保持 0 |
| 注册、Channel 1 | 关闭 | 本候选交付不更改；最终商业批准后另行启用 |
| Payment/Store/其他渠道 | 关闭 | 保持关闭；账号池排除 |
| 新人/邀请赠送 | 0 | 保持 0 |
| default 有效倍率 | 1 | 保持 1，不新增用户组覆盖 |

限制由服务器 Profile、请求形状门禁、额度预留、模型限流和并发中间件执行，不能仅依靠客户端输出上限或 Token 模型限制。
现有内存限流按用户 ID 的秒精度滑动窗口计数；并发限制在进程内生效。严格保护要求 Redis OFF，首发沿用单服务实例，不宣称跨副本聚合限流。无需限流重构。
16384 包含估计可见输入及 4480 隐藏包络，故可见估计上限为 11904；输出最多 1024。
测试最大保守预留 706 Quota（平台标称 $0.001412），单请求拒绝上限 1000 Quota（$0.002）。
这些是授权包络内的预算界限，不是供应商实际费用已证明，也不保证上游永不违反包络。
越界 Usage、失败或中断未知消费按现有保留/review 处理，不自动按零消费退款。
此上下文限制安全排除 >272K 档位；只填较小 max_tokens 不足以排除长输入。

## 验证与证据边界

新增集中测试 `service/protected_tiered_test.go`，复用现有隔离钱包/数据库夹具。
A：551 普通输入 + 3840 缓存读取 + 6 输出 → $0.00001930 → 9.65 → 10 Quota。
B：551 普通输入 + 3840 缓存读取 + 5 输出 → $0.00001920 → 9.60 → 10 Quota。
集成回放增加断言：CR=3840、P=551、Len=4391；cold Profile 下预留 <=1000、state=settled、最终 applied=10，且不产生 review marker。
测试值验证本地计算，不证明供应商真实扣费。

最终定向命令：

```text
go test ./service -run '^TestProtectedTiered' -count=1
# 分别使用独立 SQLite、PostgreSQL 17、MySQL 8.4 数据库
go test -race ./service ./middleware ./relay/helper ./pkg/billingexpr -run 'TestProtectedTiered|TestProtectedTextBudget|TestR7|TestInterruptedStream|TestCompletedStream|TestModelMemoryRateLimitReservesConcurrentSuccessAdmission|TestRelayConcurrencyReleasesAdmission|TestReservationDomain' -count=1
```

全部最终检查通过。首次测试曾暴露 tokenizer 夹具未初始化；后续重复数据库测试出现夹具主键复用及名称超过 varchar(32)；均仅为测试夹具问题并已修正。
测试 Docker 网络仅为 internal，无真实 Provider 请求；没有生产写入。

仍缺逐请求供应商账单、reasoning 是否包含在 output 的可验证事实、缺失 cache_write 的实际收费事实及中断消费。
不再等待客服解释；不把 upstream_partial 提升为 exact。
可先导出 88token 后台现有 A/B 记录，要求请求/响应 ID、UTC 时间、模型、价格版本和至少 8 位美元小数的最终扣费相互关联。
如果历史记录无法关联，另行申请一次独立核账；本轮不执行真实请求。
Reservations 11/12 共 52 Quota 待复核保持不变。

技术候选可申请受控更新审批；审批范围须包括代码合并、RC、价格配置修正、上述限额及未知消费保留/review/退款政策。
当前不是 CONFIG-ONLY，也不是生产已具备该修复。商业公开开放需在批准的更新完成后另行批准。
