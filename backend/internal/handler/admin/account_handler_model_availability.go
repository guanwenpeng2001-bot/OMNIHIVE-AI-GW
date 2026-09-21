package admin

import (
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type modelAvailabilityDeclarationResponse struct {
	service.ModelAvailabilityDeclaration
	Valid bool `json:"valid"`
}

type modelAvailabilityResponse struct {
	service.AccountModelCatalogBaseline
	Declarations []modelAvailabilityDeclarationResponse `json:"declarations"`
}

func modelAvailabilityActor(c *gin.Context) (int64, bool) {
	subject, exists := middleware.GetAuthSubjectFromContext(c)
	role, hasRole := middleware.GetUserRoleFromContext(c)
	if !exists || subject.UserID <= 0 || (!domain.WorkspaceAdministration(c.Request.Context()) && (!hasRole || role != service.RoleAdmin)) {
		response.ErrorWithDetails(c, http.StatusForbidden, "An administrator identity is required", "model_availability_forbidden", nil)
		return 0, false
	}
	return subject.UserID, true
}

func (handler *AccountHandler) modelAvailabilityAccount(c *gin.Context) (*service.Account, bool) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		writeModelAvailabilityError(c, service.ErrModelAvailabilityInvalid)
		return nil, false
	}
	account, err := handler.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return nil, false
	}
	return account, true
}

func (handler *AccountHandler) writeModelAvailability(c *gin.Context, account *service.Account) {
	var credential *service.Account
	if account.IsShadow() {
		// Failure to resolve the parent makes evidence invalid without exposing
		// the parent's credentials or borrowing a different account's identity.
		credential, _ = handler.adminService.GetAccount(c.Request.Context(), *account.ParentAccountID)
	}
	result := modelAvailabilityResponse{AccountModelCatalogBaseline: account.ModelCatalogBaseline(), Declarations: []modelAvailabilityDeclarationResponse{}}
	for _, declaration := range account.ModelAvailabilityDeclarations() {
		result.Declarations = append(result.Declarations, modelAvailabilityDeclarationResponse{ModelAvailabilityDeclaration: declaration, Valid: declaration.ValidFor(account, credential, time.Now().UTC())})
	}
	response.Success(c, result)
}

// GetModelAvailability is registered by the shared admin route owner.
func (handler *AccountHandler) GetModelAvailability(c *gin.Context) {
	if _, ok := modelAvailabilityActor(c); !ok {
		return
	}
	account, ok := handler.modelAvailabilityAccount(c)
	if !ok {
		return
	}
	handler.writeModelAvailability(c, account)
}

func (handler *AccountHandler) DeclareModelAvailability(c *gin.Context) {
	handler.changeModelAvailability(c, false)
}
func (handler *AccountHandler) RevokeModelAvailability(c *gin.Context) {
	handler.changeModelAvailability(c, true)
}

func (handler *AccountHandler) changeModelAvailability(c *gin.Context, revoke bool) {
	actor, ok := modelAvailabilityActor(c)
	if !ok {
		return
	}
	var request struct {
		IdentityRevision *string    `json:"identity_revision"`
		RulesRevision    *string    `json:"rules_revision"`
		SnapshotRevision *string    `json:"snapshot_revision"`
		Model            string     `json:"model"`
		Endpoint         string     `json:"endpoint"`
		Reason           string     `json:"reason"`
		Confirmed        bool       `json:"confirmed"`
		ExpiresAt        *time.Time `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.IdentityRevision == nil || request.RulesRevision == nil || request.SnapshotRevision == nil {
		writeModelAvailabilityError(c, service.ErrModelAvailabilityInvalid)
		return
	}
	var baseline service.AccountModelCatalogBaseline
	for target, raw := range map[*int64]string{&baseline.IdentityRevision: *request.IdentityRevision, &baseline.RulesRevision: *request.RulesRevision, &baseline.SnapshotRevision: *request.SnapshotRevision} {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 || value == math.MaxInt64 {
			writeModelAvailabilityError(c, service.ErrModelAvailabilityInvalid)
			return
		}
		*target = value
	}
	account, ok := handler.modelAvailabilityAccount(c)
	if !ok {
		return
	}
	if handler.accountTestService == nil {
		response.InternalError(c, "Account test service unavailable")
		return
	}
	change := service.ModelAvailabilityChange{AccountModelCatalogBaseline: baseline, Model: request.Model, Endpoint: request.Endpoint, Reason: request.Reason, Confirmed: request.Confirmed, ExpiresAt: request.ExpiresAt}
	if err := handler.accountTestService.ChangeModelAvailability(c.Request.Context(), account, change, actor, revoke); err != nil {
		writeModelAvailabilityError(c, err)
		return
	}
	account, ok = handler.modelAvailabilityAccount(c)
	if ok {
		handler.writeModelAvailability(c, account)
	}
}

func writeModelAvailabilityError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAccountModelCatalogConflict):
		response.ErrorWithDetails(c, http.StatusConflict, "Account configuration changed; read and confirm again", "model_availability_conflict", nil)
	case errors.Is(err, service.ErrModelAvailabilityNotFound):
		response.ErrorWithDetails(c, http.StatusNotFound, "Model availability declaration not found", "model_availability_not_found", nil)
	case errors.Is(err, service.ErrModelAvailabilityInvalid):
		response.ErrorWithDetails(c, http.StatusBadRequest, "A concrete mapped target, endpoint, reason and confirmation are required", "model_availability_invalid", nil)
	default:
		response.InternalError(c, "Could not persist model availability")
	}
}
