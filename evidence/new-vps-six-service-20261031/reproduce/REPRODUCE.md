# 本地复现材料

这些是本轮实际驱动快照，仅将私有runtime默认位置固定在仓库外，并支持CLEARAPI_SIX_LAB_RUNTIME；不是另一套生产部署系统。生产只用scripts/deploy-local.ps1和同一Python helper的stack模式。所有真实发布仍需新目标授权。

前置：Windows/Python/PowerShell/OpenSSH/Docker，固定六镜像包和源码包；本地OS测试镜像clearapi-six-helper-lab:20261031由本目录Dockerfile构建（依赖既有clearapi-isolated-ssh-test:20261023，其来源是原SSH演练环境）。固定测试镜像及依赖版本/源包摘要见报告。OS镜像准备可下载系统软件；实际服务部署不pull、不访问registry。宿主Docker需健康，不得为复现擅自重启共享Docker宿主或停止其他项目。

两轮测试分别使用仓库外D:/AI/API/runtime/six-service-helper-20261031和six-service-final-20261031；脚本中的容器名、loopback18961/18962和工作区路径明确可见。重放须使用新的空runtime和专用新容器名/端口，并同步脚本常量，不能对已有运行目标直接重跑初始化。不要将env、私钥、GPG私钥、dump、private-business.json复制到本目录或发布包。

宿主模拟命令形态：

```powershell
docker create --privileged --name clearapi-six-helper-final-20261031 --cpus 1.7 --memory 1536m --memory-swap 1536m --pids-limit 1024 -p 127.0.0.1:18962:22 --entrypoint sh -v D:/AI/API/runtime/six-service-helper-20261031/start.sh:/lab-start.sh:ro -v D:/AI/API/runtime/six-service-helper-20261031/after.sh:/lab-after.sh:ro clearapi-six-helper-lab:20261031 /lab-start.sh
docker start clearapi-six-helper-final-20261031
```

start.sh建立实际50,000,000,000字节ext4盘，Docker data-root和/opt/clearapi都在该盘；先执行官方dind的cgroup委派准备，再运行SSH和dockerd。privileged只用于本地嵌套测试，生产六服务不使用privileged。实际OS/kernel仍共享宿主，512MiB/0.3CPU预留不是完整VM验收。

准备原已校验images.tar/gateway-source.tar与当前非敏感配置到私有runtime/package。最终轮使用final-install/prepare-lab.py与enroll-and-deploy.py：生成合成secret、登记真实daemon/主机公钥、调用实际root安装器、Windows DryRun、真实SSH上传和空库部署。阶段结果会写入final-install证据目录。

final-install/business-and-restore.py business restore执行4普通、4池、2SSE、停用和真实GPG备份/两PG新目录恢复/Redis RDB→AOF与TTL/两轮重启。池Mock只读克隆此前负责人已确认条款的合成Sub2API库，依赖其私有runtime和原隔离PG容器；**该库/Token不随包交付，不创建或伪造新条款确认**。没有这个合法测试前置时，须保留门禁，不能自行accept或编造fixture。

scenarios.py negatives / faults / oom / interrupt / financial用于第一轮专用容器：损坏/缺失镜像、传输损坏/断开、实际ext4剩余量不足、内存阈值、DB/Redis/迁移/健康失败、实际小内存edge OOM、SSH重启中断及合成+1资金差异。所有Owner批准均标local-isolated-test-only，不能转为正式授权；恢复不删库/卷，代码恢复保留最新数据。

fixture修正和早期记录保留于prototype-phase及各attempt文件。最后Docker Desktop API 500/SSH banner timeout影响了最新runtime漂移检查及最终收尾核验；以最终报告记载的实际完成/未完成状态为准，不能仅运行脚本或看到init回执就宣称全量通过。
