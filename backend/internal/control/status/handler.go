package status

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	status "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/status"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

type Handler struct {
	store     *status.Store
	access    identity.AccessEvaluator
	cursorKey [32]byte
	enabled   func(context.Context) bool
	now       func() time.Time
}

func New(db *sql.DB, access identity.AccessEvaluator, secret string, enabled func(context.Context) bool) *Handler {
	return &Handler{
		store: status.NewStore(db), access: access,
		cursorKey: sha256.Sum256([]byte("omnihive/public-incident-cursor/v1\x00" + secret)),
		enabled:   enabled, now: time.Now,
	}
}

func (h *Handler) RunCollector(ctx context.Context) { h.store.Run(ctx, h.enabled) }

func Register(v2 *gin.RouterGroup, h *Handler, public, session []gin.HandlerFunc) {
	anonymous := v2.Group("/public/status", public...)
	anonymous.GET("", h.summary)
	anonymous.GET("/incidents", h.incidents)
	anonymous.GET("/incidents/:id", h.incident)
	admin := v2.Group("/admin/status/incidents", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	admin.POST("", h.publish(http.StatusCreated, h.create))
	admin.POST("/:id/updates", h.publish(http.StatusCreated, h.addUpdate))
}

func (h *Handler) summary(c *gin.Context) {
	window, err := parseWindow(c.Request.URL.RawQuery)
	if err != nil {
		writeFault(c, err)
		return
	}
	ctx := c.Request.Context()
	result, modified, err := h.store.Summary(ctx, h.now(), window, h.enabled == nil || h.enabled(ctx))
	if err != nil {
		writeFault(c, err)
		return
	}
	writePublic(c, result, modified)
}

func parseWindow(raw string) (time.Duration, error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return 0, fault.Invalid("/query")
	}
	if len(query) == 0 {
		return 24 * time.Hour, nil
	}
	if len(query) != 1 || len(query["window"]) != 1 {
		return 0, fault.Invalid("/query")
	}
	switch query.Get("window") {
	case "1h":
		return time.Hour, nil
	case "24h":
		return 24 * time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	case "30d":
		return 30 * 24 * time.Hour, nil
	default:
		return 0, fault.Invalid("/window")
	}
}

func (h *Handler) incident(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		writeFault(c, fault.Invalid("/query"))
		return
	}
	result, modified, err := h.store.Incident(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeFault(c, err)
		return
	}
	writePublic(c, result, modified)
}

type incidentCommand func(*gin.Context, int64) (status.PublishedIncident, error)

func (h *Handler) publish(code int, command incidentCommand) gin.HandlerFunc {
	return func(c *gin.Context) {
		who, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || who.UserID <= 0 {
			writeFault(c, fault.Unauthenticated)
			return
		}
		access, err := h.access.Evaluate(c.Request.Context(), who.UserID, domain.DefaultWorkspaceID, 0, status.PublishGrant)
		if err == nil {
			err = access.Require(status.PublishGrant)
		}
		if err != nil {
			writeFault(c, err)
			return
		}
		if access.ActorID != who.UserID || access.WorkspaceID != domain.DefaultWorkspaceID || access.ProjectID != 0 {
			writeFault(c, fault.Forbidden)
			return
		}
		out, err := command(c, who.UserID)
		if err != nil {
			writeFault(c, err)
			return
		}
		c.Header("ETag", versionTag(out.Version))
		c.JSON(code, envelope{Code: 0, Message: "success", Data: out})
	}
}

func (h *Handler) create(c *gin.Context, actor int64) (status.PublishedIncident, error) {
	var in status.CreateIncident
	if err := readBody(c, &in); err != nil {
		return status.PublishedIncident{}, err
	}
	return h.store.CreateIncident(c.Request.Context(), actor, c.GetHeader("Idempotency-Key"), in)
}

func (h *Handler) addUpdate(c *gin.Context, actor int64) (status.PublishedIncident, error) {
	tag := c.GetHeader("If-Match")
	var version int64
	if len(tag) >= 3 && tag[0] == '"' && tag[len(tag)-1] == '"' {
		version, _ = strconv.ParseInt(tag[1:len(tag)-1], 10, 64)
	}
	if version <= 0 || tag != versionTag(version) {
		return status.PublishedIncident{}, fault.Invalid("/If-Match")
	}
	var in status.PostUpdate
	if err := readBody(c, &in); err != nil {
		return status.PublishedIncident{}, err
	}
	return h.store.PostUpdate(c.Request.Context(), actor, c.Param("id"), version, c.GetHeader("Idempotency-Key"), in)
}

func versionTag(version int64) string { return `"` + strconv.FormatInt(version, 10) + `"` }

func readBody(c *gin.Context, out any) error {
	reader := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	reader.DisallowUnknownFields()
	if err := reader.Decode(out); err != nil || reader.Decode(new(any)) != io.EOF {
		return &fault.Fault{Status: 400, Reason: "INVALID_JSON", Message: "Invalid request body."}
	}
	return nil
}

type envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func writePublic(c *gin.Context, out any, modified time.Time) {
	data, err := json.Marshal(envelope{Code: 0, Message: "success", Data: out})
	if err != nil {
		writeFault(c, err)
		return
	}
	digest := sha256.Sum256(data)
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Cache-Control", "public, max-age=15, must-revalidate")
	c.Header("ETag", `W/"`+hex.EncodeToString(digest[:])+`"`)
	// Use HTTP's conditional request semantics, including validators from a
	// proxy or browser cache. The status API serves the whole representation.
	r := c.Request.Clone(c.Request.Context())
	r.Header.Del("Range")
	http.ServeContent(c.Writer, r, "", modified, bytes.NewReader(data))
}

func writeFault(c *gin.Context, err error) {
	c.Header("Cache-Control", "no-store")
	var f *fault.Fault
	if !errors.As(err, &f) {
		slog.ErrorContext(c.Request.Context(), "status service failed", "error", err)
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Status service is unavailable."}
	}
	category, retry := "validation", "never"
	switch f.Status {
	case 401, 403, 404:
		category = "access"
	case 409, 412:
		category = "conflict"
	case 503:
		category, retry = "dependency", "after_delay"
	}
	fields := []gin.H{}
	if f.Field != "" {
		fields = append(fields, gin.H{"path": f.Field, "reason": f.Reason})
	}
	metadata := gin.H{
		"request_id": c.Writer.Header().Get("X-Request-ID"), "category": category,
		"retry": retry, "fields": fields, "operation_id": nil, "status_url": nil,
	}
	if f.CurrentVersion > 0 {
		metadata["current_version"] = strconv.FormatInt(f.CurrentVersion, 10)
		c.Header("ETag", versionTag(f.CurrentVersion))
	}
	c.AbortWithStatusJSON(f.Status, gin.H{
		"code": f.Status, "message": f.Message, "reason": f.Reason, "data": nil, "metadata": metadata,
	})
}
