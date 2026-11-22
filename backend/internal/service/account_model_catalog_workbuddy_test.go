//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkBuddyCatalogStableSubjectAndRotation(t *testing.T) {
	original := &Account{ID: 19, Platform: PlatformWorkBuddy, Type: AccountTypeOAuth, Credentials: map[string]any{"site": "cn", "uid": "user", "domain": "www.workbuddy.cn", "enterprise_id": "team", "access_token": "old", CredentialSecretVersionKey: int64(1)}, Extra: map[string]any{UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{IdentityRevision: 3}, UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{Complete: true, IdentityRevision: 3, SourceKind: "discovery", DiscoveredModels: []string{"auto"}, EligibleModels: []AccountCatalogModel{{ID: "auto"}}}}}
	for _, field := range []string{"refresh", "site", "uid", "domain", "enterprise_id", "manual"} {
		t.Run(field, func(t *testing.T) {
			incoming := snapshotOAuthRefreshAccount(original)
			if field == "refresh" || field == "manual" {
				incoming.Credentials["access_token"] = "new"
				incoming.Credentials[CredentialSecretVersionKey] = int64(2)
			} else {
				incoming.Credentials[field] = "changed"
			}
			AdvanceAccountModelCatalogRevisionsForUpdate(original, incoming, field == "manual")
			if field == "refresh" {
				require.EqualValues(t, 3, incoming.ModelCatalogState().IdentityRevision)
				require.Equal(t, ModelCapabilityIdentityFingerprint(original), ModelCapabilityIdentityFingerprint(incoming))
				require.NotNil(t, incoming.EffectiveUpstreamModelCatalog())
			} else {
				require.EqualValues(t, 4, incoming.ModelCatalogState().IdentityRevision)
				if field != "manual" {
					require.NotEqual(t, ModelCapabilityIdentityFingerprint(original), ModelCapabilityIdentityFingerprint(incoming))
				}
				require.Nil(t, incoming.EffectiveUpstreamModelCatalog())
			}
		})
	}
	for _, platform := range []string{PlatformOpenAI, PlatformKilo} {
		before := snapshotOAuthRefreshAccount(original)
		before.Platform = platform
		after := snapshotOAuthRefreshAccount(before)
		after.Credentials["uid"] = "other"
		after.Credentials["site"] = "global"
		AdvanceAccountModelCatalogRevisions(before, after)
		require.Equal(t, before.ModelCatalogState().IdentityRevision, after.ModelCatalogState().IdentityRevision)
	}
}
