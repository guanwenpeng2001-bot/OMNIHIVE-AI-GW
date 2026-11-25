package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// Extend the shared frontend fixtures with the V2 snapshot fields at the HTTP boundary.
// Keep the frontend fixtures unchanged until their consumers migrate together.
func TestGetModelCatalogContract(t *testing.T) {
	for _, scenario := range []string{"not_synced", "empty", "nonempty", "identity_invalid", "failed_history"} {
		t.Run(scenario, func(t *testing.T) {
			state := service.AccountModelCatalogState{IdentityRevision: 2, RulesRevision: 9007199254740993}
			account := service.Account{ID: 42, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Extra: map[string]any{}}
			if scenario != "not_synced" {
				snapshot := &service.AccountModelCatalogSnapshot{
					Revision: 9007199254740993, IdentityRevision: 2, Complete: true,
					ObservedAt: "2026-09-16T01:00:00Z", SourceKind: "live", ProviderProfile: "openai", DiscoveryScope: "account",
					DiscoveredModels: []string{}, EligibleModels: []service.AccountCatalogModel{}, ExcludedModels: []service.ExcludedCatalogModel{},
				}
				if scenario != "empty" {
					snapshot.DiscoveredModels = []string{"native-target", "pending-target"}
					snapshot.EligibleModels = []service.AccountCatalogModel{{ID: "native-target", Endpoints: []string{"responses"}, EvidenceKind: "account_discovery", EvidenceObservedAt: snapshot.ObservedAt}}
					snapshot.ExcludedModels = []service.ExcludedCatalogModel{{ID: "pending-target", Reason: service.CatalogReasonEntitlementUnconfirmed}}
				}
				account.Extra[service.UpstreamModelCatalogExtraKey] = snapshot
				state.Result = "success"
			}
			if scenario == "identity_invalid" {
				state.IdentityRevision = 3
				state.Result = "credentials_invalid"
				state.Error = "credentials_invalid"
			}
			if scenario == "failed_history" {
				state.Result = "failed"
				state.Error = "upstream_unavailable"
				state.AttemptedAt = "2026-09-16T02:00:00Z"
				attemptedIdentity := state.IdentityRevision
				state.AttemptedIdentityRevision = &attemptedIdentity
			}
			account.Extra[service.UpstreamModelCatalogStateExtraKey] = state
			adminSvc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: account}
			handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.GET("/api/v1/admin/accounts/:id/models/catalog", func(c *gin.Context) {
				handler.getModelCatalog(c, time.Date(2026, 9, 16, 3, 0, 0, 0, time.UTC))
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/models/catalog", nil))
			require.Equal(t, http.StatusOK, recorder.Code)
			var payload struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
			require.Equal(t, scenario != "not_synced", payload.Data["synced"])
			expectedEligible := float64(0)
			if scenario == "nonempty" || scenario == "failed_history" {
				expectedEligible = 1
			}
			require.Equal(t, expectedEligible, payload.Data["eligible_count"])
			baseline, validBaseline := payload.Data["baseline"].(map[string]any)
			require.True(t, validBaseline)
			require.Equal(t, "9007199254740993", baseline["rules_revision"])
			require.Equal(t, "9007199254740993", payload.Data["rules_revision"])
			responseState, ok := payload.Data["state"].(map[string]any)
			require.True(t, ok)
			require.NotContains(t, responseState, "attempted_identity_revision")
			require.Equal(t, payload.Data["rules_revision"], responseState["rules_revision"])
			require.Equal(t, "inherit", payload.Data["mode"])
			require.Equal(t, []any{}, payload.Data["models"])
			require.Equal(t, map[string]any{}, payload.Data["model_mapping"])
			if scenario == "identity_invalid" {
				require.Nil(t, payload.Data["effective_snapshot"])
				require.NotNil(t, payload.Data["snapshot"])
			}
			if scenario == "failed_history" {
				require.Equal(t, "upstream_unavailable", payload.Data["last_error"])
				require.NotNil(t, payload.Data["effective_snapshot"])
			}
			require.Equal(t, service.CompositeRouteEndpointAny, payload.Data["alias_endpoint"])
			require.Empty(t, payload.Data["invalid_aliases"])
			require.Len(t, payload.Data["eligible_models"], int(expectedEligible))
			// Preserve the existing frontend fixture contract while checking the
			// additive diagnostics independently; this test does not rewrite UI files.
			delete(payload.Data, "alias_endpoint")
			delete(payload.Data, "invalid_aliases")
			delete(payload.Data, "eligible_models")
			delete(payload.Data, "rules_revision")
			delete(payload.Data, "mode")
			delete(payload.Data, "models")
			delete(payload.Data, "model_mapping")
			fixturePath := filepath.Join("testdata", "model_catalog", "model-catalog-"+scenario+".json")
			fixture, err := os.ReadFile(fixturePath)
			require.NoError(t, err)
			var expected map[string]any
			require.NoError(t, json.Unmarshal(fixture, &expected))
			for _, key := range []string{"snapshot", "effective_snapshot"} {
				if snapshot, ok := expected[key].(map[string]any); ok {
					// V2 exposes the stored snapshot rules revision, including its
					// zero default for legacy snapshots, separately from the baseline.
					snapshot["rules_revision"] = "0"
				}
			}
			require.Equal(t, expected, payload.Data)
		})
	}
}

func TestModelCatalogRulesRevisionAcceptedBySyncHandler(t *testing.T) {
	adminSvc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 42}}
	router := setupSyncUpstreamModelsRouter(adminSvc, &syncUpstreamHTTPUpstream{})
	for _, testCase := range []struct {
		revision string
		status   int
	}{
		// A valid decimal is parsed losslessly, but cannot bypass the required review.
		{"9007199254740993", http.StatusBadRequest},
		{"rules/opaque", http.StatusBadRequest},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/42/models/sync-upstream?apply=true&expected_rules_revision="+testCase.revision, nil))
		require.Equal(t, testCase.status, recorder.Code)
	}
}

func TestModelCatalogSupportedReturnsAppliedEvidence(t *testing.T) {
	observed := "2026-09-16T01:00:00Z"
	account := service.Account{ID: 42, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{"public": "claude-sonnet-4-5", "invalid": "absent"}},
		Extra: map[string]any{service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{
			Complete: true, ObservedAt: observed, DiscoveredModels: []string{"claude-sonnet-4-5-20250929"},
			EligibleModels: []service.AccountCatalogModel{{ID: "claude-sonnet-4-5-20250929", Endpoints: []string{"messages"}, EvidenceKind: "native_account_catalog", EvidenceObservedAt: observed}},
		}},
	}
	adminService := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: account}
	handler := NewAccountHandler(adminService, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/api/v1/admin/accounts/:id/models", handler.GetAvailableModels)
	router.GET("/api/v1/admin/accounts/:id/models/catalog", handler.GetModelCatalog)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/models?view=supported", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var supported struct {
		Data []map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &supported))
	require.Len(t, supported.Data, 3)
	require.Equal(t, "configured_selection", supported.Data[1]["evidence_kind"])
	require.Empty(t, supported.Data[1]["evidence_observed_at"])
	require.Equal(t, "native_account_catalog", supported.Data[0]["evidence_kind"])
	require.Equal(t, observed, supported.Data[0]["evidence_observed_at"])
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/models/catalog", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var summary struct {
		Data struct {
			InvalidAliases []string                      `json:"invalid_aliases"`
			EligibleModels []service.AccountCatalogModel `json:"eligible_models"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &summary))
	require.Empty(t, summary.Data.InvalidAliases)
	require.Equal(t, observed, summary.Data.EligibleModels[0].EvidenceObservedAt)
}

func TestModelCatalogCredentialRevisionSerializedAsString(t *testing.T) {
	encoded, err := json.Marshal(modelCatalogSnapshotResponse(&service.AccountModelCatalogSnapshot{CredentialIdentityRevision: 9007199254740993}))
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"credential_identity_revision":"9007199254740993"`)
}

func TestGetModelCatalogStaleWarningPreservesMembers(t *testing.T) {
	observed := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	adminSvc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 42, Extra: map[string]any{
		service.UpstreamModelCatalogExtraKey:      &service.AccountModelCatalogSnapshot{ObservedAt: observed.Format(time.RFC3339Nano), Complete: true, DiscoveryScope: "account", EligibleModels: []service.AccountCatalogModel{{ID: "kept"}}},
		service.UpstreamModelCatalogStateExtraKey: service.AccountModelCatalogState{Result: "failed", Error: "upstream_unavailable", AttemptedAt: observed.Add(24 * time.Hour).Format(time.RFC3339Nano)},
	}}}
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/accounts/:id", func(c *gin.Context) { handler.getModelCatalog(c, observed.Add(24*time.Hour+time.Second)) })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/accounts/42", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "stale", payload.Data["staleness"])
	require.Equal(t, observed.Format(time.RFC3339Nano), payload.Data["last_success_at"])
	require.Equal(t, float64(1), payload.Data["eligible_count"])
	require.Contains(t, payload.Data["warnings"], "stale_evidence")
}

func TestGetModelCatalogValidatesCurrentShadowParent(t *testing.T) {
	for _, parentID := range []int64{7, 8} {
		parent := &service.Account{ID: parentID, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}
		adminSvc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{
			ID: 42, ParentAccountID: &parentID,
			Extra: map[string]any{service.UpstreamModelCatalogExtraKey: &service.AccountModelCatalogSnapshot{
				CredentialAccountID: 7, Complete: true, DiscoveryScope: "account",
				EligibleModels: []service.AccountCatalogModel{{ID: "native-target"}},
			}},
		}}
		adminSvc.getAccountResult = parent
		handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		router := gin.New()
		router.GET("/api/v1/admin/accounts/:id/models/catalog", handler.GetModelCatalog)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/models/catalog", nil))
		require.Equal(t, http.StatusOK, recorder.Code)
		var payload struct {
			Data struct {
				EligibleCount int `json:"eligible_count"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
		if parentID == 7 {
			require.Equal(t, 1, payload.Data.EligibleCount)
		} else {
			require.Zero(t, payload.Data.EligibleCount)
		}
	}
}

func TestModelCatalogDisplayProjection(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
	account := service.Account{ID: 42, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"alias": "private-target"}}, Extra: map[string]any{}}
	adminService := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: account}
	handler := NewAccountHandler(adminService, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.accountTestService = service.ProvideAccountTestService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, &service.OpenAIGatewayService{}, nil, nil)
	router := gin.New()
	router.GET("/accounts/:id/models/catalog", handler.GetModelCatalog)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/accounts/42/models/catalog", nil))
	require.Equal(test, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			Display map[string]service.UpstreamModelMetadata `json:"display_metadata"`
			Synced  bool                                     `json:"synced"`
			Count   int                                      `json:"eligible_count"`
		} `json:"data"`
	}
	require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Contains(test, response.Data.Display, "private-target")
	require.False(test, response.Data.Synced)
	require.Equal(test, 1, response.Data.Count)
	require.Empty(test, adminService.account.Extra)
}
