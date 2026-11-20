package middleware

import (
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/googleapi"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// APIKeyAuthGoogle is a Google-style error wrapper for API key auth.
func APIKeyAuthGoogle(apiKeyService *service.APIKeyService, cfg *config.Config) gin.HandlerFunc {
	return APIKeyAuthWithSubscriptionGoogle(apiKeyService, nil, cfg)
}

// APIKeyAuthWithSubscriptionGoogle behaves like ApiKeyAuthWithSubscription but returns Google-style errors:
// {"error":{"code":401,"message":"...","status":"UNAUTHENTICATED"}}
//
// It is intended for Gemini native endpoints (/v1beta) to match Gemini SDK expectations.
func APIKeyAuthWithSubscriptionGoogle(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rejectInvalidAuthAbuse(c, apiKeyService) {
			abortWithGoogleError(c, 429, "Too many invalid authentication attempts; retry later")
			return
		}
		if apiKeyHeadersTooLarge(c) {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			abortWithGoogleError(c, 401, "Invalid API key")
			return
		}
		if v := strings.TrimSpace(c.Query("api_key")); v != "" {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectQueryAPIKeyDeprecated)
			abortWithGoogleError(c, 400, "Query parameter api_key is deprecated. Use Authorization header or key instead.")
			return
		}
		apiKeyString := extractAPIKeyForGoogle(c)
		if apiKeyString == "" {
			recordInvalidAuthFailure(c, apiKeyService)
			if hasAPIKeyCredentialInput(c) {
				MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			} else {
				MarkIngressRejected(c, IngressRejectAPIKeyRequired)
			}
			abortWithGoogleError(c, 401, "API key is required")
			return
		}
		if len(apiKeyString) > service.MaxAPIKeyCredentialBytes {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			abortWithGoogleError(c, 401, "Invalid API key")
			return
		}

		authenticateKey(c, apiKeyString, apiKeyService, subscriptionService, cfg, true)
	}
}

// extractAPIKeyForGoogle extracts API key for Google/Gemini endpoints.
// Priority: x-goog-api-key > Authorization: Bearer > x-api-key > query key
// This allows OpenClaw and other clients using Bearer auth to work with Gemini endpoints.
func extractAPIKeyForGoogle(c *gin.Context) string {
	// 1) preferred: Gemini native header
	if k := strings.TrimSpace(c.GetHeader("x-goog-api-key")); k != "" {
		return k
	}

	// 2) fallback: Authorization: Bearer <key>
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			if k := strings.TrimSpace(parts[1]); k != "" {
				return k
			}
		}
	}

	// 3) x-api-key header (backward compatibility)
	if k := strings.TrimSpace(c.GetHeader("x-api-key")); k != "" {
		return k
	}

	// 4) query parameter key (for specific paths)
	if allowGoogleQueryKey(c.Request.URL.Path) {
		if v := strings.TrimSpace(c.Query("key")); v != "" {
			return v
		}
	}

	return ""
}

// Native Gemini SDKs support query key on /v1beta/; headers remain preferred.
// This compatibility does not extend to /v1 or similarly prefixed paths.
func allowGoogleQueryKey(path string) bool {
	return strings.HasPrefix(path, "/v1beta/") || strings.HasPrefix(path, "/antigravity/v1beta/")
}

func abortWithGoogleError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    status,
			"message": message,
			"status":  googleapi.HTTPStatusToGoogleStatus(status),
		},
	})
	c.Abort()
}
