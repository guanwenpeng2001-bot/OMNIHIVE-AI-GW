package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/gatewayerror"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// GatewayRoutingConstraints freezes the request layer before extensions or dispatch.
func GatewayRoutingConstraints() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, err := service.WithGatewayRoutingHeaders(c.Request.Context(), c.Request.Header)
		if err != nil {
			gatewayerror.Write(c, 400, "INVALID_ROUTING_CONSTRAINT", err.Error())
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// GatewayRoutingAdmission applies the authenticated policy even on routes that
// enqueue work or use an adapter with its own selection path.
func GatewayRoutingAdmission(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		if !service.HasGatewayRoutingConstraints(ctx) {
			c.Next()
			return
		}
		models := []string{}
		if model := groupModelAllowlistModelFromParams(c); model != "" {
			models = append(models, model)
		} else if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodPatch {
			var ok bool
			models, ok = groupModelAllowlistModelsFromBody(c)
			if !ok {
				return
			}
		}
		if len(models) == 0 {
			models = []string{c.Query("model")}
		}
		for _, model := range models {
			checked, err := service.CheckGatewayRoutingPolicy(ctx, cfg, model, isResponsesWebSocketRoute(c))
			if err != nil {
				deny := policy.NativePolicyDenied
				gatewayerror.Write(c, deny.Status, deny.Reason, deny.Message)
				c.Abort()
				return
			}
			ctx = checked
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
