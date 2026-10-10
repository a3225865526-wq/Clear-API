# CLEARAPI MULTI-PRODUCT SHOP + REFERRAL REWARDS DEVELOPMENT RESULT

日期：2026-10-10。开发与本地定向验收已完成；发布流程停在关联 Issue 和负责人正文审核门禁。未生产部署，未开启真实奖励。

独立工作区：`D:/AI/worktrees/clearapi-shop-referral-rewards`。
分支：`codex/shop-referral-rewards-20261010`。
创建时通过 GitHub 核实最新 main：`f57a392ae860361dae32f38331c0ba3530ad74e4`。原开发分支与其他修改保留。

## 逐项结果

| 项目 | 结果 |
| --- | --- |
| 1. 八档商品 | ¥1/5/10/20/50/100/500/1000 对应计划 $1/5/10/20/50/100/500/1000 API 额度，配置持久化。八档默认未核实、待上架。原云猫入口独立保留且明确交付未核实，没有宣称原 ¥1 商品交付 $1。 |
| 2. 商品后台 | 超级管理员 `/system-settings/site/credit-products`，支持增改、人民币售价、美元额度、说明、排序、购买链接、交付核验和上下架。上架须服务器校验核验标识及安全链接。 |
| 3. 客服提示 | 商城顶部“大额度充值请联系客服，免手续费！”，QQ 3225865526 可复制，附需先确认购买及手续费安排说明。 |
| 4. 邀请页面 | `/wallet#referrals`：链接、邀请码、人数、规则、累计奖励、分页记录。格式 `https://www.clear999.com/sign-up?aff=<code>`。 |
| 5. 机制复用 | 复用 `AffCode`、`InviterId`，保留旧码；新码唯一索引防冲突，空码条件更新分配。未另建邀请关系表。 |
| 6. 注册绑定 | 原邮箱验证码服务器校验与一次性证明保留；注册成功才插入合法邀请人关系。客户端任意 inviter_id 不可信；普通更新不能覆盖既有关系、邀请码或累计奖励。人数由已持久化关系计数。 |
| 7. 默认规则 | 10%（1000 basis points），默认奖励关闭。后台 `/system-settings/site/referral-rewards`。 |
| 8. 固定模式 | 比例与固定额以单一 mode 互斥，支持管理员编辑，不叠加。配置快照固定已排队奖励金额，未来设置变更不重算旧奖励。 |
| 9. 付费资格 | 只有超级管理员显式将未使用 Legacy/Admin 余额卡密标记 paid 才符合资格，可批量不超过 100 个 ID，原因及操作者进入审计。其他来源/类型不纳入。普通创建及编辑不能写入资格。 |
| 10. 计算舍入 | `floor(实际到账Quota × basis_points / 10000)`，大整数乘法防溢出；固定模式取固定整数额度。超过每笔上限不产生奖励，不静默截断。 |
| 11. 幂等 | redemption_id 唯一待发奖记录；reward_id 唯一流水；锁与条件更新防重复并发。流水、余额、累计计数和 paid 状态在同一事务。 |
| 12. 事务一致性 | 兑换事务记录 pending，独立事务发奖；发奖失败保留事实及安全错误类别，后台每 30 秒重试，启动即恢复。主数据库流水不依赖独立日志库事务。买家原充值额度不变。 |
| 13. 排除范围 | 历史空标记、free/test/compensation、历史已兑换、注册、签到、管理员调整、返利本身均不触发；不按名称或金额推断。 |
| 14. 开关上限 | 邀请关系及奖励独立开关；默认每笔 $100、每邀请人累计 $1000，可调整。余额上限复用原保护，累计上限条件更新原子校验。紧急关闭锁同一配置门闩并暂停待发记录、递增 epoch；重新开启不自动恢复，root 逐笔提供理由审计恢复。待发奖励同时遵循快照与当前更严格上限。 |
| 15. 原路径 | 未修改 Billing 计算、Provider、账号池、Route Chain、CLIProxyAPI、Executor。兑换增加持久化待发奖步骤，发奖不调用 Provider；原邮箱验证、旧登录和 Legacy 兑换定向回归通过；原邀请注册赠额移除，符合“注册不奖励”。Payment/Store 原开关未修改。 |
| 16. 本地验收 | SQLite 3.50.4、PostgreSQL 17.11、MySQL 8.4.11 真实例返利测试通过，含新库、代表性旧表、两次迁移、数据及唯一约束、故障、并发、恢复。29 项前端定向测试通过，注册/权限/Billing 定向回归及构建通过。详见下表。 |
| 17. PR/CI/RC | 未创建 PR，远程 CI 未运行，RC 未构建。负责人 PR 正文审核和关联 Issue 仍待完成，不能代签人工确认。 |
| 18. 合并及镜像 | 尚未合并，无合并 commit，无 RC OCI revision 或 GHCR exact digest；不以本地构建冒充 RC。 |
| 19. 生产申请 | 尚不具备申请部署条件，先完成 Issue、PR、保护 CI/合并、RC 和镜像身份核验。此次生产部署未授权。 |
| 20. 奖励批准 | 仍需运营方单独批准；本次未操作生产设置。代码默认关闭不等于已读取生产实时状态。 |

## 实际验证

使用既有缓存 Go 1.26.8、Bun 1.4.0 运行环境；未安装或升级依赖、未改锁文件。数据库均为本任务专用隔离实例，非生产数据。

| 命令或步骤 | 观察结果 |
| --- | --- |
| `go test ./model ./common -count=1` | 通过；包含原兑换行为及共享额度保护。 |
| `go test -race ./model -run TestReferral -count=1` | 通过，无数据竞争报告。SQLite 限单连接与 CAS；真实 MySQL/PostgreSQL 另行覆盖并发。 |
| SQLite `go test ./model -run TestReferral -count=1` | 通过；额外 `-run '^TestReferralRulesAndQualification$' -v` 确认 SQLite 3.50.4 及默认比例例子。 |
| `REFERRAL_TEST_DIALECT=postgres REFERRAL_TEST_DSN=<专用测试数据库> go test ./model -run TestReferral -count=1` | 全部通过，38.984 秒；PostgreSQL 17.11。 |
| `REFERRAL_TEST_DIALECT=mysql REFERRAL_TEST_DSN=<专用测试数据库> go test ./model -run TestReferral -count=1` | 最终全部通过，34.988 秒；MySQL 8.4.11。 |
| `go test ./controller -run 'TestVerifiedRegistration\|TestReferralManagement\|Test.*Billing\|Test.*Redemption\|TestOnlinePayments' -count=1` | 通过；有效/无效/重复验证码邀请绑定、身份隔离、普通管理员管理权限拒绝及旧路径保护。 |
| `go test ./service -run 'Billing\|Quota\|Reserve\|Reservation\|Settle\|Refund' -count=1` | 通过，3.918 秒，Billing/额度/退款定向回归。 |
| `go build ./...` | 通过。 |
| `vitest run`：shop/catalog、auth/registration、referrals/referrals、wallet/credit-purchase、wallet/disable-online-payments | 合计五文件 29 项通过；最终分别 24 项、5 项通过。本地 Bun 对外置 Zod 兼容有问题，使用临时配置将 Zod 内联；仓库测试配置未修改。 |
| `oxlint -c .oxlintrc.json`：商城、返利、注册及本次钱包/设置文件 | 18 个文件，0 警告、0 错误。 |
| `oxfmt --check`：本次新增商城/返利源码 | 10 个文件格式通过；保护既有版权头，新前端文件沿用项目版权头。 |
| `tsgo -p /tmp/referral-types.json --noEmit` | 完整应用源码类型检查通过，仅排除已有账号池测试文件。 |
| `bun run typecheck` | 失败：main 既有 `src/features/upstream-pool/components/release-controls.test.tsx:3` 的 `fireEvent` 未使用 TS6133；未修改禁改模块。CI 本身已有注释明确排除该历史全量类型检查。 |
| `bun run build` | 最终构建通过，26.0 秒。 |
| `git diff --check` | 通过；仅 locale JSON 的 Git 换行提示。 |
| 浏览器深色手机/浅色桌面 | 无横向溢出；客服提示、复制按钮、原入口免责声明、八档待上架卡片显示正常。用本地隔离模拟 API 展示，未认证后台端到端或生产购买。 |

重要行为证据：测试故意令奖励流水插入失败，余额、累计额度与 paying 状态全部回滚，原买家余额保持不变；恢复后同一奖励只记一次流水。并发两个奖励竞争累计额度只允许一笔成功。关闭后排队记录不丢失、开启不自动发放，需显式审计恢复。迁移样本保留原邀请码、关系、余额和已兑事实，不补发历史兑换。

数据库复现环境变量仅接受专用容器及 `referral_rewards_test` 数据库，测试会重建其中的测试表。生产 DSN 被拒绝，禁止替换为生产地址。故障测试使用测试回调，不存在生产故障注入入口。

## 限制及发布门禁

未运行整个仓库全量测试或远程 CI；未验证支持版本下界 MySQL 5.7.8 / PostgreSQL 9.6、Redis 实例缓存行为、多副本服务端到端、真实邮件发送、认证后台浏览器端到端或云猫真实交付。三数据库结论限本表版本和代表性旧数据样本，不宣称生产升级已经成功。

模板 [`.github/PULL_REQUEST_TEMPLATE.md`](.github/PULL_REQUEST_TEMPLATE.md) 要求“人工确认”负责人“已审阅并精炼全文”及“新功能关联 Issue”。项目负责人走该人类模板是 `AGENTS.md` 的明确规定。已准备 [PR 正文](PR_DRAFT.md) 与 [Issue 正文](ISSUE_DRAFT.md)，尚未代签、发布或推送。设计批准无需重复，所欠为这两份对外发布正文审核和 Issue 创建授权。

认证参考：已读 [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html) 和 [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)。保留原验证码校验、单次证明、原登录方式；验证了错误证明与重放拒绝，没有作完整 ASVS 合规认证声明。

发布研究已打开 [官方 User Guide](https://docs.newapi.ai/en/docs/guide/home) 与 [上游兑换说明索引](https://deepwiki.com/QuantumNous/new-api/6.3-redemption-codes)，最终功能差异与财务结论以本地 main 源码及真实数据库测试为准。

DEVELOPMENT READY（本地实现与定向验收）；PUBLICATION BLOCKED（正文人工审核及关联 Issue）。

RC BLOCKED。

PRODUCTION DEPLOY NOT AUTHORIZED。

REFERRAL REWARDS PRODUCTION ENABLED = FALSE（本次未开启；代码默认关闭，未读取生产实时配置）。
