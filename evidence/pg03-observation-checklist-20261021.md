# PG03 生产只读采集与独占观察窗口 — 本地协议及管理员清单

日期：2026-10-21。范围：观测协议、受审批状态迁移、本地合同测试、未来管理员步骤；未连接生产 SSH、执行 helper、读取数据库、安装身份或取得生产事实。

## 状态和复用边界

| 项目 | 状态 | 依据和边界 |
| --- | --- | --- |
| 60 秒稳定基线、两阶段新鲜请求、执行阶段精确状态迁移的本地合同 | PASS | `python -B scripts/ci/test-pg03-observation.py`，22 项 OK、exit 0；本地替身，不是生产验收 |
| 实际生产采集身份、权限、期限、持久防重放及受信生产者 | BLOCKED | 本轮未安装、授权或采集，需要管理员独立审批和现场证据 |
| 实际 schema、受保护数据、待处理业务、配置及受约束 writer | UNKNOWN | 历史快照和旧 helper 不足以证明 |
| 真实全 writer 连续 60 秒独占观察及实际执行状态迁移 | NOT RUN | 没有执行生产采集、入口冻结或部署 |
| 所有报告能否直接授权生产执行 | FAIL | 序列化报告固定 `execution_allowed=false`；只接受存活能力和独立最终授权 |

既有真实隔离 run `39517646061/1` 与固定候选验收直接复用。本增量不修改应用、迁移、Dockerfile 或候选镜像，不触发整套隔离重跑。旧 observer/window 的 69a 和 pin 保留，不宣称支持新候选。旧 helper 不含完整数据库/schema/受保护数据/配置/Executor epoch。R2 A 只提供一次有限采集，不能提供连续窗口，不能扩充 A 的调用预算冒充新批准。

## 因果上可实施的两阶段合同

实现 `scripts/pg03-observation.py` 无 CLI、默认传输、凭据读取或生产部署入口。绑定与最终 request 分两阶段，避免“窗口先绑定尚未生成的观测 hash/qualification”的循环，也不预造未来时间。

1. **稳定意图**：建立 `ObservationWindow(binding, readers, authority, reserve, final_authority)`。binding 为 `clearapi.continuous-observation-binding/v2`，固定仓库、完整 candidate（source、精确 digest 引用、RC run/attempt）、control、`intent_sha256`、`qualification_intent_sha256`、observation_id、identity/permission 指纹、lease_id、executor_id、Unix UTC 秒 issued_at/expires_at、准确 coverage。会话租期大于 60 秒且不超过 900 秒，不预绑定最终 request 或最终 qualification hash。
2. **意图摘要**：`intent_sha256(request)` 只排除未来产生的 lease 时间和 evidence 中 production_observation/startup_write_scope/candidate_gate_bundle；保留 transaction、candidate、control、operations、incident、冻结状态、schema 与历史 backup/restore/rehearsal 引用。`qualification_intent_sha256` 是独立审批的候选/源码/control/RC/隔离证据政策摘要，不是未来最终资格结果。最终排除项仍须在步骤 5 独立认证，不能免检。
3. **受信任采集**：`authority(binding)` 实时认证会话批准、身份、撤销及独占权；`reserve(binding)` 在首次读取前跨进程/重启持久消费 observation_id。失败或失联保留 UNKNOWN，禁止 retry。四个 reader 接口 `(binding, challenge)`，返回 `clearapi.observation-sample/v1`、正确 binding hash/随机 challenge/channel/PASS/complete=true/unknown=[] 和完整 projection。JSON claims、hash、challenge 本身不认证来源；固定只读命令、来源认证、输出上限、绝对超时、取消及进程组回收均由已审查生产 reader 强制实施。
4. **连续观察后取得真实新鲜投影**：调用 start/poll/check(readiness=True)。GitHub 目标周期 15 秒，其他通道 2 秒；周期间隔最多容许 0.25 秒调度偏差。读取/失联 TTL 为 30/5/5/5 秒。最后通道建立完整基线后至少持续 60 秒，并满足最低采样次数。`proof = window.claim(template)` 一次领取活能力，template 的 startup_write_scope_sha256 为 None。其他已知状态必须匹配真实样本；observed_at_utc 由最后 host/database 实际读取完成时间生成，覆盖调用者时间，不使用窗口开始时的旧 timestamp。
5. **新鲜 scope、最终 request 和资格**：先用 proof.projection 除 startup_write_scope_sha256 外的摘要生成 scope v2；`proof.attach_scope(scope)` 校验精确 candidate、配置、baseline_state 和已批准的 scope intent 后填入精确 scope hash。scope intent 排除 observed_at/expires/baseline_state，其余操作、列行限制、审批引用等全部固定。随后取得完整 proof.projection，创建独立不超过 300 秒、且不超过会话期限的最终 request，运行真正的候选资格验证，再 `proof.bind(request, qualification_sha256)` 一次性 seal。`final_authority(binding, request, qualification_sha256, projection)` 必须独立认证最终短租、完整资格、scope 和观测证据，并将它们与预先批准的稳定意图对应；未知/拒绝即永久关闭。任何 lease/证据修改都不能重放已 seal 的 request。
6. **活检查**：`proof.check(request, request_sha256=None)` 重验窗口、最终请求全部 canonical bytes/hash、candidate/control、最终短租和 final_authority。没有传 raw hash 时使用 canonical JSON SHA256，与 successor adapter 相同。返回深拷贝 TX 投影，仍须通过 transaction.validate_observation 的全部字段与年龄检查。单次 proof 本身的 perform 固定拒绝，必须使用下述 ExecutionWindow 才具备阶段执行合同。
7. **证据审计**：report 包含 binding hash、observation_id、UTC 锚、各通道基线、样本数量及 trace hash，固定 execution_allowed=false。window.trace 保留非秘密 challenge/采样时刻/样本摘要、最终 request/qualification 和阶段 policy/nonce。受信宿主须保存完整 binding、报告、trace、生产者身份/退出结果，独立回读/hash 校验并禁止覆盖历史失败。当前未提供生产审计存储；JSON 文件不能重建活能力。

## 全覆盖投影和冻结规则

| 通道 | 完整事实 | 前置期合格状态 |
| --- | --- | --- |
| GitHub | workflow inventory、入口冻结指纹、control、自动开关、全 ref 活动运行和待部署 | control 精确、auto=false、无活动运行/待部署、指纹稳定 |
| host | Runtime、protected_runtime、精确 candidate_identity、配置/工具、Executor ID/epoch、Gateway ID/running、入口/监听、完整 writer 路由及唯一执行者 | Gateway stopped、隔离入口、无公网监听、唯一 Executor、所有入口 frozen |
| database | schema、protected_state、pending_work、获准 scope intent、database identity、unapproved_writers | 所有固定指纹稳定、无未授权 writer |
| authority | 身份/权限指纹、lease、Executor、撤销/连接/锁归属 | 当前未撤销、连接正常、锁归唯一 Executor |

全 writer 集合为 actions_all_refs、dispatch_rerun_approval、upstream_events、ghcr_verify、manual_clearadmin、manual_clearapi_deploy、console_root、external_credentials、systemd_cron_at、other_agents。只冻结一条 CI 或一把 SSH key 不能认定独占。

protected_runtime 摘要覆盖除明确获准 Gateway 变化外的全部 Runtime；protected_state 排除的只有获批 scope 中精确列/行/操作 delta。unapproved_writers 不是盲目空数组：生产 reader 须认证 candidate 连接归属并检查允许的列/行/操作范围，才可排除合法启动 writer；未知连接和越权写入均必须列出并终止。数据库收集不能运行迁移、AutoMigrate、维护 SQL 或写入锁。不能从空对象、常量、历史文件生成摘要冒充现场事实。

## ExecutionWindow：有界获批状态迁移

`ExecutionWindow(proof, trusted_policy)` 只接受已完成 final seal 的活 proof，每个 proof 最多创建一个执行窗。trusted_policy 来自独立可信政策，不能来自部署请求自由填写；固定 schema `clearapi.execution-window-policy/v1`、binding hash、final request hash、唯一 operation_nonce、owner 和五个固定 stage。

stage 顺序是 prepare_runtime、replace_gateway、verify_startup、verify_restart、commit_successor。每阶段固定 before、after、during，三个可变字段为 runtime_sha256、gateway_id、gateway_running。during 显式枚举所有获准中间态，不从新样本自动 rebaseline。其他 GitHub、database、authority 和 host 配置/工具/Executor epoch/全部 writer/protected_runtime/protected_state 全部保持稳定基线。

为实际 Docker create 生成的 CID，policy 可使用 `$successor_gateway_id` 和 `$candidate_gateway_runtime`。新 Gateway CID 只从同 request/name、成功且可信的 replace_gateway result 绑定，并要求 64hex、异于旧 Gateway 与 Executor；绑定后每次现场投影必须匹配。替换期间尚未返回 CID 的中间态仍必须为精确 candidate、Gateway stopped 和同一 protected_runtime；未知镜像/候选不能通过。Runtime 总摘要允许获准 Gateway delta，其他 protected_runtime 精确不变；真实 reader 必须独立核验 candidate 的 source/digest/binary/RC 身份，不能只信自报字段。

prepare_runtime 不可提前替换/启动 Gateway；准备 run 目录/challenge 只可在可信 reader 已按精确批准投影排除的 delta 内，不能把整个目录或所有文件都排除 protected_runtime；replace_gateway 只 create、不 start。verify_startup 才允许启动；后续运行态保持同一新 CID，restart 的新进程/challenge/迁移凭证另外由 TX/host 证明。check(request) 每次取得四通道现场样本；perform(request,name,action,*args) 检查固定次序/唯一 claim、操作前和操作后的精确状态，并启动四个独立只读监视线程覆盖整个 action。UNKNOWN、异常、未列中间态、失联、过期、重放均 sticky fail，不重试 action；containment 不依赖此窗口，由固定 host.contain 处理。

此 Python 合同不能强制中断任意不合作的 trusted callback。生产 host/readers 必须实现绝对 deadline、受控取消与有界子进程回收；窗口发现失效不代表真实 syscall 已停止。所有 monitor 共用最多 5 秒 join 预算，不能收敛的 daemon 视为 UNKNOWN；故障时 action 可能持续到受信 host 的有界 deadline，窗口不能即时撤销已进入的 syscall，必须进入 containment，不能宣称 containment 已成功。生产 transport 和 host 未安装/验收，本地成功不是生产执行验收。

## 管理员按序实施清单（均需另批）

1. 负责人批准仓库/candidate/control/qualification intent、准确列行 scope intent、收集主机、唯一 Executor、字段投影、工具 hash、全部 writer 负责人、入口冻结、期限与审计目的地。观察身份没有部署/备份/恢复权限，部署另批。
2. 管理员独立证明当前账号、SSH key/host-key 指纹、forced command、无 PTY/转发/user-rc/password/其他 key、无 docker group、精确 sudo 无参数边界，以及 helper 属主/模式/ACL/capabilities/父目录和实际字节。GitHub 权限仅当前仓库所需只读范围。不输出 key/Token/Cookie/DSN，不沿用历史审核时间或自报 attested。
3. 完成并独立审查现场四通道 reader、持久 reserve、final_authority、真实范围监视和固定 host 调用。验证拒绝权限、超时取消、失联、重放和审计保存。保持默认关闭；未实现不能将任意 SSH 命令接到回调。
4. 真实停止态备份/独立恢复使用单独批准和独立窗口，保存身份、时间、密文 hash、schema/数据验收；不放进最终 300 秒 successor lease。变化后按协议重建匹配证据，不挪用合成备份。
5. 冻结全部 writer、清空/核对运行与待批准集合，核实唯一 Executor/epoch 和锁。生产公网保持隔离，Gateway 停止；未知入口立即停止，不能临时提升权限试采集。
6. 执行上文两阶段观察顺序，连续 60 秒后以真实最新样本生成新鲜投影和 scope，再签发短租 request、资格与正式执行审批。不得改旧文档时间“刷新”。期间任何权限、状态、epoch、fingerprint 变化都重新审批 observation_id，不能补写 PASS。
7. 只有真实来源、备份恢复、资格、准确 scope、生产身份和正式部署审批全部合格，才可由已审查 adapter 使用活 ExecutionWindow。报告文件、本地测试 adapter 或单个开关均不能放行。
8. 持续保存非秘密时间线、样本摘要、身份/权限、批准引用、所有 hash 对应和真实生产者退出状态。独立回读核验并保留失败记录；观察结束由管理员按批准撤销临时入口、证明失效及进程结束。不能自动解冻 writer、恢复公网或释放未明确批准的生产锁。

## 失败、回退及验证记录

观察失败：停止读、保留已消费 claim 与 UNKNOWN/FAIL，不进入执行、不 retry。未部署时没有数据库回滚动作。失联/过期后保持隔离，管理员核查锁/进程和撤销，新的观察重新批准。执行失败时由固定 containment 停止 successor 并保持隔离；此协议不证明容器已停止、数据库已恢复，也不授权自动 restore。

初次 12 项测试出现默认真实时钟先拒绝 lease 和 malformed evidence=None 的类型错误；修正错误顺序假设并显式拒绝错误类型。后续增加阶段政策、动态 CID 和两阶段因果顺序/最终资格/范围拒绝回归。最新本地命令 `python -B scripts/ci/test-pg03-observation.py` 22 项 OK、exit 0。未运行真实生产采集、备份、恢复、部署或重复既有真实隔离演练。

内部 _collect_projection 的 allowed 谓词只由受信任 ExecutionWindow 选择，外部数据不得提供 callback、选择方法或修改 policy/基线。公开 poll(channel) 只能采稳定基线或当前固定阶段，没有外来 allowed 参数；同进程任意 Python 代码并非安全隔离边界。authority 时钟/认证使用串行锁避免四个 monitor 的更新竞争。
