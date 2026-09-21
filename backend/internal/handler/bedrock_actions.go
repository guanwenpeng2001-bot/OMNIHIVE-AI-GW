package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Bedrock standard-entry media and vector actions (embeddings, rerank and the
// image generation/edit/variation contract).
//
// AWS Bedrock is the one platform whose accounts are selected by the
// Anthropic-shaped trunk: the OpenAI-compatible scheduler requires
// IsOpenAICompatible accounts and therefore never returns a Bedrock account
// (openai_gateway_scheduling.go, "platform_mismatch"). These handlers therefore
// use the same trunk selection as GatewayHandler.CountTokens, call the Bedrock
// adapter on OpenAIGatewayService (bedrock_actions.go), and record usage through
// the OpenAI usage path, because those adapters answer the OpenAI-shaped
// envelope.
//
// Discipline shared with the sibling media adapters: one bounded account
// selection, no adapter-side retry (a generation is not idempotent), the client
// context and deadline forwarded upstream, and usage recorded only after a
// successful forward.

type bedrockActionAdmission struct {
	key          *service.APIKey
	subject      middleware2.AuthSubject
	subscription *service.UserSubscription
	body         []byte
	model        string
	pricingAt    time.Time
}

// BedrockEmbeddings handles POST /v1/embeddings for Bedrock groups by forwarding
// the OpenAI embeddings contract to the Titan text embedding family
// (ForwardBedrockEmbeddings -> InvokeModel).
func (h *GatewayHandler) BedrockEmbeddings(c *gin.Context) {
	body, ok := h.readBedrockActionBody(c)
	if !ok {
		return
	}
	admission, ok := h.admitBedrockAction(c, body, strings.TrimSpace(gjson.GetBytes(body, "model").String()), service.ContentModerationProtocolOpenAIChat)
	if !ok {
		return
	}
	result, account, ok := h.forwardBedrockAction(c, admission, func(account *service.Account) (*service.OpenAIForwardResult, error) {
		return h.openAIGatewayService.ForwardBedrockEmbeddings(c.Request.Context(), c, account, admission.body, "")
	})
	if !ok {
		return
	}
	h.recordBedrockActionUsage(c, admission, account, result, "embeddings")
}

// BedrockRerank handles the standard flat rerank contract for Bedrock groups,
// bridging to the Agents for Amazon Bedrock Runtime Rerank operation.
func (h *GatewayHandler) BedrockRerank(c *gin.Context) {
	body, ok := h.readBedrockActionBody(c)
	if !ok {
		return
	}
	admission, ok := h.admitBedrockAction(c, body, strings.TrimSpace(gjson.GetBytes(body, "model").String()), service.ContentModerationProtocolOpenAIChat)
	if !ok {
		return
	}
	result, account, ok := h.forwardBedrockAction(c, admission, func(account *service.Account) (*service.OpenAIForwardResult, error) {
		return h.openAIGatewayService.ForwardBedrockRerank(c.Request.Context(), c, account, admission.body)
	})
	if !ok {
		return
	}
	h.recordBedrockActionUsage(c, admission, account, result, "rerank")
}

// BedrockImages handles /v1/images/generations, /v1/images/edits and
// /v1/images/variations for Bedrock groups, dispatching on the serving family
// (Nova Canvas, Titan Image Generator, Stability).
func (h *GatewayHandler) BedrockImages(c *gin.Context) {
	action := BedrockImageActionForPath(c.Request.URL.Path)
	body, ok := h.readBedrockActionBody(c)
	if !ok {
		return
	}
	parsed, err := service.ParseBedrockImagesRequest(c, body, action)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	admission, ok := h.admitBedrockAction(c, body, parsed.Model, service.ContentModerationProtocolOpenAIImages)
	if !ok {
		return
	}
	if !service.GroupAllowsImageGeneration(admission.key.Group) {
		h.errorResponse(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}
	result, account, ok := h.forwardBedrockAction(c, admission, func(account *service.Account) (*service.OpenAIForwardResult, error) {
		return h.openAIGatewayService.ForwardBedrockImageGeneration(c.Request.Context(), c, account, admission.body, parsed, action, "")
	})
	if !ok {
		return
	}
	h.recordBedrockActionUsage(c, admission, account, result, action)
}

// BedrockImageActionForPath maps the inbound route onto the image action. The
// variations route is registered by the multimodal ingress; deriving the action
// here lets one handler serve all three image contracts.
func BedrockImageActionForPath(path string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(path), "/")
	switch {
	case strings.HasSuffix(trimmed, "/images/variations"):
		return "image.variation"
	case strings.HasSuffix(trimmed, "/images/edits"):
		return "image.edit"
	default:
		return "image.generate"
	}
}

func (h *GatewayHandler) readBedrockActionBody(c *gin.Context) ([]byte, bool) {
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return nil, false
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return nil, false
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return nil, false
	}
	return body, true
}

// admitBedrockAction runs the shared admission pipeline: platform pin, model
// presence, composite target, security audit, billing eligibility and the
// request-level pricing time.
func (h *GatewayHandler) admitBedrockAction(c *gin.Context, body []byte, model, auditProtocol string) (*bedrockActionAdmission, bool) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return nil, false
	}
	// The Bedrock product entry is the hosting platform; an explicit Composite
	// route resolves to it as well. A plain OpenAI/Anthropic group must not reach
	// the Bedrock adapter through these routes.
	if openAICompatibleRequestPlatform(c.Request.Context(), apiKey) != service.PlatformBedrock {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "This endpoint requires a Bedrock group or an explicit Bedrock model route")
		return nil, false
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return nil, false
	}
	if strings.TrimSpace(model) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, false
	}
	setOpsRequestContext(c, model, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	reqLog := requestLogger(c, "handler.gateway.bedrock_actions",
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
		zap.String("model", model),
	)
	ensureCompositeTargetPlatform(c, apiKey, model)
	if !compositeTargetPlatformAllowed(c, apiKey, model, service.PlatformBedrock) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this Bedrock endpoint for composite groups")
		return nil, false
	}
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, auditProtocol, model, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return nil, false
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		status, code, message, _ := billingErrorDetails(err)
		writeAdmissionRejectionHeaders(c, err)
		h.errorResponse(c, status, code, message)
		return nil, false
	}
	// Freeze the request-level pricing time without installing the OpenAI profit
	// gate: the Anthropic trunk does not profit-gate Bedrock traffic either.
	pricingCtx, pricingAt := h.openAIGatewayService.WithOpenAIRequestPricingContext(service.WithOpenAIProfitControlSuppressed(c.Request.Context()), apiKey.GroupID)
	c.Request = c.Request.WithContext(pricingCtx)
	return &bedrockActionAdmission{key: apiKey, subject: subject, subscription: subscription, body: body, model: model, pricingAt: pricingAt}, true
}

// forwardBedrockAction selects one Bedrock account through the Anthropic trunk
// scheduler and runs the forward. A failed forward releases the sticky session
// slot so a retry can pick another account; a successful one keeps it, and usage
// is never recorded for a failure.
func (h *GatewayHandler) forwardBedrockAction(c *gin.Context, admission *bedrockActionAdmission, forward func(account *service.Account) (*service.OpenAIForwardResult, error)) (*service.OpenAIForwardResult, *service.Account, bool) {
	parsed := &service.ParsedRequest{
		Body:  service.NewRequestBodyRef(admission.body),
		Model: admission.model,
		SessionContext: &service.SessionContext{
			ClientIP:  ip.GetClientIP(c),
			UserAgent: c.GetHeader("User-Agent"),
			APIKeyID:  admission.key.ID,
		},
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsed)
	account, err := h.gatewayService.SelectAccountForModel(c.Request.Context(), admission.key.GroupID, sessionHash, admission.model)
	if err != nil || account == nil {
		cls := classifyNoAccountErrorFromGin(c, h.gatewayService, admission.key, admission.model, admission.model, service.PlatformBedrock)
		if !cls.ModelNotFound {
			markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
		}
		h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
		return nil, nil, false
	}
	setOpsSelectedAccount(c, account.ID, account.Platform)
	result, err := forward(account)
	if err != nil || result == nil {
		h.gatewayService.ReleaseAccountSession(context.Background(), account, sessionHash)
		return nil, nil, false
	}
	return result, account, true
}

func (h *GatewayHandler) recordBedrockActionUsage(c *gin.Context, admission *bedrockActionAdmission, account *service.Account, result *service.OpenAIForwardResult, action string) {
	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.openAIGatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
			Result:           result,
			APIKey:           admission.key,
			User:             admission.key.User,
			Account:          account,
			Subscription:     admission.subscription,
			InboundEndpoint:  GetInboundEndpoint(c),
			UpstreamEndpoint: result.UpstreamEndpoint,
			UserAgent:        c.GetHeader("User-Agent"),
			IPAddress:        ip.GetClientIP(c),
			APIKeyService:    h.apiKeyService,
			QuotaPlatform:    service.QuotaPlatform(c.Request.Context(), admission.key),
			SessionID:        service.ExtractClientSessionID(c),
			PricingAt:        admission.pricingAt,
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.gateway.bedrock_actions"),
				zap.String("action", action),
				zap.Int64("account_id", account.ID),
			).Error("bedrock_action.record_usage_failed", zap.Error(err))
		}
	})
}
