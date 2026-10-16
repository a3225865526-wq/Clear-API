# ClearAPI 项目总控

> 项目状态统一入口，由总协调负责人维护。证据截止后发生的变化须重新核实。
> 本文件不授权合并、安装工具、修改生产数据或部署；近期任务仅为协调安排。

## 1. 项目总览

| 项目 | 当前记录 |
| --- | --- |
| 仓库 | [a3225865526-wq/Clear-API](https://github.com/a3225865526-wq/Clear-API) |
| 目标 | 尽快完成小型可商用 AI API 中转站，优先最小商业闭环 |
| 技术基础 | 基于 GitHub 开源 New API 二次开发，结合 CLIProxyAPI Executor 与账号池 |
| 首版范围 | 尽可能支持 OpenAI、Claude、Gemini；后续再扩展 |
| 当前阶段 | IN PROGRESS：A 已达到 READY_FOR_REVIEW，进入 PG03 Release → RC → Production 后续链路；商业闭环待最终验收 [E9] |
| 是否达到商业 MVP | UNVERIFIED：尚无当前版本完整商业闭环放行证据，不标记完成 |
| 当前生产状态 | 用户里程碑确认：Gateway NOT UPDATED、Executor HEALTHY、Channel 2 DISABLED；本轮未独立在线复核 [E9] |
| 历史有限开放 | DONE：2026-10-09 曾完成单渠道、单模型有限开放；范围不等同当前三 Provider MVP [E6] |
| 上次核实主线 | `fdcbc35c62f657ae1cb42adbd88fde2572d4f809`；GitHub main，PR #40 合并提交 [E1]；本次未重查 main |
| 最后更新 / 核实 | 2026-10-16 用户里程碑更新 [E9]；独立 GitHub/报告核查仍为当日 19:44（Asia/Shanghai） |
| 初始化位置 | 独立主线工作树；本文件未提交、未推送，尚未进入远程 main |

### 状态与证据口径

| 状态 | 使用条件 |
| --- | --- |
| DONE | 有明确证据完成所注明范围；必须区分代码、测试、合并、部署和商业验收 |
| IN PROGRESS | 有证据表明工作正在进行，或已实现但该阶段尚未收尾 |
| BLOCKED | 有明确问题或未满足的放行条件阻止继续 |
| UNVERIFIED | 缺少当前证据；注明上次已知状态与来源，不推定失败或成功 |

READY_FOR_REVIEW 为 A 的审阅里程碑，表示已提交审阅条件，不代表已合并、部署或商业放行。

GitHub 查询用于核实 PR/CI/主线；生产事实引用注明日期的报告或用户确认，并区分独立复核。
本地报告记载的测试属于历史证据，本轮未重跑，不升级为当前生产通过。
CI 作业名称含 typecheck/vet，也不单凭名称认定其执行内容完整。
只读接口、HTTP 200、模拟 Provider 或构建成功均不能单独证明商业可用。

## 2. 三条开发主线

### A：PG03 Production Launch — READY_FOR_REVIEW

| 字段 | 当前记录 |
| --- | --- |
| 当前任务 | 继续 PG03 Release → RC → Production 部署链路；发布流程尽可能自动连续执行 [E9] |
| 当前状态 | READY_FOR_REVIEW；统一阶段状态为 IN PROGRESS，尚未生产部署 [E9] |
| 当前分支 | `codex/pg03-legacy-retirement-20261016` |
| 独立工作区 | `D:/Codex/worktrees/pg03-legacy-retirement/pg03-mainline-integration-20261015` |
| 已核实 Commit | `fbe2140576009be893f3c3644d2bcc747cf3b086`（`fbe21405`）；用户确认该里程碑 Required CI PASS [E9] |
| PR 状态 | [#42](https://github.com/a3225865526-wq/Clear-API/pull/42) READY_FOR_REVIEW；上次独立查询 OPEN、未合并；本次未复核合并状态 |
| Required CI | PASS：用户里程碑确认 [E9]；替换此前当前 HEAD CI 进行中的记录 |
| Database validation | PASS：用户里程碑确认 [E9] |
| Migration validation | PASS：用户里程碑确认 [E9] |
| Encrypted backup / restore validation | PASS：用户里程碑确认 [E9]；不等同生产 Gateway 已更新或所有回滚路径已验收 |
| Production Gateway / Executor / Channel 2 | NOT UPDATED / HEALTHY / DISABLED：用户里程碑确认 [E9] |
| 已完成事项 | 退役事务、审计保留、一次性维护编排及 Schema 证明实现；上述验证已 PASS，达到审阅里程碑 [E3/E9] |
| 当前阻塞 / 待完成条件 | 未报告新的 CI/数据库/迁移/加密恢复阻塞；生产切换未完成，后续 Release/RC/部署须满足各自安全门禁 |
| 下一步最小行动 | 继续 PG03 Release → RC → Production；减少不必要审批和手动操作，保留全部自动安全门禁 [E9] |
| 是否影响商业上线 | 是：影响 PG03 切换、运行完整性及相关商城路径；不能宣称账号池已生产可用 |

历史：19:44 独立核查时前端 CI 仍运行；本次用户确认该 HEAD Required CI PASS [E9]。
旧 Commit `7165848d`、`645613d9` 的通过记录保留为历史，不替代合并后 main 或正式 RC 的验证。
本次只更新总控文件，不触发 Release、RC 或生产部署，不修改现有审批或保护配置。

### B：CI/CD 自动化开发 — IN PROGRESS

| 字段 | 当前记录 |
| --- | --- |
| 当前任务 | 严格 CI、受控自动发布策略及真实演练证明链闭环 [E4] |
| Git 分支 | `codex/clearapi-auto-cicd-20261015` |
| 独立工作区 | `D:/AI/API/worktrees/clearapi-auto-cicd-20261015` |
| 最新核实 Commit | `520007d535b545a8742c1a6de70a74a9367c619d`，本地与 GitHub 分支一致；为报告提交 |
| 实现 Commit / 验证范围 | `7963fb58abf1e296fd2f641fab2d4800a3d0e3b1`；报告记录本地定向检查，非完整远端 CI 通过 |
| PR 状态 | UNVERIFIED：按当前分支查询所有状态 PR 返回空；尚无该改造的 PR/合并证据 |
| 已完成事项 | 自动合并策略、自动 RC 入口和部署证明消费端代码；离线策略及部分部署回归验证 [E4] |
| 当前阻塞 | 真实 Deployment rehearsal 证明生成端未实现；PG03 合同待对齐；恢复、回滚/通知验证仍有缺口 |
| 下一步最小行动 | 先形成与 A 的最终维护/发布合同对照，明确真实演练的最小输入和产物 |
| 是否影响商业上线 | 完整无人值守自动化可延后；人工受控部署、备份恢复和失败止损能力仍影响上线 |

报告最后结论为“PARTIAL / PRODUCTION AUTOMATION NOT ENABLED”。
该分支本轮未查询到远端 CI 运行；当前启用开关与管理员配置为 UNVERIFIED。
报告中的 relaykit 离线依赖阻塞、备份加密测试失败等未由本轮复验解除。
A 的其他分支后来通过相关测试，不自动解除 B 自身的验证缺口。

### C：网站商业化开发 — UNVERIFIED（当前任务归属待确认）

| 字段 | 当前记录 |
| --- | --- |
| 当前任务 | UNVERIFIED：已有商城、注册、兑换/推荐及审批基础；当前商业验收负责人和进行中任务未核实 |
| 已核实相关分支 | `feat/clearapi-store-approval`；不将其推定为 C 当前活跃分支 |
| 已知工作区 | `D:/AI/worktrees/clearapi-store-approval`；外层 `feat/clearapi-commercial-v1` 属另一 Git 仓库，不混用 SHA |
| 最近已验证功能 Commit | `cd4ee62d3b247a3b95567305078282ea68d9c11d`；#27 三项 Required Checks 成功 [E5] |
| PR 状态 | [#27](https://github.com/a3225865526-wq/Clear-API/pull/27) 已合并，merge=`ad71806fd00518855d4f7d27d72c9d3f846e2cbb` |
| 已完成事项 | Stage 2A 单人审批、受控商品投影、交付证据绑定代码与定向验证；注册/商城基础相关 PR 已合并 [E5] |
| 当前阻塞 | 当前商城完整性问题由 A 的 #42 处理；新商品生产映射、实际交付和完整充值调用闭环未确认 |
| 下一步最小行动 | 确认 C 的当前分支与负责人；汇总现有商业验收证据和最小待测项 |
| 是否影响商业上线 | 是：真实充值到账、基本计费、用户隔离和销售交付是商业放行条件 |

旧 `DEVELOPMENT_RESULT.md` 仍记载 10 月 10 日“无 PR/未合并”，已被后续 PR 事实部分取代。
该文件在后续工作树中被继承，不能用所在目录或修改时间证明其内容是最新阶段。
[财务测试 PR #22](https://github.com/a3225865526-wq/Clear-API/pull/22) 仍 OPEN、CI 成功；
是否为当前生产路径必需项为 UNVERIFIED，不直接列成全站上线阻塞。

### 阶段区分汇总

| 工作线 | 代码 | 测试 | PR 合并 | 生产部署 | 真实商业可用 |
| --- | --- | --- | --- | --- | --- |
| A | DONE：当前审阅候选 [E9] | DONE：CI/数据库/迁移/加密备份恢复 PASS [E9] | UNVERIFIED：上次 OPEN；当前 READY_FOR_REVIEW | IN PROGRESS：Gateway NOT UPDATED [E9] | UNVERIFIED |
| B | IN PROGRESS：消费端已有，生成端缺失 | UNVERIFIED：部分通过且有失败/未运行 | UNVERIFIED：无对应 PR | UNVERIFIED：上次报告未启用 | UNVERIFIED |
| C | DONE：仅 #27 Stage 2A | DONE：仅该提交的 CI 与所列定向验证 | DONE：仅 #27 等已核实 PR | UNVERIFIED：无对应新版本验收 | UNVERIFIED |

## 3. 首版商业上线检查表

以下检查针对“拟上线的精确版本和配置”；历史通过仅在证据栏保留。

| 检查项 | 状态 | 现有证据及最小缺口 |
| --- | --- | --- |
| 用户注册和登录 | UNVERIFIED | 10-09 有限开放时通过 [E6]；当前邮件/注册/登录链未复核 |
| API Key 创建和调用 | UNVERIFIED | 10-09 注册用户创建 Key 及独立真实调用有分开的证据 [E6/E7]；当前用户完整链未验收 |
| 充值与余额系统 | UNVERIFIED | 历史 Legacy 兑换方案及代码基础存在；当前真实付款→交付→兑换到账未确认 |
| 正确计费和余额保护 | UNVERIFIED | 历史隔离调用扣费匹配、保护开启 [E7]；当前版本真实结算/预留/重复请求/隔离待核实 |
| OpenAI 代表性真实调用 | UNVERIFIED | 历史 88token 的 gpt-6-luna Chat Completions 成功 [E7]；不等同当前 PG03 OpenAI 链通过 |
| Claude 代表性真实调用 | UNVERIFIED | 本轮所核实证据未证明当前拟上线版本真实链通过；用户历史也未提供完成证明 |
| Gemini 代表性真实调用 | UNVERIFIED | 同上；需按实际 Provider/协议选择代表路径，不能由 mock E2E 代替 |
| 基本账号池切换 | IN PROGRESS | A 已 READY_FOR_REVIEW；Gateway NOT UPDATED、Executor HEALTHY、Channel 2 DISABLED，尚未完成生产切换 [E9] |
| 生产备份及回滚 | UNVERIFIED | 加密备份/恢复验证已 PASS [E9]；新 RC 对应的生产回滚/故障恢复边界仍须独立验收，不由该 PASS 推定完成 |

按 Provider、协议、调用链分组做代表性真实测试，不要求逐模型全矩阵。
代表路径需覆盖正常响应、基本计费、用户隔离、故障止损；记录版本、时间、结果与证据。
任何未完成的首版 Provider 必须由总协调负责人明确首发范围，不静默写成支持。
退役事务提交后，旧 Gateway 不识别证明，不能将旧镜像重启当作安全回滚 [E3]。

## 4. 当前最高优先级阻塞

只列商业上线放行直接相关项；未验证项不冒充已发现的功能缺陷。

| 编号 | 所属线 | 问题描述 | 解决责任方 | 下一步行动 |
| --- | --- | --- | --- | --- |
| BL-01 | A，C 配合 | 旧生产 Gateway 尚未更新；旧 Runtime/商城问题的候选已 READY_FOR_REVIEW，验证通过不代表生产问题已解除 [E3/E9] | A；发布协调负责人 | 继续 #42 后续 Release → RC → Production，减少不必要人工环节，保留安全门禁及既有有效保护 |
| BL-02 | C，A 配合 | 当前版本商业闭环尚无最终验收与放行证据；三 Provider 实际可售范围未确定 | C 汇总；A 提供调用链；运营方确认交付 | 汇总注册→Key→充值→代表调用→计费/隔离证据，列最小缺口并决定首发范围 |
| BL-03 | A，B 配合 | 加密备份/恢复验证已 PASS；新 RC 对应的生产失败恢复边界尚未最终确认，退役后不可直接回旧镜像 [E3/E9] | A 主责；B 提供演练支持；发布协调负责人 | 复用已通过证据，确认新 RC 对应的失败停止/恢复边界与可核验恢复点，不重复设置无必要人工确认 |

BL-02/03 属于验收与放行缺口，不表示已确认全部相关功能失效。
完整自动合并、无人值守部署、独立告警渠道完善不单列首版商业阻塞。

## 5. 已确定的重要决策

| 编号 | 决策 | 来源与约束 |
| --- | --- | --- |
| D01 | 优先小型商业 MVP，先形成最小商业闭环 | 本轮项目目标；不按大型企业项目标准扩张 |
| D02 | A/B/C 并行，使用独立分支和工作区 | 本轮明确安排；不切换、覆盖或清理其他 AI 工作区 |
| D03 | 首版尽可能支持 OpenAI、Claude、Gemini | 支持声明以代表性真实证据和最终销售范围为准 |
| D04 | 按 Provider、协议、调用链分组验证，不逐模型完整矩阵 | 保留正常调用、基本计费、用户隔离和故障止损检查 |
| D05 | 非核心优化延后，不因完美主义无限推迟上线 | 不据此绕过资金安全、数据保护、备份和恢复边界 |
| D06 | 取消不必要人工审批和手动操作，使发布尽可能自动连续执行 | 2026-10-16 用户里程碑更新 [E9]；取代初始化时“保留人工生产批准”的默认目标；既有保护配置本轮未改 |
| D07 | 多 AI 不随意修改同一模块 | A 主责 PG03/维护过渡，B 主责 CI/CD，C 主责网站商业化；跨界先协调 |
| D08 | 自动连续发布目标已确定，B 的完整无人值守实现和启用状态仍未确认 | [E9] 更新方向；不把目标或验证 PASS 写成自动化已启用，现存有效审批和保护须按正式流程处理 |
| D09 | 冻结历史、审计、余额及 needs_review 不为上线而清理或伪造 | 保留真实失败、迁移与退役事实；不能硬编码 Runtime PASS |
| D10 | 本文件由总协调流程单点维护 | 三条开发线提供证据摘要，不直接争抢编辑 |
| D11 | 保留全部自动安全门禁 | Required CI、Secret Scan、Migration Validation、Runtime Integrity、Billing、Backup、Digest/Manifest、Post-deployment Health Check 等不得删除或绕过 [E9] |

### 待协调的职责与事实冲突

- #42 同时涉及商城完整性和部署工作流：当前由 A 牵头；C/B 在同模块后续变更前先核对边界。
- B 分支基于 `0406a48a`，当前 main 已为 `fdcbc35c`；与 #42 的工作流/部署合同兼容性未确认。
- C 当前活跃分支、最新任务和三 Provider 首发承诺需负责人确认；不直接将旧商业工作区当作当前事实。
- 10-09 有限开放、此前 PG03 BLOCKED 与当前 READY_FOR_REVIEW 属不同阶段；当前仍未生产切换。

## 6. 下一步执行任务

| 工作线 | 近期最重要任务（每线最多 3 项） |
| --- | --- |
| A | 1. 从 #42 / fbe21405 的 READY_FOR_REVIEW 继续 PG03 Release → RC → Production。2. 减少不必要人工审批/手动操作并保留全部自动安全门禁。3. 对精确新 RC 完成部署后健康及恢复边界验收，记录 Gateway/Executor/Channel 2 实际状态。 |
| B | 1. 与 A 对齐 PG03 最终发布合同和重叠文件。2. 在独立分支定义并补齐真实演练证明生成端。3. 整理自身未通过/未运行检查；无人值守激活延后。 |
| C | 1. 确认当前分支、负责人和商业验收证据入口。2. 汇总最小充值交付及注册/Key/计费/隔离待验项。3. 与 A 确定三 Provider 代表性真实测试和首发销售范围。 |

以上均为后续任务记录，本次总控更新不执行开发、真实消费调用、生产维护或发布。

## 7. 关键变更记录

时间均为 Asia/Shanghai；只保留重要里程碑，不收集普通代码修改。

| 时间 | 关键变化 | 证据与范围 |
| --- | --- | --- |
| 2026-10-09 20:11 | 历史有限商业开放 | 仅渠道 1 / 88token / gpt-6-luna / 文本 Chat Completions；非三 Provider 放行 [E6] |
| 2026-10-13 12:40 | #27 商城 Stage 2A 合并 | merge=`ad71806f`；不代表统一商城生产切换完成 [E5] |
| 2026-10-15 22:17 / 22:27 | #38 主线账号池整合、#39 受审 RC 入口合并 | GitHub PR 记录；仍须另行生产验证 |
| 2026-10-16 03:37 | #40 部署控制及恢复修复合并 | main=`fdcbc35c`；对应 CI/RC 成功不等于部署成功 [E1] |
| 2026-10-16（报告复核） | 旧商城退役及维护过渡阻塞仍存在 | #42 OPEN；退役/切换未执行；正式 main RC 和生产维护待批准 [E2/E3] |
| 2026-10-16 19:44 | 初始化统一总控入口与人工生产批准决策 | 本轮用户要求；仅创建此文件，未提交/推送/合并/部署 |
| 2026-10-16（用户里程碑更新） | A 达到 READY_FOR_REVIEW；发布目标改为尽可能自动连续执行 | #42 / fbe21405；CI、数据库、迁移、加密备份恢复 PASS；Gateway 未更新、Executor 健康、Channel 2 禁用；保留全部自动安全门禁 [E9] |

## 8. 证据入口与维护规则

### 证据索引

| 编号 | 来源 | 可以证明的范围 |
| --- | --- | --- |
| E1 | [main](https://github.com/a3225865526-wq/Clear-API/commits/main)、[#40](https://github.com/a3225865526-wq/Clear-API/pull/40)、[CI 38795487615](https://github.com/a3225865526-wq/Clear-API/actions/runs/38795487615)、[RC 38797357843](https://github.com/a3225865526-wq/Clear-API/actions/runs/38797357843) | main SHA、合并、CI/RC 工作流成功；不证明生产成功 |
| E2 | [#42](https://github.com/a3225865526-wq/Clear-API/pull/42)、[CI 38894888260](https://github.com/a3225865526-wq/Clear-API/actions/runs/38894888260)、[历史成功 CI 38891503611](https://github.com/a3225865526-wq/Clear-API/actions/runs/38891503611) | 初始化时 PR HEAD、未合并及各 Commit CI 记录；最新里程碑见 E9 |
| E3 | 本地 `D:/AI/API/worktrees/pg03-mainline-integration-20261015/CLEARAPI_PG03_LEGACY_TRANSITION_FINAL_REPORT.md`；同目录 `CLEARAPI_PG03_LEGACY_STORE_RETIREMENT_FINAL_REPORT.md` | 历史生产只读核查、隔离验证与维护边界；报告尚有最终生产复核待定标记 |
| E4 | 本地 `D:/AI/API/worktrees/clearapi-auto-cicd-20261015/CLEARAPI_FULL_AUTOMATIC_CICD_FINAL_REPORT.md` | 10-15 自动化实现、已跑检查、失败/未运行项及未启用声明 |
| E5 | [#27](https://github.com/a3225865526-wq/Clear-API/pull/27)、[CI 38346660686](https://github.com/a3225865526-wq/Clear-API/actions/runs/38346660686)、[#7](https://github.com/a3225865526-wq/Clear-API/pull/7)、[#11](https://github.com/a3225865526-wq/Clear-API/pull/11) | 审批、注册/商城、推荐基础的代码/PR 证据；不代表当前生产商业验收 |
| E6 | 本地 `D:/AI/API/docs/clearapi-limited-go-live-result-20261009.md` | 10-09 有限开放和当时入口检查；外层仓库历史报告 |
| E7 | 本地 `D:/AI/API/worktrees/clearapi-commercial-v1/docs/clearapi-isolated-provider-acceptance-20261009.md` | 固定旧版本单次真实隔离调用与账务匹配，非当前 PG03 或供应商精确成本证明 |
| E8 | [部署 run 38754573437](https://github.com/a3225865526-wq/Clear-API/actions/runs/38754573437) | 10-15 最近查询到的 Deploy Production 为 cancelled；不证明当前服务停机 |
| E9 | 2026-10-16 本次用户提供的 A — PG03 Production Launch 里程碑及自动化目标 | #42 / fbe21405；READY_FOR_REVIEW，四类验证 PASS，Gateway NOT UPDATED、Executor HEALTHY、Channel 2 DISABLED；本次未重跑测试或独立查询生产 |

本地路径是初始化时的证据入口，不保证在其他机器可用；未进入 main 的报告不得伪装成仓库内链接。
后续交接可提供脱敏报告路径或 PR 引用，不复制凭据、生产配置、用户明细或完整日志。

### 更新约定

1. 总协调负责人在重要阶段完成后决定更新；A/B/C 只提供状态、SHA、PR、证据和最小下一步。
2. 更新时替换过期现状，保留关键历史决策；注明核实时间、版本、验证环境及来源。
3. CI、PR、RC、部署、真实商业验收分别记录，不跨阶段继承 DONE。
4. 新模型/非核心优化不自动扩入首版阻塞；当前阻塞解除后从列表移出，在关键记录留一行。
5. 文件尽量维持 150—250 行；详细测试与部署步骤仅通过报告或 PR 引用关联。
6. 禁止存入 API Key、密码、Token、Cookie、SSH 私钥、连接凭据和用户隐私数据。
7. 本次仅更新总控 Markdown；无业务代码、数据库、生产配置或其他 AI 工作区变更；未提交、未 push。
