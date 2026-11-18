package handler

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

type interruptedWSReceiptRepository struct {
	unlimitedTPMRepository
	service.UsageSettlementReceiptRepository
	receipts chan *service.UsageSettlementReceipt
	mutex    sync.Mutex
	stored   map[service.UsageSettlementKey]*service.UsageSettlementReceipt
	settled  map[service.UsageSettlementKey]int
}

func (repository *interruptedWSReceiptRepository) Settle(_ context.Context, key service.UsageSettlementKey, _ string) (*service.UsageSettlementResult, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	receipt := *repository.stored[key]
	if repository.settled == nil {
		repository.settled = make(map[service.UsageSettlementKey]int)
	}
	repository.settled[key]++
	receipt.State = service.UsageSettlementSettled
	return &service.UsageSettlementResult{Receipt: &receipt}, nil
}

type interruptedWSHTTPUpstream struct{ service.HTTPUpstream }

func (interruptedWSHTTPUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return http.DefaultClient.Do(request)
}

func (repository *interruptedWSReceiptRepository) Persist(ctx context.Context, receipt *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if repository.stored == nil {
		repository.stored = make(map[service.UsageSettlementKey]*service.UsageSettlementReceipt)
	}
	if previous := repository.stored[receipt.Key]; previous != nil {
		return previous, nil
	}
	repository.stored[receipt.Key] = receipt
	repository.receipts <- receipt
	return receipt, nil
}

func newInterruptedWSHandlerHarness(test *testing.T, upstreamURL, mode string, deadline time.Duration, failWrites *atomic.Bool) (*coderws.Conn, <-chan struct{}, *interruptedWSReceiptRepository) {
	test.Helper()
	gatewayCache := testutil.NewRedisGatewayCache(test)
	receipts := &interruptedWSReceiptRepository{receipts: make(chan *service.UsageSettlementReceipt, 8)}

	settingRepo := &contentModerationHandlerSettingRepo{values: map[string]string{
		service.SettingKeyRiskControlEnabled:          "true",
		service.SettingKeyCyberSessionBlockEnabled:    "true",
		service.SettingKeyCyberSessionBlockTTLSeconds: "60",
	}}
	moderationRepo := &contentModerationHandlerTestRepo{}
	moderationSvc := service.NewContentModerationService(settingRepo, moderationRepo, nil, nil, nil, nil, nil, nil)
	settingSvc := service.NewSettingService(settingRepo, nil)

	groupID := int64(4301)
	account := service.Account{
		ID:          9951,
		Name:        "openai-ws-passthrough-cyber",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": upstreamURL, "model_mapping": map[string]any{"gpt-5.1": "gpt-5.1"}},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
			"openai_apikey_responses_websockets_v2_mode":    mode,
		},
	}
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeStandard
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 3

	accountRepo := &openAIWSUsageHandlerAccountRepoStub{account: account}
	usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 2)}
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil)
	test.Cleanup(billingCacheSvc.Stop)
	gatewaySvc := service.ProvideOpenAIGatewayService(
		accountRepo, usageRepo, nil, nil, nil, nil, gatewayCache, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCacheSvc, interruptedWSHTTPUpstream{}, &service.DeferredService{},
		nil, nil, nil, nil, nil, settingSvc, nil, service.NewUsageSettlementService(receipts, nil, nil),
	)
	concurrencyCache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	handler := &OpenAIGatewayHandler{
		gatewayService:           gatewaySvc,
		billingCacheService:      billingCacheSvc,
		apiKeyService:            &service.APIKeyService{},
		contentModerationService: moderationSvc,
		concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(concurrencyCache), SSEPingFormatNone, time.Second),
	}

	apiKey := &service.APIKey{
		ID:      1851,
		Name:    "ws-cyber-key",
		Key:     "sk-handler-cyber-test",
		GroupID: &groupID,
		User:    &service.User{ID: 1751, Status: service.StatusActive},
	}
	handlerDone := make(chan struct{})
	router := gin.New()
	router.Use(func(clientContext *gin.Context) {
		clientContext.Set(string(middleware.ContextKeyAPIKey), apiKey)
		clientContext.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		clientContext.Next()
	})
	router.GET("/openai/v1/responses", func(clientContext *gin.Context) {
		if deadline > 0 {
			requestContext, cancel := context.WithTimeout(clientContext.Request.Context(), deadline)
			defer cancel()
			clientContext.Request = clientContext.Request.WithContext(requestContext)
		}
		handler.ResponsesWebSocket(clientContext)
		close(handlerDone)
	})
	handlerServer := httptest.NewUnstartedServer(router)
	if failWrites != nil {
		handlerServer.Listener = &interruptedWSListener{Listener: handlerServer.Listener, failWrites: failWrites}
	}
	handlerServer.Start()
	test.Cleanup(handlerServer.Close)

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(handlerServer.URL, "http")+"/openai/v1/responses", nil)
	cancelDial()
	require.NoError(test, err)
	test.Cleanup(func() { _ = clientConn.CloseNow() })

	return clientConn, handlerDone, receipts
}

func TestWebSocketInterruptedTextReceipts(test *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeHTTPBridge} {
		for _, ending := range []string{"disconnect", "deadline", "write_failure"} {
			test.Run(mode+"/"+ending, func(test *testing.T) {
				upstreamCanceled := make(chan struct{})
				releaseNextEvent := make(chan struct{})
				var failWrites atomic.Bool
				upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
					if mode == service.OpenAIWSIngressModeHTTPBridge {
						response.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(response, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_partial\",\"model\":\"gpt-5.1\",\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}}\n\n")
						fmt.Fprint(response, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
						if flusher, ok := response.(http.Flusher); ok {
							flusher.Flush()
						}
						if ending == "write_failure" {
							select {
							case <-releaseNextEvent:
							case <-request.Context().Done():
								return
							}
							fmt.Fprint(response, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"next\"}\n\n")
							if flusher, ok := response.(http.Flusher); ok {
								flusher.Flush()
							}
						}
						<-request.Context().Done()
						close(upstreamCanceled)
						return
					}
					connection, err := coderws.Accept(response, request, nil)
					if err != nil {
						return
					}
					defer func() { _ = connection.CloseNow() }()
					readContext, cancel := context.WithTimeout(request.Context(), 3*time.Second)
					defer cancel()
					_, _, err = connection.Read(readContext)
					if err != nil {
						return
					}
					// A completed first turn must never become part of the second receipt.
					if ending == "disconnect" {
						_ = connection.Write(readContext, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_complete","model":"gpt-5.1","usage":{"input_tokens":11,"output_tokens":3}}}`))
						_, _, err = connection.Read(readContext)
						if err != nil {
							return
						}
					}
					_ = connection.Write(readContext, coderws.MessageText, []byte(`{"type":"response.created","response":{"id":"resp_partial","model":"gpt-5.1","usage":{"input_tokens":7,"output_tokens":2}}}`))
					if ending == "write_failure" {
						select {
						case <-releaseNextEvent:
						case <-readContext.Done():
							return
						}
						_ = connection.Write(readContext, coderws.MessageText, []byte(`{"type":"response.output_text.delta","delta":"next"}`))
					}
					_, _, _ = connection.Read(readContext)
					close(upstreamCanceled)
				}))
				defer upstream.Close()
				deadline := time.Duration(0)
				if ending == "deadline" {
					deadline = 350 * time.Millisecond
				}
				client, done, receipts := newInterruptedWSHandlerHarness(test, upstream.URL, mode, deadline, &failWrites)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				payload := []byte(`{"type":"response.create","model":"gpt-5.1","input":[]}`)
				require.NoError(test, client.Write(ctx, coderws.MessageText, payload))
				requireRead := func() { _, _, err := client.Read(ctx); require.NoError(test, err) }
				receiveReceipt := func() *service.UsageSettlementReceipt {
					select {
					case receipt := <-receipts.receipts:
						return receipt
					case <-ctx.Done():
						test.Fatal("handler did not persist observed usage")
						return nil
					}
				}
				if ending == "disconnect" && mode != service.OpenAIWSIngressModeHTTPBridge {
					requireRead()
					receipt := receiveReceipt()
					require.Equal(test, "complete", receipt.CompletionState)
					require.Equal(test, 11, receipt.Usage.InputTokens)
					require.NoError(test, client.Write(ctx, coderws.MessageText, payload))
				}
				requireRead()
				if mode == service.OpenAIWSIngressModeHTTPBridge {
					requireRead()
				}
				if ending == "disconnect" {
					require.NoError(test, client.CloseNow())
				}
				if ending == "write_failure" {
					failWrites.Store(true)
					close(releaseNextEvent)
				}
				receipt := receiveReceipt()
				require.Equal(test, "interrupted", receipt.CompletionState)
				require.Equal(test, "resp_partial", receipt.Key.RequestID)
				require.Equal(test, 7, receipt.Usage.InputTokens)
				require.Equal(test, 2, receipt.Usage.OutputTokens)
				if ending == "deadline" || ending == "write_failure" {
					_, _, closeErr := client.Read(ctx)
					require.Error(test, closeErr)
				}
				select {
				case <-done:
				case <-ctx.Done():
					test.Fatal("handler did not release the session")
				}
				select {
				case <-upstreamCanceled:
				case <-ctx.Done():
					test.Fatal("upstream stayed active")
				}
				require.Empty(test, receipts.receipts, "each turn must be persisted once")
				receipts.mutex.Lock()
				for _, count := range receipts.settled {
					require.Equal(test, 1, count)
				}
				require.Len(test, receipts.settled, len(receipts.stored))
				receipts.mutex.Unlock()
			})
		}
	}
}

func TestWebSocketSilentDisconnectCancelsBeforeFirstEvent(test *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeHTTPBridge} {
		test.Run(mode, func(test *testing.T) {
			accepted := make(chan struct{})
			canceled := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if mode == service.OpenAIWSIngressModeHTTPBridge {
					response.Header().Set("Content-Type", "text/event-stream")
					if flusher, ok := response.(http.Flusher); ok {
						flusher.Flush()
					}
					close(accepted)
					<-request.Context().Done()
					close(canceled)
					return
				}
				connection, err := coderws.Accept(response, request, nil)
				if err != nil {
					return
				}
				defer func() { _ = connection.CloseNow() }()
				_, _, err = connection.Read(request.Context())
				if err != nil {
					return
				}
				close(accepted)
				_, _, _ = connection.Read(request.Context())
				close(canceled)
			}))
			defer upstream.Close()
			client, done, _ := newInterruptedWSHandlerHarness(test, upstream.URL, mode, 0, nil)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(test, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":[]}`)))
			select {
			case <-accepted:
			case <-ctx.Done():
				test.Fatal("upstream request not accepted")
			}
			require.NoError(test, client.CloseNow())
			// No upstream semantic event is ever released by this fixture.
			select {
			case <-canceled:
			case <-ctx.Done():
				test.Fatal("disconnect did not cancel silent upstream")
			}
			select {
			case <-done:
			case <-ctx.Done():
				test.Fatal("handler did not release silent turn")
			}
		})
	}
}

// Fail only server writes after the client has received the observed usage.
// Reads stay active so this exercises the production downstream-write error path.
type interruptedWSListener struct {
	net.Listener
	failWrites *atomic.Bool
}

func (listener *interruptedWSListener) Accept() (net.Conn, error) {
	connection, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &interruptedWSConnection{Conn: connection, failWrites: listener.failWrites}, nil
}

type interruptedWSConnection struct {
	net.Conn
	failWrites *atomic.Bool
}

func (connection *interruptedWSConnection) Write(payload []byte) (int, error) {
	if connection.failWrites.Load() {
		return 0, io.ErrClosedPipe
	}
	return connection.Conn.Write(payload)
}
