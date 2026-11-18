package identity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

type Backend interface {
	domain.AccessEvaluator
	Workspaces(context.Context, int64, domain.Page) ([]domain.Workspace, error)
	Projects(context.Context, int64, int64, domain.Page) ([]domain.Project, error)
	Project(context.Context, int64, int64, int64) (domain.Project, error)
	Keys(context.Context, int64, int64, int64, domain.Page) ([]domain.Key, error)
	CreateProject(context.Context, int64, int64, string, string) (domain.Project, error)
	ChangeProject(context.Context, int64, int64, int64, int64, string, bool) (domain.Project, error)
	CreateKey(context.Context, int64, int64, int64, domain.CreateKey, string) (domain.Key, error)
	SetGrants(context.Context, int64, int64, int64, int64, []int64, []string) (domain.GrantVersion, error)
}

type Handler struct {
	store      Backend
	operations domain.Operations
	cursorKey  []byte
}

func New(store Backend, ops domain.Operations, cfg *config.Config) *Handler {
	return &Handler{store: store, operations: ops, cursorKey: []byte(cfg.JWT.Secret)}
}

type request struct {
	*gin.Context
	actor, w, p int64
}

type endpoint func(request) (any, error)

func TenantHeaders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if c.Writer.Header().Get("X-Request-ID") == "" {
		c.Header("X-Request-ID", uuid.NewString())
	}

	c.Next()
}

func route(status int, fn endpoint) gin.HandlerFunc {
	return func(c *gin.Context) {
		who, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || who.UserID <= 0 {
			fail(c, fault.Unauthenticated)
			return
		}
		var err error
		r := request{Context: c, actor: who.UserID}
		if v := c.Param("workspace_id"); v != "" {
			r.w, err = identifier(v)
		}
		if err == nil {
			if v := c.Param("project_id"); v != "" {
				r.p, err = identifier(v)
			}
		}
		if err != nil {
			fail(c, err)
			return
		}
		data, err := fn(r)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(status, gin.H{"code": 0, "message": "success", "data": data})
	}
}

func identifier(s string) (int64, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 || strconv.FormatInt(v, 10) != s {
		return 0, fault.NotFound
	}
	return v, nil
}

func fail(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Identity service unavailable."}
	}
	category := "validation"
	retry := "never"
	switch f.Status {
	case 401, 403, 404:
		category = "access"
	case 409, 412:
		category = "conflict"
	case 503:
		category = "dependency"
		retry = "retry_later"
	}
	meta := gin.H{"request_id": c.Writer.Header().Get("X-Request-ID"), "category": category, "retry": retry, "fields": []any{}, "operation_id": nil, "status_url": nil}
	if f.Field != "" {
		meta["fields"] = []gin.H{{"path": f.Field, "reason": f.Reason}}
	}
	if f.CurrentVersion > 0 {
		meta["current_version"] = strconv.FormatInt(f.CurrentVersion, 10)
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "reason": f.Reason, "message": f.Message, "metadata": meta, "data": nil})
}

func (r request) body(v any) error {
	d := json.NewDecoder(http.MaxBytesReader(r.Writer, r.Request.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil || d.Decode(new(any)) != io.EOF {
		return &fault.Fault{Status: 400, Reason: "INVALID_JSON", Message: "Invalid request body."}
	}
	return nil
}

func (r request) version() (int64, error) {
	raw := r.GetHeader("If-Match")
	if len(raw) < 3 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return 0, fault.Invalid("/If-Match")
	}
	v, err := identifier(raw[1 : len(raw)-1])
	if err != nil {
		return 0, fault.Invalid("/If-Match")
	}
	return v, nil
}

func Register(parent *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	chain := append([]gin.HandlerFunc{TenantHeaders}, session...)
	v2 := parent.Group("", chain...)
	v2.GET("/me/scopes", route(200, h.workspaces))
	w := v2.Group("/workspaces/:workspace_id")
	w.GET("/projects", route(200, h.projects))
	w.POST("/projects", route(201, h.createProject))
	p := w.Group("/projects/:project_id")
	p.GET("", route(200, func(r request) (any, error) { return h.store.Project(r.Request.Context(), r.actor, r.w, r.p) }))
	p.PATCH("", route(200, h.changeProject))
	p.DELETE("", route(200, h.changeProject))
	p.GET("/access", route(200, func(r request) (any, error) { return h.store.Evaluate(r.Request.Context(), r.actor, r.w, r.p, "") }))
	p.GET("/keys", route(200, h.keys))
	p.POST("/keys", route(201, h.createKey))
	w.PUT("/members/:id/grants", route(200, h.setGrants))
	w.GET("/operations/:id", route(200, h.operation))
}

func (h *Handler) createProject(r request) (any, error) {
	var b struct {
		Name string `json:"name"`
	}
	if err := r.body(&b); err != nil {
		return nil, err
	}
	return h.store.CreateProject(r.Request.Context(), r.actor, r.w, b.Name, r.GetHeader("Idempotency-Key"))
}

func (h *Handler) changeProject(r request) (any, error) {
	v, err := r.version()
	if err != nil {
		return nil, err
	}
	archive := r.Request.Method == http.MethodDelete
	var b struct {
		Name string `json:"name"`
	}
	if !archive {
		if err := r.body(&b); err != nil {
			return nil, err
		}
	}
	return h.store.ChangeProject(r.Request.Context(), r.actor, r.w, r.p, v, b.Name, archive)
}

func (h *Handler) createKey(r request) (any, error) {
	var b domain.CreateKey
	if err := r.body(&b); err != nil {
		return nil, err
	}
	return h.store.CreateKey(r.Request.Context(), r.actor, r.w, r.p, b, r.GetHeader("Idempotency-Key"))
}

func (h *Handler) setGrants(r request) (any, error) {
	member, err := identifier(r.Param("id"))
	if err != nil {
		return nil, err
	}
	v, err := r.version()
	if err != nil {
		return nil, err
	}
	var b struct {
		ProjectIDs []string `json:"project_ids"`
		Actions    []string `json:"actions"`
	}
	if err := r.body(&b); err != nil {
		return nil, err
	}
	if b.ProjectIDs == nil {
		return nil, fault.Invalid("/project_ids")
	}
	if b.Actions == nil {
		return nil, fault.Invalid("/actions")
	}
	ids := make([]int64, len(b.ProjectIDs))
	for i, s := range b.ProjectIDs {
		ids[i], err = identifier(s)
		if err != nil {
			return nil, fault.Invalid("/project_ids")
		}
	}
	return h.store.SetGrants(r.Request.Context(), r.actor, r.w, member, v, ids, b.Actions)
}

func (h *Handler) operation(r request) (any, error) {
	ctx := r.Request.Context()
	if _, err := h.store.Evaluate(ctx, r.actor, r.w, 0, ""); err != nil {
		return nil, err
	}
	id := r.Param("id")
	if strings.ContainsAny(id, "/\\") || id == "." || id == ".." {
		return nil, fault.NotFound
	}
	op, err := h.operations.Lookup(ctx, r.actor, r.w, id)
	if err != nil {
		return nil, err
	}
	if op.ID != id {
		return nil, fault.NotFound
	}
	a, err := h.store.Evaluate(ctx, r.actor, r.w, op.ProjectID, "")
	if err != nil {
		return nil, err
	}
	if op.OwnerID != r.actor && (op.DelegatedAction == "" || a.Require(op.DelegatedAction) != nil) {
		return nil, fault.NotFound
	}
	op.StatusURL = "/api/v2/workspaces/" + strconv.FormatInt(r.w, 10) + "/operations/" + url.PathEscape(id)
	return op, nil
}
