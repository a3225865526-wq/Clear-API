# ClearAPI / Sub2API 低资源部署验收

2026-10-31，Asia/Shanghai。**本地组合启动、小并发Mock、逻辑备份恢复通过；2核2GB/50GB可靠商用验收仍BLOCKED，不能据本报告批准正式收费或迁移。** 不虚构物理VPS、50GB磁盘限额或真实厂商结果。

## 实际模拟边界

宿主Docker Desktop/WSL2，14核、约15.37GiB，并有其他已有工作负载；未改宿主限制或停止其他项目。新建独立Docker-in-Docker `clearapi-lowresource-lab-20261031`，未挂载宿主Docker socket，父cgroup **1536MiB、1.7 CPU、memory-swap=1536MiB（无额外swap）、pids1024**。内部六个服务和嵌套dockerd共同受这一限制；按2GB整机另预留512MiB和0.3 CPU给宿主。没有把某一个服务2GB上限当整机限制。

这是保留宿主预算的组合模拟；真实内核、sshd、Docker宿主、TLS代理、监控/备份调度未在完整2GB VM里共同实测。父容器使用privileged仅为本地嵌套Docker，本交付生产Compose不使用privileged。内层vfs用于隔离，不能把其镜像占用或性能直接套到生产overlay2。未限制底层磁盘50GB，宿主可用盘远大于50GB。

## 服务预算与实测

固定Gateway92edfc345、Sub2API d851a384，原完整后台，PG17.6/17.11、Redis7.4.11及固定本地nginx镜像。两PG独立持久库，未通过关闭账务或审计降低内存。

| 服务 | 容器上限MiB | 本轮业务采样最大Docker使用量MiB |
|---|---:|---:|
| Gateway | 448 | 136.50 |
| 主PG17.6 | 256 | 77.54 |
| Sub2API | 384 | 87.36 |
| Sub2API PG17.11 | 256 | 89.95 |
| Sub2API Redis7.4.11 | 96 | 15.18 |
| edge | 64 | 11.65 |
| 合计 | **1504** | 各服务采样最大值相加约418.18；不是同时峰值或严格进程峰值 |

Gateway SQL10/idle2、GOMEMLIMIT320MiB、GOMAXPROCS1、上游闲置连接16/每host4；Sub2API SQL10/idle2、Redis pool16/idle2、GOMEMLIMIT288MiB/GOMAXPROCS1。PG shared_buffers32MB、work_mem1MB、maintenance16MB、max_connections20、max_wal_size256MB。Redis AOF everysec、maxmemory48MB/noeviction；maxmemory不含全部进程与AOF重写内存。edge最多8条活跃HTTP连接，不是每账号调度或USD硬预算。上述Go内存目标和数据库参数不是绝对RSS/磁盘硬限制。

父memory.peak **1,610,625,024 bytes，约1536.01MiB**；这包含镜像导入、vfs页缓存及内核slab，不能当六服务常驻RSS。终态累计memory.events显示oom=0、oom_kill=0，但max计数193760（随后收尾可能变化），证明曾触及限制并回收，不代表有充足余量。采样memory.stat记录anon约185MiB、file约997MiB、kernel约300MiB的观察，详见final-resource.json。累计CPU统计包含导入/初始化/恢复：约289.7 CPU秒、95个被节流period、约4.90秒throttled；不能当真实业务CPU峰值或厂商吞吐。

## 实际流量和持久化验证

- 整体服务运行观察跨初始化/业务/恢复约二十多分钟，六服务健康；不是长时生产浸泡测试。
- 普通type1本地Mock：4并发，24次Chat JSON，每次10 input/2 output，合成定价每次14 units；24条唯一消费、336 units，用户钱包/Token/预留/消费一致，outbox待投递0。驱动含docker exec、stats与批间等待，24次批次约38.094秒；单次驱动耗时0.375–0.781秒，**不是应用纯延迟或最大QPS**。
- 账号池：将原已正式确认条款的合成Sub2API验收库只读dump，恢复至本lab独立`sub2api_poolmock`。没有复制生产数据或代调用accept；该恢复仅模拟原合成实例，不是新生产实例条款确认。4并发经真实Gateway→真实Sub2API→edge内Mock，4条新内部Usage、4条新客户消费，共56 units；驱动耗时约0.735–0.938秒。没有重复原16例全协议矩阵，原21份证据hash全部匹配。
- 最终合成账本包含前期失败：32条消费/32个唯一request，Token used=reservation applied=毛消费432，钱包从1000000变999568。24条普通+4条池成功共392；另外4次停调度账号503保留40未知units，未强制退款或假称可信Usage。另4次Mock连接前失败已refunded、0金额，无消费日志。成功与失败记录原样保留。
- 在后续池验证前，停止Gateway/Sub2API/edge写入，PG17.6的83张表和PG17.11的105张表各做一致dump→独立新库恢复→所有表排序行摘要相同；两轮Gateway/Sub2API/Redis重启后24次业务阶段的`999664|336|336|24|0`不变。此快照早于池4次成功/4次失败，不冒充最终432账本的再次恢复。
- 实际运行backup.sh完整导出两PG/globals、Redis RDB、app/env配置；11个备份文件SHA校验通过。备份保存在private-backup-2，含合成secret，不在发布包/Git。Redis两轮AOF重启健康；**未做独立RDB导入/AOF重建或真实生产加密异地恢复**。
- Redis移除客户端AUTH环境后PING明确NOAUTH；缺Key访问/v1/models为401。PG/Redis无发布端口、管理/edge仅回环。真实HTTPS/外层代理来源链、真实费用上限未验收。

首次失败/修正完整记于attempts.json：缺显式schema导致保护性拒绝、代理IP碰撞、nginx无wget、Redis变量转义、挂载inode未更新、活跃system_instances快照漂移、CRLF备份路径、误用PUT设置schedulable。均未通过修改生产业务/账务代码或删除失败记录解决。后者改用已有POST /schedulable后通过。

## 50GB空间预算与风险

固定六镜像离线tar **514,803,200 bytes**，源码tar45,936,640 bytes；vfs lab Docker数据约11.8GiB、合成/state最终约198MiB（包含演练恢复库）。不是正式库大小/生产overlay2占用。生产初步盘预算：OS/Docker/系统服务8GiB、运行与回退镜像6GiB、主/池DB及WAL上限目标10GiB、Redis/app/日志4GiB、一套备份和恢复临时空间10GiB、至少8GiB自由余量，总46GiB。50GB十进制盘约46.6GiB，余量很小；这是一份预算，不是硬配额或已测容量。原PG03历史、独立log DB和旧站真实数据必须额外盘点，超过预算就升级磁盘，不能删除历史凑数。

Docker local日志6×30MiB约180MiB轮转；Gateway默认/data/logs独立增长，提供logrotate配置但尚未在目标系统调试/安装。PG WAL/checkpoint目标不限制异常长事务/复制槽保留；AOF重写、PG备份/恢复/建索引、批量导入账号/模型、压缩加密、镜像load/双版本并存、长SSE及高Token编码均可能超内存/CPU/磁盘。备份不得与突发流量争抢内存，出现OOM、持续swap、连接池饱和、磁盘剩余<8GiB或未知Usage增长，先暂停相关池新请求并保留在途/账务。没有为通过测试删审计/数据库或关闭持久化。

## 明确结论

**A：本地受限组合实测可完成4并发、24次普通Mock及4次池Mock；没有任何已证明可安全对客户收费的真实请求率范围。** 仅可作为默认关闭、最多4个短Mock同时运行的本地试验依据，不承诺4条真实长SSE或同QPS稳定性。

**B：主要风险是硬上限总和1504MiB加宿主512MiB已达2016MiB，仅余32MiB，未含dockerd额外峰值、完整TLS/监控/备份/旧PG03历史运行开销；组合上限本身不是可靠可用余量。父cgroup已有明显回收压力。** 50GB未限盘，真实库存/备份大小未知。

**C：2核2GB/50GB可靠商用结论BLOCKED（容量与目标验收未满足，不是声称发生过OOM）。** 若希望脱离这一紧预算，首版建议的最低候选升级是2核/4GB；50GB仅在实际库/历史/备份盘点满足上述余量后保留，否则80GB或独立备份盘。4GB/80GB是容量规划建议，尚未实机证明，不写成已验最低硬件。即使升级也要目标出口/真实模型/备份恢复和小规模持续运行验收。

配置：deploy/sub2api-low-resource；证据：evidence/sub2api-commercial-deployment-20261031；运行时私有资料：D:/AI/API/runtime/commercial-deployment-20261031。全部新lab最终停止并保留数据；原隔离服务及生产不变。
