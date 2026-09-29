// Package httpclient 提供共享 HTTP 客户端池
//
// 性能优化说明：
// 原实现在多个服务中重复创建 http.Client：
// 1. proxy_probe_service.go: 每次探测创建新客户端
// 2. pricing_service.go: 每次请求创建新客户端
// 3. turnstile_service.go: 每次验证创建新客户端
// 4. github_release_service.go: 每次请求创建新客户端
// 5. claude_usage_service.go: 每次请求创建新客户端
//
// 新实现使用统一的客户端池：
// 1. 相同配置复用同一 http.Client 实例
// 2. 复用 Transport 连接池，减少 TCP/TLS 握手开销
// 3. 支持 HTTP/HTTPS/SOCKS5/SOCKS5H 代理
// 4. 代理配置失败时直接返回错误，不会回退到直连（避免 IP 关联风险）
package httpclient

import (
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

// Transport 连接池默认配置
const (
	sharedClientLimit          = 128
	sharedClientTTL            = 15 * time.Minute
	defaultMaxConnsPerHost     = 100              // Match the shared idle socket budget; excess callers wait with their context.
	defaultMaxIdleConns        = 100              // 最大空闲连接数
	defaultMaxIdleConnsPerHost = 10               // 每个主机最大空闲连接数
	defaultIdleConnTimeout     = 90 * time.Second // 空闲连接超时时间（建议小于上游 LB 超时）
	defaultDialTimeout         = 5 * time.Second  // TCP 连接超时（含代理握手），代理不通时快速失败
	defaultTLSHandshakeTimeout = 5 * time.Second  // TLS 握手超时
)

// Options 定义共享 HTTP 客户端的构建参数
type Options struct {
	ProxyURL              string        // 代理 URL（支持 http/https/socks5/socks5h）
	Timeout               time.Duration // 请求总超时时间
	ResponseHeaderTimeout time.Duration // 等待响应头超时时间
	InsecureSkipVerify    bool          // 是否跳过 TLS 证书验证（已禁用，不允许设置为 true）
	ValidateResolvedIP    bool          // 是否校验解析后的 IP（防止 DNS Rebinding）
	AllowPrivateHosts     bool          // 允许私有地址解析（与 ValidateResolvedIP 一起使用）

	// 可选的连接池参数（不设置则使用默认值）
	MaxIdleConns        int // 最大空闲连接总数（默认 100）
	MaxIdleConnsPerHost int // 每主机最大空闲连接（默认 10）
	MaxConnsPerHost     int // 每主机最大连接数（默认 100）
}

// sharedClients 存储按配置参数缓存的 http.Client 实例
type sharedClientEntry struct {
	accessOrder uint64
	client      *http.Client
	lastUsed    time.Time
}

var sharedClients = make(map[string]sharedClientEntry)
var sharedClientsMu sync.Mutex
var sharedClientAccessOrder uint64

// GetClient applies a bounded LRU and idle TTL. Eviction closes only idle sockets.
func GetClient(opts Options) (*http.Client, error) {
	key := buildClientKey(opts)
	sharedClientsMu.Lock()
	defer sharedClientsMu.Unlock()
	now := time.Now()
	sharedClientAccessOrder++
	for cachedKey, entry := range sharedClients {
		if now.Sub(entry.lastUsed) >= sharedClientTTL {
			entry.client.CloseIdleConnections()
			delete(sharedClients, cachedKey)
		}
	}
	if entry, ok := sharedClients[key]; ok {
		entry.lastUsed = now
		entry.accessOrder = sharedClientAccessOrder
		sharedClients[key] = entry
		return entry.client, nil
	}
	client, err := buildClient(opts)
	if err != nil {
		return nil, err
	}
	if len(sharedClients) >= sharedClientLimit {
		var oldestKey string
		var oldest uint64
		for cachedKey, entry := range sharedClients {
			if oldest == 0 || entry.accessOrder < oldest {
				oldestKey, oldest = cachedKey, entry.accessOrder
			}
		}
		sharedClients[oldestKey].client.CloseIdleConnections()
		delete(sharedClients, oldestKey)
	}
	sharedClients[key] = sharedClientEntry{client: client, lastUsed: now, accessOrder: sharedClientAccessOrder}
	return client, nil
}

type idleClosingTransport struct {
	http.RoundTripper
	closeIdle func()
}

func (transport idleClosingTransport) CloseIdleConnections() { transport.closeIdle() }

func buildClient(opts Options) (*http.Client, error) {
	transport, err := buildTransport(opts)
	if err != nil {
		return nil, err
	}

	var rt http.RoundTripper = transport
	if opts.ValidateResolvedIP && !opts.AllowPrivateHosts {
		rt = NewPublicTransport(transport)
	}
	// Both *http.Transport and *PublicTransport implement CloseIdleConnections; the
	// ok value is intentionally dropped because buildTransport never returns nil.
	closer, _ := rt.(interface{ CloseIdleConnections() })
	rt = idleClosingTransport{RoundTripper: servertiming.WrapRoundTripper(rt), closeIdle: closer.CloseIdleConnections}
	return &http.Client{
		Transport: rt,
		Timeout:   opts.Timeout,
	}, nil
}

func buildTransport(opts Options) (*http.Transport, error) {
	// 使用自定义值或默认值
	maxIdleConns := opts.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = defaultMaxIdleConns
	}
	maxIdleConnsPerHost := opts.MaxIdleConnsPerHost
	if maxIdleConnsPerHost <= 0 {
		maxIdleConnsPerHost = defaultMaxIdleConnsPerHost
	}

	maxConnsPerHost := opts.MaxConnsPerHost
	if maxConnsPerHost <= 0 {
		maxConnsPerHost = defaultMaxConnsPerHost
	}
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: defaultDialTimeout,
		}).DialContext,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		MaxConnsPerHost:       maxConnsPerHost,
		IdleConnTimeout:       defaultIdleConnTimeout,
		ResponseHeaderTimeout: opts.ResponseHeaderTimeout,
	}

	if opts.InsecureSkipVerify {
		// 安全要求：禁止跳过证书验证，避免中间人攻击。
		return nil, fmt.Errorf("insecure_skip_verify is not allowed; install a trusted certificate instead")
	}

	_, parsed, err := proxyurl.Parse(opts.ProxyURL)
	if err != nil {
		return nil, err
	}
	if parsed == nil {
		return transport, nil
	}

	if err := proxyutil.ConfigureTransportProxy(transport, parsed); err != nil {
		return nil, err
	}

	return transport, nil
}

func buildClientKey(opts Options) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%t|%t|%t|%d|%d|%d",
		strings.TrimSpace(opts.ProxyURL),
		opts.Timeout.String(),
		opts.ResponseHeaderTimeout.String(),
		opts.InsecureSkipVerify,
		opts.ValidateResolvedIP,
		opts.AllowPrivateHosts,
		opts.MaxIdleConns,
		opts.MaxIdleConnsPerHost,
		opts.MaxConnsPerHost,
	))))
}
