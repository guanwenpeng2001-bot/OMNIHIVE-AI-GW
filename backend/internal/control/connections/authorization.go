package connections

import (
	"github.com/gin-gonic/gin"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	connections "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
)

// The legacy issuer scopes its HttpOnly proof to this workspace path.
func RegisterAuthorization(parent *gin.RouterGroup, h *Handler, session ...gin.HandlerFunc) {
	w := parent.Group("/workspaces/:workspace_id/connections", append([]gin.HandlerFunc{identityhttp.TenantHeaders}, session...)...)
	w.PUT("/:id/authorization", h.serve(200, route{"connections.edit credentials.write", true, true}, func(c *gin.Context, b []byte) (connections.Work, error) {
		w, e := connections.ID(c.Param("workspace_id"))
		if e != nil {
			return nil, fault.NotFound
		}
		c.Request = c.Request.WithContext(domain.WithWorkspaceScope(c.Request.Context(), w))
		binding, e := admin.CredentialAuthorizationBinding(c)
		if e != nil {
			return nil, fault.NotFound
		}
		return input(func(r *connections.Session, in connections.Authorization) (any, error) { return r.Attach(in, binding) })(c, b)
	}))
}
