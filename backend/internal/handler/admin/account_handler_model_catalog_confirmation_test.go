package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type modelCatalogSelectionRepository struct {
	service.AccountRepository
	account *service.Account
	writes  int
}

func (r *modelCatalogSelectionRepository) UpdateModelSelection(_ context.Context, _ int64, _ service.ModelSelectionSource, resolve func(*service.Account) (service.ModelSelectionResult, error)) (service.ModelSelectionResult, error) {
	result, err := resolve(r.account)
	if err == nil && result.Changed {
		r.writes++
	}
	return result, err
}

func TestModelCatalogAllowlistConfirmationHTTP(t *testing.T) {
	const revision = `"expected_rules_revision":"9007199254740993"`
	for _, fixture := range []struct {
		name       string
		body       string
		wantStatus int
		wantReason string
		wantMode   service.ModelSelectionMode
		wantModels []string
		wantAdded  []string
		wantMap    map[string]string
	}{
		{
			name: "explicit mode with quoted revision", body: `{"mode":"restricted","models":["native"],` + revision + `}`,
			wantStatus: 200, wantMode: service.ModelSelectionRestricted,
			wantModels: []string{"mapped", "native"}, wantAdded: []string{"mapped"}, wantMap: map[string]string{"alias": "mapped"},
		},
		{
			name:       "legacy models override false enabled and null retains mapping",
			body:       `{"enabled":false,"models":["native"],"model_mapping":null,"expected_rules_revision":9007199254740993}`,
			wantStatus: 200, wantMode: service.ModelSelectionRestricted,
			wantModels: []string{"mapped", "native"}, wantAdded: []string{"mapped"}, wantMap: map[string]string{"alias": "mapped"},
		},
		{
			name: "empty object clears the mapping", body: `{"mode":"restricted","models":["native"],"model_mapping":{},` + revision + `}`,
			wantStatus: 200, wantMode: service.ModelSelectionRestricted,
			wantModels: []string{"native"}, wantAdded: []string{}, wantMap: map[string]string{},
		},
		{
			name: "explicit inherit", body: `{"mode":"inherit","enabled":false,"models":[],` + revision + `}`,
			wantStatus: 200, wantMode: service.ModelSelectionInherit,
			wantModels: []string{}, wantAdded: []string{}, wantMap: map[string]string{"alias": "mapped"},
		},
		{
			name: "legacy omitted enabled inherits", body: `{"models":[],` + revision + `}`,
			wantStatus: 200, wantMode: service.ModelSelectionInherit,
			wantModels: []string{}, wantAdded: []string{}, wantMap: map[string]string{"alias": "mapped"},
		},
		{
			name: "explicit deny with reason", body: `{"mode":"deny_all","reason":" maintenance ",` + revision + `}`,
			wantStatus: 200, wantMode: service.ModelSelectionDenyAll,
			wantModels: []string{}, wantAdded: []string{}, wantMap: map[string]string{"alias": "mapped"},
		},
		{
			name:       "compatibility deny with confirmation and reason",
			body:       `{"enabled":true,"models":[],"confirm_deny_all":true,"reason":"maintenance",` + revision + `}`,
			wantStatus: 200, wantMode: service.ModelSelectionDenyAll,
			wantModels: []string{}, wantAdded: []string{}, wantMap: map[string]string{"alias": "mapped"},
		},
		{
			name: "stale revision", body: `{"enabled":true,"models":["native"],"expected_rules_revision":"9007199254740992"}`,
			wantStatus: 409, wantReason: "MODEL_SELECTION_CONFLICT",
		},
		{
			name: "legacy empty map is not confirmation", body: `{"enabled":true,"models":[],"model_mapping":{},"reason":"maintenance",` + revision + `}`,
			wantStatus: 400, wantReason: "MODEL_SELECTION_CONFIRMATION_REQUIRED",
		},
		{
			name: "legacy confirmation missing reason", body: `{"enabled":true,"models":[],"confirm_deny_all":true,"reason":" ",` + revision + `}`,
			wantStatus: 400, wantReason: "MODEL_SELECTION_REASON_REQUIRED",
		},
		{
			name: "explicit deny missing reason", body: `{"mode":"deny_all","models":[],` + revision + `}`,
			wantStatus: 400, wantReason: "MODEL_SELECTION_REASON_REQUIRED",
		},
		{
			name: "enabled conflicts with explicit mode", body: `{"mode":"inherit","enabled":true,` + revision + `}`,
			wantStatus: 400, wantReason: "INVALID_MODEL_SELECTION",
		},
		{
			name: "explicit inherit conflicts with scope", body: `{"mode":"inherit","models":["native"],` + revision + `}`,
			wantStatus: 400, wantReason: "INVALID_MODEL_SELECTION",
		},
		{
			name: "explicit deny conflicts with mapping", body: `{"mode":"deny_all","reason":"maintenance","model_mapping":{"alias":"native"},` + revision + `}`,
			wantStatus: 400, wantReason: "INVALID_MODEL_SELECTION",
		},
		{
			name: "unknown mode", body: `{"mode":"open",` + revision + `}`,
			wantStatus: 400, wantReason: "INVALID_MODEL_SELECTION",
		},
		{
			name: "no revision", body: `{"enabled":true,"models":["native"]}`,
			wantStatus: 400, wantReason: "INVALID_MODEL_SELECTION",
		},
		{
			name: "out of bounds revision", body: `{"mode":"inherit","expected_rules_revision":"9223372036854775807"}`,
			wantStatus: 400, wantReason: "INVALID_MODEL_SELECTION",
		},
		{
			name: "unauthorized", body: `{"mode":"inherit",` + revision + `}`,
			wantStatus: 401, wantReason: "UNAUTHORIZED",
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			repository := &modelCatalogSelectionRepository{account: &service.Account{
				ID: 42, Credentials: map[string]any{"model_mapping": map[string]string{"alias": "mapped"}},
				Extra: map[string]any{
					service.UpstreamModelAllowlistExtraKey:    service.AccountModelAllowlist{Enabled: true, Models: []string{"before"}},
					service.UpstreamModelCatalogStateExtraKey: service.AccountModelCatalogState{RulesRevision: 9007199254740993},
					service.UpstreamModelCatalogExtraKey:      service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"native"}},
				},
			}}
			syncer := service.NewAccountTestService(repository, nil, nil, nil, nil, nil, nil, nil)
			handler := &AccountHandler{accountTestService: syncer}
			router := gin.New()
			router.PUT("/accounts/:id/models/allowlist", func(c *gin.Context) {
				if fixture.wantStatus != http.StatusUnauthorized {
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
				}
				handler.UpdateModelAllowlist(c)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/accounts/42/models/allowlist", strings.NewReader(fixture.body)))
			require.Equal(t, fixture.wantStatus, recorder.Code, recorder.Body.String())
			var reply struct {
				Reason string                       `json:"reason"`
				Data   service.ModelSelectionResult `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &reply))
			require.Equal(t, fixture.wantReason, reply.Reason)
			if fixture.wantStatus != http.StatusOK {
				require.Zero(t, repository.writes)
				return
			}
			require.Equal(t, 1, repository.writes)
			require.Equal(t, fixture.wantMode, reply.Data.Mode)
			require.Equal(t, fixture.wantModels, reply.Data.Models)
			require.Equal(t, fixture.wantModels, reply.Data.Scope.Models)
			require.Equal(t, fixture.wantAdded, reply.Data.AutoAdded)
			require.Equal(t, fixture.wantMap, reply.Data.ModelMapping)
			require.Equal(t, fixture.wantAdded, reply.Data.NotDiscovered)
			require.EqualValues(t, 7, reply.Data.Scope.ConfirmedBy)
			require.NotEmpty(t, reply.Data.Scope.ConfirmedAt)
			require.Contains(t, recorder.Body.String(), `"rules_revision":"9007199254740994"`)
			if fixture.wantMode == service.ModelSelectionDenyAll {
				require.Equal(t, "maintenance", reply.Data.Scope.Reason)
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
