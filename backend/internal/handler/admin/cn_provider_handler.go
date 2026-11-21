package admin

import (
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// CNProviderHandler 暴露国产供应商（kimi/zhipu/deepseek）的额度与余额查询端点。
//
//   - GET /admin/cn-providers/accounts/:id/quota   Coding Plan 滚动窗口用量（kimi/zhipu）
//   - GET /admin/cn-providers/accounts/:id/balance  payg 账号余额（原生及已验证兼容供应商）
//
// 智谱（zhipu）无余额端点，故同一账号仅 quota 或 balance 其一可用：服务端按账号
// platform + account_mode 校验并返回明确错误（见 CNProvider*Service 的 load*Account）。
type CNProviderHandler struct {
	quotaService   *service.CNProviderQuotaService
	balanceService *service.CNProviderBalanceService
}

func NewCNProviderHandler(
	quotaService *service.CNProviderQuotaService,
	balanceService *service.CNProviderBalanceService,
) *CNProviderHandler {
	return &CNProviderHandler{
		quotaService:   quotaService,
		balanceService: balanceService,
	}
}

// QueryQuota 查询 Coding Plan 滚动窗口用量（5h + weekly）。
func (h *CNProviderHandler) QueryQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h == nil || h.quotaService == nil {
		response.BadRequest(c, "cn provider quota service is not enabled")
		return
	}
	result, err := h.quotaService.QueryUsage(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// QueryBalance 查询原生及已验证兼容供应商的 payg 账号余额。
func (h *CNProviderHandler) QueryBalance(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h == nil || h.balanceService == nil {
		response.BadRequest(c, "cn provider balance service is not enabled")
		return
	}
	result, err := h.balanceService.QueryAccountBalance(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// QueryUsage is a manual, windowed consumed-usage query. Native quota and
// balance endpoints keep their existing behavior.
func (handler *CNProviderHandler) QueryUsage(ctx *gin.Context) {
	accountID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(ctx, "Invalid account ID")
		return
	}
	if len(ctx.Request.URL.RawQuery) > 2048 {
		response.BadRequest(ctx, "Invalid usage query parameters")
		return
	}
	query, err := url.ParseQuery(ctx.Request.URL.RawQuery)
	if err != nil {
		response.BadRequest(ctx, "Invalid usage query parameters")
		return
	}
	if handler == nil || handler.balanceService == nil {
		response.BadRequest(ctx, "cn provider usage service is not enabled")
		return
	}
	result, err := handler.balanceService.QueryBillingUsage(ctx.Request.Context(), accountID, query)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, result)
}
