//go:build unit

package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func installQianwenFakeResultDNS(t *testing.T) *int {
	return installQianwenFakeResultDNSForHost(t, qianwenLookupTestHost)
}

func installQianwenFakeResultDNSForHost(t *testing.T, host string) *int {
	t.Helper()
	previousLookup, previousClient := qianwenResultSystemLookup, qianwenResultDNSClient
	t.Cleanup(func() { qianwenResultSystemLookup, qianwenResultDNSClient = previousLookup, previousClient })
	qianwenResultSystemLookup = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("198.18.0.10"), net.ParseIP("fdfe:dcba:9876::7")}, nil
	}
	queries := 0
	client := *previousClient
	client.Transport = qianwenSpeechRoundTripper(func(request *http.Request) (*http.Response, error) {
		queries++
		require.Equal(t, host, request.URL.Query().Get("name"))
		require.Len(t, request.URL.Query(), 2, "never send the signed media URL to DNS")
		require.Equal(t, http.Header{"Accept": []string{"application/dns-json"}}, request.Header)
		body := `{"Status":0,"Answer":[{"type":1,"data":"93.184.216.34"}]}`
		if request.URL.Query().Get("type") == "AAAA" {
			body = `{"Status":0,"Answer":[]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	qianwenResultDNSClient = &client
	return &queries
}

func TestQianwenResultLookupOnlyAttachesToProviderOutputURLs(t *testing.T) {
	for _, address := range []string{"https://example.com/image.png", "https://" + qianwenLookupTestHost + ".evil.test/image.png", "https://198.18.0.10/image.png", "https://user@" + qianwenLookupTestHost + "/image.png", "https://" + qianwenLookupTestHost + ":8443/image.png", "data:image/png;base64,AA=="} {
		ctx := withQianwenResultLookup(context.Background(), address)
		require.False(t, httpclient.HasPublicIPLookup(ctx), address)
	}
	ctx := withQianwenResultLookup(context.Background(), "https://"+qianwenLookupTestHost+"/image.png?Signature=private-fixture")
	require.True(t, httpclient.HasPublicIPLookup(ctx))
}

func TestQianwenResultLookupVideoAndSpeechUseValidatedPinnedIPs(t *testing.T) {
	for _, speech := range []bool{false, true} {
		name := "video"
		if speech {
			name = "speech"
		}
		t.Run(name, func(t *testing.T) {
			queries := installQianwenFakeResultDNS(t)
			payload := []byte("synthetic-ftyp-video")
			if speech {
				payload = []byte("RIFF\x04\x00\x00\x00WAVEfixture")
			}
			server, roots := qianwenLookupTLSServer(t, qianwenLookupTestHost, func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, qianwenLookupTestHost, request.Host)
				require.Equal(t, qianwenLookupTestHost, request.TLS.ServerName)
				require.Equal(t, "private-fixture", request.URL.Query().Get("Signature"))
				require.Empty(t, request.Header.Get("Authorization"))
				require.Empty(t, request.Header.Get("Cookie"))
				_, _ = writer.Write(payload)
			})
			transport := httpclient.NewPublicTransport(&http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				require.Equal(t, "93.184.216.34:443", address)
				require.Equal(t, qianwenLookupTestHost, httpclient.OriginalHostname(ctx))
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}})
			defer transport.CloseIdleConnections()
			previous := qianwenVideoContentTransport
			qianwenVideoContentTransport = transport
			t.Cleanup(func() { qianwenVideoContentTransport = previous })
			address := "https://" + qianwenLookupTestHost + "/output?Signature=private-fixture"
			var data []byte
			var err error
			if speech {
				data, err = downloadQianwenSpeechForAccount(context.Background(), nil, address)
			} else {
				response, openErr := OpenQianwenVideoContent(context.Background(), address)
				require.NoError(t, openErr)
				defer response.Body.Close()
				data, err = io.ReadAll(response.Body)
			}
			require.NoError(t, err)
			require.Equal(t, payload, data)
			require.Equal(t, 2, *queries)
		})
	}
}

type qianwenResultImageUpstream struct {
	HTTPUpstream
	test      *testing.T
	wantProxy string
	downloads int
}

func (upstream *qianwenResultImageUpstream) Do(request *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
	if request.Method != http.MethodGet {
		require.False(upstream.test, httpclient.HasPublicIPLookup(request.Context()), "authenticated generation request must not get output resolver")
		return qianwenModelPage(`{"output":{"choices":[{"message":{"content":[{"image":"https://`+qianwenLookupTestHost+`/image.png?Signature=private-fixture"}]}}]},"usage":{"image_count":1}}`, 200), nil
	}
	upstream.downloads++
	require.Equal(upstream.test, upstream.wantProxy, proxyURL, "retain existing account-selected media route")
	require.True(upstream.test, HTTPUpstreamPublicHostsOnly(request.Context()))
	require.Empty(upstream.test, request.Header.Get("Authorization"))
	dialed := false
	stop := errors.New("synthetic dial boundary")
	transport := httpclient.NewPublicTransport(&http.Transport{DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = true
		require.Equal(upstream.test, "93.184.216.34:443", address)
		return nil, stop
	}})
	defer transport.CloseIdleConnections()
	_, err := transport.RoundTrip(request)
	require.ErrorIs(upstream.test, err, stop)
	require.True(upstream.test, dialed)
	return b64BackfillImageResponse(200, "image/png", b64BackfillPNGBytes), nil
}

func TestQianwenResultLookupImageGenerationAndEditKeepAccountRoute(t *testing.T) {
	for _, edit := range []bool{false, true} {
		for _, proxy := range []bool{false, true} {
			queries := installQianwenFakeResultDNS(t)
			account := qianwenTestAccount()
			upstream := &qianwenResultImageUpstream{test: t}
			if proxy {
				account.ProxyID = i64p(9)
				account.Proxy = &Proxy{Protocol: "http", Host: "proxy.invalid", Port: 8080}
				upstream.wantProxy = account.Proxy.URL()
			}
			gateway := &OpenAIGatewayService{httpUpstream: upstream}
			writer := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(writer)
			ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			var result *OpenAIForwardResult
			var err error
			if edit {
				body, buildErr := BuildQianwenImageEditBody("qwen-image-edit-plus", "edit", "", 1, []string{"data:image/png;base64,iVBORw0KGgoAAAAA"})
				require.NoError(t, buildErr)
				result, err = gateway.ForwardQianwenImageEdit(context.Background(), ginContext, account, body)
			} else {
				result, err = gateway.ForwardQianwenNativeImages(context.Background(), ginContext, account, []byte(`{"model":"qwen-image-plus","input":{"messages":[{"role":"user","content":[{"text":"draw"}]}]},"parameters":{"n":1}}`), "b64_json")
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 1, upstream.downloads)
			require.Equal(t, 2, *queries)
			require.Equal(t, base64.StdEncoding.EncodeToString(b64BackfillPNGBytes), gjson.GetBytes(writer.Body.Bytes(), "data.0.b64_json").String())
		}
	}
}

func qianwenLookupTLSServer(t *testing.T, host string, handler http.HandlerFunc) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	certificate, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, roots
}

func TestQianwenResultLookupHTTPSBootstrapAndTLS(t *testing.T) {
	for _, certificateHost := range []string{"cloudflare-dns.com", "wrong.example"} {
		t.Run(certificateHost, func(t *testing.T) {
			requests := 0
			server, roots := qianwenLookupTLSServer(t, certificateHost, func(writer http.ResponseWriter, request *http.Request) {
				requests++
				require.Equal(t, "cloudflare-dns.com", request.Host)
				require.Equal(t, "cloudflare-dns.com", request.TLS.ServerName)
				require.Equal(t, qianwenLookupTestHost, request.URL.Query().Get("name"))
				require.Len(t, request.URL.Query(), 2)
				require.Empty(t, request.Header.Get("Authorization"))
				require.Empty(t, request.Header.Get("Cookie"))
				require.Empty(t, request.Header.Get("Proxy-Authorization"))
				_, _ = io.WriteString(writer, `{"Status":0,"Answer":[{"type":1,"data":"93.184.216.34"}]}`)
			})
			client := newQianwenResultDNSClient(func(ctx context.Context, network, address string) (net.Conn, error) {
				require.Equal(t, "1.1.1.1:443", address, "bootstrap must never resolve the DoH hostname")
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			})
			transport := client.Transport.(*http.Transport)
			require.Nil(t, transport.Proxy)
			require.False(t, transport.TLSClientConfig.InsecureSkipVerify)
			transport.TLSClientConfig.RootCAs = roots
			defer transport.CloseIdleConnections()
			previousLookup, previousClient := qianwenResultSystemLookup, qianwenResultDNSClient
			t.Cleanup(func() { qianwenResultSystemLookup, qianwenResultDNSClient = previousLookup, previousClient })
			qianwenResultSystemLookup = func(context.Context, string, string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("198.18.0.10")}, nil
			}
			qianwenResultDNSClient = client
			ips, err := lookupQianwenResultIP(context.Background(), "ip", qianwenLookupTestHost)
			if certificateHost == "cloudflare-dns.com" {
				require.NoError(t, err)
				require.Len(t, ips, 2)
				require.Equal(t, 2, requests)
			} else {
				require.Error(t, err)
				require.Empty(t, ips)
				require.Zero(t, requests)
			}
		})
	}
}

const qianwenLookupTestHost = "dashscope-a717.oss-accelerate.aliyuncs.com"

func TestQianwenResultLookupRegionalEditHost(t *testing.T) {
	const host = "dashscope-a717.oss-cn-beijing.aliyuncs.com"
	queries := installQianwenFakeResultDNSForHost(t, host)
	ctx := withQianwenResultLookup(context.Background(), "https://"+host+"/synthetic.png?Signature=synthetic")
	require.True(t, httpclient.HasPublicIPLookup(ctx), "actual edit result host must receive the scoped lookup")
	ips, err := lookupQianwenResultIP(ctx, "ip", host)
	require.NoError(t, err)
	require.Equal(t, []net.IP{net.ParseIP("93.184.216.34")}, ips)
	require.Equal(t, 2, *queries)
}

type qianwenResultLifecycleUpstream struct {
	*qianwenResultImageUpstream
}

type qianwenResultLifecycleBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (body *qianwenResultLifecycleBody) Close() error {
	body.cancel()
	return body.ReadCloser.Close()
}

func (upstream *qianwenResultLifecycleUpstream) Do(request *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	if request.Method == http.MethodGet {
		require.NoError(upstream.test, request.Context().Err(), "native response close must not cancel media delivery")
		budget := AttemptBudgetFromContext(request.Context())
		require.NotNil(upstream.test, budget)
		require.NoError(upstream.test, budget.root.Err())
	}
	// Match repository request/response cancellation around both generation and
	// download, including cancellation when the native response body is closed.
	ctx, cancel := AttemptExecutionContext(request.Context())
	response, err := upstream.qianwenResultImageUpstream.Do(request.WithContext(ctx), proxy, accountID, concurrency)
	if err != nil || response == nil || response.Body == nil {
		cancel()
		return response, err
	}
	response.Body = &qianwenResultLifecycleBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

func TestQianwenResultLookupPostNativeForwardAttemptContext(t *testing.T) {
	queries := installQianwenFakeResultDNS(t)
	upstream := &qianwenResultLifecycleUpstream{qianwenResultImageUpstream: &qianwenResultImageUpstream{test: t}}
	gateway := &OpenAIGatewayService{httpUpstream: upstream}
	writer := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(writer)
	ctx, cancel := WithAttemptBudget(context.Background(), 1, time.Minute)
	defer cancel()
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil).WithContext(ctx)
	body, err := BuildQianwenImageEditBody("qwen-image-edit-plus", "edit", "", 1, []string{"data:image/png;base64,iVBORw0KGgoAAAAA"})
	require.NoError(t, err)
	result, err := gateway.ForwardQianwenImageEdit(ctx, ginContext, qianwenTestAccount(), body)
	require.NoError(t, err)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, http.StatusOK, writer.Code)
	require.Equal(t, 1, upstream.downloads)
	require.Equal(t, 2, *queries)
	require.NoError(t, ctx.Err())
	require.Equal(t, base64.StdEncoding.EncodeToString(b64BackfillPNGBytes), gjson.GetBytes(writer.Body.Bytes(), "data.0.b64_json").String())
}

func TestQianwenResultLookupSystemAnswerPolicy(t *testing.T) {
	for _, scenario := range []struct {
		name, host string
		addresses  []string
		wantError  bool
		queries    int
	}{
		{"normal", qianwenLookupTestHost, []string{"93.184.216.34"}, false, 0},
		{"normal_ipv6", qianwenLookupTestHost, []string{"2001:4860:4860::8888"}, false, 0},
		{"empty", qianwenLookupTestHost, nil, true, 0},
		{"fake_and_invalid", qianwenLookupTestHost, []string{"198.18.0.10", "invalid"}, true, 0},
		{"only_known_fake", qianwenLookupTestHost, []string{"198.18.0.10", "fdfe:dcba:9876::7"}, false, 2},
		{"ipv6_fake_only", qianwenLookupTestHost, []string{"fdfe:dcba:9876::7"}, false, 2},
		{"fake_and_public", qianwenLookupTestHost, []string{"198.18.0.10", "93.184.216.34"}, false, 2},
		{"expanded_ipv4_fake", qianwenLookupTestHost, []string{"198.19.0.10"}, false, 2},
		{"expanded_ipv6_fake", qianwenLookupTestHost, []string{"2001:2::10"}, false, 2},
		{"expanded_ipv4_mapped_fake", qianwenLookupTestHost, []string{"::ffff:198.19.0.10"}, false, 2},
		{"fake_and_private", qianwenLookupTestHost, []string{"198.18.0.10", "10.0.0.1"}, true, 0},
		{"private_only", qianwenLookupTestHost, []string{"127.0.0.1"}, true, 0},
		{"other_private_prefix", qianwenLookupTestHost, []string{"fdfe:dcba:9876:1::7"}, true, 0},
		{"outside_expanded_fake_ipv4", qianwenLookupTestHost, []string{"198.20.0.10"}, false, 0},
		{"fake_and_cgnat", qianwenLookupTestHost, []string{"198.18.0.10", "100.64.0.1"}, true, 0},
		{"fake_and_documentation", qianwenLookupTestHost, []string{"198.18.0.10", "203.0.113.1"}, true, 0},
		{"public_nat64", qianwenLookupTestHost, []string{"64:ff9b::808:808"}, false, 0},
		{"spoof", qianwenLookupTestHost + ".evil.test", []string{"198.18.0.10"}, true, 0},
		{"literal", "198.18.0.10", []string{"198.18.0.10"}, true, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			previousLookup, previousClient := qianwenResultSystemLookup, qianwenResultDNSClient
			t.Cleanup(func() { qianwenResultSystemLookup, qianwenResultDNSClient = previousLookup, previousClient })
			qianwenResultSystemLookup = func(context.Context, string, string) ([]net.IP, error) {
				var ips []net.IP
				for _, address := range scenario.addresses {
					ips = append(ips, net.ParseIP(address))
				}
				return ips, nil
			}
			queries := 0
			client := *previousClient
			client.Transport = qianwenSpeechRoundTripper(func(request *http.Request) (*http.Response, error) {
				queries++
				require.Equal(t, "cloudflare-dns.com", request.URL.Host)
				require.Equal(t, "/dns-query", request.URL.Path)
				require.Equal(t, "https", request.URL.Scheme)
				require.Equal(t, scenario.host, request.URL.Query().Get("name"))
				require.Len(t, request.URL.Query(), 2)
				require.Equal(t, http.Header{"Accept": []string{"application/dns-json"}}, request.Header)
				deadline, ok := request.Context().Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
				body := `{"Status":0,"Answer":[{"type":1,"data":"93.184.216.34"}]}`
				if request.URL.Query().Get("type") == "AAAA" {
					body = `{"Status":0,"Answer":[]}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			qianwenResultDNSClient = &client
			ips, err := lookupQianwenResultIP(context.Background(), "ip", scenario.host)
			if scenario.wantError {
				require.Error(t, err)
				require.Empty(t, ips)
			} else {
				require.NoError(t, err)
				require.Len(t, ips, 1)
			}
			require.Equal(t, scenario.queries, queries)
		})
	}
}

func TestQianwenResultLookupRejectsUnsafeDoH(t *testing.T) {
	for _, body := range []string{
		`{"Status":0,"Answer":[{"type":1,"data":"93.184.216.34"},{"type":1,"data":"10.0.0.1"}]}`,
		`{"Status":0,"Answer":[{"type":1,"data":"198.18.0.10"}]}`,
		`{"Status":0,"Answer":[{"type":1,"data":"198.19.0.10"}]}`,
		`{"Status":0,"Answer":[{"type":1,"data":"100.64.0.1"}]}`,
		`{"Status":0,"Answer":[{"type":28,"data":"64:ff9b::7f00:1"}]}`,
		`{"Status":0,"Answer":[{"type":28,"data":"fdfe:dcba:9876::7"}]}`,
		`{"Status":0,"Answer":[{"type":1,"data":"not-an-ip"}]}`,
		`{"Status":0,"Answer":[{"type":1,"data":"224.0.0.1"}]}`,
		strings.Repeat(" ", 9<<10) + `{"Status":0,"Answer":[{"type":1,"data":"93.184.216.34"}]}`, // Combined A/AAAA body budget exceeds 16 KiB.
		`{"Status":0,"Answer":[]}`, `{"Status":2}`, `{}`, `{"Status":0,"TC":true}`, `broken`, strings.Repeat(" ", (16<<10)+1),
	} {
		previousLookup, previousClient := qianwenResultSystemLookup, qianwenResultDNSClient
		qianwenResultSystemLookup = func(context.Context, string, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("198.18.0.10")}, nil
		}
		client := *previousClient
		client.Transport = qianwenSpeechRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		qianwenResultDNSClient = &client
		ips, err := lookupQianwenResultIP(context.Background(), "ip", qianwenLookupTestHost)
		qianwenResultSystemLookup, qianwenResultDNSClient = previousLookup, previousClient
		require.Error(t, err)
		require.Empty(t, ips, "never retain a good subset or fall back to the FakeIP")
	}
}

func TestQianwenResultLookupErrorsRedirectsAndCancellation(t *testing.T) {
	for _, mode := range []string{"system_error", "http_error", "redirect", "transport_error", "cancel", "deadline", "late_private"} {
		t.Run(mode, func(t *testing.T) {
			previousLookup, previousClient := qianwenResultSystemLookup, qianwenResultDNSClient
			t.Cleanup(func() { qianwenResultSystemLookup, qianwenResultDNSClient = previousLookup, previousClient })
			qianwenResultSystemLookup = func(context.Context, string, string) ([]net.IP, error) {
				if mode == "system_error" {
					return nil, errors.New("synthetic DNS failure")
				}
				return []net.IP{net.ParseIP("198.18.0.10")}, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer deadlineCancel()
			}
			queries := 0
			client := *previousClient
			client.Transport = qianwenSpeechRoundTripper(func(request *http.Request) (*http.Response, error) {
				queries++
				if mode == "deadline" {
					<-request.Context().Done()
					return nil, request.Context().Err()
				}
				if mode == "late_private" {
					body := `{"Status":0,"Answer":[{"type":1,"data":"93.184.216.34"}]}`
					if request.URL.Query().Get("type") == "AAAA" {
						body = `{"Status":0,"Answer":[{"type":28,"data":"fc00::1"}]}`
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
				}
				if mode == "cancel" {
					cancel()
					return nil, request.Context().Err()
				}
				if mode == "transport_error" {
					return nil, errors.New("synthetic DoH failure")
				}
				status := 500
				if mode == "redirect" {
					status = 302
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://evil.test/"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})
			qianwenResultDNSClient = &client
			ips, err := lookupQianwenResultIP(ctx, "ip", qianwenLookupTestHost)
			require.Error(t, err)
			require.Empty(t, ips)
			if mode == "system_error" {
				require.Zero(t, queries)
			} else if mode == "late_private" {
				require.Equal(t, 2, queries)
			} else {
				require.Equal(t, 1, queries)
			}
			if mode == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			}
			if mode == "deadline" {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
		})
	}
}
