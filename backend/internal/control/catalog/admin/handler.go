package admin

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
)

type Handler struct {
	reader    catalog.CatalogReader
	access    identity.AccessEvaluator
	cursorKey [32]byte
}

func New(reader catalog.CatalogReader, access identity.AccessEvaluator, cfg *config.Config) *Handler {
	return &Handler{reader: reader, access: access,
		cursorKey: sha256.Sum256([]byte("omnihive/admin-catalog-cursor/v1\x00" + cfg.JWT.Secret))}
}

func (h *Handler) List(c *gin.Context)   { h.serve(c, h.list) }
func (h *Handler) Detail(c *gin.Context) { h.serve(c, h.detail) }

type endpoint func(*gin.Context, catalog.Perspective, url.Values) (any, error)

func (h *Handler) serve(c *gin.Context, read endpoint) {
	c.Header("Cache-Control", "private, no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		writeFault(c, fault.Unauthenticated)
		return
	}
	if role, _ := middleware.GetUserRoleFromContext(c); role != domain.RoleAdmin {
		writeFault(c, fault.Forbidden)
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		writeFault(c, fault.Invalid("/query"))
		return
	}
	for _, values := range query {
		if len(values) != 1 || values[0] == "" {
			writeFault(c, fault.Invalid("/query"))
			return
		}
	}
	workspace, err := scopeID(query, "workspace_id", true)
	if err != nil {
		writeFault(c, err)
		return
	}
	project, err := scopeID(query, "project_id", false)
	if err != nil {
		writeFault(c, err)
		return
	}
	middleware.SetAuditExtra(c, map[string]any{"workspace_id": workspace, "project_id": project})
	// The existing platform catalog action verifies the persisted role together
	// with workspace existence, project ownership and the scope revision.
	a, err := h.access.Evaluate(c.Request.Context(), subject.UserID, workspace, project, "catalog.verify")
	if err != nil {
		writeFault(c, dependencyFault(err))
		return
	}
	if a.ActorID != subject.UserID || a.WorkspaceID != workspace || a.ProjectID != project {
		writeFault(c, fault.NotFound)
		return
	}
	p := catalog.Perspective{Kind: catalog.PerspectiveAdmin, Actor: subject.UserID, Workspace: workspace,
		Project: project, AccessVersion: a.Version, EndpointMetadata: true}
	out, err := read(c, p, query)
	if err != nil {
		writeFault(c, err)
		return
	}
	response.Success(c, out)
}

var modelID = regexp.MustCompile(`^m_[0-9a-f]{32}$`)
var catalogVersion = regexp.MustCompile(`^cat_[0-9a-f]{64}$`)

func scopeID(q url.Values, name string, required bool) (int64, error) {
	v := q.Get(name)
	if !required && v == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != v {
		return 0, fault.Invalid("/" + name)
	}
	return id, nil
}

func writeFault(c *gin.Context, err error) {
	var f *fault.Fault
	if !errors.As(err, &f) {
		f = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Catalog service unavailable."}
	}
	category, retry := "validation", "never"
	switch f.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		category = "access"
	case http.StatusGone:
		category = "conflict"
	case http.StatusServiceUnavailable:
		category, retry = "dependency", "retry_later"
	}
	fields := []gin.H{}
	if f.Field != "" {
		fields = append(fields, gin.H{"path": f.Field, "reason": f.Reason})
	}
	c.AbortWithStatusJSON(f.Status, gin.H{"code": f.Status, "message": f.Message, "reason": f.Reason, "data": nil, "metadata": gin.H{
		"request_id": c.Writer.Header().Get("X-Request-ID"), "category": category, "retry": retry,
		"fields": fields, "operation_id": nil, "status_url": nil,
	}})
}

func dependencyFault(err error) error {
	var f *fault.Fault
	if errors.As(err, &f) {
		switch f.Status {
		case http.StatusUnauthorized:
			return fault.Unauthenticated
		case http.StatusForbidden:
			return fault.Forbidden
		case http.StatusNotFound:
			return fault.NotFound
		case http.StatusServiceUnavailable:
			if f.Reason == "CATALOG_CHANGED" {
				return &fault.Fault{Status: 503, Reason: "CATALOG_CHANGED", Message: "The model catalog changed during the read. Try again."}
			}
		}
	}
	return &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Catalog service unavailable."}
}
