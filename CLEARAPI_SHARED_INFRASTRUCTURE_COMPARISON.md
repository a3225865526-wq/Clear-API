# 共享基础设施实测与决策

交付时间：2026-10-31T23:59:03+08:00。仅 Windows 本地隔离实验；实验分支 `codex/shared-infrastructure-optimization-20261031`。应用源码 `fa96a74df2b46c0f3617068e0f77fbdc2fae67fe`，离线操作配置源码 `dd68a49afb63fc66433f5b54b2e4eb4a8ada4ef4`。源码基线和原 release-v2 均保留；未推送 GitHub、未操作生产、未修改 DNS、未发送真实收费请求。

## 实际基线

六服务实际为 ClearAPI、Sub2API、ClearAPI PG 17.6、Sub2API PG 17.11、Redis 7.4.11、Nginx。仅一 Redis、一反向代理，无可减少的重复实例。两个 PostgreSQL 各自独立 bind 数据目录、用户和数据库；扩展分别 plpgsql、plpgsql + pg_trgm。应用通过私有网络 TCP 连接各自库；每应用连接池 open=10、idle=2，各 PG max_connections=20、shared_buffers=32MB、work_mem=1MB、maintenance_work_mem=16MB、max_wal_size=256MB。WAL 与持久数据保留，逻辑 PG dump/全局角色及加密快照可恢复。

Redis 仅供 Sub2API 使用，DB0、密码认证、无新增 ACL 共租；ClearAPI 不接 Redis。单实例 96MiB 限额，maxmemory=48MiB、noeviction、AOF everysec、连接池 16/idle2。未清除调度/会话/锁来降低内存，RDB→AOF 与 TTL 恢复已测。Redis DB 编号未被称为安全隔离。

Nginx 已为唯一入口；TLS 管理域名继续复用它。内部数据库网隔离；公开映射仅 loopback。Docker local 日志轮转、健康依赖、独立应用生命周期保持。

## A/B 实际比较

同一隔离父级 2 CPU、1536MiB（禁止 swap）、50GB ext4，独立合成数据库。A 为六服务；B 为五服务、一个 PG17.11 + 两个独立数据库/非超级用户，max_connections=32（应用合计20、维护12），384MiB 限额。B 由停写快照恢复至自己的数据目录，不覆盖 A。两个应用同时工作，跨库 CONNECT 被服务器拒绝。B 管理员凭据随后分离为独立随机秘密，并通过非 loopback TCP 验证应用密码不能登录 ops；自身库可访问、跨库拒绝，证据单列，未掩盖实验修正。

每组普通4次、账号池4次、SSE2次，最大并发4；两组各精确结算140，Sub2API Usage 增4、Sub2API 钱包不变、无重复消费或未投递结算。B 完成后停止，A 原有新写入完整恢复运行。B 是实验，不是五服务正式 helper 候选。

| 指标 | A 双 PG | B 共享 PG |
|---|---:|---:|
| 容器数 | 6 | 5 |
| 空闲进程 RSS 合计 MiB（共享页会重复计数） | 562.5 | 489.7 |
| 空闲父级 working set MiB | 1163.1 | 1196.5 |
| 空闲父级 memory.current MiB | 1476.3 | 1508.0 |
| 请求观测工作集峰值 MiB | 1209.6 | 1238.2 |
| 请求观测 memory.current 峰值 MiB | 1524.4 | 1536.0 |
| 非流 curl 中位延迟 ms（8次） | 96.0 | 101.3 |
| SSE curl 秒 | 8.07 / 8.07 | 8.05 / 8.04 |
| CPU 消耗/采样窗口秒（含采样驱动） | 3.92 / 14.96 | 3.54 / 12.55 |
| pg_stat_activity 行数（含后台/观察连接） | 各8 | 共享实例10，不能相加成20 |
| Redis 使用 MiB | 1.89 | 1.72 |
| 宿主VM MemAvailable MiB（含其他既有服务） | 11257.8 | 11350.0 |
| 宿主VM total−available MiB | 4480.3 | 4388.2 |
| Swap / 正常阶段 OOM | 0 / 0 | 0 / 0 |
| 磁盘占用（含旧包与实验数据）GB | 9.06 | 9.15 |

数据为单轮、小样本、嵌套 Docker 观察，含引擎、文件缓存与测量开销，不是生产吞吐结论。B 的66.17秒记录包括数据库启动至工作负载结束，不冒充纯启动时间；完整最终升级窗口的启动/缓存峰值另见低资源报告。前期数据库专用 psql 实验另存 `postgres-exploratory.json`，不拿它冒充应用 API 延迟。

## 决策

不采用共享 PG：RSS 少约72.8MiB，但父级工作集多约33.4MiB，未证明整机10%或128MiB节省，非流延迟也未改善；共享实例将扩大两个业务的故障域。逻辑库隔离不等于物理故障隔离。保留原双 PG。

共享实验的独立库 dump/restore、pg_dumpall 完整实例恢复、重启与数据校验通过；完整实例恢复证据来自专用合成数据库实验，不宣称已做生产全量或五服务正式发布恢复。最终采用的六服务已通过实际 helper 双库全表摘要、Redis、加密快照恢复和两轮重启。

Redis 无需合并、不新增；反向代理继续共用一实例。C“再共享 Redis”不适用。证据：`local-stack/comparison-*.json`、`shared-*-isolation.json`、`postgres-full-instance-restore.json`。运行命令 `python <private runtime>/compare-stacks.py`，角色分离复核脚本单列在运行时目录；原 A 新账务未被旧快照覆盖。
