package connections

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

type Handler struct{ store *domain.Store }

func New(store *domain.Store) *Handler { return &Handler{store} }

var ProviderSet = wire.NewSet(domain.New, New)

type route struct {
	action       string
	version, key bool
}

func (h *Handler) serve(code int, r route, bind func(*gin.Context, []byte) (domain.Work, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || subject.UserID < 1 {
			fail(c, fault.Unauthenticated)
			return
		}
		cmd := domain.Command{Actor: subject.UserID, Route: domain.CanonicalRoute(c.Request.Method, c.Request.URL.Path)}
		var e error
		cmd.Workspace, e = domain.ID(c.Param("workspace_id"))
		if e != nil {
			fail(c, fault.NotFound)
			return
		}
		if s := c.Param("id"); s != "" {
			cmd.ID, e = domain.ID(s)
			if e != nil {
				fail(c, fault.NotFound)
				return
			}
		}
		if r.version {
			v := c.GetHeader("If-Match")
			if len(v) < 3 || v[0] != '"' || v[len(v)-1] != '"' {
				fail(c, fault.Invalid("/If-Match"))
				return
			}
			cmd.Version, e = domain.ID(v[1 : len(v)-1])
			if e != nil {
				fail(c, fault.Invalid("/If-Match"))
				return
			}
		}
		if r.key {
			cmd.Key = c.GetHeader("Idempotency-Key")
			if cmd.Key == "" {
				fail(c, fault.Invalid("/Idempotency-Key"))
				return
			}
		}
		var body []byte
		if c.Request.Method != http.MethodGet {
			body, e = io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
			if e != nil {
				fail(c, invalidJSON)
				return
			}
			var obj map[string]json.RawMessage
			if json.Unmarshal(body, &obj) != nil || obj == nil {
				fail(c, invalidJSON)
				return
			}
			for k, v := range obj {
				if bytes.Equal(v, []byte("null")) {
					fail(c, fault.Invalid("/"+k))
					return
				}
			}
			// Fingerprint the actual request, including explicit empty assignment lists.
			if cmd.Key != "" {
				var canon any
				decoder := json.NewDecoder(bytes.NewReader(body))
				decoder.UseNumber()
				if decoder.Decode(&canon) != nil {
					fail(c, invalidJSON)
					return
				}
				cmd.Fingerprint, e = json.Marshal(canon)
				if e != nil {
					fail(c, invalidJSON)
					return
				}
			}
		}
		fn, e := bind(c, body)
		if e != nil {
			fail(c, e)
			return
		}
		v, e := h.store.Run(c.Request.Context(), cmd, strings.Fields(r.action), fn)
		if e != nil {
			fail(c, e)
			return
		}
		message := "success"
		if code == 202 {
			message = "accepted"
		}
		c.JSON(code, gin.H{"code": 0, "message": message, "data": v})
	}
}
func input[T any](fn func(*domain.Session, T) (any, error)) func(*gin.Context, []byte) (domain.Work, error) {
	return func(_ *gin.Context, b []byte) (domain.Work, error) {
		var v T
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&v) != nil {
			return nil, invalidJSON
		}
		return func(r *domain.Session) (any, error) { return fn(r, v) }, nil
	}
}
func page(kind string) func(*gin.Context, []byte) (domain.Work, error) {
	return func(c *gin.Context, _ []byte) (domain.Work, error) {
		return func(r *domain.Session) (any, error) { return r.List(kind, c.Request.URL.Query()) }, nil
	}
}
func Register(parent *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	w := parent.Group("/workspaces/:workspace_id", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	w.GET("/provider-schemas", h.serve(200, route{action: "connections.read"}, page("schemas")))
	w.GET("/pools", h.serve(200, route{action: "pools.read"}, page("pools")))
	w.POST("/pools", h.serve(201, route{"pools.edit", false, true}, input((*domain.Session).CreatePool)))
	c := w.Group("/connections")
	c.GET("", h.serve(200, route{action: "connections.read"}, page("connections")))
	c.GET("/:id", h.serve(200, route{action: "connections.read"}, func(*gin.Context, []byte) (domain.Work, error) { return (*domain.Session).Get, nil }))
	c.POST("", h.serve(201, route{"connections.edit", false, true}, input((*domain.Session).Create)))
	c.PATCH("/:id", h.serve(200, route{"connections.edit", true, false}, input((*domain.Session).Patch)))
	c.PUT("/:id/credential", h.serve(200, route{"connections.edit", true, false}, input(func(r *domain.Session, v credentials.Change) (any, error) { return r.Credential(v) })))
	c.POST("/:id/verify", h.serve(202, route{"connections.verify", true, true}, input((*domain.Session).Verify)))
	c.POST("/:id/preview", h.serve(200, route{"connections.edit", true, true}, input(func(r *domain.Session, p domain.Patch) (any, error) { return r.Preview(nil, p) })))
	c.POST("/:id/activate", h.serve(200, route{"connections.activate connections.edit", true, true}, input((*domain.Session).Activate)))
	c.POST("/:id/rotations", h.serve(202, route{"credentials.rotate", true, true}, input((*domain.Session).Rotate)))
	c.GET("/:id/events", h.serve(200, route{action: "credentials.read_metadata"}, page("events")))
	c.POST("/bulk-previews", h.serve(200, route{"connections.edit", false, true}, input(func(r *domain.Session, v domain.BulkPreview) (any, error) { return r.Preview(v.IDs, v.Patch) })))
	c.POST("/bulk-apply", h.serve(202, route{"connections.activate connections.edit", false, true}, input((*domain.Session).BulkApply)))
}

var invalidJSON = &fault.Fault{Status: 400, Reason: "INVALID_JSON", Message: "Invalid request body."}

func fail(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Connection service unavailable."}
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
	m := gin.H{"request_id": c.Writer.Header().Get("X-Request-ID"), "category": category, "retry": retry, "fields": []any{}, "operation_id": nil, "status_url": nil}
	if f.Field != "" {
		m["fields"] = []gin.H{{"path": f.Field, "reason": f.Reason}}
	}
	if f.CurrentVersion > 0 {
		m["current_version"] = strconv.FormatInt(f.CurrentVersion, 10)
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "message": f.Message, "reason": f.Reason, "metadata": m, "data": nil})
}
