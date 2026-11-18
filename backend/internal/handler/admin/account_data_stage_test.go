package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type stagedTestGroups struct {
	service.AdminGroupRepository
	lookups int
}

func (g *stagedTestGroups) ListActiveByPlatform(_ context.Context, platform string) ([]service.Group, error) {
	g.lookups++
	return []service.Group{{ID: 9, Name: platform + "-default"}}, nil
}

type stagedTestRepo struct {
	*createStatusRepository
	t        *testing.T
	statuses []string
	groups   [][]service.AccountGroup
	failAt   int
}

func (r *stagedTestRepo) ListByGroup(context.Context, int64) ([]service.Account, error) {
	return nil, nil
}

func (r *stagedTestRepo) CreateWithAccountGroups(ctx context.Context, account *service.Account, groups []service.AccountGroup) error {
	require.NotNil(r.t, dbent.TxFromContext(ctx), "all rows must share the import transaction")
	r.statuses = append(r.statuses, account.Status)
	r.groups = append(r.groups, groups)
	if service.IsStagedAccountImport(ctx) {
		require.Equal(r.t, "inactive", account.Status, "observe the first write, not a later status update")
		require.False(r.t, account.Schedulable)
	}
	if len(r.statuses) == r.failAt {
		return errors.New("driver error contains test-only-secret")
	}
	return r.Create(ctx, account)
}

type stagedTestAdmin struct {
	service.AdminService
	proxyReads int
	privacy    chan struct{}
}

func (s *stagedTestAdmin) ListProxies(context.Context, int, int, string, string, string, string, string) ([]service.Proxy, int64, error) {
	s.proxyReads++
	return nil, 0, nil
}
func (s *stagedTestAdmin) ForceAntigravityPrivacy(context.Context, *service.Account) string {
	s.privacy <- struct{}{}
	return ""
}

func stagedRequest() DataImportRequest {
	return DataImportRequest{StageInactive: true, Data: DataPayload{Proxies: []DataProxy{}, Accounts: []DataAccount{
		{Name: "oauth", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "test-only-secret", "refresh_token": "test-only-refresh"}, ProxyDependency: "required", Concurrency: 1},
	}}}
}

func TestDataImportStagingAndLegacyCreationPolicy(t *testing.T) {
	for _, staged := range []bool{false, true} {
		for _, skipDefault := range []bool{false, true} {
			t.Run(fmt.Sprintf("staged=%t/skip_default=%t", staged, skipDefault), func(t *testing.T) {
				pending, client := credentialDraftTestClient(t)
				groups := &stagedTestGroups{}
				repo := &stagedTestRepo{createStatusRepository: &createStatusRepository{client: client}, t: t}
				admin := &stagedTestAdmin{AdminService: service.NewAdminService(nil, nil, nil, groups, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil), privacy: make(chan struct{}, 1)}
				handler := &AccountHandler{adminService: admin, credentialDrafts: pending}
				req := stagedRequest()
				req.StageInactive, req.SkipDefaultGroupBind = staged, &skipDefault
				result, err := handler.importData(context.Background(), req)
				require.NoError(t, err)
				require.Equal(t, 1, result.AccountCreated)
				stored := client.Account.Query().OnlyX(context.Background())
				if staged {
					require.Equal(t, "inactive", stored.Status)
					require.False(t, stored.Schedulable)
					require.Equal(t, "required", stored.Extra[service.StagedProxyDependencyExtraKey])
					require.Zero(t, admin.proxyReads)
					require.Zero(t, groups.lookups)
					require.Empty(t, repo.groups[0])
					select {
					case <-admin.privacy:
						t.Fatal("staging scheduled privacy")
					case <-time.After(20 * time.Millisecond):
					}
				} else {
					require.Equal(t, service.StatusActive, stored.Status)
					require.True(t, stored.Schedulable)
					require.Equal(t, 1, admin.proxyReads)
					if !skipDefault {
						require.Equal(t, int64(9), repo.groups[0][0].GroupID)
					}
					select {
					case <-admin.privacy:
					case <-time.After(time.Second):
						t.Fatal("legacy privacy callback missing")
					}
				}
			})
		}
	}
}

func TestStagedDataImportFailureRollsBackAndRedacts(t *testing.T) {
	pending, client := credentialDraftTestClient(t)
	repo := &stagedTestRepo{createStatusRepository: &createStatusRepository{client: client}, t: t, failAt: 2}
	admin := &stagedTestAdmin{AdminService: service.NewAdminService(nil, nil, nil, &stagedTestGroups{}, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil), privacy: make(chan struct{}, 1)}
	handler := &AccountHandler{adminService: admin, credentialDrafts: pending}
	req := stagedRequest()
	second := req.Data.Accounts[0]
	second.Name, second.Credentials = "second", map[string]any{"access_token": "different-test-secret"}
	req.Data.Accounts = append(req.Data.Accounts, second)
	result, err := handler.importData(context.Background(), req)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "test-only-secret")
	require.Zero(t, result.AccountCreated)
	require.Equal(t, []string{"inactive", "inactive"}, repo.statuses)
	require.Zero(t, client.Account.Query().CountX(context.Background()))
	require.Empty(t, admin.privacy)
}

func TestStagedDataImportRejectsUnsafeRequestBeforeWrites(t *testing.T) {
	for _, field := range []string{"proxy_key", "proxy_id", "proxy_group_id", "group_ids", "groups", "credential_ref", "master_key", "extra", "credentials", "proxy_dependency", "status"} {
		t.Run(field, func(t *testing.T) {
			router, admin := setupAccountDataRouter(t)
			body, err := json.Marshal(stagedRequest())
			require.NoError(t, err)
			var document map[string]any
			require.NoError(t, json.Unmarshal(body, &document))
			account := document["data"].(map[string]any)["accounts"].([]any)[0].(map[string]any)
			account[field] = "test-only-secret"
			body, err = json.Marshal(document)
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "test-only-secret")
			require.Empty(t, admin.createdAccounts)
			require.Empty(t, admin.createdProxies)
		})
	}
	for _, mutate := range []func(*DataImportRequest){
		func(r *DataImportRequest) { r.Data.Proxies = []DataProxy{{ProxyKey: "external-secret"}} },
		func(r *DataImportRequest) { r.Data.Accounts = append(r.Data.Accounts, r.Data.Accounts[0]) },
		func(r *DataImportRequest) { r.Data.Accounts[0].Credentials["credential_ref"] = "source-ref" },
		func(r *DataImportRequest) {
			r.Data.Accounts[0].Credentials[service.CredentialSecretReferenceKey] = "source-ref"
		},
		func(r *DataImportRequest) { r.Data.Accounts[0].ProxyDependency = "" },
	} {
		req := stagedRequest()
		mutate(&req)
		require.Error(t, validateStagedDataImport(req))
	}
}

type stagedNetworkSentinel struct {
	*stubAdminService
	privacy chan struct{}
	probes  chan struct{}
}

func (s *stagedNetworkSentinel) CreateAccount(_ context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	// Deliberately return active to prove the staging branch never registers
	// network callbacks, independently of their own status checks.
	return &service.Account{ID: 123, Platform: input.Platform, Type: input.Type, Status: service.StatusActive}, nil
}
func (s *stagedNetworkSentinel) ForceAntigravityPrivacy(context.Context, *service.Account) string {
	s.privacy <- struct{}{}
	return ""
}
func (s *stagedNetworkSentinel) QueryQuota(context.Context, int64) (*service.GrokQuotaProbeResult, error) {
	s.probes <- struct{}{}
	return nil, nil
}

func TestStagedDataImportNeverSchedulesGrokOrPrivacy(t *testing.T) {
	pending, _ := credentialDraftTestClient(t)
	sentinel := &stagedNetworkSentinel{stubAdminService: newStubAdminService(), privacy: make(chan struct{}, 1), probes: make(chan struct{}, 1)}
	h := &AccountHandler{adminService: sentinel, credentialDrafts: pending, grokImportProber: sentinel}
	req := stagedRequest()
	second := req.Data.Accounts[0]
	second.Name, second.Platform = "grok", service.PlatformGrok
	req.Data.Accounts = append(req.Data.Accounts, second)
	_, err := h.importData(context.Background(), req)
	require.NoError(t, err)
	select {
	case <-sentinel.privacy:
		t.Fatal("staging scheduled an Antigravity privacy request")
	case <-sentinel.probes:
		t.Fatal("staging scheduled a Grok probe")
	case <-time.After(30 * time.Millisecond):
	}
}

func TestDataImportLegacyDecoderRemainsPermissive(t *testing.T) {
	for _, flag := range []string{"", `"stage_inactive":false,`} {
		var req DataImportRequest
		err := json.Unmarshal([]byte(`{`+flag+`"legacy_unknown_field":true,"data":{"proxies":[],"accounts":[],"groups":[1]}}`), &req)
		require.NoError(t, err)
		require.False(t, req.StageInactive)
	}
}

func TestStagedDataImportCannotBypassTransactionPolicy(t *testing.T) {
	_, err := (&AccountHandler{}).stageDataAccounts(context.Background(), stagedRequest())
	require.Error(t, err)
	_, err = (&AccountHandler{}).stageDataAccounts(service.WithStagedAccountImport(context.Background()), stagedRequest())
	require.Error(t, err)
}

func TestStagedDataImportSharedKaggleKeyPreservesModels(t *testing.T) {
	pending, client := credentialDraftTestClient(t)
	repo := &stagedTestRepo{createStatusRepository: &createStatusRepository{client: client}, t: t}
	groups := &stagedTestGroups{}
	admin := &stagedTestAdmin{AdminService: service.NewAdminService(nil, nil, nil, groups, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil), privacy: make(chan struct{}, 1)}
	handler := &AccountHandler{adminService: admin, credentialDrafts: pending}
	req := DataImportRequest{StageInactive: true, Data: DataPayload{Proxies: []DataProxy{}}}
	for i := 0; i < 3; i++ {
		req.Data.Accounts = append(req.Data.Accounts, DataAccount{
			Name: "Kaggle", Platform: "kaggle", Type: service.AccountTypeAPIKey, ProxyDependency: "unknown",
			Credentials: map[string]any{"api_key": "shared-fixture-key", "base_url": fmt.Sprintf("https://notebook-%d.example.invalid", i), "account_mode": "payg", "api_protocol": "chat_completions", "model_mapping": map[string]any{"alias": "Qwen/Qwen3.5-9B"}},
			Extra:       map[string]any{"custom_base_url_enabled": true},
		})
	}
	result, err := handler.importData(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 3, result.AccountCreated)
	stored := client.Account.Query().AllX(context.Background())
	require.Len(t, stored, 3)
	for _, account := range stored {
		require.Equal(t, "inactive", account.Status)
		require.Equal(t, map[string]any{"alias": "Qwen/Qwen3.5-9B"}, account.Credentials["model_mapping"])
		require.Equal(t, "payg", account.Credentials["account_mode"])
		require.Equal(t, "chat_completions", account.Credentials["api_protocol"])
		require.Equal(t, true, account.Extra["custom_base_url_enabled"])
		require.Equal(t, "unknown", account.Extra[service.StagedProxyDependencyExtraKey])
	}
	require.Zero(t, groups.lookups)
	require.Zero(t, admin.proxyReads)
	duplicate := req.Data.Accounts[0]
	duplicate.Name = "Renamed with different models"
	duplicate.Credentials = map[string]any{"api_key": "shared-fixture-key", "base_url": "HTTPS://NOTEBOOK-0.EXAMPLE.INVALID:443/v1/", "model_mapping": map[string]any{"different": "other"}}
	req.Data.Accounts = append(req.Data.Accounts, duplicate)
	_, err = handler.importData(context.Background(), req)
	require.Error(t, err)
	require.Len(t, repo.statuses, 3, "duplicates must be rejected before any new write")
}
