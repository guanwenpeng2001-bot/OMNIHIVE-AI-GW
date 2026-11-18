package admin

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) SetStagedAccountImportService(imports *service.StagedAccountImportService) {
	h.stagedImports = imports
}

// A nonempty legacy key must pass the durable shared-mode guard even when the
// generic coordinator is disabled. No-key legacy policy remains unchanged.
func (h *AccountHandler) reserveLegacyImportKey(c *gin.Context, req DataImportRequest) bool {
	var actorID int64
	if subject, ok := middleware.GetAuthSubjectFromContext(c); ok {
		actorID = subject.UserID
	}
	if err := h.stagedImports.ReserveLegacyKey(c.Request.Context(), h.credentialDrafts, actorID, service.IdempotencyExecuteOptions{
		IdempotencyKey: c.GetHeader("Idempotency-Key"), Method: c.Request.Method,
		Route: c.FullPath(), Payload: req,
	}); err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	return true
}

// Only the opt-in staged branch uses durable receipts. Authentication and
// workspace authorization still run before this handler, including on replay.
func (h *AccountHandler) executeStagedDataImport(c *gin.Context, req DataImportRequest) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.ErrorFrom(c, service.ErrStagedImportActorRequired)
		return
	}
	result, replayed, err := h.stagedImports.Execute(c.Request.Context(), h.credentialDrafts, subject.UserID, c.GetHeader("Idempotency-Key"), req, func(ctx context.Context) (int, error) {
		result, err := h.stageDataAccounts(ctx, req)
		return result.AccountCreated, err
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, result)
}
