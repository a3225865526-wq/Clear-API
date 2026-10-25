# 公告展示控制与每日渠道历史联合修复候选（2026-10-25）

## 用户本地验收补记：隐藏调度说明卡

按截图移除渠道状态页的每日调度频率、暂停说明和上次/下次调度时间。仅删除前端展示与不再使用的日期格式化代码，不改变后台调度、历史查询或任何数据库字段。修改文件为 `web/src/features/channel-status/index.tsx` 及其 `__tests__/interaction.test.tsx`；回归 fixture 仍返回 schedule，断言页面不展示它且保留渠道/刷新/周期筛选。

验证：新用例先失败；`node node_modules/vitest/vitest.mjs run src/features/channel-status/__tests__/interaction.test.tsx` 6 项 PASS；`bun run typecheck` PASS；两文件 oxlint 0 error/0 warning；实际 Dockerfile 构建并更新原本地预览地址，浏览器确认说明卡已消失。没有后端/数据库改动，不重复三库或历史部署 E2E；未部署生产。此补记取代下方历史截图中的调度说明展示，后台功能及此前验证继续保留。新本地验证镜像为 `clearapi-notice-history:preview-no-schedule`（SOURCE_COMMIT 空，仅供隔离预览），回执见 `evidence/notice-history-20261025/schedule-hidden.json`。

![隐藏调度说明后的渠道状态](D:/Codex/worktrees/clearapi-notice-history-fix/clearapi-controlled-launch-20261020/evidence/notice-history-20261025/schedule-hidden.png)

---

本轮完成源码修复、隔离验证及本地候选准备；**未部署生产，未修改生产公告、已读记录、渠道配置或费用策略，真实供应商收费请求为 0**。PG03 继续暂缓，D 线生图未纳入。最终候选是包含本报告的本地分支 `codex/clearapi-notice-history-fix-20261025` 的提交；完整 SHA 在最终交付及外部 candidate-receipt.json 记录，避免在提交正文中伪造自引用 SHA。

## 实际基线与成果保护

- 原工作区 `D:/AI/API/worktrees/clearapi-controlled-launch-20261020` 为 a032815c，共 152 项修改/未跟踪项；没有覆盖、stash、reset、clean 或改变原 HEAD。
- 动态监测开发线 `D:/AI/worktrees/clearapi-dynamic-monitor` 为 72d26b26，干净；运行适配工作区 `D:/AI/runtime/clearapi-monitor-runtime-activation` 为 3124d8a1，5 项改动保留。已部署源码工作区为 879eff62，干净；功能运行提交为其祖先 1aef1aec。
- 本轮通过托管工具从 879eff62 创建独立工作区，沿用其中已集成的监测传输、费用上界和租约实现，没有用旧分支整文件覆盖。原工作区 HEAD、状态、逐文件 SHA256、tracked/index 补丁保存在 `D:/Codex/backups/clearapi-notice-history-20261025/`；未跟踪文件原地保留，manifest 记录其哈希，不宣称已逐个另存副本。
- 06:31:37 UTC 与 07:16:08 UTC 两次 READ ONLY 复核：生产应用均为 `1aef1aec0e6337992f382836aaeea62c07eb41fd`，engine digest `sha256:61614562f14aeb12bfd060e766510a1c727802fe92eea588afe6c652484b9c08`，Gateway `3c1e4ab2…`、StartedAt `2026-10-25T06:13:10.011644231Z` 不变。公告共 2 条，内容 hash `b9e0e8e3a4530bb45299ae47d62d75a8` 前后一致。
- 生产 task runner 启动日志确实存在；当前监测策略 disabled，预算为 0，唯一目标 disabled/archived/unpublished/not-allowlisted；console_monitor 任务、attempt、probe 均为 0，费用合同为 0 条。没有把通用 runner 启动当作每日监测已执行。历史备份回执仍仅作历史背景，本轮未触发备份或部署。

## 公告：根因、调用链与行为

原「通知」只调用 `/api/notice` 读取单条 Notice；「时间线」从 `/api/status.announcements` 读取公告列表。多条公告从未被接入通知区域，且没有逐条位置字段；这与此前缓存刷新故障是不同根因。原已读只保存在共用浏览器 Zustand/localStorage，没有用户 ID 隔离，也没有后端保存接口；计数 memo 依赖稳定函数而未订阅读键数组，也可能保留旧计数。

修复后的路径：

`控制台内容 → 公告 → 创建/编辑 display → PUT /api/option/ → options.console_setting.announcements → GetAnnouncementsFor → /api/status → useNotifications → NotificationPopover 两个标签页`

- `display=notice/timeline/both` 实际存入现有公告 JSON；新公告默认两处显示，旧公告无字段时保持原时间线展示。草稿/撤回不进入用户列表，旧内容、时间、ID、发布状态语义保留。两处显示复用同一条记录和 read_key。
- `/api/status.announcements` 返回时间线内容，`notification_announcements` 返回通知内容；仪表盘公告亦沿用时间线过滤。原单条 Notice 保留，不复制或覆盖已有公告。
- 保存继续失效既有共享 status 缓存，并失效读状态查询；通知使用同一个 status query 每 60 秒刷新，打开时立即重取，两个 header 复用同一弹层组件。
- 新增 `GET/POST /api/console/announcement-reads`，沿用 UserAuth 和写入来源防护。身份仅从服务端 principal 取得，客户端 user_id 无法改变所有者；仅允许当前已发布公告/Notice 的键。新表 `announcement_reads` 使用 `(user_id,key)` 联合主键、冲突不更新，读操作可重放且不串用户。
- read_key 绑定公告 ID 和内容版本；展示位置/发布状态不改变同一内容的已读状态。旧无 ID 公告采用与现有编辑器相同的稳定索引兼容，空 extra 规范化避免仅改位置导致重置已读。
- 「确认已读」POST 成功后才更新对应用户 query；失败保持未读。重新登录、刷新、重开客户端从后端恢复。未登录访问的本地已读不充当登录用户的持久化记录。

## 监测：补齐每日观察和历史

原来有每分钟调度入口、真实传输执行器、console_probes/console_monitor_attempts 和最近 60 条概览，也有 Root 最近 100 条查询；但策略暂停时调度不创建任务，admission 拒绝没有记录，页面只显示状态条，没有可读的完整历史分页。不能据此推断已有每日运行。

修复路径：

`main.RegisterScheduledSystemTasks → 已有 SystemTask runner/锁 → console_monitor（每日）→ RunDailyScheduled → RunWithResult/预算预留/原传输 → console_probes → 用户历史 API → 原渠道详情`

- 复用既有任务系统，每次任务结束 24 小时后再次到期；时间按 UTC epoch 持久化，页面和日期筛选使用 Asia/Shanghai。非固定午夜 cron；polling 可能带来正常调度延迟。最近 task 保留，重启继续按其 updated_at 推算，不补造停机期间的历史。
- 存储就绪时即注册每日观察；即使费用执行 policy 暂停，也对已发布、未归档目标记录真实 skipped 状态。未发布/已归档目标不自动公开、不自动启用。没有目标时只完成空调度，不生成虚构 probe。
- 复用原 probe 表，增加 attempt_id、task_id 关联；成功/失败、skipped、blocked、unknown 分开保存。预算/并发/最小间隔阻断不会申请付费 attempt，也不会发送请求。无费用依据明确 blocked；传输结果未确认记录安全原因并保留原并发租约，未弱化费用与计量保护。
- 通用 SystemTask 的 succeeded 仅指观察任务流程完成；result 中 normal/failed/blocked/skipped 明确计数，不能把 normal=0、skipped=1 写成监测成功。用户页读取的是 probe 结果而非任务成功标记。
- 新增受 UserAuth 保护的 `GET /api/console/channel-status/:monitorID/history?page=1&page_size=20&from=YYYY-MM-DD&to=YYYY-MM-DD`。只允许已公开且未归档的目标，按 checked_at DESC、id DESC 稳定排序；日期按 UTC+8 转换，to 包含当天，非法日期/分页拒绝。单页 1–100，旧记录不依赖概览的 60 条限制。
- 保留原 30–365 天可配置清理策略（当前默认 90 天）；未决 attempt 不因过期自动删除。历史不是永久无限保留；查询空白日期不会被伪装成成功。
- 在既有渠道卡片/详情中展示最新结果、阻断原因、执行中/未决状态、日期范围、分页表格和任务 ID；单独展示渠道业务启停状态，监测不会修改业务渠道状态。既有状态条继续只表示最近 60 条，完整记录通过详情分页读取。

## 数据库迁移与交付边界

公告位置只扩展现有 JSON，无公告内容搬迁或重写。新增用户已读表需要正常主库 startup AutoMigrate。监测两列通过已有 opt-in `MigrateConsoleMonitoringDB` 增加；新执行器在缺 task_id 列时保持未就绪。Dockerfile 同时交付 `/monitor-init`，避免只有源码有迁移工具。

未来发布时，按既定备份/发布机制执行正常主库迁移与获准的监测迁移。监测工具支持：

```text
CLEARAPI_MONITOR_DSN=<受保护DSN> /monitor-init --dialect postgres --database <数据库名> --migrate
# 应用时再使用同一数据库名，并明确附加：
--apply --confirm-database <数据库名> --approve-monitor-migration
```

这只是将来操作说明，本轮未对生产执行。两个新存储路径均使用主库，不使用独立日志库；日志库模型、SQL 和迁移未改，未对无关 ClickHouse 路径另开矩阵。

## 实际验证

| 验证 | 实际结果 |
|---|---|
| 前端先失败回归 | 展示位置 3 项先因控件不存在失败，其余原 6 项通过 |
| 前端定向联合 | Node 24 / Vitest：6 文件 31 项 PASS，覆盖位置保存、旧公告、刷新、后端读状态、切换用户、失败重试、历史分页/日期/错误及原渠道交互 |
| 类型、lint、格式 | `bun run typecheck` PASS；涉及 17 个 TS/TSX 文件 oxlint 0 error/0 warning，格式处理保留原版权头 |
| 七种语言 | 35 个本任务键全部齐全；仅补齐既有 Blocked 的翻译。执行过 `bun run i18n:sync`，收回其自动带入的既有 D 线等无关键及排序变动，保留原值与顺序；新增译文全部由脚本写入 |
| 三库联合 | SQLite 3.50.4 / MySQL 8.4.11 / PostgreSQL 17.6：新库重复迁移、已发布版本 probe schema 升级、归档目标/历史保留、公告保存过滤、A/B 真实鉴权隔离、幂等与失败、实际 HTTP 监测、任务租约、预算/暂停/传输不明、分页时区、关闭重连后恢复，PASS |
| 原执行器保护回归 | 费用、传输、租约、CLI 门禁及 scheduler 定向用例；新增 admission 记录使旧 fixture 的 first/count 断言失败，已改为按结果语义查询。末次仅 panic fixture 选错 normal 行失败，修正后四种未确认清理场景定向重跑 PASS，未删除回归覆盖 |
| Go | 根模块编译/实际 Dockerfile 构建 PASS；独立 `cd relaykit && GOWORK=off go build ./...` PASS；Go 1.25.1 定向测试，实际 Dockerfile Go 1.26.1 Alpine 构建 |
| 镜像内迁移 CLI | dry-run 后，在独立 migration-check.db 应用两次 PASS；不启动 provider 或 scheduler |
| 真实本地浏览器 | 实际候选后端和前端：登录、两标签筛选、同条公告改位置→保存→即时移出时间线、确认已读、真实容器重启、最终镜像历史页面与翻译，PASS |
| 实际隔离 scheduler | 正式 main 启动了一个真实 daily task，产生 normal=0/skipped=1；此前本地 HTTP 真实执行产生 normal 一条、预算阻断一条。重启后 3 条 probe、3 条 read、1 条 daily task 均保留且未重复调度 |
| 生产与外部 | 只有两次只读核实；未执行生产每日周期、真实供应商推理、付费结算、生产用户写入或正式发布，不能将隔离证据记为线上 PASS |

实际命令与原始输出见 `evidence/notice-history-20261025/logs/`。`backend-final.log` 保留上述 fixture 失败的原始结果，修正后的 `panic-regression-final.log` 是受影响用例最终通过证据；没有把失败日志改成成功。前端首次使用 Bun-only 镜像运行 Vitest 遇到 zod 初始化/浏览器动画 API 环境问题，改用 Node 24（沿用同一 Bun 安装依赖）并补充局部浏览器 API fixture，31 项无未处理异常通过。未运行无关历史 30 项部署 E2E，未 Push/合并 main，远端 CI 未运行。

关键生产文件：`setting/console_setting/announcements.go`、`validation.go`，`controller/misc.go`、`announcement_read.go`、`console_monitor.go`、`system_task_handlers.go`，`model/announcement_read.go`、`main.go`、`console_monitor.go`、`console_monitor_control.go`，`service/console_probe.go`、`router/api-router.go`、`Dockerfile`；前端公告 section、通知 hook/popover/header、channel-status 的 card/history/api/admin、七种语言。复用既有 Dialog、Select、StaticDataTable、DataTablePagination、ErrorState/LoadingState，未新增通用交互框架或重做后台。

## 发布状态与未验证项

- 本地分支候选含全部修复。完整 Dockerfile 构建生成 `clearapi-notice-history:validation`，实际加载并运行；它是**提交前的隔离验证镜像，SOURCE_COMMIT 留空，不冒充正式 RC 或生产镜像**。镜像和二进制哈希见 validation.json。正式发布时需从候选清洁提交按既定机制重建并通过发布门禁。
- 生产当前仍是 1aef1aec，公告两条完整保留；未写生产用户已读/监测记录，没有修改渠道或费用设置。
- 真实收费执行仍需可核实的 Channel 1 供应商计费/全部 token 与固定费用/硬限额依据，再配置绑定 endpoint/credential 的费用合同、授权预算及目标的显式发布/allowlist/启用。当前合同 0 条、目标归档、policy 关闭；本轮不代用户解除这些业务配置。
- 每日链路隔离启动已跑通，**生产真正跨日自动运行尚未验证**，须在未来迁移/发布/合法配置后观察实际周期；未造历史补档。PG03 不启用，D 线无修改。

## 实际截图（均为隔离环境）

![三个展示选项](D:/Codex/worktrees/clearapi-notice-history-fix/clearapi-controlled-launch-20261020/evidence/notice-history-20261025/admin-placement.png)

![通知只展示通知与共用公告](D:/Codex/worktrees/clearapi-notice-history-fix/clearapi-controlled-launch-20261020/evidence/notice-history-20261025/notice.png)

![时间线保留旧公告](D:/Codex/worktrees/clearapi-notice-history-fix/clearapi-controlled-launch-20261020/evidence/notice-history-20261025/timeline.png)

![保存位置后立即过滤](D:/Codex/worktrees/clearapi-notice-history-fix/clearapi-controlled-launch-20261020/evidence/notice-history-20261025/placement-after-save.png)

![最终镜像的真实历史](D:/Codex/worktrees/clearapi-notice-history-fix/clearapi-controlled-launch-20261020/evidence/notice-history-20261025/history-records-final.png)
