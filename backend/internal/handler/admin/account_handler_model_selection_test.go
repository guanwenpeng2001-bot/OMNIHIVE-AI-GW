package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// Only persistence is faked; requests execute the real handler and services.
// PostgreSQL transaction/outbox behavior is covered separately by package A.
type handlerSelectionRepository struct {
	service.AdminAccountRepository
	account  service.Account
	writes   int
	conflict bool
}

func (repo *handlerSelectionRepository) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	workspaceID, scoped := domain.WorkspaceScope(ctx)
	if id != repo.account.ID || (scoped && workspaceID != repo.account.WorkspaceID) {
		return nil, service.ErrAccountNotFound
	}
	encoded, _ := json.Marshal(repo.account)
	var account service.Account
	_ = json.Unmarshal(encoded, &account)
	return &account, nil
}
func (repo *handlerSelectionRepository) ReplaceAccountModelSelectionScope(ctx context.Context, id int64, baseline service.AccountModelCatalogBaseline, scope service.AccountModelAllowlist, mapping *map[string]string) ([]string, error) {
	if _, err := repo.GetByID(ctx, id); err != nil {
		return nil, err
	}
	if repo.conflict || baseline.RulesRevision != repo.account.ModelCatalogBaseline().RulesRevision {
		return nil, service.ErrAccountModelCatalogConflict
	}
	repo.writes++
	repo.account.Extra[service.UpstreamModelAllowlistExtraKey] = scope
	state := repo.account.ModelCatalogState()
	state.RulesRevision++
	repo.account.Extra[service.UpstreamModelCatalogStateExtraKey] = state
	if mapping != nil {
		repo.account.Credentials["model_mapping"] = *mapping
	}
	return nil, nil
}
func (repo *handlerSelectionRepository) UpdateWithAccountBillingSettings(_ context.Context, account *service.Account, _, _ *bool, _ *float64) error {
	repo.account = *account
	repo.writes++
	return nil
}
func newHandlerSelectionRouter(repo *handlerSelectionRepository, userID, workspaceID int64) *gin.Engine {
	admin := service.NewAdminService(&config.Config{}, nil, nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler := &AccountHandler{adminService: admin, accountTestService: service.NewAccountTestService(repo, nil, nil, nil, nil, nil, nil, nil)}
	router := gin.New()
	router.Use(func(ctx *gin.Context) {
		ctx.Request = ctx.Request.WithContext(domain.WithWorkspaceScope(ctx.Request.Context(), workspaceID))
		if userID > 0 {
			ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		}
	})
	router.PUT("/accounts/:id/models/allowlist", handler.UpdateModelAllowlist)
	// Disable optional discovery for ordinary update tests; it has separate tests.
	updateHandler := &AccountHandler{adminService: admin}
	router.PUT("/accounts/:id", updateHandler.Update)
	return router
}
func newHandlerSelectionRepository() *handlerSelectionRepository {
	return &handlerSelectionRepository{account: service.Account{ID: 42, WorkspaceID: 2, Name: "selected", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
		Credentials: map[string]any{"api_key": "fake", "model_mapping": map[string]string{"native": "native"}},
		Extra:       map[string]any{service.UpstreamModelCatalogStateExtraKey: service.AccountModelCatalogState{RulesRevision: 3}, service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"native"}}}}}
}
func selectionHTTPRequest(router *gin.Engine, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}
func TestModelSelectionHandlerClearRestoreAndOrdinaryUpdate(t *testing.T) {
	repo := newHandlerSelectionRepository()
	router := newHandlerSelectionRouter(repo, 7, 2)
	cases := []struct {
		body     string
		enabled  bool
		models   []string
		revision string
		warning  []string
	}{
		{`{"enabled":true,"models":["Private-ID-2026"],"expected_rules_revision":"3","model_mapping":{"PublicAlias":"Private-ID-2026"},"actor_id":999}`, true, []string{"Private-ID-2026"}, "4", []string{"Private-ID-2026"}},
		{`{"enabled":true,"models":[],"expected_rules_revision":"4","model_mapping":{}}`, true, []string{}, "5", []string{}},
		// A filter-only save cannot undo deny-all, even when its request says disabled.
		{`{"enabled":false,"models":["native"],"expected_rules_revision":"5"}`, true, []string{}, "6", []string{}},
		{`{"enabled":false,"models":["Private-ID-2026"],"expected_rules_revision":"6","model_mapping":{"PublicAlias":"Private-ID-2026"}}`, false, []string{"Private-ID-2026"}, "7", []string{"Private-ID-2026"}},
	}
	for index, scenario := range cases {
		recorder := selectionHTTPRequest(router, "/accounts/42/models/allowlist", scenario.body)
		require.Equal(t, 200, recorder.Code, recorder.Body.String())
		var payload struct {
			Data struct {
				Scope    service.AccountModelAllowlist `json:"scope"`
				Revision string                        `json:"rules_revision"`
				Warning  []string                      `json:"not_discovered"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
		require.Equal(t, scenario.enabled, payload.Data.Scope.Enabled)
		require.Equal(t, scenario.models, payload.Data.Scope.Models)
		require.Equal(t, scenario.revision, payload.Data.Revision)
		require.Equal(t, scenario.warning, payload.Data.Warning)
		require.EqualValues(t, 7, payload.Data.Scope.ConfirmedBy)
		if index == 1 {
			// Ordinary credential updates retain the existing deny-all scope.
			updated := selectionHTTPRequest(router, "/accounts/42", `{"name":"renamed","credentials":{"model_mapping":{"ordinary":"ordinary-target"}}}`)
			require.Equal(t, 200, updated.Code, updated.Body.String())
			scope := repo.account.Extra[service.UpstreamModelAllowlistExtraKey]
			encoded, _ := json.Marshal(scope)
			var saved service.AccountModelAllowlist
			require.NoError(t, json.Unmarshal(encoded, &saved))
			require.True(t, saved.Enabled)
			require.Empty(t, saved.Models)
		}
	}
	require.Equal(t, "Private-ID-2026", repo.account.GetModelMapping()["PublicAlias"])
}
func TestModelSelectionHandlerRejectsUnauthorizedAndConflictingWrites(t *testing.T) {
	for _, scenario := range []struct {
		name, body      string
		user, workspace int64
		conflict        bool
		status          int
	}{
		{"missing identity", `{"enabled":true,"models":[],"expected_rules_revision":3,"model_mapping":{}}`, 0, 2, false, 401},
		{"foreign workspace", `{"enabled":true,"models":[],"expected_rules_revision":3,"model_mapping":{}}`, 7, 9, false, 404},
		{"stale revision", `{"enabled":true,"models":[],"expected_rules_revision":2,"model_mapping":{}}`, 7, 2, false, 409},
		{"concurrent write", `{"enabled":true,"models":[],"expected_rules_revision":3,"model_mapping":{}}`, 7, 2, true, 409},
		{"contradictory selection", `{"enabled":true,"models":[],"expected_rules_revision":3,"model_mapping":{"alias":"target"}}`, 7, 2, false, 400},
		{"wildcard scope", `{"enabled":true,"models":["private-*"],"expected_rules_revision":3}`, 7, 2, false, 400},
		{"missing revision", `{"enabled":true,"models":[]}`, 7, 2, false, 400},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repo := newHandlerSelectionRepository()
			repo.conflict = scenario.conflict
			before, _ := json.Marshal(repo.account)
			recorder := selectionHTTPRequest(newHandlerSelectionRouter(repo, scenario.user, scenario.workspace), "/accounts/42/models/allowlist", scenario.body)
			require.Equal(t, scenario.status, recorder.Code, recorder.Body.String())
			require.Zero(t, repo.writes)
			after, _ := json.Marshal(repo.account)
			require.JSONEq(t, string(before), string(after))
		})
	}
}
