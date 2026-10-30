# CLEARAPI — Windows 本地候选与 PG03 退出交付

2026-10-30，Asia/Shanghai。本轮仅准备候选与源码备份。**正式路径为 Windows 本地构建 → SSH 传输 → 已安装 helper 备份、切换与健康检查；GitHub 仅备份源码。GHCR / Actions 正式 RC 不属于本次发布要求。** 本轮没有生产部署、重启、数据库写入、启池或真实模型收费请求。

## 1. 身份与工作区

- 原总控工作区 HEAD `f30b247383ee5a18f370d1ad65b30204d32c8833`，大量既有 dirty 修改、分支、测试和证据完整保留，未混入候选。
- 历史统一候选 `baf610906200ee5b742d0f5773a58db471e46829` 保留。本轮基于它创建应用候选 **`92edfc345d9b9526379ed25363bb79abf9839874`**，tree `1442086ab3632db793509f2d4cc03c35deafec3c`。
- 可部署源码干净检出：`D:/AI/API/worktrees/clearapi-local-release-pg03-retired-20261030`，分支 `codex/local-release-pg03-retired-20261030`，HEAD 固定为上述应用候选。
- 备份/报告工作区：`D:/AI/API/worktrees/clearapi-local-release-backup-20261030`。报告提交仅增非敏感证据/报告，不替换应用候选身份；部署时使用应用工作区，不将文档 HEAD 冒充已构建镜像 revision。
- 22:55:25 +08:00 新鲜 pinned SSH + READ ONLY 事务观察：生产 Gateway 确为 `732a2d5162c15604aa7ec7bbba12bb5c9d356031`；镜像 config ID `sha256:3da62d984e98d0cddd6fbc1c04fe7cc502268cc23efacfa0c64286ef2f9da960`。此结论来自现场，未直接沿用历史 SHA。Executor 仍为原 `201ce387…`，运行且健康；原 PostgreSQL 17.6 健康。
- 当前 GitHub main 经 API/正常 fetch 核实为 `716cba323d59ef9c5a8e942a1678ce09f315a1cf`，它不代表当前生产，也未拿它覆盖本地商业成果。

## 2. PG03 最终退出与代码分类

未来账号池采用 Sub2API；PG03 不再继续开发或作为商业账号池启用。以下“恢复”仅修改新本地候选，没有回滚生产。

| 分类 | 处理 |
|---|---|
| 尚未发布的自动准入 | 将 `model/pool_routing.go` 恢复为现场生产732的精确源码；未发布模型继续拒绝池渠道，普通渠道仍沿原路由工作 |
| 尚未发布的自动 proof | 将 `service/accountpool/policy_snapshot.go` 恢复为732精确源码；排除无 fallback policy 时新增 active/proof、复用外部 request ID 等877增量；保留原已部署签名/请求绑定控制 |
| 对应独立测试 | 恢复 `model/pool_gate_step4_test.go`、`model/pool_gate_step6_gemini_grok_test.go` 的732断言；从新候选删除仅测试自动proof的 `policy_snapshot_test.go`；原877分支和测试仍在历史中 |
| settings 保存 | 保留 `relaykit/dto/channel_settings.go` 的 UpstreamPool/PoolRoutingEnabled 与其回归，避免 typed settings 更新丢失已有绑定数据 |
| 渠道故障处理 | 保留 `service/channel.go`、`relay_error.go`、`request_policy.go` 及其回归。该代码在绑定池渠道时将账号故障留给执行层，普通渠道仍走原 ShouldDisableChannel；本轮不扩大或重写它，也不为退出池而撤掉共享调用链 |
| 已部署PG03与共享基础 | 原 accountpool/Executor、准入/路由/鉴权、Billing/Usage、钱包预留/退款、幂等、outbox、恢复及数据库全部保留；已部署PG03模块只是关闭待将来独立退役 |

唯一应用提交修改5个文件（包含一个候选测试删除），没有新框架、价格/账务算法、schema、数据库访问方式、依赖或前端修改。当前生产732是应用候选的祖先；商业、指南、Sub2API源码未被倒退。

## 3. 原敏感门禁的真实处理

此前 `deploy-local.ps1 -DryRun` 的 `source diff classification` 拒绝**确属 Windows 本地发布流程**，不是GHCR误报：两个 `service/accountpool/policy_snapshot*` 路径有未部署PG03增量。

已做逐项差异及依赖核对，按上表排除独立增量。新候选与现场732在 `service/accountpool/` 完全无差异，模型路由也恢复为732。原版脚本没有修改、白名单没有扩大；本轮没有用 FixturePath、TestTarget 或伪造回执。

实际执行：

```powershell
pwsh -NoProfile -File scripts/deploy-local.ps1 -DryRun -ConfigPath <既有私有 local-deploy.config.json>
```

从干净应用工作区运行，**exit 0**：commit=92edfc345…，previous_commit=732a2d516…，arch=amd64，scope=backend，changed_files=101，estimated_backup=true，result=`DRY_RUN_NO_PRODUCTION_WRITES`。只读生产 preflight 验证身份、数据库与内外健康后成功完成分类。

此前正式OFF rehearsal 的 `OFF candidate repository or digest mismatch` 是GHCR专用路径，本次不再要求该回执。不修改它，不伪造GHCR/CI身份，也不将历史拒绝写成新应用失败。**本地敏感路径阻断已消除，无待负责人接受的PG03敏感增量；实际生产部署仍须另行授权。**

安全参考已查阅 OWASP [Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)，并核实 [ASVS 5.0.0](https://asvs.owasp.org/) 为稳定版。适用边界为保留服务端准入/请求身份绑定、拒绝未发布模型和缺失master、禁止客户端头代替可信proof；没有改变浏览器会话或认证协议。通过原已部署实现精确字节恢复及定向拒绝测试验证此范围，不声称完成全应用ASVS认证或覆盖全部控制项。

## 4. 本地产物与版本

旧 `D:/AI/API/runtime/unified-rc-20261030` 完整保留；由于应用源码确有变化，旧镜像不能代表新候选，必要重建已完成。

新产物目录：**`D:/AI/API/runtime/local-release-pg03-retired-20261030`**。

| 身份 | 实际值 |
|---|---|
| 应用源码/OCI revision | `92edfc345d9b9526379ed25363bb79abf9839874` |
| 本地镜像 | `clearapi-local:92edfc345d9b9526379ed25363bb79abf9839874` |
| manifest / containerd Image ID | `sha256:1f8c11550fce8c05d501b47314472e4d1b4790b800500423d5d07992b1793607` |
| 经典Docker config ID（VPS核对值） | `sha256:a70bf2ccf6da4442666175236754d14bdf2ded5e92c4ffbfcf5756fdeada848c` |
| Gateway二进制SHA256 | `59dbde3ecd4262c696885f446d91132ff2e9c6d881d2e1a61a2e7e6eca1fbdc2` |
| image.tar大小 / SHA256 | 156227584 bytes / `f1db26e4fc962315cac87fce86fe3f24d04e87bdd4f3297a4a378c566c3bf269` |

原Dockerfile、基础镜像digest、Bun锁、Go依赖、entrypoint保持。Buildx Linux/amd64以不可变 `git archive` 输入构建全部前端、Gateway和5个运维二进制，exit0。所有导出manifest/config/layer哈希与二进制逐一核对，OCI revision/source/platform一致。VERSION沿用生产空文件，`--version`为空；按完整SHA/OCI候选label识别，未虚构语义版本。

新最终镜像 `network=none`、无宿主端口/生产卷/secret，仅tmpfs合成SQLite启动，status、嵌入前端root、setup GET均成功；无setup POST、钱包操作或上游调用。容器已移除。导出包和runtime-assets未上传生产或GitHub。

## 5. 实际Compose、迁移、备份与回退

使用**当前已安装** `local-deploy-state.py.compose_args` 只读展开真实Compose，不用旧副本猜测。七个输入（包括local PG03及Executor overlay）的SHA、服务镜像、entrypoint、非秘密开关及挂载目标见 `production-compose-preview.json`；环境只记录名字和指定非秘密布尔值，未保存完整私有配置。

Gateway保留 `/data`、`/app/logs`、只读 `/run/clearapi`、原entrypoint和内部网络。预览仅计划替换Gateway镜像；不移除Executor、PG、卷、历史账务或原overlay。Sub2API独立PG/Redis及应用卷不接入本次Gateway生产Compose，隔离fixture不会自动导入。

732→候选无model schema/迁移或核心Billing/依赖变化；本轮恢复的路由只是内存选项判断，没有新DB操作。生产只读已确认reservation/outbox/receipt/reconciliation/Finance sync表存在，不能冒充完整schema升级测试。未重跑三库矩阵，复用相符历史证据；没有为了取证运行生产迁移工具。

正式backend发布仍由既有helper在部署锁内新鲜核对前驱、启动原 `clearapi-backup.service`，校验新鲜加密备份的当前镜像/状态/密文身份，再切换及健康检查。本轮不创建生产备份；历史包不能代替发布窗口新包。备份须覆盖数据库/独立账本、密钥配置、Compose、部署状态及恢复所需镜像，并保留异地解密材料。

helper对backend切换后失败且迁移状态未知要求人工恢复；本轮没有削弱该边界。禁止盲目回滚钱包DB。切换前失败可恢复原配置；已开始业务或未知usage不得换渠道重派。准备完成不等于生产恢复演练已经重跑。

## 6. 功能完整性及关闭状态

- 当前732已上线的用户/API Key、钱包、充值、账务、普通渠道、商城、兑换码、邀请返奖、旧库存资格补录及定价/节假日功能均由祖先关系保留，不重复补录或改真实价格/余额。
- 客户端39a7b9f82已包含：Codex CLI、Claude Code、OpenCode、Cherry Studio指南及全部语言内容无变化；沿用现有客户端证据。Cherry实机和真实工具能力未新增验收。
- Sub2API多厂商13平台/31档案、禁用规划、type59适配、双服务mock及unknown usage核查/幂等恢复证据完整保留。TypeSafe专用协议缺口与真实厂商未验收仍如历史报告所列，不宣传开放。
- 生产PG03 master缺失按精确true才启用的规则为关闭；`CLEARAPI_POOL_ENABLED=true`不等于master开放。生产Sub2API type59总数0/启用0，非type59启用7。**全部生产账号池继续关闭**，没有把PG03环境flag误当Sub2API总开关。
- 不将源码默认关闭理解为本轮已把生产环境flag改false。原账号/禁用token和渠道、Executor及历史数据保持。本轮不进行任何配置写入。

## 7. 必要验证与证据复用

新执行：四包定向 `go test`，命令与范围见 `evidence/local-release-pg03-retirement-20261030/verification.json`，model、service/accountpool、service、relay/channel/sub2api均exit0。覆盖master fail closed、未发布Gemini/Grok池拒绝、普通渠道继续路由、准入身份/版本及Sub2API相关契约；不是全量测试。

另执行 `relaykit/` 下 `GOWORK=off go build ./...`，exit0；Go容器均断网并使用现有模块/构建缓存。`git diff --check`通过。原21份历史证据逐一核对UTF-8/LF归一SHA，全部匹配；不重跑所有厂商、前端及三库。最终Dockerfile本身已完整编译前端和全部二进制。

## 8. GitHub源码备份安全与分支

目标仅 `a3225865526-wq/Clear-API` 的独立 **`codex/backup-local-release-pg03-retired-20261030`**，不覆盖main，不force push。所有原提交及PG03证据可从保留祖先历史追溯。

远端11个workflow已逐项读取：CI push只匹配main/codex-production-cicd，audit只匹配原专属分支，其余为手动或限定main的workflow_run；本备份分支不会启动构建/部署链。候选workflow触发器亦未变化。仓库hooks API返回空数组。本轮不修改workflow、远端安全规则或自动部署设置。

Gitleaks8.30.1下载包SHA已对照官方checksum。待推送34提交历史扫描原始exit1/38匹配，经分类全部是commit/镜像/文件摘要或隔离secret引用。当前树exit1/330匹配，已核实为摘要、引用、合成夹具、Auth-Version常量、PEM边界字符串、mask示例及公共GPG指纹等；没有发现可用生产凭据，未禁用扫描规则。现有5个1.6–25.2KB证据zip也解包核对：只有日志、patch、manifest、schema及build-info；无数据库内容/INSERT/COPY数据，4个匹配均Go模块h1摘要。无>5MB文件、私钥/OAuth文件、DB文件、新运行时包或私有备份纳入。

这是一轮模式扫描及定向人工语义核对，不保证发现任意未知格式secret。原始含匹配的扫描输出仅保存在仓库外，备份中只保存非敏感结论。

首次普通git fetch因Windows schannel握手失败；单次改用命令级OpenSSL后成功，未关闭TLS验证或改全局git配置。推送结果及准确远端最终SHA以唯一总控和交付报告的“远端完成回执”为准；本报告所在文档提交与应用源码92edfc345身份分别记录。

## 9. 正式部署剩余条件

本地构建、必要回归、身份、断网启动、真实Compose只读预览及原版DryRun均通过。**没有PG03敏感增量或GHCR身份待审批项。** 后续只需明确授权生产部署，在发布窗口重新核实实际前驱/健康/锁和新鲜备份，使用原Windows helper；若前驱漂移或原安全检查失败则停止，不伪造证据。

原helper正式执行会按应用工作区HEAD构建/导出并核对新包；本轮已准备包可审阅，但没有改helper为跳过其构建/校验。重建时间戳可能改变镜像哈希，届时记录实际值，不沿用旧包哈希冒充。

Sub2API真实厂商验证、合法凭据、选定模型/预算及运营Owner安排在启用前另办，不阻塞默认关闭的Gateway候选准备。晚到超额由平台承担、执行终结且可靠usage仍缺失最迟7天逐笔Owner退款的既定政策保留，不新建自动退款/定时任务。

唯一权威总控仍为 `D:/AI/API/worktrees/clearapi-controlled-launch-20261020/CLEARAPI_MASTER_STATUS.md`；本轮最终追加事实且保留全部既有字节。其他worktree同名文件仅历史副本，本轮不另立总控。
