package repository

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestUpdateUpstreamBillingProbeSnapshotRequiresSameIdentityAndSnapshot(t *testing.T) {
	proxyGroupID := int64(41)
	tests := []struct {
		name         string
		affected     int64
		wantErr      error
		proxyGroupID *int64
	}{
		{name: "same identity and snapshot", affected: 1},
		{name: "identity or snapshot changed", affected: 0, wantErr: service.ErrUpstreamBillingProbeIdentityChanged},
		{name: "same logical group identity and snapshot", proxyGroupID: &proxyGroupID, affected: 1},
		{name: "logical group identity or snapshot changed", proxyGroupID: &proxyGroupID, affected: 0, wantErr: service.ErrUpstreamBillingProbeIdentityChanged},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			driver := entsql.OpenDB(dialect.Postgres, db)
			client := dbent.NewClient(dbent.Driver(driver))
			t.Cleanup(func() { _ = client.Close() })

			mock.ExpectBegin()
			tx, err := client.Tx(context.Background())
			require.NoError(t, err)
			// Direct-proxy fixtures use SQL NULL for the group; grouped fixtures use no direct proxy.
			var expectedProxyID any
			if tt.proxyGroupID == nil {
				expectedProxyID = int64(9)
				mock.ExpectQuery(`(?s)` + regexp.QuoteMeta("SELECT protocol, host, port") + `.*` + regexp.QuoteMeta("FOR SHARE")).
					WithArgs(int64(9)).
					WillReturnRows(sqlmock.NewRows([]string{"protocol", "host", "port", "username", "password", "status"}).
						AddRow("http", "127.0.0.1", 3128, "user", "pass", service.StatusActive))
			}
			mock.ExpectExec(`(?s)`+regexp.QuoteMeta("UPDATE accounts")+`.*`+regexp.QuoteMeta("WHERE id = $2")+`.*`+regexp.QuoteMeta("AND platform = $3")+`.*`+regexp.QuoteMeta("AND type = $4")+`.*`+regexp.QuoteMeta("AND credentials = $5::jsonb")+`.*`+regexp.QuoteMeta("AND proxy_id IS NOT DISTINCT FROM $6 AND proxy_group_id IS NOT DISTINCT FROM $11")+`.*`+regexp.QuoteMeta("COALESCE(extra -> 'upstream_billing_probe', 'null'::jsonb) = $7::jsonb")+`.*`+regexp.QuoteMeta("COALESCE(extra -> 'upstream_billing_probe_enabled', 'null'::jsonb) = $8::jsonb")+`.*`+regexp.QuoteMeta("COALESCE(extra -> 'upstream_billing_rate_sync_enabled', 'null'::jsonb) = $9::jsonb")).
				WithArgs(sqlmock.AnyArg(), int64(17), service.PlatformOpenAI, service.AccountTypeAPIKey, `{"api_key":"sk-test","base_url":"http://127.0.0.1:8080"}`, expectedProxyID, `{"status":"stale"}`, "null", "null", nil, tt.proxyGroupID).
				WillReturnResult(sqlmock.NewResult(0, tt.affected))
			if tt.affected > 0 {
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
					WithArgs(service.SchedulerOutboxEventAccountChanged, int64(17), nil, nil, sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))
			}
			repo := newAccountRepositoryWithSQL(client, &recordingSQLExecutor{err: errors.New("must use transaction client")}, nil)
			proxyID := int64(9)
			account := &service.Account{
				ID:       17,
				Platform: service.PlatformOpenAI,
				Type:     service.AccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key":  "sk-test",
					"base_url": "http://127.0.0.1:8080",
				},
				ProxyID: &proxyID,
				Proxy: &service.Proxy{
					ID:       proxyID,
					Protocol: "http",
					Host:     "127.0.0.1",
					Port:     3128,
					Username: "user",
					Password: "pass",
					Status:   service.StatusActive,
				},
				Extra: map[string]any{
					service.UpstreamBillingProbeExtraKey: map[string]any{"status": "stale"},
				},
			}

			account.ProxyGroupID = tt.proxyGroupID
			if tt.proxyGroupID != nil {
				account.ProxyID = nil
				account.Proxy = nil
			}

			txCtx := dbent.NewTxContext(context.Background(), tx)
			err = repo.UpdateUpstreamBillingProbeSnapshot(txCtx, account, &service.UpstreamBillingProbeSnapshot{Status: service.UpstreamBillingProbeStatusOK}, nil)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			mock.ExpectRollback()
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUpdateUpstreamBillingProbeSnapshotCommitsSnapshotAndOutboxAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)`+regexp.QuoteMeta("UPDATE accounts")+`.*`+regexp.QuoteMeta("rate_multiplier = CASE")+`.*`+regexp.QuoteMeta("THEN $10::numeric")+`.*`+regexp.QuoteMeta("AND credentials = $5::jsonb")+`.*`+regexp.QuoteMeta("AND proxy_id IS NOT DISTINCT FROM $6 AND proxy_group_id IS NOT DISTINCT FROM $11")+`.*`+regexp.QuoteMeta("COALESCE(extra -> 'upstream_billing_probe', 'null'::jsonb) = $7::jsonb")).
		WithArgs(sqlmock.AnyArg(), int64(17), service.PlatformOpenAI, service.AccountTypeAPIKey, `{"api_key":"sk-test"}`, nil, "null", "true", "true", 0.065, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(17), nil, nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	account := &service.Account{
		ID:          17,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"},
		Extra: map[string]any{
			service.UpstreamBillingProbeEnabledExtraKey:    true,
			service.UpstreamBillingRateSyncEnabledExtraKey: true,
		},
	}
	rateMultiplier := 0.065

	err = repo.UpdateUpstreamBillingProbeSnapshot(
		context.Background(),
		account,
		&service.UpstreamBillingProbeSnapshot{Status: service.UpstreamBillingProbeStatusOK},
		&rateMultiplier,
	)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateUpstreamBillingProbeSnapshotRejectsChangedProxyIdentity(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	mock.ExpectQuery(`(?s)` + regexp.QuoteMeta("SELECT protocol, host, port") + `.*` + regexp.QuoteMeta("FOR SHARE")).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"protocol", "host", "port", "username", "password", "status"}).
			AddRow("http", "new.example", 3128, "user", "pass", service.StatusActive))

	proxyID := int64(9)
	account := &service.Account{
		ID:          17,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"},
		ProxyID:     &proxyID,
		Proxy: &service.Proxy{
			ID: proxyID, Protocol: "http", Host: "old.example", Port: 3128,
			Username: "user", Password: "pass", Status: service.StatusActive,
		},
	}
	repo := newAccountRepositoryWithSQL(client, db, nil)
	err = repo.UpdateUpstreamBillingProbeSnapshot(dbent.NewTxContext(context.Background(), tx), account, &service.UpstreamBillingProbeSnapshot{Status: service.UpstreamBillingProbeStatusOK}, nil)

	require.ErrorIs(t, err, service.ErrUpstreamBillingProbeIdentityChanged)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateUpstreamBillingProbeSnapshotRollsBackWhenOutboxFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)`+regexp.QuoteMeta("UPDATE accounts")+`.*`+regexp.QuoteMeta("AND proxy_id IS NOT DISTINCT FROM $6 AND proxy_group_id IS NOT DISTINCT FROM $11")+`.*`+regexp.QuoteMeta("COALESCE(extra -> 'upstream_billing_probe', 'null'::jsonb) = $7::jsonb")).
		WithArgs(sqlmock.AnyArg(), int64(18), service.PlatformOpenAI, service.AccountTypeAPIKey, `{"api_key":"sk-test"}`, nil, "null", "true", "true", 0.7, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(18), nil, nil, sqlmock.AnyArg()).
		WillReturnError(errors.New("outbox failed"))
	mock.ExpectRollback()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	account := &service.Account{
		ID:          18,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"},
		Extra: map[string]any{
			service.UpstreamBillingProbeEnabledExtraKey:    true,
			service.UpstreamBillingRateSyncEnabledExtraKey: true,
		},
	}
	rateMultiplier := 0.7

	err = repo.UpdateUpstreamBillingProbeSnapshot(
		context.Background(),
		account,
		&service.UpstreamBillingProbeSnapshot{Status: service.UpstreamBillingProbeStatusOK},
		&rateMultiplier,
	)

	require.EqualError(t, err, "outbox failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

const probeCredentialSecretMatchSQL = `SELECT 1 FROM credential_secrets
	WHERE reference = $1 AND account_id = $2 AND workspace_id = $3
	AND version = $4 AND revoked_at IS NULL FOR SHARE`

func TestUpdateUpstreamBillingProbeSnapshotBindsStoredReferenceCredentials(t *testing.T) {
	outboxErr := errors.New("outbox failed")
	for _, tt := range []struct {
		name      string
		affected  int64
		ambientTx bool
		outboxErr error
		wantErr   error
	}{
		{name: "commit snapshot rate and outbox", affected: 1},
		{name: "preserve caller transaction", affected: 1, ambientTx: true},
		{name: "concurrent snapshot or settings change", wantErr: service.ErrUpstreamBillingProbeIdentityChanged},
		{name: "outbox failure rolls back", affected: 1, outboxErr: outboxErr, wantErr: outboxErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			stored := map[string]any{
				"base_url":                           "https://upstream.example",
				service.CredentialSecretReferenceKey: "probe-reference",
				service.CredentialSecretVersionKey:   int64(3),
			}
			storedJSON, err := json.Marshal(stored)
			require.NoError(t, err)
			account := &service.Account{
				ID: 17, WorkspaceID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
				CredentialRevision: credentialRevision(stored),
				Credentials: map[string]any{
					"base_url":                           "https://upstream.example",
					"api_key":                            "sk-decrypted-runtime-only",
					service.CredentialSecretReferenceKey: "probe-reference",
					service.CredentialSecretVersionKey:   int64(3),
				},
				Proxy: &service.Proxy{ID: 9, Protocol: "http", Host: "127.0.0.1", Port: 3128, Status: service.StatusActive},
				Extra: map[string]any{
					service.UpstreamBillingProbeExtraKey:           map[string]any{"status": "stale"},
					service.UpstreamBillingProbeEnabledExtraKey:    true,
					service.UpstreamBillingRateSyncEnabledExtraKey: true,
				},
			}
			account.ProxyID = &account.Proxy.ID
			ctx := context.Background()
			mock.ExpectBegin()
			var tx *dbent.Tx
			if tt.ambientTx {
				tx, err = client.Tx(ctx)
				require.NoError(t, err)
				ctx = dbent.NewTxContext(ctx, tx)
			}
			// Ordered expectations enforce proxy -> account -> secret locking on
			// the transaction client before any snapshot or outbox write.
			mock.ExpectQuery(`(?s)SELECT protocol, host, port.*FOR SHARE`).WithArgs(int64(9)).
				WillReturnRows(sqlmock.NewRows([]string{"protocol", "host", "port", "username", "password", "status"}).
					AddRow("http", "127.0.0.1", 3128, "", "", service.StatusActive))
			mock.ExpectQuery(`(?s)SELECT workspace_id, credentials, updated_at FROM accounts.*FOR UPDATE`).WithArgs(account.ID).
				WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "credentials", "updated_at"}).AddRow(account.WorkspaceID, storedJSON, time.Now()))
			mock.ExpectQuery(regexp.QuoteMeta(normalizeSQLWhitespace(probeCredentialSecretMatchSQL))).
				WithArgs("probe-reference", account.ID, account.WorkspaceID, int64(3)).
				WillReturnRows(sqlmock.NewRows([]string{"matched"}).AddRow(1))
			mock.ExpectExec(`(?s)UPDATE accounts.*AND credentials = \$5::jsonb.*AND proxy_id IS NOT DISTINCT FROM \$6 AND proxy_group_id IS NOT DISTINCT FROM \$11.*COALESCE\(extra -> 'upstream_billing_probe', 'null'::jsonb\) = \$7::jsonb.*COALESCE\(extra -> 'upstream_billing_probe_enabled', 'null'::jsonb\) = \$8::jsonb.*COALESCE\(extra -> 'upstream_billing_rate_sync_enabled', 'null'::jsonb\) = \$9::jsonb.*AND deleted_at IS NULL`).
				WithArgs(sqlmock.AnyArg(), account.ID, account.Platform, account.Type, string(storedJSON), int64(9), `{"status":"stale"}`, "true", "true", 0.7, nil).
				WillReturnResult(sqlmock.NewResult(0, tt.affected))
			if tt.affected > 0 {
				expectation := mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
					WithArgs(service.SchedulerOutboxEventAccountChanged, account.ID, nil, nil, sqlmock.AnyArg())
				if tt.outboxErr != nil {
					expectation.WillReturnError(tt.outboxErr)
				} else {
					expectation.WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}
			if !tt.ambientTx {
				if tt.wantErr != nil {
					mock.ExpectRollback()
				} else {
					mock.ExpectCommit()
				}
			}
			repo := newAccountRepositoryWithSQL(client, &recordingSQLExecutor{err: errors.New("must use transaction client")}, nil)
			rate := 0.7
			err = repo.UpdateUpstreamBillingProbeSnapshot(ctx, account, &service.UpstreamBillingProbeSnapshot{Status: service.UpstreamBillingProbeStatusOK}, &rate)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, "sk-decrypted-runtime-only", account.Credentials["api_key"], "CAS must not mutate the runtime account")
			if tt.ambientTx {
				mock.ExpectCommit()
				require.NoError(t, tx.Commit())
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUpdateUpstreamBillingProbeSnapshotRejectsStaleReferenceCredentials(t *testing.T) {
	for _, tt := range []struct {
		name        string
		mutate      func(*service.Account, map[string]any)
		secretGone  bool
		accountGone bool
	}{
		{name: "missing read revision", mutate: func(a *service.Account, _ map[string]any) { a.CredentialRevision = "" }},
		{name: "nonsecret configuration changed", mutate: func(_ *service.Account, stored map[string]any) { stored["base_url"] = "https://changed.example" }},
		{name: "rotation changed storage revision", mutate: func(_ *service.Account, stored map[string]any) { stored[service.CredentialSecretVersionKey] = 4 }},
		{name: "runtime version differs", mutate: func(a *service.Account, _ map[string]any) { a.Credentials[service.CredentialSecretVersionKey] = 2 }},
		{name: "runtime version missing", mutate: func(a *service.Account, _ map[string]any) { delete(a.Credentials, service.CredentialSecretVersionKey) }},
		{name: "runtime reference differs", mutate: func(a *service.Account, _ map[string]any) {
			a.Credentials[service.CredentialSecretReferenceKey] = "other-reference"
		}},
		{name: "account workspace differs", mutate: func(a *service.Account, _ map[string]any) { a.WorkspaceID = 8 }},
		{name: "secret absent revoked or changed", secretGone: true},
		{name: "account deleted or outside scope", accountGone: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			stored := map[string]any{
				"base_url": "https://upstream.example", service.CredentialSecretReferenceKey: "probe-reference", service.CredentialSecretVersionKey: 3,
			}
			account := &service.Account{
				ID: 17, WorkspaceID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
				CredentialRevision: credentialRevision(stored),
				Credentials: map[string]any{
					"api_key": "sk-runtime", "base_url": "https://upstream.example", service.CredentialSecretReferenceKey: "probe-reference", service.CredentialSecretVersionKey: 3,
				},
			}
			if tt.mutate != nil {
				tt.mutate(account, stored)
			}
			storedJSON, err := json.Marshal(stored)
			require.NoError(t, err)
			mock.ExpectBegin()
			rows := sqlmock.NewRows([]string{"workspace_id", "credentials", "updated_at"})
			if !tt.accountGone {
				rows.AddRow(int64(7), storedJSON, time.Now())
			}
			mock.ExpectQuery(`(?s)SELECT workspace_id, credentials, updated_at FROM accounts.*FOR UPDATE`).WithArgs(account.ID).WillReturnRows(rows)
			if tt.secretGone {
				mock.ExpectQuery(regexp.QuoteMeta(normalizeSQLWhitespace(probeCredentialSecretMatchSQL))).
					WithArgs("probe-reference", account.ID, int64(7), int64(3)).
					WillReturnRows(sqlmock.NewRows([]string{"matched"}))
			}
			mock.ExpectRollback() // No UPDATE or outbox may be attempted on a mismatch.
			repo := newAccountRepositoryWithSQL(client, db, nil)
			err = repo.UpdateUpstreamBillingProbeSnapshot(context.Background(), account, &service.UpstreamBillingProbeSnapshot{Status: service.UpstreamBillingProbeStatusOK}, nil)
			require.ErrorIs(t, err, service.ErrUpstreamBillingProbeIdentityChanged)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
