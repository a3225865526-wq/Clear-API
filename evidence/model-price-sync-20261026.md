# ClearAPI 模型价格同步：独立本地修复

- 日期：2026-10-26 Asia/Shanghai
- 基线：`517158135021d4800ee84b333ccb593cedb2c76f`（开始任务时最新本地已提交计费候选；不是对当前生产版本的声明）
- 分支：`codex/clearapi-model-price-sync-20261026`
- 工作区：`D:/Codex/worktrees/clearapi-model-price-sync-20261026/clearapi-controlled-launch-20261020`
- 边界：仅独立 worktree；不复制部署工作区的未提交修改，不 Push、合并、部署、运行发布脚本或连接生产。价格 fixture 仅存在于前端测试的 API mock 中。

## 数据来源与根因

1. 模型栏：`models/components/models-table.tsx` 使用 `useModelPricing(当前模型名)`；`models-columns.tsx` 将返回的 `entries[].effective` 交给现有 `modelPricingDisplay` / `ModelPriceCell`。详情编辑器是 `ModelPricingPanel`。
2. 计费与支付 → 模型定价：`billing/section-registry.tsx` → `RatioSettingsCard` → `ModelRatioForm` / `ModelRatioVisualEditor`。同样调用 `useModelPricing()`；初始系统选项另经 `system-options` 查询载入。
3. 统一接口：GET/PATCH `/api/option/model_pricing` → `model.GetModelPricingSnapshot` / `UpdateModelPricing`。`options` 保留整表编辑格式（含展示用内置表达式），`configured` 是存储配置，`effective` 解析引擎默认值；编辑不得把所有 effective 默认值另行持久化为管理员覆盖。
4. 前端查询按 `['model-pricing-config', ...模型名]` 分开缓存；默认 staleTime 10 秒，关闭窗口 focus 刷新。`system-options` 与公共 `pricing` 的 staleTime 为 5 分钟。保存成功已有 `invalidateModelPricing`，失效模型定价、预览、系统选项、公共定价和模型列表；同一 QueryClient 内活跃查询立即重取，非活跃查询下次使用时重取。没有跨浏览器实时推送契约。
5. 后端统一保存既有事务与每模型版本校验，提交后更新现有运行时选项、调用 `RefreshPricing` 立即重建公共价格缓存并使 exposed ratio 缓存失效。公共 `GetPricing` 另有一分钟惰性刷新；其他实例按既有 `SyncOptions(common.SyncFrequency)` 同步。本次没有改变这些服务端机制。
6. 根因是页面把查询结果复制到只初始化一次的本地快照：`RatioSettingsCard.pricingBaseline` 与 `ModelPricingPanel.entry` 后续忽略新结果。设置页选中的可视化编辑器还持有独立 `editData`。因此查询已经更新时，页面仍能展示旧价格。

## 单位、分组及账务口径

- 旧 token 倍率模式：输入单价 `ModelRatio × 2 USD/1M`；输出、缓存读取、缓存创建分别乘 `CompletionRatio`、`CacheRatio`、`CreateCacheRatio`；图像/音频分支沿用现有倍率与专用规则。显式零价保留。
- 表达式 token 系数直接是 USD/1M，`fixed(x)` 是 USD/请求；表达式可自行规定阶梯、缓存、模态及请求条件。本次不改表达式或价格转换逻辑。
- `relay/helper/price.go:HandleGroupRatio` 使用用户组→请求分组的特殊倍率（有配置时），否则使用请求分组倍率。预扣和结算沿用所选倍率与冻结价格快照，不把管理页基础单价再写成组后价格。
- `service/text_quota.go` 旧倍率结算按真实 token 分类加权后乘模型倍率和分组倍率；固定单价按 USD 单价 × `QuotaPerUnit` × 分组倍率。表达式路径以 USD/1M 的结果除以一百万，再乘 `QuotaPerUnit` 与分组倍率。保留缓存语义归一化、退款、舍入及幂等路径。
- 旧 token 倍率的 `×2 USD/1M` 展示口径对应项目标准 quota 单位；任意修改 `QuotaPerUnit` 会涉及账务单位语义，不属于此次前端同步修复。本次未改它。
- `model-pricing/currency.ts` 与 `pricing/lib/price.ts` 使用现有展示币种汇率；编辑器把所选币种输入换回 USD 后保存。公共定价可选的充值价显示为 `USD价格 × priceRate / usdExchangeRate` 后按展示汇率格式化；充值价格不等于模型倍率。没有修改汇率、充值价、分组倍率或生产数据。

## 最小修改

- `web/src/features/model-pricing/model-pricing-panel.tsx`：未编辑时采用统一查询的新条目；编辑时保留条目及其版本，沿用服务端冲突检查和显式重载。
- `web/src/features/system-settings/models/ratio-settings-card.tsx`：价格字段只从统一定价快照初始化和刷新，系统选项仅提供独立的公开倍率开关。未编辑时刷新 baseline；表单或内嵌编辑器有草稿时冻结 baseline。避免系统选项对象重建造成价格草稿重置。
- `web/src/features/system-settings/models/model-ratio-form.tsx`、`model-ratio-visual-editor.tsx`：传递已有编辑器的 dirty 状态，并刷新当前选中的未编辑模型；关闭的移动端编辑面板不会因查询更新被重新打开。沿用现有组件，没有新建通用 UI 或第二套价格系统。
- `web/src/features/model-pricing/__tests__/synchronization.test.tsx`：仅新增一个聚焦测试文件，4 个场景验证双入口保存及输入/输出/缓存读取/缓存创建同步，以及草稿、旧版本冲突、显式重载。

## 实际本地验证

所有命令在上述独立工作区执行；前端命令目录为 `web/`。Bun 1.4.4 来自本机 npm exec 缓存，以其完整路径调用；以下简写为 `bun`。依赖仅在新工作区通过 `npm exec --yes --package=bun -- bun install --frozen-lockfile` 安装，package.json 与 bun.lock 未修改。

1. 修复前，`node node_modules/vitest/vitest.mjs run src/features/model-pricing/__tests__/synchronization.test.tsx -t 'saving from Models' --reporter=dot` 在进入实际保存后失败：另一处输入价格期望 `3`，实际仍为 `2`。早期测试 fixture 的缓存创建标签和初始化等待也已修正；不将这些 fixture 失败当成业务根因。
2. 相关现有测试和新增回归：

   ```powershell
   bun run test src/features/model-pricing/__tests__/synchronization.test.tsx src/features/model-pricing/__tests__/save-errors.test.tsx src/features/model-pricing/__tests__/editor-currency.test.tsx src/features/model-pricing/__tests__/pricing.test.ts src/features/models/__tests__/model-listing.test.tsx src/features/models/__tests__/metadata-editing.test.tsx src/features/system-settings/__tests__/pricing-sync.test.tsx --maxWorkers=2 --reporter=dot
   ```

   结果：**7 个文件、97 个用例通过**。覆盖模型列表有效价、缺失价格、零价、元数据独立保存、价格冲突/保存失败及币种转换等既有契约。已有 Base UI `nativeButton` 提示未导致失败，未扩展到无关控件修复。
3. 最后一次 callback lint 调整后，`bun run test src/features/model-pricing/__tests__/synchronization.test.tsx --reporter=dot`：**4/4 通过**；`bun run typecheck`：**通过**。
4. `bun x --no-install oxlint -c .oxlintrc.json` 加上述 5 个 TSX 修改文件：**通过，无诊断**。
5. 对上述 5 个 TSX 文件执行 `bun x --no-install oxfmt -c .oxfmtrc.json --check`（检查时临时剥离原版权头并在 finally 中逐字恢复，沿用仓库保护头部的做法）：**通过**；`git diff --check`：**通过**。

仅前端状态同步变更，无 SQL、schema、数据库驱动或账务算法改动；未执行 Go 测试、三数据库矩阵、全量前端回归或发布构建。未做真实浏览器/真实后端/真实上游及生产验收；测试的保存请求由本地 API mock 接管，不代表实际账务验证。当前本地修复无已知阻断项。

## 后续集成时更新 CLEARAPI_MASTER_STATUS.md

不要在正在部署的工作区直接更新总控记录。集成本提交时追加一条独立记录：基线、修复 Commit SHA、独立分支和工作区、上述前端快照根因、统一定价源及草稿/冲突保护、实际定向验证命令与结果。明确“仅本地验证、未 Push/合并/部署、未修改生产价格与倍率”；集成后应按实际发布状态另记部署与验收，不能把本地 API mock 测试写成生产或真实上游验证。

建议追加内容（修复 SHA 取包含本记录的提交）：

> 2026-10-26 — 模型定价同步独立本地修复：基于 `517158135`，分支 `codex/clearapi-model-price-sync-20261026`。修复两个入口在统一查询刷新后仍使用首次价格快照的问题；复用现有 GET/PATCH model_pricing、查询失效及后端缓存刷新，保留草稿和 expected_version 冲突控制。相关 7 文件 97 测试通过，最终同步回归 4/4、typecheck、改动文件 lint/格式及 diff 检查通过。计价单位/汇率/分组关系已静态核对，后端账务及生产价格/倍率未改。仅本地提交，未 Push/合并/部署；未做真实后端或生产验收。完整说明见 `evidence/model-price-sync-20261026.md`。
