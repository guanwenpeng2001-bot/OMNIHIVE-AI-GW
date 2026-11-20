package verification

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog/verification"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

type Handler struct{ store *domain.Store }

func New(store *domain.Store) *Handler { return &Handler{store: store} }

var ProviderSet = wire.NewSet(domain.NewStore, domain.NewConnectionProber, domain.NewWorker, New)

func Register(v2 *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	admin := v2.Group("/admin/catalog", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	admin.POST("/verifications", serve(http.StatusAccepted, h.enqueue))
	admin.POST("/verification-targets", serve(http.StatusCreated, h.createTarget))
	admin.GET("/verification-targets/:id", serve(http.StatusOK, h.target))
	admin.PUT("/verification-targets/:id/schedule", serve(http.StatusOK, h.schedule))
	admin.POST("/verification-targets/:id/publications", serve(http.StatusCreated, h.publish))
}

type command func(*gin.Context, int64) (any, error)

func serve(status int, fn command) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			writeFault(c, fault.Unauthenticated)
			return
		}
		if c.Request.URL.RawQuery != "" {
			writeFault(c, fault.Invalid("/query"))
			return
		}
		data, err := fn(c, subject.UserID)
		if err != nil {
			writeFault(c, err)
			return
		}
		message := "success"
		if status == http.StatusAccepted {
			message = "accepted"
		}
		c.JSON(status, gin.H{"code": 0, "message": message, "data": data})
	}
}

func (h *Handler) enqueue(c *gin.Context, actor int64) (any, error) {
	in, err := body[domain.Matrix](c)
	if err != nil {
		return nil, err
	}
	return h.store.Enqueue(c.Request.Context(), actor, c.GetHeader("Idempotency-Key"), in)
}

func (h *Handler) createTarget(c *gin.Context, actor int64) (any, error) {
	in, err := body[domain.CreateTarget](c)
	if err != nil {
		return nil, err
	}
	out, err := h.store.CreateTarget(c.Request.Context(), actor, c.GetHeader("Idempotency-Key"), in)
	if err == nil {
		c.Header("ETag", versionTag(out.ScheduleVersion))
	}
	return out, err
}

func (h *Handler) target(c *gin.Context, actor int64) (any, error) {
	out, err := h.store.Target(c.Request.Context(), actor, c.Param("id"))
	if err == nil {
		c.Header("ETag", versionTag(out.ScheduleVersion))
	}
	return out, err
}

func (h *Handler) schedule(c *gin.Context, actor int64) (any, error) {
	expected, err := version(c.GetHeader("If-Match"))
	if err != nil {
		return nil, err
	}
	in, err := body[struct {
		IntervalSeconds *int `json:"interval_seconds"`
	}](c)
	if err != nil {
		return nil, err
	}
	if in.IntervalSeconds == nil {
		return nil, fault.Invalid("/interval_seconds")
	}
	out, err := h.store.SetSchedule(c.Request.Context(), actor, c.Param("id"), expected, *in.IntervalSeconds)
	if err == nil {
		c.Header("ETag", versionTag(out.ScheduleVersion))
	}
	return out, err
}

func (h *Handler) publish(c *gin.Context, actor int64) (any, error) {
	expected, err := version(c.GetHeader("If-Match"))
	if err != nil {
		return nil, err
	}
	in, err := body[domain.Publish](c)
	if err != nil {
		return nil, err
	}
	out, err := h.store.Publish(c.Request.Context(), actor, c.Param("id"), c.GetHeader("Idempotency-Key"), expected, in)
	if err == nil {
		c.Header("ETag", versionTag(out.Version))
	}
	return out, err
}

func body[T any](c *gin.Context) (T, error) {
	r := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	r.DisallowUnknownFields()
	var in *T
	if r.Decode(&in) != nil || in == nil || r.Decode(new(any)) != io.EOF {
		var zero T
		return zero, &fault.Fault{Status: 400, Reason: "INVALID_JSON", Message: "Invalid request body."}
	}
	return *in, nil
}

func versionTag(v int64) string { return `"` + strconv.FormatInt(v, 10) + `"` }

func version(raw string) (int64, error) {
	if len(raw) >= 3 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		v, err := strconv.ParseInt(raw[1:len(raw)-1], 10, 64)
		if err == nil && v >= 0 && raw == versionTag(v) {
			return v, nil
		}
	}
	return 0, fault.Invalid("/If-Match")
}

func writeFault(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Catalog verification is unavailable."}
	}
	category, retry := "validation", "never"
	switch f.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		category = "access"
	case http.StatusConflict, http.StatusPreconditionFailed:
		category = "conflict"
	case http.StatusTooManyRequests:
		category, retry = "capacity", "after_delay"
		c.Header("Retry-After", "2")
	case http.StatusServiceUnavailable:
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
	if f.Status == http.StatusPreconditionFailed {
		metadata["current_version"] = strconv.FormatInt(f.CurrentVersion, 10)
		c.Header("ETag", versionTag(f.CurrentVersion))
	}
	c.AbortWithStatusJSON(f.Status, gin.H{
		"code": f.Status, "message": f.Message, "reason": f.Reason, "data": nil, "metadata": metadata,
	})
}
