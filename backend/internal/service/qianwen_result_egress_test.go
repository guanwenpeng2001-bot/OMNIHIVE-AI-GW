//go:build unit

package service

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/stretchr/testify/require"
)

const qianwenTranscriptTestHost = "dashscope-result-bj.oss-cn-beijing.aliyuncs.com"

// The proxy accepts only the validated public destination, then tunnels to a
// loopback TLS origin. No test changes system DNS, trust roots or proxy settings.
func qianwenResultProxyFixture(t *testing.T, host string, handler http.HandlerFunc) (*Proxy, *atomic.Int32) {
	t.Helper()
	origin, roots := qianwenLookupTLSServer(t, host, func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, host, request.Host)
		require.Equal(t, host, request.TLS.ServerName)
		for _, header := range []string{"Authorization", "Cookie", "Proxy-Authorization", "X-Provider-Secret"} {
			require.Empty(t, request.Header.Get(header), header)
		}
		handler(writer, request)
	})
	connections := &atomic.Int32{}
	proxyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connections.Add(1)
		require.Equal(t, http.MethodConnect, request.Method)
		require.Equal(t, "93.184.216.34:443", request.Host)
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-password")), request.Header.Get("Proxy-Authorization"))
		require.Empty(t, request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("Cookie"))
		upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
		require.NoError(t, err)
		defer upstream.Close()
		connection, buffered, err := writer.(http.Hijacker).Hijack()
		require.NoError(t, err)
		defer connection.Close()
		_, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		require.NoError(t, err)
		require.NoError(t, buffered.Flush())
		go func() { _, _ = io.Copy(upstream, buffered); _ = upstream.Close() }()
		_, _ = io.Copy(connection, upstream)
	}))
	t.Cleanup(proxyServer.Close)
	parsed, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	proxy := &Proxy{ID: 11, WorkspaceID: 1, Protocol: "http", Host: parsed.Hostname(), Port: port, Username: "proxy-user", Password: "proxy-password", Status: StatusActive}
	previousClient, previousLookup := qianwenResultProxyClient, qianwenResultSystemLookup
	t.Cleanup(func() { qianwenResultProxyClient, qianwenResultSystemLookup = previousClient, previousLookup })
	qianwenResultSystemLookup = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	qianwenResultProxyClient = func(options httpclient.Options) (*http.Client, error) {
		require.Equal(t, proxy.URL(), options.ProxyURL)
		require.True(t, options.ValidateResolvedIP)
		require.False(t, options.AllowPrivateHosts)
		require.False(t, options.InsecureSkipVerify)
		proxyURL, err := url.Parse(options.ProxyURL)
		require.NoError(t, err)
		// Only inject trust for the synthetic certificate; exercise real CONNECT,
		// public-IP pinning and hostname verification through the guarded transport.
		transport := httpclient.NewPublicTransport(&http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}, DisableKeepAlives: true})
		t.Cleanup(transport.CloseIdleConnections)
		return &http.Client{Transport: transport}, nil
	}
	return proxy, connections
}

func TestQianwenResultEgressSingleGroupAndDurablePin(t *testing.T) {
	for _, route := range []string{"single", "group", "durable"} {
		t.Run(route, func(t *testing.T) {
			proxy, connections := qianwenResultProxyFixture(t, qianwenLookupTestHost, func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, "private-signature", request.URL.Query().Get("Signature"))
				_, _ = io.WriteString(writer, "video")
			})
			account := qianwenTestAccount()
			account.ProxyID, account.Proxy = &proxy.ID, proxy
			account.Credentials["header_overrides"] = map[string]any{"Cookie": "provider-cookie", "X-Provider-Secret": "provider-secret"}
			if route != "single" {
				runtime, groups, cache, grouped := runtimeFixture()
				groups.group.Members = groups.group.Members[:1]
				groups.group.Members[0].Proxy = proxy
				grouped.AttachEgress(runtime)
				account = grouped
				_, err := account.EgressProxyURL(context.Background())
				require.NoError(t, err)
				if route == "durable" {
					snapshot, err := account.PinJobEgress(context.Background())
					require.NoError(t, err)
					cache.bindings[[2]int64{7, account.ID}] = 12
					account = &Account{ID: account.ID, WorkspaceID: 1, ProxyGroupID: account.ProxyGroupID}
					account.AttachEgress(runtime)
					require.NoError(t, account.RestoreJobEgress(context.Background(), snapshot))
				}
			}
			for repeat := 0; repeat < 2; repeat++ {
				response, err := OpenQianwenVideoContentForAccount(context.Background(), account, "https://"+qianwenLookupTestHost+"/video?Signature=private-signature")
				require.NoError(t, err)
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				require.Equal(t, "video", string(body))
			}
			require.EqualValues(t, 2, connections.Load())
		})
	}
}

func TestQianwenResultEgressProductionPoolReachesOnlySelectedProxy(t *testing.T) {
	proxy, connections := qianwenResultProxyFixture(t, qianwenLookupTestHost, func(http.ResponseWriter, *http.Request) {
		t.Error("untrusted synthetic certificate must prevent origin HTTP")
	})
	qianwenResultProxyClient = httpclient.GetClient
	account := &Account{ProxyID: &proxy.ID, Proxy: proxy}
	_, err := OpenQianwenVideoContentForAccount(context.Background(), account, "https://"+qianwenLookupTestHost+"/video?Signature=private-signature")
	require.EqualError(t, err, "video result unavailable")
	require.EqualValues(t, 1, connections.Load(), "production pool must actually use the selected proxy")
}

func TestQianwenResultEgressTTSUsesOperationAccount(t *testing.T) {
	wav := "RIFF\x04\x00\x00\x00WAVEfixture"
	proxy, connections := qianwenResultProxyFixture(t, qianwenTranscriptTestHost, func(writer http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(writer, wav) })
	runtime, groups, cache, account := runtimeFixture()
	groups.group.Members = groups.group.Members[:1]
	groups.group.Members[0].Proxy = proxy
	account.Platform, account.Type = PlatformQianwen, AccountTypeAPIKey
	account.Credentials = map[string]any{"api_key": "provider-secret"}
	account.AttachEgress(runtime)
	_, err := account.EgressProxyURL(context.Background())
	require.NoError(t, err)
	cache.bindings[[2]int64{7, account.ID}] = 12
	payload := strings.Replace(audioTTSResponse, "https://example.com/a.wav", "http://"+qianwenTranscriptTestHost+"/speech", 1)
	upstream := &httpUpstreamRecorder{resp: qianwenModelPage(payload, 200)}
	upstream.resp.Header.Set("Content-Type", "application/json")
	gateway := &OpenAIGatewayService{httpUpstream: upstream}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	ctx.Set("api_key", audioTestKey())
	result, err := gateway.ForwardQianwenAudioCompat(context.Background(), ctx, account, []byte(audioTTSRequest), "tts")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, wav, recorder.Body.String())
	require.Equal(t, proxy.URL(), upstream.lastProxyURL)
	require.EqualValues(t, 1, connections.Load())
}

func TestQianwenResultEgressASRRestoresJobRoute(t *testing.T) {
	proxy, connections := qianwenResultProxyFixture(t, qianwenTranscriptTestHost, func(writer http.ResponseWriter, _ *http.Request) {
		writer.(http.Flusher).Flush() // Unknown Content-Length remains supported.
		_, _ = io.WriteString(writer, `{"transcripts":[{"channel_id":0,"text":"done"}]}`)
	})
	runtime, groups, cache, account := runtimeFixture()
	groups.group.Members = groups.group.Members[:1]
	groups.group.Members[0].Proxy = proxy
	account.Platform, account.Type, account.Status = PlatformQianwen, AccountTypeAPIKey, StatusActive
	account.Credentials = map[string]any{"api_key": "provider-secret"}
	account.AttachEgress(runtime)
	snapshot, err := account.PinJobEgress(context.Background())
	require.NoError(t, err)
	cache.bindings[[2]int64{7, account.ID}] = 12
	account.AttachEgress(runtime)
	upstream := &httpUpstreamRecorder{resp: qianwenModelPage(`{"output":{"task_id":"task","task_status":"SUCCEEDED","results":[{"subtask_status":"SUCCEEDED","transcription_url":"https://`+qianwenTranscriptTestHost+`/transcript?Signature=private"}]}}`, 200)}
	gateway := &OpenAIGatewayService{httpUpstream: upstream, accountRepo: &fileBatchAccountRepo{account: account}}
	job := &UpstreamJob{Platform: PlatformQianwen, Family: "asr", AccountID: account.ID, UpstreamTaskID: "task", Billing: UpstreamJobBilling{Egress: snapshot}}
	update, err := gateway.PollQianwenAsyncJob(context.Background(), job)
	require.NoError(t, err)
	require.JSONEq(t, `[{"channel_id":0,"text":"done"}]`, update.ResultRef)
	require.Equal(t, proxy.URL(), upstream.lastProxyURL)
	require.EqualValues(t, 1, connections.Load())
	groups.group.Members = nil
	_, err = gateway.PollQianwenAsyncJob(context.Background(), job)
	require.ErrorIs(t, err, ErrProxyGroupUnavailable)
	require.Len(t, upstream.requests, 1)
	require.EqualValues(t, 1, connections.Load())
}

func TestQianwenResultEgressFailsClosedAndKeepsLegacyDirect(t *testing.T) {
	previous := qianwenVideoContentTransport
	t.Cleanup(func() { qianwenVideoContentTransport = previous })
	calls := 0
	qianwenVideoContentTransport = qianwenSpeechRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: 5, Body: io.NopCloser(strings.NewReader("video"))}, nil
	})
	nodeID, groupID := int64(11), int64(7)
	for _, account := range []*Account{nil, {ProxyID: &nodeID}, {ProxyID: &nodeID, Proxy: &Proxy{ID: nodeID, Status: "disabled"}}, {ProxyGroupID: &groupID}} {
		_, err := OpenQianwenVideoContentForAccount(context.Background(), account, "https://"+qianwenLookupTestHost+"/video")
		require.Error(t, err)
	}
	require.Zero(t, calls)
	account := &Account{}
	require.NoError(t, account.RestoreJobEgress(context.Background(), nil))
	response, err := OpenQianwenVideoContentForAccount(context.Background(), account, "https://"+qianwenLookupTestHost+"/video")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, calls)
}

func TestQianwenResultEgressGuardsBeforeProxyIO(t *testing.T) {
	proxy, connections := qianwenResultProxyFixture(t, qianwenLookupTestHost, func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", "https://"+qianwenLookupTestHost+"/other")
		writer.WriteHeader(http.StatusFound)
	})
	account := &Account{ProxyID: &proxy.ID, Proxy: proxy}
	_, err := OpenQianwenVideoContentForAccount(context.Background(), account, "https://"+qianwenLookupTestHost+"/video?Signature=private")
	require.EqualError(t, err, "video result HTTP 302")
	require.EqualValues(t, 1, connections.Load())
	qianwenResultSystemLookup = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("127.0.0.1")}, nil
	}
	_, err = OpenQianwenVideoContentForAccount(context.Background(), account, "https://"+qianwenLookupTestHost+"/video?Signature=private")
	require.EqualError(t, err, "video result unavailable")
	_, err = fetchQianwenTranscriptForAccount(context.Background(), account, "https://"+qianwenTranscriptTestHost+"/transcript?Signature=private")
	require.EqualError(t, err, "ASR result unavailable")
	_, err = OpenQianwenVideoContentForAccount(context.Background(), account, "https://"+qianwenLookupTestHost+".evil.test/video")
	require.EqualError(t, err, "unsupported video result origin")
	require.EqualValues(t, 1, connections.Load())
}

func TestQianwenResultEgressFakeIPFallbackStillPinsProxyDestination(t *testing.T) {
	proxy, connections := qianwenResultProxyFixture(t, qianwenLookupTestHost, func(writer http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(writer, "video") })
	queries := installQianwenFakeResultDNS(t)
	response, err := OpenQianwenVideoContentForAccount(context.Background(), &Account{ProxyID: &proxy.ID, Proxy: proxy}, "https://"+qianwenLookupTestHost+"/video?Signature=private")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 2, *queries)
	require.EqualValues(t, 1, connections.Load())
}

func TestQianwenResultEgressRejectsWrongTLSHostname(t *testing.T) {
	proxy, connections := qianwenResultProxyFixture(t, "wrong.example", func(http.ResponseWriter, *http.Request) { t.Error("wrong certificate must prevent HTTP") })
	_, err := OpenQianwenVideoContentForAccount(context.Background(), &Account{ProxyID: &proxy.ID, Proxy: proxy}, "https://"+qianwenLookupTestHost+"/video?Signature=private")
	require.EqualError(t, err, "video result unavailable")
	require.EqualValues(t, 1, connections.Load())
}

func TestQianwenTranscriptDownloadRetainsBodyLimitsAndLegacyRoute(t *testing.T) {
	previous := qianwenVideoContentTransport
	t.Cleanup(func() { qianwenVideoContentTransport = previous })
	for _, size := range []int{2 << 20, (2 << 20) + 1} {
		body := &qianwenVideoDownloadBody{Reader: strings.NewReader(strings.Repeat(" ", size))}
		qianwenVideoContentTransport = qianwenSpeechRoundTripper(func(request *http.Request) (*http.Response, error) {
			require.Empty(t, request.Header)
			return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: -1, Body: body}, nil
		})
		account := &Account{}
		require.NoError(t, account.RestoreJobEgress(context.Background(), nil))
		data, err := fetchQianwenTranscriptForAccount(context.Background(), account, "https://"+qianwenTranscriptTestHost+"/transcript")
		if size > 2<<20 {
			require.EqualError(t, err, "ASR result exceeds limit or is unreadable")
		} else {
			require.NoError(t, err)
			require.Len(t, data, size)
		}
		require.True(t, body.closed)
	}
	calls := 0
	qianwenVideoContentTransport = qianwenSpeechRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://" + qianwenTranscriptTestHost + "/other"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
	})
	_, err := fetchQianwenTranscriptForAccount(context.Background(), nil, "https://"+qianwenTranscriptTestHost+"/transcript")
	require.EqualError(t, err, "ASR result HTTP 302")
	require.Equal(t, 1, calls)
}
