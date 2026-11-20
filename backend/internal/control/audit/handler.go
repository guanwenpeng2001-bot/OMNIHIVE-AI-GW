package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/audit"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

type Handler struct {
	Store    *domain.Store
	Observer *domain.Observer
	secret   [32]byte
}

func New(s *domain.Store, o *domain.Observer, cfg *config.Config) *Handler {
	return &Handler{s, o, sha256.Sum256([]byte("omnihive/audit/cursor/v1\x00" + cfg.JWT.Secret))}
}
func identifier(s string) int64 {
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil || n <= 0 || strconv.FormatInt(n, 10) != s {
		return 0
	}
	return n
}
func requestScope(c *gin.Context) domain.Scope {
	a, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		return domain.Scope{}
	}
	return domain.Scope{Actor: a.UserID, Workspace: identifier(c.Param("workspace_id")), Project: identifier(c.Param("project_id"))}
}

// Capture belongs after session validation on authenticated project routes.
func (h *Handler) Capture(c *gin.Context) {
	k := requestScope(c)
	at := time.Now().UTC()
	c.Next()
	if k.Actor > 0 && k.Workspace > 0 && k.Project > 0 {
		h.Observer.Offer(domain.Observation{Scope: k, Write: c.Request.Method != "GET" && c.Request.Method != "HEAD", Success: c.Writer.Status() >= 200 && c.Writer.Status() < 300, At: at, Duration: time.Since(at)})
	}
}
func fail(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Audit service unavailable."}
	}
	fields := []gin.H{}
	if f.Field != "" {
		fields = append(fields, gin.H{"path": f.Field, "reason": f.Reason})
	}
	category, retry := "validation", "never"
	switch f.Status {
	case 401, 403, 404:
		category = "access"
	case 503:
		category, retry = "dependency", "retry_later"
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "message": f.Message, "reason": f.Reason, "data": nil, "metadata": gin.H{"request_id": c.Writer.Header().Get("X-Request-ID"), "category": category, "retry": retry, "fields": fields, "operation_id": nil, "status_url": nil}})
}

type endpoint func(*gin.Context, domain.Scope, int64) (any, error)

func serve(status int, f endpoint) gin.HandlerFunc {
	return func(c *gin.Context) {
		k := requestScope(c)
		if k.Actor <= 0 {
			fail(c, fault.Unauthenticated)
			return
		}
		id := identifier(c.Param("id"))
		if k.Workspace <= 0 || k.Project <= 0 || (c.Param("id") != "" && id <= 0) {
			fail(c, fault.NotFound)
			return
		}
		out, err := f(c, k, id)
		if err != nil {
			fail(c, err)
			return
		}
		if status == 0 {
			c.Header("Content-Disposition", `attachment; filename="audit-event.json"`)
			c.Header("X-Content-Type-Options", "nosniff")
			c.JSON(200, out)
			return
		}
		message := "success"
		if status == http.StatusAccepted {
			message = "accepted"
		}
		c.JSON(status, gin.H{"code": 0, "message": message, "data": out})
	}
}
func Register(v2 *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	g := v2.Group("/workspaces/:workspace_id/projects/:project_id/audit", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	g.GET("/events", serve(200, h.list))
	g.GET("/events/:id", serve(200, func(c *gin.Context, k domain.Scope, id int64) (any, error) {
		return h.Store.Detail(c.Request.Context(), k, id)
	}))
	g.POST("/events/:id/exports", serve(202, func(c *gin.Context, k domain.Scope, id int64) (any, error) {
		keys := c.Request.Header.Values("Idempotency-Key")
		if len(keys) != 1 || strings.TrimSpace(keys[0]) == "" {
			return nil, fault.Invalid("/Idempotency-Key")
		}
		return h.Store.StartExport(c.Request.Context(), k, id, keys[0])
	}))
	g.GET("/events/:id/exports/:export_id/download", serve(0, func(c *gin.Context, k domain.Scope, id int64) (any, error) {
		return h.Store.Download(c.Request.Context(), k, id, c.Param("export_id"))
	}))
	g.GET("/retention", serve(200, func(c *gin.Context, k domain.Scope, _ int64) (any, error) {
		return h.Store.Policy(c.Request.Context(), k)
	}))
	g.PUT("/retention", serve(200, func(c *gin.Context, k domain.Scope, _ int64) (any, error) {
		if _, err := h.Store.Authorize(c.Request.Context(), k, domain.Manage); err != nil {
			return nil, err
		}
		var p domain.Retention
		p.ContentHours = -1
		d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 2048))
		d.DisallowUnknownFields()
		if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
			return nil, fault.Invalid("/retention")
		}
		return p, h.Store.SetPolicy(c.Request.Context(), k, p)
	}))
}
