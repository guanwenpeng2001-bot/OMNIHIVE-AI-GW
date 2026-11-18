package repository

import (
	"encoding/json"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testassert"
	"maps"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// Like credential_secret_postgres_test.go, this uses only the explicitly
// supplied CREDENTIAL_SECRET_TEST_DSN and the project's migrations/encryptor.
// Run with -tags=unit to avoid the unrelated integration Redis/TestMain harness.
func TestQueryCredentialsLifecyclePostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	for _, scenario := range []struct {
		platform       string
		fields         []string
		identityFields []string
		observation    string
	}{
		{"dmxapi", []string{"usage_query_token"}, []string{"usage_query_user_id"}, "dmxapi_balance"},
		{"volcengine", []string{"volcengine_access_key_id", "volcengine_secret_access_key"}, []string{"volcengine_seat_id"}, "volcengine_5h_used_percent"},
		{"byteplus", []string{"byteplus_access_key_id", "byteplus_secret_access_key"}, []string{"byteplus_seat_id"}, "byteplus_5h_used_percent"},
		{"hunyuan", []string{"tencent_query_secret_id", "tencent_query_secret_key"}, []string{"tencent_query_team_id", "tencent_query_region"}, "hunyuan_token_plan"},
	} {
		t.Run(scenario.platform, func(t *testing.T) {
			group, err := fixture.client.Group.Create().SetName(uniqueName("query-group")).SetPlatform(scenario.platform).Save(fixture.ctx)
			require.NoError(t, err)
			for _, entrypoint := range []string{"create", "create_with_groups"} {
				t.Run(entrypoint, func(t *testing.T) {
					requestID := uniqueName("query-audit")
					ctx := service.WithConfigurationAuditActor(fixture.ctx, service.ConfigurationAuditActor{Role: "admin", RequestID: requestID})
					credentials := map[string]any{"api_key": "fixture-inference-secret", "account_mode": "coding", "billing_plan": "team", "volcengine_plan_edition": "team"}
					for _, field := range scenario.fields {
						credentials[field] = "fixture-secret-" + field
					}
					for _, field := range scenario.identityFields {
						credentials[field] = "original-identity"
					}
					account := &service.Account{Name: uniqueName("query-account"), Platform: scenario.platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Concurrency: 1, Credentials: maps.Clone(credentials)}
					if entrypoint == "create" {
						err = fixture.accounts.Create(ctx, account)
					} else {
						err = fixture.accounts.CreateWithAccountGroups(ctx, account, []service.AccountGroup{{GroupID: group.ID, Priority: 2}})
					}
					require.NoError(t, err)
					loaded, err := fixture.accounts.GetByID(ctx, account.ID)
					require.NoError(t, err)
					if entrypoint == "create_with_groups" {
						require.Contains(t, loaded.GroupIDs, group.ID)
					}
					reference := loaded.Credentials[service.CredentialSecretReferenceKey]
					require.NotEmpty(t, reference)
					assertStored := func(version int64, secrets map[string]any) *service.Account {
						t.Helper()
						resolved, readErr := fixture.accounts.GetByID(ctx, account.ID)
						require.NoError(t, readErr)
						require.Equal(t, reference, resolved.Credentials[service.CredentialSecretReferenceKey])
						require.Equal(t, version, service.CredentialVersion(resolved.Credentials))
						metadata, ciphertext, readErr := readCredentialSecret(ctx, fixture.db, account.ID)
						require.NoError(t, readErr)
						require.Equal(t, version, metadata.Version)
						decrypted, decryptErr := service.ResolveCredentialSecret(ctx, fixture.encryptor, *metadata, ciphertext)
						require.NoError(t, decryptErr)
						require.Equal(t, secrets, decrypted)
						inline := fixture.storedCredentials(t, account.ID)
						detail := dto.AccountFromService(resolved)
						require.NotEmpty(t, detail.RuntimeIdentity)
						for _, field := range append([]string{"api_key"}, scenario.fields...) {
							require.NotContains(t, inline, field)
							require.Equal(t, secrets[field], resolved.Credentials[field])
							require.Equal(t, secrets[field] != nil, detail.CredentialsStatus["has_"+field])
						}
						var audit string
						require.NoError(t, fixture.db.QueryRowContext(ctx, `SELECT COALESCE(string_agg(extra::text, ''), '') FROM audit_logs WHERE request_id=$1`, requestID).Scan(&audit))
						require.NotEmpty(t, audit)
						for _, projection := range []any{detail, dto.AccountListItemFromAccount(detail), inline} {
							encoded, encodeErr := json.Marshal(projection)
							require.NoError(t, encodeErr)
							for _, field := range append([]string{"api_key"}, scenario.fields...) {
								for _, secret := range []string{testassert.Type[string](t, credentials[field]), "fixture-rotated-secret"} {
									require.NotContains(t, string(encoded), secret)
									require.NotContains(t, audit, secret)
									require.NotContains(t, ciphertext, secret)
								}
							}
						}
						return resolved
					}
					_, expectedSecrets := service.SplitCredentialSecrets(credentials)
					loaded = assertStored(1, expectedSecrets)
					identity := dto.AccountFromService(loaded).RuntimeIdentity
					for _, blank := range []bool{false, true} {
						patch := fixture.storedCredentials(t, account.ID)
						if blank {
							for _, field := range scenario.fields {
								patch[field] = " \t "
							}
						}
						require.NoError(t, fixture.accounts.UpdateCredentials(ctx, account.ID, patch))
						loaded = assertStored(1, expectedSecrets)
						require.Equal(t, identity, dto.AccountFromService(loaded).RuntimeIdentity)
					}
					name := "renamed query account"
					_, err = fixture.accounts.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Name: &name})
					require.NoError(t, err)
					loaded = assertStored(1, expectedSecrets)
					require.Equal(t, identity, dto.AccountFromService(loaded).RuntimeIdentity)
					stale := loaded
					patch := fixture.storedCredentials(t, account.ID)
					patch[scenario.fields[0]] = "fixture-rotated-secret"
					require.NoError(t, fixture.accounts.UpdateCredentials(ctx, account.ID, patch))
					expectedSecrets[scenario.fields[0]] = "fixture-rotated-secret"
					loaded = assertStored(2, expectedSecrets)
					require.NotEqual(t, identity, dto.AccountFromService(loaded).RuntimeIdentity)
					observation := func(amount float64) map[string]any {
						var value any = amount
						if scenario.platform == "hunyuan" {
							value = map[string]any{"unit": "tokens", "remaining": amount}
						}
						return map[string]any{scenario.observation: value}
					}
					require.NoError(t, fixture.accounts.UpdateAccountRuntimeSnapshot(ctx, loaded, observation(31), nil))
					var before, after string
					require.NoError(t, fixture.db.QueryRowContext(ctx, `SELECT to_jsonb(accounts)::text FROM accounts WHERE id=$1`, account.ID).Scan(&before))
					require.ErrorIs(t, fixture.accounts.UpdateAccountRuntimeSnapshot(ctx, stale, observation(99), nil), service.ErrAccountRuntimeIdentityChanged)
					require.NoError(t, fixture.db.QueryRowContext(ctx, `SELECT to_jsonb(accounts)::text FROM accounts WHERE id=$1`, account.ID).Scan(&after))
					require.Equal(t, before, after, "rejected runtime CAS must not change any account state")
					for _, field := range scenario.identityFields {
						t.Run("bulk_"+field, func(t *testing.T) {
							loaded, err = fixture.accounts.GetByID(ctx, account.ID)
							require.NoError(t, err)
							require.NoError(t, fixture.accounts.UpdateAccountRuntimeSnapshot(ctx, loaded, observation(42), nil))
							observed, readErr := fixture.accounts.GetByID(ctx, account.ID)
							require.NoError(t, readErr)
							require.Equal(t, observation(42)[scenario.observation], observed.Extra[scenario.observation])
							count, bulkErr := fixture.accounts.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Credentials: map[string]any{field: "changed-identity"}})
							require.NoError(t, bulkErr)
							require.Equal(t, int64(1), count)
							loaded = assertStored(2, expectedSecrets)
							require.NotContains(t, loaded.Extra, scenario.observation)
						})
					}
					for index, field := range scenario.fields {
						patch = fixture.storedCredentials(t, account.ID)
						patch[field] = nil
						require.NoError(t, fixture.accounts.UpdateCredentials(ctx, account.ID, patch))
						delete(expectedSecrets, field)
						assertStored(int64(3+index), expectedSecrets) // Other query fields and inference secret survive.
					}
				})
			}
		})
	}
}

func TestQueryCredentialsRollbackPostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	group, err := fixture.client.Group.Create().SetName(uniqueName("query-rollback-group")).Save(fixture.ctx)
	require.NoError(t, err)
	for _, failure := range []string{"invalid_group", "late_group_constraint", "create_audit_constraint"} {
		t.Run(failure, func(t *testing.T) {
			requestID := uniqueName("query-rollback")
			ctx := service.WithConfigurationAuditActor(fixture.ctx, service.ConfigurationAuditActor{Role: "admin", RequestID: requestID})
			counts := func() []int64 {
				t.Helper()
				var accounts, secrets, audits, bindings, outbox int64
				require.NoError(t, fixture.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM accounts), (SELECT count(*) FROM credential_secrets), (SELECT count(*) FROM audit_logs), (SELECT count(*) FROM account_groups), (SELECT count(*) FROM scheduler_outbox)`).Scan(&accounts, &secrets, &audits, &bindings, &outbox))
				return []int64{accounts, secrets, audits, bindings, outbox}
			}
			before := counts()
			account := &service.Account{Name: uniqueName("query-rollback"), Platform: "volcengine", Type: service.AccountTypeAPIKey, Status: service.StatusActive, Concurrency: 1, Credentials: quotaCredentialMap()}
			switch failure {
			case "invalid_group":
				err = fixture.accounts.CreateWithAccountGroups(ctx, account, []service.AccountGroup{{GroupID: -1}})
			case "late_group_constraint":
				// The duplicate binding fails after account, secret and audit inserts.
				err = fixture.accounts.CreateWithAccountGroups(ctx, account, []service.AccountGroup{{GroupID: group.ID}, {GroupID: group.ID}})
				require.ErrorContains(t, err, "duplicate key")
			case "create_audit_constraint":
				_, constraintErr := fixture.db.ExecContext(ctx, `ALTER TABLE audit_logs ADD CONSTRAINT query_fixture_audit_failure CHECK (request_id <> '`+requestID+`')`)
				require.NoError(t, constraintErr)
				t.Cleanup(func() {
					_, cleanupErr := fixture.db.ExecContext(fixture.ctx, `ALTER TABLE audit_logs DROP CONSTRAINT query_fixture_audit_failure`)
					require.NoError(t, cleanupErr)
				})
				err = fixture.accounts.Create(ctx, account)
				require.ErrorContains(t, err, "query_fixture_audit_failure")
			}
			require.Error(t, err)
			require.Equal(t, before, counts(), "failed create must roll back account, secret, audit, group and outbox writes")
		})
	}
}

func TestCompatibleBalanceBulkIsolationPostgres(t *testing.T) {
	fixture := newCredentialSecretFixture(t)
	ctx := domain.WithWorkspaceScope(fixture.ctx, domain.DefaultWorkspaceID)
	credentials := map[string]any{"api_key": "fixture-inference", "usage_query_token": "fixture-management", "usage_query_user_id": "original-user", "base_url": "https://www.dmxapi.cn"}
	selected := fixture.createAccount(t, ctx, uniqueName("balance-selected"), "dmxapi", service.AccountTypeAPIKey, maps.Clone(credentials))
	unselected := fixture.createAccount(t, ctx, uniqueName("balance-unselected"), "dmxapi", service.AccountTypeAPIKey, maps.Clone(credentials))
	foreignCtx := domain.WithWorkspaceScope(fixture.ctx, fixture.createWorkspace(t, uniqueName("balance-foreign")))
	foreign := fixture.createAccount(t, foreignCtx, uniqueName("balance-foreign"), "dmxapi", service.AccountTypeAPIKey, maps.Clone(credentials))
	observations := map[string]any{"quota_used": float64(17), "configuration": "keep"}
	for _, key := range service.CompatibleBalanceObservationKeys() {
		observations[key] = "old observation"
	}
	for _, account := range []*service.Account{selected, unselected, foreign} {
		accountCtx := domain.WithWorkspaceScope(fixture.ctx, account.WorkspaceID)
		loaded, err := fixture.accounts.GetByID(accountCtx, account.ID)
		require.NoError(t, err)
		require.NoError(t, fixture.accounts.UpdateAccountRuntimeSnapshot(accountCtx, loaded, observations, nil))
	}
	row := func(accountID int64) string {
		t.Helper()
		var encoded string
		require.NoError(t, fixture.db.QueryRowContext(fixture.ctx, `SELECT to_jsonb(accounts)::text FROM accounts WHERE id=$1`, accountID).Scan(&encoded))
		return encoded
	}
	unselectedBefore, foreignBefore := row(unselected.ID), row(foreign.ID)
	before, err := fixture.accounts.GetByID(ctx, selected.ID)
	require.NoError(t, err)
	// An unchanged identity retains successful observations and valid settings.
	count, err := fixture.accounts.BulkUpdate(ctx, []int64{selected.ID, foreign.ID}, service.AccountBulkUpdate{
		Credentials: map[string]any{"usage_query_user_id": "original-user"}, Extra: map[string]any{"note": "ordinary update"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	unchanged, err := fixture.accounts.GetByID(ctx, selected.ID)
	require.NoError(t, err)
	for key, value := range observations {
		require.Equal(t, value, unchanged.Extra[key])
	}
	require.Equal(t, service.AccountRuntimeIdentityKey(before), service.AccountRuntimeIdentityKey(unchanged))
	// Replayed observation keys in the SAME extra patch must be removed after
	// merging, while ordinary patch values and the stored non-observations stay.
	patch := maps.Clone(observations)
	patch["note"] = "new identity update"
	count, err = fixture.accounts.BulkUpdate(ctx, []int64{selected.ID, foreign.ID}, service.AccountBulkUpdate{
		Credentials: map[string]any{"usage_query_user_id": "new-user"}, Extra: patch,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	after, err := fixture.accounts.GetByID(ctx, selected.ID)
	require.NoError(t, err)
	for _, key := range service.CompatibleBalanceObservationKeys() {
		require.NotContains(t, after.Extra, key)
	}
	require.Equal(t, float64(17), after.Extra["quota_used"])
	require.Equal(t, "keep", after.Extra["configuration"])
	require.Equal(t, "new identity update", after.Extra["note"])
	expectedCredentials := maps.Clone(before.Credentials)
	expectedCredentials["usage_query_user_id"] = "new-user"
	require.Equal(t, expectedCredentials, after.Credentials, "inference/query secrets, version, reference and other config survive")
	require.NotEqual(t, service.AccountRuntimeIdentityKey(before), service.AccountRuntimeIdentityKey(after))
	require.Equal(t, unselectedBefore, row(unselected.ID), "unselected account remains unchanged")
	require.Equal(t, foreignBefore, row(foreign.ID), "explicitly requested foreign-workspace account remains unchanged")
}
