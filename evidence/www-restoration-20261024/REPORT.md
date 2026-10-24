# 2026-10-24 原 Caddy 与 www 网站恢复结果

**结果：`https://www.clear999.com` 已真实恢复。原 Caddy 运行，正式恢复合同及源站/Cloudflare/Edge 页面检查通过，本次未触发安全停回。本地部署入口仍未激活。**

开始 HEAD：`d0d51ef5f7cb87982f02a11029425e5363938ccd`，正式集成分支及工作区正确且干净。用户本轮明确授权修正实际触发1010的探针，并在既有前置通过后沿原恢复合同启动原 Caddy；未将裸域、PG03、Gateway 发布或全量测试作为前置。

## 最小修复及验证

源码提交：`8a081ff2d07951d8fa7b0626743df51dcb3899cf`。

- 正式公网探针改为原生 curl，保留真实默认 User-Agent，用 `X-ClearAPI-Probe: website-health-v1` 标识用途；未伪装浏览器、未修改 Cloudflare。保留证书验证、固定 HTTPS、禁止跟随跳转、大小和时间上限、CF-Ray、API success 及四页面应用壳检查。403/521 等真实 HTTP 失败立即记录状态/Ray并走原停回，不反复请求安全拦截页。
- 源站 HTTPS/转发及 HTTP308 验证放在 Caddy 启动后、公网验收之前；前置检查不会因停机态公网521而错误阻断。
- 旧入口已有一次失败锁，本次新授权对应对原失败的人工复核。仅允许指定精确失败 SHA、已确认安全停回且实例相同的一次追加尝试；原 attempt/failure 保留原字节，新的尝试文件带 reviewed SHA，成功结果仍发布至既有 canonical result 路径，供原只读 Caddy 合同识别。没有删除锁、重写 FAILED/target/hold 或自动重试。
- 本地 Linux 无网络、只读容器执行16项定向测试通过，Python 编译及 Git diff 检查通过；未重跑30项部署 E2E。Windows 直接执行因 fcntl 不适用，真实测试在 Linux 执行；一次测试夹具未保留临时 CONFIG 路径的问题已修正后重测。现有 legacy V2/CONTROL 源码哈希引用同步仅为保持仓库一致，**未安装这两个旧生产资产**。

## 真实执行与身份

前置只读复核全部7项已安装资产、原 Gateway/PG/Executor 身份及启动时间、四层 Compose/接管状态/旧隔离凭证均匹配；Runtime、boot installed/live PASS。原 Caddy 配置 SHA `6b1246669fbab3287a5c9e99f041cbcff0c44ebbcedb9c0a751310f7b1b05969` 匹配；公开证书 SAN=www，2026-10-06至2027-01-04有效，未读取或导出私钥内容。原备份20261024T025143Z的密文/manifest/当前身份一致，复用前轮认证解密及既有恢复方法证据。

4个旧生产 workflow仍 disabled_manually，queued/in_progress/waiting/pending/requested均0，普通CI保留。带正常原生curl探针标识的停机态兼容检查返回521，明确只判断客户端未遇1010，不算网站健康通过。

UTC03:30:35，仅原子更新 `/opt/clearapi/bin/pg03-failed-recovery.py`，root:root0750，34573 bytes，SHA256 **`04b237bd1337fb535fb6c2cb45c3462e13fd69e01a50e98c8df4b245e6c628bd`**。其他6项资产不变；旧脚本和旧 attempt/failure 保全在 `/opt/clearapi/backups/www-restoration-20261024-8a081ff2d0`。没有新增部署资产、sudoers、服务或发布系统。

UTC03:31:23–03:32:01，实际调用原入口 `python3 -I /opt/clearapi/bin/pg03-failed-recovery.py restore-adopted` 一次，明确引用原 adopted source `1aa591f4bb230c4fce5770114e45e9311beb87a4` 和已复核失败 SHA `42eca429d65a16f71568df7414b9d79aec0b59953c4915f2bb1808284ec7ead5`；本次工具代码身份独立记录为8a081ff2d0/04b237bd，不冒充原工具未变化。

原合同内部生成一次正式备份 **20261024T033125Z** 后才开启 Caddy：Result=success/exit0，密文12390727 bytes，SHA256 `f0fbffaaf17116b46f93ab41986033fc84902fd52089f8a1bf07365d6af667e2`；manifest SHA `48cf9630ea93a75d02fdc76c8e9b9d7ae07ff50ce727a0c2e53cba122c334de0`。密文 SHA/大小、当前 Gateway/元数据身份及 staging 清理通过。未重复全量恢复演练或新密文解密验收，不将复用旧可读取性证据描述为新密文完整恢复测试。

原 Caddy `54c8550bbaca837ae906dfeb28a51ac54223da0405d908940caeb1d1920c00bf`，原 f2a1290d 镜像，StartedAt **2026-10-24T03:31:58.532588899Z**，Running=true、restart=unless-stopped。源码中的原正式启动方式及异常停回方式保持；没有额外 Gateway/PG/Executor start/restart/Compose up。

## 分开记录的访问结果

| 层次 | 实际结果 |
| --- | --- |
| 源站 HTTPS | `--resolve www.clear999.com:443:127.0.0.1`，证书校验正常，代理到现有 Gateway 的 `/api/status` success=true |
| 源站 HTTP | 308至 `https://www.clear999.com/` |
| Cloudflare 状态 API | UTC03:32:00，200，Ray `a4f5e96bdb1ae2f5-HKG`，success=true |
| Cloudflare 首页 | 200，Ray `a4f5e96c988de6ba-HKG`，应用壳有效 |
| Cloudflare 登录页 | 200，Ray `a4f5e96d39651f2b-HKG`，应用壳有效 |
| Cloudflare dashboard/overview | 200，Ray `a4f5e96dca390982-HKG`，应用壳有效，不冒称已登录 |
| Windows 原生curl首页 | 200 / DYNAMIC，Ray `a4f5ebe04a443ceb-LAX`，应用壳SHA与VPS一致 |
| 普通 Edge 浏览器 | 真实首页DOM已渲染：品牌/导航/主标题正常；随后浏览器直接访问 `/api/status` 显示JSON，UTC03:37:08确认success=true |

浏览器为现有正常 Edge 会话的只读自动观察，**没有伪称人工验收、登录、充值、交易或 Provider 验收**。起初 CUA 导航等待超时，后续使用其文档提供的 DOM API读到了实际已渲染页面，没有反复刷新。没有读取 cookies/Token 或更改账号设置。

Windows **直连curl** 的两次状态API请求发生连接超时（含一次明确IPv4诊断），无HTTP响应或Ray；一次JS下载收到200及188814 bytes但在15秒超时，不能报完整下载PASS。这些记录不删除，也不把000连接超时猜测为Cloudflare拒绝。VPS规定公网验收及正常 Edge 首页/API均通过；没有以Windows超时宣称所有网络路径正常，也没有因此修改防护或生产设置。

## 最终合同与边界

成功结果 `/opt/clearapi/app/website-restoration-result-39347012653.json` root:root0600，3701 bytes，SHA **`d2bfd7f6fbe7da232b5e91ea39d64ae9622d66070497d444fb9643a516b99328`**，真实 status=website_restored/public_traffic=open，绑定原 adoption、Caddy及当前实例，明确 gateway_replaced=false/database_restored=false/original_deployment_status=failed/circuit_cleared=false。

UTC03:36只读后置：Runtime、boot PASS；复用仓库既有 Caddy reconcile 源码以内存只读模式核对实际现场，PASS/effective_stage=website_restored，无生产资产安装或审计状态覆盖。原 FAILED/target/hold、旧 attempt SHA43d5f560、旧 failure SHA42eca429、接管state SHAcdcea866均不变。Gateway `0a9a93c0`/ed6/a032、StartedAt2026-10-23T14:07:58.383082222Z原样；PG99299db0、Executor1081a1b5的CID/镜像/启动时间/restart/mounts原样。

本次未触发安全停回，未调用数据库恢复或写入业务配置/数据；PG03仍暂缓商业。未修改DNS、NS、Cloudflare、旧workflow状态；未Push/合并main。

**www恢复没有剩余服务端阻塞。** Windows直连通路的部分诊断超时不隐瞒，但正常浏览器访问成功。裸域DNS/HTTPS跳转继续按上一轮独立建议后续授权，不作为本次前置，未执行。`/opt/clearapi/config/local-deploy.json` 与 `local-deploy.enabled` 均不存在，本地正式部署入口仍待单独激活。

证据：本目录 preflight、github-preflight、github-before-opening、targeted-validation、install-curl-probe、restore-www、final-production-state、windows-public-postflight、windows-ipv4-api-postflight、browser-postflight JSON。生产写入已在网站恢复及必要后置完成处结束。
