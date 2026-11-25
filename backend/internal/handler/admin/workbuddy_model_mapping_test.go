//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// Use the real update/selection services and DTO redaction. Only persistence is
// in memory; the database merge interleaving has a separate repository regression.
func mappingRoundTripRouter(repo *handlerSelectionRepository) *gin.Engine {
	admin := service.NewAdminService(&config.Config{}, nil, nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler := &AccountHandler{adminService: admin}
	router := newHandlerSelectionRouter(repo, 7, 2)
	// New paths reuse identity middleware but execute services with the merging repository.
	router.PUT("/roundtrip/:id", handler.Update)
	router.GET("/roundtrip/:id", handler.GetByID)
	return router
}

func rereadMapping(t *testing.T, router *gin.Engine) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/roundtrip/42", nil))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "synthetic-private-token")
	var payload struct {
		Data struct {
			Credentials map[string]any `json:"credentials"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload.Data.Credentials
}

func TestAccountModelMappingSaveReread(t *testing.T) {
	for _, account := range []struct{ platform, kind string }{
		{service.PlatformWorkBuddy, service.AccountTypeOAuth},
		{service.PlatformWorkBuddy, service.AccountTypeAPIKey},
		{service.PlatformAnthropic, service.AccountTypeOAuth},
		{service.PlatformGemini, service.AccountTypeOAuth},
	} {
		t.Run(account.platform+"/"+account.kind, func(t *testing.T) {
			repo := newHandlerSelectionRepository()
			repo.account.Platform, repo.account.Type = account.platform, account.kind
			repo.account.Credentials["access_token"] = "synthetic-private-token"
			repo.account.Credentials["api_key"] = "synthetic-private-token"
			router := mappingRoundTripRouter(repo)
			mapping := map[string]any{"native": "native", "PublicAlias": "private-target"}
			saved := selectionHTTPRequest(router, "/roundtrip/42", `{"credentials":{"model_mapping":{"native":"native","PublicAlias":"private-target"}}}`)
			require.Equal(t, 200, saved.Code, saved.Body.String())
			require.NotContains(t, saved.Body.String(), "synthetic-private-token")
			require.Equal(t, mapping, rereadMapping(t, router)["model_mapping"])

			// Auth/metadata edits omit selection, and must retain the saved mapping.
			saved = selectionHTTPRequest(router, "/roundtrip/42", `{"name":"renamed","preserve_model_selection":true,"credentials":{"site":"cn"}}`)
			require.Equal(t, 200, saved.Code, saved.Body.String())
			require.Equal(t, "renamed", repo.account.Name)
			require.Equal(t, mapping, rereadMapping(t, router)["model_mapping"])

			// The independent selection endpoint also echoes via fresh account GET.
			saved = selectionHTTPRequest(router, "/accounts/42/models/allowlist", `{"enabled":true,"models":["chosen"],"expected_rules_revision":"4","model_mapping":{"chosen":"chosen","Alias":"chosen"}}`)
			require.Equal(t, 200, saved.Code, saved.Body.String())
			require.Equal(t, map[string]any{"chosen": "chosen", "Alias": "chosen"}, rereadMapping(t, router)["model_mapping"])
			// Legacy explicit clear remains valid when the preservation flag is absent.
			saved = selectionHTTPRequest(router, "/roundtrip/42", `{"credentials":{"model_mapping":{}}}`)
			require.Equal(t, 200, saved.Code, saved.Body.String())
			require.Empty(t, rereadMapping(t, router)["model_mapping"])
		})
	}
}

func TestAccountModelMappingPreserveRejectsContradictions(t *testing.T) {
	for _, body := range []string{
		`{"preserve_model_selection":true,"credentials":{"model_mapping":{}}}`,
		`{"preserve_model_selection":true,"credentials":{"model_mapping":null}}`,
		`{"preserve_model_selection":true,"credentials":{"model_whitelist":[]}}`,
		`{"preserve_model_selection":true,"extra":{"upstream_model_allowlist":{"enabled":false}}}`,
	} {
		repo := newHandlerSelectionRepository()
		response := selectionHTTPRequest(mappingRoundTripRouter(repo), "/roundtrip/42", body)
		require.Equal(t, 400, response.Code, response.Body.String())
		require.Zero(t, repo.writes)
	}
}

func TestAccountModelMappingGeneralSavePreservesLegacyAndCompact(t *testing.T) {
	repo := newHandlerSelectionRepository()
	repo.account.Platform, repo.account.Type = service.PlatformWorkBuddy, service.AccountTypeOAuth
	repo.account.Credentials["model_whitelist"] = []string{"legacy"}
	repo.account.Credentials["compact_model_mapping"] = map[string]string{"compact": "target"}
	router := mappingRoundTripRouter(repo)
	before := rereadMapping(t, router)
	for _, body := range []string{
		`{"name":"metadata only","preserve_model_selection":true}`,
		`{"name":"auth edit","preserve_model_selection":true,"credentials":{"site":"cn"}}`,
	} {
		response := selectionHTTPRequest(router, "/roundtrip/42", body)
		require.Equal(t, 200, response.Code, response.Body.String())
		after := rereadMapping(t, router)
		for _, key := range []string{"model_mapping", "model_whitelist", "compact_model_mapping"} {
			require.Equal(t, before[key], after[key])
		}
	}
	response := selectionHTTPRequest(router, "/roundtrip/42", `{"preserve_model_selection":true,"credentials":{"compact_model_mapping":{}}}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	after := rereadMapping(t, router)
	require.Empty(t, after["compact_model_mapping"])
	require.Equal(t, before["model_mapping"], after["model_mapping"])
	require.Equal(t, before["model_whitelist"], after["model_whitelist"])
}

func TestAccountDefaultWorkspaceResolver(t *testing.T) {
	handler := &AccountHandler{}
	_, err := handler.resolveDefaultWorkspace(context.Background())
	require.Error(t, err)
	handler.SetDefaultWorkspaceResolver(func(context.Context) (int64, error) { return 37, nil })
	id, err := handler.resolveDefaultWorkspace(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 37, id)
	handler.SetDefaultWorkspaceResolver(func(context.Context) (int64, error) { return 0, nil })
	_, err = handler.resolveDefaultWorkspace(context.Background())
	require.Error(t, err)
}
