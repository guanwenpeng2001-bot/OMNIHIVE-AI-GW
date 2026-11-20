package admin

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// PreviewPricing only reads catalog and saved configuration. POST carries a
// potentially large candidate catalog; it does not apply or persist it.
func (handler *ChannelHandler) PreviewPricing(ctx *gin.Context) {
	var request struct {
		Model        string                     `json:"model" binding:"required,max=300"`
		Profile      string                     `json:"profile" binding:"max=500"`
		TaskType     string                     `json:"task_type" binding:"max=50"`
		Pricing      channelModelPricingRequest `json:"pricing" binding:"required"`
		Candidate    json.RawMessage            `json:"candidate"`
		GroupID      int64                      `json:"group_id" binding:"min=0"`
		InputTokens  int                        `json:"input_tokens" binding:"min=0,max=10000000"`
		OutputTokens int                        `json:"output_tokens" binding:"min=0,max=10000000"`
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 16<<20)
	if err := ctx.ShouldBindJSON(&request); err != nil {
		response.Error(ctx, http.StatusBadRequest, "Invalid pricing preview request")
		return
	}
	if handler.pricingService == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "Pricing catalog unavailable")
		return
	}
	card := pricingRequestToService([]channelModelPricingRequest{request.Pricing}, true)[0]
	result := service.ChannelPricingPreview{Differences: handler.pricingService.CompareChannelPricing(request.Model, request.Profile, request.TaskType, card)}
	if len(request.Candidate) != 0 {
		if request.GroupID <= 0 || handler.channelService == nil || handler.billingService == nil {
			response.Error(ctx, http.StatusBadRequest, "Source impact preview requires a saved group")
			return
		}
		sample, err := handler.channelService.PricingPreviewSample(ctx.Request.Context(), request.GroupID, card.Platform, request.Model, request.InputTokens, request.OutputTokens)
		if err != nil {
			response.ErrorFrom(ctx, err)
			return
		}
		result.Source, err = handler.pricingService.PreviewSource(request.Candidate)
		if err == nil {
			result.Impacts, err = handler.pricingService.PreviewChargeImpact(request.Candidate, handler.billingService, handler.channelService, []service.CostInput{sample})
		}
		if err != nil {
			response.Error(ctx, http.StatusBadRequest, "Cannot preview candidate pricing catalog")
			return
		}
	}
	response.Success(ctx, result)
}
