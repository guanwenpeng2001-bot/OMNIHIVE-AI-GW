package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/gatewayerror"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// reserveGatewayRequestBudget takes the request-level budget hold before a
// request is forwarded.
//
// The hold is atomic and lives on the request plan, so failover retries reuse
// the same reservation instead of holding twice. It returns false when the
// request was already rejected (the caller must return immediately).
func reserveGatewayRequestBudget(c *gin.Context, key *service.APIKey, model string, body []byte) bool {
	if c == nil || key == nil {
		return true
	}
	if err := reserveRequestBudget(c.Request.Context(), key, model, body, c.GetHeader("Content-Type")); err != nil {
		abortGatewayRequestBudget(c, err)
		return false
	}
	return true
}

func reserveRequestBudget(ctx context.Context, key *service.APIKey, model string, body []byte, contentType ...string) error {
	plan := service.GatewayRequestPlanFromContext(ctx)
	if !key.HasPeriodBudget() && !plan.HasProjectBudgetCoordinator() {
		return nil
	}
	if plan == nil {
		return service.ErrAPIKeyBudgetUnavailable
	}
	return plan.ReserveBudget(ctx, gatewayRequestBudgetEstimate(plan, key, model, body, contentType...))
}

// Media units come from the inbound protocol and the generation parameters.
func gatewayRequestBudgetEstimate(plan *service.GatewayRequestPlan, key *service.APIKey, model string, body []byte, contentType ...string) service.RequestBudgetEstimate {
	estimate := service.RequestBudgetEstimate{
		Model:           model,
		SkipKeyBudget:   !key.HasPeriodBudget(),
		RateMultiplier:  gatewayRequestRateMultiplier(key),
		SkipBalanceHold: key != nil && key.Group != nil && key.Group.IsSubscriptionType(),
	}
	if key != nil {
		estimate.Group = key.Group
	}
	estimate.ServiceTier = gjson.GetBytes(body, "service_tier").String()
	estimate.ReasoningEffort = gjson.GetBytes(body, "reasoning.effort").String()
	if plan != nil && budgetEstimateIgnoresBody(plan.Snapshot().Protocol) {
		estimate.Meter = requestMediaMeter(plan.Snapshot(), body, contentType...)
		return estimate
	}
	estimate.InputTokens = estimateGatewayRequestInputTokens(body)
	estimate.OutputTokens = declaredGatewayMaxOutputTokens(body)
	return estimate
}

// budgetEstimateIgnoresBody 报告该协议是否"请求体不含 token 用量"。
func budgetEstimateIgnoresBody(protocol string) bool {
	switch protocol {
	case "images", "media", "audio", "realtime":
		return true
	}
	return false
}

// abortGatewayRequestBudget serializes the rejection in the caller's protocol.
func abortGatewayRequestBudget(c *gin.Context, err error) {
	status, reason, message := gatewayBudgetErrorResponse(err)
	gatewayerror.Write(c, status, reason, message)
}

func gatewayBudgetErrorResponse(err error) (status int, reason, message string) {
	var f *fault.Fault
	if errors.As(err, &f) {
		return f.Status, f.Reason, f.Message
	}
	appErr := infraerrors.FromError(err)
	switch appErr.Reason {
	case service.ErrAPIKeyBudgetExceeded.Reason, service.ErrAPIKeyBudgetInsufficientBalance.Reason:
		return http.StatusTooManyRequests, appErr.Reason, appErr.Message
	default:
		// Any other failure means the hold could not be taken. Fail closed: an
		// unenforced budget would silently spend beyond the configured limit.
		return http.StatusServiceUnavailable, service.ErrAPIKeyBudgetUnavailable.Reason, "api key period budget could not be reserved"
	}
}

// estimateGatewayRequestInputTokens bounds the input size from the request body.
// It is an admission estimate only; settlement always uses real usage.
func estimateGatewayRequestInputTokens(body []byte) int {
	tokens, err := service.EstimateLocalCountTokens(body)
	if err != nil || tokens <= 0 {
		return 0
	}
	return tokens
}

// declaredGatewayMaxOutputTokens reads the declared output bound across the
// supported protocols. Zero means unknown and the estimate uses its default.
func declaredGatewayMaxOutputTokens(body []byte) int {
	if len(body) == 0 {
		return 0
	}
	for _, path := range []string{
		"max_tokens",
		"max_completion_tokens",
		"max_output_tokens",
		"generation_config.max_output_tokens",
		"generationConfig.maxOutputTokens",
	} {
		if value := gjson.GetBytes(body, path); value.Exists() && value.Int() > 0 {
			return int(value.Int())
		}
	}
	return 0
}

func gatewayRequestRateMultiplier(key *service.APIKey) float64 {
	if key == nil || key.Group == nil || key.Group.RateMultiplier <= 0 {
		return 1
	}
	return key.Group.RateMultiplier
}
