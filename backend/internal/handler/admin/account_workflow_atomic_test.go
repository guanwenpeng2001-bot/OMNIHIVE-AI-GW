package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/account"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

type atomicWorkflowAdmin struct {
	*stubAdminService
	accounts service.AdminAccountRepository
	failure  error
}

func (admin *atomicWorkflowAdmin) CreateAccount(ctx context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	if input.Name == "rejected" {
		return nil, admin.failure
	}
	transaction := dbent.TxFromContext(ctx)
	if transaction == nil {
		return nil, errors.New("account write escaped the batch transaction")
	}
	account, err := transaction.Account.Create().SetName(input.Name).SetPlatform(input.Platform).SetType(input.Type).SetCredentials(input.Credentials).Save(ctx)
	if err != nil {
		return nil, err
	}
	return &service.Account{ID: account.ID, Name: account.Name, Platform: account.Platform, Type: account.Type, Credentials: account.Credentials}, nil
}

func (admin *atomicWorkflowAdmin) CreateProxy(ctx context.Context, input *service.CreateProxyInput) (*service.Proxy, error) {
	transaction := dbent.TxFromContext(ctx)
	if transaction == nil {
		return nil, errors.New("proxy write escaped the import transaction")
	}
	proxy, err := transaction.Proxy.Create().SetName(input.Name).SetProtocol(input.Protocol).SetHost(input.Host).SetPort(input.Port).SetStatus(service.StatusActive).Save(ctx)
	if err != nil {
		return nil, err
	}
	return &service.Proxy{ID: proxy.ID, Name: proxy.Name, Status: proxy.Status}, nil
}

func TestAccountWorkflowImportFailureRollsBackAccountsAndProxies(t *testing.T) {
	pending, client := credentialDraftTestClient(t)
	failure := errors.New("invalid second account")
	admin := &atomicWorkflowAdmin{stubAdminService: newStubAdminService(), failure: failure}
	handler := &AccountHandler{adminService: admin}
	handler.SetCredentialDraftService(pending)
	payload := DataImportRequest{Data: DataPayload{
		Proxies: []DataProxy{{Name: "import-proxy", Protocol: "http", Host: "127.0.0.1", Port: 8081, Status: service.StatusActive}},
		Accounts: []DataAccount{
			{Name: "first", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "first"}, Concurrency: 1},
			{Name: "rejected", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "second"}, Concurrency: 1},
		},
	}}
	_, err := handler.importData(context.Background(), payload)
	require.ErrorIs(t, err, failure)
	require.Zero(t, client.Account.Query().CountX(context.Background()))
	require.Zero(t, client.Proxy.Query().CountX(context.Background()))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = handler.importData(cancelled, payload)
	require.Error(t, err)
	require.Zero(t, client.Account.Query().CountX(context.Background()))
	require.Zero(t, client.Proxy.Query().CountX(context.Background()))
	payload.Data.Proxies[0].BackupProxyName = "missing-backup"
	_, err = handler.importData(context.Background(), payload)
	require.Error(t, err)
	require.Zero(t, client.Account.Query().CountX(context.Background()))
	require.Zero(t, client.Proxy.Query().CountX(context.Background()))
}

func (admin *atomicWorkflowAdmin) GetAccount(ctx context.Context, accountID int64) (*service.Account, error) {
	return admin.accounts.GetByID(ctx, accountID)
}

func (admin *atomicWorkflowAdmin) UpdateAccount(ctx context.Context, accountID int64, input *service.UpdateAccountInput) (*service.Account, error) {
	transaction := dbent.TxFromContext(ctx)
	if transaction == nil {
		return nil, errors.New("update escaped confirmation transaction")
	}
	account, err := admin.accounts.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	account.Credentials = service.MergeCredentials(account.Credentials, input.Credentials)
	if err := admin.accounts.Update(ctx, account); err != nil {
		return nil, err
	}
	return admin.accounts.GetByID(ctx, accountID)
}

func TestCredentialDraftExistingAccountConfirmationIsBoundAndDoesNotCreate(t *testing.T) {
	// This path uses SELECT FOR UPDATE for the account identity CAS. SQLite
	// cannot exercise those locking semantics; run against an isolated database.
	dsn := os.Getenv("ACCOUNT_WORKFLOW_TEST_DSN")
	if dsn == "" {
		t.Skip("set ACCOUNT_WORKFLOW_TEST_DSN to an isolated PostgreSQL database")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	require.NoError(t, repository.ApplyMigrations(ctx, database))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	pending := service.NewAuthPendingIdentityService(client)
	fixture := "credential-draft-" + uuid.NewString()
	t.Cleanup(func() {
		_, err := database.ExecContext(ctx, "DELETE FROM pending_auth_sessions WHERE browser_session_key=$1", fixture)
		require.NoError(t, err)
		_, err = database.ExecContext(ctx, "DELETE FROM credential_secrets WHERE account_id IN (SELECT id FROM accounts WHERE name IN ($1,$2))", fixture, fixture+"-changed")
		require.NoError(t, err)
		_, err = database.ExecContext(ctx, "DELETE FROM accounts WHERE name IN ($1,$2)", fixture, fixture+"-changed")
		require.NoError(t, err)
	})
	accounts := repository.ProvideAdminAccountRepositoryWithSecrets(client, database, nil, testutil.CredentialEncryptor(t), nil)
	existing := &service.Account{Name: fixture, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "old"}}
	require.NoError(t, accounts.Create(ctx, existing))
	admin := &atomicWorkflowAdmin{stubAdminService: newStubAdminService(), accounts: accounts}
	draft, err := pending.IssueAccountCredentialDraft(ctx, fixture, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"access_token": "new"})
	require.NoError(t, err)
	request := CreateAccountRequest{ExistingAccountID: existing.ID, Name: fixture, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, CredentialDraft: draft.Reference, CredentialDraftRevision: draft.Revision, Credentials: map[string]any{"model_mapping": map[string]any{"public": "native"}}}
	digest, err := credentialDraftSubmission(request)
	require.NoError(t, err)
	// The review endpoint records the target account's credential revision; mirror it here.
	reviewed, err := accounts.GetByID(ctx, existing.ID)
	require.NoError(t, err)
	reviewCtx := service.WithCredentialRevision(ctx, reviewed.CredentialRevision)
	review, err := pending.ReviewAccountCredentialDraft(reviewCtx, draft.Reference, fixture, draft.Revision, digest)
	require.NoError(t, err)
	request.CredentialDraftRevision = review.Revision
	for range 2 {
		account, err := confirmCredentialDraftWithBinding(ctx, pending, admin, fixture, request, nil, false, false, "admin", false)
		require.NoError(t, err)
		require.Equal(t, existing.ID, account.ID)
	}
	require.Equal(t, 1, client.Account.Query().Where(account.NameEQ(fixture)).CountX(ctx))
	updated, err := accounts.GetByID(ctx, existing.ID)
	require.NoError(t, err)
	require.Equal(t, "new", updated.GetCredential("access_token"))
	request.Name = fixture + "-changed"
	_, err = confirmCredentialDraftWithBinding(ctx, pending, admin, fixture, request, nil, false, false, "admin", false)
	require.ErrorIs(t, err, service.ErrAccountCredentialDraftConflict)
	require.Equal(t, 1, client.Account.Query().Where(account.NameIn(fixture, request.Name)).CountX(ctx))
}

func TestAccountWorkflowBatchFailureLeavesNoAccounts(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "validation", true: "mixed-channel"}[mixed], func(t *testing.T) {
			pending, client := credentialDraftTestClient(t)
			failure := error(errors.New("second account failed"))
			status := http.StatusInternalServerError
			if mixed {
				failure = &service.MixedChannelError{GroupID: 7, GroupName: "mixed", CurrentPlatform: service.PlatformAnthropic, OtherPlatform: service.PlatformAntigravity}
				status = http.StatusConflict
			}
			admin := &atomicWorkflowAdmin{stubAdminService: newStubAdminService(), failure: failure}
			handler := &AccountHandler{adminService: admin}
			handler.SetCredentialDraftService(pending)
			router := gin.New()
			router.POST("/accounts/batch", handler.BatchCreate)
			request := httptest.NewRequest(http.MethodPost, "/accounts/batch", strings.NewReader(`{"accounts":[{"name":"first","platform":"anthropic","type":"apikey","credentials":{"api_key":"first"}},{"name":"rejected","platform":"anthropic","type":"apikey","credentials":{"api_key":"second"}}]}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, status, recorder.Code, recorder.Body.String())
			require.Zero(t, client.Account.Query().CountX(context.Background()))
			if mixed {
				require.Contains(t, recorder.Body.String(), `"reason":"mixed_channel_warning"`)
			}
		})
	}
}

func TestManualCursorDraftValidationAndCancellationNeverCreateAccount(t *testing.T) {
	pending, client := credentialDraftTestClient(t)
	handler := &AccountHandler{}
	handler.SetCredentialDraftService(pending)
	router := gin.New()
	router.POST("/drafts", handler.IssueCursorCredentialDraft)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/drafts", strings.NewReader(`{"credentials":{}}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, client.PendingAuthSession.Query().CountX(context.Background()))
	require.Zero(t, client.Account.Query().CountX(context.Background()))
	valid, err := json.Marshal(map[string]any{"credentials": map[string]any{"access_token": "synthetic-access", "refresh_token": "synthetic-refresh", "machine_id": strings.Repeat("a", 64), "mac_machine_id": strings.Repeat("b", 64)}})
	require.NoError(t, err)
	request = httptest.NewRequest(http.MethodPost, "/drafts", strings.NewReader(string(valid)))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "synthetic-access")
	var envelope struct {
		Data service.AccountCredentialDraft `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	session := client.PendingAuthSession.Query().OnlyX(context.Background())
	require.NoError(t, pending.CancelAccountCredentialDraft(context.Background(), envelope.Data.Reference, session.BrowserSessionKey))
	require.Zero(t, client.Account.Query().CountX(context.Background()))
}

func TestCodexSessionPreparationCreatesOnlyDrafts(t *testing.T) {
	pending, client := credentialDraftTestClient(t)
	admin := newCodexImportMemoryAdminService(nil)
	handler := &AccountHandler{adminService: admin}
	handler.SetCredentialDraftService(pending)
	result, err := handler.prepareCodexSessionDrafts(context.Background(), "browser", CodexSessionImportRequest{}, []codexImportEntry{
		{Index: 1, Value: buildCodexAccessOnlyImportValue(t, "workspace", "user")},
	})
	require.NoError(t, err)
	require.Len(t, result.Ready, 1)
	require.Empty(t, admin.createdAccounts)
	require.Zero(t, client.Account.Query().CountX(context.Background()))
	require.NoError(t, pending.CancelAccountCredentialDraft(context.Background(), result.Ready[0].Draft.Reference, "browser"))
	require.Zero(t, client.Account.Query().CountX(context.Background()))
}
