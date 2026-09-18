package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type modelCatalogAllowlistRepository struct {
	service.AccountRepository
	calls    int
	baseline service.AccountModelCatalogBaseline
}

func (r *modelCatalogAllowlistRepository) UpdateAccountModelAllowlist(_ context.Context, _ int64, baseline service.AccountModelCatalogBaseline, _ service.AccountModelAllowlist) error {
	r.calls++
	r.baseline = baseline
	return nil
}

func TestModelCatalogAllowlistConfirmationHTTP(t *testing.T) {
	for _, fixture := range []struct {
		name, revision, models, reason string
		authenticated                  bool
		status                         int
	}{
		{"string revision", `"9007199254740993"`, `["native"]`, "confirmed", true, 200},
		{"numeric revision", `9007199254740993`, `["native"]`, "confirmed", true, 200},
		{"stale revision", `9007199254740992`, `["native"]`, "confirmed", true, 409},
		{"empty scope", `9007199254740993`, `[]`, "confirmed", true, 400},
		{"unknown member", `9007199254740993`, `["guessed"]`, "confirmed", true, 400},
		{"no reason", `9007199254740993`, `["native"]`, "", true, 400},
		{"no identity", `9007199254740993`, `["native"]`, "confirmed", false, 401},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			repository := &modelCatalogAllowlistRepository{}
			admin := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 42, Extra: map[string]any{
				service.UpstreamModelCatalogStateExtraKey: service.AccountModelCatalogState{RulesRevision: 9007199254740993},
				service.UpstreamModelCatalogExtraKey:      service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"native"}},
			}}}
			syncer := service.NewAccountTestService(repository, nil, nil, nil, nil, nil, nil, nil)
			handler := &AccountHandler{adminService: admin, accountTestService: syncer}
			router := gin.New()
			router.PUT("/accounts/:id/models/allowlist", func(c *gin.Context) {
				if fixture.authenticated {
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
				}
				handler.UpdateModelAllowlist(c)
			})
			body := `{"enabled":true,"models":` + fixture.models + `,"reason":"` + fixture.reason + `","expected_rules_revision":` + fixture.revision + `}`
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/accounts/42/models/allowlist", strings.NewReader(body)))
			require.Equal(t, fixture.status, response.Code, response.Body.String())
			if fixture.status == 200 {
				require.Equal(t, 1, repository.calls)
				require.EqualValues(t, 9007199254740993, repository.baseline.RulesRevision)
				require.Contains(t, response.Body.String(), `"rules_revision":"9007199254740994"`)
			} else {
				require.Zero(t, repository.calls)
			}
		})
	}
}

func TestModelCatalogReviewCannotApplyToAnotherAccount(t *testing.T) {
	admin := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 42, Status: service.StatusActive}}
	upstream := &syncUpstreamHTTPUpstream{}
	router := setupSyncUpstreamModelsRouter(admin, upstream)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/42/models/sync-upstream?apply=true", strings.NewReader(`{"review":{"account_id":43,"digest":"not-authority"}}`)))
	require.Equal(t, http.StatusConflict, response.Code)
	require.Empty(t, upstream.requests)
	require.Zero(t, admin.commitCalls)
}
