# ClearAPI 当前工作区与恢复入口

唯一活动开发工作区：`D:/AI/API/worktrees/clearapi-new-vps-six-service-helper-20261031`。
应用及部署工具验收基线：`ff560c159faaea7eabf364d886afc6c51b3266f3`。本索引只整理本地路径，不改变应用、计费或发布逻辑；new-api / QuantumNous 项目信息保持。

- 唯一权威总控仍在 `D:/AI/API/worktrees/clearapi-controlled-launch-20261020/CLEARAPI_MASTER_STATUS.md`，历史工作区因运行依赖保留。本目录中同名文件是历史副本，不能作为新的权威。
- 最新六服务验收：本目录 `CLEARAPI_NEW_VPS_SIX_SERVICE_DEPLOYMENT_FINAL.md`。
- 本次备份清理报告：`D:/AI/API/CLEARAPI_FULL_BACKUP_AND_WORKSPACE_CLEANUP_FINAL.md`。
- 桌面完整归档：`D:/Users/Chen/Desktop/ClearAPI_FULL_BACKUP_20261031`；恢复入口为其中 `RESTORE_README.md`。
- 最终部署包：`D:/AI/API/runtime/six-service-helper-20261031/release-v2.tar`，SHA256 `ed0c3dba06d435852892483ca3647f635d3308f35536673bc62c1a2600a1f01d`。
- 部署入口仍是 `scripts/deploy-local.ps1`，新目标用 `-StackTarget` 和受保护目标配置；操作单为 `deploy/sub2api-low-resource/SIX_SERVICE_RUNBOOK.md`、`MIGRATION.md`。

历史报告中的旧工作区路径只表示当时证据来源。已移除的源码从归档Git bundle及工作区差异恢复，不要把不同支线未经审查拼入当前候选。运行数据、数据库、密钥、Docker卷和仍有绑定依赖的历史目录不因清理自动删除。

有界OFFLINE_DEPLOYMENT_READY不等于生产上线授权；真实新机SSH/资源/网络、真实厂商Usage和费用验收、生产迁移及启池仍分开执行。
