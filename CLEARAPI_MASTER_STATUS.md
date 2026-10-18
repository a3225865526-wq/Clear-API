# ClearAPI — 权威总控（MASTER STATUS）

> **本文件是 ClearAPI 项目唯一权威总控文件。**
> 位置：主产品仓库 `D:\AI\API\upstream\new-api` 根目录。
> 最近更新：2026-10-18 17:52 +08（Asia/Shanghai）
> 维护人：**仅项目总协调负责人**。各功能开发 AI 不得修改本文件。

---

## 0. 维护规则

1. 只有项目总协调负责人更新本文件；各工作线 AI 一律不得修改。
2. 工作线通过**报告**提交状态：分支、Commit SHA、修改文件、测试结果、剩余问题。
3. 必须区分五件事，不得合并表述：
   **代码完成 → 测试通过 → PR 合并 → 生产部署 → 真实可用**
4. 每条状态标注证据口径：`USER_CONFIRMED` / `VERIFIED` / `UNVERIFIED` / `NOT RUN`。
5. 本文件**精简可维护**：只留结论、边界、索引，不复制测试日志。
6. 历史总控文件不删除、不覆盖、不移动，仅作只读历史（见第 9 节）。

### 状态词汇（固定用法）

| 标记 | 含义 |
| --- | --- |
| `USER_CONFIRMED_COMPLETE` | 用户确认完成；**不等于**全链路证据已逐项复核 |
| `VERIFIED` | 有可复核证据（CI / RC / digest / 实测） |
| `IN_PROGRESS` | 正在进行，尚未交付 |
| `PARTIAL` | 部分完成，存在明确未闭合项 |
| `DEFERRED` / `UNVERIFIED` | 明确延期或未验证 |
| `NOT RUN` | 从未执行 |
| `NOT READY` | 链路存在但未达就绪条件 |

---

## 1. 项目战略

**目标：最快完成一个稳定、可收费、可调用、可部署、可恢复的小型商业 MVP。**

- 不追求大型企业级复杂度。
- 不要求所有模型逐一完整验收。
- 完成线 = 能上线、能收款、能调用、能回滚，而非覆盖所有边角。
- 已完成阶段不得因历史门禁文档被重新拉回 P0，也不得重复启动。

**优先级（唯一顺序）**

1. **P0 — PG03 FINAL LAUNCH**（当前主线，`IN_PROGRESS`）
2. **P1 — 五条并行工作线**：定价、公告、五家上游、生图、门禁审查
3. **P2 — 本地深度清理**：仅在 PG03 成功部署并完成基础验证之后

---

## 2. 当前阶段总览

| 阶段 | 状态 | 证据口径 | 说明 |
| --- | --- | --- | --- |
| 自动部署 | `USER_CONFIRMED_COMPLETE` | `USER_CONFIRMED` | 用户已确认完成。**「自动化能力全链路已验证」未复核**，不可对外如此宣称。 |
| 商业化 MVP | `USER_CONFIRMED_COMPLETE` | `USER_CONFIRMED` | 用户已确认完成。不得重新列为 P0。真实 Provider 计费仍 `UNVERIFIED`。 |
| **PG03 FINAL LAUNCH** | **`IN_PROGRESS`** | 进行中 | **当前主线，未完成，未冻结。** 见第 4 节。 |
| 五条并行工作线 | 工作区就绪，功能未开发 | — | 见第 5 节。 |
| 本地深度清理 | `DEFERRED` | — | 排在 PG03 部署与基础验证之后。 |

---

## 3. 已确认完成的阶段（禁止重复启动）

### 3.1 自动部署 — `USER_CONFIRMED_COMPLETE`

- 用户确认完成，此状态为准。
- 历史总控中的 `Automatic Deployment: BLOCKED / NOT READY` 属于**该阶段历史记录**，不作为当前阻塞。
- 事实澄清（不改变上述状态）：历史证据显示「无人值守自动路径」曾为 `NOT_READY`，2026-10-18 的实际发布走的是**受权手动 Deploy V2**路径。因此：
  - 「自动部署机制已交付」= `USER_CONFIRMED_COMPLETE`
  - 「完全无人值守自动部署已逐项验证」= `UNVERIFIED`
- 任何工作线**不得**以「自动部署未完成」为由阻塞自身任务。

### 3.2 商业化 MVP — `USER_CONFIRMED_COMPLETE`

- 用户确认完成，此状态为准。
- 历史总控中的 `Small Commercial MVP: NOT READY` 已过期。
- 保留边界：**真实 Provider usage/billing = `DEFERRED / UNVERIFIED`**。这是已知延期项，不是当前阻塞；**不得伪造 receipt 绕过门禁**。

---

## 4. PG03 FINAL LAUNCH — `IN_PROGRESS`

**进行中。不冻结、不标记完成，直至取得正式上线证据。**

### 4.1 目标

1. 个人 OAuth 账号接入
2. 网站 JSON 导入
3. 真实账号池
4. 正式生产上线

### 4.2 专用工作区（禁止其他工作线进入开发）

| 项目 | 值 |
| --- | --- |
| Gateway 工作区 | `D:\AI\API\worktrees\clearapi-pg03-full-launch` |
| 分支 | `codex/pg03-rehearsal-fix-20261018` |
| HEAD | `48857622b` |
| 未提交改动 | **6 项，必须原样保留**（`service/accountpool/` 4 文件已修改 + 2 个 `__pycache__` 未跟踪） |
| Executor 工作区 | `D:\AI\API\worktrees\pg03-executor-mvp` @ `228ecbaa6`（8 项未提交，原样保留） |
| 导入专项工作区 | `D:\AI\API\worktrees\clearapi-executor-pg03-import-20261018` @ `265750da8`（7 项未提交，原样保留） |

**硬约束：** 不得在 PG03 工作区内创建或执行其他开发任务；不得 reset / stash / checkout / 清理其未提交改动；不得删除或移动工作区。

### 4.3 已完成部分（PG03 自报口径，本轮未复核）

- 网站 JSON 导入：前端、Gateway API、Executor 原子导入已开发，multipart 路由测试 PASS；**生产未部署**。
- Gateway RC / Executor RC 身份、隔离备份恢复演练：报告为 PASS。
- Rehearsal：除 Provider 外全部 gate PASS；assembler 拒绝生成 `release-proof.json`。

### 4.4 已知阻塞（记录，不据此冻结）

- **真实 Provider usage/billing 证据缺失**：现有两个 OAuth 账号均为 `free` 计划、未确认商业用途。
- 自动 proof 合同要求绑定精确 RC 的成功 Provider receipt；伪造 receipt 属违规。

**处理原则：** 该阻塞影响「无人值守自动发布」，**不影响 PG03 继续推进 FINAL LAUNCH**。是否以受权手动 Deploy V2 推进由用户与总协调负责人决定。**不得因历史文档写着 PG03 FROZEN 而冻结当前 FINAL LAUNCH。**

---

## 5. 五条并行工作线

统一基线：**`353cd0a23b723f86c6b21e759a6d691ce1ba5457`**（`production/main` 本地与远程一致，2026-10-18 实测）

| 线 | 工作区 | 分支 | 职责 | 边界 |
| --- | --- | --- | --- | --- |
| **A 定价系统** | `D:\AI\API\worktrees\clearapi-pricing` | `codex/clearapi-pricing` | 清理现有模型定价记录；统一「模型管理」与「集中定价页面」价格逻辑；验证新增 / 编辑 / 保存 | **仅限模型定价记录**；不得误删模型定义、渠道、用户数据、历史账单。**用户已明确**：不要求先完成备份恢复演练或「未配置价格拒绝逻辑」单独验证才可开工。 |
| **B 公告与通知** | `D:\AI\API\worktrees\clearapi-announcements` | `codex/clearapi-announcements` | 公告及通知管理入口；新增、编辑、发布、撤回、删除；发布生命周期；管理权限与用户侧展示 | 不改动其他工作线文件；不触碰生产配置 |
| **C 五家上游 API** | `D:\AI\API\worktrees\clearapi-providers` | `codex/clearapi-providers` | Grok、DeepSeek、通义千问、智谱 AI、Kimi 的上游 API 接入 | **只接上游 API，不为这五家建设自有账号池** |
| **D 生图功能** | `D:\AI\API\worktrees\clearapi-image-generation` | `codex/clearapi-image-generation` | 检查并恢复原有生图入口；检查生图 API、路由、结果展示、计费；补齐必要功能 | 以恢复现有实现为主，不重写架构 |
| **E 门禁审查** | `D:\AI\API\worktrees\clearapi-gate-audit` | `codex/clearapi-gate-audit` | **只读**盘点 CI、构建、发布、部署、运行时门禁；识别重复检查、无关阻塞、过度严格规则；形成精简建议 | **本阶段只审查**，不修改正在使用的生产发布工作流或生产部署脚本 |

**共同规则**

- 各线独立分支；**不得自行修改本总控文件**。
- 完成后上报：分支、Commit SHA、修改文件清单、测试结果、剩余问题。
- 未获指令不得 push、不得合并 PR、不得部署生产。

---

## 6. 已核实事实快照（2026-10-18 17:47 +08）

| 项目 | 值 | 口径 |
| --- | --- | --- |
| 主产品仓库 | `D:\AI\API\upstream\new-api` | `VERIFIED` |
| 产品远程 | `production` → `https://github.com/a3225865526-wq/Clear-API.git` | `VERIFIED` |
| 上游远程（非产品） | `origin` → `QuantumNous/new-api` | `VERIFIED` |
| `production/main` 本地 | `353cd0a23b723f86c6b21e759a6d691ce1ba5457` | `VERIFIED` |
| `production/main` 远程 | `353cd0a23b723f86c6b21e759a6d691ce1ba5457` | `VERIFIED`（`ls-remote` 实测一致） |
| 本地 `main` | `ad71806fd00518855d4f7d27d72c9d3f846e2cbb` | `VERIFIED`（**落后于 production/main，本轮未改动**） |
| 外层仓库 | `D:\AI\API`，HEAD `82242b9cf`，**无任何 remote** | `VERIFIED`（**不是主产品仓库**） |
| Executor 仓库 | `D:\AI\API\upstream\CLIProxyAPI` | `VERIFIED` |
| 生产站点 | `https://www.clear999.com/api/status` → HTTP 200 | `VERIFIED`（本轮只读探测） |

**历史发布（本轮未复核）：** 2026-10-18 03:48 曾记录一次生产发布完成（main `eb67c5f2`、RC `39060814419`、digest `sha256:51ddf6df…`）。此后 main 前进至 `353cd0a2`，PG03 FINAL LAUNCH 针对新候选继续推进。

---

## 7. 状态冲突与裁定

| # | 冲突 | 处理 |
| --- | --- | --- |
| 1 | 历史记 PG03 = `FROZEN`(10-16) / `BLOCKED`(10-18) | **已裁定**：现为 `IN_PROGRESS` FINAL LAUNCH；历史段仅作历史。 |
| 2 | 历史记自动部署 `BLOCKED / NOT READY` | **已裁定**：现为 `USER_CONFIRMED_COMPLETE`。 |
| 3 | 历史记商业化 MVP `NOT READY` | **已裁定**：现为 `USER_CONFIRMED_COMPLETE`。 |
| 4 | 历史「个人 Codex OAuth 未确认商业用途前不得作为商业上游」 vs 当前目标含「个人 OAuth 账号接入」 | **待裁定**：需用户明确个人 OAuth 用于**自用/测试**还是**商业中转**。在此之前不得声称已获准商业上游。 |
| 5 | 10-18 03:48「生产发布完成」 vs 09:44「生产未切换」 | **已解释**：前者为旧候选 `eb67c5f2`，后者针对新候选 `353cd0a2`，不矛盾。 |
| 6 | 三份历史总控内容不一致（11.9 / 13.9 / 145.5 / 9.0 KB） | **已处理**：本文件为唯一权威；旧文件保留只读。 |

---

## 8. 目录与工作区布局

```
D:\AI\API                         外层协调仓库（无 remote：deploy/docs/runtime/scripts/tests）
├── upstream\new-api              ★ 主产品仓库（production → Clear-API）
├── upstream\CLIProxyAPI          Executor 仓库
└── worktrees\                    ★ 两个仓库的工作树混放，命令必须用 git -C "<路径>"
    ├── clearapi-pg03-full-launch              PG03 主线（禁止其他任务）
    ├── pg03-executor-mvp                      PG03 Executor（禁止其他任务）
    ├── clearapi-executor-pg03-import-20261018 PG03 导入专项（禁止其他任务）
    ├── clearapi-pricing                       A 定价
    ├── clearapi-announcements                 B 公告
    ├── clearapi-providers                     C 上游
    ├── clearapi-image-generation              D 生图
    ├── clearapi-gate-audit                    E 门禁审查
    └── clearapi-master-status-unification     本文件的工作区
```

> ⚠️ **操作铁律**：`D:\AI\API\worktrees` 同时存放**两个不同仓库**的工作树。所有 Git 命令必须显式指定仓库或工作区，如 `git -C "<工作区路径>"`，禁止在外层仓库误执行。

---

## 9. 历史总控文件索引（只读保留）

| 路径 | 大小 | Git 状态 |
| --- | --- | --- |
| `<主仓库根>/CLEARAPI_MASTER_STATUS.md`（本文件） | 见最新提交 | 已跟踪 — **唯一权威** |
| `worktrees\clearapi-pg03-full-launch\CLEARAPI_MASTER_STATUS.md` | 13.9 KB | 已跟踪（PG03 分支较新版本，含 `PARTIAL` 分类） |
| `worktrees\clearapi-master-status-20261016\CLEARAPI_MASTER_STATUS.md` | 145.5 KB | 已跟踪 + 本地已修改 |
| `worktrees\pg03-mainline-integration-20261015\CLEARAPI_MASTER_STATUS.md` | 9.0 KB | **未跟踪** |

以上三份历史文件**均保留原状**，其未提交修改已原样保全，不再作为当前状态依据。

---

## 10. 变更记录

| 时间 | 变更 |
| --- | --- |
| 2026-10-18 17:52 +08 | **重写为唯一权威总控**（分支 `codex/clearapi-master-status-unification`，基线 `353cd0a2`）。原基线根目录已有 11.9 KB 旧版总控（内容为 PG03 `RESUMED` 段 + 2026-10-16 冻结历史），本次整体替换为精简权威版。统一三份互相冲突的历史总控；确认自动部署与商业化 MVP 为 `USER_CONFIRMED_COMPLETE`；确认 PG03 为 `IN_PROGRESS`；登记五条并行工作线及边界。**未修改任何业务代码，未 push，未部署。** |
