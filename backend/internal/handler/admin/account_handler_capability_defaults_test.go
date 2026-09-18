package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCapabilityDefaultsWritesRejectInvalidShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// A nil service proves validation happens before any write or service access.
	handler := NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/accounts", handler.Create)
	router.PUT("/accounts/:id", handler.Update)
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		path := "/accounts"
		if method == http.MethodPut {
			path += "/1"
		}
		request := httptest.NewRequest(method, path, strings.NewReader(`{"name":"private","platform":"openai","type":"apikey","credentials":{},"extra":{"model_capability_defaults":[]}}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Contains(t, recorder.Body.String(), "model_capability_defaults must be a JSON object")
	}
}

func TestCapabilityDefaultsPreviewRejectsInvalidJSONBeforeDiscovery(t *testing.T) {
	for _, raw := range []string{`[]`, `42`, `"secret-value"`, `true`, `{"padding":"` + strings.Repeat("x", 1<<20) + `"}`} {
		t.Run(raw[:min(len(raw), 32)], func(t *testing.T) {
			upstream := &syncUpstreamHTTPUpstream{}
			router := setupSyncUpstreamModelsRouter(newStubAdminService(), upstream)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/models/sync-upstream-preview", strings.NewReader(`{"platform":"openai","type":"apikey","model_capability_defaults":`+raw+`}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Empty(t, upstream.requests)
			require.NotContains(t, recorder.Body.String(), "secret-value")
		})
	}
}

func TestCapabilityDefaultsDraftCannotBeApplied(t *testing.T) {
	svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 48, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}}
	upstream := &syncUpstreamHTTPUpstream{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/48/models/sync-upstream?apply=true", strings.NewReader(`{"model_capability_defaults":{}}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	setupSyncUpstreamModelsRouter(svc, upstream).ServeHTTP(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "review")
	require.Empty(t, upstream.requests)
	require.Zero(t, svc.commitCalls)
}

func TestCapabilityDefaultsPreviewDoesNotMutateSavedAccount(t *testing.T) {
	extra := map[string]any{"unrelated": "preserved", "model_capability_defaults": map[string]any{"models": map[string]any{}}}
	before, err := json.Marshal(extra)
	require.NoError(t, err)
	svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{
		ID: 48, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "key", "base_url": "https://provider.example/v1"}, Extra: extra,
	}}
	upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"private-model","reasoning":false,"context_window":4096,"max_output_tokens":1024,"input_modalities":["text"]}]}`))}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/48/models/sync-upstream?apply=false", strings.NewReader(`{"model_capability_defaults":null}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	setupSyncUpstreamModelsRouter(svc, upstream).ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Zero(t, svc.commitCalls)
	after, err := json.Marshal(extra)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	require.Contains(t, recorder.Body.String(), `"applied":false`)
}

func TestCapabilityDefaultsSemanticValidation(t *testing.T) {
	wrap := func(model string) string {
		return `{"schema_version":1,"reason":"Verified deployment settings","models":{"private-id":` + model + `}}`
	}
	for _, model := range []string{
		`{"max_input_tokens":32768,"max_output_tokens":4096,"supports_function_calling":false,"reasoning":false,"reasoning_control":{"kind":"none"}}`,
		`{"reasoning":true,"reasoning_control":{"kind":"effort","effort_values":["low","high"],"default":"high"}}`,
		`{"reasoning":true,"reasoning_control":{"kind":"toggle","toggle_values":[false,true],"default":false}}`,
		`{"reasoning":true,"reasoning_control":{"kind":"budget","budget_min":0,"budget_max":2048,"default":1024}}`,
		`{"reasoning":true,"reasoning_control":{"kind":"fixed"}}`,
		`{"reasoning":true,"reasoning_control":{"kind":"adaptive","adaptive":true}}`,
		`{"reasoning_control":{"kind":"unknown"}}`,
	} {
		decoded, err := decodeModelCapabilityDefaults(json.RawMessage(wrap(model)))
		require.NoError(t, err, model)
		encoded, err := json.Marshal(decoded)
		require.NoError(t, err)
		require.JSONEq(t, wrap(model), string(encoded), "all presence and false values survive the DTO")
	}
	for _, model := range []string{
		`{"max_input_tokens":0}`, `{"max_output_tokens":-1}`, `{"max_input_tokens":1.5}`,
		`{"max_input_tokens":9007199254740992}`, `{"max_output_tokens":"4096"}`,
		`{"context_window":100,"context_limit_kind":"total","max_output_tokens":101}`,
		`{"context_window":100,"context_limit_kind":"input","max_input_tokens":101}`,
		`{"tools":{"search":true}}`, `{"tools":{"function_calling":true},"supports_function_calling":false}`,
		`{"reasoning":false,"reasoning_control":{"kind":"fixed"}}`,
		`{"reasoning":true,"reasoning_control":{"kind":"none"}}`,
		`{"reasoning_control":{"kind":"invented"}}`, `{"reasoning_control":{"kind":"effort"}}`,
		`{"reasoning_control":{"kind":"effort","effort_values":["low"],"default":"high"}}`,
		`{"reasoning_control":{"kind":"budget","budget_min":20,"budget_max":10}}`,
		`{"reasoning_control":{"kind":"budget","budget_min":0,"budget_max":10,"default":11}}`,
		`{"reasoning_control":{"kind":"toggle","default":"true"}}`,
		`{"reasoning_control":{"kind":"toggle","supported_kinds":["effort"]}}`,
		`{"input_modalities":["text","text"]}`, `{"field_evidence":{"context_window":{}}}`,
	} {
		_, err := decodeModelCapabilityDefaults(json.RawMessage(wrap(model)))
		require.Error(t, err, model)
	}
	for _, raw := range []string{`{}`, `{"schema_version":2,"reason":"checked","models":{"x":{}}}`, `{"schema_version":1,"models":{"x":{}}}`, `{"schema_version":1,"reason":"checked","models":{"wild*":{}}}`} {
		_, err := decodeModelCapabilityDefaults(json.RawMessage(raw))
		require.Error(t, err, raw)
	}
}

func TestCapabilityDefaultsIdentityBindingAndReconfirmation(t *testing.T) {
	raw := json.RawMessage(`{"schema_version":1,"reason":"Verified by deployment owner","models":{"private-id":{"max_input_tokens":8192,"supports_function_calling":false}},"confirmed_by":"forged"}`)
	declaration, err := decodeModelCapabilityDefaults(raw)
	require.NoError(t, err)
	account := &service.Account{ID: 48, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "private-secret", "base_url": "https://private.example/v1"},
		Extra:       map[string]any{modelCapabilityDefaultsKey: declaration}}
	require.NoError(t, bindModelCapabilityDefaults(account, account.Extra, "admin:42"))
	bound := account.Extra[modelCapabilityDefaultsKey].(map[string]any)
	require.Equal(t, "admin:42", bound["confirmed_by"])
	require.NotEmpty(t, bound["confirmed_at"])
	require.Len(t, bound["identity_fingerprint"], 64)
	encoded, err := json.Marshal(bound)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-secret")
	require.NoError(t, validateSavedModelCapabilityIdentity(account))
	account.Credentials["base_url"] = "https://different.example/v1"
	require.ErrorContains(t, validateSavedModelCapabilityIdentity(account), "identity is stale")
	require.ErrorContains(t, bindModelCapabilityDefaults(account, account.Extra, "admin:42"), "different account identity")
	delete(bound, "identity_fingerprint") // Explicit holder reconfirmation.
	require.NoError(t, bindModelCapabilityDefaults(account, account.Extra, "admin:42"))
	require.NoError(t, validateSavedModelCapabilityIdentity(account))
	account.Extra[service.UpstreamModelCatalogStateExtraKey] = service.AccountModelCatalogState{IdentityRevision: 2}
	require.ErrorContains(t, validateSavedModelCapabilityIdentity(account), "identity is stale", "returning to the same credentials must not revive an older declaration")
	require.ErrorContains(t, bindModelCapabilityDefaults(account, account.Extra, "admin:42"), "different account identity")
}

func TestCapabilityDefaultsOAuthRotationUsesStableSubject(t *testing.T) {
	account := &service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"sub": "holder-1", "access_token": "old"}}
	before := modelCapabilityIdentityFingerprint(account)
	account.Credentials["access_token"] = "new"
	require.Equal(t, before, modelCapabilityIdentityFingerprint(account))
	account.Credentials["sub"] = "holder-2"
	require.NotEqual(t, before, modelCapabilityIdentityFingerprint(account))
	delete(account.Credentials, "sub")
	before = modelCapabilityIdentityFingerprint(account)
	account.Credentials["access_token"] = "different-unknown-holder"
	require.NotEqual(t, before, modelCapabilityIdentityFingerprint(account))
}

func TestCapabilityDefaultsStaleSavedIdentityStopsDiscovery(t *testing.T) {
	svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 48, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Extra: map[string]any{modelCapabilityDefaultsKey: map[string]any{"schema_version": 1, "reason": "verified", "models": map[string]any{"private": map[string]any{}}, "identity_fingerprint": "old"}}}}
	upstream := &syncUpstreamHTTPUpstream{}
	recorder := httptest.NewRecorder()
	setupSyncUpstreamModelsRouter(svc, upstream).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/48/models/sync-upstream", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "identity is stale")
	require.Empty(t, upstream.requests)
	require.Zero(t, svc.commitCalls)
}

func TestCapabilityDefaultsUpdateBindsBeforeWriteAndRejectsIdentityChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{
		ID: 48, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"base_url": "https://private.example/v1", "api_key": "secret"},
	}}
	handler := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.PUT("/accounts/:id", handler.Update)
	send := func(body []byte) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPut, "/accounts/48", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}
	recorder := send([]byte(`{"extra":{"model_capability_defaults":{"schema_version":1,"reason":"owner verified","models":{"private-id":{"max_input_tokens":8192,"supports_function_calling":false}}}}}`))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, svc.updateAccountCalls)
	saved := svc.lastUpdateAccountInput.Extra[modelCapabilityDefaultsKey].(map[string]any)
	require.Equal(t, "admin:0", saved["confirmed_by"])
	require.NotEmpty(t, saved["confirmed_at"])
	require.Equal(t, modelCapabilityIdentityFingerprint(&svc.account), saved["identity_fingerprint"])
	model := saved["models"].(map[string]any)["private-id"].(map[string]any)
	require.Equal(t, false, model["supports_function_calling"])
	require.Nil(t, svc.account.Extra, "binding must not mutate the loaded account")
	body, err := json.Marshal(map[string]any{"credentials": map[string]any{"base_url": "https://changed.example/v1"}, "extra": map[string]any{modelCapabilityDefaultsKey: saved}})
	require.NoError(t, err)
	recorder = send(body)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "different account identity")
	require.Equal(t, 1, svc.updateAccountCalls, "stale declaration must never reach the writer")
}
