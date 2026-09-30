package handler

import (
	"errors"
	"net/http"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// CompositeVideoJobTarget resolves model-less lifecycle requests from the same
// owner-scoped store the concrete handlers use. A supplied model must never
// replace a persisted job's provider or cause a new account selection.
func (gatewayHandler *OpenAIGatewayHandler) CompositeVideoJobTarget(next gin.HandlerFunc) gin.HandlerFunc {
	return func(request *gin.Context) {
		key, ok := middleware2.GetAPIKeyFromContext(request)
		action := service.GatewayActionForRoute(request.Request.Method, request.Request.URL.Path)
		if !ok || key == nil || key.Group == nil || key.Group.Platform != service.PlatformComposite || request.Param("request_id") == "" ||
			(action != "video.status" && action != "video.content" && action != "video.cancel") {
			next(request)
			return
		}
		if gatewayHandler == nil || gatewayHandler.upstreamJobs == nil {
			request.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "api_error", "message": "Video jobs are unavailable"}})
			return
		}
		job, err := gatewayHandler.upstreamJobs.Get(request.Request.Context(), request.Param("request_id"), key.UserID, key.ID)
		if err == nil && job != nil && job.UserID == key.UserID && job.APIKeyID == key.ID && job.Family == "video" {
			request.Request = request.Request.WithContext(service.WithResolvedTargetPlatform(request.Request.Context(), job.Platform))
			request.Next()
			return
		}
		if err != nil && !errors.Is(err, service.ErrUpstreamJobNotFound) {
			request.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "api_error", "message": "Video jobs are unavailable"}})
			return
		}
		// Grok's existing lifecycle uses a user/key-scoped ownership binding rather
		// than the durable public-job store. Require that positive binding; never
		// guess Grok from an unrecognized or foreign public ID.
		if errors.Is(err, service.ErrUpstreamJobNotFound) && gatewayHandler.gatewayService != nil {
			accountID, bindingErr := gatewayHandler.gatewayService.ResolveGrokMediaVideoRequestAccount(request.Request.Context(), key.GroupID, request.Param("request_id"), key.UserID, key.ID)
			if bindingErr == nil && accountID > 0 {
				request.Request = request.Request.WithContext(service.WithResolvedTargetPlatform(request.Request.Context(), service.PlatformGrok))
				request.Next()
				return
			}
		}
		request.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "Video not found"}})
	}
}
