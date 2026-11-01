# ClearAPI 财务统计及公告布局修复最终报告

日期：2026-11-01（北京时间）。应用提交 14da5b6bf25270802a0acac03e05a09c931bb4a0，候选 release-v3-financial-fix。仅本地修复与新 VPS 离线准备；旧 VPS 未连接、未升级、未改配置/数据库，未执行生产迁移、DNS、启池或真实收费请求。

## 财务结果

模型调用分析 /api/data/ 与 /api/data/self 改为只读消费日志投影，读取实际日志时间后按小时/模型/Usage质量聚合，不再叠加 quota_data 或预留表。保留原统计字段，新增 usage_quality；用户名筛选通过主库用户ID关联，覆盖没有冗余用户名的outbox日志；普通用户仍强制自身ID。查询不写任何聚合或原始记录，重复刷新不会累计补写。

区分 confirmed（完整上游观测）、partial（部分上游观测）、estimated（本地估算）、unknown（缺失/不一致/无法解码）。分类仅描述观测可信度，不代表供应商发票核验。金额明确是用户消费而非真实上游成本；未知Token保存值为0不表示实际零用量。历史无质量元数据的日志保守归未知。消费查询只涵盖保留日志，已被清理或关闭日志后未记录的历史不能从小时表补造，主库与独立日志库也不宣称跨库一致快照。本轮未改变日志保留策略。

经营概览主卡和日趋势在原始type=6上识别现有写入路径的精确 `Durable reservation refund` 事件，计入release_quota，不计refund_quota。既有任务退款/负差额退款仍计真实退款，净消费=消费总额−真实退款；预扣释放独立展示。没有修改退款写入机制，也没有通过通用关键字猜测退款类型。

## 审计快照回放

以原报告485条CSV逐请求金额/时间/模型作为隔离fixture，观测类别按报告重建，未获取或伪造新的上游Usage。固定区间1793462400至1793548799：

| 指标 | 修复后 |
|---|---:|
| 消费记录 | 485 |
| 用户消费总额 | 695,331 quota / $1.390662 |
| 42条未知Usage | 407,035 quota / $0.814070，完整覆盖 |
| 估算Usage | 1条 |
| 部分观测Usage | 442条 |
| 完整上游观测 | 快照0条；单独fixture验证分类 |
| 6笔预扣释放 | 29,450 quota / $0.058900，真实退款为0 |
| 快照净消费 | 695,331 quota / $1.390662 |
| 加入100 quota真实任务退款后 | refund=100，net=695,231；日趋势一致 |

重复读取完全相同，普通用户隔离和用户名/ID过滤一致。比较读取前后日志完整字段、用户Quota/UsedQuota不变。原始消费、原始退款、预留、钱包、实际结算、自动退款和历史账务没有改动。56条待核对（本日49、历史7）仍是原审计时点事实，不当作56条当日新增消费；不修改待核对状态、不补扣或退款。$0.033530历史截图差异继续未解决，不扩大追溯。

## 公告修复

复用现有NotificationPopover、Tabs、RichContent、Button、Alert，没有新增通知系统或后端API。原公告正文与通知列表各52vh形成双滚动区；现改为当前标签页的单一overflow-y-auto、min-h-0区域，外层受Base UI available-height、100dvh和42rem共同约束；标题、分类与底部操作shrink-0。长单词可断行，按钮可换行。保留原onMarkRead和读取状态hook，不在打开时自动已读，发布/删除/归档/权限不变。

## 实际验证

- Docker Go1.26.1-alpine，GOWORK=off、CGO_ENABLED=0、GOPROXY=off：`go test ./model ./controller -run ^TestFinancialAnalyticsSnapshot$ -count=1`，model PASS；controller编译通过且无匹配测试，不能计控制器测试通过。SQLite隔离测试，不连接生产。
- `node node_modules/vitest/vitest.mjs run src/hooks/__tests__/notifications.test.tsx`：7/7 PASS。新增1/2/3/12条长通知回归在修复前4失败/原3通过，修复后全部通过；现有服务端已读持久化/换用户/保存失败重试覆盖保留。
- Playwright Chromium：1440×900、1024×600、390×844、390×568、568×320；每种1/2/3/12条，公告/时间线40组实际几何与elementFromPoint命中检查通过，列表可滚动、按钮始终可点，无页面横向溢出，20次回调通过。隔离UI fixture没有业务接口；真实服务端状态由上述hook测试验证。不是Safari/Firefox实测。
- `bun run i18n:sync`、`bun run typecheck`、涉及6个TS/TSX文件的oxlint、oxfmt、`bun run build`通过（主机通过npx bun运行）。新增6个文案键七语言齐全；没有处理历史无关未翻译条目。
- 同一提交源码归档+本次dist，在Go1.26.1-alpine/greenteagc离线编译Linux/amd64 Gateway通过。新镜像network=none、read-only执行`/new-api --version`返回release-v3-financial-fix；这只是新二进制启动检查，不冒充业务部署验收。

证据：evidence/financial-fix-20261101（日志空白作规范化；原始输出留runtime）。未重跑完整历史矩阵、完整三数据库测试、Sub2API完整验收、六服务部署或历史镜像测试。没有schema/持久化/核心Billing变更；本轮按用户限定只做SQLite定向读查询测试，MySQL/PostgreSQL/独立ClickHouse日志现场执行未验证，不宣称已完成三数据库验证。

## 关键文件及剩余边界

controller/usedata.go；model/usage_analytics.go；model/owner_overview.go；model/usage_analytics_test.go；web/src/components/notification-popover.tsx；dashboard模型卡片/类型；owner-finance概览/类型；notifications.test.tsx；七语言locale。共享组件已覆盖需求，不存在需另建通用组件的能力缺口。

本地修复验证完成；新 VPS 现场安装、真实生产跨版本恢复/迁移、TLS/Access、资金核对及切换仍待现场授权和验收。PG03商用延期、D图像生成不准部署等既有边界保持。
