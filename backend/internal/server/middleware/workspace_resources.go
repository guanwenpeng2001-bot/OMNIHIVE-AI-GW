package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

var workspaceResourcePaths = map[string]string{
	"proxy-groups": "proxy_groups",
	"accounts":     "accounts", "groups": "groups", "channels": "channels", "proxies": "proxies",
	"api-keys": "api_keys", "keys": "api_keys", "channel-monitors": "channel_monitors",
	"channel-monitor-templates": "channel_monitor_request_templates", "scheduled-test-plans": "scheduled_test_plans",
}

var workspaceReferenceFields = map[string]string{
	"proxy_group_id": "proxy_groups", "proxy_group_ids": "proxy_groups", "fallback_proxy_id": "proxies",
	"user_id": "users", "user_ids": "users",
	"account_id": "accounts", "account_ids": "accounts", "parent_account_id": "accounts",
	"group_id": "groups", "group_ids": "groups", "fallback_group_id": "groups", "fallback_group_id_on_invalid_request": "groups",
	"copy_accounts_from_group_ids": "groups",
	"target_group_id":              "groups", "target_group_ids": "groups", "proxy_id": "proxies", "proxy_ids": "proxies", "backup_proxy_id": "proxies",
	"channel_id": "channels", "channel_ids": "channels", "api_key_id": "api_keys", "api_key_ids": "api_keys",
	"monitor_id": "channel_monitors", "monitor_ids": "channel_monitors", "template_id": "channel_monitor_request_templates",
}

// WorkspaceResources checks identifiers before existing handlers perform side
// effects or consult caches. Repositories still scope list queries and writes.
func WorkspaceResources(workspaces *service.WorkspaceService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		workspaceID, scoped := domain.WorkspaceScope(ctx.Request.Context())
		if !scoped {
			ctx.Abort()
			response.ErrorFrom(ctx, service.ErrWorkspaceNotFound)
			return
		}
		references := map[string][]int64{}
		segments := strings.Split(strings.Trim(ctx.FullPath(), "/"), "/")
		resource := ""
		for _, segment := range segments {
			if candidate, exists := workspaceResourcePaths[segment]; exists {
				resource = candidate
				break
			}
		}
		if identifier := ctx.Param("id"); identifier != "" && resource != "" {
			parsed, err := strconv.ParseInt(identifier, 10, 64)
			if err != nil || parsed <= 0 {
				response.ErrorFrom(ctx, service.ErrWorkspaceNotFound)
				ctx.Abort()
				return
			}
			references[resource] = append(references[resource], parsed)
		}
		if identifier := ctx.Param("route_id"); identifier != "" {
			parsed, err := strconv.ParseInt(identifier, 10, 64)
			if err != nil || parsed <= 0 {
				response.ErrorFrom(ctx, service.ErrWorkspaceNotFound)
				ctx.Abort()
				return
			}
			references["composite_model_routes"] = append(references["composite_model_routes"], parsed)
		}
		for field, values := range ctx.Request.URL.Query() {
			target := workspaceReferenceFields[field]
			if field == "ids" {
				target = resource
			}
			if target == "" {
				continue
			}
			for _, value := range values {
				for _, token := range strings.Split(value, ",") {
					identifier, err := strconv.ParseInt(token, 10, 64)
					if err == nil && identifier > 0 {
						references[target] = append(references[target], identifier)
					}
				}
			}
		}
		if ctx.Request.Body != nil && strings.Contains(ctx.GetHeader("Content-Type"), "application/json") {
			body, err := io.ReadAll(ctx.Request.Body)
			if err != nil {
				response.BadRequest(ctx, "Invalid request body")
				ctx.Abort()
				return
			}
			ctx.Request.Body = io.NopCloser(bytes.NewReader(body))
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.UseNumber()
			var payload any
			if decoder.Decode(&payload) == nil {
				collectWorkspaceReferences(payload, resource, references)
			}
		}
		for target, identifiers := range references {
			if err := workspaces.RequireResources(ctx.Request.Context(), workspaceID, target, identifiers); err != nil {
				response.ErrorFrom(ctx, err)
				ctx.Abort()
				return
			}
		}
		ctx.Next()
	}
}

func collectWorkspaceReferences(payload any, resource string, references map[string][]int64) {
	switch value := payload.(type) {
	case map[string]any:
		for field, child := range value {
			if field == "model_routing" {
				if routing, ok := child.(map[string]any); ok {
					for _, targets := range routing {
						collectWorkspaceReferences(map[string]any{"account_ids": targets}, resource, references)
					}
				}
			}
			target := workspaceReferenceFields[field]
			if field == "ids" {
				target = resource
			}
			if target != "" {
				collect := func(raw any) {
					if number, ok := raw.(json.Number); ok {
						if identifier, err := number.Int64(); err == nil && identifier > 0 {
							references[target] = append(references[target], identifier)
						}
					}
				}
				if items, ok := child.([]any); ok {
					for _, item := range items {
						collect(item)
					}
				} else {
					collect(child)
				}
			}
			// Credentials and provider metadata have their own identifier namespaces.
			if field != "credentials" && field != "extra" && field != "model_mapping" {
				collectWorkspaceReferences(child, resource, references)
			}
		}
	case []any:
		for _, child := range value {
			collectWorkspaceReferences(child, resource, references)
		}
	}
}
