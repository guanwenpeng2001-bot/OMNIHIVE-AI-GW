package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	rm "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/billing/readmodel"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

type StepUp interface {
	HasStepUpGrant(context.Context, int64, string) (bool, error)
}
type Handler struct {
	store  rm.Store
	access identity.AccessEvaluator
	step   StepUp
	key    [32]byte
}

func New(db *sql.DB, a identity.AccessEvaluator, step StepUp, secret string) *Handler {
	return &Handler{rm.Store{DB: db}, a, step, sha256.Sum256([]byte("billing/cursor/v1\x00" + secret))}
}
func Register(v2 *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	w := v2.Group("/workspaces/:workspace_id", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	p := w.Group("/projects/:project_id/billing")
	for _, kind := range []string{"activity", "charges", "ledger"} {
		grant := "billing.read"
		if kind == "ledger" {
			grant = "billing.ledger.read"
		}
		p.GET("/"+kind, h.serve(grant, 200, h.list(kind)))
	}
	w.GET("/billing/activity", h.serve("billing.read", 200, h.list("activity")))
	p.GET("/quotas", h.serve("billing.read", 200, h.quotas))
	p.PUT("/budgets/:id", h.serve("billing.budgets.write", 200, h.budget))
	p.POST("/adjustments", h.serve("billing.adjust", 201, h.adjust))
	p.POST("/exports", h.serve("billing.export", 202, h.export))
	p.GET("/exports/:id/download", h.serve("billing.export", 0, h.download))
	w.GET("/billing/reconciliation", h.serve("billing.reconcile.read", 200, h.list("reconciliation")))
	w.POST("/billing/reconciliation/:id/resolve", h.serve("billing.reconcile.write", 202, h.resolve))
}

type request struct {
	*gin.Context
	scope  rm.Scope
	access identity.Access
}
type endpoint func(request) (any, error)

func id(v string) int64 {
	n, e := strconv.ParseInt(v, 10, 64)
	if e != nil || n <= 0 || strconv.FormatInt(n, 10) != v {
		return 0
	}
	return n
}
func (h *Handler) authorize(ctx context.Context, s rm.Scope, action string) (identity.Access, error) {
	a, err := h.access.Evaluate(ctx, s.Actor, s.Workspace, s.Project, action)
	if err != nil {
		return a, err
	}
	if a.ActorID != s.Actor || a.ProjectID != s.Project || a.WorkspaceID != s.Workspace {
		return a, fault.NotFound
	}
	return a, a.Require(action)
}
func (h *Handler) serve(grant string, status int, next endpoint) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			fail(c, fault.Unauthenticated)
			return
		}
		s := rm.Scope{Workspace: id(c.Param("workspace_id")), Project: id(c.Param("project_id")), Actor: subject.UserID}
		if s.Workspace == 0 || (c.Param("project_id") != "" && s.Project == 0) {
			fail(c, fault.NotFound)
			return
		}
		// Reconciliation can be delegated to a project. Resolve derives that
		// project from the scoped case. The list accepts an explicit project_id;
		// a workspace-wide list requires the workspace-level evaluator.
		if grant == "billing.reconcile.write" {
			var err error
			s.Project, err = h.store.CaseProject(c.Request.Context(), s.Workspace, c.Param("id"))
			if err != nil {
				fail(c, err)
				return
			}
		} else if grant == "billing.reconcile.read" && c.Query("project_id") != "" {
			s.Project = id(c.Query("project_id"))
			if s.Project == 0 {
				fail(c, fault.Invalid("/project_id"))
				return
			}
		}
		a, err := h.authorize(c.Request.Context(), s, grant)
		if err == nil && grant == "billing.export" {
			err = a.Require("billing.read")
		}
		if err != nil {
			fail(c, err)
			return
		}
		data, err := next(request{c, s, a})
		if err != nil {
			if !c.Writer.Written() {
				fail(c, err)
			} else {
				slog.ErrorContext(c.Request.Context(), "billing export interrupted", "error", err)
			}
			return
		}
		if status != 0 {
			msg := "success"
			if status == 202 {
				msg = "accepted"
			}
			c.JSON(status, gin.H{"code": 0, "message": msg, "data": data})
		}
	}
}
func fail(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		slog.ErrorContext(c.Request.Context(), "billing workbench failed", "error", err)
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Billing data is unavailable."}
	}
	category, retry := "validation", "never"
	switch f.Status {
	case 401, 403, 404:
		category = "authorization"
	case 409, 412:
		category = "conflict"
	case 503:
		category, retry = "persistence", "after_delay"
	}
	fields := []gin.H{}
	if f.Field != "" {
		fields = append(fields, gin.H{"path": f.Field, "reason": f.Reason})
	}
	meta := gin.H{"request_id": c.Writer.Header().Get("X-Request-ID"), "fields": fields, "category": category, "retry": retry, "operation_id": nil, "status_url": nil}
	if f.Status == 412 {
		meta["current_version"] = fmt.Sprint(f.CurrentVersion)
	}
	c.Writer.Header().Del("Content-Disposition")
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "reason": f.Reason, "message": f.Message, "metadata": meta, "data": nil})
}
func body(r request, out any) error {
	dec := json.NewDecoder(http.MaxBytesReader(r.Writer, r.Request.Body, 16384))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil || dec.Decode(new(any)) != io.EOF {
		return &fault.Fault{Status: 400, Reason: "INVALID_JSON", Message: "Invalid request body."}
	}
	return nil
}
func key(r request) string {
	v := r.Request.Header.Values("Idempotency-Key")
	if len(v) != 1 {
		return ""
	}
	return v[0]
}
func version(r request) (int64, error) {
	v := r.Request.Header.Values("If-Match")
	if len(v) == 1 {
		s, e := strconv.Unquote(v[0])
		if e == nil {
			n, e := strconv.ParseInt(s, 10, 64)
			if e == nil && n >= 0 && strconv.FormatInt(n, 10) == s {
				return n, nil
			}
		}
	}
	return 0, fault.Invalid("/If-Match")
}
func query(r request, allowed []string) (url.Values, error) {
	q, err := url.ParseQuery(r.Request.URL.RawQuery)
	if err != nil {
		return nil, fault.Invalid("/query")
	}
	for k, v := range q {
		if !slices.Contains(allowed, k) || len(v) != 1 || v[0] == "" || len(v[0]) > 2048 {
			return nil, fault.Invalid("/" + k)
		}
	}
	return q, nil
}
func instant(q url.Values, k string) (time.Time, error) {
	if !q.Has(k) {
		return time.Time{}, nil
	}
	at, err := time.Parse(time.RFC3339Nano, q.Get(k))
	if err != nil || at.Year() < 1 {
		return at, fault.Invalid("/" + k)
	}
	return at.UTC(), nil
}

type token struct {
	Match [32]byte
	AsOf  time.Time
	After rm.Seek
}

func (h *Handler) sign(raw []byte) []byte {
	sig := hmac.New(sha256.New, h.key[:])
	_, _ = sig.Write(raw)
	return sig.Sum(nil)
}
func (h *Handler) cursor(raw string, v token) (token, error) {
	if raw == "" {
		return v, nil
	}
	b, t, ok := strings.Cut(raw, ".")
	data, e := base64.RawURLEncoding.DecodeString(b)
	tag, te := base64.RawURLEncoding.DecodeString(t)
	var out token
	if !ok || e != nil || te != nil || !hmac.Equal(tag, h.sign(data)) || json.Unmarshal(data, &out) != nil || out.Match != v.Match || out.After.ID == "" || out.AsOf.After(v.AsOf) {
		return v, fault.Invalid("/cursor")
	}
	if v.AsOf.Sub(out.AsOf) >= 15*time.Minute {
		return v, &fault.Fault{Status: 410, Reason: "SNAPSHOT_EXPIRED", Message: "The cursor has expired."}
	}
	return out, nil
}
func (h *Handler) list(kind string) endpoint {
	return func(r request) (any, error) {
		allowed := []string{"from", "to", "currency", "cursor", "limit"}
		if kind == "reconciliation" {
			allowed = append(allowed, "project_id", "state")
		} else {
			allowed = append(allowed, "timezone", "key_id", "member_id", "model_id", "funding_source", "settlement_state")
		}
		if kind == "activity" {
			allowed = append(allowed, "group_by")
		}
		if kind == "ledger" {
			allowed = append(allowed, "request_id", "settlement_id", "funding_account_id")
		}
		q, err := query(r, allowed)
		if err != nil {
			return nil, err
		}
		f := rm.Filter{Key: id(q.Get("key_id")), Member: id(q.Get("member_id")), Model: q.Get("model_id"), Group: q.Get("group_by"), State: q.Get("settlement_state"), Source: q.Get("funding_source"), Request: q.Get("request_id"), Settlement: q.Get("settlement_id"), Funding: q.Get("funding_account_id")}
		f.Timezone, f.Currency = q.Get("timezone"), q.Get("currency")
		if f.From, err = instant(q, "from"); err != nil {
			return nil, err
		}
		if f.To, err = instant(q, "to"); err != nil {
			return nil, err
		}
		if kind == "reconciliation" {
			f.State = q.Get("state")
			if f.State != "" && !slices.Contains([]string{"needs_review", "resolved"}, f.State) {
				return nil, fault.Invalid("/state")
			}
			if !f.From.IsZero() && !f.To.IsZero() && !f.From.Before(f.To) {
				return nil, fault.Invalid("/to")
			}
			if f.Currency != "" && !validCurrency(f.Currency) {
				return nil, fault.Invalid("/currency")
			}
		} else {
			if err = f.Validate(); err != nil {
				return nil, err
			}
			if f.Source != "" && !slices.Contains([]string{"wallet", "subscription", "byok", "mixed", "unknown"}, f.Source) {
				return nil, fault.Invalid("/funding_source")
			}
			if f.State != "" && !slices.Contains([]string{"estimated", "reserved", "pending", "settled", "needs_review", "adjusted", "refunded", "unknown"}, f.State) {
				return nil, fault.Invalid("/settlement_state")
			}
		}
		for _, k := range []string{"key_id", "member_id", "settlement_id", "funding_account_id"} {
			if q.Has(k) && id(q.Get(k)) == 0 {
				return nil, fault.Invalid("/" + k)
			}
		}
		if f.Group == "" {
			f.Group = "day"
		}
		size := 25
		if q.Has("limit") {
			n := id(q.Get("limit"))
			if n == 0 || n > 100 {
				return nil, fault.Invalid("/limit")
			}
			size = int(n)
		}
		fingerprint, _ := json.Marshal([]any{kind, r.scope, r.access.Version, f})
		cur, err := h.cursor(q.Get("cursor"), token{Match: sha256.Sum256(fingerprint), AsOf: time.Now().UTC().Truncate(time.Microsecond)})
		if err != nil {
			return nil, err
		}
		var output any
		var p *rm.Result
		if kind == "activity" {
			v, e := h.store.Activity(r.Request.Context(), r.scope, f, cur.After, size, cur.AsOf)
			if e != nil {
				return nil, e
			}
			p, output = &v.Result, &v
		} else {
			v, e := h.store.List(r.Request.Context(), r.scope, kind, f, cur.After, size, cur.AsOf)
			if e != nil {
				return nil, e
			}
			p, output = &v, &v
		}
		if p.Last != nil {
			cur.After = *p.Last
			raw, _ := json.Marshal(cur)
			next := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(h.sign(raw))
			p.Next = &next
		}
		return output, nil
	}
}
func validCurrency(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, v := range s {
		if v < 'A' || v > 'Z' {
			return false
		}
	}
	return true
}
func (h *Handler) quotas(r request) (any, error) {
	q, err := query(r, []string{"api_key_id", "member_id", "model_id", "at"})
	if err != nil {
		return nil, err
	}
	f := rm.QuotaFilter{Key: id(q.Get("api_key_id")), Member: id(q.Get("member_id")), Model: q.Get("model_id")}
	if f.At, err = instant(q, "at"); err != nil || f.At.After(time.Now()) {
		return nil, fault.Invalid("/at")
	}
	if q.Has("member_id") && f.Member == 0 || q.Has("api_key_id") && f.Key == 0 {
		return nil, fault.Invalid("/identity")
	}
	if f.Key != 0 {
		if err = r.access.Require("keys.read"); err != nil {
			return nil, err
		}
	}
	if f.Member > 0 && f.Member != r.scope.Actor {
		if err = r.access.Require("billing.payers.read"); err != nil {
			return nil, err
		}
		subject := r.scope
		subject.Actor = f.Member
		if _, err = h.authorize(r.Request.Context(), subject, "projects.read"); err != nil {
			return nil, fault.NotFound
		}
	}
	return h.store.Quotas(r.Request.Context(), r.scope, f, r.access.Require("billing.budgets.write") == nil)
}
func (h *Handler) budget(r request) (any, error) {
	var b rm.Budget
	if err := body(r, &b); err != nil {
		return nil, err
	}
	v, err := version(r)
	if err != nil {
		return nil, err
	}
	c, err := h.store.UpdateBudget(r.Request.Context(), r.scope, r.Param("id"), key(r), v, b)
	return c.Result, err
}
func (h *Handler) adjust(r request) (any, error) {
	if h.step == nil || r.GetString("auth_method") == "admin_api_key" {
		return nil, fault.Forbidden
	}
	ok, err := h.step.HasStepUpGrant(r.Request.Context(), r.scope.Actor, middleware.StepUpSessionKey(r.Context, r.scope.Actor))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &fault.Fault{Status: 403, Reason: "STEP_UP_REQUIRED", Message: "Verify this session to post an adjustment."}
	}
	var a rm.Adjustment
	if err = body(r, &a); err != nil {
		return nil, err
	}
	if id(a.Entry) == 0 {
		return nil, fault.Invalid("/ledger_entry_id")
	}
	c, err := h.store.Adjust(r.Request.Context(), r.scope, key(r), a)
	return c.Result, err
}
func (h *Handler) resolve(r request) (any, error) {
	var input rm.Resolution
	if err := body(r, &input); err != nil {
		return nil, err
	}
	v, err := version(r)
	if err != nil {
		return nil, err
	}
	input.Version = v
	c, err := h.store.Resolve(r.Request.Context(), r.scope, r.Param("id"), key(r), input)
	return operation(c), err
}
func (h *Handler) export(r request) (any, error) {
	var input struct {
		rm.Window
		Format string `json:"format"`
	}
	if err := body(r, &input); err != nil {
		return nil, err
	}
	if input.Format != "csv" {
		return nil, fault.Invalid("/format")
	}
	input.From = input.From.UTC()
	input.To = input.To.UTC()
	c, err := h.store.Export(r.Request.Context(), r.scope, key(r), input.Window)
	return operation(c), err
}
func operation(c rm.Command) identity.Operation {
	result := any(c.Result)
	if c.Route == "exports" && !time.Now().Before(c.Expires) {
		result = gin.H{"download_url": nil, "expires_at": c.Expires}
	}
	return identity.Operation{ID: c.ID, OwnerID: c.Actor, ProjectID: c.Project, State: "succeeded", StatusURL: fmt.Sprintf("/api/v2/workspaces/%d/operations/%s", c.Workspace, c.ID), ResourceID: &c.ID, Result: result}
}
func (h *Handler) ReadOperation(ctx context.Context, w, actor int64, id string) (identity.Operation, error) {
	if _, err := uuid.Parse(id); err != nil {
		return identity.Operation{}, fault.NotFound
	}
	c, err := h.store.Operation(ctx, w, actor, id)
	if err != nil {
		return identity.Operation{}, err
	}
	grant := "billing.reconcile.write"
	if c.Route == "exports" {
		grant = "billing.export"
	}
	a, err := h.authorize(ctx, c.Scope, grant)
	if err == nil && grant == "billing.export" {
		err = a.Require("billing.read")
	}
	return operation(c), err
}
func (h *Handler) download(r request) (any, error) {
	if _, err := uuid.Parse(r.Param("id")); err != nil {
		return nil, fault.NotFound
	}
	r.Header("Content-Disposition", `attachment; filename="billing-charges.csv"`)
	r.Header("Content-Type", "text/csv; charset=utf-8")
	r.Header("X-Content-Type-Options", "nosniff")
	return nil, h.store.Download(r.Request.Context(), r.scope, r.Param("id"), r.Writer)
}
