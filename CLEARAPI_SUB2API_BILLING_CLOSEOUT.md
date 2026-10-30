# CLEARAPI — Sub2API 未知 Usage 与预留额结算收尾

2026-10-30，Asia/Shanghai。**结论：现有持久账本及人工核查路径可以安全保留、退还并终结有证据的异常请求；本轮实际验证了并发重复核查、重启恢复及净额一致性。未发现必须修改核心Billing的缺陷。未知Usage没有自动变成零消耗，晚到Usage高于保留额仍不能自动追补，全部自动精确结算未通过。可进入默认关闭的统一发布候选，不可据此启用生产池。**

## 1. 基线和范围

- 工作区 `D:/AI/API/worktrees/clearapi-sub2api-integration-20261030`，分支 `codex/sub2api-isolated-integration-20261030`，开始干净，HEAD `6867f8a2e`，保留 `216f2e67e`、此前实现和报告。唯一总控继续为 controlled-launch 工作区的指定主文件。
- 复用固定Sub2API d851a384完整前端镜像、原PostgreSQL/Redis/卷、既有Gateway二进制及Linux SQLite卷；没有重新构建基础镜像、安装平台、整理13家清单或重复16例协议验收。423正式确认仍有效。
- 本轮仅启动原有两个验收容器，使用合成余额和mock；未访问生产、真实厂商或真实客户钱包。所有金额均为隔离测试quota units，不是现金退款。
- 已全文阅读 `.agents/rules/billing.md`。没有修改Go、鉴权、定价表达式实现、数据库模型/迁移代码、前端或Sub2API源码；没有新跑历史三库矩阵、全量CI、前端构建、收费调用。

## 2. 源码结论：UNKNOWN本身不是缺陷

核对 `service/billing_session.go`、`relay/request_billing.go`、`service/text_quota.go`、`model/billing_reservation.go`、`model/billing_completion_outbox.go` 和 `model/billing_reconciliation.go`：

1. 钱包和Token预扣与reservation同事务提交，请求ID唯一。发送前可证明未派发的失败走持久退款；已派发或不能证明零执行的错误走保留与review marker，不按客户端断开、HTTP错误文本或记录年龄推断零费用。
2. 完成意图的target一旦保存不可改写；`settle_pending`/`refund_pending`才自动恢复，`active`/`needs_review`不按超时自动退款。未覆盖余额的已知target保留待核查，不静默免单。
3. 最终消费日志由outbox投递，日志库receipt按request_id唯一。重复恢复不会再扣款或插入第二笔消费。日志投递失败不丢失主库完成意图。
4. 流中断无可信最终Usage会标记`interrupted_unknown_usage`。本地估算可影响保留额，但日志明确标为unknown/retained-for-review，不能把这些token和金额当作已确认的厂商消耗。保留不是精确终算。
5. **已有人工核查并非缺失：** `GET /api/owner/finance/billing-reservations?scope=pending`，`POST .../:id/decision`；原UI在`/finance`的“Billing reservation review”。核查要求指定Finance Owner、执行已结束确认、原state/applied/target快照一致，且当前请求不在执行。每个reservation仅一个最终decision，重复同内容返回同ID，改变内容返回409。决定、钱包/Token退款和审计同事务提交。
6. 核查退款是追加调整，**不重写原消费日志**。实际净额应按历史总扣款减decision退款核对；已有入账收入还需原Finance流程处理，不能把毛消费日志直接当净收入。

必要的现有部署条件是 `CLEARAPI_DURABLE_WALLET=true`、`CLEARAPI_BILLING_OUTBOX=true`、`CLEARAPI_FINANCE_ENABLED=true`、指定Owner ID，以及`CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE=true`；Gateway不得使用Redis/批量余额更新，同一账本只能有这一个执行进程。Sub2API使用自己的Redis与此不冲突。这个开关是进程内执行注册表的既有边界，不是分布式租约；多副本共享库不能直接照搬。

上轮验收Gateway未启用Finance Owner和核查开关，本轮在**仅供验收的overlay**启用。首次新请求暴露隔离库缺少`finance_sync_events`，使已知890完成意图停在settle_pending：保存备份且integrity_check=ok后，停测试Gateway，使用既有 `finance-migrate --dialect sqlite --apply-approved-finance-v7` 补齐该依赖，再启动。原恢复任务将该意图完成并仅写一条890日志。没有改库伪造状态或补余额。这是验收配置/迁移依赖遗漏，不是UNKNOWN状态机代码错误。

本轮只验证Owner核查API。完整Finance UI按既有设计要求schema v9，本隔离库未为UI追加其余无关迁移，也未做页面回归；不能把API通过写成UI通过。该要求未被删除或绕过。

## 3. 八类情形与证据

复用原 `evidence/sub2api-final-acceptance-20261030/mock-e2e.json`；新增证据统一在 `evidence/sub2api-billing-closeout-20261030/`。

| 情形 | 实际资金状态与处理 | 验收状态 |
|---|---|---|
| 发送前连接失败 | 原用例mock尝试0，reservation=refunded，净扣0，无消费日志 | 已完整退款，复用原证据 |
| 已发送后客户端主动取消 | 原用例收到delta后真实关闭socket，保留8并标review；Sub2API随后drain完整Usage | 仍待核对，不假设取消成功或零费用 |
| 上游完成、客户端连接已断开 | 同一原取消用例：Sub2API最终记录input800+cache200/output20，而Gateway没有可信终态 | 证明服务端完成与客户端断开可并存；未自动补收至890 |
| 上游已执行、最终响应丢失 | 新`mock-completed-drop`先保存执行完成证据，再断连接；Gateway502，保留4并标unknown。另一个small执行记录只有2个uncached input token，响应同样丢失 | full用例保持待核查；small按证据完成净2结算，见下节 |
| SSE中途断开，无最终Usage | 原missing-terminal保留8并有marker | 待核查；EOF不被当作已完成 |
| 超时、最终状态未知 | 新timeout在约5.115秒触发Gateway流超时，保留10并标interrupted；释放mock后Sub2API记录800+cache200/20 | 保留可追踪，不自动当零，不将晚到内部Usage自动用于补扣 |
| 重复核查/恢复 | 两笔决定分别并发提交2次、再提交1次，始终同decision ID；两次Gateway重启及周期恢复后钱包、Token、预留、receipt、日志、审计均不变 | 没有重复释放或再次扣款 |
| 内部重试后Gateway唯一终算 | 原503明确拒绝执行→成功，2次上游尝试，1个reservation/1条890最终日志 | 复用原通过证据；不保证物理上游调用恰好一次 |

新增timeout的首次fixture仍走Chat转换且没有等待，形成一笔正常890请求；随后才修正mock的Chat等待分支。该笔和缺表导致的pending/recovery证据都原样保留，不冒充timeout首次即通过。真正timeout用例的观察器又先遇到SQLite busy导致的outbox退避，20秒观察期不足；投递后来由原恢复任务完成。保留这次观察快照，按原request_id继续观察，**没有重新派发该业务请求**。driver随后保存响应后再观察，支持按已记录用例续跑，不删除失败试次。

## 4. 两笔实际人工核查与幂等终态

通过正常隔离Owner API执行，而非直接改SQL：

- 原400执行前拒绝：fixture代码和原请求证据明确零执行。reservation 10原保留12，核查退12，最终`reviewed`、final_quota=0。这是有证据退款，不是将所有400视为零。
- 新small响应丢失：可控provider执行记录是input=2、output=0、cache=0；既有合成价格表达式`p*2 + cr*0.5 + c*4`、QuotaPerUnit=500000，对应准确2 units。reservation 23原保留4，退2，最终`reviewed`、final_quota=2。已执行被明确计入，没有因502全额退款。

每笔2次并发加1次重复提交只产生一个decision/一条`billing.decide`审计；合计只退14。错误expected_applied和超额退款返回409，缺execution_ended确认返回400，已完成决定更改理由返回409。两笔毛日志、outbox和receipt都保持原样；新增退款事件携带decision身份。

两轮真实Gateway重启，每轮启动恢复后再等待至少一个10秒周期恢复，全部财务身份及数值均与核查后快照一致。证明已核查终态不会再被恢复任务覆盖，未知记录也不会仅因重启或老化而释放。不是新增Usage回调接口；本版本没有可凭任意客户端Usage请求安全改账的公开回调，本轮也未伪造这种接口。

最终合成全账本（包括历史试次及本轮一笔前期正常请求）：

| 项目 | 数值 |
|---|---:|
| reservations | 24 |
| 唯一消费日志 | 23 |
| 毛消费日志quota | 9866 |
| 核查decision退款 | 14 |
| 钱包净减少 / Token used / reservation applied合计 | **9852** |
| 退款事件与decision | 各2 |
| 全部outbox | delivered |

因此`9852 = 9866 - 14`，不是把原毛日志改成净额“凑平”。`user.used_quota=9828`仍是另一累计观察口径，不能替代钱包净额；原错误保留及核查退款路径不保证该计数等于财务净支出，未改该历史计数。

本轮还在Finance核查已启用的配置下关闭所有type59池渠道，只启用原type1普通mock渠道，实际200、钱包/Token各890、唯一日志；核查决定不变。此项因共享资金配置改变而定向补验，没有重跑整套协议。

## 5. 尚需人工核对及真实边界

最终**9条**pending（reservation ID 22、21、17、15、14、13、12、11、1），包括full响应丢失、timeout、原取消/缺终态/异常、旧fixture试次及此前SQLite I/O造成的needs_review。10个marker保留，其中2条已由decision终结；marker是历史证据，不应删除或按marker总数认定全部未完成。

这些记录没有遗失或不可达的预留：Owner API可查询、查看原target/applied与reason，取得可靠执行/Usage证据后决定。没有证据时仍保留pending，本轮未替负责人对未知真实消耗作收费/免单决定，也未为了清空列表强制退款。

现有人工路径的能力是`0 <= refund <= applied`：可保留或退回原金额，并写reviewed终态；**不能把已settled保留额追补到更高晚到Usage金额**。例如取消8、超时10对应内部890，不能宣称已经完成890精算；本轮没有创建额外扣款、虚构tokens或实施固定异常费。若业务将来允许仅保留原额并核销差额，应由Owner在证据/理由中明确记录该业务处置，它与“精确按Usage结算”是两种状态；本轮没有用这种处置冒充通过。

Sub2API内部Usage不是可以无条件套用的客户账单：两服务request_id目前没有已验证的唯一可信跨服务关联，取消可能丢终态，重试可能有多个执行，价格/缓存归一化也必须使用原请求的定价事实。此次small依据是在隔离环境里唯一模型、串行可控执行的原始记录，**不是通用自动对账服务或真实厂商签名收据**。未建立第二套Billing或把管理接口暴露给普通客户。

测试范围内未发现重复最终扣款或重复释放；这不是任意故障、多副本或物理上游恰好一次的保证。证据不足的pending可管理但尚未精算，不能将它们统一标PASS。原source中正常target可结算、outbox幂等和人工减免机制够用，因此不为UNKNOWN状态改核心代码；自动关联、超保留额的受控追补是未实施边界，而非本轮已完成能力。

## 6. 最小操作路径与未来真实验证

Owner核查时先通过现有Finance capability确认ready，读取pending；确认HTTP执行结束及Sub2API记录/原请求价格、尝试次数、Usage证据，核对原state/applied/target，提交以下已有契约：

```json
{
  "expected_state": "从当前记录读取",
  "expected_applied": 4,
  "expected_target": 4,
  "refund_quota": 2,
  "reason": "保存证据位置、执行身份、实际Usage及计算依据",
  "execution_ended": true
}
```

数字只是本轮small示例，不可照抄到其他请求。0退款表示保留原额；一次最终决定不可更改。409应刷新记录，不能反复改字段绕过；不确认执行结束或缺证据时保持待核查。已确认金额超出applied不能用此API冒充追补。必要的后续财务收入调整沿用原Finance流程，毛消费日志与核查退款要一起展示/核对。

未来只选择确有商业需求、合法且不与PG03共用的一个代表厂商/账号类型，例如允许商业调用的官方API Key；无需13家全部验真。最小授权建议：明确厂商、模型、账号、协议和费用范围；最多3个业务请求（1次JSON、1次完整SSE、可选1次受控取消），默认Gateway/Sub2API均不重试。如确需重试，另明确至多1次额外派发并计入累计上限。可把**每请求≤USD0.02、整批≤USD0.10**作为待负责人批准的示例硬上限，负责人可另定；它们不是现有授权或模型报价。

执行前按获批模型实际价格限制input/output，若无法在发出前保证预算则不调用。每次保存真实响应、Usage与request身份，核对钱包/Token/reservation、毛消费日志和核查调整、Sub2API内部Usage；取消不保证免费。只有选择不同认证或协议时才追加必要差异验证。没有明确凭据、商业授权和限额，本轮真实调用0，未发送收费请求。

## 7. 交付、收尾与发布判断

实际代码提交 **`a15623442`**，仅修改隔离Compose开关、扩充有限mock场景及新增分阶段closeout driver；没有核心缺陷修复提交。运行了新双服务3例、两笔正常API核查的并发/重复/失败分支、两次重启恢复、Finance启用后的普通渠道定向回归、Python AST、diff检查和最终账本断言，均达到各自明确状态。源码未变的16例及历史单测/三库证据复用，不冒充本轮重跑。

新增资产：`scripts/sub2api-billing-closeout.py`、原mock/Compose扩充、`evidence/sub2api-billing-closeout-20261030/`。driver通过仓库外私有runtime读取测试身份，使用正常API、只读SQL快照，phase为exercise/review/snapshot/ordinary/close；恢复验证需要在snapshot前重启原测试Gateway。它是该既有合成fixture的续验工具，不能对任意数据库或真实账号执行。迁移备份位于原runtime的`gateway.before-billing-closeout.db`，包含测试secret，不入Git。

收尾：13内部Key全部inactive，2模拟账号unschedulable，5测试渠道disabled，仅Gateway/mock已stop，保留卷/备份/证据；原Sub2API、PG、Redis继续healthy、管理前端可用。没有推送、合并、生产部署或生产池开关操作。唯一权威master只追加本轮事实，原历史及其他dirty成果完整保留。

**隔离账务验收：** 已达到“持久保留可追踪 + 有证据人工终结 + 幂等退款/日志/恢复 + 普通渠道隔离”的人工核查边界；**未达到“所有未知请求自动精确计费并清空pending”**。可纳入下一批默认关闭的统一发布候选；本报告不批准生产启池，也不证明多副本核查安全。正式启用前仍需选定合法真实凭据/获批限额，并明确单实例人工核查责任与超保留额处置；若要求全自动精算，应另补可信关联及原定价重放后再验收。负责人无需重复接受条款或购买全部厂商账号。
