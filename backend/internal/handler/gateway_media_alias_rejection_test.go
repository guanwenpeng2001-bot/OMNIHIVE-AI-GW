//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMediaAliasConflictsRejectBeforeBudgetAndUpstream(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
	}{
		{name: "speech", path: "/v1/audio/speech", body: `{"model":"tts-mock","input":"hello","text":"different"}`},
		{name: "images", path: "/v1/images/generations", body: `{"model":"gpt-image-mock","size":"1024x1024","resolution":"4k"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			upstreamCalls := 0
			receipts := &mediaHoldReceiptStub{}
			key := &service.APIKey{ID: 7, UserID: 3, BudgetPolicy: service.APIKeyBudgetPolicy{Mode: service.APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 600}}
			router := gin.New()
			router.POST(tc.path, func(c *gin.Context) {
				plan := service.NewGatewayRequestPlan(service.GatewayRequestSpec{UserID: key.UserID, APIKeyID: key.ID, Route: tc.path})
				c.Request = c.Request.WithContext(service.WithGatewayRequestPlan(c.Request.Context(), plan))
				if !reserveGatewayRequestBudget(c, key, "model", []byte(tc.body)) {
					return
				}
				upstreamCalls++
			})
			request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "invalid_request_error")
			require.Zero(t, upstreamCalls)
			require.Empty(t, receipts.reservations)
		})
	}
}

func TestMediaAliasConflictHandlersNeverDispatchOrReceipt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, model, body string
	}{
		{name: "speech", model: "grok-voice", body: `{"model":"grok-voice","input":"hello","text":"different"}`},
		{name: "images", model: "gpt-image-mock", body: `{"model":"gpt-image-mock","prompt":"test","size":"1024x1024","resolution":"4k"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(31)
			account := service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 20, GroupIDs: []int64{groupID}}
			if tc.name == "speech" {
				account.Platform = service.PlatformGrok
			}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			receipts := &openAIAudioReceiptCapture{}
			gateway := service.ProvideOpenAIGatewayService(
				openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
				service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
			concurrency := service.NewConcurrencyService(nil)
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			h := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(tc.body)).WithContext(context.Background())
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("Idempotency-Key", tc.name)
			c.Set("gateway_dispatch_input_checked", true)
			if tc.name == "images" {
				c.Request.URL.Path = "/v1/images/generations"
			}
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 20, UserID: 10, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: account.Platform, Status: service.StatusActive, RateMultiplier: 1, AllowImageGeneration: true}, User: &service.User{ID: 10, Status: service.StatusActive}})
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 10, Concurrency: 5})
			if tc.name == "speech" {
				h.GrokVoice(c, "tts")
			} else {
				h.Images(c)
			}
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "invalid_request_error")
			require.Empty(t, receipts.stored())
		})
	}
}
