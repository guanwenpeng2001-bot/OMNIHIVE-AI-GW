//go:build unit

package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type antigravityReceiptUpstream struct {
	service.HTTPUpstream
	bodies     [][]byte
	emptyFirst bool
}

func (s *antigravityReceiptUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	s.bodies = append(s.bodies, body)
	payload := `{"response":{"responseId":"approved-response","candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3}}}`
	if s.emptyFirst && len(s.bodies) == 1 {
		payload = `{"response":{"responseId":"signature-response","candidates":[{"content":{"role":"model","parts":[{"thought":true,"thoughtSignature":"opaque-signature"}]},"finishReason":"MALFORMED_FUNCTION_CALL"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + payload + "\n\n")), Request: request}, nil
}

func TestAntigravityCompatHandlerPersistsReceipt(t *testing.T) {
	for _, scenario := range []struct {
		name, path, payload string
		retry, pdf, bare    bool
	}{
		{"responses_pdf", EndpointResponses, `"input":[{"role":"user","content":[{"type":"input_text","text":"read this"},{"type":"input_file","file_data":"data:application/pdf;base64,JVBERi0="}]}]`, false, true, false},
		{"chat_pdf", EndpointChatCompletions, `"messages":[{"role":"user","content":[{"type":"text","text":"read this"},{"type":"file","file":{"filename":"test.pdf","file_data":"data:application/pdf;base64,JVBERi0="}}]}]`, false, true, false},
		{"signature_failover", EndpointResponses, `"input":"hello"`, true, false, false},
		{"approved_bare_variant", EndpointResponses, `"input":"hello"`, false, false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Default.RateMultiplier = 1
			groupID := int64(945)
			group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformAntigravity, Status: service.StatusActive}
			model := "gemini-3-flash-high"
			publicModel := model
			if scenario.bare {
				publicModel = "gemini-3-flash"
			}
			account := service.Account{ID: 946, WorkspaceID: 949, Name: "approved-antigravity", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
				Credentials:   map[string]any{"access_token": "fixture-token", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339), "project_id": "fixture-project", "model_mapping": map[string]any{model: model}},
				AccountGroups: []service.AccountGroup{{AccountID: 946, GroupID: groupID}}}
			account = preparedGatewayFixtureAccount(account)
			repository := &heartbeatAccountRepository{platform: service.PlatformAntigravity}
			repository.accounts = []service.Account{account}
			upstream := &antigravityReceiptUpstream{emptyFirst: scenario.retry}
			receipts := newHunyuanSTTReceiptRepository()
			billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billingCache.Stop)
			billingConfig := *cfg
			billingConfig.RunMode = "standard"
			gateway := service.ProvideGatewayService(repository, &heartbeatGroupRepository{group: group}, nil, nil, nil, nil, nil, nil, &billingConfig, nil, nil, service.NewBillingService(&billingConfig, nil), nil, billingCache, nil, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
			handler := &GatewayHandler{gatewayService: gateway, billingCacheService: billingCache,
				concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0), maxAccountSwitches: 1, cfg: cfg,
				antigravityGatewayService: service.NewAntigravityGatewayService(repository, nil, nil, service.NewAntigravityTokenProvider(repository, nil, nil), nil, upstream, nil, nil)}
			recorder := httptest.NewRecorder()
			client, _ := gin.CreateTestContext(recorder)
			client.Request = httptest.NewRequest(http.MethodPost, scenario.path, strings.NewReader(`{"model":"`+publicModel+`","stream":true,`+scenario.payload+`}`))
			client.Request.Header.Set("Content-Type", "application/json")
			client.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 947, WorkspaceID: 949, UserID: 948, GroupID: &groupID, Group: group, Status: service.StatusActive, User: &service.User{ID: 948, Status: service.StatusActive, Concurrency: 1, Balance: 100}})
			client.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 948, Concurrency: 1})
			if scenario.path == EndpointResponses {
				handler.Responses(client)
			} else {
				handler.ChatCompletions(client)
			}
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "hello")
			plan := service.GatewayRequestPlanFromContext(client.Request.Context())
			require.NotNil(t, plan)
			require.True(t, plan.State().Finalized)
			require.True(t, plan.State().UsageClaimed)
			require.Equal(t, int64(949), plan.Snapshot().WorkspaceID)
			require.Equal(t, publicModel, plan.Snapshot().Model)
			require.NotEmpty(t, plan.Snapshot().ModelVersion)
			require.False(t, plan.Snapshot().PricingAt.IsZero())
			expectedAttempts := 1
			if scenario.retry {
				expectedAttempts = 2
			}
			require.Len(t, upstream.bodies, expectedAttempts)
			require.Len(t, plan.Attempts(), expectedAttempts)
			require.NotEmpty(t, receipts.receipts)
			for _, receipt := range receipts.receipts {
				require.Equal(t, int64(947), receipt.Key.APIKeyID)
				require.Equal(t, int64(948), receipt.Key.UserID)
				require.NotContains(t, receipt.Usage.PricingSnapshot, "JVBERi0=")
				require.Equal(t, service.UsageSettlementNeedsReview, receipt.State, "unknown variant pricing must remain reviewable")
				require.Equal(t, 8, receipt.Usage.InputTokens)
				require.Equal(t, 3, receipt.Usage.OutputTokens)
			}
			require.Equal(t, model, gjson.GetBytes(upstream.bodies[0], "model").String())
			if scenario.pdf {
				require.Contains(t, gjson.GetBytes(upstream.bodies[0], "request.contents").Raw, `"mimeType":"application/pdf"`)
				require.Contains(t, gjson.GetBytes(upstream.bodies[0], "request.contents").Raw, `"data":"JVBERi0="`)
			}
		})
	}
}
