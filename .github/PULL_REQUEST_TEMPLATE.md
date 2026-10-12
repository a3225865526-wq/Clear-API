<!--
AI coding agents must follow the template selection rules in AGENTS.md; project-owner PRs may use this template without pre-submission human review.
-->
# ⚠️ 提交说明 / PR Notice

English template: `.github/PULL_REQUEST_TEMPLATE/en.md`

> [!IMPORTANT]
>
> - 请披露 AI 辅助开发情况。PR 正文及后续评论均不得粘贴未经整理的大段 AI 输出；仅保留维护者需要复核的要点。
> - 开启透传请勿反馈问题；透传会直接发送内容，不会经过 new-api 的逻辑处理。
> - 请按本模板填写后再提交。

## 🔗 关联任务 / Related Issue
- 新功能请填写下方 Issue 编号；若还没有对应 Issue，请先自行创建。功能讨论请放在 Issue 中进行。
- 改动较大或方向性变更，请先在关联 Issue 中与维护者达成一致，再提交 PR。
- Bug 修复请关联对应 Issue。设计取舍、理解偏差或预期不一致，更适合作为讨论或功能请求。

- Closes #

## 🚀 变更类型 / Type of change
- [ ] 🐛 Bug 修复 (Bug fix)
- [ ] ✨ 新功能 (New feature)
- [ ] ⚡ 性能优化 / 重构 (Refactor)
- [ ] 📝 文档更新 (Documentation)

## 📝 变更描述 / Description
(简述做了什么、修改范围及为什么生效。不得直接粘贴未经整理的大段 AI 输出。如果难以简述，建议先拆分范围，或在 Issue 中与维护者对齐。)

## 📋 验证与风险 / Validation and risks
(请写明实际验证命令及观察结果、已知风险和未验证项。截图或录屏仅在真实生成且有验证价值时提供，不得伪造。)

## 🤖 AI 辅助开发 / AI assistance
(说明是否使用 Codex 或其他 AI 辅助；不得虚构人工审核或责任签署。)

## 🔄 必需 CI / Required CI
(列出必需检查及当前状态；不得绕过分支保护。)

## ✅ 提交前检查项 / Checklist
- [ ] **AI 辅助披露:** 已如实说明 AI 辅助开发情况，未虚构人工审核、责任签署、测试通过或部署成功。
- [ ] **非重复提交:** 我已搜索现有的 [Issues](https://github.com/QuantumNous/new-api/issues) 与 [PRs](https://github.com/QuantumNous/new-api/pulls)，确认不是重复提交。
- [ ] **新功能关联 Issue:** 若此 PR 标记为 `New feature`，我已关联对应 Issue；若尚无 Issue，我已先自行创建。
- [ ] **事前沟通:** 若改动较大或涉及方向性变更，已在关联 Issue 中与维护者沟通并达成一致。
- [ ] **功能范围:** 本 PR 不是 Coding Plan、逆向渠道、第三方封装接口，也不是对 Codex 渠道类型的改动。
- [ ] **非透传:** 本 PR 不是针对开启透传后的转发行为；透传会直接发送内容，不会经过 new-api 的逻辑处理。
- [ ] **范围聚焦:** 本 PR 为一项聚焦改动，未包含无关代码。
- [ ] **本地验证:** 已按变更路径实际验证，并写明命令与观察结果。仅声明 `go build` 通过或测试通过，不视为有效证明。
- [ ] **安全合规:** 代码中无敏感凭据，且符合项目代码规范。
