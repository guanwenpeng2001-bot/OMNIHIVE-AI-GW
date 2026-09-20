package securityaudit

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

func TestNormalizeBaseURLAllowsAdministratorConfiguredDestinations(t *testing.T) {
	allowed := []string{
		"https://guard.example.com", "https://guard.example.com/v1", "http://guard.example.com",
		"http://127.0.0.1:8080", "http://10.0.0.8:8080", "https://172.16.0.5",
		"http://169.254.169.254", "https://metadata.google.internal", "https://192.0.2.1",
		"http://internal-admin.local", "http://guard.local:8080",
	}
	for _, raw := range allowed {
		_, err := NormalizeBaseURL(raw)
		require.NoError(t, err, raw)
	}
	blocked := []string{
		"ftp://guard.example.com", "https://user:pass@guard.example.com",
		"https://guard.example.com?q=secret", "https://guard.example.com/#fragment",
	}
	for _, raw := range blocked {
		_, err := NormalizeBaseURL(raw)
		require.Error(t, err, raw)
	}
	url, err := ChatCompletionsURL("https://guard.example.com/v1")
	require.NoError(t, err)
	require.Equal(t, "https://guard.example.com/v1/chat/completions", url)
}

func TestHTTPClientUsesDirectStandardDialer(t *testing.T) {
	client, err := NewSecureHTTPClient(ActiveEndpoint{BaseURL: "https://guard.example.com", TimeoutMS: 1000}, config.URLAllowlistConfig{})
	require.NoError(t, err)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.Nil(t, transport.Proxy)
	require.NotNil(t, transport.DialContext)
}

func TestPromptOutboundAllowlistAppliesToScanAndProbe(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		policy  config.URLAllowlistConfig
		allowed bool
	}{
		{name: "disabled preserves private access", policy: config.URLAllowlistConfig{UpstreamHosts: []string{"other.invalid"}}, allowed: true},
		{name: "explicit private match", policy: config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"127.0.0.1"}, AllowPrivateHosts: true, AllowInsecureHTTP: true}, allowed: true},
		{name: "unmatched private host", policy: config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"other.invalid"}, AllowPrivateHosts: true, AllowInsecureHTTP: true}},
		{name: "empty allowlist", policy: config.URLAllowlistConfig{Enabled: true, AllowPrivateHosts: true, AllowInsecureHTTP: true}},
		{name: "private disabled", policy: config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"127.0.0.1"}, AllowInsecureHTTP: true}},
		{name: "http disabled", policy: config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"127.0.0.1"}, AllowPrivateHosts: true}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, path := range []string{"scan", "models", "probe fallback"} {
				t.Run(path, func(t *testing.T) {
					var calls atomic.Int64
					server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
						calls.Add(1)
						if request.URL.Path == "/v1/models" {
							if path == "probe fallback" {
								writer.WriteHeader(http.StatusNotFound)
								return
							}
							_, _ = writer.Write([]byte(`{"data":[{"id":"` + DefaultGuardModel + `"}]}`))
							return
						}
						_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
					}))
					defer server.Close()
					cfg := &config.Config{}
					cfg.Security.URLAllowlist = scenario.policy
					scanner := NewConfiguredOpenAICompatibleScanner(cfg)
					if path == "scan" {
						_, err := scanner.Scan(context.Background(), ActiveEndpoint{BaseURL: server.URL, Model: DefaultGuardModel, TimeoutMS: 1000}, "hello", AllScannerIDs)
						require.Equal(t, scenario.allowed, err == nil)
					} else {
						service := newProbeTestService()
						service.scanner = scanner
						result := service.Probe(context.Background(), ProbeRequest{Endpoint: probeEndpoint(server.URL, "")})
						require.Equal(t, scenario.allowed, result.OK)
					}
					if !scenario.allowed {
						require.Zero(t, calls.Load(), "rejected destinations must not receive requests")
					} else if path == "probe fallback" {
						require.EqualValues(t, 2, calls.Load())
					} else {
						require.EqualValues(t, 1, calls.Load())
					}
				})
			}
		})
	}
}

func TestPromptOutboundAllowlistChecksRedirectAndDial(t *testing.T) {
	var targetCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		targetCalls.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	for _, allowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "matching redirect", false: "unmatched redirect"}[allowed], func(t *testing.T) {
			destination := target.URL
			if !allowed {
				destination = strings.Replace(destination, "127.0.0.1", "localhost", 1)
			}
			redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				http.Redirect(writer, request, destination, http.StatusFound)
			}))
			defer redirect.Close()
			client, err := NewSecureHTTPClient(ActiveEndpoint{BaseURL: redirect.URL, TimeoutMS: 1000}, config.URLAllowlistConfig{
				Enabled: true, UpstreamHosts: []string{"127.0.0.1"}, AllowPrivateHosts: true, AllowInsecureHTTP: true,
			})
			require.NoError(t, err)
			defer client.CloseIdleConnections()
			before := targetCalls.Load()
			response, err := client.Get(redirect.URL)
			require.Equal(t, allowed, err == nil)
			if response != nil {
				_ = response.Body.Close()
			}
			if allowed {
				require.Equal(t, before+1, targetCalls.Load())
			} else {
				require.Equal(t, before, targetCalls.Load())
			}
			transport := client.Transport.(*http.Transport)
			connection, err := transport.DialContext(context.Background(), "tcp", "localhost:80")
			require.Error(t, err)
			require.Nil(t, connection)
		})
	}
}

func TestPromptOutboundAllowlistChecksResolvedPrivateAddress(t *testing.T) {
	// Resolve a permitted domain using an isolated DNS fixture, never public DNS.
	dnsServer, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 4096)
		for {
			length, peer, err := dnsServer.ReadFrom(buffer)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buffer[:length]) != nil {
				continue
			}
			response := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: query.ID, Response: true, RecursionAvailable: true},
				Questions: query.Questions,
			}
			for _, question := range query.Questions {
				if question.Type == dnsmessage.TypeA {
					response.Answers = append(response.Answers, dnsmessage.Resource{
						Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
						Body:   &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}},
					})
				}
			}
			packed, err := response.Pack()
			if err == nil {
				_, _ = dnsServer.WriteTo(packed, peer)
			}
		}
	}()
	originalResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", dnsServer.LocalAddr().String())
	}}
	t.Cleanup(func() {
		net.DefaultResolver = originalResolver
		_ = dnsServer.Close()
		<-done
	})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	endpoint := strings.Replace(server.URL, "127.0.0.1", "guard.test", 1)
	for _, allowPrivate := range []bool{false, true} {
		client, err := NewSecureHTTPClient(ActiveEndpoint{BaseURL: endpoint, TimeoutMS: 1000}, config.URLAllowlistConfig{
			Enabled: true, UpstreamHosts: []string{"*.test"}, AllowPrivateHosts: allowPrivate, AllowInsecureHTTP: true,
		})
		require.NoError(t, err)
		response, err := client.Get(endpoint)
		if allowPrivate {
			require.NoError(t, err)
			_ = response.Body.Close()
		} else {
			require.ErrorContains(t, err, "resolved address is not allowed")
		}
		client.CloseIdleConnections()
	}
}

func TestOpenAICompatibleScannerRequestContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, DefaultGuardModel, payload["model"])
		require.Equal(t, float64(0), payload["temperature"])
		require.Equal(t, float64(64), payload["max_tokens"])
		require.Equal(t, float64(42), payload["seed"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	defer server.Close()
	scanner := NewOpenAICompatibleScanner()
	result, err := scanner.Scan(context.Background(), ActiveEndpoint{ID: "one", BaseURL: server.URL, Model: DefaultGuardModel, Token: "token", TimeoutMS: 1000}, "hello", AllScannerIDs)
	require.NoError(t, err)
	require.Equal(t, EventPass, result.Decision)
}

func TestOpenAICompatibleScannerFollowsRedirectAndRejectsOversize(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	result, err := NewOpenAICompatibleScanner().Scan(context.Background(), ActiveEndpoint{ID: "redirect", BaseURL: redirect.URL, Model: DefaultGuardModel, TimeoutMS: 1000}, "hello", AllScannerIDs)
	require.NoError(t, err)
	require.Equal(t, EventPass, result.Decision)
	oversize := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", int(maxGuardResponseBytes)+1)))
	}))
	defer oversize.Close()
	_, err = NewOpenAICompatibleScanner().Scan(context.Background(), ActiveEndpoint{ID: "large", BaseURL: oversize.URL, Model: DefaultGuardModel, TimeoutMS: 1000}, "hello", AllScannerIDs)
	require.Error(t, err)
}

func TestOpenAICompatibleScannerClassifiesHTTPConnectionAndTimeoutFailures(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		retryable bool
	}{
		{name: "authentication", status: http.StatusUnauthorized, retryable: false},
		{name: "forbidden", status: http.StatusForbidden, retryable: false},
		{name: "rate limited", status: http.StatusTooManyRequests, retryable: true},
		{name: "server failure", status: http.StatusBadGateway, retryable: true},
		{name: "other client error", status: http.StatusBadRequest, retryable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()
			_, err := NewOpenAICompatibleScanner().Scan(context.Background(), ActiveEndpoint{ID: "status", BaseURL: server.URL, Model: DefaultGuardModel, TimeoutMS: 1000}, "hello", AllScannerIDs)
			var guardErr *GuardError
			require.ErrorAs(t, err, &guardErr)
			require.Equal(t, ErrorCodeUnavailable, guardErr.Code)
			require.Equal(t, tt.status, guardErr.HTTPStatus)
			require.Equal(t, tt.retryable, guardErr.Retryable)
			require.NotContains(t, err.Error(), server.URL)
		})
	}

	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closed.URL
	closed.Close()
	_, err := NewOpenAICompatibleScanner().Scan(context.Background(), ActiveEndpoint{ID: "closed", BaseURL: closedURL, Model: DefaultGuardModel, TimeoutMS: 100}, "hello", AllScannerIDs)
	var connectionErr *GuardError
	require.ErrorAs(t, err, &connectionErr)
	require.True(t, connectionErr.Retryable)

	timeout := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer timeout.Close()
	_, err = NewOpenAICompatibleScanner().Scan(context.Background(), ActiveEndpoint{ID: "timeout", BaseURL: timeout.URL, Model: DefaultGuardModel, TimeoutMS: 20}, "hello", AllScannerIDs)
	var timeoutErr *GuardError
	require.ErrorAs(t, err, &timeoutErr)
	require.True(t, timeoutErr.Retryable)
	require.True(t, timeoutErr.Timeout)
}

func TestPromptAuditProbeModelsFallbackAndResponseSafety(t *testing.T) {
	t.Run("models contains configured model", func(t *testing.T) {
		var chatCalls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "Bearer temporary-token", r.Header.Get("Authorization"))
			if r.URL.Path == "/v1/models" {
				_, _ = w.Write([]byte(`{"data":[{"id":"` + DefaultGuardModel + `"}]}`))
				return
			}
			chatCalls.Add(1)
		}))
		defer server.Close()
		result := newProbeTestService().Probe(context.Background(), ProbeRequest{Endpoint: probeEndpoint(server.URL, "temporary-token")})
		require.True(t, result.OK)
		require.True(t, result.TokenApplied)
		require.Equal(t, http.StatusOK, result.HTTPStatus)
		require.Zero(t, chatCalls.Load())
	})

	t.Run("invalid models response performs real guard fallback", func(t *testing.T) {
		var chatCalls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/models" {
				_, _ = w.Write([]byte(`{"unexpected":true}`))
				return
			}
			chatCalls.Add(1)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
		}))
		defer server.Close()
		result := newProbeTestService().Probe(context.Background(), ProbeRequest{Endpoint: probeEndpoint(server.URL, "temporary-token")})
		require.True(t, result.OK)
		require.Equal(t, int64(1), chatCalls.Load())
	})

	t.Run("fallback authentication failure is stable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/models" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()
		result := newProbeTestService().Probe(context.Background(), ProbeRequest{Endpoint: probeEndpoint(server.URL, "temporary-token")})
		require.False(t, result.OK)
		require.Equal(t, ErrorCodeUnavailable, result.ErrorCode)
		require.Equal(t, http.StatusUnauthorized, result.HTTPStatus)
		require.False(t, result.Retryable)
	})

	t.Run("oversized models response is rejected without fallback", func(t *testing.T) {
		var chatCalls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				chatCalls.Add(1)
			}
			_, _ = w.Write([]byte(strings.Repeat("x", int(maxGuardResponseBytes)+1)))
		}))
		defer server.Close()
		result := newProbeTestService().Probe(context.Background(), ProbeRequest{Endpoint: probeEndpoint(server.URL, "temporary-token")})
		require.False(t, result.OK)
		require.Equal(t, "response_too_large", result.ErrorCode)
		require.Zero(t, chatCalls.Load())
	})
}

func TestResolveProbeEndpointReusesTokenOnlyForMatchingBaseURL(t *testing.T) {
	manager := &ConfigManager{}
	manager.snapshot.Store(&activeConfigSnapshot{active: ActiveConfig{Endpoints: []ActiveEndpoint{{
		ID: "guard-1", BaseURL: "https://guard.example.com", Token: "STORED_GUARD_TOKEN", TimeoutMS: 1000, InputLimit: 1024, Enabled: true,
	}}}})
	service := &PromptService{config: manager}

	matched, applied, err := service.resolveProbeEndpoint(UpdateEndpoint{
		ID: "guard-1", BaseURL: "https://guard.example.com/v1", TimeoutMS: 1000, InputLimit: 1024,
	})
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, "STORED_GUARD_TOKEN", matched.Token)

	mismatched, applied, err := service.resolveProbeEndpoint(UpdateEndpoint{
		ID: "guard-1", BaseURL: "https://attacker.example.com", TimeoutMS: 1000, InputLimit: 1024,
	})
	require.NoError(t, err)
	require.False(t, applied)
	require.Empty(t, mismatched.Token)
}

func newProbeTestService() *PromptService {
	return &PromptService{
		config: &ConfigManager{}, scanner: NewOpenAICompatibleScanner(), clock: realClock{},
		probes: map[string]ProbeResult{},
	}
}

func probeEndpoint(baseURL, token string) UpdateEndpoint {
	return UpdateEndpoint{
		ID: "probe-one", Name: "Probe One", Protocol: "openai_compatible", BaseURL: baseURL,
		Model: DefaultGuardModel, Token: token, TimeoutMS: 1000, InputLimit: 1024, Enabled: true,
	}
}
