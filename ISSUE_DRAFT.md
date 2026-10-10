## Agent

- Tool: Codex
- Tool version: 宿主未提供精确版本
- Model (full id): 系统标识 GPT-6，未提供完整模型 ID
- Host (CLI / IDE / GitHub coding agent / other): Codex desktop / Windows
- Date (UTC): 2026-10-10

## User request

- Verbatim: “允许跳过缺失的 writing-plans，按已批准设计直接编写实施计划并开发”
- Later constraints or corrections from the user (quote, or none): 无；先前设计批准及完整要求保存在本任务。

## Out of scope — refuse

- Matched: no。本 Issue 向运营方自有 Clear-API 仓库提交其批准的产品开发，不向上游反馈第三方网站故障。
- If yes, what was told to the user (stop here; do not file): 不适用。

## File gate — do not file unless all are satisfied

- Out of scope (including pass-through): no
- Usage / configuration / integration (answered instead of filing): no
- Required facts present without invention (actual behavior, impact, frequency, evidence in new-api, applicable type fields): 本地功能需求与复现具备；无需真实生产交易。
- Body is short and factual; no unfiltered AI-generated text: 待负责人审核。
- File: no，尚待发布正文人工审阅。
- If no, what was told to the user (stop here): 先完成代码与验证，再审核发布正文。

## Kind

- [x] Feature
- [x] Bug

## Usage / configuration / integration check

- https://docs.newapi.ai/ — what was searched, conclusion: 已打开首页与 User Guide / Home，网页仅说明现有首页配置；本次功能差异以本地 main 源码和测试为准。
- https://deepwiki.com/QuantumNous/new-api — what was searched, conclusion: 已打开 Overview 与 Redemption Codes，描述单次兑换及已有管理流程；不能代替当前分支源码验证。
- README / repo docs: 现有贡献约定要求关联 Issue 与人工过滤正文。
- Relevant code paths and conclusion: `features/shop/catalog.ts` 单商品硬编码；`controller/user.go` 邮箱验证分支清空 affCode；现有兑换路径没有持久化返利事务。
- Can the current version already do this? (required for feature requests): 无当前所需八档持久化管理与合格兑换返利。
- Verdict: product bug or new feature / usage question (stop here): 新功能与注册邀请参数修复。

## Environment

- new-api version / commit / image tag (not `latest` / `unknown`): Clear-API main f57a392ae860361dae32f38331c0ba3530ad74e4
- Deploy source (repo release / official image / main source / other): main 源码独立工作区。
- Database (sqlite / mysql / postgres): 开发验证 SQLite 3.50.4、PostgreSQL 17.11、MySQL 8.4.11。

## Problem facts

- Actual behavior: 商品单档硬编码；邮箱验证码注册清空邀请参数；缺少合格兑换持久化返利。
- Impact: 无法由管理员维护八档商品，也无法按批准规则可靠返利。
- Frequency: 邮箱验证码注册分支每次清空邀请参数；缺失功能持续存在。
- Evidence that the problem is in new-api rather than the client or upstream: main 本地源码和新增注册回归测试；修复前前端路由邀请参数测试失败、修复后通过。

## Type-specific details

### Relay / API

不适用；没有转发或 Provider 请求变更。

### Billing

不修改模型计费。返利比例默认 10%，只增加邀请人站内额度，不扣被邀请人余额；500000 Quota 对应 $1 API 使用额度。

### Frontend

- Page path: `/shop`、`/wallet#referrals`、`/sign-up?aff=...`、超级管理员站点设置。
- Browser and version: 本地 Codex 内置浏览器，未提供精确引擎版本。
- Active theme: 深色手机 390×844，浅色桌面 1280×900。
- Relevant browser Console / Network errors: 未对浏览器控制台作完整认证端到端检查；预览采用隔离模拟接口。

### Deployment / upgrade

实际生产升级不适用；三个隔离数据库验证新表、历史样本与重复迁移。没有部署生产。

## Reproduction and expected result

- Steps to reproduce: 访问带 aff 参数的邮箱验证注册页面，验证邮箱后提交；检查 main 的 affCode 清空分支。
- Expected result: 邮箱验证仍严格校验，邀请关系只在注册成功时一次绑定。合格付费卡密兑换后异步持久化发奖，重试不重复增加额度。
- Related screenshots (optional): `validation/shop-dark-mobile.jpg`、`validation/shop-light-desktop.jpg`。

## Feature (feature requests only)

- Feature description: 八档商城配置、专属邀请链接、持久化且幂等的合格兑换奖励、超级管理员配置与审计。
- Use case: 运营方核实商品交付后上架，独立审批奖励开启；奖励失败可恢复且不重复入账。

## Duplicate check

- Search queries (issues, PRs, discussions): Clear-API 全部 Issue、最近 15 个 PR。
- Closest existing threads: PR #7 单商品商城及邮箱注册。
- Why this is not a duplicate: 本次新增多商品后台及兑换返利，修复现有注册邀请绑定。

## Research

### Docs

- https://docs.newapi.ai/ : [User Guide / Home](https://docs.newapi.ai/en/docs/guide/home)，2026-10-10 打开。
- https://deepwiki.com/QuantumNous/new-api : [Redemption Codes](https://deepwiki.com/QuantumNous/new-api/6.3-redemption-codes)，2026-10-10 打开，第三方索引可能滞后。
- README / other repo docs: AGENTS、PR 模板、Billing 规则已读取。
- Conclusions: 本地批准的源码功能开发，不是配置问答。

### Code

`controller/user.go` 注册；`model/user.go` 邀请关系；`model/redemption.go` 原子兑换；`features/shop` 商品；`features/wallet` 原购买与 Legacy 兑换。

### Experiments

- Command or redacted request: 验证码注册、29 项前端测试、三数据库返利故障及并发测试，详见 `DEVELOPMENT_RESULT.md`。
- Direct upstream result: 不适用。
- Result through new-api: 定向行为验证通过；全量类型检查存在 main 既有错误。
- Conclusion: 新开发已完成本地定向验收，发布门禁尚未完成。

## Working theory

- What is broken or missing: 邀请参数被清空，商品配置及持久化返利缺失。
- Why: 现有注册分支和商城源码直接体现。
- What would falsify this: 原 main 能保持邮箱注册邀请关系并完成上述可配置返利验收。

## Scope

- In scope for a later PR: 本次已批准商城与返利功能。
- Out of scope / not this repo: 生产部署、真实奖励开启、支付集成、商品库存、账号池与模型计费。
- Large or directional feature? If yes, this issue is for maintainer alignment; do not open a PR yet: 是；负责人已在本任务批准设计，发布正文仍待审核。

## Proposed direction

符合已批准要求，历史默认排除；同一兑换最多一次发奖；余额、累计上限、流水与状态原子一致；默认关闭奖励；紧急关闭后人工审计恢复。

## Not verified

生产实时配置、真实云猫交付、认证后台浏览器端到端、最低支持版本数据库、远程 CI、RC 与镜像 digest 尚未验证。

## Related

- Issues / PRs / upstream docs: Clear-API PR #7；待创建关联 Issue。
