# CLEARAPI — 模型调用分析与经营概览只读对账

审计日期：2026-11-01。结论适用于北京时间 **10:02:33 的一致性快照**，补充读取截至10:05:41。要求的区间固定为 **[2026-11-01 00:00, 2026-11-02 00:00) Asia/Shanghai**，Unix秒 **[1793462400,1793548800)**。当天尚未结束，不能将本报告冒充完整日终对账。

## 1. 结论

差异不是已证实的“上游成本与用户售价差异”。两个页面的quota都来源于用户扣费语义，均以500,000 quota/USD换算。存在两项已证实的统计缺陷：

1. **模型分析漏掉42笔用量未知、保留预扣的消费日志，合计407,035 quota，即 $0.814070。** 这些请求走异常结算/outbox路径，没有执行普通消费日志路径中的小时聚合更新。因此两套数据源覆盖范围不同。
2. **经营概览把预扣释放当成已入账消费退款，再减一次。** 本日6笔type=6合计29,450 quota，即 $0.058900；对应预留均refunded/applied=0，全部没有任何日期的type=2消费日志。消费日志只记录最终扣费，不记录初始预扣，不能从其合计中再扣这6笔释放。

快照的精确桥接为：

```text
模型分析持久化聚合              288,296 quota = $0.576592（443次）
+ 未进入模型聚合的42条消费       407,035 quota = $0.814070
= 经营概览消费日志合计           695,331 quota = $1.390662（485条）
- 页面减去的预扣释放              29,450 quota = $0.058900
= 页面按当前公式得到的净消费     665,881 quota = $1.331762
```

**本日请求队列当前实际保留扣费为 $1.390662，其中 $0.847014 仍待核对；不等于全部已确认收入。** 对应请求账本的applied与485条消费日志逐笔一致，没有发现重复扣费证据。不能据此认定未知用量请求的收费最终正确，也不能自动退款。

原始用户读数440次/$0.5424/3083.8万不是本次快照值；未取得当时API原始响应、筛选参数及读取时间，不能宣称已逐笔复原该历史画面。**主要结构性差额已确定，原始画面的剩余时间/筛选差额尚未逐笔定位。**

## 2. 现场与证据身份

- SSH使用既有pinned known_hosts和专用身份接线；没有放宽主机验证，没有打印凭据。生产只执行Docker inspect、只读SQL、公开status GET。
- 实际运行应用revision标签：`732a2d5162c15604aa7ec7bbba12bb5c9d356031`，不是本地候选fa96/v3。镜像ID `sha256:3da62d984e98d0cddd6fbc1c04fe7cc502268cc23efacfa0c64286ef2f9da960`。
- 本地HEAD：`d0cfe6a2fa8a6aa8598a743d1975e6e03b5e9dec`。所列核心统计、日志、预留、outbox及两个页面相关文件对生产revision的定向git diff为空；按镜像revision映射源码，未重新构建/部署二进制。
- 实际数据库：PostgreSQL **17.6**，数据库`clearapi`，会话时区Asia/Shanghai。`LOG_SQL_DSN`未配置，因此`model.LOG_DB = model.DB`，本现场主库和日志库是同一库；没有跨库延迟可以解释本次差额。
- Durable wallet、billing outbox、single-instance reconciliation开关均为true；status实读`quota_per_unit=500000`。
- SQL以`REPEATABLE READ READ ONLY`事务读取，statement_timeout 25秒，最终ROLLBACK。第一次结构读取也为只读事务。数据库未写入业务记录。
- 开始/结束应用和PG容器ID、StartedAt一致。只说明本轮未重启现场，不代替所有生产活动的证明。

证据目录：`evidence/billing-analytics-20261101/`：

| 文件 | 内容 |
|---|---|
| `summary.json` | 快照时间、金额、Token、待核对与断言结果 |
| `consume-request-reconciliation.csv` | 485条log_id/request_id→reservation_id/applied关联；42条缺失聚合可直接筛选 |
| `refund-request-reconciliation.csv` | 6笔预扣释放及无对应消费证明 |
| `pending-review-56.csv` | 全部56条，区分本日/历史、实际保留与目标金额 |
| `hourly-quota-data.csv` | 10条小时聚合，合计443次 |
| `read-only-queries.sql` | 本轮实际执行的底层SELECT及只读事务约束 |
| `schema.json`、`closing.json` | 脱敏结构、运行身份、换算单位与结束身份 |
| `SHA256.json` | 上述证据摘要 |

证据不含API Key、Cookie、请求正文、完整环境变量或完整消费content。CSV中的request_id仅作关联标识。原始有限字段投影及采集脚本保存在本机`D:/AI/API/runtime/billing-analytics-20261101/`，未将整库或用户请求导出到仓库。

## 3. 两个页面、接口及计算公式

### 模型调用分析

调用链：

`web/src/features/dashboard/components/models/log-stat-cards.tsx` → `getUserQuotaDates`（`web/src/features/dashboard/api.ts`）→ 管理员`GET /api/data/` → `controller.GetAllQuotaDates` → `model.GetAllQuotaDates`（`model/usedata.go`）。非管理员为`/api/data/self`，按user_id过滤；管理员可通过username额外限制。

管理员无username时的实际查询形状：

```sql
SELECT model_name, SUM(count) AS count, SUM(quota) AS quota,
       SUM(token_used) AS token_used, created_at
FROM quota_data
WHERE created_at >= :start AND created_at <= :end
GROUP BY model_name, created_at;
```

前端`calculateDashboardStats`分别累加count、quota、token_used。金额是`SUM(quota)/500000`，USD展示不再乘组倍率；组倍率已经在结算时应用。图表的Top/Other分组不是本次42条差额的来源。

`quota_data`存主库：按user/model/小时/group/token/channel/node分组；created_at在`LogQuotaData`中向下取整到小时。普通`RecordConsumeLog`在调用`createLog`后执行`LogQuotaData`，在内存缓存累加，由`SaveQuotaDataCache`定期落库。DataExportEnabled默认true、间隔默认5分钟，本现场未发现持久化覆盖项；没有触发刷新或强制落库。

**该表既不是逐请求财务账本，也不是上游发票成本表。** 表中没有request_id/log_id、没有独立输入/输出Token字段，不能直接一对一join；本轮用user/model/hour/group/token/channel与普通日志分组比较，10组全部精确相等。

### 全站经营概览

`web/src/features/owner-finance/overview.tsx` → `financeRequest('/overview')` → `GET /api/owner/finance/overview` → `controller.GetOwnerOverview` → `model.GetOwnerOverview`（`model/owner_overview.go`）。路由入口分别在`router/api-router.go`、`router/finance-router.go`。

SQL核心为日志表的单次条件聚合：

```sql
SELECT
  SUM(CASE WHEN type=2 THEN 1 ELSE 0 END) AS requests,
  SUM(CASE WHEN type=2 THEN quota ELSE 0 END) AS quota,
  SUM(CASE WHEN type=6 THEN quota ELSE 0 END) AS refund_quota,
  SUM(CASE WHEN type=2 THEN prompt_tokens ELSE 0 END) AS input,
  SUM(CASE WHEN type=2 THEN completion_tokens ELSE 0 END) AS output
FROM logs
WHERE created_at >= :from AND created_at < :effective_end
  AND type IN (2,5,6);
```

实际实现以COALESCE处理空集，并计算错误数/活跃用户/平均时长。`effective_end=min(to,read_at+1)`。主卡金额由前端计算：`quota/500000`、`refund_quota/500000`、`(quota-refund_quota)/500000`。日趋势和Top10是另行展示，没有再加回主卡，未发现重复聚合。

来源是日志数据库`logs`。主库`users`提供用户统计；`finance_revenue_records`提供付费额度用户统计；`billing_reservations`和`billing_review_markers`提供当前待核对数量。这些表的金额没有被另加到上述消费卡片。

本轮没有使用Owner凭据重放管理API，也未接管浏览器。接口与前端公式由生产revision对应源码确认，金额从底层只读SQL独立复算；不把SQL复算冒称原始API响应采集。

## 4. 时间范围及显示精度

经营概览today固定北京时间自然日；本轮使用相同from/to。模型页默认是浏览器保存的滚动日期范围，独立于经营概览；前端`computeTimeRange`处理Date并转换Unix秒，不保证默认即北京时间今天。username、管理员/普通用户身份亦可改变范围。原图没有提供这些参数，不能假定两页当时完全相同。

模型API使用闭区间`<=end`，经营概览使用半开区间`<to`；本轮SQL统一为半开区间。在未来11月2日00:00数据尚不存在的本次快照中，该边界差异没有产生金额差额。历史任意分钟起止还可能受到小时向下取整影响。

金额格式通常是绝对值>=1保留2位、小于1保留4位；Token大数可缩写。因此`$1.39`不是底层精确值，不能从它反推695,000 quota。当前精确净值$1.331762按现有默认格式会显示约$1.33，**未复现原文$1.3311**。后者恰好等于以已舍入的1.39减去0.0589，但没有证据证明这是前端计算缺陷；源码使用原始quota相减，报告不猜测原截图的获取方式。

## 5. 统一区间对账结果

| 指标 | 底层结果 | 含义 |
|---|---:|---|
| 模型分析调用次数 | 443 | 小时聚合SUM(count)，包含6条结算失败但有用量的零扣费记录 |
| 消费日志 | 485 | type=2；485个不同request_id |
| 预扣释放日志 | 6 | type=6；与485条消费不重叠 |
| 错误日志 | 0 | 仅表示本日type=5为0，不能推导无失败/取消请求 |
| 输入Token | 31,409,207 | type=2的prompt_tokens之和 |
| 输出Token | 76,546 | type=2的completion_tokens之和 |
| 记录总Token | 31,485,753 | 当前quota_data.token_used合计也精确相同 |
| 模型页聚合额度 | 288,296 / $0.576592 | 用户侧聚合金额，不是模型供应商成本 |
| 用户请求实际保留扣费 | 695,331 / $1.390662 | 本日关联预留applied；与485条消费逐笔相等 |
| 预扣释放 | 29,450 / $0.058900 | 已退回的钱包预扣；6笔applied均为0 |
| 经营概览当前净消费公式 | 665,881 / $1.331762 | 少计了上列预扣释放；不是正确的最终扣费合计 |
| 本日待核对实际保留 | 423,507 / $0.847014 | 已包含在695,331中，不能再加一次 |
| 其余消费扣费 | 271,824 / $0.543648 | 扣除本日待核对保留之后的金额；不代表已核验供应商发票 |
| 实际供应商成本 | 未知 | 无上游账单/成本凭证，不能把模型页金额或售价表达式当成本 |

这里“本日”用于请求集的账本核对；预留本身没有created_at，按日志关联，并核验本日updated_at的applied合计相同。**不将updated_at当预扣发生时间。** 未取得零点钱包余额及期间全部资金流水，不能将请求队列applied冒称完整钱包现金流变化。

## 6. 差异逐类构成及关联

### 42条未知用量

42条日志的quota共407,035，输入/输出存储值均0，但实际用量**未知**。全部关联`settled`预留及`upstream_usage_unknown`标记。

路径为`relay/request_billing.go`异常处理 → `BillingSession.SettleUncertain` → 标记未知用量 → 保留预扣 → `RequestBillingCompletion`/`ApplyBillingCompletion` → outbox投递。该路径没有调用普通消费日志的`LogQuotaData`。outbox直接创建日志，经营概览将它们计入type=2，模型聚合不计入。

例如log_id **216**，request_id `202610312121599943336918268d9d6DeqwXGBv`，保留2,345 quota，Token未知；request_id文本前缀不能作为统计时间，实际以created_at Unix秒为准。全部42条可在消费CSV中筛选`in_model_aggregate=False`，再按request_id关联预留。

此路径保留预扣是既有安全策略，并非凭本次审计就能判为重复扣费。是否应退差额需上游实际用量和负责人决定。本轮没有查询付费供应商接口。

### 6条预扣释放

| refund log_id | reservation_id | quota | USD | 最终applied |
|---:|---:|---:|---:|---:|
| 537 | 492 | 2,752 | 0.005504 | 0 |
| 538 | 493 | 2,412 | 0.004824 | 0 |
| 539 | 494 | 2,752 | 0.005504 | 0 |
| 540 | 495 | 2,412 | 0.004824 | 0 |
| 541 | 496 | 2,752 | 0.005504 | 0 |
| 627 | 582 | 16,370 | 0.032740 | 0 |
| 合计 | | 29,450 | 0.058900 | 0 |

全库按相同request_id查type=2均为0条；不是仅本日找不到。账本均refunded，代码记录的是`Durable reservation refund`。因此这是经营统计重复抵减，不是钱包执行了两次退款。没有退款决策记录，6笔也不是人工核对后的消费退款。

### 原始数字与快照间剩余差额

直接按用户提供的展示数字：`1.3311-0.5424=0.7887`。已证实的结构性净差额是`0.814070-0.058900=0.755170`。剩余展示差额 **$0.033530** 尚不能逐笔归因。

算术上它等于：模型快照与原显示差`0.576592-0.5424=0.034192`，减去经营净值与原文差`1.331762-1.3311=0.000662`。这仅是展示数到当前快照的桥接，**不把$0.034192认定为已找到的3笔交易**。需原图API响应/筛选/时间才能进一步复原；当前数据库不能恢复已被覆盖的历史聚合缓存快照。

## 7. Token、售价、倍率与重复计费检查

### Token

- 当前443条普通路径日志：输入31,409,207、输出76,546，总计31,485,753；逐小时10组与quota_data计数/额度/Token全部精确一致。
- 其中442条观测标为`upstream_partial`：输入30,997,448、输出76,536；不能把“partial”改称完整精确上游用量。
- 另1条log_id **652**为`local_estimate`，输入411,759、输出10，总计411,769，保留16,472 quota并有`interrupted_unknown_usage`标记；不能把估算Token当上游精确实测。
- 42条未知用量日志没有可用Token数；加进经营概览后Token增加0是存储现象，真实用量不等于0。
- 用户的3083.8万若按30,838,000理解，与当前总量差约647,753；原数是缩写且旧快照不可得，精确输入/输出拆分无法恢复。本轮统一时点的两源记录Token差为 **0**，没有发现本日Token重复累加证据。

### 售价及倍率

消费元数据保存了442份DeepSeek售价表达式和1份GLM表达式。11月1日为周日，DeepSeek落off_peak：

```text
DeepSeek base = (非缓存输入*1 + 缓存读取*0.02 + 输出*4) / 1,000,000 USD
GLM base      = (非缓存输入*0.8 + 缓存读取*0.23 + 输出*2.8) / 1,000,000 USD
正常结算quota = round_half_away_from_zero(base * 500000 * 0.08)
```

输入缓存从prompt_tokens中拆出，不重复收费。这里的base是配置定价，不是已核实供应商成本；表达式模式不再叠加日志中为0的旧model_ratio/completion_ratio。没有在显示时再次乘0.08。

443条按冻结表达式及记录Token逐笔复算，仅log_id **162–167**有差异：应收合计7 quota、实际均0，元数据明确settlement_failed，预留needs_review/target合计7/applied=0。其余437条公式与扣费一致。该7 quota（$0.000014）是已记录的结算失败待处理，不能直接补扣，也不能称全部正常结算成功。未知用量42条不具备按真实Token复算条件。

### 重试、失败、取消和日志保留

- 485条消费request_id全部唯一，逐笔quota=reservation.applied，未发现同一请求多条消费；本日outbox相关485条（含退款）全部delivered，全库596条outbox也全部delivered，没有待投递积压解释当前差异。
- 443条带普通元数据的请求策略均最多attempt=1；42条缺此完整元数据，不能据此证明上游没有重试，亦不能把485消费日志等同于所有上游HTTP尝试次数。
- 取消/发送后用量未知会保留预扣并进入核对，未发送/确定未做功可退预扣；6笔零扣费结算失败与6笔预扣释放各有不同request_id，不是重复计费证据。
- 消费日志可关闭，小时聚合可关闭；缓存刷新存在时延，且非账务事务。源码的保存失败处理也不能保证可靠重建，但当前10组精确匹配，**没有证据把本日42条遗漏归因于缓存丢失**。
- 仓库提供`DeleteOldLogBatch`/system_task的显式日志清理入口；ClickHouse可有TTL，本现场为PG，没有该ClickHouse TTL因素。未全面读取清理任务历史，不能保证从未删过历史日志。当前消费/账本/outbox投影一致，无本日缺失消费的证据；未执行清理或更改保留策略。

## 8. 56条待核对的准确含义

经营概览使用如下条件计数，**没有日期过滤**：

```sql
SELECT COUNT(*) FROM billing_reservations
WHERE state IN ('needs_review','settle_pending','refund_pending')
   OR (state='settled' AND id IN
       (SELECT reservation_id FROM billing_review_markers));
```

| 类别 | 本日条数 | 本日applied | 历史条数 | 历史applied |
|---|---:|---:|---:|---:|
| settled + upstream_usage_unknown | 42 | 407,035 | 1 | 311 |
| settled + interrupted_unknown_usage | 1 | 16,472 | 4 | 2,694 |
| needs_review | 6 | 0 | 2 | 45 |
| 合计 | 49 | 423,507 | 7 | 3,050 |

全部56条applied共426,557 quota（$0.853114）；本日49条为$0.847014。settle_pending/refund_pending本次均0。历史日期按updated_at/标记区分，不把它们算入本日消费。

本日42条未知记录解释全部443↔485条数差；本日1条估算已在两个页面金额中，6条needs_review均为零实际扣费也已在443中。待核对数量本身不会触发额外扣费或额外聚合，不能将56当作56条本日新增收费。

## 9. 是否存在真实账务缺陷：按A–E判断

| 类别 | 判断 |
|---|---|
| A 成本与售价差异 | 不能作为解释：两页均为用户扣费quota；供应商成本未取得 |
| B 正确但口径不同 | 部分成立：小时聚合/原始日志、滚动区间/北京时间日、全历史核对数量确实不同；但当前差额同时包含程序缺陷 |
| C 待核对记录 | 已证实：本日42条$0.814070漏入模型聚合；另1条$0.032944估算两边都有 |
| D 重试/退款/失败重复统计 | 未发现同request_id重复扣费；已证实经营净消费重复扣减6笔预扣释放$0.058900 |
| E 查询/统计/账务缺陷 | 模型聚合覆盖不全、净消费公式语义错误；另6笔结算失败7 quota待核对。未发现485笔已记消费与实际applied不一致 |

另一个直接相关的经营计数问题：用户24的token.used_quota与reservation.applied累计均272,894，但users.used_quota为218,867，少54,027；用户25分别422,437与69,429，少353,008；差额合计恰为42条未知用量的407,035。说明普通用户累计消费计数也未覆盖该异常结算路径。**这是统计计数遗漏，不是钱包少扣或多扣的证明。** 用户1/6的对应累计数相等；用户2/3当前用户/Token记录不存在，不扩大为历史账户专项审计。

结论：存在真实的经营统计程序缺陷与已记录待核对收费；没有证据要求修改钱包或消费原始账本。尚缺上游用量、供应商账单、原截图响应及日终快照，不能签署“全部账务无误/完整日终核验通过”。

## 10. 最小修复建议（未实施，等待负责人决定）

1. 经营概览保留“预扣释放”作为独立指标；已结算消费净额仅扣除有明确原始消费关联的消费后退款，不能无差别减所有type=6。修复覆盖本例6笔无消费预扣释放，避免仅改文案而保留错误净值。
2. 模型分析优先复用消费日志聚合的用户扣费事实，明确展示“普通用量记录/未知用量保留/估算用量”及缺失Token。若继续保留quota_data，应在统一结算完成路径设计幂等投影并从durable事实重建；不能简单在outbox投递再调用LogQuotaData，否则普通路径会重复。不要将当前42条存储0冒称真实Token0。
3. 名称建议：“记录消费额度（含待核对保留）”“已确认用量对应额度”“预扣释放”“待核对保留金额”；模型分析金额不称“上游成本”。保留正确的不同指标，而不是强制显示同一个数字。
4. 两页提供一致的北京时间半开区间、read_at/数据更新时间与精确quota明细，小时聚合说明刷新周期；保留待核对“全部日期”提示，另提供本日分项。原始金额用于计算，显示舍入不反向参与计算。
5. 6笔target>reserved的零扣费异常及49笔本日核对记录留给负责人逐笔核定；不自动补扣、不凭估算退款。users.used_quota遗漏应复用统一结算的幂等计数方案，不在多处追加增量。

未来获准修改后的最小验证：预扣释放无消费、消费后真正退款、普通路径与未知路径各只聚合一次、取消估算标识、零额度结算失败、北京时间端点。只有变更账务持久化/投影时按仓库规则执行真实三库验证。本轮没有改代码或数据库，因此未运行历史单测、全量回归、数据库迁移矩阵或收费调用。

## 11. 本轮验证与停止条件

实际执行：生产结构/身份只读核验；一致性事务投影；本机485条唯一request_id及quota=applied断言；443条普通日志与10个小时组的精确相等断言；6笔预扣释放的全库消费关联检查；56条状态/金额拆分；443条冻结售价公式复算；生产revision相关源码定向diff；证据摘要与git diff --check。

已验证结果均如上，不重复历史测试。必要但未完成的事实：完整11月1日日终快照、原440次画面的原始响应/筛选/时间、42条真实上游用量及供应商成本、零点钱包和完整期间资金流水。以上不伪报通过；其中原始画面剩余$0.033530和Token差额仍不可逐笔归因。

本轮仅生成报告/脱敏证据，并向唯一权威`D:/AI/API/worktrees/clearapi-controlled-launch-20261020/CLEARAPI_MASTER_STATUS.md`追加记录、验证历史字节前缀保留。不修改当前工作区的旧总控副本；不修改源码、钱包、消费记录或生产库；不退款、不部署、不删日志、不提交/推送。完成后停止，等待负责人决定是否修复。
