package quotes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScenarioUsage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		scenario  Scenario
		wantError bool
	}{
		{"cached subset", Scenario{Input: "100", Cached: "25", Requests: "2", AudioSeconds: "1.25"}, false},
		{"cached exceeds input", Scenario{Input: "100", Cached: "101"}, true},
		{"zero requests", Scenario{Requests: "0"}, true},
		{"fractional token", Scenario{Input: "1.5"}, true},
		{"aggregate overflow", Scenario{Input: "99999999999999999999", Requests: "2"}, true},
		{"excess duration precision", Scenario{VideoSeconds: "1.00000000001"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage, err := tc.scenario.Usage()
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "100", usage.Context.Amount().String())
			require.Len(t, usage.Dimensions, 4)
			require.Equal(t, "150", usage.Dimensions[0].Quantity.Amount().String())
			require.Equal(t, "50", usage.Dimensions[1].Quantity.Amount().String())
			require.Equal(t, "2.5", usage.Dimensions[2].Quantity.Amount().String())
		})
	}
}
