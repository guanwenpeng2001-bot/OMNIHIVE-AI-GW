package handler

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayRequestPlanAdapterLifecycleFixtures(t *testing.T) {
	for _, protocol := range []string{"anthropic_messages", "openai_responses"} {
		for _, exit := range []string{"success", "failure", "cancel", "committed"} {
			t.Run(protocol+"/"+exit, func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				c.Request = httptest.NewRequest("POST", "/fixture", nil).WithContext(ctx)
				var capturer gatewayPlanCapturer = &service.GatewayService{}
				if protocol == "openai_responses" {
					capturer = &service.OpenAIGatewayService{}
				}
				finalize := beginGatewayRequestPlan(c, capturer, &service.APIKey{ID: 2, UserID: 1}, "fixture-model", protocol, 3)
				plan := service.GatewayRequestPlanFromContext(c.Request.Context())
				var released atomic.Int32
				release := wrapReleaseOnDone(c.Request.Context(), func() { released.Add(1) })
				if exit == "cancel" {
					cancel()
				}
				if exit == "committed" {
					c.String(200, "already written")
				}
				finalize()
				release()
				require.EqualValues(t, 1, released.Load())
				var records int
				task := func(recordCtx context.Context) {
					records++
					require.NoError(t, recordCtx.Err())
					require.Same(t, plan, service.GatewayRequestPlanFromContext(recordCtx))
				}
				// The shared ownership guard covers accidental cross-adapter submission.
				(&GatewayHandler{}).submitMandatoryUsageRecordTask(c.Request.Context(), task)
				(&OpenAIGatewayHandler{}).submitMandatoryUsageRecordTask(c.Request.Context(), task)
				require.Equal(t, 1, records)
				require.Equal(t, exit == "committed", plan.State().ClientCommitted)
				require.Equal(t, exit == "cancel", plan.State().Canceled)
				require.Empty(t, plan.State().ReceiptState)
				require.Equal(t, protocol, plan.Snapshot().Protocol)
			})
		}
	}
}

func TestMediaPlanScopesSynchronousImagesAndAsynchronousVideoJobs(t *testing.T) {
	for _, scenario := range []struct{ path, kind string }{
		{"/v1/images/generations", "request"}, {"/v1/videos/generations", "job"},
		{"/v1/videos/edits", "job"}, {"/v1/videos/extensions", "job"},
	} {
		client, _ := gin.CreateTestContext(httptest.NewRecorder())
		client.Request = httptest.NewRequest("POST", scenario.path, nil)
		finish := beginGatewayRequestPlan(client, &service.OpenAIGatewayService{}, &service.APIKey{ID: 2, UserID: 1}, "media-model", "media", 4)
		plan := service.GatewayRequestPlanFromContext(client.Request.Context())
		require.Equal(t, scenario.kind, plan.Snapshot().ExecutionKind)
		require.NotEmpty(t, plan.Snapshot().ExecutionID)
		require.Equal(t, 5, plan.Snapshot().GenerationAttempts)
		finish()
	}
}

// Existing transport/credential fixtures now carry explicit prepared catalog
// evidence. This fixture is not used by missing/revoked-catalog tests.
func preparedGatewayFixtureAccount(account service.Account) service.Account {
	extra := make(map[string]any, len(account.Extra)+1)
	for key, value := range account.Extra {
		extra[key] = value
	}
	if _, exists := extra[service.UpstreamModelCatalogExtraKey]; exists {
		return account
	}
	models := []string{"gpt-image-1", "gpt-5.6-luna", "grok-imagine-video", "default", "claude-sonnet-4-5", "claude-sonnet-4-5-thinking", "gpt-6-astra", "sol", "terra", "sol-channel", "terra-channel", "gpt-4.1", "gpt-5.1", "gpt-5.2", "gpt-5.4", "gpt-5.5", "gpt-5.6-sol", "gpt-5.6-terra", "grok-4", "grok-4.1-fast", "grok-4.20-beta", "grok-4.3"}
	for _, target := range account.GetModelMapping() {
		models = append(models, target)
	}
	catalog := service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveredModels: models, EligibleModels: []service.AccountCatalogModel{}}
	for _, model := range models {
		catalog.EligibleModels = append(catalog.EligibleModels, service.AccountCatalogModel{ID: model, Endpoints: []string{"responses", "chat_completions", "messages", "generate_content", "images", "grok_media_generation"}, EvidenceKind: "fixture", EvidenceObservedAt: "2026-09-18T00:00:00Z"})
	}
	extra[service.UpstreamModelCatalogExtraKey] = catalog
	account.Extra = extra
	return account
}
