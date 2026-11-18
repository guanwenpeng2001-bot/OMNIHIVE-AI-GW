package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const importSafetyData = `"data":{"proxies":[],"accounts":[{"name":"fixture","platform":"openai","type":"apikey","credentials":{"api_key":"synthetic-import-key"},"proxy_dependency":"none","concurrency":1}]}`

func TestDataImportHandlerRejectsAmbiguousSwitchesAndMultipleDocuments(t *testing.T) {
	for index, body := range []string{
		`{"stage_inactive":true,"stage_inactive":false,` + importSafetyData + `}`,
		`{"stage_inactive":false,"stage_inactive":true,` + importSafetyData + `}`,
		`{"stage_inactive":false,"stage_inactive":false,` + importSafetyData + `}`,
		`{"stage_inactive":true,"STAGE_INACTIVE":false,` + importSafetyData + `}`,
		`{"stage\u005finactive":true,"stage_inactive":false,` + importSafetyData + `}`,
		`{"\u017Ftage_inactive":true,"stage_inactive":false,` + importSafetyData + `}`,
		`{"skip_default_group_bind":true,"SKIP_DEFAULT_GROUP_BIND":false,` + importSafetyData + `}`,
		`{"confirm_mixed_channel_risk":true,"CONFIRM_MIXED_CHANNEL_RISK":false,` + importSafetyData + `}`,
		`{"stage_inactive":null,` + importSafetyData + `}`,
		`{"stage_inactive":"true",` + importSafetyData + `}`,
		`{` + importSafetyData + `} {"stage_inactive":true}`,
		`{"stage_inactive":false,` + importSafetyData + `} null`,
		`{"stage_inactive":true,` + importSafetyData + `} {}`,
		`{` + importSafetyData + `} trailing`,
	} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			pending, client := credentialDraftTestClient(t)
			repo := &stagedTestRepo{createStatusRepository: &createStatusRepository{client: client}, t: t}
			admin := &stagedTestAdmin{AdminService: service.NewAdminService(nil, nil, nil, &stagedTestGroups{}, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)}
			handler := &AccountHandler{adminService: admin, credentialDrafts: pending}
			router := gin.New()
			router.POST("/api/v1/admin/accounts/data", handler.ImportData)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", strings.NewReader(body)))
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "synthetic-import-key")
			require.Zero(t, admin.proxyReads)
			require.Empty(t, repo.statuses, "ambiguous input must never reach the active create path")
			require.Zero(t, client.Account.Query().CountX(context.Background()))
		})
	}
}

func TestDataImportHandlerPreservesUnambiguousLegacyModes(t *testing.T) {
	for index, prefix := range []string{"", `"stage_inactive":false,`, `"stage_inactive":true,`, `"STAGE_INACTIVE":true,`} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			pending, client := credentialDraftTestClient(t)
			repo := &stagedTestRepo{createStatusRepository: &createStatusRepository{client: client}, t: t}
			admin := &stagedTestAdmin{AdminService: service.NewAdminService(nil, nil, nil, &stagedTestGroups{}, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)}
			handler := &AccountHandler{adminService: admin, credentialDrafts: pending}
			router := gin.New()
			router.POST("/api/v1/admin/accounts/data", handler.ImportData)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", strings.NewReader("{"+prefix+importSafetyData+"} \n\t")))
			if index >= 2 {
				// Durable staged imports require an authenticated actor. An
				// anonymous request must never fall back to legacy creation.
				require.Equal(t, http.StatusUnauthorized, recorder.Code, recorder.Body.String())
				require.Empty(t, repo.statuses)
				require.Zero(t, client.Account.Query().CountX(context.Background()))
				return
			}
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Equal(t, []string{service.StatusActive}, repo.statuses)
			require.Equal(t, service.StatusActive, client.Account.Query().OnlyX(context.Background()).Status)
		})
	}
}

func TestDataImportHandlerRejectsCursorSelectorDedupBypass(t *testing.T) {
	for _, field := range []string{"tenant_id", "project_id", "api_base_urls", "id_token", "session_key", "aws_secret_access_key"} {
		t.Run(field, func(t *testing.T) {
			pending, client := credentialDraftTestClient(t)
			repo := &stagedTestRepo{createStatusRepository: &createStatusRepository{client: client}, t: t}
			admin := &stagedTestAdmin{AdminService: service.NewAdminService(nil, nil, nil, &stagedTestGroups{}, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)}
			handler := &AccountHandler{adminService: admin, credentialDrafts: pending}
			first := DataAccount{Name: "first", Platform: service.PlatformCursor, Type: service.AccountTypeAPIKey, ProxyDependency: "unknown", Credentials: map[string]any{"api_key": "shared-cursor-key"}}
			second := first
			second.Name, second.Credentials = "second", map[string]any{"api_key": "shared-cursor-key", field: "unused-identity"}
			if field == "api_base_urls" {
				second.Credentials[field] = map[string]any{"chat_completions": "https://ignored.example.invalid"}
			}
			body, err := json.Marshal(DataImportRequest{StageInactive: true, Data: DataPayload{Proxies: []DataProxy{}, Accounts: []DataAccount{first, second}}})
			require.NoError(t, err)
			router := gin.New()
			router.POST("/api/v1/admin/accounts/data", handler.ImportData)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", strings.NewReader(string(body))))
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Empty(t, repo.statuses)
			require.Zero(t, client.Account.Query().CountX(context.Background()))
		})
	}
}

func TestDataImportHandlerRejectsNestedStructAliases(t *testing.T) {
	account := `{"name":"fixture","platform":"openai","type":"apikey","credentials":{"api_key":"synthetic-key"},"proxy_dependency":"required"}`
	for index, data := range []string{
		`{"proxies":[],"accounts":[` + strings.Replace(account, `"proxy_dependency":"required"`, `"proxy_dependency":"required","PROXY_DEPENDENCY":"none"`, 1) + `]}`,
		`{"proxies":[],"accounts":[` + strings.Replace(account, `"proxy_dependency":"required"`, `"proxy_dependency":"required","proxy_dependency":"none"`, 1) + `]}`,
		`{"proxies":[],"accounts":[` + strings.Replace(account, `"proxy_dependency":"required"`, `"proxy_\u0064ependency":"required","PROXY_DEPENDENCY":"none"`, 1) + `]}`,
		`{"proxies":[],"accounts":[` + strings.Replace(account, `"proxy_dependency":"required"`, `"proxy_dependency":"required","proxy_key":"external","PROXY_KEY":null`, 1) + `]}`,
		`{"proxies":[],"accounts":[` + strings.Replace(account, `"credentials":{"api_key":"synthetic-key"}`, `"credentials":{"api_key":"synthetic-key"},"CREDENTIALS":{"api_key":"different"}`, 1) + `]}`,
		`{"proxies":[],"accounts":[],"ACCOUNTS":[` + account + `]}`,
		`{"proxies":[],"PROXIES":[],"accounts":[` + account + `]}`,
	} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			router, admin := setupAccountDataRouter(t)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", strings.NewReader(`{"stage_inactive":true,"data":`+data+`}`)))
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Empty(t, admin.createdAccounts)
			require.Empty(t, admin.createdProxies)
			require.NotContains(t, recorder.Body.String(), "synthetic-key")
		})
	}
	var decoded DataImportRequest
	require.Error(t, json.Unmarshal([]byte(`{"stage_inactive":true,"data":{"proxies":[],"accounts":[]},"DATA":{"proxies":[],"accounts":[]}}`), &decoded))
}

func TestDataImportHandlerPreservesModelDictionaryCase(t *testing.T) {
	pending, client := credentialDraftTestClient(t)
	repo := &stagedTestRepo{createStatusRepository: &createStatusRepository{client: client}, t: t}
	admin := &stagedTestAdmin{AdminService: service.NewAdminService(nil, nil, nil, &stagedTestGroups{}, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)}
	handler := &AccountHandler{adminService: admin, credentialDrafts: pending}
	router := gin.New()
	router.POST("/api/v1/admin/accounts/data", handler.ImportData)
	body := `{"stage_inactive":true,"DaTa":{"proxies":[],"AcCoUnTs":[{"name":"fixture","platform":"openai","type":"apikey","credentials":{"api_key":"synthetic-key","model_mapping":{"Model":"target-A","model":"target-B","PROXY_DEPENDENCY":"target-C","proxy_dependency":"target-D"}},"PROXY_DEPENDENCY":"required"}]}}`
	var decoded DataImportRequest
	require.NoError(t, json.Unmarshal([]byte(body), &decoded))
	require.NoError(t, validateStagedDataImport(decoded))
	require.Equal(t, "required", decoded.Data.Accounts[0].ProxyDependency)
	require.Equal(t, map[string]any{"Model": "target-A", "model": "target-B", "PROXY_DEPENDENCY": "target-C", "proxy_dependency": "target-D"}, decoded.Data.Accounts[0].Credentials["model_mapping"])
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", strings.NewReader(body)))
	// This fixture lacks an authenticated actor; the parser still preserves
	// case but the staged handler correctly refuses creation.
	require.Equal(t, http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	require.Zero(t, client.Account.Query().CountX(context.Background()))
}
