//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountModelSelectionPreservationLockedMerge(t *testing.T) {
	for _, billing := range []bool{false, true} {
		for _, clear := range []bool{false, true} {
			t.Run(map[bool]string{false: "update", true: "billing"}[billing]+map[bool]string{false: "/selection", true: "/deny_all"}[clear], func(t *testing.T) {
				ctx := context.Background()
				repo, client, _ := newCursorEditTestRepository(t)
				before := map[string]any{"model_mapping": map[string]any{"old": "old"}, "model_whitelist": []any{"old"}, "compact_model_mapping": map[string]any{"old": "old"}, "site": "cn"}
				entity, err := client.Account.Create().SetName("before").SetPlatform(service.PlatformWorkBuddy).
					SetType(service.AccountTypeOAuth).SetStatus(service.StatusActive).SetCredentials(before).Save(ctx)
				require.NoError(t, err)
				stale := accountEntityToService(entity)
				stale.Name = "renamed"
				stale.Credentials = map[string]any{"site": "intl"} // Full auth object omitted model fields.
				ctx = service.WithAccountCredentialChanges(service.WithAccountModelSelectionPreserved(ctx, false), before, stale.Credentials)
				mapping := map[string]any{"Alias": "new-target"}
				if clear {
					mapping = map[string]any{}
				}
				current := map[string]any{"model_mapping": mapping, "model_whitelist": []any{"new-target"}, "compact_model_mapping": map[string]any{"compact": "new-target"}, "site": "cn"}
				scope := service.AccountModelAllowlist{Enabled: true, Models: []string{"new-target"}}
				if clear {
					scope.Models = []string{}
				}
				// Selection commit lands after the admin read and before its row lock.
				_, err = client.Account.UpdateOneID(entity.ID).SetCredentials(current).SetExtra(map[string]any{
					service.UpstreamModelAllowlistExtraKey:    scope,
					service.UpstreamModelCatalogStateExtraKey: service.AccountModelCatalogState{RulesRevision: 9},
				}).Save(context.Background())
				require.NoError(t, err)
				if billing {
					err = repo.UpdateWithAccountBillingSettings(ctx, stale, nil, nil, nil)
				} else {
					err = repo.Update(ctx, stale)
				}
				require.NoError(t, err)
				got, err := client.Account.Get(context.Background(), entity.ID)
				require.NoError(t, err)
				require.Equal(t, "renamed", got.Name)
				require.Equal(t, "intl", got.Credentials["site"])
				for _, key := range []string{"model_mapping", "model_whitelist", "compact_model_mapping"} {
					require.Equal(t, current[key], got.Credentials[key])
				}
				saved := accountEntityToService(got)
				// Site is credential identity: this auth edit legitimately advances
				// revisions from the newer selection's baseline without losing it.
				require.EqualValues(t, 10, saved.ModelCatalogBaseline().RulesRevision)
				encoded, err := json.Marshal(saved.Extra[service.UpstreamModelAllowlistExtraKey])
				require.NoError(t, err)
				var savedScope service.AccountModelAllowlist
				require.NoError(t, json.Unmarshal(encoded, &savedScope))
				require.Equal(t, scope.Enabled, savedScope.Enabled)
				require.Equal(t, scope.Models, savedScope.Models)
			})
		}
	}
}
