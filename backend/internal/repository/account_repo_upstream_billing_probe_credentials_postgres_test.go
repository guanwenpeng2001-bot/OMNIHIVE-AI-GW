package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestUpstreamBillingProbeCredentialCASPostgres(t *testing.T) {
	f := newCredentialSecretFixture(t)
	ctx := f.ctx
	snapshot := &service.UpstreamBillingProbeSnapshot{Status: service.UpstreamBillingProbeStatusOK, LastAttemptAt: time.Now().UTC()}
	rate := 0.7

	for _, referenced := range []bool{false, true} {
		mode := "inline"
		if referenced {
			mode = "referenced"
		}
		t.Run(mode, func(t *testing.T) {
			t.Run("persists resolved snapshot and rate", func(t *testing.T) {
				account := createProbeCASAccount(t, f, referenced)
				before := readProbeCASState(t, f, account.ID)
				require.Equal(t, "sk-probe-cas", account.Credentials["api_key"])
				require.NotEmpty(t, account.CredentialRevision)
				if referenced {
					require.NotContains(t, before.credentials, "sk-probe-cas")
					require.Equal(t, credentialRevision(f.storedCredentials(t, account.ID)), account.CredentialRevision)
				} else {
					// Legacy inline callers do not need a read revision.
					account.CredentialRevision = ""
				}
				require.NoError(t, f.accounts.UpdateUpstreamBillingProbeSnapshot(ctx, account, snapshot, &rate))
				after := readProbeCASState(t, f, account.ID)
				require.JSONEq(t, before.credentials, after.credentials)
				require.Equal(t, rate, after.rate)
				require.Equal(t, before.outbox+1, after.outbox)
				loaded, err := f.accounts.GetByID(ctx, account.ID)
				require.NoError(t, err)
				encoded, err := json.Marshal(snapshot)
				require.NoError(t, err)
				got, err := json.Marshal(loaded.Extra[service.UpstreamBillingProbeExtraKey])
				require.NoError(t, err)
				require.JSONEq(t, string(encoded), string(got))
				require.Equal(t, "sk-probe-cas", loaded.Credentials["api_key"])

				// Another in-flight result cannot replace the committed snapshot.
				err = f.accounts.UpdateUpstreamBillingProbeSnapshot(ctx, account, snapshot, &rate)
				require.ErrorIs(t, err, service.ErrUpstreamBillingProbeIdentityChanged)
				require.Equal(t, after, readProbeCASState(t, f, account.ID))

				failedRate := 0.9
				require.NoError(t, f.accounts.UpdateUpstreamBillingProbeSnapshot(ctx, loaded,
					&service.UpstreamBillingProbeSnapshot{Status: service.UpstreamBillingProbeStatusFailed}, &failedRate))
				require.Equal(t, rate, readProbeCASState(t, f, account.ID).rate, "failed probes cannot synchronize rates")
			})

			for _, tt := range []struct {
				name string
				sql  string
			}{
				{"credential configuration", `UPDATE accounts SET credentials = credentials || '{"base_url":"https://changed.example"}'::jsonb WHERE id=$1`},
				{"platform", `UPDATE accounts SET platform='gemini' WHERE id=$1`},
				{"account type", `UPDATE accounts SET type='oauth' WHERE id=$1`},
				{"proxy binding", `UPDATE accounts SET proxy_id=NULL WHERE id=$1`},
				{"proxy endpoint", `UPDATE proxies SET host='changed.example' WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1)`},
				{"proxy status", `UPDATE proxies SET status='disabled' WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1)`},
				{"proxy group", `WITH new_group AS (INSERT INTO proxy_groups(name) VALUES ('probe-cas') RETURNING id) UPDATE accounts SET proxy_id=NULL, proxy_group_id=(SELECT id FROM new_group) WHERE id=$1`},
				{"snapshot", `UPDATE accounts SET extra=extra || '{"upstream_billing_probe":{"status":"failed"}}'::jsonb WHERE id=$1`},
				{"probe switch", `UPDATE accounts SET extra=extra || '{"upstream_billing_probe_enabled":false}'::jsonb WHERE id=$1`},
				{"rate sync switch", `UPDATE accounts SET extra=extra || '{"upstream_billing_rate_sync_enabled":false}'::jsonb WHERE id=$1`},
				{"deleted account", `UPDATE accounts SET deleted_at=NOW() WHERE id=$1`},
			} {
				t.Run("rejects changed "+tt.name, func(t *testing.T) {
					account := createProbeCASAccount(t, f, referenced)
					_, err := f.db.ExecContext(ctx, tt.sql, account.ID)
					require.NoError(t, err)
					before := readProbeCASState(t, f, account.ID)
					err = f.accounts.UpdateUpstreamBillingProbeSnapshot(ctx, account, snapshot, &rate)
					require.ErrorIs(t, err, service.ErrUpstreamBillingProbeIdentityChanged)
					require.Equal(t, before, readProbeCASState(t, f, account.ID), "rejected probes cannot write snapshots, rates or outbox events")
				})
			}
		})
	}

	for _, revoke := range []bool{false, true} {
		name := "rotation"
		if revoke {
			name = "revocation"
		}
		t.Run("rejects "+name, func(t *testing.T) {
			account := createProbeCASAccount(t, f, true)
			version := service.CredentialVersion(account.Credentials)
			if revoke {
				_, err := f.secrets.Revoke(ctx, account.ID, version)
				require.NoError(t, err)
			} else {
				_, err := f.secrets.Rotate(ctx, account.ID, version, map[string]any{"api_key": "sk-rotated"})
				require.NoError(t, err)
			}
			before := readProbeCASState(t, f, account.ID)
			err := f.accounts.UpdateUpstreamBillingProbeSnapshot(ctx, account, snapshot, &rate)
			require.ErrorIs(t, err, service.ErrUpstreamBillingProbeIdentityChanged)
			require.Equal(t, before, readProbeCASState(t, f, account.ID))
			if !revoke {
				fresh, err := f.accounts.GetByID(ctx, account.ID)
				require.NoError(t, err)
				require.Equal(t, "sk-rotated", fresh.Credentials["api_key"])
				require.NoError(t, f.accounts.UpdateUpstreamBillingProbeSnapshot(ctx, fresh, snapshot, &rate))
			}
		})
	}

	// Change only secret metadata, leaving the stored credentials/revision intact.
	// A revision-only CAS would incorrectly accept each of these cases.
	for _, tt := range []struct {
		name string
		sql  string
	}{
		{"secret version", `UPDATE credential_secrets SET version=version+1 WHERE account_id=$1`},
		{"secret revoked", `UPDATE credential_secrets SET revoked_at=NOW() WHERE account_id=$1`},
		{"secret reference", `UPDATE credential_secrets SET reference=reference || '-changed' WHERE account_id=$1`},
		{"secret workspace", `WITH other AS (INSERT INTO workspaces(name) VALUES ('probe-cas-foreign') RETURNING id) UPDATE credential_secrets SET workspace_id=(SELECT id FROM other) WHERE account_id=$1`},
		{"secret owner", `UPDATE credential_secrets SET account_id=$2 WHERE account_id=$1`},
		{"secret missing", `DELETE FROM credential_secrets WHERE account_id=$1`},
	} {
		t.Run("rejects changed "+tt.name, func(t *testing.T) {
			account := createProbeCASAccount(t, f, true)
			args := []any{account.ID}
			if tt.name == "secret owner" {
				args = append(args, createProbeCASAccount(t, f, false).ID)
			}
			_, err := f.db.ExecContext(ctx, tt.sql, args...)
			require.NoError(t, err)
			before := readProbeCASState(t, f, account.ID)
			err = f.accounts.UpdateUpstreamBillingProbeSnapshot(ctx, account, snapshot, &rate)
			require.ErrorIs(t, err, service.ErrUpstreamBillingProbeIdentityChanged)
			require.Equal(t, before, readProbeCASState(t, f, account.ID))
		})
	}

	t.Run("inline key replacement and conversion reject old probe", func(t *testing.T) {
		account := createProbeCASAccount(t, f, false)
		require.NoError(t, f.accounts.UpdateCredentials(ctx, account.ID, map[string]any{"api_key": "sk-new"}))
		before := readProbeCASState(t, f, account.ID)
		require.ErrorIs(t, f.accounts.UpdateUpstreamBillingProbeSnapshot(ctx, account, snapshot, &rate), service.ErrUpstreamBillingProbeIdentityChanged)
		require.Equal(t, before, readProbeCASState(t, f, account.ID))
		fresh, err := f.accounts.GetByID(ctx, account.ID)
		require.NoError(t, err)
		_, err = f.secrets.Convert(ctx, account.ID, f.accountRevision(t, account.ID))
		require.NoError(t, err)
		before = readProbeCASState(t, f, account.ID)
		require.ErrorIs(t, f.accounts.UpdateUpstreamBillingProbeSnapshot(ctx, fresh, snapshot, &rate), service.ErrUpstreamBillingProbeIdentityChanged)
		require.Equal(t, before, readProbeCASState(t, f, account.ID))
	})

	t.Run("workspace scope", func(t *testing.T) {
		account := createProbeCASAccount(t, f, true)
		foreignCtx := domain.WithWorkspaceScope(ctx, f.createWorkspace(t, uniqueName("probe-cas-foreign")))
		before := readProbeCASState(t, f, account.ID)
		err := f.accounts.UpdateUpstreamBillingProbeSnapshot(foreignCtx, account, snapshot, &rate)
		require.ErrorIs(t, err, service.ErrUpstreamBillingProbeIdentityChanged)
		require.Equal(t, before, readProbeCASState(t, f, account.ID))
	})

	for _, commit := range []bool{false, true} {
		name := "rollback"
		if commit {
			name = "commit"
		}
		t.Run("holds locks and publishes atomically on "+name, func(t *testing.T) {
			account := createProbeCASAccount(t, f, true)
			before := readProbeCASState(t, f, account.ID)
			tx, err := f.client.Tx(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			require.NoError(t, f.accounts.UpdateUpstreamBillingProbeSnapshot(dbent.NewTxContext(ctx, tx), account, snapshot, &rate))
			require.Equal(t, before, readProbeCASState(t, f, account.ID), "snapshot, rate and outbox must remain invisible before commit")
			for _, query := range []string{
				`SELECT id FROM proxies WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1) FOR UPDATE NOWAIT`,
				`SELECT id FROM accounts WHERE id=$1 FOR UPDATE NOWAIT`,
				`SELECT id FROM credential_secrets WHERE account_id=$1 FOR UPDATE NOWAIT`,
			} {
				var id int64
				err := f.db.QueryRowContext(ctx, query, account.ID).Scan(&id)
				var pgErr *pq.Error
				require.ErrorAs(t, err, &pgErr)
				require.Equal(t, pq.ErrorCode("55P03"), pgErr.Code, "identity locks must be held until transaction completion")
			}
			if commit {
				require.NoError(t, tx.Commit())
				after := readProbeCASState(t, f, account.ID)
				require.NotEqual(t, before.extra, after.extra)
				require.Equal(t, rate, after.rate)
				require.Equal(t, before.outbox+1, after.outbox)
				// A later rotation proceeds and invalidates the committed result.
				_, err := f.secrets.Rotate(ctx, account.ID, service.CredentialVersion(account.Credentials), map[string]any{"api_key": "sk-after-commit"})
				require.NoError(t, err)
				fresh, err := f.accounts.GetByID(ctx, account.ID)
				require.NoError(t, err)
				require.NotContains(t, fresh.Extra, service.UpstreamBillingProbeExtraKey)
			} else {
				require.NoError(t, tx.Rollback())
				require.Equal(t, before, readProbeCASState(t, f, account.ID))
			}
		})
	}
}

func createProbeCASAccount(t *testing.T, f *credentialSecretFixture, referenced bool) *service.Account {
	t.Helper()
	proxy, err := f.client.Proxy.Create().SetName(uniqueName("probe-cas-proxy")).
		SetProtocol("http").SetHost("127.0.0.1").SetPort(3128).Save(f.ctx)
	require.NoError(t, err)
	initialRate := 1.0
	account := &service.Account{
		Name: uniqueName("probe-cas"), Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Concurrency: 1, RateMultiplier: &initialRate, ProxyID: &proxy.ID,
		Credentials: map[string]any{"api_key": "sk-probe-cas", "base_url": "https://upstream.example"},
		Extra: map[string]any{
			service.UpstreamBillingProbeEnabledExtraKey: true, service.UpstreamBillingRateSyncEnabledExtraKey: true,
		},
	}
	require.NoError(t, f.accounts.Create(f.ctx, account))
	t.Cleanup(func() {
		// Some cases deliberately corrupt secret metadata; do not leave those
		// rows for other suites using the same disposable database.
		for _, query := range []string{
			`DELETE FROM scheduler_outbox WHERE account_id=$1`,
			`DELETE FROM credential_secrets WHERE account_id=$1`,
			`DELETE FROM accounts WHERE id=$1`,
		} {
			_, err := f.db.ExecContext(f.ctx, query, account.ID)
			require.NoError(t, err)
		}
		_, err := f.db.ExecContext(f.ctx, `DELETE FROM proxies WHERE id=$1`, proxy.ID)
		require.NoError(t, err)
	})
	if referenced {
		_, err := f.secrets.Convert(f.ctx, account.ID, f.accountRevision(t, account.ID))
		require.NoError(t, err)
	}
	loaded, err := f.accounts.GetByID(f.ctx, account.ID)
	require.NoError(t, err)
	// Treat fixture creation/conversion events as consumed so a probe's
	// outbox insertion cannot be hidden by an existing deduplication key.
	_, err = f.db.ExecContext(f.ctx, `UPDATE scheduler_outbox SET dedup_key=NULL WHERE account_id=$1`, account.ID)
	require.NoError(t, err)
	return loaded
}

type probeCASState struct {
	credentials string
	extra       string
	rate        float64
	outbox      int
	updatedAt   time.Time
}

func readProbeCASState(t *testing.T, f *credentialSecretFixture, accountID int64) probeCASState {
	t.Helper()
	var state probeCASState
	require.NoError(t, f.db.QueryRowContext(context.Background(), `
		SELECT credentials::text, extra::text, rate_multiplier, updated_at,
			(SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=accounts.id)
		FROM accounts WHERE id=$1`, accountID).
		Scan(&state.credentials, &state.extra, &state.rate, &state.updatedAt, &state.outbox))
	return state
}
