package repository

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// Exercise both public entry points. The TLS case uses the test server's trusted
// transport so the test remains local and does not depend on system trust roots.
func lifecycleClient(t *testing.T, srv *httptest.Server, withTLS bool) (*httpUpstreamService, func(*http.Request) (*http.Response, error)) {
	t.Helper()
	upstreamService, ok := NewHTTPUpstream(loopbackUpstreamConfig()).(*httpUpstreamService)
	require.True(t, ok)
	profile := &tlsfingerprint.Profile{Name: "body-lifecycle-test"}
	var entry *upstreamClientEntry
	var err error
	if withTLS {
		entry, err = upstreamService.getClientEntryWithTLS("", 1, 1, profile, service.HTTPUpstreamProfileDefault, false, true)
	} else {
		entry, err = upstreamService.acquireClientWithProfile("", 1, 1, service.HTTPUpstreamProfileDefault)
		if err == nil {
			atomic.AddInt64(&entry.inFlight, -1)
		}
	}
	require.NoError(t, err)
	baseTransport, ok := srv.Client().Transport.(*http.Transport)
	require.True(t, ok)
	tr := baseTransport.Clone()
	tr.MaxConnsPerHost = 1
	tr.MaxIdleConnsPerHost = 1
	tr.IdleConnTimeout = 10 * time.Second
	entry.client = &http.Client{Transport: tr}
	t.Cleanup(tr.CloseIdleConnections)
	return upstreamService, func(req *http.Request) (*http.Response, error) {
		if withTLS {
			return upstreamService.DoWithTLS(req, "", 1, 1, profile)
		}
		return upstreamService.Do(req, "", 1, 1)
	}
}

func requireNoUpstreamInFlight(t *testing.T, upstreamService *httpUpstreamService) {
	t.Helper()
	upstreamService.mu.RLock()
	defer upstreamService.mu.RUnlock()
	for _, entry := range upstreamService.clients {
		require.Zero(t, atomic.LoadInt64(&entry.inFlight))
	}
}

type notifyReadCloser struct {
	io.ReadCloser
	started chan<- struct{}
	once    sync.Once
}

func (request *notifyReadCloser) Read(buffer []byte) (int, error) {
	request.once.Do(func() { close(request.started) })
	return request.ReadCloser.Read(buffer)
}

func TestHTTPUpstreamConcurrentEarlyCloseDoesNotPoisonNextResponse(t *testing.T) {
	for _, withTLS := range []bool{false, true} {
		t.Run(fmt.Sprintf("tls=%v", withTLS), func(t *testing.T) {
			finishFirst := make(chan struct{})
			firstFinished := make(chan struct{})
			var requests atomic.Int32
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
				responseWriter.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(responseWriter, "data: begin\n\n")
				_ = http.NewResponseController(responseWriter).Flush()
				if requests.Add(1) == 1 {
					defer close(firstFinished)
					select {
					case <-finishFirst:
					case <-request.Context().Done():
						return
					}
				}
				_, _ = io.WriteString(responseWriter, "data: complete\n\n")
			}))
			if withTLS {
				srv.StartTLS()
			} else {
				srv.Start()
			}
			defer srv.Close()
			defer srv.CloseClientConnections()
			upstreamService, do := lifecycleClient(t, srv, withTLS)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			require.NoError(t, err)
			resp, err := do(req)
			require.NoError(t, err)
			prefix := make([]byte, len("data: begin\n\n"))
			_, err = io.ReadFull(resp.Body, prefix)
			require.NoError(t, err)
			readerDone := make(chan struct{})
			readStarted := make(chan struct{})
			resp.Body = &notifyReadCloser{ReadCloser: resp.Body, started: readStarted}
			go func() { _, _ = io.Copy(io.Discard, resp.Body); close(readerDone) }()
			// Start Close only after the background reader has entered Read;
			// the server is still holding the response open at this point.
			<-readStarted
			closed := make(chan error, 1)
			go func() { closed <- resp.Body.Close() }()
			select {
			case err := <-closed:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("Close blocked behind the active reader")
			}
			// Let the early-close drain finish only after Close has returned.
			// Go 1.27 can reuse that connection while its old reader awaits EOF.
			close(finishFirst)
			<-firstFinished
			for iteration := 0; iteration < 8; iteration++ {
				resp, err := do(req)
				require.NoError(t, err)
				done := make(chan error, 1)
				go func() { _, err := io.Copy(io.Discard, resp.Body); _ = resp.Body.Close(); done <- err }()
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("response EOF waits for a subsequent request")
				}
			}
			select {
			case <-readerDone:
			case <-time.After(5 * time.Second):
				t.Fatal("early-closed reader leaked")
			}
			require.NoError(t, req.Context().Err(), "closing an attempt must not cancel its caller or retries")
			requireNoUpstreamInFlight(t, upstreamService)
		})
	}
}

func TestHTTPUpstreamCompletedBodyPreservesKeepAlive(t *testing.T) {
	for _, encoding := range []string{"", "gzip"} {
		t.Run("encoding="+encoding, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
				if encoding == "gzip" {
					responseWriter.Header().Set("Content-Encoding", encoding)
					zw := gzip.NewWriter(responseWriter)
					_, _ = io.WriteString(zw, "complete response")
					_ = zw.Close()
					return
				}
				_, _ = io.WriteString(responseWriter, "complete response")
			}))
			defer srv.Close()
			defer srv.CloseClientConnections()
			upstreamService, do := lifecycleClient(t, srv, false)
			for iteration := 0; iteration < 20; iteration++ {
				var reused bool
				ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }})
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
				require.NoError(t, err)
				req.Header.Set("Accept-Encoding", encoding)
				resp, err := do(req)
				require.NoError(t, err)
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Equal(t, "complete response", string(body))
				var wg sync.WaitGroup
				for j := 0; j < 4; j++ {
					wg.Add(1)
					go func() { defer wg.Done(); _ = resp.Body.Close() }()
				}
				wg.Wait()
				if iteration > 0 {
					require.True(t, reused, "fully consumed responses must keep their connection")
				}
			}
			requireNoUpstreamInFlight(t, upstreamService)
		})
	}
}

func TestHTTPUpstreamCancellationReleasesAttempt(t *testing.T) {
	for _, beforeHeaders := range []bool{false, true} {
		t.Run(fmt.Sprintf("before_headers=%v", beforeHeaders), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
				if !beforeHeaders {
					responseWriter.WriteHeader(http.StatusOK)
					_ = http.NewResponseController(responseWriter).Flush()
				}
				<-request.Context().Done()
			}))
			defer srv.Close()
			defer srv.CloseClientConnections()
			upstreamService, do := lifecycleClient(t, srv, false)
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			require.NoError(t, err)
			resp, err := do(req)
			if beforeHeaders {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			} else {
				require.NoError(t, err)
				_, err = io.ReadAll(resp.Body)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.NoError(t, resp.Body.Close())
			}
			requireNoUpstreamInFlight(t, upstreamService)
		})
	}
}

func TestHTTPUpstreamCompressedEarlyCloseStopsReader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(responseWriter)
		_, _ = io.WriteString(zw, "data: begin\n\n")
		_ = zw.Flush()
		_ = http.NewResponseController(responseWriter).Flush()
		<-request.Context().Done()
		_ = zw.Close()
	}))
	defer srv.Close()
	defer srv.CloseClientConnections()
	upstreamService, do := lifecycleClient(t, srv, false)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := do(req)
	require.NoError(t, err)
	prefix := make([]byte, len("data: begin\n\n"))
	_, err = io.ReadFull(resp.Body, prefix)
	require.NoError(t, err)
	done := make(chan error, 1)
	readStarted := make(chan struct{})
	resp.Body = &notifyReadCloser{ReadCloser: resp.Body, started: readStarted}
	go func() { _, err := io.Copy(io.Discard, resp.Body); done <- err }()
	<-readStarted
	closed := make(chan error, 1)
	go func() { closed <- resp.Body.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("compressed body Close blocked")
	}
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("compressed body reader leaked")
	}
	requireNoUpstreamInFlight(t, upstreamService)
}

func TestHTTPUpstreamHTTP2CloseDoesNotCancelOtherStream(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/held" {
			responseWriter.WriteHeader(http.StatusOK)
			_ = http.NewResponseController(responseWriter).Flush()
			<-request.Context().Done()
			return
		}
		_, _ = io.WriteString(responseWriter, "complete response")
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	defer srv.CloseClientConnections()
	upstreamService, do := lifecycleClient(t, srv, false)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/held", nil)
	require.NoError(t, err)
	held, err := do(req)
	require.NoError(t, err)
	require.Equal(t, 2, held.ProtoMajor)
	for iteration := 0; iteration < 2; iteration++ {
		var reused bool
		ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }})
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		require.NoError(t, err)
		resp, err := do(req)
		require.NoError(t, err)
		if iteration == 0 {
			require.NoError(t, held.Body.Close())
		}
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "complete response", string(body))
		require.NoError(t, resp.Body.Close())
		require.True(t, reused, "closing one HTTP/2 stream must retain the shared connection")
	}
	requireNoUpstreamInFlight(t, upstreamService)
}

func TestHTTPUpstreamDetachedRequestDrainsAfterClientDisconnect(t *testing.T) {
	disconnectCtx, disconnect := context.WithCancel(t.Context())
	defer disconnect()
	srv := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(responseWriter, "begin ")
		_ = http.NewResponseController(responseWriter).Flush()
		<-disconnectCtx.Done()
		_, _ = io.WriteString(responseWriter, "usage and completion")
	}))
	defer srv.Close()
	defer srv.CloseClientConnections()
	upstreamService, do := lifecycleClient(t, srv, false)
	req, err := http.NewRequestWithContext(context.WithoutCancel(disconnectCtx), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := do(req)
	require.NoError(t, err)
	disconnect()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "begin usage and completion", string(body))
	require.NoError(t, resp.Body.Close())
	requireNoUpstreamInFlight(t, upstreamService)
}

// Keep the same benchmark runnable against the base revision: it measures a
// complete public Do/read/Close lifecycle without network latency hiding the
// per-attempt context and per-Read synchronization costs.
func BenchmarkHTTPUpstreamBodyLifecycle(benchmark *testing.B) {
	for _, size := range []int{256, 64 * 1024} {
		benchmark.Run(fmt.Sprintf("bytes=%d", size), func(benchmark *testing.B) {
			upstreamService, ok := NewHTTPUpstream(loopbackUpstreamConfig()).(*httpUpstreamService)
			require.True(benchmark, ok)
			entry, err := upstreamService.getOrCreateClient("", 1, 1)
			require.NoError(benchmark, err)
			payload := strings.Repeat("x", size)
			entry.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(payload)),
				}, nil
			})}
			req, err := http.NewRequestWithContext(benchmark.Context(), http.MethodGet, "https://example.com", nil)
			require.NoError(benchmark, err)
			buf := make([]byte, 256)
			benchmark.ReportAllocs()
			benchmark.SetBytes(int64(size))
			benchmark.ResetTimer()
			for benchmark.Loop() {
				resp, err := upstreamService.Do(req, "", 1, 1)
				if err != nil {
					benchmark.Fatal(err)
				}
				for {
					_, err = resp.Body.Read(buf)
					if err != nil {
						break
					}
				}
				if err != io.EOF {
					benchmark.Fatal(err)
				}
				if err := resp.Body.Close(); err != nil {
					benchmark.Fatal(err)
				}
			}
		})
	}
}
