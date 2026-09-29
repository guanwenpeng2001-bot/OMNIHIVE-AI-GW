package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// This is the independently recorded 525-key observation, NOT the missing
// historical 213-key fixture and NOT a claim about today's live account state.
func TestModelCatalogRecordedMappingFixtureRoundTrip(test *testing.T) {
	data, err := os.ReadFile("testdata/catalog_recorded_mapping.json")
	require.NoError(test, err)
	var fixture struct {
		Baseline    string            `json:"baseline_commit"`
		ObservedAt  string            `json:"observed_at"`
		MappingHash string            `json:"canonical_mapping_sha256"`
		Mapping     map[string]string `json:"mapping"`
		Discovered  []string          `json:"discovered_models"`
	}
	require.NoError(test, json.Unmarshal(data, &fixture))
	require.Equal(test, "0bf6459c0", fixture.Baseline)
	require.Equal(test, "2026-09-16T17:48:29.1561881Z", fixture.ObservedAt)
	require.Len(test, fixture.Mapping, 525)
	require.Len(test, fixture.Discovered, 252)
	before, err := json.Marshal(fixture.Mapping)
	require.NoError(test, err)
	digest := sha256.Sum256(before)
	require.Equal(test, fixture.MappingHash, hex.EncodeToString(digest[:]))
	account := &Account{Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": fixture.Mapping}}
	for _, discovered := range [][]string{fixture.Discovered, {}} {
		catalog := &UpstreamModelCatalog{Models: discovered}
		snapshot := projectAccountModelCatalog(account, catalog)
		require.Equal(test, 525, snapshot.CapabilitySummary.MappingTargets)
		expectedTotal := 525
		if len(discovered) > 0 {
			expectedTotal = 534
		}
		require.Equal(test, expectedTotal, snapshot.CapabilitySummary.Total)
		require.ElementsMatch(test, discovered, snapshot.DiscoveredModels)
		for _, eligible := range snapshot.EligibleModels {
			require.Contains(test, discovered, eligible.ID, "mapping alone must not authorize")
		}
		encoded, err := json.Marshal(account.Credentials)
		require.NoError(test, err)
		var roundTrip Account
		require.NoError(test, json.Unmarshal(encoded, &roundTrip.Credentials))
		after, err := json.Marshal(roundTrip.Credentials["model_mapping"])
		require.NoError(test, err)
		require.Equal(test, before, after)
	}
}
