package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountModelCatalogSchemaSeparatesDiscoveryCapabilitiesAndEligibility(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]string{"alias": "private-target", "wild": "family-*"}}}
	catalog := &UpstreamModelCatalog{Models: []string{"native"}, Metadata: map[string]UpstreamModelMetadata{"native": {TaskType: "text_generation"}}}
	snapshot := projectAccountModelCatalog(account, catalog)
	require.True(t, snapshot.Complete)
	require.Equal(t, "incomplete", snapshot.CapabilityStatus)
	require.Equal(t, &AccountCatalogCapabilitySummary{Total: 2, Discovered: 1, MappingTargets: 1, Eligible: 1, Incomplete: 2}, snapshot.CapabilitySummary)
	require.Equal(t, []string{"native"}, snapshot.DiscoveredModels)
	require.Len(t, snapshot.EligibleModels, 1)
}

func TestAccountModelCatalogSpecificationAndReleaseAreVersionedRules(t *testing.T) {
	for _, key := range []string{ModelCapabilityDefaultsExtraKey, ModelCapabilityReleaseExtraKey} {
		t.Run(key, func(t *testing.T) {
			current := &Account{Credentials: map[string]any{}, Extra: map[string]any{}}
			incoming := &Account{Credentials: map[string]any{}, Extra: map[string]any{key: map[string]any{"identity_revision": 0, "reason": "deployment facts"}}}
			AdvanceAccountModelCatalogRevisions(current, incoming)
			require.Equal(t, int64(1), incoming.ModelCatalogState().RulesRevision)
			require.Zero(t, incoming.ModelCatalogState().IdentityRevision)
		})
	}
}

func TestAccountModelCatalogSchemaSourceVersionProtection(t *testing.T) {
	previous := AccountModelCatalogSnapshot{SchemaVersion: 2, EnrichmentVersion: "capability-v2", SourceGeneration: 4, SourceVersions: map[string]string{"catalog": "z-old-hash"}}
	for _, tc := range []struct {
		name          string
		schema        int
		generation    int64
		version, hash string
		pinned, ok    bool
	}{
		{"same release", 2, 4, "capability-v2", "z-old-hash", false, true},
		{"older generation", 2, 3, "capability-v2", "new-looking-hash", false, false},
		{"same generation different hash", 2, 4, "capability-v2", "different", false, false},
		{"new generation unordered hash", 2, 5, "capability-v2", "a-new-hash", false, true},
		{"legacy writer", 0, 0, "", "", false, false},
		{"unknown schema", 3, 5, "future", "a", false, false},
		{"algorithm requires approval", 2, 5, "next", "a", false, false},
		{"approved algorithm", 2, 5, "next", "a", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := &AccountModelCatalogSnapshot{SchemaVersion: tc.schema, SourceGeneration: tc.generation, EnrichmentVersion: tc.version, SourceVersions: map[string]string{"catalog": tc.hash}}
			extra := map[string]any{UpstreamModelCatalogExtraKey: previous}
			if tc.pinned {
				extra[ModelCapabilityReleaseExtraKey] = AccountCapabilityRelease{SchemaVersion: tc.schema, SourceGeneration: tc.generation, EnrichmentVersion: tc.version, SourceVersions: candidate.SourceVersions}
			}
			err := ValidateAccountModelCatalogVersion(extra, candidate)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrAccountModelCatalogConflict)
			}
		})
	}
	candidate := previous
	require.ErrorIs(t, ValidateAccountModelCatalogVersion(map[string]any{ModelCapabilityReleaseExtraKey: AccountCapabilityRelease{SchemaVersion: 2, SourceGeneration: 5}}, &candidate), ErrAccountModelCatalogConflict)
}

func TestAccountModelCatalogSchemaPersistenceEnvelopeRoundTrip(t *testing.T) {
	no := false
	snapshot := &AccountModelCatalogSnapshot{Complete: true, SchemaVersion: 2, EnrichmentVersion: "capability-v2", SourceGeneration: 7, SourceVersions: map[string]string{"catalog": "sha256:abc"}, IdentityRevision: 8, RulesRevision: 9, Revision: 10, CapabilityStatus: "incomplete"}
	metadata := &UpstreamModelMetadataSnapshot{Source: "enriched", Models: map[string]UpstreamModelMetadata{"private": {ID: "private", Reasoning: &no, MaxOutputTokens: 4096}}}
	encoded, err := json.Marshal(map[string]any{UpstreamModelCatalogExtraKey: snapshot, UpstreamModelMetadataExtraKey: AccountModelMetadataEnvelope(snapshot, metadata)})
	require.NoError(t, err)
	account := &Account{}
	require.NoError(t, json.Unmarshal(encoded, &account.Extra))
	require.Equal(t, snapshot, account.GetUpstreamModelCatalog())
	var roundtrip UpstreamModelMetadataSnapshot
	require.True(t, decodeCatalogExtra(account.Extra, UpstreamModelMetadataExtraKey, &roundtrip))
	require.Equal(t, metadata.Models, roundtrip.Models)
	envelope := account.Extra[UpstreamModelMetadataExtraKey].(map[string]any)
	require.Equal(t, float64(2), envelope["schema_version"])
	require.Equal(t, float64(8), envelope["identity_revision"])
	require.Equal(t, float64(9), envelope["rules_revision"])
	require.Equal(t, float64(10), envelope["snapshot_revision"])
	require.Equal(t, "sha256:abc", envelope["source_versions"].(map[string]any)["catalog"])
}
