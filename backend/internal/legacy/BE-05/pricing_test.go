package legacy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
)

func TestLegacyRateKeepsDeclaredPerUnitScale(t *testing.T) {
	one, err := money.Parse("1")
	require.NoError(t, err)

	rate, err := legacyRate(1.25, money.Unit("input_token"), "1000000", one)
	require.NoError(t, err)

	million, err := money.NewQuantity("input_token", "1000000")
	require.NoError(t, err)
	charged, err := rate.Cost(million)
	require.NoError(t, err)
	settled, err := charged.RoundCharge()
	require.NoError(t, err)
	require.Equal(t, "1.25000000", settled.String())

	single, err := money.NewQuantity("input_token", "1")
	require.NoError(t, err)
	perToken, err := rate.Cost(single)
	require.NoError(t, err)
	require.Equal(t, "0.00000125", trimDiagnostic(perToken.Diagnostic()))
}

func TestLegacyRateAppliesMultiplierOnce(t *testing.T) {
	onePointFive, err := money.Parse("1.5")
	require.NoError(t, err)

	rate, err := legacyRate(2, money.Unit("output_token"), "1000000", onePointFive)
	require.NoError(t, err)

	million, err := money.NewQuantity("output_token", "1000000")
	require.NoError(t, err)
	charged, err := rate.Cost(million)
	require.NoError(t, err)
	settled, err := charged.RoundCharge()
	require.NoError(t, err)
	require.Equal(t, "3.00000000", settled.String())
}

func trimDiagnostic(value string) string {
	for len(value) > 0 && value[len(value)-1] == '0' {
		value = value[:len(value)-1]
	}
	if len(value) > 0 && value[len(value)-1] == '.' {
		value = value[:len(value)-1]
	}
	return value
}
