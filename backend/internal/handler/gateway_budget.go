package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"unicode/utf8"

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
	return reserveGatewayRequestBudgetInContext(c, c.Request.Context(), key, model, body)
}

func reserveGatewayRequestBudgetInContext(c *gin.Context, ctx context.Context, key *service.APIKey, model string, body []byte) bool {
	if err := reserveRequestBudget(ctx, key, model, body, c.GetHeader("Content-Type")); err != nil {
		abortGatewayRequestBudget(c, err)
		return false
	}
	hit, _ := c.Get(gatewayCacheHitKey)
	if serve, ok := hit.(func()); ok {
		serve()
		return false
	}

	return true
}

func reserveRequestBudget(ctx context.Context, key *service.APIKey, model string, body []byte, contentType ...string) error {
	plan := service.GatewayRequestPlanFromContext(ctx)
	if err := validateMediaRequestAliases(plan.Snapshot(), body, contentType...); err != nil {
		return &fault.Fault{Status: http.StatusBadRequest, Reason: "invalid_request_error", Message: err.Error()}
	}
	if !key.HasPeriodBudget() && !plan.HasProjectBudgetCoordinator() && !plan.HasBalanceBudgetCheck() && !plan.RequiresLanePricing() {
		return nil
	}
	if plan == nil {
		return service.ErrAPIKeyBudgetUnavailable
	}
	estimate := gatewayRequestBudgetEstimate(plan, key, model, body, contentType...)
	err := plan.ReserveBudget(ctx, estimate)
	if errors.Is(err, service.ErrAPIKeyBudgetUnavailable) && service.GatewayBudgetModelUnknown(plan, estimate) {
		return service.ErrGatewayBudgetModelNotFound
	}
	return err
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
	if plan != nil && (budgetEstimateIgnoresBody(plan.Snapshot().Protocol) || geminiImageBudgetSize(plan.Snapshot(), body) != "") {
		estimate.Meter = requestMediaMeter(plan.Snapshot(), body, contentType...)
		return estimate
	}
	estimate.InputTokens = estimateGatewayRequestInputTokens(body)
	if plan.Snapshot().Protocol == "embeddings" {
		estimate.InputOnly = true
		return estimate
	}
	output := declaredGatewayMaxOutputTokens(body)
	if output == 0 {
		output = service.GatewayModelOutputBudgetLimit(plan, model)
		if output == 0 {
			_, output, _ = service.NormalizeBudgetEstimateBounds(estimate)
		}
	}
	reasoning, candidates := declaredGatewayReasoningTokens(body), gatewayCandidateCount(body)
	if output > math.MaxInt-reasoning || output+reasoning > math.MaxInt/candidates {
		estimate.InvalidBounds = true
		return estimate
	}
	estimate.OutputTokens = candidates * (output + reasoning)
	estimate.CacheCreationTokens = estimateGatewayCacheCreationTokens(body, estimate.InputTokens)
	return estimate
}

func gatewayCandidateCount(body []byte) int {
	count := 1
	for _, name := range []string{"n", "best_of", "generationConfig.candidateCount", "generation_config.candidate_count"} {
		if value := gjson.GetBytes(body, name); value.Type == gjson.Number && value.Int() > 1 {
			count = max(count, int(min(value.Int(), int64(math.MaxInt))))
		}
	}
	return count
}

func declaredGatewayReasoningTokens(body []byte) int {
	for _, name := range []string{"generationConfig.thinkingConfig.thinkingBudget", "generation_config.thinking_config.thinking_budget"} {
		if value := gjson.GetBytes(body, name); value.Exists() && value.Type == gjson.Number && value.Int() > 0 {
			return int(value.Int())
		}
	}
	return 0
}

func estimateGatewayCacheCreationTokens(body []byte, input int) int {
	var total int
	var inspect func(gjson.Result)
	inspect = func(value gjson.Result) {
		if value.IsObject() && value.Get("cache_control.type").String() == "ephemeral" {
			encoded, _ := json.Marshal(value.Value())
			total += (utf8.RuneCount(encoded) + 3) / 4
			return
		}
		if value.IsObject() || value.IsArray() {
			value.ForEach(func(_, child gjson.Result) bool {
				inspect(child)
				return true
			})
		}
	}
	inspect(gjson.ParseBytes(body))
	return min(total, input)
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
	if errors.Is(err, service.ErrModelPricingUnavailable) {
		return http.StatusUnprocessableEntity, "model_pricing_unavailable", "No applicable pricing is available for this model"
	}
	if errors.Is(err, service.ErrGatewayBudgetModelNotFound) {
		return http.StatusNotFound, "model_not_found", "Model does not exist or is not available for this group"
	}
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
	bound := 0
	for _, path := range []string{
		"max_tokens",
		"max_completion_tokens",
		"max_output_tokens",
		"generation_config.max_output_tokens",
		"generationConfig.maxOutputTokens",
	} {
		if value := gjson.GetBytes(body, path); value.Exists() && value.Int() > 0 {
			bound = max(bound, int(min(value.Int(), int64(math.MaxInt))))
		}
	}
	return bound
}

func gatewayRequestRateMultiplier(key *service.APIKey) float64 {
	if key == nil || key.Group == nil || key.Group.RateMultiplier <= 0 {
		return 1
	}
	return key.Group.RateMultiplier
}
