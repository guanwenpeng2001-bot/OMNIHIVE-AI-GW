# 公网出口与 SSRF 防护

## 判断依据与实施规则

官方数据下载到 `backend/internal/util/urlvalidator/testdata/`，仅规范化行末空白/换行，测试不联网：

- [IANA IPv4 特殊地址](https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry.xhtml)，登记更新时间 2025-10-09。
- [IANA IPv6 特殊地址](https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry.xhtml)，登记更新时间 2025-10-09。
- [IANA IPv6 单播分配](https://www.iana.org/assignments/ipv6-unicast-address-assignments/ipv6-unicast-address-assignments.xhtml)，登记更新时间 2025-10-10。
- [IANA IPv6 地址空间](https://www.iana.org/assignments/ipv6-address-space/ipv6-address-space.xhtml)。
- [RFC 6052 §3.1](https://www.rfc-editor.org/rfc/rfc6052.html#section-3.1)，标准 NAT64 的内嵌地址限制。

`IsPublicIP(net.IP)` 是统一的、与 DNS 实现无关的目的地址判断：

1. 非法长度、nil、作用域 zone 均拒绝。IPv4-mapped IPv6 先 `Unmap`，按真实 IPv4 判断。
2. IPv4 拒绝非全局特殊地址、组播和保留空间，包括完整 `198.18.0.0/15`、CGNAT、文档地址及已废弃的 `192.88.99.0/24`。
3. 保留明确公网例外：`192.0.0.9`、`.10`，以及 AS112、AMT 等登记为全球可到达的地址。
4. IPv6 采用登记的 RIR 分配范围，加上特殊登记中明确全球可到达的具体子网；不把整个 `2001::/23` 一刀切拒绝。
   保留 `2001:1::1/2/3`、`2001:3::/32`、`2001:4:112::/48`、ORCHIDv2、DETs 等登记例外。
   文档地址仍拒绝。未分配、保留、ULA、link/site-local、组播、Teredo/6to4（全局可达属性为 N/A）不进入公网集合。
5. `64:ff9b::/96` 不是整个拒绝，而是递归验证低 32 位 IPv4。非全局的本地 NAT64 前缀 `64:ff9b:1::/48` 拒绝。
6. 登记中的 False、空值、N/A 不作为公网授权；公网属性表示策略资格，不保证本机路由或远端 HTTP 服务可用。

这是一份随代码审查更新的策略快照，不在运行时下载或信任远程地址表。
IPv6 新增合法分配可能需要更新快照及代码；更新时运行独立 CSV 对照测试，核对子网覆盖和例外。
IPv4 普通单播仍按特殊用途排除法判断，不维护实时 BGP/RIR 所有者清单。

## 请求生命周期与既有授权

- URL 字面量、DNS 预检和 PublicTransport 使用同一策略。`localhost.`、带 zone 的 IPv6 及畸形冒号主机名拒绝。
- 空 DNS 结果或解析错误拒绝；返回的任一地址不合规，则整组拒绝，不挑出一个好地址继续。
- PublicTransport 每个请求（含重定向和可复用连接）先校验；仅把验证后的 IP 交给现有连接逻辑。
  备用地址也只能来自同一已验证集合。域名 Host、SNI、证书身份以及响应关联的原始 URL 保持原有逻辑。
- 字面量 IP 不交给自定义 resolver 改写；request-local resolver 的返回值仍需完整验证。
- `ValidateResolvedIP` 只是预检，不能单独消除 TOCTOU；实际连接必须继续经过带 pinning 的通道。
- 租户上游默认只连公网。允许内网需要管理员同时开启 `allow_private_hosts`，并在 `upstream_hosts` 中填写准确主机名或 IP。通配符只用于普通主机名白名单。私网例外仍在每跳解析、验证并固定地址。图片与遥测等强制公网路径使用独立连接池。
- 十进制整数、八进制以及其他非标准 IPv4 写法均在解析前拒绝。
- 用户配置的可信代理和自定义 dialer 仍是信任边界。代理端点可以在内网；受限目标以验证后的 IP
  传给 HTTP absolute URI、CONNECT、SOCKS。不能以本地 socket 的代理 IP 替代目标策略。
- Qianwen 已有的限定结果域名、已知 fake-IP 前缀、固定 DoH bootstrap 和账户路由规则保留。
  `198.19.*` 以前被某条测试当作公网；现在正确拒绝，未扩展为新的通用 DNS 回退触发器。

## 可复现验证

普通测试包含地址正反例、边界、映射/畸形 IP、完整官方 CSV 对照、私网开关与白名单组合、
默认/请求级 DNS、多地址顺序、重定向及字面量 resolver 绕过。
既有传输测试覆盖 TLS/SNI、错误证书、HTTP/HTTPS CONNECT、SOCKS、HTTP/2、TLS fingerprint 和连接池隔离。
Kaggle 新增回归直接走 `kaggleVideoRequest`，确认显式私网授权可用，撤销授权或白名单不匹配时不会调用上游。

```sh
cd backend
go test -tags=unit ./internal/util/urlvalidator ./internal/pkg/httpclient
go test -tags=unit ./internal/service ./internal/repository \
  -run 'Test(QianwenResultLookup|Kaggle|HTTPUpstream(Public|DNS|Pinned)|OpenAIValidateUpstreamBaseURL|GrokAPIKeyURLPolicy)'

# Linux，或先在其他系统交叉编译，再在 WSL/Linux 执行脚本。
CGO_ENABLED=0 GOOS=linux go test -c -o /tmp/omnihive-httpclient.test ./internal/pkg/httpclient
bash scripts/test-public-egress-netns.sh /tmp/omnihive-httpclient.test
```

网络脚本新建 user/network namespace，仅启用 loopback，设置测试地址，没有外部接口或外部路由。
测试通过真实 UDP DNS 和 TCP listener/socket 验证目标，没有将被测 IP 偷换为 localhost 的 dialer：

- 13 类目的地址先证明可信通道实际可连，再验证严格通道的 DNS 名称和字面量访问结果。
- 混合 A/AAAA 和不同地址顺序均在 dial 前拒绝。
- 预检公网、连接前换到 `198.19.*` 时拒绝。
- 首次正常连接只解析一次；DNS 改变后，即使已有 keep-alive 连接也拒绝下一请求。
- 同域重定向触发新一轮 DNS 校验并拒绝非公网结果。

NAT64 测试是将合成 IPv6 地址绑定到隔离 loopback，验证地址策略和实际 IPv6 socket；
**没有运行 NAT64 翻译器，也没有证明真实 DNS64/NAT64 网络可用。**

## 部署边界

使用 fake-IP DNS、CGNAT 或保留地址的请求会被严格公网策略拒绝。可信内网服务需要显式授权。网络特定的 NAT64 翻译和公网地址的内部路由需要由可信代理、路由配置与出口隔离控制。地址分类本身不能验证 DNS 权威性。
