package byok

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/byok"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type Handler struct{ store *domain.Store }

func New(store *domain.Store) *Handler { return &Handler{store} }
func provideStore(db *sql.DB, enc service.SecretEncryptor, probe *service.AccountTestService, a identity.AccessEvaluator, cfg *config.Config) *domain.Store {
	return domain.New(db, enc, probe, a, []byte(cfg.JWT.Secret))
}

var ProviderSet = wire.NewSet(provideStore, New)

func Register(parent *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	w := parent.Group("/workspaces/:workspace_id", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	p := w.Group("/projects/:project_id/byok/credentials")
	for _, r := range []*gin.RouterGroup{w.Group("/byok"), p} {
		r.GET("", h.serve("list", 200))
		r.POST("", h.serve("create", 201))
		r.PUT("/:id/policy", h.serve("policy", 200))
		for _, action := range []string{"verify", "rotate", "revoke"} {
			r.POST("/:id/"+action, h.serve(action, 202))
		}
	}
	p.GET("/:id/estimate", h.serve("estimate", 200))
}
func target(c *gin.Context) (domain.Target, error) {
	who, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || who.UserID < 1 {
		return domain.Target{}, fault.Unauthenticated
	}
	t := domain.Target{Actor: who.UserID}
	for k, v := range map[string]*int64{"workspace_id": &t.Workspace, "project_id": &t.Project, "id": &t.ID} {
		if text := c.Param(k); text != "" {
			id, e := connections.ID(text)
			if e != nil {
				return t, fault.NotFound
			}
			*v = id
		}
	}
	return t, nil
}
func decode(c *gin.Context, out any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	// Require exactly one object. In particular a null verify command is invalid.
	var doc json.RawMessage
	if e := decoder.Decode(&doc); e != nil || len(doc) == 0 || doc[0] != '{' || decoder.Decode(new(any)) != io.EOF {
		return fault.Invalid("/body")
	}
	return strict(doc, out)
}
func strict(doc json.RawMessage, out any) error {
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(doc, &fields); e != nil {
		return fault.Invalid("/body")
	}
	for _, v := range fields {
		if string(v) == "null" {
			return fault.Invalid("/body")
		}
	}
	d := json.NewDecoder(bytes.NewReader(doc))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return fault.Invalid("/body")
	}
	return nil
}
func (h *Handler) serve(action string, status int) gin.HandlerFunc {
	return func(c *gin.Context) {
		t, e := target(c)
		var out any
		if e == nil {
			out, e = h.run(c, t, action)
		}
		if e != nil {
			fail(c, e)
			return
		}
		message := "success"
		if status == 202 {
			message = "accepted"
		}
		c.JSON(status, gin.H{"code": 0, "message": message, "data": out})
	}
}
func (h *Handler) run(c *gin.Context, t domain.Target, action string) (any, error) {
	ctx := c.Request.Context()
	switch action {
	case "list":
		after := int64(0)
		var e error
		if text := c.Query("cursor"); text != "" {
			after, e = connections.ID(text)
			if e != nil {
				return nil, fault.Invalid("/cursor")
			}
		}
		limit, e := strconv.Atoi(c.DefaultQuery("limit", "25"))
		if e != nil || limit < 1 || limit > 100 {
			return nil, fault.Invalid("/limit")
		}
		return h.store.List(ctx, t, after, limit)
	case "estimate":
		member, e := connections.ID(c.Query("member_id"))
		if e != nil {
			return nil, fault.Invalid("/member_id")
		}
		key, e := connections.ID(c.Query("key_id"))
		if e != nil {
			return nil, fault.Invalid("/key_id")
		}
		in, e := strconv.ParseInt(c.Query("input_token_bound"), 10, 64)
		if e != nil || in < 0 {
			return nil, fault.Invalid("/input_token_bound")
		}
		out, e := strconv.ParseInt(c.Query("output_token_bound"), 10, 64)
		if e != nil || out < 0 {
			return nil, fault.Invalid("/output_token_bound")
		}
		return h.store.Estimate(ctx, t, member, key, c.Query("model_id"), domain.Bounds{Input: in, Output: out})
	}
	if t.ID != 0 {
		value := c.GetHeader("If-Match")
		if len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' {
			return nil, fault.Invalid("/If-Match")
		}
		version, e := connections.ID(value[1 : len(value)-1])
		if e != nil {
			return nil, fault.Invalid("/If-Match")
		}
		t.Version = version
	}
	cmd := domain.Command{Target: t, Action: action, Key: c.GetHeader("Idempotency-Key")}
	var e error
	switch action {
	case "create":
		e = decode(c, &cmd.Create)
	case "policy":
		e = decode(c, &cmd.Policy)
	case "verify":
		e = decode(c, &struct{}{})
	case "rotate":
		var in struct {
			Replacement struct {
				Secret string `json:"secret"`
			} `json:"replacement"`
		}
		e = decode(c, &in)
		cmd.Secret = in.Replacement.Secret
	case "revoke":
		var in struct {
			Mode string `json:"mode"`
		}
		e = decode(c, &in)
		cmd.Mode = in.Mode
	}
	if e != nil {
		return nil, e
	}
	return h.store.Change(ctx, cmd)
}
func fail(c *gin.Context, e error) {
	var f *fault.Fault
	if !errors.As(e, &f) {
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "BYOK service unavailable."}
	}
	category, retry := "validation", "never"
	switch f.Status {
	case 401, 403, 404:
		category = "access"
	case 409, 412:
		category = "conflict"
	case 503:
		category, retry = "dependency", "retry_later"
	}
	m := gin.H{"category": category, "retry": retry, "request_id": c.Writer.Header().Get("X-Request-ID"), "fields": []any{}, "operation_id": nil, "status_url": nil}
	if f.Field != "" {
		m["fields"] = []gin.H{{"path": f.Field, "reason": f.Reason}}
	}
	if f.CurrentVersion > 0 {
		m["current_version"] = strconv.FormatInt(f.CurrentVersion, 10)
		c.Header("ETag", strconv.Quote(strconv.FormatInt(f.CurrentVersion, 10)))
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "message": f.Message, "reason": f.Reason, "data": nil, "metadata": m})
}
