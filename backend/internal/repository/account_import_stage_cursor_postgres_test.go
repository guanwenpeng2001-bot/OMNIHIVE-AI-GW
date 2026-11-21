package repository

import (
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"testing"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	adminhandler "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func stagedCursorAccount(oauth, withIDs bool) adminhandler.DataAccount {
	account := adminhandler.DataAccount{
		Name: "native-cursor-stage", Platform: service.PlatformCursor, Type: service.AccountTypeAPIKey,
		ProxyDependency: "unknown", Concurrency: 1,
		Credentials: map[string]any{"api_key": "cursor-staged-key", "client_version": "2.0.0", "model_mapping": map[string]any{"alias": "model"}},
	}
	if oauth {
		account.Type = service.AccountTypeOAuth
		delete(account.Credentials, "api_key")
		account.Credentials["access_token"], account.Credentials["refresh_token"] = "cursor-staged-access", "cursor-staged-refresh"
	}
	if withIDs {
		account.Credentials["machine_id"], account.Credentials["mac_machine_id"] = strings.Repeat("ab", 32), strings.Repeat("CD", 32)
	}
	return account
}

func TestStagedImportPostgresNativeCursor(t *testing.T) {
	for _, mode := range []string{"apikey_generate_ids", "apikey_preserve_ids", "oauth_preserve_identity"} {
		t.Run(mode, func(t *testing.T) {
			f := newCredentialSecretFixture(t)
			before := stagedTableCounts(t, f)
			var id int64
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
						for _, secret := range []string{"api_key", "access_token", "refresh_token"} {
							require.NotContains(t, credentials, secret)
						}
					}
					value, err := next.Mutate(ctx, mutation)
					if err == nil && mutation.Op() == dbent.OpCreate {
						created, ok := value.(*dbent.Account)
						if !ok {
							return nil, fmt.Errorf("unexpected account mutation value %T", value)
						}
						id = created.ID
					}
					return value, err
				})
			})
			req := stagedPostgresRequest()
			req.Data.Accounts = []adminhandler.DataAccount{stagedCursorAccount(mode == "oauth_preserve_identity", mode != "apikey_generate_ids")}
			recorder := serveStagedPostgresImport(t, f, req)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Positive(t, id)
			stored := f.client.Account.GetX(f.ctx, id)
			require.Equal(t, "inactive", stored.Status)
			require.False(t, stored.Schedulable)
			require.Nil(t, stored.ProxyID)
			require.Nil(t, stored.ProxyGroupID)
			require.Equal(t, "unknown", stored.Extra[service.StagedProxyDependencyExtraKey])
			require.Equal(t, req.Data.Accounts[0].Credentials["model_mapping"], stored.Credentials["model_mapping"])
			for _, field := range []string{"machine_id", "mac_machine_id"} {
				value, ok := stored.Credentials[field].(string)
				require.True(t, ok)
				decoded, err := hex.DecodeString(value)
				require.NoError(t, err)
				require.Len(t, decoded, 32)
				if mode != "apikey_generate_ids" {
					require.Equal(t, req.Data.Accounts[0].Credentials[field], value)
				}
			}
			metadata, ciphertext, err := readCredentialSecret(f.ctx, f.db, id)
			require.NoError(t, err)
			resolved, err := service.ResolveCredentialSecret(f.ctx, f.encryptor, *metadata, ciphertext)
			require.NoError(t, err)
			var audit string
			require.NoError(t, f.db.QueryRowContext(f.ctx, "SELECT extra::text FROM audit_logs WHERE action='credential_secret.convert' AND extra->>'account_id'=$1", metadata.AccountID).Scan(&audit))
			for _, field := range []string{"api_key", "access_token", "refresh_token"} {
				if secret, exists := req.Data.Accounts[0].Credentials[field]; exists {
					secretText, ok := secret.(string)
					require.True(t, ok)
					require.Equal(t, secret, resolved[field])
					require.NotContains(t, stored.Credentials, field)
					require.NotContains(t, ciphertext, secretText)
					require.NotContains(t, audit, secretText)
					require.NotContains(t, recorder.Body.String(), secretText)
				}
			}
			for _, requireRefresh := range []bool{false, true} {
				page, err := f.accounts.ListOAuthRefreshCandidatePage(f.ctx, service.OAuthRefreshPageOptions{Platforms: []string{service.PlatformCursor}, Limit: 1000, ActiveOnly: true, RequireRefreshToken: requireRefresh})
				require.NoError(t, err)
				for _, candidate := range page.Accounts {
					require.NotEqual(t, id, candidate.ID)
				}
			}
			after := stagedTableCounts(t, f)
			require.Equal(t, before[0]+1, after[0])
			require.Equal(t, before[1]+1, after[1])
			require.Equal(t, before[2], after[2], "no group bindings")
		})
	}
}

func TestStagedImportPostgresCursorRejectsIncompleteAndCachedIdentity(t *testing.T) {
	for _, invalid := range []string{"missing_machine", "invalid_machine", "missing_refresh", "cached_access_token", "cached_expires_at", "relay_fields", "mixed_api_key"} {
		t.Run(invalid, func(t *testing.T) {
			f := newCredentialSecretFixture(t)
			before := stagedTableCounts(t, f)
			writes := 0
			f.client.Account.Use(func(next dbent.Mutator) dbent.Mutator {
				return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
					if mutation.Op() == dbent.OpCreate {
						writes++
					}
					return next.Mutate(ctx, mutation)
				})
			})
			second := stagedCursorAccount(true, true)
			switch invalid {
			case "missing_machine":
				delete(second.Credentials, "machine_id")
			case "invalid_machine":
				second.Credentials["mac_machine_id"] = strings.Repeat("g", 64)
			case "missing_refresh":
				delete(second.Credentials, "refresh_token")
			case "cached_access_token", "cached_expires_at":
				second.Credentials[invalid] = "untrusted-session-cache"
			case "relay_fields":
				second.Credentials["base_url"] = "https://relay.example.invalid"
			case "mixed_api_key":
				second.Credentials["api_key"] = "synthetic-conflicting-key"
			}
			req := stagedPostgresRequest()
			req.Data.Accounts = []adminhandler.DataAccount{stagedCursorAccount(false, false), second}
			recorder := serveStagedPostgresImport(t, f, req)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Zero(t, writes, "incomplete, cached or conflicting authentication fields fail before writing")
			require.Equal(t, before, stagedTableCounts(t, f), "credentials, rows, groups, audit and outbox roll back together")
			require.NotContains(t, recorder.Body.String(), "cursor-staged-")
			require.NotContains(t, recorder.Body.String(), "untrusted-session-cache")
		})
	}
}

func TestStagedImportPostgresCursorRelayPreservesEffectiveBaseURLs(t *testing.T) {
	f := newCredentialSecretFixture(t)
	var ids []int64
	f.client.Account.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
			v, err := next.Mutate(ctx, m)
			if err == nil && m.Op() == dbent.OpCreate {
				created, ok := v.(*dbent.Account)
				if !ok {
					return nil, fmt.Errorf("unexpected account mutation value %T", v)
				}
				ids = append(ids, created.ID)
			}
			return v, err
		})
	})
	req := stagedPostgresRequest()
	req.Data.Accounts = nil
	for _, host := range []string{"relay-a", "relay-b"} {
		req.Data.Accounts = append(req.Data.Accounts, adminhandler.DataAccount{Name: host, Platform: service.PlatformCursor, Type: service.AccountTypeUpstream, ProxyDependency: "unknown", Credentials: map[string]any{"api_key": "shared-relay-key", "base_url": "https://root.example.invalid", "api_protocol": "adaptive", "api_base_urls": map[string]any{"chat_completions": "https://" + host + ".example.invalid"}, "model_mapping": map[string]any{"Alias": "model"}}})
	}
	recorder := serveStagedPostgresImport(t, f, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(t, ids, 2)
	for index, id := range ids {
		stored := f.client.Account.GetX(f.ctx, id)
		require.Equal(t, "inactive", stored.Status)
		require.False(t, stored.Schedulable)
		require.Equal(t, req.Data.Accounts[index].Credentials["api_base_urls"], stored.Credentials["api_base_urls"])
		require.Equal(t, req.Data.Accounts[index].Credentials["model_mapping"], stored.Credentials["model_mapping"])
		require.NotContains(t, stored.Credentials, "api_key")
		metadata, ciphertext, err := readCredentialSecret(f.ctx, f.db, id)
		require.NoError(t, err)
		resolved, err := service.ResolveCredentialSecret(f.ctx, f.encryptor, *metadata, ciphertext)
		require.NoError(t, err)
		require.Equal(t, "shared-relay-key", resolved["api_key"])
	}
	before := stagedTableCounts(t, f)
	duplicate := req.Data.Accounts[0]
	duplicate.Name = "renamed"
	duplicate.Credentials = maps.Clone(duplicate.Credentials)
	duplicate.Credentials["api_base_urls"] = map[string]any{"chat_completions": "HTTPS://RELAY-A.EXAMPLE.INVALID:443/v1/"}
	req.Data.Accounts = append(req.Data.Accounts, duplicate)
	recorder = serveStagedPostgresImport(t, f, req)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, before, stagedTableCounts(t, f))
}
