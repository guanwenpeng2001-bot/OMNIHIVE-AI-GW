package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	adminhandler "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/pagination"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Count queries only, never SQL arguments, ciphertext or decrypted secrets.
type credentialBatchCountingDriver struct {
	*entsql.Driver
	queries, secretQueries atomic.Int32
	secretReadError        error
	beforeSecretRead       func()
	beforeAccountRead      func()
}

func (d *credentialBatchCountingDriver) Query(ctx context.Context, query string, args, value any) error {
	d.queries.Add(1)
	if strings.Contains(query, `FROM "accounts"`) && d.beforeAccountRead != nil {
		hook := d.beforeAccountRead
		d.beforeAccountRead = nil
		hook()
	}
	return d.Driver.Query(ctx, query, args, value)
}
func (d *credentialBatchCountingDriver) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	d.queries.Add(1)
	if strings.Contains(query, "FROM credential_secrets") {
		d.secretQueries.Add(1)
		if d.beforeSecretRead != nil {
			hook := d.beforeSecretRead
			d.beforeSecretRead = nil
			hook()
		}
		if d.secretReadError != nil {
			return nil, d.secretReadError
		}
	}
	return d.Driver.QueryContext(ctx, query, args...)
}
func (d *credentialBatchCountingDriver) reset() { d.queries.Store(0); d.secretQueries.Store(0) }

func countingCredentialRepository(f *credentialSecretFixture) (*accountRepository, *credentialBatchCountingDriver) {
	driver := &credentialBatchCountingDriver{Driver: entsql.OpenDB(dialect.Postgres, f.db)}
	client := dbent.NewClient(dbent.Driver(driver))
	repo := newAccountRepositoryWithSQL(client, f.db, nil)
	repo.credentialEncryptor = f.encryptor
	return repo, driver
}

func createBatchReferencedAccount(t *testing.T, f *credentialSecretFixture, platform, kind string, credentials map[string]any) *service.Account {
	t.Helper()
	account := f.createAccount(t, f.ctx, uniqueName("batch-reference"), platform, kind, credentials)
	_, err := f.secrets.Convert(f.ctx, account.ID, f.accountRevision(t, account.ID))
	require.NoError(t, err)
	return account
}

func TestCredentialBatchPostgresOrderQueriesRevisionAndCAS(t *testing.T) {
	f := newCredentialSecretFixture(t)
	f.ctx = domain.WithWorkspaceScope(f.ctx, f.createWorkspace(t, uniqueName("batch-read")))
	accounts := make([]*service.Account, 0, 24)
	for i := 0; i < 24; i++ {
		accounts = append(accounts, createBatchReferencedAccount(t, f, service.PlatformCursor, service.AccountTypeOAuth, map[string]any{"access_token": fmt.Sprintf("batch-access-%d", i), "refresh_token": fmt.Sprintf("batch-refresh-%d", i), "model_mapping": map[string]any{"alias": "model"}}))
	}
	inline := f.createLegacyInlineAccount(t, f.ctx, uniqueName("inline-control"), service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"refresh_token": "inline-refresh-control"})
	repo, counts := countingCredentialRepository(f)
	for _, size := range []int{1, len(accounts)} {
		ids := []int64{-1, 0, 999999999, accounts[size-1].ID}
		for i := size - 1; i >= 0; i-- {
			ids = append(ids, accounts[i].ID)
		}
		ids = append(ids, inline.ID)
		counts.reset()
		out, err := repo.GetByIDs(f.ctx, ids)
		require.NoError(t, err)
		require.Len(t, out, size+1)
		require.EqualValues(t, 1, counts.secretQueries.Load())
		require.EqualValues(t, 3, counts.queries.Load(), "one account query, one group query, one secret batch regardless of row count")
		for i := 0; i < size; i++ {
			original := accounts[size-1-i]
			require.Equal(t, original.ID, out[i].ID)
			require.Equal(t, original.Credentials["access_token"], out[i].Credentials["access_token"])
			require.Equal(t, original.Credentials["refresh_token"], out[i].Credentials["refresh_token"])
			require.Equal(t, credentialRevision(f.storedCredentials(t, original.ID)), out[i].CredentialRevision)
			single, err := f.accounts.GetByID(f.ctx, original.ID)
			require.NoError(t, err)
			require.Equal(t, single.Credentials, out[i].Credentials)
			require.Equal(t, single.CredentialRevision, out[i].CredentialRevision)
		}
		require.Equal(t, inline.ID, out[size].ID)
		require.Equal(t, credentialRevision(f.storedCredentials(t, inline.ID)), out[size].CredentialRevision)
	}
	counts.reset()
	_, err := repo.GetByIDs(f.ctx, []int64{inline.ID, inline.ID})
	require.NoError(t, err)
	require.Zero(t, counts.secretQueries.Load())
	require.EqualValues(t, 2, counts.queries.Load())
	counts.reset()
	out, err := repo.GetByIDs(f.ctx, []int64{0, -1})
	require.NoError(t, err)
	require.Empty(t, out)
	require.Zero(t, counts.queries.Load())
	loaded, err := repo.GetByIDs(f.ctx, []int64{accounts[0].ID})
	require.NoError(t, err)
	stale := *loaded[0]
	updated := maps.Clone(loaded[0].Credentials)
	updated["access_token"] = "batch-rotated-access"
	require.NoError(t, repo.UpdateCredentialsCAS(f.ctx, loaded[0], updated))
	fresh, err := repo.GetByID(f.ctx, accounts[0].ID)
	require.NoError(t, err)
	require.Equal(t, "batch-rotated-access", fresh.Credentials["access_token"])
	require.EqualValues(t, 2, service.CredentialVersion(fresh.Credentials))
	require.ErrorIs(t, repo.UpdateCredentialsCAS(f.ctx, &stale, updated), service.ErrCredentialSecretConflict)

	// The same bulk read joins the caller's transaction and sees uncommitted
	// rotated rows without opening another connection or publishing plaintext.
	tx, err := f.client.Tx(f.ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(f.ctx, tx)
	newer := maps.Clone(fresh.Credentials)
	newer["access_token"] = "transaction-only-access"
	require.NoError(t, f.accounts.UpdateCredentialsCAS(txCtx, fresh, newer))
	inside, err := repo.GetByIDs(txCtx, []int64{fresh.ID})
	require.NoError(t, err)
	require.Equal(t, "transaction-only-access", inside[0].Credentials["access_token"])
	require.NoError(t, tx.Rollback())
	after, err := repo.GetByIDs(f.ctx, []int64{fresh.ID})
	require.NoError(t, err)
	require.Equal(t, "batch-rotated-access", after[0].Credentials["access_token"])
}

func TestCredentialBatchPostgresScopeOwnershipCorruptionAndRace(t *testing.T) {
	for _, failure := range []string{"revoked", "missing", "ciphertext", "reference", "workspace", "version", "missing_version", "ciphertext_swap", "payload_injection"} {
		t.Run(failure, func(t *testing.T) {
			f := newCredentialSecretFixture(t)
			workspace := f.createWorkspace(t, uniqueName("batch-failure"))
			f.ctx = domain.WithWorkspaceScope(f.ctx, workspace)
			good := createBatchReferencedAccount(t, f, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"refresh_token": "good-batch-refresh"})
			bad := createBatchReferencedAccount(t, f, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"refresh_token": "bad-batch-refresh"})
			metadata, ciphertext, err := readCredentialSecret(f.ctx, f.db, bad.ID)
			require.NoError(t, err)
			switch failure {
			case "revoked":
				_, err = f.secrets.Revoke(f.ctx, bad.ID, 1)
			case "missing":
				_, err = f.db.ExecContext(f.ctx, "DELETE FROM credential_secrets WHERE account_id=$1", bad.ID)
			case "ciphertext":
				f.writeInconsistentSecret(t, "UPDATE credential_secrets SET ciphertext='broken-ciphertext' WHERE account_id=$1", bad.ID)
			case "reference":
				_, err = f.db.ExecContext(f.ctx, "UPDATE credential_secrets SET reference=$2 WHERE account_id=$1", bad.ID, uniqueName("unbound-fixture-reference"))
			case "workspace":
				other := f.createWorkspace(t, uniqueName("foreign-batch"))
				_, err = f.db.ExecContext(f.ctx, "UPDATE credential_secrets SET workspace_id=$2 WHERE account_id=$1", bad.ID, other)
			case "version":
				_, err = f.db.ExecContext(f.ctx, "UPDATE credential_secrets SET version=version+1 WHERE account_id=$1", bad.ID)
			case "missing_version":
				_, err = f.db.ExecContext(f.ctx, "UPDATE accounts SET credentials=credentials-'credential_secret_version' WHERE id=$1", bad.ID)
			case "ciphertext_swap":
				_, ciphertext, err = readCredentialSecret(f.ctx, f.db, good.ID)
				require.NoError(t, err)
				f.writeInconsistentSecret(t, "UPDATE credential_secrets SET ciphertext=$2 WHERE account_id=$1", bad.ID, ciphertext)
			case "payload_injection":
				ciphertext, err = service.EncryptCredentialSecret(f.encryptor, *metadata, map[string]any{"refresh_token": "bad-batch-refresh", "credential_secret_reference": "injected"})
				require.NoError(t, err)
				f.writeInconsistentSecret(t, "UPDATE credential_secrets SET ciphertext=$2 WHERE account_id=$1", bad.ID, ciphertext)
			}
			require.NoError(t, err)
			out, err := f.accounts.GetByIDs(f.ctx, []int64{good.ID, bad.ID})
			require.Error(t, err)
			require.Nil(t, out, "no partial general-read result")
			require.NotContains(t, err.Error(), "bad-batch-refresh")
			require.NotContains(t, err.Error(), ciphertext)
			_, singleErr := f.accounts.GetByID(f.ctx, bad.ID)
			require.Error(t, singleErr)
			omitted, err := f.accounts.GetByIDs(service.WithUnusableCredentialRowsOmitted(f.ctx), []int64{good.ID, bad.ID})
			require.NoError(t, err)
			require.Len(t, omitted, 1)
			require.Equal(t, good.ID, omitted[0].ID)
			listed, _, err := f.accounts.ListWithFilters(f.ctx, pagination.PaginationParams{Page: 1, PageSize: 20}, "", "", "", "", 0, "")
			require.NoError(t, err, "one unusable credential must not fail the whole list")
			require.Equal(t, []int64{good.ID}, listedAccountIDs(listed))
			listed, _, err = f.accounts.ListWithFilters(service.WithUnresolvedCredentialRows(f.ctx), pagination.PaginationParams{Page: 1, PageSize: 20}, "", "", "", "", 0, "")
			require.NoError(t, err)
			require.ElementsMatch(t, []int64{good.ID, bad.ID}, listedAccountIDs(listed))
			for _, account := range listed {
				if account.ID != bad.ID {
					require.False(t, account.CredentialUnavailable)
					continue
				}
				require.True(t, account.CredentialUnavailable)
				for field, value := range account.Credentials {
					require.False(t, service.IsSensitiveCredentialKey(field), "unresolved row exposes %s", field)
					require.NotContains(t, fmt.Sprint(value), "bad-batch-refresh")
				}
			}
			for _, activeOnly := range []bool{true, false} {
				page, err := f.accounts.ListOAuthRefreshCandidatePage(f.ctx, service.OAuthRefreshPageOptions{Platforms: []string{service.PlatformOpenAI}, Limit: 10, ActiveOnly: activeOnly, RequireRefreshToken: true})
				require.NoError(t, err)
				require.Len(t, page.Accounts, 1)
				require.Equal(t, good.ID, page.Accounts[0].ID)
				require.Equal(t, bad.ID, page.NextAfterID)
			}
		})
	}
	t.Run("scope and race", func(t *testing.T) {
		f := newCredentialSecretFixture(t)
		local := createBatchReferencedAccount(t, f, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"refresh_token": "local-refresh"})
		foreignCtx := domain.WithWorkspaceScope(f.ctx, f.createWorkspace(t, uniqueName("scope-batch")))
		original := f.ctx
		f.ctx = foreignCtx
		foreign := createBatchReferencedAccount(t, f, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"refresh_token": "foreign-refresh"})
		f.ctx = original
		out, err := f.accounts.GetByIDs(foreignCtx, []int64{local.ID, foreign.ID})
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, foreign.ID, out[0].ID)
		out, err = f.accounts.GetByIDs(f.ctx, []int64{foreign.ID, local.ID})
		require.NoError(t, err)
		require.Len(t, out, 2)
		require.Equal(t, []int64{foreign.ID, local.ID}, []int64{out[0].ID, out[1].ID})
		repo, driver := countingCredentialRepository(f)
		driver.beforeSecretRead = func() {
			_, err := f.secrets.Rotate(f.ctx, local.ID, 1, map[string]any{"refresh_token": "rotated-between-queries"})
			require.NoError(t, err)
		}
		out, err = repo.GetByIDs(f.ctx, []int64{local.ID})
		require.ErrorIs(t, err, service.ErrCredentialSecretConflict)
		require.Nil(t, out)
		out, err = repo.GetByIDs(f.ctx, []int64{local.ID})
		require.NoError(t, err)
		require.Equal(t, "rotated-between-queries", out[0].Credentials["refresh_token"])
		driver.secretReadError = errors.New("fixture database unavailable")
		page, err := repo.ListOAuthRefreshCandidatePage(f.ctx, service.OAuthRefreshPageOptions{Platforms: []string{service.PlatformOpenAI}, AfterID: local.ID - 1, Limit: 1, ActiveOnly: true, RequireRefreshToken: true})
		require.ErrorIs(t, err, driver.secretReadError)
		require.Nil(t, page)
	})
}

func TestCredentialBatchPostgresRedisAndSelectedExport(t *testing.T) {
	f := newCredentialSecretFixture(t)
	a := createBatchReferencedAccount(t, f, service.PlatformCursor, service.AccountTypeAPIKey, map[string]any{"api_key": "reference-api-canary", "base_url": "https://example.invalid", "model_mapping": map[string]any{"Alias": "model"}})
	b := createBatchReferencedAccount(t, f, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"access_token": "reference-access-canary", "refresh_token": "reference-refresh-canary"})
	out, err := f.accounts.GetByIDs(f.ctx, []int64{b.ID, a.ID})
	require.NoError(t, err)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := NewSchedulerCache(rdb)
	for _, account := range out {
		require.NoError(t, cache.SetAccount(f.ctx, account))
		for _, key := range []string{schedulerAccountKey(fmt.Sprint(account.ID)), schedulerAccountMetaKey(fmt.Sprint(account.ID))} {
			raw, err := rdb.Get(f.ctx, key).Result()
			require.NoError(t, err)
			for _, secret := range []string{"reference-api-canary", "reference-access-canary", "reference-refresh-canary"} {
				require.NotContains(t, raw, secret)
			}
			var decoded service.Account
			require.NoError(t, json.Unmarshal([]byte(raw), &decoded))
			reference, ok := service.CredentialReference(decoded.Credentials)
			require.True(t, ok)
			require.Equal(t, account.Credentials[service.CredentialSecretReferenceKey], reference)
			require.EqualValues(t, 1, service.CredentialVersion(decoded.Credentials))
		}
	}
	// Explicit admin backup export intentionally returns resolved credentials;
	// unlike scheduler serialization it must not silently export unusable refs.
	gin.SetMode(gin.TestMode)
	h := adminhandler.NewAccountHandler(queryClearAdmin(f), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/api/v1/admin/accounts/data", h.ExportData)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/accounts/data?ids=%d,%d,%d&include_proxies=false", b.ID, a.ID, b.ID), nil).WithContext(f.ctx)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Data struct {
			Accounts []adminhandler.DataAccount `json:"accounts"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Len(t, payload.Data.Accounts, 2)
	require.Equal(t, "reference-refresh-canary", payload.Data.Accounts[0].Credentials["refresh_token"])
	require.Equal(t, "reference-api-canary", payload.Data.Accounts[1].Credentials["api_key"])
}

func TestCredentialBatchPostgresRefreshPagesActivationAndSkippedRows(t *testing.T) {
	f := newCredentialSecretFixture(t)
	f.ctx = domain.WithWorkspaceScope(f.ctx, f.createWorkspace(t, uniqueName("candidate-batch")))
	create := func(credentials map[string]any) *service.Account {
		return createBatchReferencedAccount(t, f, service.PlatformCursor, service.AccountTypeOAuth, credentials)
	}
	noRefresh := create(map[string]any{"access_token": "no-refresh-access"})
	blankRefresh := create(map[string]any{"access_token": "blank-refresh-access", "refresh_token": "  "})
	wrongType := create(map[string]any{"access_token": "wrong-type-access", "refresh_token": 123})
	revoked := create(map[string]any{"refresh_token": "revoked-refresh"})
	_, err := f.secrets.Revoke(f.ctx, revoked.ID, 1)
	require.NoError(t, err)
	broken := create(map[string]any{"refresh_token": "broken-refresh"})
	f.writeInconsistentSecret(t, "UPDATE credential_secrets SET ciphertext='bad' WHERE account_id=$1", broken.ID)
	paused := create(map[string]any{"refresh_token": "paused-refresh"})
	_, err = f.client.Account.UpdateOneID(paused.ID).SetStatus("inactive").Save(f.ctx)
	require.NoError(t, err)
	cooldown := create(map[string]any{"refresh_token": "cooldown-refresh"})
	_, err = f.client.Account.UpdateOneID(cooldown.ID).SetTempUnschedulableUntil(time.Now().Add(time.Hour)).SetTempUnschedulableReason("token refresh retry exhausted: fixture").Save(f.ctx)
	require.NoError(t, err)
	good := create(map[string]any{"refresh_token": "good-refresh"})
	_, err = f.client.Account.UpdateOneID(good.ID).SetSchedulable(false).Save(f.ctx)
	require.NoError(t, err)
	inline := f.createLegacyInlineAccount(t, f.ctx, uniqueName("legacy-refresh"), service.PlatformCursor, service.AccountTypeOAuth, map[string]any{"refresh_token": "inline-refresh"})
	legacyMissing := f.createLegacyInlineAccount(t, f.ctx, uniqueName("legacy-missing"), service.PlatformCursor, service.AccountTypeOAuth, map[string]any{"access_token": "legacy-only-access"})
	options := service.OAuthRefreshPageOptions{Platforms: []string{service.PlatformCursor}, Limit: 2, ActiveOnly: true, RequireRefreshToken: true, ExcludeRetryCooldown: true}
	var found []int64
	emptyPages := 0
	for scans := 0; scans < 20; scans++ {
		page, err := f.accounts.ListOAuthRefreshCandidatePage(f.ctx, options)
		require.NoError(t, err)
		if len(page.Accounts) == 0 && page.HasMore {
			emptyPages++
		}
		for _, a := range page.Accounts {
			require.Greater(t, a.ID, options.AfterID)
			require.NotEmpty(t, a.Credentials["refresh_token"])
			found = append(found, a.ID)
		}
		if page.NextAfterID > 0 {
			require.Greater(t, page.NextAfterID, options.AfterID)
		}
		if !page.HasMore {
			break
		}
		options.AfterID = page.NextAfterID
	}
	require.Positive(t, emptyPages, "a fully filtered page must still advance")
	require.Equal(t, []int64{good.ID, inline.ID}, found)
	_, err = f.client.Account.UpdateOneID(paused.ID).SetStatus(service.StatusActive).Save(f.ctx)
	require.NoError(t, err)
	options.AfterID = paused.ID - 1
	options.Limit = 1
	page, err := f.accounts.ListOAuthRefreshCandidatePage(f.ctx, options)
	require.NoError(t, err)
	require.Len(t, page.Accounts, 1)
	require.Equal(t, paused.ID, page.Accounts[0].ID)
	require.Equal(t, "paused-refresh", page.Accounts[0].Credentials["refresh_token"])
	options.AfterID = 0
	options.Limit = 100
	options.RequireRefreshToken = false
	page, err = f.accounts.ListOAuthRefreshCandidatePage(f.ctx, options)
	require.NoError(t, err)
	ids := make([]int64, 0, len(page.Accounts))
	for _, a := range page.Accounts {
		ids = append(ids, a.ID)
	}
	require.Equal(t, []int64{noRefresh.ID, blankRefresh.ID, wrongType.ID, paused.ID, good.ID, inline.ID, legacyMissing.ID}, ids, "reconciliation keeps no-refresh rows but never unusable references")
	options.RequireRefreshToken = true
	options.ExcludeRetryCooldown = false
	page, err = f.accounts.ListOAuthRefreshCandidatePage(f.ctx, options)
	require.NoError(t, err)
	ids = nil
	for _, a := range page.Accounts {
		ids = append(ids, a.ID)
	}
	require.Contains(t, ids, cooldown.ID)
	// One connection proves the scan cursor is closed before bulk hydration.
	f.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	_, err = f.accounts.ListOAuthRefreshCandidatePage(ctx, options)
	require.NoError(t, err)
}

func TestCredentialBatchPostgresStagedActivationAndConcurrentEligibility(t *testing.T) {
	f := newCredentialSecretFixture(t)
	f.ctx = domain.WithWorkspaceScope(f.ctx, f.createWorkspace(t, uniqueName("activation-batch")))
	staged := &service.Account{Name: uniqueName("inactive-reference"), Platform: service.PlatformCursor, Type: service.AccountTypeOAuth, Status: "inactive", Schedulable: false,
		Credentials: map[string]any{"access_token": "staged-access-fixture", "refresh_token": "staged-refresh-fixture"}}
	err := service.NewAuthPendingIdentityService(f.client).RunAccountWriteTransaction(service.WithStagedAccountImport(f.ctx), func(ctx context.Context) error { return f.accounts.Create(ctx, staged) })
	require.NoError(t, err)
	options := service.OAuthRefreshPageOptions{Platforms: []string{service.PlatformCursor}, Limit: 1, ActiveOnly: true, RequireRefreshToken: true, ExcludeRetryCooldown: true}
	page, err := f.accounts.ListOAuthRefreshCandidatePage(f.ctx, options)
	require.NoError(t, err)
	require.Empty(t, page.Accounts)
	require.Zero(t, page.NextAfterID)
	_, err = f.client.Account.UpdateOneID(staged.ID).SetStatus(service.StatusActive).Save(f.ctx)
	require.NoError(t, err)
	repo, driver := countingCredentialRepository(f)
	driver.reset()
	page, err = repo.ListOAuthRefreshCandidatePage(f.ctx, options)
	require.NoError(t, err)
	require.Len(t, page.Accounts, 1)
	require.Equal(t, staged.ID, page.Accounts[0].ID)
	require.Equal(t, "staged-refresh-fixture", page.Accounts[0].Credentials["refresh_token"])
	require.EqualValues(t, 1, driver.secretQueries.Load())
	require.EqualValues(t, 3, driver.queries.Load(), "ID scan is separate; hydration stays one account/group/secret batch")
	require.Equal(t, credentialRevision(f.storedCredentials(t, staged.ID)), page.Accounts[0].CredentialRevision)
	// Revocation discovered during hydration is omitted while the raw ID still
	// advances. A rotation would similarly cause a version conflict for this page.
	driver.beforeSecretRead = func() { _, err := f.secrets.Revoke(f.ctx, staged.ID, 1); require.NoError(t, err) }
	page, err = repo.ListOAuthRefreshCandidatePage(f.ctx, options)
	require.NoError(t, err)
	require.Empty(t, page.Accounts)
	require.Equal(t, staged.ID, page.NextAfterID)
	require.True(t, page.HasMore)
	options.AfterID = page.NextAfterID
	page, err = repo.ListOAuthRefreshCandidatePage(f.ctx, options)
	require.NoError(t, err)
	require.False(t, page.HasMore)
	require.Zero(t, page.NextAfterID)

	for _, change := range []string{"inactive", "cooldown", "deleted", "platform", "type"} {
		t.Run(change, func(t *testing.T) {
			account := createBatchReferencedAccount(t, f, service.PlatformCursor, service.AccountTypeOAuth, map[string]any{"refresh_token": "race-refresh-fixture"})
			opts := options
			opts.AfterID = account.ID - 1
			driver.beforeAccountRead = func() {
				update := f.client.Account.UpdateOneID(account.ID)
				switch change {
				case "inactive":
					update.SetStatus("inactive")
				case "cooldown":
					update.SetTempUnschedulableUntil(time.Now().Add(time.Hour)).SetTempUnschedulableReason("token refresh retry exhausted: concurrent")
				case "deleted":
					update.SetDeletedAt(time.Now())
				case "platform":
					update.SetPlatform(service.PlatformOpenAI)
				case "type":
					update.SetType(service.AccountTypeAPIKey)
				}
				_, err := update.Save(f.ctx)
				require.NoError(t, err)
			}
			page, err := repo.ListOAuthRefreshCandidatePage(f.ctx, opts)
			require.NoError(t, err)
			require.Empty(t, page.Accounts)
			require.Equal(t, account.ID, page.NextAfterID)
		})
	}
}

func listedAccountIDs(accounts []service.Account) []int64 {
	ids := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.ID)
	}
	return ids
}
