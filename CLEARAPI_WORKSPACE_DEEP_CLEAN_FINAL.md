# CLEARAPI 正式工作区深度清理与实验准备最终报告

日期：2026-10-31（Asia/Shanghai）。本轮只清理开发环境及准备分支，保留 new-api / QuantumNous 归属。没有业务代码、数据库架构、Redis 配置或生产部署变更。

## 1. 正式基线与最终开发入口

- 工作区：`D:/AI/API/worktrees/clearapi-new-vps-six-service-helper-20261031`；已用 `git rev-parse --show-toplevel` 实核。
- 正式源码基线：`c8e4c06fe9bbd282a48deb16d6714b9688f08bf6`，Tree `4ca6d56719e4b727d07e058d8efcd80922d0fe76`。
- 固定正式候选：`codex/new-vps-six-service-helper-20261031`；受保护备份：`codex/clearapi-sub2api-offline-ready-backup-20261031`。两者保持原 SHA，不移动、删除或重新指定候选。
- 已核对 c8 相对 ff560c159faaea7eabf364d886afc6c51b3266f3 仅新增工作区索引；ff 相对 helper 修复 8839580ed5989b1b0ce679babf7e3f35f2babd8b 仅增加最终验收报告。未直接切换 GitHub 备份来猜测基线。
- 实验分支：`codex/shared-infrastructure-optimization-20261031`，创建时 SHA 为上述 c8。当前正式目录已切换到此分支；只承载本轮索引和清理报告的必要本地文档提交，未开始共享基础设施开发。
- **最终工作区 HEAD / 实验分支最终完整 SHA**：为包含本文件的文档提交，见 `D:/AI/API/runtime/workspace-deep-clean-20261031/final-receipt.json` 的 `head` / `experiment_sha`，以及唯一权威总控本轮追加项。可用 `git rev-parse HEAD` 和 `git rev-parse codex/shared-infrastructure-optimization-20261031` 直接复核。提交无法在其自身内容中嵌入自身 SHA，故在提交后独立落盘精确回执；正式基线完整 SHA 已在上文固定。

## 2. Git 完整审计与修改保护

开始：分支为 GitHub 备份分支，HEAD=c8，tracking production 同名分支 ahead0/behind0。staged、unstaged、untracked、ignored 均空，stash 为空；没有需要另行保存的本工作区修改、二进制或本地密钥，未使用 reset --hard / clean -xfd。

已记录仓库根、全部本地/远端跟踪 refs、每个分支独有提交数及未被远端包含的提交数、全部未推送提交 SHA、worktree/detached HEAD、Git 对象统计。完整原始审计在 `D:/AI/API/runtime/workspace-deep-clean-20261031/audit-before.json`；不是把旧报告当本轮审计。

结束验收要求：`git status --porcelain=v2` 空，`git ls-files --others --exclude-standard` 空，`git ls-files --others --ignored --exclude-standard` 空；提交后真实结果记录于 final-receipt.json。该要求仅针对当前正式目录；历史工作区已有修改保留，唯一权威总控按要求在其历史目录追加，不能将所有外部 worktree 宣称为干净。

初始 111 个本地分支：A 正式候选1；B 相对基线无独有提交且无 worktree44；D 有独有提交41；E 有 worktree24；受保护备份1。C 类已完成实验中，能证明已完全包含于正式基线的归入B，其余保守归D；不因名字过期就判无用。87 个远端跟踪引用保留，未 fetch/prune 改写它们。

## 3. 分支与 worktree 整理

普通 `git branch -d` 删除 **41** 个 B 类分支。每项均核对 `git rev-list --count BRANCH --not c8e4c06fe9` 为0、没有 worktree，并先写入 `refs/archive/workspace-deep-clean-20261031/原分支名`。这些提交均在 c8 历史可达且由已验证备份保护。恢复示例：`git branch 原分支名 refs/archive/workspace-deep-clean-20261031/原分支名`。

local main 保留。两个虽已合入 c8、但普通删除因未合入其跟踪上游而被 Git 拒绝的分支保留：`codex/pg03-autonomous-production-20261016`、`codex/pg03-rc-containerd-20261016`；未使用 -D、未解绑上游绕过拒绝。D/E 类、备份和正式候选全部保留。

worktree 登记38→36，只清理两个实际路径不存在且 Git 原生标为 prunable 的 Temp/opencode 登记：clearapi-probe-cost-policy、pg03-phase1-baseline。操作前保留每个有效 HEAD 的本地归档引用；只运行 `git worktree prune --dry-run --verbose` 和随后普通 prune，没有删除任何实际工作目录。外部三个零 HEAD 的空仓库登记未触碰，不将其当损坏源码删除。

三个运行依赖历史目录保留：

- `D:/AI/API/worktrees/clearapi-controlled-launch-20261020`（唯一权威总控及原运行绑定）
- `D:/AI/API/worktrees/clearapi-sub2api-integration-20261030`（原运行绑定）
- `D:/AI/API/worktrees/pg03-images-v1-executor-20261019`（独立仓库/原运行绑定）

所有其他外部 worktree 也保留。本轮未查询或改动 Docker 控制面、容器、镜像、数据卷、数据库或运行绑定；不把历史绑定记录冒充当前容器健康检查。

逐项分类（含独有及未推送数）见 audit-before.json 的 branches；删除/保留结果见 branch-cleanup.json；失效登记见 worktree-prune.txt，均在上述 runtime 审计目录。

## 4. 文件与真实占用

扫描3755个文件，不跟随 junction/symlink；未发现链接、node_modules、dist、Go构建二进制、日志/coverage/test输出、未跟踪下载或忽略缓存。当前最大文件是正式翻译/指南资产，evidence 约4.60 MB、web 约22.55 MB，均属应保留内容。因此本轮**删除源码目录内临时文件/缓存为0，释放0 bytes**；前轮28.404 GB清理不计入本轮。

| 指标 | 开始 | 结束 |
|---|---:|---:|
| 文件数 | 3755 | 3756 |
| 文件逻辑字节 | 43417470 | 43431619 |
| NTFS实际分配字节（文件ID去重） | 50570064 | 50586448 |

结束增量仅为索引和本报告。实际分配使用 Windows `GetFileInformationByHandleEx(FileStandardInfo).AllocationSize`、按文件ID去重，避免把逻辑字节或 GetCompressedFileSizeW 的非压缩文件长度误报为真实分配；不包括工作区外共享 Git 管理目录、目录元数据或外部 runtime。证据 size-before.json / size-after.json。

共享 Git 管理目录位于 `D:/AI/API/upstream/new-api/.git`。开始 loose3745/363.80MiB，12个pack/316.67MiB，prune-packable1037；垃圾58项/156.93MiB（含历史 tmp_pack/tmp_obj 及 worktree空refs目录）。这些位于其他工作区共用管理目录，未自动 GC、重打包或删除；不扩大到共享对象清理，不影响当前源码干净状态。Git对象体积不宣称释放。

`web/package.json`、`web/bun.lock`、所有 go.mod/go.sum 保留；本轮没有 node_modules 可删。恢复依赖使用既有 Bun 锁及 Go 模块清单（Bun建议 `bun install --frozen-lockfile`，Go按各模块版本恢复），未安装依赖或运行构建来重新生成缓存。

## 5. 部署包与资产保护

`D:/AI/API/runtime/six-service-helper-20261031/release-v2.tar`：560998400 bytes；SHA256 **ed0c3dba06d435852892483ca3647f635d3308f35536673bc62c1a2600a1f01d**，与桌面副本均本轮重新计算一致。只读校验 tar 内27个成员、SHA256清单26项全部一致。

operator-source.json 实核：operator8839580ed5989b1b0ce679babf7e3f35f2babd8b、Gateway92edfc345d9b9526379ed25363bb79abf9839874、Sub2APId851a38477597157e78450d30af2d8504764d085。包内5个helper及18个部署输入与当前源码逐字节比对（仅规范LF/CRLF）一致；不把 c8 当成包内Gateway版本。证据 package-source-verification.json。

已保存/核查原跟踪文件内容：源码、Sub2API集成、Windows deploy-local.ps1及4个辅助helper、六服务compose.yaml、Dockerfile、环境/目标配置模板、备份恢复/迁移脚本、数据库迁移、测试源码、evidence/new-vps-six-service-20261031/final-release-v2、最终验收报告、历史报告和项目身份。未整理或改写任何最终验收内容；本工作区同名 MASTER_STATUS 仍为历史副本。

## 6. GitHub与桌面恢复

本轮 `git ls-remote production refs/heads/main refs/heads/codex/clearapi-sub2api-offline-ready-backup-20261031` 实得：main716cba323d59ef9c5a8e942a1678ce09f315a1cf；backup c8e4c06fe9bbd282a48deb16d6714b9688f08bf6。只读操作，无push/force/main或远端历史删除。

桌面 `D:/Users/Chen/Desktop/ClearAPI_FULL_BACKUP_20261031` 的 SHA256.json 所列18项本轮全部重算匹配，含3个AES-GCM归档、源码ZIP、增量bundle、恢复说明/工具、历史清理证据、v2包。加上runtime v2共19项验证通过。`git bundle verify latest-candidate.bundle` exit0，明确依赖 ff560c159f，不将增量包冒充独立完整备份。先前完整恢复/认证证据也在清单中且摘要未变；本轮未再解密数GB历史或重复克隆，不声称进行了新一次全量恢复。

密钥文件 `D:/Codex/private-backups/clearapi-20261031/RECOVERY_KEY.hex` 存在且64bytes，未读取/输出其内容；父目录原受限ACL保留。工作区无本地secret待移动，不删除生产密钥或原runtime。桌面与本机私有密钥仍属同一物理设备，异地灾备未在本轮建立。最新文档提交不推GitHub，提交后额外本地增量bundle保存在本轮runtime目录。

## 7. 实际验证、未解决事项与停止点

已执行：Git状态/diff/refs/stash/worktree/独有与未推送审计、磁盘逐文件扫描、远端refs只读核对、19项备份摘要、增量bundle verify、v2包26项摘要及源码对应、归档refs恢复可达性、所有原跟踪文件保持不变、最终diff check/clean状态、总控字节前缀追加验证。提交后最终命令/结果与完整SHA另存 final-receipt.json。

未运行应用测试、前后端构建、三数据库矩阵或真实provider测试：只有Git元数据及两份工作区文档变更，无相应实现变化。未执行生产VPS/钱包/迁库/启池/收费/DNS/容器卷操作，未重新开发PG03，也未进行PG/Redis共享优化。

无阻断本次清理/准备的已知问题。保留事项：两个普通删除被拒绝的历史分支、共享Git临时对象、全部有独有成果或外部worktree、历史三个运行依赖目录；它们均非未处理工作区修改。新VPS验收、真实厂商验收、生产上线仍各自PENDING，本轮不改变原结论。

下一轮从当前实验分支开展共享PostgreSQL/Redis设计；开始前执行 `git status`、核对本报告提交与固定c8差异仅文档，并按下一轮授权阅读适用规则。实验失败可放弃实验分支，从固定正式候选c8重新建立工作区；不使用旧数据库快照覆盖新账务，不触动正式v2包。完成本报告和唯一权威总控追加后停止。

## 已删除分支清单

| 分支 | 原SHA（完整值见branch-cleanup.json） |
|---|---|
| `clearapi-docs-center` | `a2f97420777b` |
| `codex/abce-audit-preserved-20261025` | `8230b6013c36` |
| `codex/clearapi-abce-release-20261024` | `a9fe76f301c0` |
| `codex/clearapi-announcement-monitor-fix-20261025` | `74ff807aa950` |
| `codex/clearapi-announcements` | `f7b4884dbc3b` |
| `codex/clearapi-controlled-launch-20261020` | `67b0002389a3` |
| `codex/clearapi-dynamic-monitor-runtime-20261025` | `879eff62e017` |
| `codex/clearapi-local-deploy-formal-integration` | `80d3dc5b3a58` |
| `codex/clearapi-pricing` | `b140f5a7b73c` |
| `codex/clearapi-protocol-billing-compat-20261025` | `382d521086a4` |
| `codex/clearapi-rehearsal-consumption-fix-20261020` | `8111b59b16ed` |
| `codex/local-release-pg03-retired-20261030` | `92edfc345d9b` |
| `codex/monitor-admin-allowlist` | `a55fffb9a1c4` |
| `codex/pg03-billing-finding-20261019` | `731cf6f4dd35` |
| `codex/pg03-blocker-doc-20261019` | `b540937ca0f3` |
| `codex/pg03-chain-run-20261019` | `c36898e1aa69` |
| `codex/pg03-exact-rc-acceptance-20261021` | `41c58069ae77` |
| `codex/pg03-executor-repin-20261019` | `d952574195ac` |
| `codex/pg03-full-launch-20261018` | `1c2c555a3cc6` |
| `codex/pg03-general-pool-gateway-20261029` | `87732dc22fce` |
| `codex/pg03-images-v1-integration-20261019` | `0551bc982991` |
| `codex/pg03-isolated-hostname-binding-20261021` | `1fc0158e7433` |
| `codex/pg03-legacy-retirement-20261016` | `fbe214057600` |
| `codex/pg03-mainline-integration-20261015` | `b6f4c3ef8ce7` |
| `codex/pg03-migration-proof-20261021` | `1a57f501a941` |
| `codex/pg03-production-readiness-20261021` | `f2e179ebc8e2` |
| `codex/pg03-rehearsal-dispatch-20261019` | `ab3b47b89621` |
| `codex/pg03-rehearsal-fix-20261018` | `1a280879829c` |
| `codex/pg03-report-correction-20261019` | `915df2a0994a` |
| `codex/pg03-reviewed-rc-entrypoint-20261015` | `1f7f946c62a3` |
| `codex/pg03-runtime-store-policy-20261019` | `550c2b2e9a8a` |
| `codex/pg03-successor-preparation-20261021` | `5024c6f4f657` |
| `codex/pg03-trigger-chain-20261019` | `67d2162f7ac9` |
| `codex/pg03-v2-deploy-unblock-20261015` | `1ec64b7bc83b` |
| `codex/production-deploy-safety-fix` | `0fd58cf6bd03` |
| `codex/remove-tool-budget-gate` | `86451cd20e9e` |
| `codex/runtime-integrity-v1-gate` | `260831724f02` |
| `codex/sub2api-commercial-deployment-20261031` | `6f92278ad3ca` |
| `feat/runtime-identity-v1` | `0e31977683e6` |
| `feature/pool-strategy-integration` | `ed461d35ec80` |
| `integrate/clearapi-pg03-abce` | `f1a75fb9c929` |
