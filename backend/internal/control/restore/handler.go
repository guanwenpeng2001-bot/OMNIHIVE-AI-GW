package restore

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/restore"
	rs "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/restore"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type Handler struct{ commands *rs.Service }

func New(r *legacy.Runtime) *Handler { return &Handler{commands: r.Service} }

var ProviderSet = wire.NewSet(legacy.New, New)

func Register(parent *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	g := parent.Group("/admin/restores", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	g.POST("/previews", h.serve(201, h.preview))
	g.POST("", h.serve(202, h.execute))
	g.GET("/:id", h.serve(200, func(c *gin.Context, a rs.Actor) (any, error) {
		return h.commands.Get(c.Request.Context(), a, c.Param("id"))
	}))
}
func (h *Handler) serve(status int, fn func(*gin.Context, rs.Actor) (any, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		who, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || who.UserID <= 0 {
			fail(c, fault.Unauthenticated)
			return
		}
		session := ""
		if c.GetString("auth_method") != service.AuditAuthMethodAdminAPIKey {
			session = middleware.StepUpSessionKey(c, who.UserID)
		}
		db := h.commands.Targets[rs.Production].DB
		if db == nil {
			fail(c, rs.Unavailable)
			return
		}
		workspace, err := domain.ResolveDefaultWorkspaceID(c.Request.Context(), db)
		if err != nil {
			fail(c, rs.Unavailable)
			return
		}
		data, err := fn(c, rs.Actor{ID: who.UserID, Workspace: workspace, Session: session})
		if err != nil {
			fail(c, err)
			return
		}
		message := "success"
		if status == 202 {
			message = "accepted"
		}
		c.JSON(status, gin.H{"code": 0, "message": message, "data": data})
	}
}
func body(c *gin.Context, out any) error {
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return &fault.Fault{Status: 400, Reason: "INVALID_JSON", Message: "Invalid request body."}
	}
	return nil
}
func (h *Handler) preview(c *gin.Context, a rs.Actor) (any, error) {
	var cmd rs.PreviewInput
	if err := body(c, &cmd); err != nil {
		return nil, err
	}
	return h.commands.Preview(c.Request.Context(), a, c.GetHeader("Idempotency-Key"), cmd)
}
func (h *Handler) execute(c *gin.Context, a rs.Actor) (any, error) {
	var cmd rs.ExecuteInput
	if err := body(c, &cmd); err != nil {
		return nil, err
	}
	ctx := c.Request.Context()
	return h.commands.Execute(ctx, a, c.GetHeader("Idempotency-Key"), cmd)
}
func fail(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		f = rs.Unavailable
	}
	category, retry := "validation", "never"
	switch f.Status {
	case 401, 403, 404:
		category = "access"
	case 409:
		category = "conflict"
	case 503:
		category, retry = "dependency", "retry_later"
	}
	fields := []gin.H{}
	if f.Field != "" {
		fields = append(fields, gin.H{"path": f.Field, "reason": f.Reason})
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "message": f.Message, "reason": f.Reason, "data": nil, "metadata": gin.H{
		"request_id": c.Writer.Header().Get("X-Request-ID"), "category": category, "retry": retry, "fields": fields, "operation_id": nil, "status_url": nil}})
}
