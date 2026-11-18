package repository

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCopilotNousReferencedBulkBoundaryPostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	const oldJWT = "eyJhbGciOiJSUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDAsInNjb3BlIjoiaW5mZXJlbmNlOmludm9rZSJ9.c3ludGhldGlj"
	create := func(referenced bool) *service.Account {
		account := fixture.createAccount(t, fixture.ctx, uniqueName("nous-bulk-boundary"), "nous_research", service.AccountTypeAPIKey, map[string]any{"account_mode": "payg", "auth_mode": service.NousAuthInvocationToken, "api_key": oldJWT, "base_url": "", "model_mapping": map[string]any{"fixture-model": "fixture-model"}})
		if referenced {
			_, err := fixture.secrets.Convert(fixture.ctx, account.ID, fixture.accountRevision(t, account.ID))
			require.NoError(t, err)
		}
		return account
	}
	for _, replacement := range []string{"", "new-frontdoor"} {
		t.Run("bulk-rejected/"+replacement, func(t *testing.T) {
			inline, referenced := create(false), create(true)
			before := fixture.storedCredentials(t, referenced.ID)
			_, ciphertextBefore, err := readCredentialSecret(fixture.ctx, fixture.db, referenced.ID)
			require.NoError(t, err)
			patch := map[string]any{"auth_mode": service.NousAuthOfficialProxy, "base_url": "https://new-operator.invalid/v1", "api_key": replacement}
			count, err := fixture.accounts.BulkUpdate(fixture.ctx, []int64{inline.ID, referenced.ID}, service.AccountBulkUpdate{Credentials: patch})
			require.ErrorContains(t, err, "single-account", "must fail with an actionable limitation before JSONB merge")
			require.Zero(t, count)
			require.Equal(t, before, fixture.storedCredentials(t, referenced.ID))
			_, ciphertextAfter, err := readCredentialSecret(fixture.ctx, fixture.db, referenced.ID)
			require.NoError(t, err)
			require.Equal(t, ciphertextBefore, ciphertextAfter)
			for _, id := range []int64{inline.ID, referenced.ID} {
				loaded, err := fixture.accounts.GetByID(fixture.ctx, id)
				require.NoError(t, err)
				require.Equal(t, service.NousAuthInvocationToken, loaded.GetCredential("auth_mode"))
				require.Empty(t, loaded.GetCredential("base_url"))
				require.Equal(t, oldJWT, loaded.GetCredential("api_key"))
			}
			count, err = fixture.accounts.BulkUpdate(fixture.ctx, []int64{inline.ID}, service.AccountBulkUpdate{Credentials: patch})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			loaded, err := fixture.accounts.GetByID(fixture.ctx, inline.ID)
			require.NoError(t, err)
			require.Equal(t, replacement, loaded.GetCredential("api_key"))
		})
	}
	account := create(true)
	before := fixture.storedCredentials(t, account.ID)
	for _, patch := range []map[string]any{{"api_key": "replacement"}, {"api_key": ""}, {"auth_mode": service.NousAuthOfficialProxy}} {
		_, err := fixture.accounts.BulkUpdate(fixture.ctx, []int64{account.ID}, service.AccountBulkUpdate{Credentials: patch})
		require.ErrorContains(t, err, "single-account")
		require.Equal(t, before, fixture.storedCredentials(t, account.ID))
	}
	count, err := fixture.accounts.BulkUpdate(fixture.ctx, []int64{account.ID}, service.AccountBulkUpdate{Credentials: map[string]any{"auth_mode": service.NousAuthInvocationToken, "api_protocol": "adaptive"}})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	loaded, err := fixture.accounts.GetByID(fixture.ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, oldJWT, loaded.GetCredential("api_key"))
	require.Equal(t, before[service.CredentialSecretReferenceKey], loaded.Credentials[service.CredentialSecretReferenceKey])
	wrongWorkspace := domain.WithWorkspaceScope(fixture.ctx, fixture.createWorkspace(t, uniqueName("wrong-owner")))
	count, err = fixture.accounts.BulkUpdate(wrongWorkspace, []int64{account.ID}, service.AccountBulkUpdate{Credentials: map[string]any{"api_key": "foreign"}})
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = fixture.accounts.GetByID(wrongWorkspace, account.ID)
	require.Error(t, err)
	foreign := *loaded
	foreign.Credentials = maps.Clone(loaded.Credentials)
	foreign.Credentials["api_key"] = "foreign"
	require.Error(t, fixture.accounts.Update(wrongWorkspace, &foreign))
	loaded, err = fixture.accounts.GetByID(fixture.ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, oldJWT, loaded.GetCredential("api_key"))
	count, err = fixture.accounts.BulkUpdate(fixture.ctx, []int64{account.ID}, service.AccountBulkUpdate{Credentials: map[string]any{"model_mapping": map[string]any{"safe-alias": "fixture-model"}}})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	loaded, err = fixture.accounts.GetByID(fixture.ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, oldJWT, loaded.GetCredential("api_key"))
	require.Equal(t, before[service.CredentialSecretReferenceKey], loaded.Credentials[service.CredentialSecretReferenceKey])

	for _, replacement := range []string{"", "new-frontdoor"} {
		t.Run("single-update-send/"+replacement, func(t *testing.T) {
			account := create(true)
			var received string
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				received = request.Header.Get("Authorization")
				require.NotContains(t, received, oldJWT)
				writer.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(writer, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			resolved, err := fixture.accounts.GetByID(fixture.ctx, account.ID)
			require.NoError(t, err)
			resolved.Credentials = maps.Clone(resolved.Credentials)
			resolved.Credentials["auth_mode"], resolved.Credentials["base_url"], resolved.Credentials["api_key"] = service.NousAuthOfficialProxy, server.URL+"/v1", replacement
			require.NoError(t, fixture.accounts.Update(fixture.ctx, resolved))
			reloaded, err := fixture.accounts.GetByID(fixture.ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, replacement, reloaded.GetCredential("api_key"))
			require.NotContains(t, fixture.storedCredentials(t, account.ID), "api_key")
			metadata, ciphertext, err := readCredentialSecret(fixture.ctx, fixture.db, account.ID)
			require.NoError(t, err)
			secrets, err := service.ResolveCredentialSecret(context.Background(), fixture.encryptor, *metadata, ciphertext)
			require.NoError(t, err)
			require.Equal(t, replacement, secrets["api_key"])
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			probe := service.NewAccountTestService(fixture.accounts, nil, nil, nil, nil, NewHTTPUpstream(cfg), cfg, nil)
			result := probe.ProbeAccount(fixture.ctx, service.AccountProbeInput{AccountID: account.ID, ModelID: "fixture-model"}, func(service.TestEvent) {})
			require.True(t, result.Success, result.Error)
			expected := ""
			if replacement != "" {
				expected = "Bearer " + replacement
			}
			require.Equal(t, expected, received)
		})
	}
}
