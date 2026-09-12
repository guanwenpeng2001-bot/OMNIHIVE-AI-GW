package cursor

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestResolveModelVariant(t *testing.T) {
	yes, no := true, false
	catalog := []AvailableModel{{ID: "picker", RunSlug: "server", Aliases: []string{"alias"}, LegacySlugs: []string{"family-low", "family-medium", "family-high", "family-extra-high", "family-medium-fast", "family-thinking-medium-fast", "family-high-thinking"}}}
	for _, test := range []struct {
		name, requested string
		options         RunOptions
		want            string
		failure         bool
	}{
		{name: "picker medium", requested: "picker", want: "family-medium"},
		{name: "alias", requested: "alias", options: RunOptions{Effort: "HIGH"}, want: "family-high-thinking"},
		{name: "server name", requested: "server", want: "family-medium"},
		{name: "legacy exact", requested: "family-low", want: "family-low"},
		{name: "normalize", requested: "picker", options: RunOptions{Effort: "extra_high"}, want: "family-extra-high"},
		{name: "nearest", requested: "picker", options: RunOptions{Effort: "max"}, want: "family-extra-high"},
		{name: "fast", requested: "picker", options: RunOptions{Fast: &yes, Thinking: &no}, want: "family-medium-fast"},
		{name: "thinking fast", requested: "alias", options: RunOptions{Fast: &yes, Thinking: &yes}, want: "family-thinking-medium-fast"},
		{name: "thinking suffix", requested: "picker", options: RunOptions{Effort: "high", Thinking: &yes}, want: "family-high-thinking"},
		{name: "synthetic suffix", requested: "picker-high", want: "family-high-thinking"},
		{name: "alias suffix", requested: "alias-high", want: "family-high-thinking"},
		{name: "legacy options preserve flags", requested: "family-medium-fast", options: RunOptions{Effort: "low"}, want: "family-medium-fast"},
		{name: "unknown model", requested: "no-model", failure: true},
		{name: "unknown effort", requested: "picker", options: RunOptions{Effort: "turbo"}, failure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveModelVariant(test.requested, test.options, catalog)
			if test.failure {
				require.Error(t, err)
				require.Empty(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.want, got)
			}
		})
	}
	// Equal distance selects the higher tier, independent of catalog ordering.
	got, err := ResolveModel("picker", "medium", []AvailableModel{{ID: "picker", LegacySlugs: []string{"family-low", "family-high"}}})
	require.NoError(t, err)
	require.Equal(t, "family-high", got)
	_, err = ResolveModelVariant("picker", RunOptions{Fast: &yes}, []AvailableModel{{ID: "picker", RunSlug: "plain"}})
	require.Error(t, err)
	require.Len(t, FallbackModels(), 38)
	for _, model := range FallbackModels() {
		resolved, err := ResolveModel(model.ID, "", nil)
		require.NoError(t, err)
		require.NotEmpty(t, resolved)
		if model.ID != "default" {
			require.NotEqual(t, "default", resolved)
		}
	}
	_, err = ResolveModel("grok-4.6", "", []AvailableModel{{ID: "only-live"}})
	require.Error(t, err, "live picker is authoritative")
}
