// Package payments exposes reviewed links between project-scoped BE-16
// adjustments and the payer's personal payment evidence.
package payments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	pay "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/payments"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

type StepUp interface {
	HasStepUpGrant(context.Context, int64, string) (bool, error)
}
type Handler struct {
	Store  pay.Store
	Access identity.AccessEvaluator
	Step   StepUp
}

func Register(v2 *gin.RouterGroup, h Handler, session ...gin.HandlerFunc) {
	g := v2.Group("/workspaces/:workspace_id/projects/:project_id/billing/adjustments/:id/payment-evidence", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	g.GET("", h.serve)
	g.PUT("/:event_id", h.serve)
}

func (h Handler) ReadOperation(ctx context.Context, workspace, actor int64, id string) (identity.Operation, error) {
	command, err := uuid.Parse(id)
	if err != nil {
		return identity.Operation{}, fault.NotFound
	}
	project, err := h.Store.OperationProject(ctx, workspace, actor, command)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.Operation{}, fault.NotFound
	}
	if err != nil {
		return identity.Operation{}, err
	}
	access, err := h.Access.Evaluate(ctx, actor, workspace, project, "billing.read")
	if err != nil {
		return identity.Operation{}, err
	}
	if err := access.Require("billing.read"); err != nil {
		return identity.Operation{}, err
	}
	id = command.String()
	return identity.Operation{
		ID: id, State: "succeeded", ProjectID: project, OwnerID: actor,
		StatusURL: fmt.Sprintf("/api/v2/workspaces/%d/operations/%s", workspace, id),
	}, nil
}

func (h Handler) serve(c *gin.Context) {
	ctx := c.Request.Context()
	actor, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || actor.UserID <= 0 {
		fail(c, fault.Unauthenticated)
		return
	}
	w, _ := strconv.ParseInt(c.Param("workspace_id"), 10, 64)
	p, _ := strconv.ParseInt(c.Param("project_id"), 10, 64)
	adj, err := uuid.Parse(c.Param("id"))
	if err != nil || w <= 0 || p <= 0 {
		fail(c, fault.NotFound)
		return
	}
	grant := "billing.read"
	mutate := c.Request.Method == http.MethodPut
	if mutate {
		grant = "billing.adjust"
	}
	a, err := h.Access.Evaluate(ctx, actor.UserID, w, p, grant)
	if err == nil {
		err = a.Require(grant)
	}
	if err != nil {
		fail(c, err)
		return
	}
	sc := pay.EvidenceScope{Workspace: w, Project: p, Actor: actor.UserID}
	if mutate {
		sid := c.GetString(middleware.ContextKeySessionID)
		if sid == "" || c.GetString("auth_method") == "admin_api_key" || h.Step == nil {
			fail(c, fault.Forbidden)
			return
		}
		ok, err = h.Step.HasStepUpGrant(ctx, actor.UserID, sid)
		if err != nil {
			fail(c, err)
			return
		}
		if !ok {
			fail(c, &fault.Fault{Status: 403, Reason: "STEP_UP_REQUIRED", Message: "Verify this session to link payment evidence."})
			return
		}
		event, err := strconv.ParseInt(c.Param("event_id"), 10, 64)
		if err == nil && event > 0 {
			err = h.Store.LinkAdjustment(ctx, sc, adj, event, a.Require("billing.payers.read") == nil)
		} else {
			err = fault.Invalid("/event_id")
		}
		if err != nil {
			fail(c, err)
			return
		}
		c.Status(http.StatusNoContent)
		return
	}
	items, err := h.Store.Evidence(ctx, sc, adj)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"code": 0, "message": "success", "data": gin.H{"items": items}})
}
func fail(c *gin.Context, err error) {
	var f *fault.Fault
	if errors.Is(err, sql.ErrNoRows) {
		f = fault.NotFound
	} else if !errors.As(err, &f) {
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Payment evidence is unavailable."}
	}
	category, retry := "authorization", "never"
	if f.Status == 503 {
		category, retry = "persistence", "after_delay"
	} else if f.Status == 422 {
		category = "validation"
	}
	fields := []gin.H{}
	if f.Field != "" {
		fields = append(fields, gin.H{"path": f.Field, "reason": f.Reason})
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "reason": f.Reason, "message": f.Message, "data": nil,
		"metadata": gin.H{"request_id": c.Writer.Header().Get("X-Request-ID"), "fields": fields, "category": category, "retry": retry, "operation_id": nil, "status_url": nil}})
}
