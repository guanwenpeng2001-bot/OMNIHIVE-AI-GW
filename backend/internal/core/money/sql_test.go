package money

import (
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	_ sql.Scanner   = (*Amount)(nil)
	_ driver.Valuer = Amount{}
	_ sql.Scanner   = (*Money)(nil)
	_ driver.Valuer = Money{}
	_ sql.Scanner   = (*Rate)(nil)
	_ driver.Valuer = Rate{}
	_ sql.Scanner   = (*Quantity)(nil)
	_ driver.Valuer = Quantity{}
)

var columns = []struct {
	format                       Numeric
	sql, limit, overflow, excess string
}{
	{Wallet20_8, "NUMERIC(20,8)", "999999999999.99999999", "1000000000000", "0.123456789"},
	{Usage20_10, "NUMERIC(20,10)", "9999999999.9999999999", "10000000000", "0.12345678901"},
	{Payment20_2, "NUMERIC(20,2)", "999999999999999999.99", "1000000000000000000", "0.123"},
	{Ledger28_8, "NUMERIC(28,8)", "99999999999999999999.99999999", "100000000000000000000", "0.123456789"},
	{RateNumeric, "NUMERIC(38,18)", "99999999999999999999.999999999999999999", "100000000000000000000", "0.1234567890123456789"},
}

func TestSQLNumericText(t *testing.T) {
	for _, tc := range columns {
		t.Run(tc.sql, func(t *testing.T) {
			for _, sign := range []string{"", "-"} {
				s := sign + tc.limit
				for _, src := range []any{s, []byte(s)} {
					var dst Amount
					require.NoError(t, tc.format.Scanner(&dst).Scan(src))
					out, err := tc.format.Bind(dst)
					require.NoError(t, err)
					require.Equal(t, s, out)
				}
				for _, bad := range []string{sign + tc.overflow, sign + tc.excess} {
					var dst Amount
					require.ErrorIs(t, tc.format.Scanner(&dst).Scan(bad), ErrBounds)
					if a, err := Parse(bad); err == nil {
						_, err = tc.format.Bind(a)
						require.ErrorIs(t, err, ErrBounds)
					}
				}
			}
		})
	}
	for _, src := range []any{nil, 0.1, float32(1), 1, true, "0E-1", []byte("-Inf")} {
		a := amount(t, "1")
		require.Error(t, a.Scan(src))
		require.Equal(t, "1", a.String())
	}

	aLimit := amount(t, "1000000000000")
	_, err := Wallet20_8.Bind(aLimit)
	var bound *BoundsError
	require.ErrorAs(t, err, &bound)
	require.Equal(t, 20, bound.Precision)
	require.Equal(t, 8, bound.Scale)

	var a Amount
	require.NoError(t, a.Scan([]byte("-0.0000")))
	v, err := a.Value()
	require.NoError(t, err)
	require.Equal(t, "0", v)
	v, err = Wallet20_8.Bind(a)
	require.NoError(t, err)
	require.Equal(t, "0.00000000", v)
}
