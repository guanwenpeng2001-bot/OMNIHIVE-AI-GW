package admin

import (
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// SyncQianwenPricing fetches an account-native price snapshot. By default it is
// a preview. An explicit group_id applies supported cards through UpdateGroup,
// including its validation and auth-cache invalidation. Only a single-account
// Qianwen group is eligible, so another region/account cannot inherit its price.
func (h *AccountHandler) SyncQianwenPricing(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_ACCOUNT_ID", "invalid account ID"))
		return
	}
	var req struct {
		service.QianwenPricingOptions
		GroupID *int64 `json:"group_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_REQUEST", "invalid pricing sync request"))
		return
	}
	ctx := c.Request.Context()
	account, err := h.adminService.GetAccount(ctx, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var group *service.Group
	if req.GroupID != nil {
		group, err = h.adminService.GetGroup(ctx, *req.GroupID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		accounts, total, err := h.adminService.ListAccounts(ctx, 1, 2, "", "", "", "", group.ID, "", "id", "asc")
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if group.Platform != service.PlatformQianwen || total != 1 || len(accounts) != 1 || accounts[0].ID != id {
			response.ErrorFrom(c, infraerrors.BadRequest("UNSUPPORTED_PRICING_GROUP", "target must be a Qianwen group bound only to this account"))
			return
		}
	}
	result, err := h.accountTestService.FetchQianwenPricing(ctx, account, req.QianwenPricingOptions)
	if err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("QIANWEN_PRICING_FETCH_FAILED", err.Error()))
		return
	}
	// Even unsupported FX has durable explicit source status, never price rows.
	if err := h.adminService.UpdateAccountExtra(ctx, id, map[string]any{"qianwen_pricing_sync": result}); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	written := 0
	if group != nil && result.Status == "ready" {
		cards := mergeQianwenPricing(group.ModelPricing, result)
		if _, err := h.adminService.UpdateGroup(ctx, group.ID, &service.UpdateGroupInput{ModelPricing: &cards}); err != nil {
			response.ErrorFrom(c, err)
			return
		}
		written = result.Imported
	}
	response.Success(c, gin.H{"result": result, "written": written, "group_id": req.GroupID})
}

func mergeQianwenPricing(existing []service.ChannelModelPricing, result *service.QianwenPricingResult) []service.ChannelModelPricing {
	matched := map[string]bool{}
	cards := []service.ChannelModelPricing{}
	for _, m := range result.Models {
		if m.Pricing != nil {
			cards = append(cards, *m.Pricing)
			matched[m.Model] = true
		}
	}
	for _, card := range existing {
		// Replace only exact imported model IDs; preserve manual cards for other
		// models and keep imported exact cards ahead of existing wildcards.
		copy := card.Clone()
		copy.Models = nil
		for _, model := range card.Models {
			if !matched[model] {
				copy.Models = append(copy.Models, model)
			}
		}
		if len(copy.Models) > 0 {
			cards = append(cards, copy)
		}
	}
	return cards
}
