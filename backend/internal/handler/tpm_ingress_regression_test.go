//go:build unit

package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestAuditQianwenTPMOnlyRejectsBeforeSend(t *testing.T) {
	for _, action := range []string{"embeddings", "rerank"} {
		t.Run(action, func(t *testing.T) {
			model, payload, request := "text-embedding-v4", `{"object":"list","data":[{"embedding":[0.1],"index":0}],"usage":{"prompt_tokens":11,"total_tokens":11}}`, `{"model":"text-embedding-v4","input":["hello"]}`
			endpoint := "embeddings"
			if action == "rerank" {
				model = "gte-rerank-v2"
				endpoint = "chat_completions"
				payload = `{"output":{"results":[{"index":0,"relevance_score":0.9}]},"usage":{"total_tokens":79},"request_id":"audit"}`
				request = `{"model":"gte-rerank-v2","query":"q","documents":["hello"]}`
			}
			upstream := &httpUpstreamRecorderStub{body: payload}
			account := qianwenCatalogAccount(1, model, endpoint)
			account.TPMLimit = 1
			h := newQianwenStandardTestHandler(t, upstream, []service.Account{account})
			denied := &unlimitedTPMReceiptStub{unlimitedTPMRepository: unlimitedTPMRepository{rejection: &service.TPMLimitExceeded{Scope: service.TPMScopeAPIKey}}}
			h.gatewayService = service.ProvideOpenAIGatewayService(&qianwenStandardAccountRepo{accounts: []service.Account{account}}, nil, nil, nil, nil, nil, nil, h.cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(denied, nil, nil))
			c, w := qianwenStandardContext(t, http.MethodPost, "/v1/"+action, []byte(request), nil)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.TPMLimit = 1
			key.Group.TPMLimit = 1
			if action == "embeddings" {
				h.QianwenEmbeddings(c)
			} else {
				h.QianwenRerank(c)
			}
			t.Logf("status=%d upstream=%s plan_nil=%t key_has_period_budget=%t", w.Code, upstream.lastURL, service.GatewayRequestPlanFromContext(c.Request.Context()) == nil, key.HasPeriodBudget())
			require.Empty(t, upstream.lastURL, "TPM policy must be checked before upstream")
			require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
		})
	}
}

func TestAuditBedrockTPMOnlyRejectsBeforeSend(t *testing.T) {
	group := &service.Group{ID: 7401, Hydrated: true, Platform: service.PlatformBedrock, Status: service.StatusActive, TPMLimit: 1}
	h, router, upstream := bedrockHandlerFixtureForGroup(t, group, 7402, map[string]any{"aws_access_key_id": "AKIAFIXTUREEXAMPLE", "aws_secret_access_key": "fixture-secret", "aws_region": "us-east-1", "model_mapping": map[string]any{"titan-embed": "amazon.titan-embed-text-v2:0"}}, false, func(*http.Request) *http.Response {
		return bedrockHandlerJSON(200, `{"embedding":[0.5,0.25],"inputTextTokenCount":3}`)
	}, nil)
	router.Use(func(c *gin.Context) { key, _ := middleware.GetAPIKeyFromContext(c); key.TPMLimit = 1; c.Next() })
	router.POST("/v1/embeddings", h.BedrockEmbeddings)
	w := bedrockHandlerServe(t, router, http.MethodPost, "/v1/embeddings", `{"model":"titan-embed","input":"alpha"}`, "application/json")
	requests, _ := upstream.snapshot()
	for _, request := range requests {
		t.Logf("status=%d upstream=%s plan_nil=%t", w.Code, request.URL.Path, service.GatewayRequestPlanFromContext(request.Context()) == nil)
	}
	require.Empty(t, requests, "TPM policy must be checked before upstream")
	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
}

func TestQianwenNativeAndImageTPMAdmission(t *testing.T) {
	for _, scenario := range []struct{ name, model, path, body string }{
		{"native_embedding", "text-embedding-v4", service.QianwenNativeEmbeddingPath, `{"model":"text-embedding-v4","input":{"texts":["hello"]}}`},
		{"native_rerank", "gte-rerank-v2", service.QianwenNativeRerankPath, `{"model":"gte-rerank-v2","input":{"query":"q","documents":["a"]}}`},
		{"image", "qwen-image-3.0", "/v1/images/generations", `{"model":"qwen-image-3.0","prompt":"cat"}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorderStub{body: `{}`}
			account := qianwenCatalogAccount(1, scenario.model, "chat_completions", "images")
			handler := newQianwenStandardTestHandler(t, upstream, []service.Account{account})
			denied := &unlimitedTPMReceiptStub{unlimitedTPMRepository: unlimitedTPMRepository{rejection: &service.TPMLimitExceeded{Scope: service.TPMScopeAPIKey}}}
			handler.gatewayService = service.ProvideOpenAIGatewayService(&qianwenStandardAccountRepo{accounts: []service.Account{account}}, nil, nil, nil, nil, nil, nil, handler.cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(denied, nil, nil))
			requestContext, recorder := qianwenStandardContext(t, http.MethodPost, scenario.path, []byte(scenario.body), nil)
			if scenario.name == "image" {
				handler.QianwenImages(requestContext)
			} else {
				handler.QianwenNative(requestContext)
			}
			require.Empty(t, upstream.lastURL)
			require.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
			plan := service.GatewayRequestPlanFromContext(requestContext.Request.Context())
			require.NotNil(t, plan, recorder.Body.String())
			require.True(t, service.IsTPMAdmissionError(plan.TPMAdmissionRejection()), recorder.Body.String())
		})
	}
}

func TestBatchImageHandlerInstallsParentPlan(t *testing.T) {
	openAI := newQianwenStandardTestHandler(t, &httpUpstreamRecorderStub{}, nil)
	handler := ProvideBatchImageHandler(&service.BatchImagePublicService{}, nil, nil, openAI)
	requestContext, _ := qianwenStandardContext(t, http.MethodPost, "/v1/images/batches", []byte(`{"model":"gemini-image","items":[{"custom_id":"image","prompt":"cat"}]}`), nil)
	key, _ := middleware.GetAPIKeyFromContext(requestContext)
	key.WorkspaceID = 51
	handler.Submit(requestContext)
	parent := service.GatewayRequestPlanFromContext(requestContext.Request.Context())
	require.NotNil(t, parent)
	require.EqualValues(t, 51, parent.Snapshot().WorkspaceID)
	require.Equal(t, key.ID, parent.Snapshot().APIKeyID)
	require.Equal(t, "gemini-image", parent.Snapshot().Model)
	require.True(t, parent.State().Finalized)
}
