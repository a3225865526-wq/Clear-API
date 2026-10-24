# 执行边界与纠正记录

仅操作 clearapi-e2e-host 内的固定独立 daemon；SSH 仅 127.0.0.1:2222。专用测试密钥和 pinned host key 复用自已拥有的测试夹具；未读取生产密钥、连接生产、推送 GitHub 或修改工作流。

基线 d9cd1299 的历史 mock 仍引用不存在的 REGISTRY/pull_candidate，不适用归档代码；现已更新相关接口并运行 16 个聚焦用例。历史 substrate 结果未当作正式 helper 验收。

首次恢复测试主机时 dockerd 被过期 PID 文件阻断，只在该隔离主机确认没有运行 daemon 后清理 PID 并启动；daemon ID 仍精确匹配。临时 ssh sudo 包装器后续已换成真实 sudo 1.9.17，受限策略通过 visudo，权限边界在实际 clearadmin 测试用户下验证。

未计为 PASS 的前期尝试：测试夹具转义错误；两个测试进程共享临时归档造成的错配；Windows CRLF shebang 导致 python3\r 不存在；Compose 启动后 HTTP 未就绪导致回退误报；并发拒绝时 CopyTo 管道异常掩盖 SSH 原因；Windows 默认 GBK 解码 PowerShell UTF-8 日志失败；测试生产模式目录尚未创建。对应夹具或直接相关代码均已修正，失败不改写为通过。最终结果以 results.json 各案例及其原始日志为准。

源码修复保持后端失败后的 pending / MANUAL RECOVERY 合同。backend-explicit-test-operator-recovery.log 记录测试操作者显式恢复夹具，绝不算作后端自动回退。

成功部署是隔离开发模式 Gateway、真实 PostgreSQL 的工具链验收，公网健康地址在测试 receipt 中为空；不代表生产网站、真实登录/支付/计费、Executor、Caddy、Provider 或 PG03 商业业务验收。

备份身份的 GHCR 旧前驱回归使用 12 个现有定向用例；归档身份和测试证明拒绝另以真实 Linux/Compose 读取验证。正式加密备份服务的生产运行及首次切换均未执行。旧部署安装器的批准哈希未擅自更新，新工具须在独立生产切换授权内按确切源码哈希安装。

最终矩阵在 sudo 的实际拒绝信息“需要密码”与旧预期文本不一致处停止；修正断言后整组隔离项定向重跑 PASS，后端合同和最终完整入口分别 PASS。最终 30 个场景的结果及代码哈希均核对一致。

收尾已逐字节证实 Runtime 工作区 CRLF 与 Git LF 版本代码相同；执行资产哈希与规范源码哈希分开保留，最终 LF 资产实际 --check PASS。统一 diff 的空上下文行和诊断末尾空行是原始输出，限定证据路径保留其空白；源码空白检查未放宽。
