# 网络安全升级须知

API Key 白名单、认证限流、会话绑定和日志默认使用套接字对端 IP。升级前在 `server.trusted_proxies` 中填写实际代理 IP 或专用 CIDR，并在边缘覆盖 X-Forwarded-For。同机 Caddy 可填写 `[127.0.0.1/32, "::1/128"]`。容器部署应填写源站实际看到的 Caddy 地址。旧数据库和 YAML 的转发头开关及自定义头不再控制 IP 解析。会话绑定 IP 变化时用户需要重新登录。

自建内网上游需要同时配置 `security.url_allowlist.allow_private_hosts: true` 并在 `upstream_hosts` 中列出准确的上游主机名或 IP（可带端口）。开发栈例如使用 `upstream_hosts: [mock-upstream, "127.0.0.1"]`。该列表中的通配符不授权私网；只允许私网、回环地址，不允许链路本地或云元数据地址。旧的 HTTP 开发上游还需要显式设置 `allow_insecure_http: true`。图片下载与 BYOK 探针继续要求公网。
