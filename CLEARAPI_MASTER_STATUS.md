# ClearAPI — 权威总控（MASTER STATUS）

> **本文件是 ClearAPI 项目唯一权威总控文件。**
> 位置：主产品仓库 `D:\AI\API\upstream\new-api` 根目录。
> 最近更新：2026-10-18 18:15 +08（Asia/Shanghai）
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
| 自动部署 | `USER_CONFIRMED_COMPLETE` | `USER_CONFIRMED` | 用户已确认完成。**「自动化能力全链路已验证」未复核**，不可对外如此宣称。⚠️ 存在一项门禁缺陷待修（备份硬门禁误通过），见 3.1 与 6.1。 |
| 商业化 MVP | `USER_CONFIRMED_COMPLETE` | `USER_CONFIRMED` | 用户已确认完成。不得重新列为 P0。真实 Provider 计费仍 `UNVERIFIED`。 |
| **PG03 FINAL LAUNCH** | **`IN_PROGRESS`** | 已有真实请求证据 | **当前主线，未完成，未冻结。** 导入 + 真实请求 + 计费已走通；剩余可用性、双账号、提交/RC、正式发布。见第 4 节。 |
| 五条并行工作线 | 工作区就绪，功能未开发 | — | 见第 5 节。E 线已完成首轮审查并产出确认缺陷。 |
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
- ⚠️ **已知门禁缺陷（E 线审查确认，待修）**：备份硬门禁存在误通过路径 — 锁争用时 `production-backup.sh` 记录 SKIP 并 `exit 0`，部署脚本据 `ExecMainStatus=0` 判定成功，而新鲜度检查扫描整个 `BACKUP_DIR`（含 staging 文件）。详见 6.1。**该缺陷使「自动部署已完成」不能延伸解读为「自动部署门禁可靠」。** 发布前必须修复并验证。

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

### 4.3 已完成部分（截至 2026-10-18 18:09）

**证据来源：用户提供的 PG03 FINAL LAUNCH 进度截图（2026-10-18）。** 标 `用户截图` 的条目为截图陈述，本轮**未由 Agent 独立复核**生产侧。

#### 网站 JSON 导入 — PASS

- 两个 CPA 文件经 Gateway multipart API 导入。
- Executor 物理隔离写入成功。
- 凭证初始为 `pending_trust` / 未验证。
- 正式 trust 与 capability 验证已完成。
- 模型注册已恢复并验证。

#### 真实请求链路 — PASS

- Gateway Channel 测试通过。
- Executor `/v1/models` 返回 `gpt-6-luna`。
- 站点 API Key 请求 `/v1/responses` 返回 **HTTP 200**。
- 响应内容：`GATEWAY`。

#### Usage / Billing — 与日志一致

- 上游 usage：input 13、output 7、total 20 tokens。
- 站点日志与上报 usage 一致。
- 实际扣费：**1013 quota**。
- 请求记录：1 次 attempt、1 次 success。
- 本请求未观察到重复派发或重复结算。

#### 早前阶段（PG03 自报，未复核）

- Gateway RC / Executor RC 身份、隔离备份恢复演练：报告为 PASS。
- Rehearsal：除 Provider 外全部 gate PASS；assembler 拒绝生成 `release-proof.json`。

> ⚠️ **口径提醒**：以上「真实请求 + 计费」证明**单账号单请求链路可用**，**不等于**双账号调度与故障转移已验证，也不等于 PG03 已完成。

### 4.4 剩余工作（4 项，缺一不可标记上线）

| # | 项目 | 状态 |
| --- | --- | --- |
| 1 | 验证 Gateway 账号可用性 | `NOT VERIFIED` |
| 2 | 验证真实双账号调度与故障转移 | `NOT VERIFIED` |
| 3 | 完成代码提交与 RC 验证 | `NOT RUN` |
| 4 | 完成正式生产发布与部署后检查 | `NOT RUN` |

**结论口径：在上述 4 项全部取得证据前，PG03 保持 `IN_PROGRESS`，不得标记为 fully launched。本轮未标记完成。**

### 4.5 相关的独立部署缺陷

E 线审查确认「备份硬门禁误通过」路径（见 6.1）。该缺陷影响**部署门禁可靠性**，与本轮 PG03 请求链路验证相互独立：

- 发布前必须修复，并验证**只有完成且有效的备份及其 manifest** 才能满足门禁。
- 修复责任归属：automatic-deployment / PG03 集成负责人。
- 在该修复完成并验证前，不得依赖现有备份门禁作为发布放行依据。

**处理原则：** PG03 的剩余工作不因历史阻塞项停滞；是否以受权手动 Deploy V2 推进由用户与总协调负责人决定。**不得因历史文档写着 PG03 FROZEN 而冻结当前 FINAL LAUNCH，也不得因链路已走通而提前标记上线。**

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

### 5.1 E 线（门禁审查）首轮结果

| 项目 | 值 |
| --- | --- |
| 工作区 | `D:\AI\API\worktrees\clearapi-gate-audit` |
| 分支 | `codex/clearapi-gate-audit` |
| 审查 HEAD | `7b9144df` |
| 工作树 | CLEAN |
| E 改动的业务/部署文件 | **无**（保持只读） |
| push / merge / deploy | **均未执行** |

E 线仍为只读，未见代码变更。

---

## 6. 已核实事实快照（2026-10-18 18:12 +08）

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
| 生产站点 | `https://www.clear999.com/api/status` → HTTP 200 | `VERIFIED`（2026-10-18 17:47 只读探测；18:12 未复测） |

**历史发布（本轮未复核）：** 2026-10-18 03:48 曾记录一次生产发布完成（main `eb67c5f2`、RC `39060814419`、digest `sha256:51ddf6df…`）。此后 main 前进至 `353cd0a2`，PG03 FINAL LAUNCH 针对新候选继续推进。

### 6.1 E 线确认缺陷 — 备份硬门禁误通过

**状态：`CONFIRMED — FIX REQUIRED`（生产发布阻塞级）**

E 线指出，Agent 本轮已逐行核实以下代码属实：

| 位置 | 行为 |
| --- | --- |
| `scripts/production-backup.sh:22-25` | 锁争用时记录 `SKIP concurrent run detected` 并 **`exit 0`**（成功退出码） |
| `scripts/production-deploy.sh:834-836` | `systemctl start` 后仅校验 `ExecMainStatus == 0`，因此**接受被跳过的调用** |
| `scripts/production-deploy.sh:842-843` | 新鲜度检查 `find "$BACKUP_DIR" -newermt ... -size +0` 扫描**整个备份目录，含 staging 文件** |

**风险：** 一个并发进行中、尚未完成的 staging dump 可能满足部署的备份新鲜度检查，使部署在**没有有效完成备份**的情况下放行。

**要求的修复方向：** 只有**已完成且有效**的备份**及其必需的 manifest** 才能满足门禁；`SKIP` 退出必须与真实成功可区分；新鲜度检查不得把 staging 文件当作完成品。

**归属：** 交由 automatic-deployment / PG03 集成负责人做**窄范围**修复。E 线继续保持只读。

**验证要求：** 修复后必须验证「并发锁争用 → 部署被阻断」「半成品 staging 文件 → 不满足门禁」「真实完成备份 + manifest → 放行」三条路径，再接入发布流程。

### 6.2 已撤回的发现（勿再作为缺陷处理）

- **V2 header SHA256 不是缺陷。** 实际文件 SHA256 与 `scripts/pg03-release-control.py` 中的 `V2_SHA`（`f115059f07bf4a6dc669833e6680d29493931154bdfc2f4339a23bf81fb67262`）一致。无需处理。

### 6.3 保留的其他发现

- v1 与 v2 之间有**九个 helper 逐字节相同** — 精简候选，需评估合并风险。
- **G42 可能因单行条件阻断整个发布** — 需单独评估，确认是否为过度严格规则。

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
| 7 | 链路已走通 vs 是否可标记上线 | **已裁定**：真实请求与计费 PASS 仅证明单账号单请求可用；4 项剩余工作未完成前保持 `IN_PROGRESS`。 |
| 8 | 「自动部署已完成」 vs 备份门禁可被误通过 | **已裁定**：阶段状态不变（`USER_CONFIRMED_COMPLETE`），但新增独立缺陷记录，发布前必须修复验证。 |

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
| 2026-10-18 18:15 +08 | **写入 PG03 最新进度与 E 线审查结果。** PG03：网站 JSON 导入 PASS（2 个 CPA 文件、物理隔离写入、trust/capability 验证、模型注册恢复）；真实请求 PASS（Channel 测试、`/v1/models` 返回 `gpt-6-luna`、`/v1/responses` HTTP 200 返回 `GATEWAY`）；计费与日志一致（input 13 / output 7 / total 20、扣费 1013 quota、1 attempt 1 success、无重复派发结算）。新增 4 项剩余工作，**PG03 仍为 `IN_PROGRESS`，未标记上线**。E 线：登记 `CONFIRMED — FIX REQUIRED` 备份硬门禁误通过缺陷（已逐行核实代码），撤回 V2 SHA256 发现，保留九 helper 重复与 G42 单行阻断两项。**未修改业务代码，未 push，未部署。** |
