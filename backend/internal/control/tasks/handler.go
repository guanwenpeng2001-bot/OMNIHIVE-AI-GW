package tasks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ip"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

type Handler struct {
	Store          *domain.Store
	Access         identity.AccessEvaluator
	key            []byte
	trustForwarded bool
}

func New(s *domain.Store, a identity.AccessEvaluator, cfg *config.Config) *Handler {
	return &Handler{s, a, []byte(cfg.JWT.Secret), cfg.TrustForwardedIPForAPIKeyACL()}
}

type request struct {
	*gin.Context
	a              domain.Scope
	ctx            context.Context
	task, artifact string
}

func (h *Handler) route(action string, status int, fn func(request) (any, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			fail(c, fault.Unauthenticated)
			return
		}
		w, e := identifier(c.Param("workspace_id"))
		p, pe := identifier(c.Param("project_id"))
		if e != nil || pe != nil {
			fail(c, fault.NotFound)
			return
		}
		ctx := c.Request.Context()
		a, e := h.Access.Evaluate(ctx, subject.UserID, w, p, action)
		if e != nil {
			fail(c, e)
			return
		}
		r := request{c, a, ctx, c.Param("id"), c.Param("artifact_id")}
		data, e := fn(r)
		if e != nil {
			fail(c, e)
			return
		}
		if status > 0 {
			message := "success"
			if status == 202 {
				message = "accepted"
			}
			c.JSON(status, gin.H{"code": 0, "message": message, "data": data})
		}
	}
}
func Register(parent *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	headers := func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		if c.Writer.Header().Get("X-Request-ID") == "" {
			c.Header("X-Request-ID", uuid.NewString())
		}
		c.Next()
	}
	g := parent.Group("/workspaces/:workspace_id/projects/:project_id/tasks", append([]gin.HandlerFunc{headers}, session...)...)
	g.GET("", h.route("tasks.read", 200, h.list))
	g.POST("", h.route("tasks.create", 202, h.create))
	g.GET("/:id", h.route("tasks.read", 200, func(r request) (any, error) { return h.Store.Detail(r.ctx, r.a, r.task) }))
	g.GET("/:id/items", h.route("tasks.read", 200, h.items))
	g.POST("/:id/retry", h.route("tasks.retry", 202, h.retry))
	g.POST("/:id/cancel", h.route("tasks.cancel", 202, h.cancel))
	g.POST("/:id/artifacts/:artifact_id/download", h.route("tasks.artifacts.read", 200, h.download))
	g.GET("/:id/artifacts/:artifact_id/content", h.route("tasks.artifacts.read", 0, h.content))
	g.DELETE("/:id/artifacts/:artifact_id", h.route("tasks.artifacts.revoke", 200, func(r request) (any, error) {
		return gin.H{"state": "revoked"}, h.Store.Revoke(r.ctx, r.a, r.task, r.artifact)
	}))
}
func identifier(value string) (int64, error) {
	n, e := strconv.ParseInt(value, 10, 64)
	if e != nil || n < 1 || strconv.FormatInt(n, 10) != value {
		return 0, fault.NotFound
	}
	return n, nil
}
func fail(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		f = domain.Unavailable
	}
	category, retry := "validation", "never"
	switch f.Status {
	case 401, 403, 404:
		category = "access"
	case 402:
		category = "funding"
	case 409:
		category = "conflict"
	case 503:
		category = "dependency"
		retry = "retry_later"
	}
	var statusURL *string
	if f.Reason == "SUBMISSION_UNKNOWN" && c.Param("id") != "" {
		v := "/api/v2/workspaces/" + url.PathEscape(c.Param("workspace_id")) + "/projects/" + url.PathEscape(c.Param("project_id")) + "/tasks/" + url.PathEscape(c.Param("id"))
		statusURL = &v
		retry = "query_status"
	}
	fields := []gin.H{}
	if f.Field != "" {
		fields = append(fields, gin.H{"path": f.Field, "reason": f.Reason})
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "message": f.Message, "reason": f.Reason, "metadata": gin.H{"request_id": c.Writer.Header().Get("X-Request-ID"), "category": category, "retry": retry, "fields": fields, "operation_id": nil, "status_url": statusURL}, "data": nil})
}
func body(r request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(r.Writer, r.Request.Body, 1<<20))
	dec.DisallowUnknownFields()
	var raw json.RawMessage
	if e := dec.Decode(&raw); e != nil || len(raw) == 0 || raw[0] != '{' || dec.Decode(new(any)) != io.EOF {
		return &fault.Fault{Status: 400, Reason: "INVALID_JSON", Message: "Invalid JSON request body."}
	}
	var check = json.NewDecoder(bytes.NewReader(raw))
	check.DisallowUnknownFields()
	if err := check.Decode(dst); err != nil {
		return &fault.Fault{Status: 400, Reason: "INVALID_JSON", Message: "Invalid JSON request body."}
	}
	return nil
}
func (h *Handler) create(r request) (any, error) {
	var v domain.Create
	if e := body(r, &v); e != nil {
		return nil, e
	}
	return h.Store.Create(r.ctx, r.a, v, r.GetHeader("Idempotency-Key"), ip.GetSecurityClientIP(r.Context, h.trustForwarded))
}
func (h *Handler) retry(r request) (any, error) {
	var v domain.Retry
	if e := body(r, &v); e != nil {
		return nil, e
	}
	return h.Store.Retry(r.ctx, r.a, r.task, r.GetHeader("Idempotency-Key"), ip.GetSecurityClientIP(r.Context, h.trustForwarded), v)
}
func (h *Handler) cancel(r request) (any, error) {
	if e := body(r, &struct{}{}); e != nil {
		return nil, e
	}
	return h.Store.Cancel(r.ctx, r.a, r.task, r.GetHeader("Idempotency-Key"))
}

type token struct {
	Actor, Workspace, Project, Version int64
	Resource, Filters, After           string
	AsOf, AfterTime, Expires           time.Time
}

func binding(r request, res string) token {
	return token{Actor: r.a.ActorID, Workspace: r.a.WorkspaceID, Project: r.a.ProjectID, Version: r.a.Version, Resource: res}
}
func (h *Handler) mac(b []byte) []byte {
	m := hmac.New(sha256.New, h.key)
	m.Write([]byte("image-task-center/v1\x00"))
	m.Write(b)
	return m.Sum(nil)
}
func (h *Handler) sign(v token) (string, error) {
	if len(h.key) == 0 {
		return "", domain.Unavailable
	}
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(h.mac(b)), nil
}
func (h *Handler) verify(raw string, want token) (token, bool) {
	if len(h.key) == 0 || len(raw) > 8192 {
		return token{}, false
	}
	a, b, ok := strings.Cut(raw, ".")
	if !ok {
		return token{}, false
	}
	msg, e := base64.RawURLEncoding.DecodeString(a)
	sig, f := base64.RawURLEncoding.DecodeString(b)
	var v token
	if e != nil || f != nil || !hmac.Equal(h.mac(msg), sig) || json.Unmarshal(msg, &v) != nil {
		return v, false
	}
	return v, v.Actor == want.Actor && v.Workspace == want.Workspace && v.Project == want.Project && v.Version == want.Version && v.Resource == want.Resource && v.Filters == want.Filters
}
func (h *Handler) download(r request) (any, error) {
	if e := body(r, &struct{}{}); e != nil {
		return nil, e
	}
	a, e := h.Store.Artifact(r.ctx, r.a, r.task, r.artifact)
	if e != nil {
		return nil, e
	}
	u := domain.TaskURL(r.a, r.task) + "/artifacts/" + url.PathEscape(r.artifact) + "/content"
	v := binding(r, u)
	v.Expires = time.Now().UTC().Add(time.Minute)
	if a.Expires != nil && a.Expires.Before(v.Expires) {
		v.Expires = a.Expires.UTC()
	}
	tk, e := h.sign(v)
	if e != nil {
		return nil, e
	}
	return struct {
		URL         string    `json:"url"`
		Ticket      string    `json:"ticket"`
		Expires     time.Time `json:"expires_at"`
		ContentType string    `json:"content_type"`
		Size        *int64    `json:"size_bytes,string"`
	}{u, tk, v.Expires, a.ContentType, a.Size}, nil
}
func (h *Handler) content(r request) (any, error) {
	v, e := h.Store.Artifact(r.ctx, r.a, r.task, r.artifact)
	if e != nil {
		return nil, e
	}
	u := domain.TaskURL(r.a, r.task) + "/artifacts/" + url.PathEscape(r.artifact) + "/content"
	tk, ok := h.verify(r.GetHeader("X-Artifact-Ticket"), binding(r, u))
	if !ok {
		return nil, fault.NotFound
	}
	if !time.Now().Before(tk.Expires) {
		return nil, domain.Gone
	}
	if h.Store.Objects == nil {
		return nil, domain.Unavailable
	}
	stream, e := h.Store.Objects.Open(r.ctx, v)
	if e != nil {
		return nil, e
	}
	defer stream.Close()
	// A slow provider/object fetch can cross ticket expiry or a grant change.
	current, err := h.Access.Evaluate(r.ctx, r.a.ActorID, r.a.WorkspaceID, r.a.ProjectID, "tasks.artifacts.read")
	if err != nil {
		return nil, err
	}
	if current.Version != tk.Version {
		return nil, fault.NotFound
	}
	if !time.Now().Before(tk.Expires) {
		return nil, domain.Gone
	}
	if _, err = h.Store.Artifact(r.ctx, current, r.task, r.artifact); err != nil {
		return nil, err
	}
	mime := v.ContentType
	if !slices.Contains([]string{"image/png", "image/jpeg", "image/gif", "image/webp"}, mime) {
		mime = "application/octet-stream"
	}
	r.Header("X-Content-Type-Options", "nosniff")
	r.Header("Content-Disposition", `attachment; filename="image"`)
	r.Header("Content-Type", mime)
	if v.Size != nil {
		r.Header("Content-Length", strconv.FormatInt(*v.Size, 10))
	}
	r.Status(200)
	if _, err := io.Copy(r.Writer, stream); err != nil {
		_ = r.Error(err)
	}
	return nil, nil
}
