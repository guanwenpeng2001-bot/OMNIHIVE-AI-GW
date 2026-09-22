package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestReadOpenAIWSClientMessage_ControlCloseFrames(t *testing.T) {
	tests := []struct {
		name          string
		timeout       time.Duration
		timeoutStatus coderws.StatusCode
		timeoutReason string
		cancelCause   error
		wantStatus    coderws.StatusCode
		wantReason    string
	}{
		{
			name:          "inter-turn idle sends normal close",
			timeout:       25 * time.Millisecond,
			timeoutStatus: coderws.StatusNormalClosure,
			timeoutReason: "websocket idle timeout",
			wantStatus:    coderws.StatusNormalClosure,
			wantReason:    "websocket idle timeout",
		},
		{
			name:          "first message timeout sends policy close",
			timeout:       25 * time.Millisecond,
			timeoutStatus: coderws.StatusPolicyViolation,
			timeoutReason: "missing first response.create message",
			wantStatus:    coderws.StatusPolicyViolation,
			wantReason:    "missing first response.create message",
		},
		{
			name:        "lease loss sends retry close",
			cancelCause: ErrOpenAIWSIngressLeaseLost,
			wantStatus:  coderws.StatusTryAgainLater,
			wantReason:  "websocket ingress capacity lease lost; please reconnect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controlCtx, cancelControl := context.WithCancelCause(context.Background())
			defer cancelControl(context.Canceled)
			serverResult := make(chan error, 1)
			readStarted := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					serverResult <- err
					return
				}
				defer func() { _ = conn.CloseNow() }()
				close(readStarted)
				_, _, err = ReadOpenAIWSClientMessage(
					controlCtx,
					conn,
					tt.timeout,
					tt.timeoutStatus,
					tt.timeoutReason,
				)
				serverResult <- err
			}))
			defer server.Close()

			dialCtx, cancelDial := context.WithTimeout(context.Background(), time.Second)
			clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			cancelDial()
			require.NoError(t, err)
			defer func() { _ = clientConn.CloseNow() }()
			<-readStarted
			if tt.cancelCause != nil {
				cancelControl(tt.cancelCause)
			}

			readCtx, cancelRead := context.WithTimeout(context.Background(), time.Second)
			_, _, err = clientConn.Read(readCtx)
			cancelRead()
			var clientClose coderws.CloseError
			require.ErrorAs(t, err, &clientClose)
			require.Equal(t, tt.wantStatus, clientClose.Code)
			require.Equal(t, tt.wantReason, clientClose.Reason)

			select {
			case serverErr := <-serverResult:
				var closeErr *OpenAIWSClientCloseError
				require.ErrorAs(t, serverErr, &closeErr)
				require.Equal(t, tt.wantStatus, closeErr.StatusCode())
				require.Equal(t, tt.wantReason, closeErr.Reason())
			case <-time.After(time.Second):
				t.Fatal("server read goroutine did not exit after close handshake")
			}
		})
	}
}

func TestReadOpenAIWSClientMessage_ParentCancellationStillJoinsRead(t *testing.T) {
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	serverResult := make(chan error, 1)
	readStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			serverResult <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()
		close(readStarted)
		_, _, err = ReadOpenAIWSClientMessage(controlCtx, conn, 0, 0, "")
		serverResult <- err
	}))
	defer server.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()
	<-readStarted
	cancelControl(errors.New("server shutting down"))
	readCtx, cancelRead := context.WithTimeout(context.Background(), time.Second)
	_, _, err = clientConn.Read(readCtx)
	cancelRead()
	var clientClose coderws.CloseError
	require.ErrorAs(t, err, &clientClose)
	require.Equal(t, coderws.StatusGoingAway, clientClose.Code)
	require.Equal(t, "websocket request canceled", clientClose.Reason)

	select {
	case <-serverResult:
	case <-time.After(time.Second):
		t.Fatal("server read goroutine leaked after parent cancellation")
	}
}

func TestOpenAIWSClientReaderSurvivesAccountAttemptCancellation(test *testing.T) {
	firstAttemptEnded := make(chan struct{})
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := coderws.Accept(response, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = connection.CloseNow() }()
		client, _ := gin.CreateTestContext(httptest.NewRecorder())
		client.Request = request
		reader := openAIWSIngressClientReader(client, connection)
		first := &openAIWSClientFrameConn{conn: connection, reader: reader, controlCtx: request.Context()}
		attempt, cancel := context.WithCancel(request.Context())
		cancel()
		_, _, readErr := first.ReadFrame(attempt)
		if !errors.Is(readErr, context.Canceled) {
			received <- "first attempt did not stop"
			return
		}
		close(firstAttemptEnded)
		// The next account (or ctx_pool mode) must reuse the only socket reader.
		secondReader := openAIWSIngressClientReader(client, connection)
		if reader != secondReader {
			received <- "reader replaced"
			return
		}
		second := &openAIWSClientFrameConn{conn: connection, reader: secondReader, controlCtx: request.Context()}
		readContext, stop := context.WithTimeout(request.Context(), time.Second)
		defer stop()
		_, payload, readErr := second.ReadFrame(readContext)
		if readErr != nil {
			received <- readErr.Error()
			return
		}
		received <- string(payload)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(test, err)
	defer func() { _ = client.CloseNow() }()
	select {
	case <-firstAttemptEnded:
	case <-ctx.Done():
		test.Fatal("first account reader did not stop")
	}
	payload := `{"type":"response.create","model":"gpt-5.1","input":[]}`
	require.NoError(test, client.Write(ctx, coderws.MessageText, []byte(payload)))
	select {
	case actual := <-received:
		require.Equal(test, payload, actual)
	case <-ctx.Done():
		test.Fatal("follow-up turn was stolen by a stale attempt reader")
	}
}

func TestReviewCanceledAccountDoesNotConsumeRealQueuedFrames(test *testing.T) {
	const samples = 100
	outcomes := make(chan bool, 1)
	serverErrors := make(chan error, 1)
	fixtureContext, stopFixture := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopFixture()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, acceptErr := coderws.Accept(response, request, nil)
		if acceptErr != nil {
			serverErrors <- acceptErr
			return
		}
		defer func() { _ = connection.CloseNow() }()
		clientContext, _ := gin.CreateTestContext(httptest.NewRecorder())
		clientContext.Request = request
		reader := openAIWSIngressClientReader(clientContext, connection)
		for sample := 0; sample < samples; sample++ {
			for len(reader.frames) == 0 {
				select {
				case <-fixtureContext.Done():
					serverErrors <- fixtureContext.Err()
					return
				case <-time.After(time.Millisecond):
				}
			}
			attempt, cancelAttempt := context.WithCancel(fixtureContext)
			cancelAttempt()
			oldAccount := &openAIWSClientFrameConn{conn: connection, reader: reader, controlCtx: fixtureContext}
			_, payload, readErr := oldAccount.ReadFrame(attempt)
			consumed := readErr == nil && len(payload) > 0
			if !consumed {
				if !errors.Is(readErr, context.Canceled) {
					serverErrors <- readErr
					return
				}
				_, stalePayload, staleErr := oldAccount.ReadFrame(attempt)
				if !errors.Is(staleErr, context.Canceled) || len(stalePayload) != 0 {
					serverErrors <- errors.New("old account did not remain canceled")
					return
				}
				nextAccount := &openAIWSClientFrameConn{conn: connection, reader: openAIWSIngressClientReader(clientContext, connection), controlCtx: fixtureContext}
				_, nextPayload, nextErr := nextAccount.ReadFrame(fixtureContext)
				if nextErr != nil {
					serverErrors <- nextErr
					return
				}
				if string(nextPayload) != `{"type":"session.update","session":{"model":"gpt-5.1"}}` || nextAccount.reader != reader || len(reader.frames) != 0 {
					serverErrors <- errors.New("handoff must preserve the exact frame once using the same reader")
					return
				}
			}
			outcomes <- consumed
		}
	}))
	defer server.Close()
	client, _, dialErr := coderws.Dial(fixtureContext, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(test, dialErr)
	defer func() { _ = client.CloseNow() }()
	consumed := 0
	for sample := 0; sample < samples; sample++ {
		require.NoError(test, client.Write(fixtureContext, coderws.MessageText, []byte(`{"type":"session.update","session":{"model":"gpt-5.1"}}`)))
		select {
		case stolen := <-outcomes:
			if stolen {
				consumed++
			}
		case fixtureErr := <-serverErrors:
			test.Fatalf("server fixture failed: %v", fixtureErr)
		case <-fixtureContext.Done():
			test.Fatal("fixture timed out")
		}
	}
	test.Logf("real socket: canceled old account consumed %d/%d queued session updates", consumed, samples)
	require.Zero(test, consumed, "queued client messages must remain available to the next account attempt")
}

// Cancel at the post-dequeue check, with a newer frame already in the socket
// queue. This deterministically exercises the cancellation race and FIFO return.
type cancelAfterDequeueContext struct {
	context.Context
	checks       int
	afterDequeue func()
}

func (attempt *cancelAfterDequeueContext) Err() error {
	attempt.checks++
	if attempt.checks == 2 {
		attempt.afterDequeue()
	}
	return attempt.Context.Err()
}

func TestOpenAIWSClientReaderReturnsFrameCanceledDuringDequeue(test *testing.T) {
	reader := &openAIWSIngressReader{ctx: context.Background(), frames: make(chan openAIWSClientReadResult, 1), stopped: make(chan struct{})}
	reader.frames <- openAIWSClientReadResult{messageType: coderws.MessageText, payload: []byte("queued")}
	baseContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempt := &cancelAfterDequeueContext{Context: baseContext, afterDequeue: func() {
		cancel()
		reader.frames <- openAIWSClientReadResult{messageType: coderws.MessageText, payload: []byte("late")}
	}}
	// No transport operations occur: both frames are already socket-reader output.
	connection := &openAIWSClientFrameConn{conn: &coderws.Conn{}, reader: reader, controlCtx: context.Background()}
	_, payload, err := connection.ReadFrame(attempt)
	require.ErrorIs(test, err, context.Canceled)
	require.Empty(test, payload)
	_, payload, err = connection.ReadFrame(attempt)
	require.ErrorIs(test, err, context.Canceled)
	require.Empty(test, payload)
	for _, expected := range []string{"queued", "late"} {
		messageType, actual, readErr := connection.ReadFrame(context.Background())
		require.NoError(test, readErr)
		require.Equal(test, coderws.MessageText, messageType)
		require.Equal(test, expected, string(actual))
	}
	require.Nil(test, reader.pending)
	require.Empty(test, reader.frames)
}

// Exercise the production ingress after a passthrough attempt relinquishes a
// frame. Both non-passthrough modes must consume pending before newer frames.
func TestOpenAIWSClientReaderPendingFrameSurvivesIngressModeHandoff(test *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeHTTPBridge} {
		test.Run(mode, func(test *testing.T) {
			for _, retainPending := range []bool{false, true} {
				name := "channel_control"
				if retainPending {
					name = "canceled_dequeue_pending"
				}
				test.Run(name, func(test *testing.T) {
					cfg := newOpenAIWSV2TestConfig()
					cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
					cfg.Gateway.OpenAIWS.IngressModeDefault = mode
					cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
					cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
					cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
					cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
					cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
					upstream := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"review_first","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)}}
					httpUpstream := &httpUpstreamRecorder{resp: &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
						Body:       io.NopCloser(strings.NewReader("data: " + string(upstream.events[0]) + "\n\n")),
					}}
					pool := newOpenAIWSConnPool(cfg)
					defer pool.Close()
					pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: upstream})
					service := &OpenAIGatewayService{cfg: cfg, httpUpstream: httpUpstream, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
					account := &Account{ID: 114, Name: "review-handoff", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "sk-test"}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
					followup := []byte(`{"type":"response.create","model":"gpt-5.1","input":[{"role":"user","content":"queued-followup"}]}`)
					type outcome struct {
						reached bool
						pending bool
						queued  int
						err     error
					}
					results := make(chan outcome, 1)
					server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
						connection, acceptErr := coderws.Accept(response, request, nil)
						if acceptErr != nil {
							results <- outcome{err: acceptErr}
							return
						}
						defer func() { _ = connection.CloseNow() }()
						clientContext, _ := gin.CreateTestContext(httptest.NewRecorder())
						clientContext.Request = request
						reader := openAIWSIngressClientReader(clientContext, connection)
						fixtureContext, stopFixture := context.WithTimeout(request.Context(), 3*time.Second)
						defer stopFixture()
						if retainPending {
							oldContext, cancelOld := context.WithCancel(fixtureContext)
							attempt := &cancelAfterDequeueContext{Context: oldContext, afterDequeue: cancelOld}
							oldAccount := &openAIWSClientFrameConn{conn: connection, reader: reader, controlCtx: fixtureContext}
							_, stalePayload, staleErr := oldAccount.ReadFrame(attempt)
							cancelOld()
							if !errors.Is(staleErr, context.Canceled) || len(stalePayload) != 0 || reader.pending == nil {
								results <- outcome{err: errors.New("failed to establish canceled dequeue pending state")}
								return
							}
						}
						if retainPending {
							reader.frames <- openAIWSClientReadResult{messageType: coderws.MessageText, payload: []byte("newer-frame")}
						}
						reached := false
						hooks := &OpenAIWSIngressHooks{BeforeRequest: func(turn int, payload []byte, model string) error {
							if turn == 2 {
								reached = strings.Contains(string(payload), "queued-followup")
								return errors.New("review follow-up reached")
							}
							return nil
						}}
						nextContext, stopNext := context.WithTimeout(fixtureContext, 2*time.Second)
						defer stopNext()
						proxyErr := service.ProxyResponsesWebSocketFromClient(nextContext, clientContext, connection, account, "sk-test", []byte(`{"type":"response.create","model":"gpt-5.1","input":[{"role":"user","content":"first"}]}`), hooks)
						results <- outcome{reached: reached, pending: reader.pending != nil, queued: len(reader.frames), err: proxyErr}
					}))
					defer server.Close()
					fixtureContext, stopFixture := context.WithTimeout(context.Background(), 5*time.Second)
					defer stopFixture()
					client, _, dialErr := coderws.Dial(fixtureContext, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
					require.NoError(test, dialErr)
					defer func() { _ = client.CloseNow() }()
					require.NoError(test, client.Write(fixtureContext, coderws.MessageText, followup))
					_, firstResponse, firstErr := client.Read(fixtureContext)
					require.NoError(test, firstErr)
					require.Contains(test, string(firstResponse), "review_first")
					// Continue reading so any bounded control close can complete.
					go func() { _, _, _ = client.Read(fixtureContext) }()
					select {
					case result := <-results:
						test.Logf("followup_reached=%v pending_retained=%v proxy_error=%v", result.reached, result.pending, result.err)
						require.True(test, result.reached, "next ingress mode must deliver the oldest frame")
						require.False(test, result.pending, "next mode must release the retained frame")
						if retainPending {
							require.Equal(test, 1, result.queued, "newer frame must remain behind pending")
						}
					case <-fixtureContext.Done():
						test.Fatal("handoff fixture timed out")
					}
				})
			}
		})
	}
}
