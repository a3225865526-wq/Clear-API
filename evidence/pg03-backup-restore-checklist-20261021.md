# PG03 真实备份与独立恢复待执行清单 — 2026-10-21

当前结论：**本地离线材料审计器 PASS；真实生产备份/恢复 NOT RUN；来源认证 UNKNOWN；生产恢复资格 BLOCKED。** 本文是管理员的条件执行清单，不是操作授权。本轮未访问生产、读取备份、取得密钥、运行 GPG/Docker/SQL 或恢复数据库。

## 已核查现状

复用 `scripts/production-backup.sh`、`backup-identity.py`、`pg03-backup-state.py` 和 successor adapter 的 restore 合同。现有备份生成 PostgreSQL custom `-Fc` dump、globals、config/legal/edge/secrets/deploy-control、PG03 state archive/snapshot；加密使用既有离线私钥对应公钥指纹，AES256/OCB。身份 helper 绑定实际旧运行镜像及容器 incarnation；successor 停止态模式绑定 failed/exited/restart=no Gateway。备份完成判定必须使用真实进程 exit=0、staging 不存在、密文实际 bytes/hash 和不可覆盖的 completion receipt，不能仅以 manifest 存在判定成功。

既有 manifest 中 postgres/finance/legal 静态 baseline 字段不是新鲜数据库证明。既有脚本只备份 `clearapi` 一个数据库；**如现场为独立日志库，当前备份脚本不满足日志恢复覆盖**，必须先批准并补齐实际日志库 dump/原始来源，不能将主库 dump 重命名或设置 pass 跳过。共享日志布局必须通过全表 catalog 对比证明日志表确实纳入。

`pg03-backup-state.py restore` 只允许不存在的 `/tmp` 或 `/var/tmp` 目标，不覆盖生产高水位和 receipts。不得导入 globals/roles、运行归档中的 helper、恢复 secrets/config 到主机、覆盖 release manifest、将历史授权或 ARMED 状态恢复到 live。

## 新工具接口及明确边界

`python3 scripts/pg03-restore-acceptance.py --evidence-dir "$EVIDENCE" --plan-sha256 "$OUT_OF_BAND_PLAN_SHA256"`

仅在管理员另外批准的离线恢复环境运行。工具无网络、SSH、GPG、Docker、SQL 或提权调用；流式读取备份、不提取、不生成临时明文副本。输入目录须位于管理员控制的离线私有文件系统；不得由不可信并发进程改写。JSON 拒绝重复 key，路径拒绝穿越、链接和特殊归档成员。审计 bundle 实际 hash、全部声明 artifact hash、PG03 archive/snapshot 一致性及恢复后的 snapshot；比较全表行数/内容 hash、schema/index/constraint/sequence/migration 状态；核查精确独立 PG 目标、真实 restore/decrypt/cleanup 子进程记录及输出 hash、时序、有效期、挑战、候选和备份绑定。

成功输出是 **`clearapi.restore-evidence-audit/v1`**，`content_status=PASS` 仅表示材料内部一致，始终 `production_readiness=BLOCKED`、`producer_authentication=UNKNOWN`、`production_execution_allowed=false`。exit0 不是生产验收或部署许可；本工具刻意不生成 `clearapi.successor-restore-acceptance/v1`。伪造全部输入和 plan 仍可构造内部一致材料，所以必须由另行认证的真实 producer 提供命令结果及来源，并在 adapter 验证原始字节的可信来源。外部 plan SHA 只防换包，不认证批准人。

**未实现部分：** 自动解密/真实数据库恢复 runner、从生产取得与 dump 同一停止窗口的完整无秘密数据库指纹 collector、不可伪造的离线 producer 身份/签名发布、管理员授权校验和 successor producer 接入。工具不证明 OpenPGP 数学解密关系，不证明 JSON 中的 subprocess 确实发生，也不凭 Docker inspect JSON 排除未记录连接。不得把本地合成夹具或该工具的 PASS 变为真实 receipt。管理员必须在这些部分经实施及独立审查后再执行下列涉及数据的步骤；缺少任一项继续保持 BLOCKED。

## 逐项批准与停止点

1. **批准人/身份/范围**：负责人另行批准本次唯一管理员、全 writer 停止窗口、准确旧 release identity、目标备份 ID、离线恢复主机、其磁盘和操作身份、离线私钥使用人及公钥指纹引用。记录 UTC 起止、来源和一次性 challenge，计划有效期最长24小时；生产操作另有更短窗口及授权，不因该24小时有效期获准。批准范围仅备份与独立恢复，不包括生产恢复、迁移、部署或开放流量。任何新 writer、连接失联、状态漂移、超时、来源不明或 UNKNOWN 均停止，不自动续期/重试。
2. **生产准备（管理员专属，当前 NOT RUN）**：在覆盖 Gateway、Executor、队列、定时器、手动/CI/console 等所有 writer 的独占窗口，核验停止态准确容器、restart=no、identity 与读源一致。取得全表 catalog、schema/index/constraint/sequence/migration、业务关键行的无秘密指纹（只存 hash 和行数）及实际日志布局。指纹必须与 dump 同一停止窗口相联系，不能用历史静态 schema 或健康检查代替。不得在一般只读授权下自行停止 writer。
3. **真实备份（管理员专属，当前 NOT RUN）**：使用已审查、已固定 hash 的现有脚本/身份 helper。successor 特殊停止模式要求 `CLEARAPI_BACKUP_STOPPED_GATEWAY=<准确完整64位CID>` 和 `CLEARAPI_BACKUP_PRESERVE_EXISTING=true`；只在管理员单独授权该调用后使用。持有部署独占窗口并证明 backup lock 无竞争；记录实际进程开始/结束/exit，调用 `backup-identity.successor_completion(...)` 检查真实完成与 staging 清理。exit75、非零、丢失返回码、时间/身份变化、缺失 PG03/日志或 cleanup 不完整即无有效恢复点，保留现场和失败证据，不删除历史备份。
4. **转移与密钥（管理员专属）**：仅将 manifest、完成 receipt 和加密文件转移到已批准离线环境，比较源/目的完整 ciphertext SHA256 与 bytes。私钥仅在已有获准密钥环境使用，不复制到生产、证据目录、argv、日志或仓库；归档 manifest 仅记公钥指纹。使用禁止 shell trace 的私有终端、umask077、私有 tmpfs，stdout/stderr 不回传原始数据。真实备份的创建者、主机身份、backup ID、UTC dump interval、生产旧 release identity、manifest/completion hash 必须由可信 producer 的外部 seal 绑定。
5. **解密与安全预检**：先验证密文和已固定 manifest，再用下述固定 GPG argv 解密。记录真实 exit0 和 status-fd 的 DECRYPTION_OKAY，保留无秘密状态及输出 hash；错误/缺失完整性结果立即停止。比较 plaintext bundle SHA256 与 manifest。在任何提取前调用新工具的 `bundle_inventory(evidence_dir, pinned_manifest)`，它验证嵌套 tar 成员类型/路径/重复/大小和全部 payload hash；不得用未经检查的 `tar -x`。预检之后仅由已审查 runner 复制 database dump 到独立容器 tmpfs，并在私有临时目录提取 PG03 archive/snapshot；不导出 secrets/globals/config。
6. **独立恢复**：下方命令模板仅由管理员在离线主机执行，源镜像须预先离线取得并绑定既有精确 digest。容器 CID 必须和所有生产 CID 不同，network=none、无 host mounts、公网端口、生产 DSN、生产账号和私钥。目标数据库名必须 `clearapi_restore_<唯一后缀>`。只允许本地 postgres socket 用户，不从 globals 导入生产角色。`pg_restore --exit-on-error` 实际退出必须0，不能只运行 `pg_restore -l`、仅看 stderr 无错误或恢复健康。
7. **恢复验收**：在候选迁移前，对独立恢复库重采同一种完整无秘密 inventory，逐表行数/hash 与源一致；catalog 表集合、schema、全部索引定义与有效性、约束定义/有效性、唯一性违规数0、序列状态及 migration/options marker hash 均一致。关键金融/任务/Pool保护字段必须纳入完整行 hash，不能抽样替代全量。独立日志库还要求独立 dump 和 restore，主/日志都比较；共享日志须列日志表覆盖。PG03 使用既有 restore 工具恢复到不存在的 `/var/tmp/clearapi-pg03-restore-<challenge>`，检查全部文件 hash/mode/uid/gid/mtime、guard_version 和高水位；不运行 Executor 或将其复制回 live。
8. **候选迁移单独验收**：真实数据恢复前后比较通过后，若下一操作需要候选启动迁移，应在独立副本应用准确候选，固定源码/RC/digest，采集专用迁移 receipt、目标 schema、关键数据及重复启动后的保留性。原始 restore inventory 不能被迁移后的状态覆盖。已有 schema-only 隔离演练直接复用，不重跑；它不覆盖真实数据恢复证明。失败时停止候选，不自动恢复生产数据库或回退 Pool 高水位。
9. **清理/留证**：仅清理本次创建且 CID/label/时序一致的离线容器，保留成功和失败的不可覆盖、无秘密原始 receipt、输出 hash、Docker inspect 投影、完整 inventory 和生产来源 seal。验证 owned 容器最终为空，私有明文临时卷按已批准生命周期销毁；禁止删除历史 ciphertext/失败证据。未完成清理则真实恢复不能PASS。任何清理不确定即停止新操作、报告管理员，不自动广泛 prune。
10. **接入与批准**：可信 producer 将完整原始材料和真实 subprocess 结果封存；独立审查后由 adapter 同时校验 producer、备份完成 receipt、candidate、observation/window、scope 与恢复内容，才可形成现有 successor restore acceptance。再次检查新鲜度、撤销和版本漂移，需重新申请一次性生产部署批准。未经该批准，任何内容 PASS 均不得进入生产执行。

## 离线命令模板（未执行）

这些命令假设前述授权、真实 producer、数据来源验证、安全预检和私有文件系统均已完成。`RESTORE_CID` 必须来自本次 `docker create` 的原始真实返回值并与 plan、inspect 相同。命令由 runner 捕获准确 argv、UTC 区间、实际退出和输出 hash；手写 exit0 不合格。

```bash
umask 077
# 离线密钥环境；工作目录为批准的私有tmpfs；不通过参数给出私钥
gpg --batch --no-tty --status-fd=1 --output "$BUNDLE_NAME" --decrypt "$ENCRYPTED_NAME"

# PG镜像须已离线导入；无自动pull，无host mounts，无生产DSN
docker create --pull=never --name "$UNIQUE_RESTORE_NAME" --network none \
  --tmpfs /var/lib/postgresql/data:rw,noexec,nosuid,nodev,mode=0700 \
  --tmpfs /restore:rw,noexec,nosuid,nodev,mode=0700 \
  -e POSTGRES_HOST_AUTH_METHOD=trust \
  postgres:17.6-alpine@sha256:ef257d85f76e48da1c64832459b59fcaba1a4dac97bf5d7450c77753542eee94
# 记录CID；启动/就绪/版本/inspect由可信runner核验，任一异常停止
docker start "$RESTORE_CID"
docker exec "$RESTORE_CID" pg_isready -U postgres
docker exec "$RESTORE_CID" psql -X -U postgres -tAc 'SHOW server_version_num'
# 仅复制安全预检之后的准确dump；不得装载globals/secrets或生产角色
docker cp "$VALIDATED_DUMP_PATH" "$RESTORE_CID:/restore/$DUMP_NAME"
docker exec "$RESTORE_CID" createdb -U postgres "$TARGET_DATABASE"
docker exec "$RESTORE_CID" pg_restore --exit-on-error --no-owner --no-acl \
  -U postgres "--dbname=$TARGET_DATABASE" "/restore/$DUMP_NAME"
# 此处完成真实inventory采集、比较、PG03独立目录恢复及持久化receipt
# 仅在本次CID/label仍相符且获准清理后运行
docker rm -f "$RESTORE_CID"
```

trust 仅限该 network=none、无外部连接和生产凭据的一次性离线 socket 环境。若不能证明隔离则不启动。需独立日志库时按新的唯一数据库名和其真实 dump 执行第二次 restore，不可复用主库结果。

## 输入格式与证据保留

审计目录固定 JSON：`restore-plan.json`、`backup-manifest.json`、`backup-completion.json`、`restore-run.json`、`source-inventory.json`、`restored-inventory.json`、`docker-inspect.json`、`pg03-restored-inventory.json`；另含 manifest 指定的 ciphertext/bundle 与每个命令 stdout/stderr 文件。plan 通过 `evidence_sha256` 固定后7份 JSON 的实际 hash，并由外部 seal 固定 plan SHA。私钥或明文数据不进入 JSON。严格 JSON 示例/字段以 `scripts/ci/test-pg03-restore-acceptance.py` 为格式夹具，**其数据全部合成，不能用作生产证据**。

plan schema 为 `clearapi.offline-restore-plan/v1`，包含准确 repository、candidate（复用 successor 的7字段）、issued/expires/challenge、backup ID、stopped_writer_identity、公钥 fingerprint、main/log layout 和每库的独立 target_database/dump_artifact、完整非生产 target CID/生产 CID 集合、固定 PG image ref 及 JSON pins。run schema 为 `clearapi.offline-restore-run/v1`；命令记录必须有 argv、开始/结束 UTC、integer returncode 和 stdout/stderr 文件/hash，含 decrypt→restore_main[/restore_log]→cleanup 顺序。source/restored inventory 按完整数据库/表记录 rows/rows_sha256 和 schema/index/constraint/sequence/migration hash、有效约束数、未验证约束数0和唯一性违规0。

当前 successor stopped备份的 running identity 是**旧恢复点**，candidate 是**拟部署镜像**；二者不同是正常语义，禁止把备份字段重写成拟部署candidate。源指纹、真实 dump、manifest 和完成 receipt 之间的可信绑定须由 producer 提供，工具无法从旧清单补造。

## 本轮验证与停止状态

- `python scripts/ci/test-pg03-restore-acceptance.py`：14 tests OK / exit0。
- `wsl -u root --cd /mnt/d/AI/API/worktrees/clearapi-controlled-launch-20261020 python3 scripts/ci/test-pg03-restore-acceptance.py`：14 tests OK / exit0；WSL有既有localhost代理提示，不影响无网络测试。
- 覆盖内容正常但来源UNKNOWN、过期/挑战重放/候选漂移、哈希变化、数据/迁移/索引/sequence差异、约束/唯一性异常、失败/UNKNOWN/布尔退出、生产/联网目标、独立日志缺dump、PG03变化、归档穿越/链接/重复、shape-only pass、密文/内部payload篡改、假restore argv、解密失败和材料缺失。
- 第一次Windows运行因 `st_ctime_ns` handle/path语义不一致失败；修复为POSIX保留ctime检查，Windows比较dev/inode/size/mtime并固定内容hash。已在Windows与Linux分别通过，不把初次失败隐藏为成功。
- 真实生产备份、GPG解密、真实恢复、数据指纹采集、producer认证、production adapter接入均 **NOT RUN / BLOCKED**。本工具不修改数据库行为或备份格式，不触及应用候选，因此不要求为本改动重新构建应用RC或重跑已有真实隔离演练。


## 已实现的独立恢复 runner（未授权、未真实运行）

`scripts/pg03-offline-restore.py` 现在提供固定本地 Docker 动作。默认 `plan` 无子进程；`execute` 不读取私钥，也不解密，管理员先在获准离线环境完成 GPG 和认证 decryption receipt。恢复签发在真实备份完成和实际 input hash 齐备后进行，**不得为了工具约束倒填签发时间**；审计接受后发恢复批准并固定拒绝超过24小时的备份，实际生产准入仍要更严格的同停止窗口状态一致证明。

执行前必须独立安装和批准：`/etc/clearapi/offline-restore-policy.json`（root私有、外部hash），`/etc/clearapi/offline-restore.pub`（只含公钥），固定 Docker/OpenSSL/runner/两依赖脚本 SHA256、准确 machine-id 摘要和专用空 offline daemon ID。输入固定 `/var/tmp/clearapi-offline-inputs/<authorization_id>`，结果固定 `/var/tmp/clearapi-offline-results/<authorization_id>`，均root私有tmpfs；claim目录 `/var/lib/clearapi-offline-restore/claims` 必须持久、root私有。不得在生产主机安装/调用。管理员签名来自原已获准的签名系统；本任务不创建、读取或导出签名私钥。

先制备 `clearapi.offline-restore-execute-spec/v1`：32hex authorization_id、64hex challenge、独立UTC签发/最长7200秒期限、准确candidate、backup_id/stopped_writer_identity、公钥fingerprint、PG17.6固定镜像、生产容器排除清单、main/log目标与layout，以及4份已存在输入的SHA256（backup-manifest.json、backup-completion.json、source-inventory.json、predecryption-run.json）。输入格式见唯一合成测试夹具；填入真实结果，不能使用夹具值。执行spec不包含未来CID或结果hash；runner完成后生成后验audit plan，避免循环绑定。

`clearapi.offline-restore-authorization/v1` 需管理员签名，固定 approved_by=a3225865526-wq、action=independent_offline_restore_only、authorization_id、精确spec_sha256、issued_at_utc/expires_at_utc（不超spec）。policy固定 schema=clearapi.offline-restore-policy/v1、environment=isolated_offline_restore、public_key_file/public_key_sha256、machine_id_sha256、docker_binary_sha256、openssl_binary_sha256、runner_sha256、module_sha256（pg03-restore-acceptance.py与pg03-backup-state.py）、offline_daemon_id。没有这组实际已审查事实时，停于plan。

```bash
# 全部变量仅引用获准离线文件或公开hash；不放私钥、DSN或凭据
python3 pg03-offline-restore.py plan --spec "$SPEC_FILE" --spec-sha256 "$SPEC_SHA256"
# 仅由另行获准的管理员在上述专用离线主机执行
python3 pg03-offline-restore.py execute --spec "$SPEC_FILE" --spec-sha256 "$SPEC_SHA256" \
  --policy /etc/clearapi/offline-restore-policy.json --policy-sha256 "$POLICY_SHA256" \
  --authorization "$AUTH_FILE" --authorization-sha256 "$AUTH_SHA256" --signature "$SIGNATURE_FILE"
```

runner每次命令核对lease/撤销/daemon身份；stdin非阻塞、输出有界、绝对超时和进程组收敛。先持久消费claim，再安全bundle预检和准确dump复制，只启动network=none/tmpfs/无host mounts/无生产DSN的新PG17.6。采完整行、schema/index/constraint/sequence（含is_called）和迁移指纹；唯一性REINDEX只针对独立恢复库，**不能在生产只读源采集时调用该写入步骤**。生产源只能使用等价只读投影，在原获准停止窗口中由认证producer取得。其余配置/Secrets/roles档只审计，未装入主机或PG；不声称已完整恢复业务环境。

正常完成保存真实命令结果/日志hash、PG03独立恢复和不可覆盖后验audit。失败保留FAIL与真实owned CID/label；租约过期或身份UNKNOWN时cleanup=BLOCKED，可能仍有独立容器/tmpfs，管理员需另批精确CID清理。禁止把BLOCKED清理记PASS或删除旧证据。所有材料需获准seal/export回读后再关闭tmpfs环境；内容PASS仍生产BLOCKED/来源UNKNOWN，不能直接形成successor授权。

最新本地验证：WSL Linux/root `python3 -B scripts/ci/test-pg03-restore-acceptance.py` **26 tests，25 PASS，1 opt-in真实PG skip，exit0**。新增真实PG定向测试只允许GitHub hosted本仓库/root，合成main/log、两个networknone容器，真实custom dump/restore与完整指纹对比；由本轮CI单独执行，结果以最终待批包为准。本机无Docker CLI，未运行真实PG，不冒充真实生产备份恢复验收。
