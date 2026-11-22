//go:build unit

package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayBudgetErrorResponseMapsRejections(t *testing.T) {
	status, reason, message := gatewayBudgetErrorResponse(service.ErrAPIKeyBudgetExceeded)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "API_KEY_BUDGET_EXCEEDED", reason)
	require.NotEmpty(t, message)

	status, reason, _ = gatewayBudgetErrorResponse(service.ErrAPIKeyBudgetInsufficientBalance)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "INSUFFICIENT_BALANCE", reason)

	// Anything else means the hold could not be taken: fail closed instead of
	// forwarding a request whose budget was never reserved.
	status, reason, _ = gatewayBudgetErrorResponse(errors.New("database unavailable"))
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "API_KEY_BUDGET_UNAVAILABLE", reason)

	status, reason, _ = gatewayBudgetErrorResponse(service.ErrAPIKeyBudgetUnavailable)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "API_KEY_BUDGET_UNAVAILABLE", reason)

	status, reason, _ = gatewayBudgetErrorResponse(infraerrors.BadRequest("SOMETHING_ELSE", "boom"))
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "API_KEY_BUDGET_UNAVAILABLE", reason)
}

func TestGatewayBudgetUnknownModelIsProtocolNotFound(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, path, nil)
		abortGatewayRequestBudget(c, service.ErrGatewayBudgetModelNotFound)
		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Equal(t, "model_not_found", gjson.Get(recorder.Body.String(), "error.code").String())
		if path == "/v1/messages" {
			require.Equal(t, "error", gjson.Get(recorder.Body.String(), "type").String())
		}
	}
}

func TestDeclaredGatewayMaxOutputTokensAcrossProtocols(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"anthropic max_tokens", `{"model":"claude","max_tokens":4096}`, 4096},
		{"openai max_completion_tokens", `{"model":"gpt-5","max_completion_tokens":2048}`, 2048},
		{"responses max_output_tokens", `{"model":"gpt-5","max_output_tokens":1024}`, 1024},
		{"gemini generation config", `{"generation_config":{"max_output_tokens":512}}`, 512},
		{"gemini camel case", `{"generationConfig":{"maxOutputTokens":256}}`, 256},
		{"absent", `{"model":"claude"}`, 0},
		{"empty body", ``, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, declaredGatewayMaxOutputTokens([]byte(tc.body)))
		})
	}
}

func TestEstimateGatewayRequestInputTokensIsBounded(t *testing.T) {
	require.Zero(t, estimateGatewayRequestInputTokens(nil))
	require.Zero(t, estimateGatewayRequestInputTokens([]byte("not json")))
	// An Anthropic-shaped body is estimated locally. Other shapes are only
	// approximated (the estimator is Anthropic-shaped) and the reservation keeps
	// the conservative default bound whenever the estimate is unusable.
	require.Positive(t, estimateGatewayRequestInputTokens([]byte(
		`{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello budget world"}]}`)))
	require.GreaterOrEqual(t, estimateGatewayRequestInputTokens([]byte(`{"model":"gpt-5","input":"hi"}`)), 0)
}

func TestGatewayRequestRateMultiplierDefaultsToOne(t *testing.T) {
	require.InDelta(t, 1, gatewayRequestRateMultiplier(nil), 1e-9)
	require.InDelta(t, 1, gatewayRequestRateMultiplier(&service.APIKey{}), 1e-9)
	require.InDelta(t, 0.8, gatewayRequestRateMultiplier(&service.APIKey{Group: &service.Group{RateMultiplier: 0.8}}), 1e-9)
}

func TestGatewayTextBudgetUsesCompleteOutputUpperBound(t *testing.T) {
	plan := service.NewGatewayRequestPlan(service.GatewayRequestSpec{Route: "/v1/chat/completions", Model: "gpt-5.4"})
	estimate := gatewayRequestBudgetEstimate(plan, &service.APIKey{}, "gpt-5.4", []byte(`{"max_completion_tokens":100,"n":3}`))
	require.Equal(t, 300, estimate.OutputTokens)

	plan = service.NewGatewayRequestPlan(service.GatewayRequestSpec{Route: "/v1beta/models/gemini:generateContent", Model: "gemini"})
	estimate = gatewayRequestBudgetEstimate(plan, &service.APIKey{}, "gemini", []byte(`{"generationConfig":{"maxOutputTokens":100,"thinkingConfig":{"thinkingBudget":50},"candidateCount":2}}`))
	require.Equal(t, 300, estimate.OutputTokens)
}

func TestGatewayTextBudgetCountsExplicitCacheWrites(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4","max_tokens":10,"messages":[{"role":"user","content":[{"type":"text","text":"cached context","cache_control":{"type":"ephemeral"}}]}]}`)
	estimate := gatewayRequestBudgetEstimate(service.NewGatewayRequestPlan(service.GatewayRequestSpec{Model: "claude-sonnet-4"}), &service.APIKey{}, "claude-sonnet-4", body)
	require.Positive(t, estimate.CacheCreationTokens)
	require.LessOrEqual(t, estimate.CacheCreationTokens, estimate.InputTokens)
}

func TestGatewayBudgetOutputDefaultsPerCandidateAndRejectsOverflow(t *testing.T) {
	estimate := gatewayRequestBudgetEstimate(nil, &service.APIKey{}, "unlisted", []byte(`{"n":3}`))
	require.Equal(t, 12288, estimate.OutputTokens)
	estimate = gatewayRequestBudgetEstimate(nil, &service.APIKey{}, "unlisted", []byte(`{"n":2,"max_tokens":7,"max_output_tokens":9}`))
	require.Equal(t, 18, estimate.OutputTokens)
	estimate = gatewayRequestBudgetEstimate(nil, &service.APIKey{}, "unlisted", []byte(`{"n":3,"max_output_tokens":9223372036854775807}`))
	require.True(t, estimate.InvalidBounds)
}
