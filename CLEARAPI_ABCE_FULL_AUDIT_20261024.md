# ClearAPI A/B/C/E 全面复验与生产一致性审计

本报告按用户指定日期命名；实际复验跨 2026-10-24/25，最终生产快照为 **2026-10-25 02:56:11 UTC / 10:56:11 Asia/Shanghai**。历史 PASS 不作为本轮执行结果。本轮只有生产只读检查、隔离测试和本地修复；没有发布、重启、Push、合并 main、修改生产数据库或配置、真实上游请求。

## 1. 结论与统一矩阵

**生产确实运行 dc685cbe，不是只更新了工具或误用旧前端。发现的是功能验收遗漏与另一开发线尚未完成集成。此前“四线全面验收通过”的表述过宽，应由下表替代。**

| 业务线 | 源码集成 | 候选构建 | 生产镜像 | 端到端功能 | 最终结果 |
|---|---|---|---|---|---|
| A 定价 | PASS，共享实现保留 | PASS，资源和源码一致 | PASS，二进制/前端哈希一致 | 隔离三数据库/API/前端/关键结算 PASS；生产管理员双页面实际操作及真实消费未执行 | **PARTIAL** |
| B 公告通知 | PASS，有管理页面与完整持久化路径 | PASS | PASS | 原镜像保存/发布成功，但当前页面通知缓存不刷新；菜单“系统公告”和“公告”容易混淆。修复镜像隔离浏览器链路 PASS | **FAIL（当前生产缺陷仍在）；本地修复 PASS** |
| C 五家上游 | PASS，逐厂商核对 | PASS | PASS，同一生产二进制隔离运行五种渠道成功 | 各自定向测试、模拟流式/非流式 PASS；真实调用、真实计费均 UNKNOWN | **PARTIAL，各家独立见第5节** |
| E 部署门禁 | PASS，当前有效本地链具备保护 | 宿主部署工具，不属于前端构建 | 宿主安装脚本哈希与候选一致；不属于 Gateway 功能 | 17+7+2 项聚焦合同测试 PASS，真实发布/备份证据一致；本轮未重演生产故障或30项E2E | **PASS（限定本轮门禁与证据范围）** |

独立渠道监测线：**FAIL / 最新运行适配未集成**。不是 PG03：生产触发端点仍返回 `ErrMonitorIntegration`；策略关闭、目标归档，探测记录0。普通 Channel 2 的数据库状态与能力表一致为禁用；进程内缓存状态 **UNKNOWN**，没有把历史缓存故障当作现况。

## 2. 真实工作区、镜像与同次发布证据

- 正式应用工作区：`D:/Codex/worktrees/clearapi-abce-release-20261024/clearapi-controlled-launch-20261020`；分支 `codex/clearapi-abce-release-20261024`。
- 审计起始 HEAD：`a9fe76f301c023fb8ae716901bbe4f94a4184b3e`，起始干净。该提交是发布回执文档；**已提交应用代码相对 dc685cbe 无差异**。本轮修复后本地有未提交改动，未把它冒称生产版本或新发布 SHA。
- 正式工具工作区仍为 `D:/Codex/worktrees/clearapi-local-formal-integration/clearapi-controlled-launch-20261020`，HEAD `80d3dc5b3a58655f736e4fcd49372971c30014b4`。两工作区分别核对，没有覆盖、清理旧开发目录。
- 生产候选完整 SHA：`dc685cbe338e8f700cae347e2eaac351d984c9c7`。
- 运行镜像引用：`clearapi-local:dc685cbe338e8f700cae347e2eaac351d984c9c7`。
- Docker engine/store image ID / RepoDigest：`sha256:aec51220165189cae1eec95882de39e1ebb5c2c51a7ba195eb1ad8747dba3020`。
- 实际导出运行镜像所得 OCI config digest：`sha256:830b9755da75b6e1e085d24323eb48c4213a9f0b5b317abf12966fc240281728`。这里与 engine ID 不是同一层摘要，不能混用。
- OCI revision 为 dc685cbe 完整 SHA；OCI created 标签 `2026-10-24T15:10:51Z`，镜像实际 Created `2026-10-24T15:12:07.348029029Z`。
- Gateway CID：`aaf0638277ab12b71f6df795066e315d196e64952c4aa951d09d6998e0b433e0`，容器 Created `2026-10-24T15:18:11.361128443Z`，Started `15:18:11.692823281Z`。收尾快照 CID/启动时间不变。
- 实际运行 `/new-api` SHA256：`c69df666a7d5755917454609ba16922b3bdfcf4bb4f89fb35c804b601da4e7c7`，与本地原候选镜像二进制一致，**不只依赖镜像标签**。
- Buildx 构建记录 `o50e5us9hcb4a9jlzmq5kfrzv` completed，34/34 steps，VCSRevision 和 SOURCE_COMMIT 均 dc685cbe，linux/amd64，产物 index 与上述 engine ID 一致。构建记录只保存 Context `.`，**历史绝对构建目录不能仅由此记录单独证明**；本轮从源码确认实际脚本绑定自身目录、同时核对最终二进制和前端产物。
- 接收归档142606336 bytes，SHA256 `94101ca6fb96c2b7dc653de29f299e4ca4b5d0535a253547f1a3ad6308c3edce`，现存归档与本次部署回执、`local-deploy-state.json.releases.current` 一致；current 成功时间 `2026-10-24T15:18:26Z`。原 `release-manifest.json` 是历史链的一部分，不能误读为当前运行身份。
- 备份 `20261024T151740Z`，密文12416579 bytes，SHA256 `e5a171ab877977c2c3ba5741dd715b46458416bd6e40444880e302108ca3c756`；本轮重新读 manifest 并计算密文，大小/摘要匹配，绑定切换前 a032815c / OCI config48df93b3身份。备份完成时间在切换之前，非错用候选身份。没有声称本轮做过生产数据库完整恢复演练。

证据：[生产初始只读快照](evidence/abce-full-audit-20261024/production-fresh.json)、[实际二进制/资源/监测数据](evidence/abce-full-audit-20261024/production-artifacts-and-monitor.json)、[Buildx记录](evidence/abce-full-audit-20261024/release-build-record.json)、[收尾快照](evidence/abce-full-audit-20261024/production-end-snapshot.json)。初次 SQL JSON 解析失败已保留于 `initial_errors`，随后单条 JSON 只读事务成功；不能把初次失败算作 PASS。

### 前端与工作区选择

公网和源站 HTML SHA256同为 `9e07389a40656f1663a5f63fe873bc65db268bb11f41dac7100a188d8b56ae9d`。实际引用的5份 JS 和1份 CSS逐一下载计算，公网/源站均一致；主包 `/static/js/index.55158802f7.js` 5430637 bytes，SHA256 `55a9a87b926bea4f36f8f167759869d56cbf9aebef1b32565e538a83eb254e1a`，也与本地原候选镜像完全一致。

当前 `scripts/deploy-local.ps1:16` 从 `$PSScriptRoot/..` 解析 repo，检查 Git root、remote、完整 SHA、干净状态，并将同一个 `$repo` 作为 Buildx context。没有发现“从工具专用工作区构建导致应用遗漏”的证据。**用户现有浏览器自身缓存/当前账号角色仍 UNKNOWN**；公网资源一致不能代替该用户登录页面核查。

全工作树清单已只读核对68个条目，不能访问的路径按 UNAVAILABLE 记录；未提交修改未被清理。来源与当前候选逐文件重新比较，不依赖历史 PASS：[工作区清单](evidence/abce-full-audit-20261024/workspace-inventory.json)、[本轮源码比较](evidence/abce-full-audit-20261024/source-comparison.json)。A的3处差异仅为测试换行/导入排序，B/C所选功能文件一致；没有整旧分支覆盖 main。

## 3. A：统一模型定价

来源：目标 `2e1b2238`，包含后续修复的功能线 `b140f5a7b73c00f022eae00062b41759b25cec56`。当前候选保留其实现，非本轮重新开发。

### 数据路径与运行时刷新

- `web/src/features/model-pricing/api.ts`：两处视图共用 `GET /api/option/model_pricing`、`PATCH /api/option/model_pricing`。模型管理通过 `features/system-settings/models/model-pricing-sheet.tsx` 组合相同定价面板；不是两个独立价格库。
- 保存后的 `invalidateModelPricing` 刷新 model-pricing-config、preview、system-options、pricing、models。前端不同 query key 的展示需要刷新，但保存接口一致。
- `controller/model_pricing_config.go` → `model/model_pricing_config.go` 使用事务、版本校验；提交后更新内存 option/ratio/billing map，执行 `RefreshPricing()` 与 `ratio_setting.InvalidateExposedDataCache()`。未发现此路径“仅入库而不刷新运行时”的证据。
- 隔离 HTTP 实例实际新增5个合成模型，按 shared GET/PATCH 配置表达式并读取，`runtime-mock-and-permissions.json` 逐个保存回读证据。关键输入/输出、缓存读取、单位转换、显式零、冲突/清理及重载另由真实数据库 fixtures 验证。
- 生产 PostgreSQL实际为 **17.6 Alpine**。本轮测试引擎：SQLite **3.50.4**、MySQL **8.4.11**、PostgreSQL **17.6 Debian**。生产方言及主要版本已覆盖；未把 Debian 与 Alpine 构建串写成完全相同的发行包。没有改变数据库结构、驱动或迁移，因此未为本轮前端缓存修复新增迁移。
- 本轮97项定价前端测试、模型清理跨三库、controller转换跨三库与6项关键结算测试通过；结算覆盖缓存 token、同价缓存保留、预扣/结算相符、group ratio、冻结请求输入。**这不是生产付费请求验收。**
- 107条定价语言值已逐条从80d3→dc685提取：en1、zh1，zh-TW/fr/ja/ru/vi各21；全部在与生产哈希相同的实际主包中找到。[逐条构建产物证据](evidence/abce-full-audit-20261024/pricing-locales-bundle.json)。本轮没有修改语言文件。
- 生产只读 `/api/pricing` 的 gpt-6-luna 显示 model_ratio37.5、completion_ratio2；存储的6个定价覆盖 map均为 `{}`。37.5符合 `setting/ratio_setting/model_ratio.go:GetModelRatio` 未命中时的回退，不代表另有一份已保存的模型价格。**无法据此确认这是用户期望的商业定价**；本轮没有替用户改价。

结果 **PARTIAL**：隔离数据合同和生产产物存在性通过；生产管理员两个页面的登录操作、实际历史账单逐笔对账、实际消费/真实计费未执行。没有修改生产模型价格、余额或历史账单。历史规则没有本轮后端改动，但“完全不受影响”不能由未执行的生产交易测试推断。

## 4. B：公告与通知——入口混淆和缓存缺陷是两件事

来源 `1b04b8bc`；生产候选包含前后端实现，二进制/资源与运行镜像对应。用户本轮澄清：**找不到新增或发布入口**，不是已提供失败请求或后端报错。

### 4.1 已实际复现的入口差异

在生产原镜像的隔离实例，以及只修缓存的隔离镜像，用超级管理员通过真实侧边栏导航：

| 菜单 | 路径 | 实际操作与数据 |
|---|---|---|
| 系统设置 → 站点与品牌 → **系统公告** | `/system-settings/site/notice` | 单条 `Notice` 文本，“保存通知”；没有“添加/发布”按钮，保存会替换单条通知文本 |
| 系统设置 → 控制台内容 → **公告** | `/system-settings/content/announcements` | “添加公告”“保存设置”、行“编辑/打开菜单”，草稿行菜单“发布”，已发布行“撤回/删除”；JSON数组 `console_setting.announcements` |

前一页面确实没有用户寻找的按钮，**这是已证明的导航/概念混淆**。`web/src/routes/_authenticated/system-settings/route.tsx` 还要求 `ROLE.SUPER_ADMIN`（100）；后端 option API也拦截普通用户。本轮生产只读角色统计有4个role1、1个role100，不能由统计推断用户当前登录了哪个账户。未获得生产登录页面/角色证据，所以“用户具体误入哪页、是否角色限制或旧浏览器缓存”仍 **UNKNOWN**，不能伪称已唯一定位其具体会话。

截图：[只有保存通知的系统公告页](evidence/abce-full-audit-20261024/system-notice-has-no-add-publish.png)、[存在新增按钮的公告管理页](evidence/abce-full-audit-20261024/announcement-management-add-save-entry.png)。本地镜像未改这两处菜单/按钮，所以此差异来自原候选。

### 4.2 原生产代码缺陷：保存成功，但通知未即时显示

原镜像隔离复现：新增草稿 → 保存设置 → 行菜单发布 → 保存设置。实际请求为 `PUT /api/option/`，请求体 `{key:"console_setting.announcements",value:"[...status=published...]"}`；HTTP200且success=true，PostgreSQL行已持久化，公共status也返回已发布内容。**同一页面通知时间线仍为空；硬刷新后出现。**

根因：`hooks/use-update-option.ts` 只使 system-options 失效，status相关keys漏了 `console_setting.announcements` / `announcements_enabled`。`hooks/use-notifications.ts` 从共享status读取公告，staleTime5分钟、全局不在窗口聚焦时自动刷新。`Notice` 则另读 `['notice']` query；原保存也没使该query失效。它解释保存后显示滞后，**不能解释新增按钮为何不存在**。

本地最小修复仅给上述两个key刷新status，给Notice保存刷新notice query，业务生产代码 **+6行**；未更改权限、后端、存储格式或旧公告。

### 4.3 隔离端到端与保留规则

- 原镜像先稳定复现；3个新缓存回归用例修复前全失败，修复后全通过。
- 本地修复镜像：`clearapi-audit:notification-cache-fix-20261024`，OCI config `sha256:e22a4718b21dc6d0dfabef1d4b978f224ae8ea9ff8e76fd48b9f1134e06b4d56`；SOURCE_COMMIT标成 `local-audit-uncommitted-not-release`，**不是新正式候选，也未部署**。
- PostgreSQL专用 `clearapi_audit_fixed`、测试管理员与普通用户，通过真实浏览器完成：管理页面 → 新增测试公告 → 保存草稿 → 发布 → 保存 → **不刷新页面通知即出现** → 普通用户登录并看到同一内容 → 管理员清理指定测试公告 → 保存；数据库回读为 `[]`。原镜像测试库也清理为 `[]`。没有在生产创建测试公告。
- 普通测试用户真实 `GET /api/option/` 与 `PUT /api/option/` 都403 `AUTH_INSUFFICIENT_PRIVILEGE`；前端隐藏不能替代后端权限，此处后端也已执行验证。
- **遵守用户新增边界：旧公告保留，仅用户主动删除时删除。** `AnnouncementsSection` 新增使用追加，发布/编辑使用按ID更新；后端 `GetAnnouncements` 只过滤公开状态并排序，不按年龄删除；缺失status的旧记录兼容为已发布。聚焦用例已扩展：发布新草稿后，旧公告仍完整保存在提交数组中。
- 通知弹窗当前 `.slice(0,20)` 仅限制最近20条展示，管理列表/数据库没有因此删历史；草稿/撤回仍留存但不公开。本轮不自动删、迁移或覆盖生产旧公告，也不把单条Notice当成历史列表。

证据：[原代码浏览器请求](evidence/abce-full-audit-20261024/B-browser-before-fix.json)、[修复后请求/响应](evidence/abce-full-audit-20261024/B-browser-fixed-network.json)、[普通用户实际显示截图](evidence/abce-full-audit-20261024/announcement-visible-normal-user-after-fix.png)、[测试库清理](evidence/abce-full-audit-20261024/B-isolated-cleanup.json)。原浏览器部分响应体在reload后已不可取，文件明确保留这一限制，没有补造。

结果 **FAIL（当前生产仍有缓存缺陷）**。持久化不是全坏、功能也没有整体漏打进镜像；以往CRUD PASS只覆盖接口/组件，未保护实际通知缓存的完整链路。现在本地修复链路已验证，但尚未上线。

## 5. C：五家上游分层复验

来源 `88e935b934b3eb04f2619a8b6bf94997acea498c`，功能文件与候选逐一一致。下面“镜像PASS”来自 **与生产二进制哈希相同的原镜像** 的隔离运行，不是线上厂商请求。

| 厂商 | 源码/候选/生产镜像 | 定向自动测试 | 原镜像模拟流/非流、映射/认证 | 真实上游 | 真实计费 | 最终 |
|---|---|---|---|---|---|---|
| Grok/xAI（48） | PASS，`relay/channel/xai` | PASS，透传/模型suffix/流usage/中断不伪造DONE | PASS，audit-grok→grok-3-mini，Bearer，`/v1/chat/completions` | UNKNOWN，未执行 | UNKNOWN | PARTIAL |
| DeepSeek（43） | PASS，`relay/channel/deepseek` | PASS，Gemini→Chat转换；共享传输测试PASS | PASS，audit-deepseek→deepseek-chat，Bearer，`/v1/chat/completions` | UNKNOWN，未执行 | UNKNOWN | PARTIAL |
| Qwen（17） | PASS，`relay/channel/ali` | PASS，thinking budget、显式0、top_p、转换/拒绝路径 | PASS，audit-qwen→qwen-plus，Bearer，`/compatible-mode/v1/chat/completions` | UNKNOWN，未执行 | UNKNOWN | PARTIAL |
| 智谱（现代26、旧16） | PASS，`zhipu_4v` / `zhipu` | PASS，旧流每帧/usage/DONE顺序、空body/nil防护、断连；现代转换 | PASS现代26，audit-zhipu→glm-4，Bearer，`/api/paas/v4/chat/completions`；旧16仅定向单测 | UNKNOWN，未执行 | UNKNOWN | PARTIAL |
| Kimi（25） | PASS，`relay/channel/moonshot` | PASS，Kimi温度规则、缺省字段、Gemini转换 | PASS，audit-kimi→kimi-k2，Bearer，`/v1/chat/completions` | UNKNOWN，未执行 | UNKNOWN | PARTIAL |

五种渠道均用正常管理API在隔离实例创建，模型定价通过实际统一接口保存/读取，再分别调用渠道测试API stream=false/true；10次结果HTTP200且success=true。模拟服务实际记录路径、上游模型、stream布尔值与认证scheme，未保存可用密钥。[运行检查](evidence/abce-full-audit-20261024/runtime-mock-and-permissions.json)、[模拟上游线协议记录](evidence/abce-full-audit-20261024/provider-mock-wire.json)。

模拟返回固定10输入/5输出tokens。xAI专用usage与中断回归、智谱usage顺序回归及共享HTTP2重放/重定向/客户端取消测试有证据；**不能把这些推成五家所有用量类型、错误码、超时和计费均完整验收**。本轮未对五家逐一注入超时/所有异常，也没有真实供应商usage账单。缺少真实凭证且用户禁止未授权收费调用，故真实层全部留UNKNOWN。

生产只读渠道实际只有3个type1渠道，只有Channel1启用；没有看到分别配置成48/43/17/26/25的五个生产渠道。这说明“五家代码可配置”成立，**五家已在生产配置可用不成立**。正常管理页面的具体生产登录操作未观察；隔离API配置和原镜像执行可证实配置能力。

## 6. E：当前实际部署链

E来源 `33fcb21d54` 修改了旧发布器及共用备份脚本；不是把旧GitHub发布脚本重新设成入口。本轮比较其两个有效语义：完成证明必须真实有效、锁竞争必须失败，并核对当前Windows入口→Linux helper等效实现。

- `scripts/deploy-local.ps1` 实际执行本地Buildx、镜像归档、SSH直传到 `scripts/local-deploy-vps.py` receive。源码SHA、amd64、revision、OCI config、归档SHA256/字节、前驱身份、正式成功回执逐层关联。
- 服务器已安装helper SHA `b9c11350b7d8d3aef15165bd7a5ea2a5a1ab0f731c39d1f8983bad672e45417f`；state helper `e9596a284d3ebecab4528f60b266831b644c065fa475ea65cf3a84a0cb02da3e`；备份脚本 `8a59049196861273b2c0e2b59e9c46f7f70dbbfba6dddf2740fadb2760fff8cc`。本轮重新计算安装资产，与当前源码一致。
- 后端切换前执行backup，验证新鲜唯一的完成manifest、backup/schema、运行前驱身份、systemd结果/退出码、加密产物名/大小/摘要及恢复合同引用；旧/截断/损坏/错身份/失败服务不能通过。
- Linux receive共用非阻塞`flock`部署锁；备份锁竞争退出75，不生成假完成manifest。聚焦测试实际覆盖竞争行为。
- 健康检查有时限，校验数据库健康与源站/公网应用status；前端失败按合同恢复原镜像/overlay；后端切换后数据库状态未知则保留pending/manual recovery，禁止不安全自动回退。对应故障测试在隔离fixture完成，没有在生产故障注入。
- 本轮当前对象测试：`tests/test_local_deploy_vps.py`17项、`tests/test_local_deploy_adoption.py`7项、`scripts/ci/test-backup-identity.py`2项全部通过。后两项备份fixture包含真实GPG/tar加密和旧恢复reader兼容，不冒充真实生产全库恢复。
- GitHub只读状态：Deploy Production、Automatic production release、Failed production recovery、GHCR Pull Rehearsal均 `disabled_manually`；`CLEARAPI_AUTO_PRODUCTION_ENABLED=false`、`CLEARAPI_AUTO_RC_ENABLED=false`。普通CI等保持active。[workflow证据](evidence/abce-full-audit-20261024/workflows.json)、[变量](evidence/abce-full-audit-20261024/auto-release-variables.json)。
- 生产systemd timers/已知ClearAPI cron与安装配置没有发现其他自动生产触发器。**未覆盖任意外部系统、所有用户私人cron或手工root操作，不能证明绝无其他触发来源**。

结果 **PASS，限定当前脚本/安装资产/发布证据及聚焦合同**。本轮没有调用deploy-local DryRun/receive或触发生产发布，不重复30项部署E2E，没有修改部署工具或恢复旧workflow。

## 7. 独立渠道状态线：具体遗漏与当前现场

| 工作区 | 分支/HEAD | 本轮状态与意义 |
|---|---|---|
| `D:/AI/worktrees/clearapi-dynamic-monitor` | HEAD `72d26b2693…` | clean，早期动态监测实现 |
| `D:/Codex/worktrees/clearapi-monitor-allowlist/clearapi-store-deploy-gate` | HEAD `a55fffb9…` | clean，allowlist阶段 |
| `D:/AI/runtime/clearapi-monitor-runtime-activation` | `codex/monitor-runtime-activation`，`3124d8a1b6bbc44e8a6203266567f72c1734c877` | **4个tracked修改+1个untracked实际运行适配文件，不在正式候选** |

具体未提交文件：`controller/console_monitor_admin.go`、`model/console_monitor_control.go`、`service/console_probe.go`、`service/console_probe_test.go`、新增 `controller/console_monitor_probe.go`。新增文件注册channel运行适配；生产当前 `TriggerConsoleMonitorAdmin` 却仍是 `monitorAdminError(c, model.ErrMonitorIntegration)`，SavePolicy和enabled切换也明确拒绝启用。**有页面/安全框架不代表接通真实执行。**

生产只读数据：policy enabled=false，daily_requests=0，daily_cost_micros=0；唯一channel1/gpt-6-luna目标 enabled=false、archived=true、published=false、allowlisted=false；probe_count=0。因此没有可更新的动态测量结果，不能通过刷新前端凭空补齐。这是已证实的代码遗漏+状态关闭；未擅自启用，也没有把用户报告归入PG03。

普通渠道状态另查：Channel1 status1且ability启用；Channel2 status2、9个ability全false；Channel3 status2、ability false。`controller/channel.go:UpdateChannelStatus`走 `model.UpdateChannelStatus` 后 `InitChannelCache()`；模型层更新缓存/数据库/abilities；`main.go`在启用内存缓存时启动 `SyncChannelCache`。本轮保存状态、禁止普通channel update夹带status、保留非属主列等定向测试PASS。**源码存在定时同步不能证明生产某次同步已执行**；没有授权登录进程缓存检查或真实路由请求，缓存/运行选择留UNKNOWN，未执行生产状态切换。

旧activation分支基于更早版本，不能整文件覆盖当前main的探测成本/预留/账本/恢复门禁。最小后续方案是只移入缺少的channel adapter和触发接线，保留当前成本上限、预留与完成证明、allowlist+published+enabled+not archived条件；先用合成上游和隔离三库验证，再单独申请真实运行授权。现阶段只有fail-closed聚焦验证PASS，**尚不具备把动态监测宣称生产可用的条件**。

## 8. 本轮执行验证、限制与历史PASS纠正

完整聚焦日志保存于 `evidence/abce-full-audit-20261024/logs/`，测试命令与环境摘要见 `validation-current.json`。使用Go1.26.9、Node24.22.1缓存工具；正式Dockerfile仍Go1.26.1/Bun1.4.0。没有升级依赖。

| 实际执行 | 结果 |
|---|---|
| model pricing cleanup/reload/stored-default，真实SQLite/MySQL/PG | PASS，无方言skip |
| controller `TestModelPricingConversionDatabaseMatrix`，SQLite/PG与MySQL分开运行 | PASS，真实三库、转换/preview/零/单位/持久化 |
| 定价相关8份前端测试（含pricing-sync） | 97 PASS |
| 关键6项billing service回归+monitor fail-closed回归 | PASS |
| 原生产镜像公告真实浏览器发布链 | 持久化PASS，通知缓存FAIL，reload后显示 |
| 修复后3项缓存回归+6项公告生命周期（含旧公告保留） | 9 PASS；修复前缓存3项FAIL可复现 |
| console_setting公告过滤/验证、普通用户实际API权限 | PASS；GET/PUT均403 |
| 五家7包Go定向测试+原镜像10次合成上游流/非流 | PASS；真实调用/计费未执行 |
| 渠道状态单键/多键持久化、权限字段边界 | PASS，非生产fixture；未覆盖生产内存缓存现场 |
| E当前helper/adoption/备份 | 17+7+2 PASS，未重跑30项E2E |
| 修改文件oxlint、oxfmt，TS全项目typecheck | PASS（新测试初次lint5项已修复；最终0error） |
| 本地完整Dockerfile构建，含前端与Go编译 | PASS，仅构建隔离修复镜像，未发布 |

历史CI40105138077、历史构建测试依然属于过去的执行证据，本轮没有重新触发CI或将其写成当前新run。仍成立：发布身份/产物、代码集成、适配单测与门禁合同；不足：B真实页面通知刷新、用户管理入口可发现性、动态监测最新接线、真实厂商调用和账单验收。此前“B全面可用”“四线全面生产验收通过”应撤回为上述分层结论。

## 9. 最小修复清单与下一次发布条件

| 项目 | 文件/业务范围 | 当前交付及验证 | 下一步条件 |
|---|---|---|---|
| 公告/Notice保存后刷新 | `web/src/features/system-settings/hooks/use-update-option.ts` | 已本地+6行修复；新增hook回归、扩展现有生命周期用例；隔离浏览器普通用户显示通过 | 整理为干净候选提交，必要CI，另行获得生产部署授权；本轮不发布 |
| 旧公告保留 | 现有announcements数组与生命周期测试 | 保持追加/按ID更新，扩展用例证明发布不删旧记录；生产历史未操作 | 不做自动过期清理，不用Notice单值替代多条历史；若调整20条显示上限，应另明确历史展示需求 |
| 公告入口混淆 | 后续可在 `maintenance/notice-section.tsx` 复用现有导航/按钮链接到content/announcements，并区分两种用途 | **仅方案，未改菜单/文案/权限**；实际两菜单与路由截图已取证 | 推荐保留两数据源及历史，在单条Notice页提供多条公告管理直达；不推荐迁移合并或更改权限。若实施新UI设计须按项目设计流程确认，不影响本轮缓存修复完成 |
| 动态渠道状态接线 | activation工作区上述5文件的有效差异 | 已定位遗漏，现有拒绝执行边界定向测试通过；没有盲目搬旧分支 | 从当前主线做最小移植，保留最新成本/账本门禁；合成上游流量与三库验证完成前不得启用；真实调用另授权 |
| A生产商业价格确认 | 无本轮自动代码/数据修改 | 已报告gpt-6-luna回退37.5与空覆盖map，隔离定价合同PASS | 用户确认期望价格后走正式管理保存；本轮禁止代改价格 |
| C真实供应商与计费 | 无需因无凭证删除已集成适配 | 每家代码/模拟证据分别保留 | 有对应凭证、预算与真实调用授权后分别验收，不能一次PASS覆盖五家 |

**下一次生产部署：可为已经隔离验证的B缓存最小修复准备申请材料；当前没有新的干净候选Commit、当前改动未Push、当前新CI未运行，因此尚不具备立即发布条件。** 若下一次范围还包括动态监测运行接线，则目前更不具备全面申请条件。用户本轮明确禁止发布，该边界优先于早前发布授权。

## 10. D、PG03和现有业务边界

重新检查 `9725fe43`、`112f1cad`、`f126bd10` 均不是dc685候选祖先，候选tree没有 `web/src/features/image-generation/`；实际应用diff无其专属下载/尺寸/重复提交修改。旧有“绘图”配置菜单、生产已有模型名不等于本次D线新功能，不作删除或扩展。

PG03 pool/dedicated/closed-launch标记仍false，继续暂缓商业使用；没有关闭普通API/注册/登录/计费/共享Executor。收尾公网首页、sign-in/sign-up应用壳200，公开status200且success=true、注册开关true，历史公告仍返回1条；未带令牌的 `/v1/models` 为预期401，**只证明认证边界可达，不冒称已验证真实普通API业务请求**。生产登录/注册提交和真实付费API均未执行，端到端业务状态因此不能只靠HTTP200断言。

本轮Gateway/PostgreSQL/Executor/Caddy的容器ID与启动时间未改变。所有测试凭证、公告、合成模型和模拟上游只在专用隔离环境，清理只针对本轮命名资源。生产数据库、渠道状态、模型价格、余额、历史账单、旧公告、DNS/Cloudflare、部署配置均未修改。
