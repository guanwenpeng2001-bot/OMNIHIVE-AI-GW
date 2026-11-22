package models

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/quotes"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	identitypostgres "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type plazaSettings interface {
	GetModelPlazaRuntime(context.Context) service.ModelPlazaRuntime
}

type Handler struct {
	settings  plazaSettings
	reader    catalog.CatalogReader
	access    identity.AccessEvaluator
	cursorKey [32]byte
	quotes    quotes.Service
}

func New(reader catalog.CatalogReader, access identity.AccessEvaluator, cfg *config.Config) *Handler {
	return &Handler{reader: reader, access: access,
		cursorKey: sha256.Sum256([]byte("omnihive/catalog-cursor/v1\x00" + cfg.JWT.Secret)),
		quotes:    quotes.Service{Catalog: reader}}
}

func NewWithPricing(reader catalog.CatalogReader, access identity.AccessEvaluator, cfg *config.Config, db *sql.DB) *Handler {
	h := New(reader, access, cfg)
	if db != nil {
		h.settings = plazaDBSettings{db}
		published := pricing.VersionStore{DB: db}
		h.quotes = quotes.Service{
			Catalog:   reader,
			Published: published,
			Keys:      scopedKeyPriceResolver{db: db, access: access, store: published, reader: reader},
		}
	}
	return h
}

// NewWithPricingAndKeys connects quote policy to the same service as the gateway.
func NewWithPricingAndKeys(reader catalog.CatalogReader, access identity.AccessEvaluator, cfg *config.Config, db *sql.DB, keys *service.APIKeyService) *Handler {
	h := NewWithPricing(reader, access, cfg, db)
	if db != nil {
		h.quotes.Keys = scopedKeyPriceResolver{db: db, access: access, store: pricing.VersionStore{DB: db}, keys: keys, reader: reader}
	}
	return h
}

// The production constructor already receives the settings database. Preserve
// New's nil-settings behavior for embedded callers and existing tests.
type plazaDBSettings struct{ db *sql.DB }

func (s plazaDBSettings) GetModelPlazaRuntime(ctx context.Context) service.ModelPlazaRuntime {
	values := map[string]string{}
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM settings WHERE key IN ('model_plaza_enabled','model_plaza_require_auth')`)
	if err != nil {
		return service.ModelPlazaRuntime{}
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if rows.Scan(&key, &value) != nil {
			return service.ModelPlazaRuntime{}
		}
		values[key] = value
	}
	if rows.Err() != nil {
		return service.ModelPlazaRuntime{}
	}
	return service.ModelPlazaRuntimeFromValues(values)
}
func (h *Handler) publicGuard(session []gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.settings == nil {
			return
		}
		runtime := h.settings.GetModelPlazaRuntime(c.Request.Context())
		if !runtime.Enabled {
			writeFault(c, fault.NotFound)
			return
		}
		if !runtime.RequireAuth {
			return
		}
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(session) == 0 {
			writeFault(c, fault.Unauthenticated)
			return
		}
		// Reuse the strict session middleware supplied by the router: signature,
		// expiry, user status, token version, revocation and session binding.
		session[0](c)
	}
}

type scopedKeyPriceResolver struct {
	db     *sql.DB
	access identity.AccessEvaluator
	store  pricing.VersionStore
	keys   *service.APIKeyService
	reader catalog.CatalogReader
}

func (r scopedKeyPriceResolver) ResolveKeyPrice(ctx context.Context, p catalog.Perspective, keyID int64, d catalog.Detail, protocol, action string, usage pricing.Usage, at time.Time) (pricing.Version, error) {
	if r.db == nil || p.Workspace <= 0 || p.Project <= 0 || keyID <= 0 {
		return pricing.Version{}, fault.NotFound
	}
	if _, err := r.access.Evaluate(ctx, p.Actor, p.Workspace, p.Project, "models.read"); err != nil {
		return pricing.Version{}, err
	}
	use, err := identitypostgres.New(r.db).AuthorizeKeyUse(ctx, p.Actor, p.Workspace, p.Project, keyID, action, []string{d.Model.Model})
	if err != nil {
		return pricing.Version{}, err
	}
	if use.Platform != "composite" && domain.CanonicalPlatform(use.Platform) != domain.CanonicalPlatform(d.Model.Provider) {
		return pricing.Version{}, fault.NotFound
	}
	if r.keys == nil {
		return pricing.Version{}, fault.Forbidden
	}
	ctx = domain.WithWorkspaceScope(ctx, p.Workspace)
	if err := r.keys.AuthorizePricingModel(ctx, keyID, d.Model.Provider, d.Model.Model); err != nil {
		return pricing.Version{}, err
	}
	if use.OwnerID != p.Actor {
		if r.reader == nil {
			return pricing.Version{}, &fault.Fault{Status: 503, Reason: "PRICING_UNAVAILABLE", Message: "Credential owner pricing is unavailable."}
		}
		payerPerspective := p
		payerPerspective.PricingActor = use.OwnerID
		snapshot, err := r.reader.Snapshot(ctx, payerPerspective, at)
		if err != nil {
			return pricing.Version{}, err
		}
		found := false
		for _, ownerDetail := range snapshot.Details {
			if ownerDetail.Model.ID == d.Model.ID {
				d, found = ownerDetail, true
				break
			}
		}
		if !found {
			return pricing.Version{}, fault.NotFound
		}
	}
	pc := pricing.PriceContext{WorkspaceID: p.Workspace, ProjectID: p.Project, ActorID: p.Actor, PayerID: use.OwnerID, KeyID: keyID, GroupID: use.GroupID,
		Provider: d.Model.Provider, Model: d.Model.Model, ModelID: d.Model.ID, Funding: "wallet", PolicyVersion: p.AccessVersion, CatalogVersion: d.Model.Version, PricingAt: at}
	return quotes.ResolveSaleVersion(ctx, r.store, pc, d)
}

var ProviderSet = wire.NewSet(
	postgres.New, legacy.NewSource, catalog.NewReader, NewWithPricingAndKeys,
	wire.Bind(new(catalog.Acquisition), new(*legacy.Source)),
	wire.Bind(new(catalog.CatalogPublisher), new(*postgres.Store)),
	wire.Bind(new(catalog.CatalogReader), new(*catalog.Reader)),
)

func Register(parent *gin.RouterGroup, h *Handler, public, session []gin.HandlerFunc) {
	anonymous := parent.Group("/public/models", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, public...)...)
	anonymous.Use(h.publicGuard(session))
	scoped := parent.Group("/workspaces/:workspace_id/projects/:project_id/models", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	project := parent.Group("/workspaces/:workspace_id/projects/:project_id", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	for _, g := range []*gin.RouterGroup{anonymous, scoped} {
		g.GET("", h.serve(h.list))
		g.GET("/*id", h.serve(h.detail))
		g.POST("/compare", h.serve(h.compare))
	}
	project.POST("/quotes", h.serve(func(ctx context.Context, p catalog.Perspective, c *gin.Context) (any, error) {
		return h.quotes.Handle(ctx, p, c)
	}))
	project.GET("/model-diagnostics/:model_id", h.serve(h.diagnostics))
}

type endpoint func(context.Context, catalog.Perspective, *gin.Context) (any, error)

func (h *Handler) serve(fn endpoint) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		if subject, ok := middleware.GetAuthSubjectFromContext(c); ok && subject.UserID > 0 {
			ctx = catalog.WithPublicViewerAuthenticated(ctx)
		}
		p, err := h.perspective(ctx, c)
		if err == nil {
			var data any
			data, err = fn(ctx, p, c)
			if err == nil && !p.Public() {
				var current catalog.Perspective
				current, err = h.perspective(ctx, c)
				if err == nil && current != p {
					err = snapshotExpired
				}
			}
			if err == nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": data})
				return
			}
		}
		writeFault(c, err)
	}
}

func (h *Handler) perspective(ctx context.Context, c *gin.Context) (catalog.Perspective, error) {
	workspace := c.Param("workspace_id")
	if workspace == "" {
		return catalog.Perspective{Workspace: domain.DefaultWorkspaceID, Audience: "public"}, nil
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		return catalog.Perspective{}, fault.Unauthenticated
	}
	w, project := positiveID(workspace), positiveID(c.Param("project_id"))
	if w == 0 || project == 0 {
		return catalog.Perspective{}, fault.NotFound
	}
	a, err := h.access.Evaluate(ctx, subject.UserID, w, project, "models.read")
	if err != nil {
		return catalog.Perspective{}, err
	}
	return catalog.Perspective{Actor: a.ActorID, Workspace: a.WorkspaceID, Project: a.ProjectID,
		AccessVersion: a.Version, EndpointMetadata: a.Require("connections.read") == nil, Audience: "scoped", ScopeKind: "project"}, nil
}

func positiveID(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != s {
		return 0
	}
	return n
}

func parseQuery(c *gin.Context) (url.Values, error) {
	q, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, fault.Invalid("/query")
	}
	return q, nil
}

func (h *Handler) detail(ctx context.Context, p catalog.Perspective, c *gin.Context) (any, error) {
	q, err := parseQuery(c)
	if err != nil {
		return nil, err
	}
	for field, v := range q {
		if field != "catalog_version" || len(v) != 1 || !isIdentifier(v[0], "cat_", 64) {
			return nil, fault.Invalid("/" + field)
		}
	}
	id := strings.TrimPrefix(c.Param("id"), "/")
	stableID := isIdentifier(id, "m_", 32)
	provider, model, slug := strings.Cut(id, "/")
	if !stableID && (!slug || provider == "" || model == "") {
		return nil, fault.NotFound
	}
	snapshot, err := h.reader.Snapshot(ctx, p, time.Time{})
	if err != nil {
		return nil, err
	}
	if version := q.Get("catalog_version"); version != "" && version != snapshot.Version {
		return nil, snapshotExpired
	}
	var modelMatch *catalog.Detail
	ambiguous := false
	for _, d := range snapshot.Details {
		if d.Model.ID == id || (slug && d.Model.Provider == provider && d.Model.Model == model) {
			return d, nil
		}
		if slug && d.Model.Model == id {
			ambiguous = modelMatch != nil
			modelMatch = &d
		}
	}
	if modelMatch != nil && !ambiguous {
		return *modelMatch, nil
	}
	return nil, fault.NotFound
}

func isIdentifier(value, prefix string, n int) bool {
	digits, ok := strings.CutPrefix(value, prefix)
	return ok && len(digits) == n && strings.IndexFunc(digits, func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'f')
	}) < 0
}

var snapshotExpired = &fault.Fault{Status: 410, Reason: "SNAPSHOT_EXPIRED", Message: "Refresh the model catalog and try again."}

func writeFault(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		slog.WarnContext(c.Request.Context(), "catalog_read_failed", "request_id", c.Writer.Header().Get("X-Request-ID"), "error", err)
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Catalog service unavailable."}
	}
	category, retry := "validation", "never"
	switch f.Status {
	case 401, 403, 404:
		category = "access"
	case 410:
		category = "conflict"
	case 503:
		category, retry = "dependency", "retry_later"
	}
	fields := []gin.H{}
	if f.Field != "" {
		fields = append(fields, gin.H{"path": f.Field, "reason": f.Reason})
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "message": f.Message, "reason": f.Reason, "data": nil,
		"metadata": gin.H{"category": category, "retry": retry, "fields": fields, "request_id": c.Writer.Header().Get("X-Request-ID"),
			"operation_id": nil, "status_url": nil}})
}

func decodeJSON(c *gin.Context, v any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(v) != nil || decoder.Decode(new(any)) != io.EOF {
		return &fault.Fault{Status: 400, Reason: "INVALID_JSON", Message: "Invalid request body."}
	}
	return nil
}
