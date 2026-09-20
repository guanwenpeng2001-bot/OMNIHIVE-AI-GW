package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func credentialDraftTestStore(t *testing.T) *service.AuthPendingIdentityService {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	return service.NewAuthPendingIdentityService(client)
}

func issueHTTPCredentialDraft(t *testing.T, pending *service.AuthPendingIdentityService, platform, accountType string, credentials map[string]any) (*http.Cookie, *service.AccountCredentialDraft) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/exchange", nil)
	respondCredentialDraft(ctx, pending, platform, accountType, credentials)
	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Data service.AccountCredentialDraft `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Data.Reference)
	return recorder.Result().Cookies()[0], &envelope.Data
}

func TestCredentialDraftHTTPBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pending := credentialDraftTestStore(t)
	router := gin.New()
	router.POST("/exchange", func(ctx *gin.Context) {
		respondCredentialDraft(ctx, pending, service.PlatformAnthropic, service.AccountTypeOAuth, map[string]any{"access_token": "access-secret", "refresh_token": "refresh-secret"})
	})
	handler := &OAuthHandler{}
	handler.SetCredentialDraftService(pending)
	router.GET("/drafts/:draft", handler.PreviewCredentialDraft)
	router.DELETE("/drafts/:draft", handler.CancelCredentialDraft)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/exchange", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "secret")
	var envelope struct {
		Data service.AccountCredentialDraft `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	cookie := recorder.Result().Cookies()[0]
	require.True(t, cookie.HttpOnly)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		foreign := httptest.NewRecorder()
		router.ServeHTTP(foreign, httptest.NewRequest(method, "/drafts/"+envelope.Data.Reference, nil))
		require.Equal(t, http.StatusUnauthorized, foreign.Code)
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodDelete} {
		request := httptest.NewRequest(method, "/drafts/"+envelope.Data.Reference, nil)
		request.AddCookie(cookie)
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code, "%s: %s", method, recorder.Body.String())
		require.NotContains(t, recorder.Body.String(), "secret")
	}
}

func TestCredentialAuthorizationBoundToInitiatingBrowserAndPurpose(t *testing.T) {
	pending := credentialDraftTestStore(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/start", nil)
	reference, err := beginCredentialAuthorization(ctx, pending, "anthropic:oauth", "upstream-session")
	require.NoError(t, err)
	cookie := recorder.Result().Cookies()[0]
	other, _ := gin.CreateTestContext(httptest.NewRecorder())
	other.Request = httptest.NewRequest(http.MethodPost, "/exchange", nil)
	_, err = consumeCredentialAuthorization(other, pending, "anthropic:oauth", reference)
	require.ErrorIs(t, err, service.ErrPendingAuthBrowserMismatch)
	other.Request.AddCookie(cookie)
	_, err = consumeCredentialAuthorization(other, pending, "anthropic:setup", reference)
	require.ErrorIs(t, err, service.ErrPendingAuthCodeInvalid)
	session, err := consumeCredentialAuthorization(other, pending, "anthropic:oauth", reference)
	require.NoError(t, err)
	require.Equal(t, "upstream-session", session)
	_, err = consumeCredentialAuthorization(other, pending, "anthropic:oauth", reference)
	require.ErrorIs(t, err, service.ErrPendingAuthCodeConsumed)
	_, err = pending.PreviewAccountCredentialDraft(context.Background(), reference, strings.Repeat("a", 64))
	require.Error(t, err)
}

func TestCredentialDraftPreviewRejectsForeignIdentityAndSecretOverrides(t *testing.T) {
	pending := credentialDraftTestStore(t)
	cookie, draft := issueHTTPCredentialDraft(t, pending, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"access_token": "server-secret"})
	handler := &AccountHandler{}
	handler.SetCredentialDraftService(pending)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/preview", nil)
	ctx.Request.AddCookie(cookie)
	request := SyncUpstreamModelsPreviewRequest{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, CredentialDraft: draft.Reference, CredentialDraftRevision: draft.Revision, ModelMapping: map[string]string{"public": "upstream"}}
	account := &service.Account{}
	require.NoError(t, handler.resolveCredentialDraftPreview(ctx, request, account))
	require.Equal(t, "server-secret", account.GetCredential("access_token"))
	request.Credentials = map[string]any{"access_token": "browser-secret"}
	require.ErrorIs(t, handler.resolveCredentialDraftPreview(ctx, request, account), service.ErrAccountCredentialDraftConflict)
	request.Credentials = nil
	request.Platform = service.PlatformAnthropic
	require.ErrorIs(t, handler.resolveCredentialDraftPreview(ctx, request, account), service.ErrAccountCredentialDraftConflict)
}

func TestRefreshUnsupportedAccountNeverClaimsSuccess(t *testing.T) {
	handler := &AccountHandler{}
	for _, account := range []*service.Account{{Platform: service.PlatformAnthropic, Type: service.AccountTypeSetupToken}, {Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}, {Platform: service.PlatformKimi, Type: service.AccountTypeOAuth}} {
		updated, _, err := handler.refreshSingleAccount(context.Background(), account)
		require.Error(t, err)
		require.Nil(t, updated)
	}
}

type credentialPrivacyAdmin struct {
	service.AdminService
	calls int
}

func (admin *credentialPrivacyAdmin) ForceOpenAIPrivacy(context.Context, *service.Account) string {
	admin.calls++
	return "disabled"
}

func TestCredentialPrivacyReplaysSameLogicalAction(t *testing.T) {
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig()))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	admin := &credentialPrivacyAdmin{}
	handler := &AccountHandler{adminService: admin}
	account := &service.Account{ID: 42, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "secret"}}
	for range 2 {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/accounts/42/set-privacy", nil)
		ctx.Request.Header.Set("Idempotency-Key", "privacy-account-42")
		mode, err := handler.setCredentialPrivacy(ctx, account)
		require.NoError(t, err)
		require.Equal(t, "disabled", mode)
	}
	require.Equal(t, 1, admin.calls)
}
