package policies

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type Handler struct {
	store  *policy.Store
	access identity.AccessEvaluator
}

func New(store *policy.Store, access identity.AccessEvaluator) *Handler {
	return &Handler{store, access}
}
func NewStore(db *sql.DB, cfg *config.Config) *policy.Store {
	store := policy.NewStore(db, []byte(cfg.JWT.Secret))
	service.BindNativeRoutingPolicies(cfg, store)
	return store
}

var ProviderSet = wire.NewSet(NewStore, New)

type request struct {
	*gin.Context
	ctx              context.Context
	actor, workspace int64
	id               string
}
type endpoint func(request) (any, error)

func route(status int, fn endpoint) gin.HandlerFunc {
	return func(c *gin.Context) {
		who, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || who.UserID <= 0 {
			fail(c, fault.Unauthenticated)
			return
		}
		w := policy.ID(c.Param("workspace_id"))
		if w == 0 {
			fail(c, fault.NotFound)
			return
		}
		out, err := fn(request{c, c.Request.Context(), who.UserID, w, c.Param("id")})
		if err != nil {
			fail(c, err)
			return
		}
		if h, ok := out.(policy.Head); ok {
			c.Header("ETag", strconv.Quote(strconv.FormatInt(h.ETag, 10)))
		}
		c.JSON(status, gin.H{"code": 0, "message": "success", "data": out})
	}
}

func Register(parent *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	g := parent.Group("/workspaces/:workspace_id/routing/policies", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	g.GET("", route(200, h.list))
	g.POST("", route(201, h.create))
	g.GET("/:id", route(200, h.get))
	g.PUT("/:id/draft", route(200, h.draft))
	g.GET("/:id/versions", route(200, h.versions))
	g.POST("/:id/simulate", route(200, h.simulate))
	g.POST("/:id/publish", route(200, h.publish))
	g.POST("/:id/rollback", route(200, h.rollback))
}
func fail(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Routing policy service unavailable."}
	}
	category, retry := "validation", "never"
	switch f.Status {
	case 401, 403, 404:
		category = "access"
	case 409, 410, 412:
		category = "conflict"
	case 503:
		category, retry = "dependency", "retry_later"
	}
	meta := gin.H{"request_id": c.Writer.Header().Get("X-Request-ID"), "category": category, "retry": retry, "fields": []any{}, "operation_id": nil, "status_url": nil}
	if f.CurrentVersion > 0 {
		meta["current_version"] = strconv.FormatInt(f.CurrentVersion, 10)
	}
	if f.Field != "" {
		meta["fields"] = []gin.H{{"path": f.Field, "reason": f.Reason}}
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "reason": f.Reason, "message": f.Message, "metadata": meta, "data": nil})
}
func body(r request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(r.Writer, r.Request.Body, 256<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil || d.Decode(new(any)) != io.EOF {
		return &fault.Fault{Status: 400, Reason: "INVALID_JSON", Message: "Invalid request body."}
	}
	return nil
}
func version(r request) (int64, error) {
	raw := r.GetHeader("If-Match")
	if len(raw) > 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		if n := policy.ID(raw[1 : len(raw)-1]); n > 0 {
			return n, nil
		}
	}
	return 0, fault.Invalid("/If-Match")
}
func (h *Handler) authorize(r request, sc policy.Scope, action string) (policy.Caller, error) {
	var c policy.Caller
	if !sc.Valid(r.workspace) || sc.Type == "platform" || sc.Type == "request" {
		return c, fault.Invalid("/scope")
	}
	a, err := h.access.Evaluate(r.ctx, r.actor, r.workspace, sc.ProjectID(), action)
	if err != nil {
		return c, err
	}
	if sc.Type == "api_key" {
		owner, err := h.store.KeyOwner(r.ctx, r.workspace, sc.ProjectID(), policy.ID(sc.ID))
		if err != nil {
			return c, err
		}
		if action == "routing.simulate" && owner != r.actor {
			if err = a.Require("keys.act_as"); err != nil {
				return c, err
			}
		}
	}
	return policy.Caller{Workspace: a.WorkspaceID, Actor: a.ActorID, AccessVersion: a.Version}, nil
}
func (h *Handler) object(r request, action string) (policy.Caller, error) {
	sc, err := h.store.Scope(r.ctx, r.workspace, r.id)
	if err != nil {
		return policy.Caller{}, err
	}
	return h.authorize(r, sc, action)
}

func (h *Handler) get(r request) (any, error) {
	c, err := h.object(r, "routing.read")
	if err != nil {
		return nil, err
	}
	return h.store.Get(r.ctx, c.Workspace, r.id)
}
func (h *Handler) create(r request) (any, error) {
	var d policy.Definition
	if err := body(r, &d); err != nil {
		return nil, err
	}
	c, err := h.authorize(r, d.Scope, "routing.edit")
	if err != nil {
		return nil, err
	}
	return h.store.Create(r.ctx, c, r.GetHeader("Idempotency-Key"), d)
}
func (h *Handler) draft(r request) (any, error) {
	c, err := h.object(r, "routing.edit")
	if err != nil {
		return nil, err
	}
	v, err := version(r)
	if err != nil {
		return nil, err
	}
	var d policy.Definition
	if err = body(r, &d); err != nil {
		return nil, err
	}
	return h.store.Draft(r.ctx, c, r.id, v, d)
}
func (h *Handler) simulate(r request) (any, error) {
	object, err := h.object(r, "routing.simulate")
	if err != nil {
		return nil, err
	}
	var in policy.SimulationInput
	if err = body(r, &in); err != nil {
		return nil, err
	}
	sc := policy.Scope{Type: "project", ID: in.Project}
	if in.Key != "" {
		sc = policy.Scope{Type: "api_key", ID: in.Key, Project: in.Project}
	}
	c, err := h.authorize(r, sc, "routing.simulate")
	if err != nil {
		return nil, err
	}
	if object.AccessVersion != c.AccessVersion {
		return nil, fault.Forbidden
	}
	return h.store.Simulate(r.ctx, c, r.id, in)
}
func (h *Handler) publish(r request) (any, error) {
	c, err := h.object(r, "routing.publish")
	if err != nil {
		return nil, err
	}
	v, err := version(r)
	if err != nil {
		return nil, err
	}
	var in policy.Publish
	if err = body(r, &in); err != nil {
		return nil, err
	}
	return h.store.Publish(r.ctx, c, r.id, r.GetHeader("Idempotency-Key"), v, in)
}
func (h *Handler) rollback(r request) (any, error) {
	c, err := h.object(r, "routing.publish")
	if err != nil {
		return nil, err
	}
	v, err := version(r)
	if err != nil {
		return nil, err
	}
	var in policy.Rollback
	if err = body(r, &in); err != nil {
		return nil, err
	}
	return h.store.Rollback(r.ctx, c, r.id, r.GetHeader("Idempotency-Key"), v, in)
}
func pageQuery(r request, list bool) (url.Values, int, error) {
	q, err := url.ParseQuery(r.Request.URL.RawQuery)
	if err != nil {
		return nil, 0, fault.Invalid("/query")
	}
	limit := 25
	for key, vs := range q {
		if len(vs) != 1 {
			return nil, 0, fault.Invalid("/" + key)
		}
		switch key {
		case "limit":
			limit, err = strconv.Atoi(vs[0])
			if err != nil || limit < 1 || limit > 100 {
				return nil, 0, fault.Invalid("/limit")
			}
		case "cursor":
		case "scope_type", "scope_id", "project_id":
			if !list {
				return nil, 0, fault.Invalid("/" + key)
			}
		default:
			return nil, 0, fault.Invalid("/" + key)
		}
	}
	return q, limit, nil
}
func (h *Handler) list(r request) (any, error) {
	q, lim, err := pageQuery(r, true)
	if err != nil {
		return nil, err
	}
	sc := policy.Scope{Type: q.Get("scope_type"), ID: q.Get("scope_id"), Project: q.Get("project_id")}
	if sc.Type == "" && sc.ID == "" && sc.Project == "" {
		sc = policy.Scope{Type: "workspace", ID: strconv.FormatInt(r.workspace, 10)}
	}
	c, err := h.authorize(r, sc, "routing.read")
	if err != nil {
		return nil, err
	}
	return h.store.List(r.ctx, c, sc, q.Get("cursor"), lim)
}
func (h *Handler) versions(r request) (any, error) {
	c, err := h.object(r, "routing.read")
	if err != nil {
		return nil, err
	}
	q, lim, err := pageQuery(r, false)
	if err != nil {
		return nil, err
	}
	return h.store.Versions(r.ctx, c, r.id, q.Get("cursor"), lim)
}
