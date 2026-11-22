package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	adminhandler "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func stagedPostgresRequest() adminhandler.DataImportRequest {
	return adminhandler.DataImportRequest{StageInactive: true, Data: adminhandler.DataPayload{
		Proxies: []adminhandler.DataProxy{}, Accounts: []adminhandler.DataAccount{
			{Name: "staged-encryption-test", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ProxyDependency: "required", Credentials: map[string]any{"access_token": "test-stage-access", "refresh_token": "test-stage-refresh", "expires_at": "100"}, Concurrency: 1},
		},
	}}
}

func serveStagedPostgresImport(t *testing.T, f *credentialSecretFixture, req adminhandler.DataImportRequest) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := adminhandler.NewAccountHandler(queryClearAdmin(f), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.SetCredentialDraftService(service.NewAuthPendingIdentityService(f.client))
	h.SetStagedAccountImportService(service.NewStagedAccountImportService(NewStagedImportReceiptRepository(), f.encryptor))
	router := gin.New()
	router.POST("/api/v1/admin/accounts/data", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		c.Next()
	}, h.ImportData)
	body, err := json.Marshal(req)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body)).WithContext(f.ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", uuid.NewString())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	for _, secret := range []string{"test-stage-access", "test-stage-refresh", "test-stage-second", "encryptor-plaintext-detail"} {
		require.NotContains(t, recorder.Body.String(), secret)
	}
	return recorder
}

func TestStagedImportPostgresAtomicEncryptedStorage(t *testing.T) {
	f := newCredentialSecretFixture(t)
	active := f.createAccount(t, f.ctx, "legacy-refresh-control", service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"access_token": "test-legacy-access", "refresh_token": "test-legacy-refresh"})
	proxy, err := f.client.Proxy.Create().SetName("target-local-proxy").SetProtocol("http").SetHost("127.0.0.1").SetPort(18080).SetPassword("test-local-proxy-secret").SetStatus("inactive").Save(f.ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.client.Proxy.DeleteOneID(proxy.ID).Exec(f.ctx)) })
	proxy = f.client.Proxy.GetX(f.ctx, proxy.ID) // compare persisted timestamp precision
	// A real default group exists. The service has no group/proxy dependency
	// injected: any accidental lookup would fail this test instead of succeeding.
	group, err := f.client.Group.Create().SetName("openai-default").SetPlatform(service.PlatformOpenAI).Save(f.ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.client.Group.DeleteOneID(group.ID).Exec(f.ctx) })
	var createdIDs []int64
	f.client.Account.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
			if mutation.Op() == dbent.OpCreate {
				m, ok := mutation.(*dbent.AccountMutation)
				if !ok {
					return nil, fmt.Errorf("unexpected account mutation type %T", mutation)
				}
				status, _ := m.Status()
				require.Equal(t, "inactive", status, "the initial INSERT is already inactive")
				credentials, _ := m.Credentials()
				require.NotContains(t, credentials, "access_token")
				require.NotContains(t, credentials, "refresh_token")
			}
			value, err := next.Mutate(ctx, mutation)
			if err == nil && mutation.Op() == dbent.OpCreate {
				created, ok := value.(*dbent.Account)
				if !ok {
					return nil, fmt.Errorf("unexpected account mutation value %T", value)
				}
				createdIDs = append(createdIDs, created.ID)
			}
			return value, err
		})
	})
	req := stagedPostgresRequest()
	skip := false
	req.SkipDefaultGroupBind = &skip
	recorder := serveStagedPostgresImport(t, f, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(t, createdIDs, 1)
	id := createdIDs[0]
	stored := f.client.Account.GetX(f.ctx, id)
	require.Equal(t, "inactive", stored.Status)
	require.False(t, stored.Schedulable)
	require.Nil(t, stored.ProxyID)
	require.Nil(t, stored.ProxyGroupID)
	localProxy := f.client.Proxy.GetX(f.ctx, proxy.ID)
	require.Equal(t, proxy.Status, localProxy.Status)
	require.Equal(t, proxy.Password, localProxy.Password)
	require.Equal(t, proxy.UpdatedAt, localProxy.UpdatedAt, "staging must not modify existing proxies")
	require.Equal(t, "required", stored.Extra[service.StagedProxyDependencyExtraKey])
	require.NotContains(t, stored.Credentials, "access_token")
	require.NotContains(t, stored.Credentials, "refresh_token")
	ref, referenced := service.CredentialReference(stored.Credentials)
	require.True(t, referenced)
	require.NotEmpty(t, ref)
	metadata, ciphertext, err := readCredentialSecret(f.ctx, f.db, id)
	require.NoError(t, err)
	require.Equal(t, ref, metadata.Reference)
	require.NotContains(t, ciphertext, "test-stage-")
	resolved, err := service.ResolveCredentialSecret(f.ctx, f.encryptor, *metadata, ciphertext)
	require.NoError(t, err)
	require.Equal(t, "test-stage-access", resolved["access_token"])
	require.Equal(t, "test-stage-refresh", resolved["refresh_token"])
	otherKey, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("17", 32), EncryptionKeyConfigured: true}})
	require.NoError(t, err)
	_, err = service.ResolveCredentialSecret(f.ctx, otherKey, *metadata, ciphertext)
	require.Error(t, err, "only the target instance key may decrypt the new reference")
	var bindings, outbox int
	require.NoError(t, f.db.QueryRowContext(f.ctx, "SELECT count(*) FROM account_groups WHERE account_id=$1", id).Scan(&bindings))
	require.Zero(t, bindings)
	require.NoError(t, f.db.QueryRowContext(f.ctx, "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", id).Scan(&outbox))
	require.Equal(t, 1, outbox)
	var audit string
	require.NoError(t, f.db.QueryRowContext(f.ctx, "SELECT extra::text FROM audit_logs WHERE action='credential_secret.convert' AND extra->>'account_id'=$1", fmt.Sprint(id)).Scan(&audit))
	require.NotContains(t, audit, "test-stage-")
	// Actual PostgreSQL candidate predicates exclude staged OAuth both with and
	// without a refresh-token requirement (the Grok reconciliation case).
	for _, requireToken := range []bool{false, true} {
		page, err := f.accounts.ListOAuthRefreshCandidatePage(f.ctx, service.OAuthRefreshPageOptions{Platforms: []string{service.PlatformOpenAI}, Limit: 1000, ActiveOnly: true, IncludeSetupToken: true, RequireRefreshToken: requireToken})
		require.NoError(t, err)
		foundActive := false
		for _, candidate := range page.Accounts {
			require.NotEqual(t, id, candidate.ID)
			foundActive = foundActive || candidate.ID == active.ID
		}
		require.True(t, foundActive, "legacy active OAuth accounts remain refresh candidates")
	}
}

type stagedFailingEncryptor struct {
	service.SecretEncryptor
	calls int
}

type stagedEphemeralEncryptor struct{ service.SecretEncryptor }

func (e *stagedEphemeralEncryptor) EncryptionKeyConfigured() bool { return false }

func (e *stagedFailingEncryptor) EncryptionKeyConfigured() bool { return true }
func (e *stagedFailingEncryptor) Encrypt(plaintext string) (string, error) {
	e.calls++
	if e.calls == 2 {
		return "", errors.New("encryptor-plaintext-detail")
	}
	return e.SecretEncryptor.Encrypt(plaintext)
}

func stagedTableCounts(t *testing.T, f *credentialSecretFixture) []int64 {
	t.Helper()
	counts := make([]int64, 5)
	for i, table := range []string{"accounts", "credential_secrets", "account_groups", "scheduler_outbox", "audit_logs"} {
		require.NoError(t, f.db.QueryRowContext(f.ctx, "SELECT count(*) FROM "+table).Scan(&counts[i]))
	}
	return counts
}

func TestStagedImportPostgresRollbackAndMissingKey(t *testing.T) {
	for _, failure := range []string{"second_encryption", "missing_key", "ephemeral_key", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			f := newCredentialSecretFixture(t)
			before := stagedTableCounts(t, f)
			req := stagedPostgresRequest()
			second := req.Data.Accounts[0]
			second.Name, second.Credentials = "second-staged-account", map[string]any{"access_token": "test-stage-second"}
			req.Data.Accounts = append(req.Data.Accounts, second)
			switch failure {
			case "second_encryption":
				f.accounts.credentialEncryptor = &stagedFailingEncryptor{SecretEncryptor: f.encryptor}
			case "missing_key":
				f.accounts.credentialEncryptor = nil
			case "ephemeral_key":
				f.accounts.credentialEncryptor = &stagedEphemeralEncryptor{SecretEncryptor: f.encryptor}
			case "cancelled":
				ctx, cancel := context.WithCancel(f.ctx)
				cancel()
				f.ctx = ctx
			}
			recorder := serveStagedPostgresImport(t, f, req)
			switch failure {
			case "missing_key", "ephemeral_key":
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Contains(t, recorder.Body.String(), "encryption_key_required")
			case "cancelled":
				require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			default:
				require.Equal(t, http.StatusInternalServerError, recorder.Code)
			}
			f.ctx = context.Background()
			require.Equal(t, before, stagedTableCounts(t, f), "account, ciphertext, bindings, outbox and audit must roll back together")
		})
	}
}

func TestStagedImportPostgresKaggleEndpointsAndConfiguration(t *testing.T) {
	f := newCredentialSecretFixture(t)
	before := stagedTableCounts(t, f)
	var ids []int64
	f.client.Account.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
			if mutation.Op() == dbent.OpCreate {
				m, ok := mutation.(*dbent.AccountMutation)
				if !ok {
					return nil, fmt.Errorf("unexpected account mutation type %T", mutation)
				}
				status, _ := m.Status()
				require.Equal(t, "inactive", status)
				credentials, _ := m.Credentials()
				require.NotContains(t, credentials, "api_key")
			}
			value, err := next.Mutate(ctx, mutation)
			if err == nil && mutation.Op() == dbent.OpCreate {
				created, ok := value.(*dbent.Account)
				if !ok {
					return nil, fmt.Errorf("unexpected account mutation value %T", value)
				}
				ids = append(ids, created.ID)
			}
			return value, err
		})
	})
	req := adminhandler.DataImportRequest{StageInactive: true, Data: adminhandler.DataPayload{Proxies: []adminhandler.DataProxy{}}}
	for i := 0; i < 3; i++ {
		req.Data.Accounts = append(req.Data.Accounts, adminhandler.DataAccount{
			Name: "Shared Kaggle label", Platform: "kaggle", Type: service.AccountTypeAPIKey, ProxyDependency: "unknown",
			Credentials: map[string]any{
				"api_key": "shared-kaggle-fixture", "base_url": fmt.Sprintf("https://kaggle-%d.example.invalid/v1", i),
				"account_mode": "payg", "api_protocol": "chat_completions", "tenant_id": "tenant-a", "project_id": "project-a",
				"model_mapping":         map[string]any{"alias": "Qwen/Qwen3.5-9B"},
				"compact_model_mapping": map[string]any{"compact": "small-model"},
			}, Extra: map[string]any{"custom_base_url_enabled": true},
		})
	}
	recorder := serveStagedPostgresImport(t, f, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "shared-kaggle-fixture")
	require.Len(t, ids, 3)
	refs := make(map[string]bool)
	for i, id := range ids {
		stored := f.client.Account.GetX(f.ctx, id)
		require.Equal(t, "inactive", stored.Status)
		require.False(t, stored.Schedulable)
		require.Nil(t, stored.ProxyID)
		require.Nil(t, stored.ProxyGroupID)
		for _, field := range []string{"base_url", "tenant_id", "project_id", "account_mode", "api_protocol", "model_mapping", "compact_model_mapping"} {
			require.Equal(t, req.Data.Accounts[i].Credentials[field], stored.Credentials[field], field)
		}
		require.Equal(t, true, stored.Extra["custom_base_url_enabled"])
		require.Equal(t, "unknown", stored.Extra[service.StagedProxyDependencyExtraKey])
		require.NotContains(t, stored.Credentials, "api_key")
		metadata, ciphertext, err := readCredentialSecret(f.ctx, f.db, id)
		require.NoError(t, err)
		require.NotContains(t, ciphertext, "shared-kaggle-fixture")
		resolved, err := service.ResolveCredentialSecret(f.ctx, f.encryptor, *metadata, ciphertext)
		require.NoError(t, err)
		require.Equal(t, "shared-kaggle-fixture", resolved["api_key"])
		refs[metadata.Reference] = true
	}
	require.Len(t, refs, 3, "each account gets a new target-local encrypted reference")
	after := stagedTableCounts(t, f)
	require.Equal(t, before[0]+3, after[0])
	require.Equal(t, before[1]+3, after[1])
	require.Equal(t, before[2], after[2], "no group bindings, including Composite")
	duplicate := req.Data.Accounts[0]
	duplicate.Name = "Another label"
	duplicate.Credentials = map[string]any{"api_key": "shared-kaggle-fixture", "base_url": "HTTPS://KAGGLE-0.EXAMPLE.INVALID:443/", "tenant_id": "tenant-a", "project_id": "project-a", "model_mapping": map[string]any{"different": "model"}}
	req.Data.Accounts = append(req.Data.Accounts, duplicate)
	recorder = serveStagedPostgresImport(t, f, req)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, after, stagedTableCounts(t, f), "duplicate validation precedes every write")
}
