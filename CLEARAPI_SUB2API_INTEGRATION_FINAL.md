# CLEARAPI — Sub2API 隔离集成交付

日期：2026-10-30，Asia/Shanghai。

**结论：隔离环境及可独立执行的代码验证已完成；完整双服务集成验收 BLOCKED / UNVERIFIED，当前不能将 Sub2API 作为已验收功能纳入下一批正式启用。** 负责人确认条款前，账号、内部组、内部 Key 的管理 API 仍受 423 门禁保护。本轮没有接受条款、伪造确认、真实厂商调用、生产访问、推送、合并或部署。

## 1. 版本、工作区与交付范围

| 项目 | 实际状态 |
|---|---|
| 新工作区 | `D:/AI/API/worktrees/clearapi-sub2api-integration-20261030` |
| 分支 | `codex/sub2api-isolated-integration-20261030` |
| 基线 | `ed461d35ec8087c53265fdadb85617d5b44fae91`，原选型分支干净；保留其客户端和 PG03 候选成果 |
| 实现与测试 Commit | `871bdf75ba22d6482c7d7633be4a8360ca0a4db6`，仅本地 |
| 固定 Sub2API | `D:/AI/API/references/sub2api`，`d851a38477597157e78450d30af2d8504764d085`，构建后仍干净 |
| 官方构建产物 | Sub2API `0.2.19`，Go `1.27.3`，原 Dockerfile 构建前端并 `-tags embed` |
| 本地镜像 | `clearapi/sub2api-isolated:d851a384`，镜像 ID `sha256:bd3c64a8b3cf74d3ddd19e25d10ba4a7888f3fe69ce2a250500c358ed370220a`，46,640,454 bytes |

开始时盘点全部已注册 worktree 的真实 HEAD、分支和 dirty 状态，见 `evidence/sub2api-integration-20261030/workspace-inventory.json`。原主工作区 `f30b2473` 大量未提交成果原样保留；应用聊天快照中原选型任务 idle，相关 PG03 任务 notLoaded。这不是全机互斥锁证明。没有采用旧 main 替代当前生产；最近生产版本仅引用既有 `732a2d51` 记录，本轮没有读取新鲜生产状态。

已读并复用四份指定成果。两份客户端报告实际在独立 `clearapi-client-config-integration-20261030` 工作区；其三 CLI 配置解析/418 模拟请求证据与 Cherry 实机 UNVERIFIED 结论保持，不冒充本轮推理成功证据。PG03 历史真实调用和账务证据保留，不把它计入 Sub2API 通过项。

改动仅为：`compose.sub2api-isolated.yml`、`scripts/sub2api-isolated-verify.py`、扩展现有 `relay/channel/sub2api/adaptor_test.go` 和 `service/reserve_hardening_integration_test.go`，以及本报告、证据和同名总控历史追加。没有修改应用生产 Go 逻辑、Billing 系统、数据库模型/迁移、依赖、前端、PG03 调度或普通渠道。

## 2. 隔离环境实际验收

专用 Compose 项目为 `clearapi-sub2api-isolated-20261030`；专用 bridge 网络只包含本次三个容器。Sub2API 仅发布 `127.0.0.1:18882 → 8080`；PostgreSQL、Redis 均无宿主端口。没有连接既有生产/本机业务数据库，没有复制生产凭据。

| 验收项 | 观察结果 |
|---|---|
| 健康、HTML、前端 JS | 首次检查、整栈 restart、force-recreate 三阶段均 200；前端 JS SHA 保持一致 |
| 管理员正常登录 | 三阶段均 200，使用新生成的本地密码；token 仅在验证进程内存中 |
| 数据库 | PostgreSQL **17.11**；新专用 DB/角色 `sub2test`，管理员 ID/邮箱跨重启和重建保留 |
| Redis | **7.4.11**；独立卷、AOF；无敏感内容的哨兵跨重启/重建保留后已删除 |
| 应用卷 | `/app/data` 中配置、安装标记等 hash 跨重启/重建相同 |
| 备份客户端 | 正式运行层的 `pg_dump 17.11` 与 PG server 匹配 |
| 管理门禁 | accounts/groups/users 均 423 `ADMIN_COMPLIANCE_ACK_REQUIRED` |
| 正式确认状态 | `GET /api/v1/admin/compliance`：required=true，version=`v2026.06.10`，无 acknowledgement |
| 未认证协议入口 | Chat、Responses、Messages 均 JSON 401；这仅证明路由/鉴权拒绝 |
| 空池证明 | 隔离只读 SQL：accounts=0、api_keys=0、usage_logs=0 |

准确证据：`isolation.json`、`runtime-boundary.json`、`source-manifest.json`。容器收尾时保持 healthy，便于负责人在本机访问后台；只占用本地隔离资源。原选型实验的卷和全部其他容器未更改。

发现并修复的是**本轮隔离配置问题**：最初 `internal:true` 网络在当前 Docker Desktop 中没有形成可用的宿主回环发布，改为专用 bridge + 明确 loopback 发布；首次整栈 restart 时应用先于 PG 就绪退出，补回上游 Compose 已使用的 `restart: unless-stopped`，最终 restart/recreate 均通过。验证脚本对启动期间的连接关闭作有界健康等待。没有修改上游程序、关闭 TLS 验证或削弱 423。

## 3. type=59 协议与实际完成的链路

继续复用 Sub2API → newapi → OpenAI/Claude/Gemini 的现有适配器；没有第二套 Gateway。现有 `streamSupportedChannels` 已包含 type=59。

**已通过的最完整本轮链路是测试进程内的 ClearAPI 正式适配器/结算代码 → HTTP mock → 隔离 SQLite 持久钱包和消费日志。不存在 ClearAPI → 实际 Sub2API → 上游的完整成功调用。**

新增六组 HTTP 契约用例覆盖 Chat、Responses、Messages 各自 JSON/SSE，使用对应 DTO 和现有转换方法。核对原生路径及 Messages `?beta=true`、`model_mapping`、原名与上游名分离、input/output/cache Usage、SSE terminal 事件。上游只收到渠道专用内部 Key，未收到 ClearAPI 用户 Key、Cookie 或无关厂商 Key；Messages 使用内部 `x-api-key` 和现有 anthropic-version 默认值。

会话头通过现有显式 override 验证，例如 `{"Session_id":"{client_header:Session_id}"}`，不建议开放通配透传或配置用户 Authorization 覆盖。标准 `prompt_cache_key`、Messages metadata 等沿用 DTO。Sub2API 的 session hash 按内部 API Key 隔离；共享内部 Key 并不代表它知道 ClearAPI 每个终端用户身份。真实多轮亲和、租约/账号持续性、跨租户会话语义仍未验，不承诺 PG03 receipt 同等语义。

ClearAPI 的用户组/Token 权限、渠道 abilities 和独立普通渠道选择代码未改。实际渠道选择与 Sub2API 内部账号组的跨服务隔离仍 UNVERIFIED；不能将手工构造适配器的测试称为鉴权/路由中间件全链路通过。

## 4. Usage 与恰好一次计费

运行配置明确为 `RUN_MODE=simple`、`SIMPLE_MODE_KEY_RATE_LIMIT_ENABLED=false`。固定源码中的该分支保留身份检查，写内部 Usage 后返回，不执行终端余额/订阅/账号/平台扣款；本轮也运行了其 simple 模式金融副作用与去重契约测试。内部运营成本字段不等于 ClearAPI 用户钱包扣款。

新增持久钱包 mock 用例的精确结果：一次 HTTP 派发，input=1000、output=20、cache_read=200；沿用既有测试表达式，最终 **9 units**。用户余额和 Token 余额均从10000变9991；用户 UsedQuota=9、RequestCount=1、渠道 UsedQuota=9；reservation=settled/applied9；按 request_id 查询恰好一条消费日志、quota9。随后重复调用该 BillingSession 的 Settle、Refund 和 completion recovery，没有再次改变余额。这证明此测试范围内一致，不证明独立业务重放、并发多进程或两服务端到端恰好一次。

| 场景 | 本轮证据与边界 |
|---|---|
| 正常 input/output/cache | type59 六组响应契约 PASS；新增 Chat mock 的钱包/日志精确对账 PASS |
| SSE terminal Usage | Chat `[DONE]`、Responses completed、Messages stop 的适配器契约 PASS；真实双服务 UNVERIFIED |
| 缺 Usage / 不确定完成 | 现有估算、缺 terminal、保留 reservation/review marker 的聚焦测试 PASS；不得把估算当上游精确值 |
| 失败/退款 | 现有预扣不足、存储失败、重复退款与成功后退款拒绝测试 PASS；真实双服务故障 UNVERIFIED |
| 客户端取消 | ClearAPI 共用传输取消、Claude 停止写测试 PASS；上轮固定 Sub2API drain/保留 usage 单测原证据有效，未重跑 |
| 重试与重复最终结算 | 现有 dispatch fence、已输出后不重试、429/skip-retry/channel pin 测试 PASS；新增同 BillingSession 重复结算不重复扣款 PASS |
| Gateway消费日志 | 新增 Chat mock 用例一条日志，与用户/Token/渠道/持久预留账一致；SSE双服务日志 UNVERIFIED |

**取消存在尚未闭环的跨服务差异**：ClearAPI 取消对 Sub2API 的连接，不代表 Sub2API 立即硬取消厂商任务；它可能继续 drain 并在自己数据库记录终态 Usage，而 Gateway 已拿不到后续终态。当前没有 Usage 回传/对账补偿的双服务证据，未新造一套 Billing 或假定已能精确结算。

**重试不能推断外部派发恰好一次**：Sub2API 本身存在失败切换/同账号重试；OpenAI handler 默认 maxAccountSwitches=3，配置值仅大于0时覆盖，不能把设置0当成禁用证明。Gateway 钱包幂等不等价于厂商只派发一次。解除门禁后的 mock 必须记录实际 attempt、完成/未知、两层 retry 及最终 Usage；本轮没有关闭现有生产安全控制。

## 5. simple 模式的组隔离限制

这是本轮新增核实、会影响接入配置的真实限制：`scheduler_snapshot_service.go:1476` 将 simple 模式的 groupID 归零，按 platform 读取可调度账号；现有 `TestGroupIsolation_SimpleMode_*` 两项也明确验证该模式可调度跨组/未分组账号。

因此**不能声称多个内部 Key 绑定不同组就形成独立账号集合**。可沿用 simple 的前提是该实例仅服务 ClearAPI 单一信任域，同平台账号允许统一调度；平台组路由和 Key 身份约束仍需保留。若确有同平台账号必须硬隔离的业务要求，需要使用独立实例/凭据/卷/网络等真实边界，或另行验证上游支持的配置；本轮不改写 scheduler，也不退回会重复计费的配置。ClearAPI 终端组/渠道限制继续由 ClearAPI 执行。

## 6. 运营能力及性能

复用原后台已经存在的账号创建/编辑、启停/批量、额度/健康/错误、并发调度、Usage、模型关系、代理与刷新功能；源码依据沿用固定版本选型报告及其 source manifest，没有另造 React/Vue 运营页。OpenAI OAuth/API Key、Anthropic OAuth/setup-token/API Key、Gemini OAuth/API Key、Grok OAuth/API Key、Kimi API Key/Coding Plan 的区别保留，不能把 Kimi Key 当设备 OAuth。

后台 HTML/JS 已从正式镜像实际返回，但账号添加、实际 OAuth/API Key 接入、批量操作、真实额度/刷新、Usage运营查询均因423未验。页面资源可用不代表所有管理功能已通过浏览器验收。

本次空闲采样：Sub2API CPU0.79% / 42.37 MiB；PG CPU1.14% / 52.91 MiB；Redis CPU0.44% / 6.281 MiB。Docker Desktop 可见总内存15.37 GiB、有其他容器并行；只是瞬时空闲值，不作容量结论。完整转发被门禁阻断，延迟、TTFB、请求资源、PG03同条件对比均 **NOT MEASURED**。

## 7. 重现、未来统一部署及回滚准备

固定源码构建和本轮实际运行命令：

```powershell
# 从固定 d851a384 源码；使用未修改的上游正式 Dockerfile。
docker build --build-arg COMMIT=d851a38477597157e78450d30af2d8504764d085 --build-arg POSTGRES_IMAGE=postgres:17-alpine -t clearapi/sub2api-isolated:d851a384 -f D:/AI/API/references/sub2api/Dockerfile D:/AI/API/references/sub2api

Set-Location D:/AI/API/worktrees/clearapi-sub2api-integration-20261030
python scripts/sub2api-isolated-verify.py --runtime D:/AI/API/runtime/sub2api-integration-20261030
```

首次生成的新随机测试密码/secret 仅在仓库外 runtime/test.env，验证脚本不调用 accept，不输出登录 token。重跑脚本会对该固定本地项目 restart/recreate，不要改成生产项目。当前三个服务保持 healthy。停止/恢复仅影响这组服务：

```powershell
docker compose -p clearapi-sub2api-isolated-20261030 --env-file D:/AI/API/runtime/sub2api-integration-20261030/test.env -f compose.sub2api-isolated.yml stop
docker compose -p clearapi-sub2api-isolated-20261030 --env-file D:/AI/API/runtime/sub2api-integration-20261030/test.env -f compose.sub2api-isolated.yml up -d
```

未来统一部署只需沿用原正式镜像 + 独立 PG/Redis + 私有网络；数据库和Redis不发布公网，管理入口保留回环/受控管理入口及适当 TLS。将 Gateway 加入明确的池内部网络，内部 URL=`http://sub2api:8080`，不追加 `/v1`。type=59 填专用内部 Key、实际可用模型和必要 model_mapping，先限定测试渠道/组，保持普通渠道独立。避免不同 Sub2API 实例在同一网络竞争 `sub2api` DNS alias；不得把该渠道再绑定 PG03 账号池或让同一账号双调度。

最小环境变量及数据依赖已在 Compose 中列出；正式环境另行生成 secret，不复用本地密码。持久化/备份范围：PG完整库及角色/schema，应用 `/app/data` 配置及加密材料，Redis持久化和亲和状态；账号凭据及 JWT/TOTP 加密材料按秘密备份处理。pg_dump与PG版本配套，备份恢复演练尚未执行，不能声称已有灾备验收。

池故障时，按既有渠道重试/错误策略处理；回滚优先禁用或移出新增 type59 池渠道，再确认普通渠道仍可选。可单独 stop Sub2API 栈，保留其卷、ClearAPI余额和历史消费日志，不回滚钱包、不删除账本。未知已派发请求不应通过业务重发制造第二次上游执行。本轮未改生产 Compose 或开发新部署系统；美国 VPS 购买、迁移和域名切换均未执行。

## 8. 验证清单与未运行项

完整命令、镜像/挂载、版本、日志 hash、失败尝试及结果见 `evidence/sub2api-integration-20261030/verification.json`。

- 上游正式 Docker build：PASS，含前端；源码干净。
- 隔离 verify 脚本：PASS（环境），管理 gate 保持 BLOCKED；restart/recreate/persistence/ports 已实测。
- 新增 type59 协议：1个顶层测试、6个子场景 PASS；新增 wallet/mock：1项 PASS。
- ClearAPI现有 billing/Usage/retry：11个顶层聚焦测试 PASS；错误/取消：10个顶层聚焦测试 PASS。
- Sub2API simple/分组语义/模型记录：8个顶层聚焦测试 PASS。
- 修改的 Go 文件 gofmt、Python AST、`git diff --check`：PASS。

没有改变生产数据库行为，未触发 ClearAPI SQLite/MySQL/PostgreSQL 强制矩阵；本轮钱包测试使用真实临时 SQLite，不将其称为三库验收。未改 relaykit 或其公共 API，无需重复独立 build。未改前端，不重复客户端分支已有验证。没有完整 CI、真实厂商/客户端调用、双服务成功/SSE/取消/账务、请求性能、备份恢复或生产验收。

认证边界核对参考 [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)、[ASVS 5.0.0](https://owasp.org/www-project-application-security-verification-standard/)。适用检查为服务器认证与管理员门禁、不同用途凭据分离、不向上游传用户Key/Cookie、不记录可用token、受控传输；本轮未改登录/会话算法，也未进行整个产品的ASVS合规认证。应用本地HTTP仅供回环/隔离测试，不能作为公网明文登录配置。

## 9. 423 官方操作与负责人最少事项

1. 在需要继续验证时访问本机 `http://127.0.0.1:18882/login`，使用测试管理员 `sub2-test@example.invalid`；密码在仓库外 `D:/AI/API/runtime/sub2api-integration-20261030/test.env`，不要复制到 Git/聊天。本人完整阅读固定版本 [中文承诺](https://github.com/Wei-Shaw/sub2api/blob/d851a38477597157e78450d30af2d8504764d085/docs/legal/admin-compliance.zh.md)或[英文承诺](https://github.com/Wei-Shaw/sub2api/blob/d851a38477597157e78450d30af2d8504764d085/docs/legal/admin-compliance.en.md)，自行判断是否能够承担其中的上游授权、运营、数据和其他义务。这里是官方操作说明，不代替法律判断或宣称商业用途已获上游授权。
2. 如本人有权且决定同意，按原强制弹窗要求逐字输入显示的确认短语并自行点击确认。原 UI 调用 `POST /api/v1/admin/compliance/accept`，保存当前版本、管理员ID、时间、IP/UA；确认按管理员分别记录。AI未调用该接口，也未写确认数据。之后用官方 `GET /api/v1/admin/compliance` 检查 required=false/对应正式记录，再访问 accounts。不要通过SQL、旧版本或改程序处理423。
3. 提供一个获授权、与PG03不重叠的隔离测试账号/受控mock接入，并通过原后台创建所需平台路由和专用内部Key；确认 simple 实例内同平台账号允许共享调度。真实供应商、OAuth授权和秘密凭据仅在确需验证时由负责人通过原流程处理，不要求付费调用或生产权限。

之后仍应由集成工作完成的内容：实际 ClearAPI→Sub2API→mock 三协议成功/SSE、失败/重试/取消、两库Usage和Gateway账务对账；发现取消终态Usage缺口时再决定最小修复，并记录实际attempt。它们是技术未验项，不是要求负责人手工测试或新增审批。

**验收判断**：环境 PASS、Gateway mock及独立契约 PASS、完整隔离集成未达到标准。Sub2API现阶段可保留为下一批候选资产，不能随批次正式启用或宣传端到端已验。客户端配置指南原有独立候选资格不受此阻塞。总控只在本隔离分支已有同名 `CLEARAPI_MASTER_STATUS.md` 追加历史与本轮记录，不新建第二个总控文件、不修改原主工作区；随统一集成进入唯一总控，不把本支线提升为新的生产状态来源。
