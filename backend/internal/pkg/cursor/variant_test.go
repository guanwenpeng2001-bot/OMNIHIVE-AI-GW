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
	require.NotEmpty(t, ProtocolVariantModels())
	for _, model := range ProtocolVariantModels() {
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

func TestResolveModelGrok47Fallback(t *testing.T) {
	slug, err := ResolveModel("grok-4.7", "xhigh", nil)
	require.NoError(t, err)
	require.Equal(t, "cursor-grok-4.7-xhigh", slug)
}

func TestReasoningModelsWithoutThinkingSuffix(t *testing.T) {
	enabled := true
	for _, test := range []struct {
		model AvailableModel
		want  string
	}{
		{AvailableModel{ID: "composer-2.5", LegacySlugs: []string{"composer-2.5-fast"}}, "composer-2.5-fast"},
		{AvailableModel{ID: "grok-4.7", LegacySlugs: []string{"cursor-grok-4.7-low", "cursor-grok-4.7-high", "cursor-grok-4.7-xhigh"}}, "cursor-grok-4.7-xhigh"},
	} {
		t.Run(test.model.ID, func(t *testing.T) {
			slug, err := ResolveModelVariant(test.model.ID, RunOptions{Thinking: &enabled, Effort: "max"}, []AvailableModel{test.model})
			require.NoError(t, err)
			require.Equal(t, test.want, slug)
		})
	}
	disabled := false
	_, err := ResolveModelVariant("explicit-thinking", RunOptions{Thinking: &disabled}, []AvailableModel{{ID: "explicit-thinking", LegacySlugs: []string{"family-high-thinking"}}})
	require.Error(t, err, "a known thinking-only variant cannot claim thinking was disabled")
}

func TestVariantSelectionRespectsOperatorCeiling(t *testing.T) {
	catalog := []AvailableModel{{ID: "picker", LegacySlugs: []string{"model-low", "model-high"}}}
	slug, err := ResolveModelVariant("picker", RunOptions{Effort: "medium", MaxEffort: "medium"}, catalog)
	require.NoError(t, err)
	require.Equal(t, "model-low", slug)
	require.Equal(t, "low", ResolvedModelEffort(slug))
	_, err = ResolveModelVariant("picker", RunOptions{Effort: "minimal", MaxEffort: "minimal"}, catalog)
	require.ErrorIs(t, err, ErrVariantUnavailable)
	slug, err = ResolveModelVariant("plain", RunOptions{Effort: "low", MaxEffort: "low"}, []AvailableModel{{ID: "plain", RunSlug: "plain"}})
	require.NoError(t, err)
	require.Empty(t, ResolvedModelEffort(slug), "unknown must remain unknown")
}

func TestDisabledThinkingDistinguishesKnownAndUnknownVariants(t *testing.T) {
	off := false
	for _, tc := range []struct {
		name  string
		slugs []string
		want  string
	}{
		{"positive effort only", []string{"run-low", "run-high", "run-xhigh"}, ""},
		{"verified none", []string{"run-none", "run-low"}, "run-none"},
		{"unknown suffix free", []string{"composer-2.5-fast"}, "composer-2.5-fast"},
		{"known thinking switch", []string{"run-low", "run-low-thinking"}, "run-low"},
		{"thinking only", []string{"run-low-thinking"}, ""},
	} {
		for _, options := range []RunOptions{{Effort: "none"}, {Thinking: &off}, {Effort: "none", Thinking: &off}} {
			t.Run(tc.name, func(t *testing.T) {
				slug, err := ResolveModelVariant("picker", options, []AvailableModel{{ID: "picker", LegacySlugs: tc.slugs}})
				if tc.want == "" {
					require.ErrorIs(t, err, ErrVariantUnavailable)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tc.want, slug)
				if tc.name == "unknown suffix free" {
					require.Empty(t, ResolvedModelEffort(slug))
				}
			})
		}
	}
}
