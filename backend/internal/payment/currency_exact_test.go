package payment

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExactCallbackMinorUnits(t *testing.T) {
	for _, tc := range []struct {
		amount, currency string
		minor            int64
	}{
		{"12", "JPY", 12}, {"12.34", "CNY", 1234}, {"12.345", "KWD", 12345}, {"12", "ISK", 1200}, {"1e1", "USD", 1000}, {"92233720368547758.07", "USD", math.MaxInt64},
	} {
		minor, err := AmountToMinorUnit(tc.amount, tc.currency)
		require.NoError(t, err)
		require.Equal(t, tc.minor, minor)
	}
	for _, bad := range []string{"92233720368547758.08", "1e2147483647", "1e-2147483647", "12.345"} {
		_, err := AmountToMinorUnit(bad, "USD")
		require.Error(t, err)
	}
	require.Zero(t, LegacyCallbackAmount(math.MaxInt64, "USD"))
	require.Equal(t, 12.345, LegacyCallbackAmount(12345, "KWD"))
}
