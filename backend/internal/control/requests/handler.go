package requests

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/requests"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

type Handler struct {
	store     *domain.Store
	access    identity.AccessEvaluator
	cursorKey [32]byte
}

func New(db *sql.DB, access identity.AccessEvaluator, cursorSecret string) *Handler {
	return &Handler{
		store: domain.NewStore(db), access: access,
		cursorKey: sha256.Sum256([]byte("omnihive/request-cursor/v1\x00" + cursorSecret)),
	}
}

func Register(parent *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	p := parent.Group("/workspaces/:workspace_id/projects/:project_id/requests",
		append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	p.GET("", h.serve(http.StatusOK, false, h.list))
	p.GET("/:id", h.serve(http.StatusOK, false, h.detail))
	p.GET("/:id/route-explain", h.serve(http.StatusOK, false, h.explain))
	p.POST("/:id/exports", h.serve(http.StatusAccepted, true, h.startExport))
	p.GET("/:id/exports/:export_id/download", h.serve(0, true, h.download))
}

type request struct {
	*gin.Context
	actor   int64
	version int64
	id      string
	view    domain.Projection
}

type endpoint func(request) (any, error)

func (h *Handler) serve(status int, export bool, fn endpoint) gin.HandlerFunc {
	return func(c *gin.Context) {
		who, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || who.UserID <= 0 {
			writeFault(c, fault.Unauthenticated)
			return
		}
		scope := domain.Scope{
			WorkspaceID: positiveID(c.Param("workspace_id")), ProjectID: positiveID(c.Param("project_id")),
		}
		if scope.WorkspaceID == 0 || scope.ProjectID == 0 {
			writeFault(c, fault.NotFound)
			return
		}
		a, err := h.authorize(c.Request.Context(), who.UserID, scope, export)
		if err != nil {
			writeFault(c, err)
			return
		}
		id := c.Param("id")
		if id != "" && !validRequestID(id) {
			writeFault(c, fault.NotFound)
			return
		}
		out, err := fn(request{Context: c, actor: who.UserID, version: a.Version, id: id, view: perspective(a)})
		if err != nil {
			if !c.Writer.Written() {
				writeFault(c, err)
			} else {
				slog.WarnContext(c.Request.Context(), "request export stream interrupted", "request_id", c.Writer.Header().Get("X-Request-ID"), "error", err)
			}
			return
		}
		if status != 0 {
			message := "success"
			if status == http.StatusAccepted {
				message = "accepted"
			}
			c.JSON(status, gin.H{"code": 0, "message": message, "data": out})
		}
	}
}

func (h *Handler) authorize(ctx context.Context, actor int64, scope domain.Scope, export bool) (identity.Access, error) {
	a, err := h.access.Evaluate(ctx, actor, scope.WorkspaceID, scope.ProjectID, domain.ReadGrant)
	if err != nil {
		return a, err
	}
	if a.ActorID != actor || a.WorkspaceID != scope.WorkspaceID || a.ProjectID != scope.ProjectID {
		return a, fault.NotFound
	}
	if err = a.Require(domain.ReadGrant); err != nil {
		return a, err
	}
	if export {
		err = a.Require(domain.ExportGrant)
	}
	return a, err
}

func perspective(a identity.Access) domain.Projection {
	return domain.Projection{
		Scope:   domain.Scope{WorkspaceID: a.WorkspaceID, ProjectID: a.ProjectID},
		Finance: a.Require(domain.FinanceGrant) == nil, Connections: a.Require(domain.ConnectionGrant) == nil,
		Export: a.Require(domain.ExportGrant) == nil,
	}
}

func (h *Handler) detail(r request) (any, error) {
	return h.store.Detail(r.Request.Context(), r.view, r.id)
}

func (h *Handler) explain(r request) (any, error) {
	return h.store.Explain(r.Request.Context(), r.view, r.id)
}

func positiveID(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 || strconv.FormatInt(v, 10) != s {
		return 0
	}
	return v
}

func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	id, err := uuid.Parse(s)
	return err == nil && id.String() == s
}

func validRequestID(s string) bool {
	for _, prefix := range []string{"legacy-receipt-", "legacy-usage-"} {
		if id, ok := strings.CutPrefix(s, prefix); ok {
			return positiveID(id) != 0
		}
	}
	return validUUID(s)
}

var snapshotExpired = &fault.Fault{Status: http.StatusGone, Reason: "SNAPSHOT_EXPIRED", Message: "List cursor has expired."}

func writeFault(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		slog.ErrorContext(c.Request.Context(), "request read failed", "request_id", c.Writer.Header().Get("X-Request-ID"), "error", err)
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Request service is unavailable."}
	}
	category, retry := "validation", "never"
	switch f.Status {
	case http.StatusUnauthorized:
		category = "authentication"
	case http.StatusForbidden, http.StatusNotFound:
		category = "authorization"
	case http.StatusConflict:
		category = "conflict"
	case http.StatusServiceUnavailable:
		category, retry = "persistence", "after_delay"
	}
	fields := []gin.H{}
	if f.Field != "" {
		fields = append(fields, gin.H{"path": f.Field, "reason": f.Reason})
	}
	c.AbortWithStatusJSON(f.Status, gin.H{
		"code": f.Status, "message": f.Message, "reason": f.Reason, "data": nil,
		"metadata": gin.H{
			"request_id": c.Writer.Header().Get("X-Request-ID"), "category": category, "retry": retry,
			"fields": fields, "operation_id": nil, "status_url": nil,
		},
	})
}
