package observability

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/google/wire"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	identitydb "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/export"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type Handler struct{ store *export.Store }

func New(store *export.Store) *Handler { return &Handler{store: store} }

func ProvideStore(cfg *config.Config) (*export.Store, func(), error) {
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		return nil, nil, errors.New("telemetry export database configuration is invalid")
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	store := export.New(db, identitydb.New(db), service.NewArtifactEgress())
	return store, func() { _ = db.Close() }, nil
}

var ProviderSet = wire.NewSet(ProvideStore, New)

func Register(v2 *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	g := v2.Group("/workspaces/:workspace_id/observability",
		append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	g.POST("/destinations", serve(http.StatusCreated, h.registerDestination))
	g.DELETE("/destinations/:id", serve(http.StatusOK, h.revokeDestination))
	g.POST("/exports", serve(http.StatusCreated, h.create))
	g.GET("/exports/:id", serve(http.StatusOK, h.detail))
	g.PATCH("/exports/:id", serve(http.StatusOK, h.change))
}

type request struct {
	*gin.Context
	caller export.Caller
	id     uuid.UUID
}

type endpoint func(request) (any, error)

func serve(status int, fn endpoint) gin.HandlerFunc {
	return func(c *gin.Context) {
		who, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || who.UserID <= 0 {
			fail(c, fault.Unauthenticated)
			return
		}
		w := positiveID(c.Param("workspace_id"))
		if w == 0 {
			fail(c, fault.NotFound)
			return
		}
		r := request{Context: c, caller: export.Caller{ActorID: who.UserID, WorkspaceID: w}}
		if raw := c.Param("id"); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil || id == uuid.Nil || id.String() != raw {
				fail(c, fault.NotFound)
				return
			}
			r.id = id
		}
		out, err := fn(r)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(status, gin.H{"code": 0, "message": "success", "data": out})
	}
}

func positiveID(raw string) int64 {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != raw {
		return 0
	}
	return n
}

func (r request) key() (string, error) {
	values := r.Request.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		return "", fault.Invalid("/Idempotency-Key")
	}
	return values[0], nil
}

func (r request) version() (int64, error) {
	values := r.Request.Header.Values("If-Match")
	if len(values) == 1 {
		v := values[0]
		if len(v) >= 3 && v[0] == '"' && v[len(v)-1] == '"' {
			if n := positiveID(v[1 : len(v)-1]); n != 0 {
				return n, nil
			}
		}
	}
	return 0, fault.Invalid("/If-Match")
}

func (r request) decode(out any) error {
	d := json.NewDecoder(http.MaxBytesReader(r.Writer, r.Request.Body, 8<<10))
	var doc json.RawMessage
	if d.Decode(&doc) != nil || len(doc) == 0 || doc[0] != '{' || d.Decode(new(any)) != io.EOF {
		return fault.Invalid("/body")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(doc, &fields); err != nil {
		return fault.Invalid("/body")
	}
	for _, value := range fields {
		if bytes.Equal(value, []byte("null")) {
			return fault.Invalid("/body")
		}
	}
	d = json.NewDecoder(bytes.NewReader(doc))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return fault.Invalid("/body")
	}
	return nil
}

func (h *Handler) registerDestination(r request) (any, error) {
	key, err := r.key()
	if err != nil {
		return nil, err
	}
	var input struct {
		Endpoint string `json:"endpoint"`
	}
	if err = r.decode(&input); err != nil {
		return nil, err
	}
	return h.store.RegisterDestination(r.Request.Context(), r.caller, key, input.Endpoint)
}

func (h *Handler) revokeDestination(r request) (any, error) {
	version, err := r.version()
	if err != nil {
		return nil, err
	}
	return h.store.RevokeDestination(r.Request.Context(), r.caller, r.id, version)
}

func (h *Handler) create(r request) (any, error) {
	key, err := r.key()
	if err != nil {
		return nil, err
	}
	var input export.CreateInput
	if err = r.decode(&input); err != nil {
		return nil, err
	}
	return h.store.Create(r.Request.Context(), r.caller, key, input)
}

func (h *Handler) detail(r request) (any, error) {
	return h.store.Get(r.Request.Context(), r.caller, r.id)
}

func (h *Handler) change(r request) (any, error) {
	version, err := r.version()
	if err != nil {
		return nil, err
	}
	var input export.PatchInput
	if err = r.decode(&input); err != nil {
		return nil, err
	}
	return h.store.Change(r.Request.Context(), r.caller, r.id, version, input)
}

func fail(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		f = &fault.Fault{Status: http.StatusServiceUnavailable, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Telemetry export service is unavailable."}
	}
	category, retry := "validation", "never"
	switch f.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		category = "access"
	case http.StatusConflict, http.StatusPreconditionFailed:
		category = "conflict"
	case http.StatusTooManyRequests:
		category, retry = "capacity", "retry_later"
	case http.StatusServiceUnavailable:
		category, retry = "dependency", "retry_later"
	}
	fields := []gin.H{}
	if f.Field != "" {
		fields = append(fields, gin.H{"path": f.Field, "reason": f.Reason})
	}
	metadata := gin.H{"request_id": c.Writer.Header().Get("X-Request-ID"), "category": category,
		"retry": retry, "fields": fields, "operation_id": nil, "status_url": nil}
	if f.CurrentVersion > 0 {
		v := strconv.FormatInt(f.CurrentVersion, 10)
		metadata["current_version"] = v
		c.Header("ETag", strconv.Quote(v))
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "message": f.Message, "reason": f.Reason, "data": nil, "metadata": metadata})
}
