package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestParseLiveCallRequestMultipartPreservesSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	session := `{"model":"gpt-live-test","delegation":{"type":"client"},"instructions":"你好"}`
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("sdp", "v=0\r\n"))
	require.NoError(t, writer.WriteField("session", session))
	require.NoError(t, writer.Close())

	request := httptest.NewRequest("POST", "/v1/live", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	parsed, err := parseLiveCallRequest(context)
	require.NoError(t, err)
	require.Equal(t, "v=0\r\n", parsed.SDP)
	require.JSONEq(t, session, string(parsed.Session))
	require.Equal(t, "client", jsonPathString(t, parsed.Session, "delegation", "type"))
}

func TestParseLiveCallRequestJSONPreservesSessionWithoutDelegation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"sdp":"v=0\\r\\n","session":{"model":"gpt-live-test","instructions":"standalone"}}`
	request := httptest.NewRequest("POST", "/backend-api/codex/realtime/calls", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	parsed, err := parseLiveCallRequest(context)
	require.NoError(t, err)
	require.NotContains(t, string(parsed.Session), "delegation")
	require.Equal(t, "standalone", jsonPathString(t, parsed.Session, "instructions"))
}

func TestParseLiveCallRequestRejectsInvalidJSONShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testCases := []string{
		`{"session":{"type":"quicksilver"}}`,
		`{"sdp":"v=0\\r\\n","session":[]}`,
		`{"sdp":"v=0\\r\\n","session":null}`,
		`{"sdp":"v=0\\r\\n","session":{"type":"quicksilver"}} {}`,
	}
	for _, body := range testCases {
		request := httptest.NewRequest("POST", "/backend-api/codex/realtime/calls", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = request
		_, err := parseLiveCallRequest(context)
		require.Error(t, err)
	}
}

func TestLiveSidebandLocationMatchesCreateRoute(t *testing.T) {
	require.Equal(t, "/v1/live/call_123", liveSidebandLocation("/v1/live", "call_123"))
	require.Equal(
		t,
		"/backend-api/codex/call_123",
		liveSidebandLocation("/backend-api/codex/realtime/calls", "call_123"),
	)
}

func TestLiveEnabledForAPIKey(t *testing.T) {
	require.False(t, liveEnabledForAPIKey(nil))
	require.False(t, liveEnabledForAPIKey(&service.APIKey{}))
	require.False(t, liveEnabledForAPIKey(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformOpenAI},
	}))
	require.False(t, liveEnabledForAPIKey(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformAnthropic, AllowLive: true},
	}))
	require.True(t, liveEnabledForAPIKey(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformOpenAI, AllowLive: true},
	}))
	require.True(t, liveEnabledForAPIKey(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformComposite, AllowLive: true},
	}))
}

func TestLiveAttestationErrorIsExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	(&OpenAIGatewayHandler{}).writeLiveCreateError(context, &service.LiveAttestationUnavailableError{
		Reason: "Live attestation is only supported when Sub2API runs on macOS",
	})

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Sub2API runs on macOS")
}

func jsonPathString(t *testing.T, raw json.RawMessage, keys ...string) string {
	t.Helper()
	var value any
	require.NoError(t, json.Unmarshal(raw, &value))
	current := value
	for _, key := range keys {
		object, ok := current.(map[string]any)
		require.True(t, ok)
		current = object[key]
	}
	result, ok := current.(string)
	require.True(t, ok)
	return result
}

func TestLiveBillingUnsupportedErrorIsExplicit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	(&OpenAIGatewayHandler{}).writeLiveCreateError(c, service.ErrLiveBillingUnsupported)
	require.Equal(t, http.StatusNotImplemented, recorder.Code)
	require.Contains(t, recorder.Body.String(), "live_billing_unsupported")
	require.Contains(t, recorder.Body.String(), "explicit free policy")
}

func TestLiveRejectsUnapprovedBillingBeforeUpstream(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformComposite} {
		for _, enabled := range []bool{false, true} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/live", nil)
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
				Group: &service.Group{Platform: platform, AllowLive: enabled},
			})
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 1})
			// Nil billing, scheduler and upstream dependencies enforce early rejection.
			(&OpenAIGatewayHandler{}).Live(c)
			if enabled {
				require.Equal(t, http.StatusNotImplemented, recorder.Code)
			} else {
				require.Equal(t, http.StatusForbidden, recorder.Code)
			}
		}
	}
}

func TestLiveCreatesRequestPlanAndBudgetBeforeBilling(t *testing.T) {
	for _, path := range []string{"/v1/live", "/backend-api/codex/realtime/calls"} {
		cfg := &config.Config{}
		cfg.Gateway.Live.BillingPolicy = "free"
		gateway := service.ProvideOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
		handler := &OpenAIGatewayHandler{gatewayService: gateway, cfg: cfg, maxAccountSwitches: 2}
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("POST", path, bytes.NewBufferString(`{"sdp":"v=0\r\n","session":{"model":"gpt-live-fixture"}}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		group := &service.Group{ID: 31, Platform: service.PlatformOpenAI, AllowLive: true}
		ctx.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 7, UserID: 9, GroupID: &group.ID, Group: group, User: &service.User{ID: 9}})
		ctx.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 9, Concurrency: 1})
		handler.Live(ctx)
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code, "no billing dependency: fail before dispatch")
		plan := service.GatewayRequestPlanFromContext(ctx.Request.Context())
		require.NotNil(t, plan)
		require.Equal(t, "live", plan.Snapshot().Protocol)
		require.Equal(t, "gpt-live-fixture", plan.Snapshot().Model)
		require.Equal(t, path, plan.Snapshot().Route)
		require.EqualValues(t, 7, plan.Snapshot().APIKeyID)
		require.True(t, plan.State().Finalized)
		require.NotNil(t, service.AttemptBudgetFromContext(ctx.Request.Context()))
		_, deadlineSet := ctx.Request.Context().Deadline()
		require.True(t, deadlineSet)
	}
}
