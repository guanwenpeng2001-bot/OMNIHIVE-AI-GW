package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	adminhandler "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testassert"
	"github.com/stretchr/testify/require"
)

var queryClearFields = []string{
	"volcengine_access_key_id", "volcengine_secret_access_key", "byteplus_access_key_id",
	"byteplus_secret_access_key", "tencent_query_secret_id", "tencent_query_secret_key", "usage_query_token",
}

type queryClearSnapshotCache struct {
	service.SchedulerCache
	account *service.Account
}

func (cache *queryClearSnapshotCache) SetAccount(_ context.Context, account *service.Account) error {
	cache.account = account
	return nil
}

func queryClearAdmin(fixture *credentialSecretFixture) service.AdminService {
	return service.NewAdminService(nil, nil, nil, nil, fixture.accounts, nil, nil, nil, nil, nil, nil, nil, nil, nil, fixture.client, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func queryClearAccount(t *testing.T, fixture *credentialSecretFixture) *service.Account {
	t.Helper()
	credentials := map[string]any{"api_key": "fixture-clear-inference", "base_url": "https://www.dmxapi.cn", "usage_query_user_id": "fixture-user", "note": "preserved"}
	for _, field := range queryClearFields {
		credentials[field] = "fixture-clear-" + field
	}
	account := &service.Account{Name: uniqueName("admin-query-clear"), Platform: "dmxapi", Type: service.AccountTypeAPIKey, Status: "inactive", Credentials: credentials, Concurrency: 1}
	require.NoError(t, fixture.accounts.Create(fixture.ctx, account))
	loaded, err := fixture.accounts.GetByID(fixture.ctx, account.ID)
	require.NoError(t, err)
	return loaded
}

func TestAdminQueryClearHTTPPostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	cache := &queryClearSnapshotCache{}
	fixture.accounts.schedulerCache = cache
	admin := queryClearAdmin(fixture)
	handler := adminhandler.NewAccountHandler(admin, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.PUT("/accounts/:id", handler.Update)
	router.GET("/accounts/:id", handler.GetByID)
	request := func(method string, id int64, body any) map[string]any {
		t.Helper()
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, fmt.Sprintf("/accounts/%d", id), bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.NotContains(t, recorder.Body.String(), "fixture-clear-")
		var response struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		return response.Data
	}
	for _, target := range append(append([]string{}, queryClearFields...), "all") {
		t.Run(target, func(t *testing.T) {
			account := queryClearAccount(t, fixture)
			_, expected := service.SplitCredentialSecrets(account.Credentials)
			require.NoError(t, fixture.accounts.UpdateAccountRuntimeSnapshot(fixture.ctx, account, map[string]any{"dmxapi_balance": float64(42)}, nil))
			before := request(http.MethodGet, account.ID, nil)
			credentials := testassert.Type[map[string]any](t, before["credentials"])
			fields := []string{target}
			if target == "all" {
				fields = queryClearFields
			}
			for _, field := range fields {
				credentials[field] = nil
				delete(expected, field)
			}
			credentials["note"] = nil // Ordinary null still deletes inline configuration.
			updated := request(http.MethodPut, account.ID, map[string]any{"credentials": credentials})
			fresh := request(http.MethodGet, account.ID, nil)
			for _, key := range []string{"credentials_status", "credentials", "runtime_identity"} {
				require.Contains(t, updated, key)
				require.Equal(t, fresh[key], updated[key], "immediate PUT/GET %s", key)
			}
			resolved, err := fixture.accounts.GetByID(fixture.ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, int64(2), service.CredentialVersion(resolved.Credentials))
			require.Equal(t, account.Credentials[service.CredentialSecretReferenceKey], resolved.Credentials[service.CredentialSecretReferenceKey])
			require.NotEqual(t, service.AccountRuntimeIdentityKey(account), service.AccountRuntimeIdentityKey(resolved))
			require.NotContains(t, resolved.Extra, "dmxapi_balance")
			require.NotNil(t, cache.account)
			require.Equal(t, service.AccountRuntimeIdentityKey(resolved), service.AccountRuntimeIdentityKey(cache.account))
			require.NotContains(t, cache.account.Extra, "dmxapi_balance")
			require.NotContains(t, resolved.Credentials, "note")
			require.Equal(t, "inactive", resolved.Status)
			metadata, ciphertext, err := readCredentialSecret(fixture.ctx, fixture.db, account.ID)
			require.NoError(t, err)
			secrets, err := service.ResolveCredentialSecret(fixture.ctx, fixture.encryptor, *metadata, ciphertext)
			require.NoError(t, err)
			require.Equal(t, expected, secrets, "only requested query secrets are cleared")
			var audits string
			require.NoError(t, fixture.db.QueryRow(`SELECT COALESCE(string_agg(extra::text, ''), '') FROM audit_logs WHERE extra->>'account_id'=$1`, fmt.Sprint(account.ID)).Scan(&audits))
			require.NotEmpty(t, audits)
			require.NotContains(t, audits, "fixture-clear-")
			require.NotContains(t, ciphertext, "fixture-clear-")
			status := testassert.Type[map[string]any](t, updated["credentials_status"])
			for _, field := range append(append([]string{}, queryClearFields...), "api_key") {
				// The existing wire contract omits false flags entirely.
				if expected[field] != nil {
					require.Equal(t, true, status["has_"+field])
				} else {
					require.NotContains(t, status, "has_"+field)
				}
				require.NotContains(t, fixture.storedCredentials(t, account.ID), field)
			}
			require.ErrorIs(t, fixture.accounts.UpdateAccountRuntimeSnapshot(fixture.ctx, account, map[string]any{"dmxapi_balance": float64(99)}, nil), service.ErrAccountRuntimeIdentityChanged)
			// Repeating explicit null on absent fields is idempotent with fresh metadata.
			credentials = testassert.Type[map[string]any](t, fresh["credentials"])
			for _, field := range fields {
				credentials[field] = nil
			}
			again := request(http.MethodPut, account.ID, map[string]any{"credentials": credentials})
			require.Equal(t, updated["runtime_identity"], again["runtime_identity"])
			require.Equal(t, updated["credentials"], again["credentials"])
		})
	}
}

func TestAdminQueryClearPreservationPostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	admin := queryClearAdmin(fixture)
	account := queryClearAccount(t, fixture)
	for _, mode := range []string{"omit", "blank", "whitespace", "api_key_null"} {
		t.Run(mode, func(t *testing.T) {
			patch := dto.AccountFromService(account).Credentials
			for _, field := range queryClearFields {
				switch mode {
				case "blank":
					patch[field] = ""
				case "whitespace":
					patch[field] = " \t "
				}
			}
			if mode == "api_key_null" {
				patch["api_key"] = nil
			}
			updated, err := admin.UpdateAccount(fixture.ctx, account.ID, &service.UpdateAccountInput{Credentials: patch})
			if mode == "api_key_null" {
				// Compatible-provider validation already rejects null inference
				// keys; this query-clear fix must not alter that contract.
				require.ErrorContains(t, err, "api_key is required")
				updated, err = fixture.accounts.GetByID(fixture.ctx, account.ID)
			}
			require.NoError(t, err)
			require.Equal(t, account.Credentials, updated.Credentials)
			require.Equal(t, service.AccountRuntimeIdentityKey(account), service.AccountRuntimeIdentityKey(updated))
		})
	}
}

func TestAdminQueryClearVersionRequiredPostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	admin := queryClearAdmin(fixture)
	account := queryClearAccount(t, fixture)
	rotated := maps.Clone(account.Credentials)
	rotated["usage_query_token"] = "fixture-current-query"
	require.NoError(t, fixture.accounts.UpdateCredentials(fixture.ctx, account.ID, rotated))
	for _, mode := range []string{"stale", "missing"} {
		t.Run(mode, func(t *testing.T) {
			patch := maps.Clone(account.Credentials)
			patch["usage_query_token"] = nil
			if mode == "stale" {
				patch[service.CredentialSecretVersionKey] = int64(1)
			} else {
				delete(patch, service.CredentialSecretVersionKey)
			}
			before, ciphertext, err := readCredentialSecret(fixture.ctx, fixture.db, account.ID)
			require.NoError(t, err)
			_, err = admin.UpdateAccount(fixture.ctx, account.ID, &service.UpdateAccountInput{Credentials: patch})
			require.ErrorIs(t, err, service.ErrCredentialSecretConflict)
			after, unchanged, err := readCredentialSecret(fixture.ctx, fixture.db, account.ID)
			require.NoError(t, err)
			require.Equal(t, before.Version, after.Version)
			require.Equal(t, ciphertext, unchanged)
		})
	}
}

func TestAdminQueryClearReadLockRacePostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	admin := queryClearAdmin(fixture)
	for _, operation := range []string{"clear", "rotate", "nonsecret"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(fixture.ctx, 20*time.Second)
			defer cancel()
			account := queryClearAccount(t, fixture)
			patch := maps.Clone(account.Credentials)
			require.IsType(t, int64(1), patch[service.CredentialSecretVersionKey])
			switch operation {
			case "clear":
				patch["usage_query_token"] = nil
			case "rotate":
				patch["usage_query_token"] = "fixture-losing-rotation"
			case "nonsecret":
				patch["note"] = "waiting-edit"
			}
			winner, err := fixture.client.Tx(ctx)
			require.NoError(t, err)
			defer func() { _ = winner.Rollback() }()
			_, _, _, err = lockCredentialAccount(ctx, winner.Client(), account.ID)
			require.NoError(t, err)
			rows, err := winner.Client().QueryContext(ctx, `SELECT pg_backend_pid()`)
			require.NoError(t, err)
			require.True(t, rows.Next())
			var winnerPID int
			require.NoError(t, rows.Scan(&winnerPID))
			require.NoError(t, rows.Close())
			finished := make(chan error, 1)
			go func() {
				_, updateErr := admin.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{Credentials: patch})
				finished <- updateErr
			}()
			// The database barrier proves UpdateAccount completed its initial read
			// and is now waiting on the winner's actual row lock. No timing sleeps.
			for {
				var blocked bool
				require.NoError(t, fixture.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))`, winnerPID).Scan(&blocked))
				if blocked {
					break
				}
				select {
				case updateErr := <-finished:
					t.Fatalf("update returned before row-lock barrier: %v", updateErr)
				default:
				}
				runtime.Gosched()
			}
			winningPatch := maps.Clone(account.Credentials)
			winningPatch["usage_query_token"] = "fixture-winning-rotation"
			winningPatch["concurrent_config"] = "preserved"
			require.NoError(t, fixture.accounts.UpdateCredentials(dbent.NewTxContext(ctx, winner), account.ID, winningPatch))
			_, winningCiphertext, err := readCredentialSecret(ctx, winner.Client(), account.ID)
			require.NoError(t, err)
			require.NoError(t, winner.Commit())
			updateErr := <-finished
			if operation == "nonsecret" {
				require.NoError(t, updateErr)
			} else {
				require.ErrorIs(t, updateErr, service.ErrCredentialSecretConflict)
			}
			loaded, err := fixture.accounts.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, int64(2), service.CredentialVersion(loaded.Credentials))
			require.Equal(t, "fixture-winning-rotation", loaded.Credentials["usage_query_token"])
			require.Equal(t, "preserved", loaded.Credentials["concurrent_config"])
			_, expectedSecrets := service.SplitCredentialSecrets(winningPatch)
			_, actualSecrets := service.SplitCredentialSecrets(loaded.Credentials)
			require.Equal(t, expectedSecrets, actualSecrets)
			_, ciphertext, err := readCredentialSecret(ctx, fixture.db, account.ID)
			require.NoError(t, err)
			require.Equal(t, winningCiphertext, ciphertext, "waiting writer must not rewrite the winner's ciphertext")
			if operation == "nonsecret" {
				require.Equal(t, "waiting-edit", loaded.Credentials["note"])
			}
		})
	}
}
