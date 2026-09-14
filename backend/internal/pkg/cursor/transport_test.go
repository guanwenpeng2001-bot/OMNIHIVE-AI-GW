package cursor

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type forbiddenDirectClient struct{ calls atomic.Int32 }

type countingDirectTransport struct{ calls *atomic.Int32 }

func (transport countingDirectTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls.Add(1)
	return nil, io.EOF
}

func (client *forbiddenDirectClient) Do(*http.Request) (*http.Response, error) {
	client.calls.Add(1)
	return nil, io.EOF
}

func TestAccountProxyRoutesCallFamilies(t *testing.T) {
	var requests atomic.Int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		require.Equal(t, "Bearer synthetic-key", request.Header.Get("Authorization"))
		switch request.URL.Path {
		case "/auth/exchange_user_api_key":
			_, err := io.WriteString(writer, `{"accessToken":"session","expires_in":3600}`)
			require.NoError(t, err)
		default:
			writer.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer proxyServer.Close()
	ctx := WithAccountProxy(context.Background(), proxyServer.URL, false)
	session, err := ExchangeKey(ctx, &http.Client{Timeout: time.Second}, "http://unresolvable.invalid", "synthetic-key")
	require.NoError(t, err)
	require.Equal(t, "session", session.Token)
	require.EqualValues(t, 1, requests.Load())
}

func TestNativeProxyTunnelAndFailureNeverDirect(t *testing.T) {
	for _, family := range []string{"run", "models"} {
		t.Run(family, func(t *testing.T) {
			var tunnels atomic.Int32
			proxyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				tunnels.Add(1)
				require.Equal(t, http.MethodConnect, request.Method)
				require.Equal(t, "native.invalid:443", request.Host)
				require.Empty(t, request.Header.Get("Authorization"))
				writer.WriteHeader(http.StatusProxyAuthRequired)
			}))
			defer proxyServer.Close()
			var direct atomic.Int32
			client := NewClient()
			client.HTTPClient.Transport = countingDirectTransport{calls: &direct}
			client.BaseURL = "https://native.invalid"
			ctx := WithAccountProxy(context.Background(), proxyServer.URL, false)
			var err error
			if family == "run" {
				_, err = client.Run(ctx, Credentials{AccessToken: "secret"}, []ChatMessage{{Role: "user", Content: "hello"}}, "model", func(StreamEvent) error { return nil })
			} else {
				_, err = client.AvailableModels(ctx, Credentials{AccessToken: "secret"})
			}
			require.Error(t, err)
			require.EqualValues(t, 1, tunnels.Load())
			require.Zero(t, direct.Load())
		})
	}
}

func TestInvalidAccountProxyNeverCallsDirectClient(t *testing.T) {
	for _, scenario := range []struct {
		address string
		missing bool
	}{{"", true}, {"invalid://localhost:80", false}, {"http://", false}, {"http://127.0.0.1:1", false}} {
		client := &forbiddenDirectClient{}
		request, err := http.NewRequestWithContext(WithAccountProxy(context.Background(), scenario.address, scenario.missing), http.MethodGet, "http://example.invalid", nil)
		require.NoError(t, err)
		_, err = Do(client, request, false)
		require.Error(t, err)
		require.Zero(t, client.calls.Load())
	}
}

func TestProxyTunnelCancellation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	proxyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { close(entered); <-release }))
	defer proxyServer.Close()
	defer close(release)
	proxyURL, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		connection, err := dialAccountProxy(ctx, "tcp", "native.invalid:443", proxyURL)
		if connection != nil {
			_ = connection.Close()
		}
		done <- err
	}()
	defer func() { cancel(); <-exited }()
	// Dial can fail before the handler runs (for example, exhausted sockets).
	// Report that error instead of waiting for the package-wide test timeout.
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("proxy CONNECT returned before reaching handler: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("proxy CONNECT did not reach handler")
	}
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("proxy CONNECT ignored cancellation")
	}
}

func TestRelayProtectedHeaders(t *testing.T) {
	headers := make(http.Header)
	overrides := make(http.Header)
	for _, name := range strings.Fields("Authorization Proxy-Authorization X-Api-Key Host Content-Length Connection Proxy-Connection Keep-Alive Transfer-Encoding TE Trailer Upgrade Content-Type Accept Anthropic-Version") {
		overrides[name] = []string{"bad"}
	}
	overrides["x-route"] = []string{"tenant"}
	ApplyRelayHeaders(headers, overrides, "real-key")
	require.Equal(t, "tenant", headers.Get("X-Route"))
	require.Equal(t, "Bearer real-key", headers.Get("Authorization"))
	for name := range overrides {
		if name != "x-route" {
			require.NotEqual(t, "bad", headers.Get(name), name)
		}
	}
}

func TestConcurrentAccountProxyIsolation(t *testing.T) {
	var completed sync.WaitGroup
	for _, token := range []string{"alpha", "beta"} {
		token := token
		proxyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			require.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
			_, err := io.WriteString(writer, `{"accessToken":"`+token+`","expires_in":3600}`)
			require.NoError(t, err)
		}))
		defer proxyServer.Close()
		for range 10 {
			completed.Add(1)
			go func(address string) {
				defer completed.Done()
				ctx := WithAccountProxy(context.Background(), address, false)
				session, err := ExchangeKey(ctx, http.DefaultClient, "http://account.invalid", token)
				require.NoError(t, err)
				require.Equal(t, token, session.Token)
			}(proxyServer.URL)
		}
	}
	completed.Wait()
}

func TestSOCKSProxyRoutesNativeAndHTTP(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native=%t", native), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer func() { _ = listener.Close() }()
			requested := make(chan string, 1)
			go func() {
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				defer func() { _ = connection.Close() }()
				if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
					t.Error(err)
					return
				}
				greeting := make([]byte, 2)
				if _, err := io.ReadFull(connection, greeting); err != nil {
					return
				}
				methods := make([]byte, int(greeting[1]))
				if _, err := io.ReadFull(connection, methods); err != nil {
					return
				}
				if _, err := connection.Write([]byte{5, 0}); err != nil {
					t.Error(err)
					return
				}
				request := make([]byte, 5)
				if _, err := io.ReadFull(connection, request); err != nil {
					return
				}
				if request[3] != 3 {
					requested <- "unexpected local DNS"
					return
				}
				target := make([]byte, int(request[4])+2)
				if _, err := io.ReadFull(connection, target); err != nil {
					return
				}
				requested <- string(target[:len(target)-2])
				if _, err := connection.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
					t.Error(err)
					return
				}
			}()
			ctx := WithAccountProxy(context.Background(), "socks5h://"+listener.Addr().String(), false)
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://account.invalid/usage", nil)
			require.NoError(t, err)
			client := &forbiddenDirectClient{}
			_, err = Do(client, request, native)
			require.Error(t, err)
			require.Zero(t, client.calls.Load())
			select {
			case target := <-requested:
				require.Equal(t, "account.invalid", target)
			case <-time.After(time.Second):
				t.Fatal("SOCKS proxy did not receive target")
			}
		})
	}
}
