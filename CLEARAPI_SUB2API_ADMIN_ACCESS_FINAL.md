# Sub2API 独立管理入口交付

交付时间：2026-10-31T23:59:03+08:00。仅 Windows 本地隔离实验；实验分支 `codex/shared-infrastructure-optimization-20261031`。应用源码 `fa96a74df2b46c0f3617068e0f77fbdc2fae67fe`，离线操作配置源码 `dd68a49afb63fc66433f5b54b2e4eb4a8ada4ef4`。源码基线和原 release-v2 均保留；未推送 GitHub、未操作生产、未修改 DNS、未发送真实收费请求。

`SUB2API_ADMIN_URL` 提供独立 HTTPS Origin；拒绝携带用户名密码、查询、片段或业务路径。管理员菜单通过服务端管理员接口取得固定配置，新标签页使用 noopener/noreferrer；普通用户看不到入口且直接请求被拒绝。URL 不携带密码或 Token。保留 Sub2API 官方前端、原生登录和独立会话，不共享两应用 Cookie。

可选 `compose.admin-access.yaml` 复用现有唯一 Nginx。渲染器校验主机名、HTTPS Origin 和有限 CIDR；模板启用 TLS 1.2/1.3、IP 白名单、默认拒绝、错误 Host 421、原生静态资源/API 根路径及未缓冲的 SSE/长连接。默认 loopback 端口继续保持；公网入口、VPN/Access 和正式证书仍须负责人现场配置。

Windows helper 可通过明确的 `admin_access: true` 使用覆盖文件，所有 Compose 操作包含覆盖文件，并固定额外配置和证书 SHA256、root 所有权与权限。加密备份包含这些保护文件。默认示例为 false，不改变未选择该功能的部署。

本地实际结果（`local-stack/admin-tls-native.json`）：使用明确信任的隔离证书，原生 HTML、193223 字节静态资源、原生登录/API 成功；错误 Host 421，非白名单来源 403；未使用跳过 TLS 校验选项。DB 和 Redis 无公开端口。管理权限前后端回归和普通用户实机拒绝通过；账号池/SSE 仍通过同一代理。原生认证机制没有重写；未将负向暴力登录、全体系安全审计或真实公网 TLS 标成通过。

关键文件：`controller/group_status.go`、`router/api-router.go`、`web/src/components/layout/components/sub2api-admin-link.tsx`、`deploy/sub2api-low-resource/compose.admin-access.yaml`、`render-admin-proxy.py`、`scripts/local-deploy-stack.py` 和安装器。安全参考：OWASP [Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)、[ASVS 5.0.0 授权](https://github.com/OWASP/ASVS/blob/v5.0.0/5.0/en/0x17-V8-Authorization.md)。仅核验本次适用的授权边界、来源保护、凭据不进入 URL 和独立会话；不声明全体系 ASVS 合规。

公网 DNS、正式 TLS 链、真实访问策略、负责人原生管理员账户/MFA/恢复和审计策略为 NEW_VPS_ACCEPTANCE_PENDING。
