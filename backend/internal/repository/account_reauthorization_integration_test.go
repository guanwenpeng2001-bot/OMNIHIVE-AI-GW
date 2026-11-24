package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/pendingauthsession"
	"github.com/gin-gonic/gin"
	adminhandler "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func referencedAccountRequest(t *testing.T, ctx context.Context, router *gin.Engine, method, path string, cookie *http.Cookie, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(payload)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestReferencedAccountOAuthDraftApplyPostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	pending := service.NewAuthPendingIdentityService(fixture.client)
	handler := adminhandler.NewAccountHandler(queryClearAdmin(fixture), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.SetCredentialDraftService(pending)
	router := gin.New()
	router.POST("/drafts/:draft/review", handler.ReviewCredentialDraft)
	router.POST("/accounts/:id/apply-oauth-credentials", handler.ApplyOAuthCredentials)
	browser := strings.Repeat("b", 64)
	cookie := &http.Cookie{Name: "account_authorization_browser", Value: browser}
	binding := sha256.Sum256([]byte("admin:0:" + browser))

	for _, test := range []struct {
		name, platform    string
		rotateAfterReview bool
	}{
		{"openai", service.PlatformOpenAI, false},
		{"anthropic", service.PlatformAnthropic, false},
		{"gemini", service.PlatformGemini, false},
		{"antigravity", service.PlatformAntigravity, false},
		{"grok", service.PlatformGrok, false},
		{"rotation-after-review", service.PlatformOpenAI, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := fixture.createAccount(t, fixture.ctx, uniqueName("hf1-oauth"), test.platform, service.AccountTypeOAuth, map[string]any{
				"access_token": "fixture-old-access", "refresh_token": "fixture-old-refresh",
				"model_mapping": map[string]any{"old-alias": "old-upstream"},
			})
			metadata, err := fixture.secrets.Convert(fixture.ctx, account.ID, fixture.accountRevision(t, account.ID))
			require.NoError(t, err)
			require.EqualValues(t, 1, metadata.Version)
			require.NoError(t, fixture.accounts.SetError(fixture.ctx, account.ID, "fixture expired credential"))

			draft, err := pending.IssueAccountCredentialDraft(fixture.ctx, hex.EncodeToString(binding[:]), test.platform, service.AccountTypeOAuth, map[string]any{
				"access_token": "fixture-new-access", "refresh_token": "fixture-new-refresh",
			})
			require.NoError(t, err)
			mapping := map[string]any{"reviewed-alias": "reviewed-upstream"}
			review := adminhandler.CreateAccountRequest{
				Name: fmt.Sprintf("account:%d", account.ID), Platform: test.platform, Type: service.AccountTypeOAuth,
				CredentialDraft: draft.Reference, CredentialDraftRevision: draft.Revision,
				Credentials: map[string]any{"model_mapping": mapping},
			}
			recorder := referencedAccountRequest(t, fixture.ctx, router, http.MethodPost, "/drafts/"+draft.Reference+"/review", cookie, review)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			var reviewed struct {
				Data service.AccountCredentialDraft
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &reviewed))
			require.NotEqual(t, draft.Revision, reviewed.Data.Revision)

			if test.rotateAfterReview {
				_, err := fixture.secrets.Rotate(fixture.ctx, account.ID, 1, map[string]any{"access_token": "fixture-winning-access"})
				require.NoError(t, err)
			}
			apply := adminhandler.ApplyOAuthCredentialsRequest{
				Type: review.Type, CredentialDraft: draft.Reference, CredentialDraftRevision: reviewed.Data.Revision, Credentials: review.Credentials,
			}
			path := fmt.Sprintf("/accounts/%d/apply-oauth-credentials", account.ID)
			recorder = referencedAccountRequest(t, fixture.ctx, router, http.MethodPost, path, cookie, apply)
			loaded, err := fixture.accounts.GetByID(fixture.ctx, account.ID)
			require.NoError(t, err)
			require.EqualValues(t, 2, service.CredentialVersion(loaded.Credentials), recorder.Body.String())
			stored := fixture.storedCredentials(t, account.ID)
			require.Equal(t, metadata.Reference, stored[service.CredentialSecretReferenceKey])
			require.EqualValues(t, 2, service.CredentialVersion(stored))
			require.NotContains(t, stored, "access_token")
			require.NotContains(t, stored, "refresh_token")
			if test.rotateAfterReview {
				require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), "ACCOUNT_CREDENTIAL_DRAFT_CONFLICT")
				require.Equal(t, "fixture-winning-access", loaded.GetCredential("access_token"))
				require.Equal(t, "fixture-old-refresh", loaded.GetCredential("refresh_token"))
				require.Equal(t, "old-upstream", loaded.GetModelMapping()["old-alias"])
				require.Equal(t, service.StatusError, loaded.Status)
				require.Equal(t, "fixture expired credential", loaded.ErrorMessage)
				return
			}
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.NotContains(t, recorder.Body.String(), "fixture-new-")
			require.Equal(t, "fixture-new-access", loaded.GetCredential("access_token"))
			require.Equal(t, "fixture-new-refresh", loaded.GetCredential("refresh_token"))
			require.Equal(t, mapping, stored["model_mapping"])
			require.Equal(t, service.StatusActive, loaded.Status)
			require.Empty(t, loaded.ErrorMessage)

			replay := referencedAccountRequest(t, fixture.ctx, router, http.MethodPost, path, cookie, apply)
			require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
			current, err := fixture.secrets.Metadata(fixture.ctx, account.ID)
			require.NoError(t, err)
			require.EqualValues(t, 2, current.Version)
		})
	}
}

func TestReferencedAccountExistingAccountDraftConfirmPostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	pending := service.NewAuthPendingIdentityService(fixture.client)
	handler := adminhandler.NewAccountHandler(queryClearAdmin(fixture), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.SetCredentialDraftService(pending)
	router := gin.New()
	router.POST("/drafts/:draft/review", handler.ReviewCredentialDraft)
	router.POST("/accounts", handler.Create)

	for _, test := range []struct {
		name          string
		legacyReview  bool
		rotate        bool
		expectedCode  int
		expectedError string
	}{
		{name: "rotation-after-review", rotate: true, expectedCode: http.StatusConflict, expectedError: "ACCOUNT_CREDENTIAL_DRAFT_CONFLICT"},
		{name: "pre-patch-review", legacyReview: true, rotate: true, expectedCode: http.StatusConflict, expectedError: "review again"},
		{name: "unchanged-review", expectedCode: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := fixture.createAccount(t, fixture.ctx, uniqueName("revhf-existing"), service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{
				"access_token": "fixture-old-access", "refresh_token": "fixture-old-refresh",
				"model_mapping": map[string]any{"old-alias": "old-upstream"},
			})
			metadata, err := fixture.secrets.Convert(fixture.ctx, account.ID, fixture.accountRevision(t, account.ID))
			require.NoError(t, err)
			require.EqualValues(t, 1, metadata.Version)
			browser := strings.Repeat("c", 64)
			if test.legacyReview {
				browser = strings.Repeat("d", 64)
			} else if !test.rotate {
				browser = strings.Repeat("e", 64)
			}
			cookie := &http.Cookie{Name: "account_authorization_browser", Value: browser}
			binding := sha256.Sum256([]byte("admin:0:" + browser))
			bindingKey := hex.EncodeToString(binding[:])
			draft, err := pending.IssueAccountCredentialDraft(fixture.ctx, bindingKey, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{
				"access_token": "fixture-draft-access", "refresh_token": "fixture-draft-refresh",
			})
			require.NoError(t, err)
			review := adminhandler.CreateAccountRequest{
				ExistingAccountID: account.ID, Name: account.Name, Platform: account.Platform, Type: account.Type,
				CredentialDraft: draft.Reference, CredentialDraftRevision: draft.Revision,
				Credentials: map[string]any{"model_mapping": map[string]any{"reviewed-alias": "reviewed-upstream"}},
			}
			recorder := referencedAccountRequest(t, fixture.ctx, router, http.MethodPost, "/drafts/"+draft.Reference+"/review", cookie, review)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			var reviewed struct {
				Data service.AccountCredentialDraft
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &reviewed))
			review.CredentialDraftRevision = reviewed.Data.Revision

			if test.legacyReview {
				session, err := fixture.client.PendingAuthSession.Query().Where(pendingauthsession.BrowserSessionKeyEQ(bindingKey)).Only(fixture.ctx)
				require.NoError(t, err)
				encoded, ok := session.LocalFlowState["credential_draft_state"].(string)
				require.True(t, ok)
				state := map[string]any{}
				require.NoError(t, json.Unmarshal([]byte(encoded), &state))
				delete(state, "reviewed_credential_revision")
				legacy, err := json.Marshal(state)
				require.NoError(t, err)
				_, err = fixture.client.PendingAuthSession.UpdateOneID(session.ID).SetLocalFlowState(map[string]any{"credential_draft_state": string(legacy)}).Save(fixture.ctx)
				require.NoError(t, err)
			}
			if test.rotate {
				_, err = fixture.secrets.Rotate(fixture.ctx, account.ID, 1, map[string]any{"access_token": "fixture-winning-access"})
				require.NoError(t, err)
			}

			recorder = referencedAccountRequest(t, fixture.ctx, router, http.MethodPost, "/accounts", cookie, review)
			require.Equal(t, test.expectedCode, recorder.Code, recorder.Body.String())
			if test.expectedError != "" {
				require.Contains(t, recorder.Body.String(), test.expectedError)
			}
			current, err := fixture.secrets.Metadata(fixture.ctx, account.ID)
			require.NoError(t, err)
			loaded, err := fixture.accounts.GetByID(fixture.ctx, account.ID)
			require.NoError(t, err)
			if test.expectedCode == http.StatusOK {
				require.EqualValues(t, 2, current.Version)
				require.Equal(t, "fixture-draft-access", loaded.GetCredential("access_token"))
				require.Equal(t, "fixture-draft-refresh", loaded.GetCredential("refresh_token"))
				require.Equal(t, "reviewed-upstream", loaded.GetModelMapping()["reviewed-alias"])
				return
			}
			require.EqualValues(t, 2, current.Version)
			require.Equal(t, "fixture-winning-access", loaded.GetCredential("access_token"))
			require.Equal(t, "fixture-old-refresh", loaded.GetCredential("refresh_token"))
			require.Equal(t, "old-upstream", loaded.GetModelMapping()["old-alias"])
			stored := fixture.storedCredentials(t, account.ID)
			require.Equal(t, metadata.Reference, stored[service.CredentialSecretReferenceKey])
			session, err := fixture.client.PendingAuthSession.Query().Where(pendingauthsession.BrowserSessionKeyEQ(bindingKey)).Only(fixture.ctx)
			require.NoError(t, err)
			require.Nil(t, session.ConsumedAt)
		})
	}
}

func TestReferencedAccountGuardedRefreshPostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	account := fixture.createAccount(t, fixture.ctx, uniqueName("hf1-refresh"), service.PlatformGrok, service.AccountTypeOAuth,
		map[string]any{"access_token": "fixture-access-before-refresh"})
	_, err := fixture.secrets.Convert(fixture.ctx, account.ID, fixture.accountRevision(t, account.ID))
	require.NoError(t, err)
	admin := queryClearAdmin(fixture)
	baseline, err := admin.GetAccount(fixture.ctx, account.ID)
	require.NoError(t, err)
	pending := service.NewAuthPendingIdentityService(fixture.client)
	updated, err := pending.UpdateAccountCredentialsIfUnchanged(service.WithCredentialRevision(fixture.ctx, baseline.CredentialRevision), baseline, admin, &service.UpdateAccountInput{
		Credentials: map[string]any{"access_token": "fixture-access-after-refresh"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, service.CredentialVersion(updated.Credentials))
	require.Equal(t, "fixture-access-after-refresh", updated.GetCredential("access_token"))
	_, err = pending.UpdateAccountCredentialsIfUnchanged(fixture.ctx, baseline, admin, &service.UpdateAccountInput{
		Credentials: map[string]any{"access_token": "fixture-losing-refresh"},
	})
	require.ErrorIs(t, err, service.ErrAccountCredentialDraftConflict)
}

func TestReferencedAccountAdminSecretEditPostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	handler := adminhandler.NewAccountHandler(queryClearAdmin(fixture), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/accounts/:id", handler.GetByID)
	router.PUT("/accounts/:id", handler.Update)

	for _, test := range []struct{ platform, accountType, field string }{
		{service.PlatformWorkBuddy, service.AccountTypeOAuth, "access_token"},
		{service.PlatformOpenAI, service.AccountTypeAPIKey, "api_key"},
	} {
		t.Run(test.platform, func(t *testing.T) {
			account := fixture.createAccount(t, fixture.ctx, uniqueName("hf1-edit"), test.platform, test.accountType, map[string]any{test.field: "fixture-old-secret"})
			metadata, err := fixture.secrets.Convert(fixture.ctx, account.ID, fixture.accountRevision(t, account.ID))
			require.NoError(t, err)
			require.EqualValues(t, 1, metadata.Version)
			path := fmt.Sprintf("/accounts/%d", account.ID)
			get := referencedAccountRequest(t, fixture.ctx, router, http.MethodGet, path, nil, nil)
			require.Equal(t, http.StatusOK, get.Code, get.Body.String())
			var envelope struct {
				Data struct {
					Credentials map[string]any
				}
			}
			require.NoError(t, json.Unmarshal(get.Body.Bytes(), &envelope))
			credentials := envelope.Data.Credentials
			require.NotContains(t, credentials, test.field)
			require.EqualValues(t, 1, service.CredentialVersion(credentials))

			credentials[test.field] = "fixture-new-secret"
			edit := referencedAccountRequest(t, fixture.ctx, router, http.MethodPut, path, nil, map[string]any{"credentials": credentials})
			require.Equal(t, http.StatusOK, edit.Code, edit.Body.String())
			require.NotContains(t, edit.Body.String(), "fixture-new-secret")
			rotated, ciphertext, err := readCredentialSecret(fixture.ctx, fixture.db, account.ID)
			require.NoError(t, err)
			require.EqualValues(t, 2, rotated.Version)

			credentials[test.field] = "fixture-losing-secret"
			stale := referencedAccountRequest(t, fixture.ctx, router, http.MethodPut, path, nil, map[string]any{"credentials": credentials})
			require.Equal(t, http.StatusConflict, stale.Code, stale.Body.String())
			require.Contains(t, stale.Body.String(), "CREDENTIAL_SECRET_CONFLICT")
			delete(credentials, test.field)
			credentials["note"] = "metadata-only"
			metadataEdit := referencedAccountRequest(t, fixture.ctx, router, http.MethodPut, path, nil, map[string]any{"credentials": credentials})
			require.Equal(t, http.StatusOK, metadataEdit.Code, metadataEdit.Body.String())
			unchanged, afterCiphertext, err := readCredentialSecret(fixture.ctx, fixture.db, account.ID)
			require.NoError(t, err)
			require.Equal(t, rotated.Version, unchanged.Version)
			require.Equal(t, ciphertext, afterCiphertext)
			loaded, err := fixture.accounts.GetByID(fixture.ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, "fixture-new-secret", loaded.GetCredential(test.field))
			require.Equal(t, "metadata-only", loaded.GetCredential("note"))
		})
	}
}
