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
// PostgreSQL transaction and outbox behavior are covered by the HTTP regression.
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
func (repo *handlerSelectionRepository) UpdateModelSelection(ctx context.Context, id int64, _ service.ModelSelectionSource, resolve func(*service.Account) (service.ModelSelectionResult, error)) (service.ModelSelectionResult, error) {
	account, err := repo.GetByID(ctx, id)
	if err != nil {
		return service.ModelSelectionResult{}, err
	}
	if repo.conflict {
		return service.ModelSelectionResult{}, service.ErrModelSelectionConflict
	}
	result, err := resolve(account)
	if err == nil && result.Changed {
		applySelectionFixtureResult(&repo.account, result)
		repo.writes++
	}
	return result, err
}

func (repo *handlerSelectionRepository) UpdateWithAccountBillingSettings(ctx context.Context, account *service.Account, _, _ *bool, _ *float64) error {
	if merged, ok := service.MergeAccountCredentialChanges(ctx, repo.account.Credentials); ok {
		account.Credentials = merged
	}
	if cmd := account.ModelSelectionCommand; cmd != nil {
		result, err := service.ResolveLockedModelSelection(&repo.account, *cmd)
		if err != nil {
			return err
		}
		if result.Changed {
			applySelectionFixtureResult(account, result)
		}
		account.ModelSelectionResult = &result
	}
	repo.account = *account
	repo.writes++
	return nil
}

func applySelectionFixtureResult(account *service.Account, result service.ModelSelectionResult) {
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	state := account.ModelCatalogState()
	state.RulesRevision = result.RulesRevision
	account.Extra[service.UpstreamModelAllowlistExtraKey] = result.Scope
	account.Extra[service.UpstreamModelCatalogStateExtraKey] = state
	if result.MappingChanged {
		if account.Credentials == nil {
			account.Credentials = map[string]any{}
		}
		account.Credentials["model_mapping"] = result.ModelMapping
	}
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
		{`{"enabled":true,"models":[],"expected_rules_revision":"4","confirm_deny_all":true,"reason":"maintenance"}`, true, []string{}, "5", []string{}},
		// A nonempty legacy range restores restricted mode even with enabled=false.
		{`{"enabled":false,"models":["native"],"expected_rules_revision":"5"}`, true, []string{"Private-ID-2026", "native"}, "6", []string{"Private-ID-2026"}},
		{`{"mode":"inherit","enabled":false,"models":[],"expected_rules_revision":"6"}`, false, []string{}, "7", []string{}},
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
			// A mapping-only write cannot change a denied account.
			updated := selectionHTTPRequest(router, "/accounts/42", `{"name":"renamed","credentials":{"model_mapping":{"ordinary":"ordinary-target"}}}`)
			require.Equal(t, 409, updated.Code, updated.Body.String())
			require.Contains(t, updated.Body.String(), `"reason":"MODEL_SELECTION_DENIED"`)
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
		{"foreign workspace", `{"mode":"restricted","models":["native"],"expected_rules_revision":3}`, 7, 9, false, 404},
		{"stale revision", `{"mode":"restricted","models":["native"],"expected_rules_revision":2}`, 7, 2, false, 409},
		{"concurrent write", `{"mode":"restricted","models":["native"],"expected_rules_revision":3}`, 7, 2, true, 409},
		{"contradictory selection", `{"mode":"deny_all","reason":"maintenance","models":[],"expected_rules_revision":3,"model_mapping":{"alias":"target"}}`, 7, 2, false, 400},
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

type accountSelectionAdminStub struct {
	*stubAdminService
	updated *service.Account
	bulk    *service.BulkUpdateAccountsResult
}

func (s *accountSelectionAdminStub) UpdateAccount(ctx context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	_, err := s.stubAdminService.UpdateAccount(ctx, id, input)
	return s.updated, err
}

func (s *accountSelectionAdminStub) BulkUpdateAccounts(ctx context.Context, input *service.BulkUpdateAccountsInput) (*service.BulkUpdateAccountsResult, error) {
	_, err := s.stubAdminService.BulkUpdateAccounts(ctx, input)
	return s.bulk, err
}

func TestAccountModelSelectionWriteResponse(t *testing.T) {
	revision := int64(9007199254740993)
	admin := &accountSelectionAdminStub{
		stubAdminService: newStubAdminService(),
		updated: &service.Account{
			ID: 42, Credentials: map[string]any{"api_key": "test-redacted-placeholder"},
			Extra: map[string]any{
				service.UpstreamModelCatalogStateExtraKey:  service.AccountModelCatalogState{RulesRevision: revision + 1},
				service.ModelSelectionRepairBackupExtraKey: map[string]any{"scope": "private backup"},
			},
			ModelSelectionResult: &service.ModelSelectionResult{AutoAdded: []string{"target"}, RulesRevision: revision},
		},
		bulk: &service.BulkUpdateAccountsResult{
			Success: 3, SuccessIDs: []int64{42, 43, 44},
			Results: []service.BulkUpdateAccountResult{
				{AccountID: 42, Success: true, AutoAdded: []string{"target"}, RulesRevision: &revision},
				{AccountID: 43, Success: true, RulesRevision: &revision},
				{AccountID: 44, Success: true},
			},
		},
	}
	handler := &AccountHandler{adminService: admin}
	router := gin.New()
	router.PUT("/accounts/:id", handler.Update)
	router.POST("/accounts/bulk-update", handler.BulkUpdate)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/accounts/42", strings.NewReader(
		`{"credentials":{"model_mapping":{"alias":"target"}}}`)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"rules_revision":"9007199254740993"`)
	require.Contains(t, recorder.Body.String(), `"auto_added":["target"]`)
	require.NotContains(t, recorder.Body.String(), "test-redacted-placeholder")
	require.NotContains(t, recorder.Body.String(), service.ModelSelectionRepairBackupExtraKey)
	require.True(t, admin.lastUpdateAccountInput.PatchCredentials)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/accounts/bulk-update", strings.NewReader(
		`{"account_ids":[42,43,44],"credentials":{"model_mapping":{"alias":"target"}}}`)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var reply struct {
		Data struct {
			SuccessIDs []int64           `json:"success_ids"`
			Results    []json.RawMessage `json:"results"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &reply))
	require.Equal(t, []int64{42, 43, 44}, reply.Data.SuccessIDs)
	require.Len(t, reply.Data.Results, 3)
	require.JSONEq(t, `{"account_id":42,"success":true,"auto_added":["target"],"rules_revision":"9007199254740993"}`, string(reply.Data.Results[0]))
	require.JSONEq(t, `{"account_id":43,"success":true,"auto_added":[],"rules_revision":"9007199254740993"}`, string(reply.Data.Results[1]))
	require.JSONEq(t, `{"account_id":44,"success":true}`, string(reply.Data.Results[2]))
}

func TestAccountModelSelectionBatchCredentialPatch(t *testing.T) {
	admin := newStubAdminService()
	accountCredentials := map[string]any{"model_mapping": map[string]any{"alias": "snapshot"}, "base_url": "https://upstream.example"}
	admin.getAccountResult = &service.Account{ID: 42, Credentials: accountCredentials}
	handler := &AccountHandler{adminService: admin}
	router := gin.New()
	router.POST("/accounts/batch-update-credentials", handler.BatchUpdateCredentials)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/accounts/batch-update-credentials", strings.NewReader(
		`{"account_ids":[42],"field":"account_uuid","value":null}`)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, map[string]any{"account_uuid": nil}, admin.lastUpdateAccountInput.Credentials)
	require.True(t, admin.lastUpdateAccountInput.PatchCredentials)
	require.True(t, admin.lastUpdateAccountInput.PreserveModelSelection)
	require.NotContains(t, accountCredentials, "account_uuid")
}

func TestAccountModelSelectionDraftMappingPresence(t *testing.T) {
	stale := map[string]any{"access_token": "fresh", "model_mapping": map[string]any{"alias": "stale"}}
	for _, options := range []map[string]any{nil, {"model_mapping": nil}} {
		require.Equal(t, map[string]any{"access_token": "fresh"}, existingAccountDraftCredentials(stale, options))
		require.Contains(t, stale, "model_mapping")
	}
	cleared := map[string]any{"access_token": "fresh", "model_mapping": map[string]any{}}
	require.Equal(t, cleared, existingAccountDraftCredentials(cleared, map[string]any{"model_mapping": map[string]any{}}))
}

func TestAccountModelSelectionCatalogDerivation(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		scope  service.AccountModelAllowlist
		mode   string
		models string
	}{
		{name: "restricted", scope: service.AccountModelAllowlist{Enabled: true, Models: []string{"target"}}, mode: "restricted", models: `["target"]`},
		{name: "explicit deny", scope: service.AccountModelAllowlist{Enabled: true, ConfirmedBy: 7, Reason: "maintenance"}, mode: "deny_all", models: `[]`},
		{name: "legacy upgrade freeze", scope: service.AccountModelAllowlist{Enabled: true, Reason: "system_upgrade_baseline_freeze"}, mode: "inherit", models: `[]`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			admin := newStubAdminService()
			admin.getAccountResult = &service.Account{
				ID: 42, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"model_mapping": map[string]any{"alias": "target"}},
				Extra:       map[string]any{service.UpstreamModelAllowlistExtraKey: fixture.scope},
			}
			handler := &AccountHandler{adminService: admin}
			router := gin.New()
			router.GET("/accounts/:id/models/catalog", handler.GetModelCatalog)
			router.GET("/accounts/:id/models", handler.GetAvailableModels)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/accounts/42/models/catalog", nil))
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), `"mode":"`+fixture.mode+`"`)
			require.Contains(t, recorder.Body.String(), `"models":`+fixture.models)
			require.Contains(t, recorder.Body.String(), `"model_mapping":{"alias":"target"}`)
			if fixture.mode == "deny_all" {
				for _, suffix := range []string{"", "?view=supported"} {
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/accounts/42/models"+suffix, nil))
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					require.JSONEq(t, `{"code":0,"message":"success","data":[]}`, recorder.Body.String())
				}
			}
		})
	}
}
