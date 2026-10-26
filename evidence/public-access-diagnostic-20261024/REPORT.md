# 2026-10-24 Cloudflare 1010 与 DNS 只读排障

基线：正式集成分支 `codex/clearapi-local-deploy-formal-integration`，HEAD `22f85af11f68a9de62807332f62953dd0b2385b9`；开始时工作区干净。本轮未执行生产写入、服务启停、DNS/Cloudflare 修改、入口激活、Push 或发布测试。

## 已证实的请求差异

请求均为少量直接 GET，不使用代理、不跟随跳转、不伪装普通浏览器。目标为 `https://www.clear999.com/api/status?clearapi_readonly_dns=20261024`。以下时间取 Cloudflare Date，均为 UTC。

| 客户端 | UTC | HTTP / 错误 | CF-Ray |
| --- | --- | --- | --- |
| VPS 原生 curl/8.5.0 | 03:14:12 | 521 / 521 | a4f5cf5a5f85df9d-HKG |
| 同一 VPS Python-urllib/3.12 | 03:14:12 | 403 / 1010 | a4f5cf5aeb77107b-HKG |
| Windows 原生 curl/8.21.0 | 03:14:13 | 521 / 521 | a4f5cf621b61a425-LAX |
| VPS curl，仅将显式 User-Agent 改为上述 Python 值 | 03:16:46 | 403 / 1010 | a4f5d31faa871483-HKG |

VPS 公网来源由同次 Cloudflare trace 确认为 `103.42.181.219` / HKG。Windows 为 IPv6 直连 / LAX，Git 证据隐去主机位，原始采集保留于 Git 外的维护 runtime 目录。两个默认 curl 没有收到 1010；同一 VPS 上可以随实际客户端签名复现差异，不能归因为整个 VPS IP 或所有真实用户一律被封禁。对照请求解析到不同 Cloudflare anycast 地址，未把它当成固定边缘节点的严格单变量实验。

Cloudflare 官方将 [1010](https://developers.cloudflare.com/support/troubleshooting/http-status-codes/cloudflare-1xxx-errors/error-1010/) 定义为基于浏览器签名拒绝；[BIC](https://developers.cloudflare.com/waf/tools/browser-integrity-check/) 会检查 HTTP 头和非标准 User-Agent。因此本次强烈指向 Python 探针签名触发此类检查，**本站 BIC 开关、具体安全产品/规则 ID 仍未由 Security Events 核实**。没有关闭任何防护。

既有恢复入口 `scripts/pg03-failed-recovery.py:276` 的公网探针使用默认 urllib User-Agent，仅显式设置 Cache-Control；要求四个页面的 200、CF-Ray、API success 和 HTML 应用壳。这个探针即使重新启动 Caddy，也可能先被 1010 拒绝。不能原样重试并假定一定恢复，也不能把 403/521 当成健康通过。

## DNS 事实

父区 `a.gtld-servers.net` 的非递归答复与两台权威 NS 一致：`drew.ns.cloudflare.com`、`khloe.ns.cloudflare.com`；当前实际权威服务为 Cloudflare，无需凭本次证据修改 Nameserver。

| 名称 | 两台权威服务器的 A | AAAA | CNAME 查询 |
| --- | --- | --- | --- |
| clear999.com | NOERROR / NODATA | NOERROR / NODATA | NOERROR / NODATA |
| www.clear999.com | 104.21.2.201、172.67.129.157 | 2606:4700:3031::6815:2c9、2606:4700:3034::ac43:819d | NOERROR / NODATA |

权威答复均有 `aa` 标志。SOA serial `2416861397`，裸域负缓存 TTL 为 1800 秒；www 地址 TTL 为 300 秒。裸域存在 DNS 区但不提供地址，不是 NXDOMAIN，也不是仅 VPS 默认解析器故障或陈旧递归缓存。不能只靠刷新本机 DNS 修复。

VPS 默认 `127.0.0.53`（上游 Google 8.8.8.8/8.8.4.4）、公共 1.1.1.1、8.8.8.8、9.9.9.9 对裸域 A/AAAA/CNAME 均返回相同 NODATA。www 在默认、1.1.1.1、9.9.9.9 返回一致地址；Google 的单次 www A 查询超时，AAAA 正常，未无限重试。Windows 默认 10.187.34.32 最终同样明确返回裸域无 A、www 两条 A；初次裸域查询有瞬时超时。Windows 直连 1.1.1.1 UDP53 超时，Google DoH 超时、Cloudflare DoH 连接重置，是本机这些查询通路的限制，不据此推断域名全球故障。

www 的公开 DNS 和 CF-Ray 证实流量进入 Cloudflare 代理。[代理记录会返回 Cloudflare 地址，CNAME 也可能被展平](https://developers.cloudflare.com/dns/proxy-status/)；所以不能把公开 CNAME NODATA 写成“面板没有 CNAME”，也不能从公开地址推导面板内的源站目标。**面板记录类型、Content、橙云状态及源站目标尚未读取核实。**

## 源站与未完成检查

只读 Docker inspect：原 Caddy 54c8550b 仍 stopped / restart=no；Gateway 0a9a93c0 镜像及启动时间未变，loopback `/api/status` 为 200；PostgreSQL 99299db0、Executor 1081a1b5 的 CID/镜像/启动时间未变。未重验全部 Runtime/boot/备份，沿用前轮带身份的证据。

两个默认 curl 的 [521](https://developers.cloudflare.com/support/troubleshooting/http-status-codes/cloudflare-5xx-errors/error-521/) 与源站 Caddy 停止一致，不能证明源站 HTTPS/转发正常，也不能仅凭 521 排除错误的面板源站目标或源站防火墙条件。

仅发现现有 Edge 会话，没有 Cloudflare 专用管理连接器。打开 Dashboard 后未能取得可读页面，浏览器工具随后因无法可靠识别当前 URL 而安全停止；已停止 UI 操作，没有尝试绕过、读取 cookies/Token 或索取凭据。普通浏览器网站访问及 Security Events/BIC/DNS 面板均未完成。无法确认当前浏览器是否已登录，不能声称普通浏览器被拦或未被拦。

分类：**C（裸域缺少可用地址）已证实；另有已复现的探针签名拒绝和独立的 Caddy 停止问题。** A/B 的真实浏览器分界尚未验证，不能认定“只有 VPS 测试请求受影响”或“普通浏览器同受 1010”；若以 D 表示 DNS 与 Cloudflare 检查同时影响当前探针链路，则成立，但不代表 Cloudflare 对所有用户配置错误。

## 最小恢复建议（本轮未执行）

1. 在可用的既有管理会话中只读按上述 UTC / Ray / 来源查看 Security Events，确认是否为 BIC，并读取 www 的实际源站 Content、代理及 TLS 设置；用一次普通浏览器访问补齐真实用户范围。保留全站防护。
2. 针对正式恢复探针的默认 urllib 签名做最小兼容修正，使用明确标识、正常获准的健康客户端；保留原四页面、TLS、CF-Ray、内容校验和失败停回合同。优先解决探针兼容，不先申请全站 BIC/WAF 关闭。本轮不改代码或安装 root 资产；若更改已冻结 root 脚本，必须单独审核精确差异、针对性验证及新 SHA 后授权。
3. 在上述条件满足并另获受控维护授权后，复用既有正式恢复入口仅启动原 Caddy；先核实源站 TLS/转发，再完成规定公网验收。持续 403/521 或任何合同失败即停止并安全停回 restart=no，保留原 FAILED/target/hold/失败审计。无需更新或重启健康 Gateway。
4. 裸域另作最小修复：读取面板后为 @ 配置有效的代理解析，并设置到 `https://www.clear999.com` 的规范跳转。优先评估 Cloudflare 边缘跳转，确认边缘证书覆盖裸域；原 Caddy 及源站证书仅配置 www，**不能只添加裸域 DNS 就宣称 HTTPS 可用**。不添加未经确认可达的源站 AAAA、不修改 NS，不与 www 恢复捆绑扩大维护。

需要另行授权的写入仅为：精确的恢复探针修正/对应 root 文件更新、满足正式隔离合同后的 Caddy 恢复；若要恢复裸域，再批准具体 @ 记录和精确边缘跳转。只有 Security Events 证明必须设置狭窄例外时才提交该规则的单独方案。当前不申请或执行全站防护关闭、本地入口激活、Gateway 更新或数据库修改。

原始脱敏采集：`vps-readonly-diagnostic.json`、`vps-ua-control.json`、`windows-curl.json`、`authoritative-dns.json`、`windows-default-dns.json`、`windows-public-doh.json`。只进行了上述诊断及本地文档检查，未重复部署 E2E。上一轮生产维护事实保留在 `../production-maintenance-20261024/`，本报告不覆盖其失败记录。
