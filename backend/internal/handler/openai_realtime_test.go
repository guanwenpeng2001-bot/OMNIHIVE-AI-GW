//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	middleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type openAIRealtimeHandlerRepository struct{ gatewayResourceHandlerRepo }

func (repository *openAIRealtimeHandlerRepository) CreateRealtimeSession(_ context.Context, job *service.UpstreamJob) (*service.UpstreamJob, error) {
	repository.job = job
	return job, nil
}

func (repository *openAIRealtimeHandlerRepository) InsertRealtimeReceipt(_ context.Context, receipt *service.UpstreamJob) (*service.UpstreamJob, error) {
	repository.jobs = append(repository.jobs, receipt)
	return receipt, nil
}

func TestOpenAIRealtimeHandlerFakeUpstream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	upstreamClosed := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer close(upstreamClosed)
		connection, err := ws.Accept(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.CloseNow()
		_ = connection.Write(ctx, ws.MessageText, []byte(`{"type":"session.created","session":{"id":"upstream-session","model":"gpt-realtime-2.1"}}`))
		_, _, err = connection.Read(ctx)
		if err != nil {
			return
		}
		_ = connection.Write(ctx, ws.MessageText, []byte(`{"type":"response.done","response":{"id":"upstream-turn","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3,"input_token_details":{"text_tokens":1,"audio_tokens":0,"cached_tokens":0},"output_token_details":{"text_tokens":2,"audio_tokens":0}}}}`))
		_, _, _ = connection.Read(ctx)
	}))
	defer provider.Close()
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{ConcurrencySlotTTLMinutes: 3}, Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}}
	account := service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"api_key": "fixture", "base_url": provider.URL, "model_mapping": map[string]any{"gpt-realtime-2.1": "gpt-realtime-2.1"}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}, Extra: map[string]any{service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveredModels: []string{"gpt-realtime-2.1"}, EligibleModels: []service.AccountCatalogModel{{ID: "gpt-realtime-2.1", Endpoints: []string{"realtime", "chat_completions"}, EvidenceKind: "fixture", EvidenceObservedAt: "2026-09-18T00:00:00Z"}}}}}
	repository := &openAIWSUsageHandlerAccountRepoStub{account: account}
	require.True(t, service.AccountSupportsResolvedModel(ctx, &account, "gpt-realtime-2.1", service.AccountModelBindingOptions{Endpoint: "realtime"}), "fixture must expose realtime binding")
	gateway := service.ProvideOpenAIGatewayService(repository, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	defer billing.Stop()
	handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	jobs := &openAIRealtimeHandlerRepository{}
	handler.upstreamJobs = service.NewUpstreamJobService(jobs, nil, service.GatewayResourceJobCharge)
	groupID := int64(3)
	key := &service.APIKey{ID: 2, UserID: 1, GroupID: &groupID, User: &service.User{ID: 1, Status: service.StatusActive}, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1}}
	router := gin.New()
	finished := make(chan struct{})
	// The production action descriptor is owned by the later wiring package.
	// Use a fixture-only route to test the handler without changing that registry.
	router.GET("/fixture/socket", func(request *gin.Context) {
		defer close(finished)
		request.Set(string(middleware.ContextKeyAPIKey), key)
		request.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
		handler.OpenAIRealtime(request)
	})
	server := httptest.NewServer(router)
	defer server.Close()
	client, response, err := ws.Dial(ctx, server.URL+"/fixture/socket?model=gpt-realtime-2.1", nil)
	if err != nil && response != nil {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("handshake: %v status=%d body=%s", err, response.StatusCode, body)
	}
	require.NoError(t, err)
	defer client.CloseNow()
	_, _, err = client.Read(ctx)
	require.NoError(t, err)
	require.NoError(t, client.Write(ctx, ws.MessageText, []byte(`{"type":"response.create"}`)))
	_, responseBody, err := client.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(responseBody), "response.done")
	require.NoError(t, client.CloseNow())
	select {
	case <-upstreamClosed:
	case <-time.After(time.Second):
		t.Fatal("upstream remained open")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("handler remained active")
	}
	require.Len(t, jobs.jobs, 1)
	require.Equal(t, service.PricingStateUnpriced, jobs.jobs[0].Billing.PricingState)
	require.Nil(t, jobs.jobs[0].ChargedUsageLogID)
	require.Equal(t, "gpt-realtime-2.1", jobs.jobs[0].Billing.Realtime.UpstreamResponseModel)
	require.Equal(t, "session_closed", jobs.job.ErrorCode)
}

func TestOpenAIRealtimeHandshakeAdmission(t *testing.T) {
	for _, test := range []struct {
		name, platform, query, origin string
		auth, upgrade                 bool
		status                        int
	}{
		{"missing key", "openai", "model=gpt-realtime", "", false, true, 401},
		{"group pinned", "qianwen", "model=gpt-realtime", "", true, true, 404},
		{"non websocket", "openai", "model=gpt-realtime", "", true, false, 426},
		{"duplicate model", "openai", "model=gpt-realtime&model=other", "", true, true, 400},
		{"missing model", "openai", "", "", true, true, 400},
		{"empty model", "openai", "model=", "", true, true, 400},
		{"malformed query", "openai", "model=gpt-realtime&bad=%zz", "", true, true, 400},
		{"foreign origin", "openai", "model=gpt-realtime", "https://evil.test", true, true, 403},
		{"opaque origin", "openai", "model=gpt-realtime", "null", true, true, 403},
		{"same origin reaches dependencies", "openai", "model=gpt-realtime", "http://example.com", true, true, 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			request, _ := gin.CreateTestContext(writer)
			request.Request = httptest.NewRequest("GET", "/v1/realtime?"+test.query, nil)
			if test.auth {
				request.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 2, UserID: 1, User: &service.User{ID: 1}, Group: &service.Group{Platform: test.platform}})
				request.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
			}
			if test.upgrade {
				request.Request.Header.Set("Connection", "Upgrade")
				request.Request.Header.Set("Upgrade", "websocket")
				request.Request.Header.Set("Sec-WebSocket-Version", "13")
				request.Request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
			}
			request.Request.Header.Set("Origin", test.origin)
			(&OpenAIGatewayHandler{}).OpenAIRealtime(request)
			require.Equal(t, test.status, writer.Code)
			require.Empty(t, writer.Header().Get("Upgrade"))
		})
	}
}

func TestOpenAIRealtimeDialErrorAttribution(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		writer := httptest.NewRecorder()
		request, _ := gin.CreateTestContext(writer)
		request.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
		body := []byte(`{"error":{"code":"not_entitled","message":"Realtime is not enabled"}}`)
		(&OpenAIGatewayHandler{}).openAIRealtimeDialError(request, &service.UpstreamFailoverError{StatusCode: status, ResponseBody: body, ResponseHeaders: http.Header{"Retry-After": {"7"}, "Authorization": {"secret"}, "Set-Cookie": {"secret"}}})
		require.Equal(t, status, writer.Code)
		require.Equal(t, string(body), writer.Body.String())
		require.Equal(t, "7", writer.Header().Get("Retry-After"))
		require.Empty(t, writer.Header().Get("Authorization"))
		require.Empty(t, writer.Header().Get("Set-Cookie"))
	}
	for _, test := range []struct {
		err    error
		status int
	}{{errors.New("network failed"), 502}, {service.ErrAccountModelUnsupported, 422}} {
		writer := httptest.NewRecorder()
		request, _ := gin.CreateTestContext(writer)
		request.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
		(&OpenAIGatewayHandler{}).openAIRealtimeDialError(request, test.err)
		require.Equal(t, test.status, writer.Code)
	}
}

func TestOpenAIRealtimeRejectsMalformedUpgradeBeforeDependencies(t *testing.T) {
	for _, headers := range []map[string]string{
		{"Sec-WebSocket-Key": "invalid"},
		{"Sec-WebSocket-Version": "12"},
		{"Connection": "notupgrade"},
	} {
		writer := httptest.NewRecorder()
		request, _ := gin.CreateTestContext(writer)
		request.Request = httptest.NewRequest("GET", "/v1/realtime?model=gpt-realtime-2.1", nil)
		request.Set(string(middleware.ContextKeyAPIKey), gatewayResourceKey(service.PlatformOpenAI))
		request.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		request.Request.Header.Set("Connection", "Upgrade")
		request.Request.Header.Set("Upgrade", "websocket")
		request.Request.Header.Set("Sec-WebSocket-Version", "13")
		request.Request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		for name, value := range headers {
			request.Request.Header.Set(name, value)
		}
		(&OpenAIGatewayHandler{}).OpenAIRealtime(request)
		require.Equal(t, 400, writer.Code)
	}
}
