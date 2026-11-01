# ClearAPI 新 VPS 正式部署候选交付

日期：2026-11-01（北京时间）。候选名称 release-v3-financial-fix。LOCAL_FIX_VERIFIED、OFFLINE_PACKAGE_READY；NEW_VPS_ACCEPTANCE_PENDING、MIGRATION_PENDING、DNS_SWITCH_PENDING。本轮没有部署旧/新生产服务器。

## 固定来源与离线包

- 应用源码：14da5b6bf25270802a0acac03e05a09c931bb4a0；打包工具提交：82cf08b34984426a155506fc35578859223020d4。
- 镜像：clearapi-local:14da5b6bf25270802a0acac03e05a09c931bb4a0；engine ID：sha256:f86cacb16b7ebff8e2748ecf5aa81c3dc308fc9f0254c6848b7c6296d0a8b2ae；离线config ID：sha256:c2929206a8c08d23cd2994c188d36ac01bf57021c0e20fb11a002f7e55aa6167。
- 桌面交付目录：D:/Users/Chen/Desktop/ClearAPI_RELEASE_V3_FINANCIAL_FIX_20261101。
- 安装包 release-v3-financial-fix.tar：735078400 bytes；SHA256：`bc0a58b4051abfc5b3a74d9c84568a69c97e681fe467622ff2e6a8aea13a10f3`。
- runtime原件：D:/AI/API/runtime/release-v3-financial-fix-20261101/release-v3-financial-fix-final.tar。
- 安装包内27项文件摘要+SHA256.json，package.py build/bundle校验通过；gateway-source.tar Git pax comment、OCI revision和应用提交一致。四份本轮报告与SOURCE/remote备份回执放在桌面交付目录，SHA256SUMS覆盖交付文件；外层offline归档摘要另存同名.sha256。

只重编译/重建Gateway主二进制和嵌入前端，基于已验fa96 Gateway基础镜像保留其余bootstrap/迁移工具、运行库和许可证。其他五镜像config逐一与原锁文件核对一致后复用，未重建。没有运行历史镜像验收或六服务部署回归。

## 功能及安全策略保留

沿当前d0cfe6验收基线增量修复财务统计和公告布局，保留ClearAPI商业功能、客户端配置、七语言、Sub2API运营后台、低内存六服务和安全Helper。五个Helper文件与基线Git字节摘要一致，未放宽权限、root登记/daemon/固定主机身份、备份或恢复拒绝覆盖新交易的门禁。

compose与两份target模板只更新Gateway固定提交；资源限额、内网/回环端口、生产readiness、PG03关闭等保持。已从运行部署清单排除provider-plan.historical.json；仓库历史不删除，旧VPS升级适配器等未跟踪材料不收进安装包。旧release-v3商业原始归档、桌面备份和v2均未覆盖。

Sub2API：sub2api.clear999.com / 3225865526@qq.com；原生管理员登录保留，admin_access=false；Access未验前公网管理入口关闭，PG/Redis内部端口不开放，账号池关闭，不收费、不虚构厂商验收。PG03商用延期/D图像生成不准部署保持。

## 实际验证与现场待办

定向测试和精确结果见CLEARAPI_BILLING_ANALYTICS_FIX_FINAL.md；SQLite快照、通知7测试、Chromium40布局/20回调、前端typecheck/lint/format/build、Linux编译与新镜像version均通过。package.py实际校验27项文件、镜像linux/amd64与config集合；没有新增schema或修改核心Billing。

未提供真实新VPS host/user/port、pinned指纹、身份文件和root配置，未尝试猜测或连接。SSH、CPU/RAM/磁盘、Docker/Compose、防火墙、出口、域名/TLS和Access全部待验。安装/迁移/备份/回滚详见另两份指南；当前不宣称生产READY。

按负责人要求未重跑完整历史矩阵、完整三数据库测试、Sub2API全验收、六服务部署和历史镜像验证。MySQL/PostgreSQL/ClickHouse本轮查询现场执行未验，生产跨版本恢复/迁移与真实库存容量也未验。$0.033530截图差异保持未解决。

## Git与停止边界

本地独立分支codex/release-v3-financial-fix-20261101，不修改main；GitHub production远端的同名独立分支用于源码备份，实际推送SHA/远端main前后值以桌面remote-backup-receipt.json为准。源码ZIP包含最终本地提交；安装包应用版本固定上列应用提交，报告/工具提交独立记录，避免循环摘要。

下一步只需真实新VPS到位后做现场安装及验收，再单独安排最新生产备份、最终停写同步、单写切换和回滚批准；不再安排旧VPS升级。没有最终批准不迁移生产数据或改DNS。
