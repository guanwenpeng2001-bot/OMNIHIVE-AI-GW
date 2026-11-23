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
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type plazaSettings interface {
	GetModelPlazaRuntime(context.Context) service.ModelPlazaRuntime
}

type Handler struct {
	settings      plazaSettings
	reader        catalog.CatalogReader
	access        identity.AccessEvaluator
	cursorKey     [32]byte
	quotes        quotes.Service
	comparePrices ComparePricePort
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
		}
	}
	return h
}

func (h *Handler) SetComparePricePort(port ComparePricePort) {
	h.comparePrices = port
}

func (h *Handler) SetQuoteService(quoteService quotes.Service) {
	h.quotes = quoteService
}

func (h *Handler) SetPlazaSettings(settings *service.SettingService) {
	h.settings = settings
}

// The production constructor already receives the settings database. Preserve
// New's nil-settings behavior for embedded callers and existing tests.
type plazaDBSettings struct{ db *sql.DB }

func (s plazaDBSettings) GetModelPlazaRuntime(ctx context.Context) service.ModelPlazaRuntime {
	runtime := service.ModelPlazaRuntime{}
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
		if key == "model_plaza_enabled" {
			runtime.Enabled = value == "true"
		}
		if key == "model_plaza_require_auth" {
			runtime.RequireAuth = value == "true"
		}
	}
	if rows.Err() != nil {
		return service.ModelPlazaRuntime{}
	}
	return runtime
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

var ProviderSet = wire.NewSet(
	postgres.New, legacy.NewSource, catalog.NewReader, NewWithPricing,
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
			if !p.Public() {
				c.Header("Cache-Control", "private, no-store")
			}
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
		return catalog.Perspective{Workspace: domain.DefaultWorkspaceID}, nil
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
	return catalog.Perspective{Actor: a.ActorID, Workspace: a.WorkspaceID, Project: a.ProjectID, Kind: catalog.PerspectiveProject,
		AccessVersion: a.Version, EndpointMetadata: a.Require("connections.read") == nil}, nil
}

func withSelectedKey(p catalog.Perspective, value string) (catalog.Perspective, error) {
	if value == "" {
		return p, nil
	}
	key := positiveID(value)
	if p.Public() || key <= 0 {
		return catalog.Perspective{}, fault.Invalid("/api_key_id")
	}
	p.Kind, p.KeyID = catalog.PerspectiveSelectedKey, key
	return p, nil
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
		if field == "api_key_id" && len(v) == 1 && v[0] != "" {
			p, err = withSelectedKey(p, v[0])
			if err != nil {
				return nil, err
			}
			continue
		}
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
	if d, ok := snapshot.Find(id); ok {
		return d, nil
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
