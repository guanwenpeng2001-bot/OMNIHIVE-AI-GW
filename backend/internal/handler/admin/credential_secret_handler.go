package admin

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// CredentialSecretHandler only exposes metadata and explicit mutations. There
// is intentionally no plaintext or ciphertext read/export endpoint.
type CredentialSecretHandler struct {
	secrets *service.CredentialSecretService
}

func NewCredentialSecretHandler(secrets *service.CredentialSecretService) *CredentialSecretHandler {
	return &CredentialSecretHandler{secrets: secrets}
}

func (h *CredentialSecretHandler) Metadata(ctx *gin.Context) {
	accountID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.ErrorFrom(ctx, service.ErrCredentialSecretInvalid)
		return
	}
	metadata, err := h.secrets.Metadata(ctx.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, metadata)
}

func (h *CredentialSecretHandler) Convert(ctx *gin.Context) {
	var request struct {
		Confirmed       bool   `json:"confirmed"`
		AccountRevision string `json:"account_revision"`
	}
	accountID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || accountID <= 0 || ctx.ShouldBindJSON(&request) != nil {
		response.ErrorFrom(ctx, service.ErrCredentialSecretInvalid)
		return
	}
	metadata, err := h.secrets.Convert(ctx.Request.Context(), accountID, request.AccountRevision, request.Confirmed)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, metadata)
}

func (h *CredentialSecretHandler) Rotate(ctx *gin.Context) {
	var request struct {
		ExpectedVersion int64          `json:"expected_version"`
		Credentials     map[string]any `json:"credentials"`
	}
	accountID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || accountID <= 0 || ctx.ShouldBindJSON(&request) != nil {
		response.ErrorFrom(ctx, service.ErrCredentialSecretInvalid)
		return
	}
	metadata, err := h.secrets.Rotate(ctx.Request.Context(), accountID, request.ExpectedVersion, request.Credentials)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, metadata)
}

func (h *CredentialSecretHandler) Revoke(ctx *gin.Context) {
	var request struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	accountID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || accountID <= 0 || ctx.ShouldBindJSON(&request) != nil {
		response.ErrorFrom(ctx, service.ErrCredentialSecretInvalid)
		return
	}
	metadata, err := h.secrets.Revoke(ctx.Request.Context(), accountID, request.ExpectedVersion)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, metadata)
}
