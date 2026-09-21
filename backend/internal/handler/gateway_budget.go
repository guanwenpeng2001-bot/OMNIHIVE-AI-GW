package handler

import (
	"net/http"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/gatewayerror"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// reserveGatewayRequestBudget takes the request-level budget hold before a
// request is forwarded.
//
// The hold is atomic and lives on the request plan, so failover retries reuse
// the same reservation instead of holding twice. It returns false when the
// request was already rejected (the caller must return immediately). Keys
// without a period budget keep their previous path with zero extra work.
func reserveGatewayRequestBudget(c *gin.Context, key *service.APIKey, model string, body []byte) bool {
	if c == nil || key == nil || !key.HasPeriodBudget() {
		return true
	}
	plan := service.GatewayRequestPlanFromContext(c.Request.Context())
	if plan == nil {
		abortGatewayRequestBudget(c, service.ErrAPIKeyBudgetUnavailable)
		return false
	}
	estimate := service.RequestBudgetEstimate{
		Model:           model,
		InputTokens:     estimateGatewayRequestInputTokens(body),
		OutputTokens:    declaredGatewayMaxOutputTokens(body),
		RateMultiplier:  gatewayRequestRateMultiplier(key),
		SkipBalanceHold: key.Group != nil && key.Group.IsSubscriptionType(),
	}
	if err := plan.ReserveBudget(c.Request.Context(), estimate); err != nil {
		abortGatewayRequestBudget(c, err)
		return false
	}
	return true
}

// abortGatewayRequestBudget serializes the rejection in the caller's protocol.
func abortGatewayRequestBudget(c *gin.Context, err error) {
	status, reason, message := gatewayBudgetErrorResponse(err)
	gatewayerror.Write(c, status, reason, message)
}

func gatewayBudgetErrorResponse(err error) (status int, reason, message string) {
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
