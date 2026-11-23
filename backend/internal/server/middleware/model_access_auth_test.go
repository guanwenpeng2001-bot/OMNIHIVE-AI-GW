package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type modelAccessAuthRepo struct {
	fakeAPIKeyRepo
	policy    []string
	policyErr error
}

func (r *modelAccessAuthRepo) ReadModelAccessSnapshot(context.Context, int64) (service.ModelAccessSnapshot, error) {
	return service.ModelAccessSnapshot{Key: r.policy}, r.policyErr
}

func TestModelAccessBothAuthenticationEntrypointsRefreshPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		name := "standard"
		if google {
			name = "google"
		}
		t.Run(name, func(t *testing.T) {
			key := &service.APIKey{WorkspaceID: 1, ID: 18, UserID: 3, Key: "model-access-key", Status: service.StatusActive, User: &service.User{ID: 3, Status: service.StatusActive, Role: service.RoleUser}}
			repo := &modelAccessAuthRepo{fakeAPIKeyRepo: fakeAPIKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
			router := gin.New()
			if google {
				router.Use(APIKeyAuthGoogle(keys, cfg))
			} else {
				router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, nil, cfg)))
			}
			account := &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Extra: map[string]any{service.UpstreamModelCatalogExtraKey: &service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"public"}, EligibleModels: []service.AccountCatalogModel{{ID: "public", Endpoints: []string{"responses"}, EvidenceKind: "native_account_catalog"}}}}}
			paths := []string{"/v1/models", "/v1/messages", "/v1/responses", "/v1/chat/completions", "/v1/images/generations", "/v1/embeddings", "/v1/audio/speech", "/v1/realtime", "/v1beta/models", "/v1beta/models/public:generateContent"}
			for _, path := range paths {
				router.GET(path, func(ctx *gin.Context) {
					binding := service.ResolveAccountModelBinding(ctx.Request.Context(), account, "public", service.AccountModelBindingOptions{Endpoint: "responses"})
					if !binding.Supported {
						ctx.Status(403)
						return
					}
					ctx.Status(200)
				})
			}
			for _, policy := range [][]string{nil, {}, {"public"}, {"other"}} {
				repo.policy = policy
				want := 403
				if policy == nil || len(policy) == 1 && policy[0] == "public" {
					want = 200
				}
				for _, path := range paths {
					request := httptest.NewRequest(http.MethodGet, path, nil)
					request.Header.Set("x-goog-api-key", key.Key)
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					require.Equal(t, want, response.Code, path)
				}
			}
			repo.policyErr = errors.New("unavailable")
			request := httptest.NewRequest(http.MethodGet, paths[0], nil)
			request.Header.Set("x-goog-api-key", key.Key)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, 503, response.Code)
		})
	}
}
