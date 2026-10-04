//go:build unit

package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompatiblePlanCatalogValidation(t *testing.T) {
	original, _ := LookupCompatibleProvider("aicodemirror")
	for _, change := range []func(*CompatibleProvider){
		func(p *CompatibleProvider) { p.Plans[0].ID = "PRO" },
		func(p *CompatibleProvider) { p.Plans[0].ID = "pro-plus" },
		func(p *CompatibleProvider) { p.Plans[1].ID = p.Plans[0].ID },
		func(p *CompatibleProvider) { p.Plans[0].Label = " " },
		func(p *CompatibleProvider) { p.Plans[0].LabelZh = "" },
		func(p *CompatibleProvider) { p.Plans[0].Query = "unlimited" },
		func(p *CompatibleProvider) { p.Modes = append(p.Modes, CompatibleMode{Mode: "coding"}) },
		func(p *CompatibleProvider) { p.Modes = nil },
	} {
		candidate := copyCompatibleProvider(original)
		change(&candidate)
		require.Error(t, ValidateCompatibleProviders([]CompatibleProvider{candidate}))
	}
	fresh, _ := LookupCompatibleProvider("aicodemirror")
	require.Equal(t, original, fresh, "plan copies must not alias registry memory")
	for _, provider := range CompatibleProviders() {
		if len(provider.Plans) > 0 {
			require.Len(t, provider.Modes, 1)
		}
		for _, plan := range provider.Plans {
			require.NoError(t, provider.ValidateBillingPlan(map[string]any{"billing_plan": plan.ID}))
		}
		require.NoError(t, provider.ValidateBillingPlan(nil))
		for _, bad := range []any{"unknown", "PRO", 1, false, []string{}, map[string]any{}, " pro"} {
			require.Error(t, provider.ValidateBillingPlan(map[string]any{"billing_plan": bad}))
		}
	}
	encoded, err := json.Marshal([]CompatibleProvider{original})
	require.NoError(t, err)
	decoded, err := ParseCompatibleProviders(encoded)
	require.NoError(t, err)
	require.Equal(t, original, decoded[0])
	for _, clear := range []any{nil, ""} {
		credentials := map[string]any{"billing_plan": clear, "base_url": "", "api_key": "fixture-key"}
		require.NoError(t, original.ValidateBillingPlan(credentials))
		ClearEmptyBillingPlan(credentials)
		require.NotContains(t, credentials, "billing_plan")
		require.Contains(t, credentials, "base_url")
	}
}
