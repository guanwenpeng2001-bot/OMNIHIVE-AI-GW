package money

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func amount(t *testing.T, s string) Amount {
	t.Helper()
	a, err := Parse(s)
	require.NoError(t, err)
	return a
}

func exact(t *testing.T, s string) Exact {
	t.Helper()
	a, err := amount(t, s).Exact("USD")
	require.NoError(t, err)
	return a
}

func quantity(t *testing.T, u Unit, s string) Quantity {
	t.Helper()
	q, err := NewQuantity(u, s)
	require.NoError(t, err)
	return q
}

func cost(t *testing.T, price string, u Unit, per string, q Quantity) Exact {
	t.Helper()
	r, err := NewRate("USD", price, u, per)
	require.NoError(t, err)
	v, err := r.Cost(q)
	require.NoError(t, err)
	return v
}

func TestRoundChargeAndReserveCeil(t *testing.T) {
	for _, tc := range []struct{ in, charge, hold string }{
		{"0.000078124", "0.00007812", "0.00007813"},
		{"0.000078125", "0.00007813", "0.00007813"},
		{"0.000078126", "0.00007813", "0.00007813"},
		{"-0.000078125", "-0.00007813", ""},
		{"-0.000078124", "-0.00007812", ""},
		{"-0.000000001", "0.00000000", ""},
		{"0.000000000000000001", "0.00000000", "0.00000001"},
		{"-0.000000000", "0.00000000", "0.00000000"},
		{"1", "1.00000000", "1.00000000"},
		{"99999999999999999999.99999999", "99999999999999999999.99999999", "99999999999999999999.99999999"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			e := exact(t, tc.in)
			m, err := e.RoundCharge()
			require.NoError(t, err)
			require.Equal(t, tc.charge, m.String())
			h, err := e.ReserveCeil()
			if tc.hold == "" {
				require.ErrorIs(t, err, ErrNegative)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.hold, h.String())
		})
	}
	carry := exact(t, "99999999999999999999.999999995")
	_, err := carry.RoundCharge()
	require.ErrorIs(t, err, ErrBounds)
	_, err = carry.ReserveCeil()
	require.ErrorIs(t, err, ErrBounds)
}

func TestSignedZero(t *testing.T) {
	m, err := NewMoney("USD", "-0.00000000")
	require.NoError(t, err)
	require.Equal(t, "0.00000000", m.String())
	r, err := NewRate("USD", "-0.000", "hour", "1")
	require.NoError(t, err)
	require.Equal(t, "0", r.Amount().String())
	duration, err := NewQuantity("second", "-0.00")
	require.NoError(t, err)
	require.Equal(t, "0", duration.Amount().String())
	_, err = NewQuantity("byte", "-0")
	require.Error(t, err)
	_, err = NewQuantity("minute", "-0.0000000001")
	require.ErrorIs(t, err, ErrNegative)
}

func TestExactCostAndUnitConversion(t *testing.T) {
	in := cost(t, "1.25", "input_token", "1000000", quantity(t, "input_token", "10"))
	out := cost(t, "10", "output_token", "1000000", quantity(t, "output_token", "5"))
	total, err := in.Add(out)
	require.NoError(t, err)
	actual, err := total.Multiply(amount(t, "1.25"))
	require.NoError(t, err)
	require.Equal(t, "1/12800", actual.Ratio().RatString())
	actual.Ratio().SetInt64(0)
	require.Equal(t, "0.000078125000000000", actual.Diagnostic())
	for _, tc := range []struct {
		price, per, quantity   string
		unit, usage            Unit
		fraction, charge, hold string
	}{
		{"1", "1", "1", "hour", "second", "1/3600", "0.00027778", "0.00027778"},
		{"1", "60", "0.5", "minute", "second", "1/7200", "0.00013889", "0.00013889"},
		{"1", "1", "1.25", "second", "hour", "4500", "4500.00000000", "4500.00000000"},
		{"1", "3", "1", "request", "request", "1/3", "0.33333333", "0.33333334"},
		{"1", "200000001", "1", "request", "request", "1/200000001", "0.00000000", "0.00000001"},
	} {
		v := cost(t, tc.price, tc.unit, tc.per, quantity(t, tc.usage, tc.quantity))
		require.Equal(t, tc.fraction, v.Ratio().RatString())
		m, err := v.RoundCharge()
		require.NoError(t, err)
		require.Equal(t, tc.charge, m.String())
		h, err := v.ReserveCeil()
		require.NoError(t, err)
		require.Equal(t, tc.hold, h.String())
	}
	third := cost(t, "1", "request", "3", quantity(t, "request", "1"))
	sum, err := third.Add(third)
	require.NoError(t, err)
	sum, err = sum.Add(third)
	require.NoError(t, err)
	require.Equal(t, "1", sum.Ratio().RatString())
	large := cost(t, "99999999999999999999.999999999999999999", "token", "1", quantity(t, "token", "99999999999999999999"))
	_, err = large.RoundCharge()
	require.ErrorIs(t, err, ErrBounds)
}

func TestTierBounds(t *testing.T) {
	lo := quantity(t, "input_token", "9007199254740992")
	hi := quantity(t, "input_token", "9007199254741002")
	for _, tc := range []struct {
		s     string
		match bool
	}{
		{"9007199254740992", false}, {"9007199254740993", true}, {"9007199254741002", true}, {"9007199254741003", false},
	} {
		q := quantity(t, "input_token", tc.s)
		in, err := q.InTier(&lo, &hi)
		require.NoError(t, err)
		require.Equal(t, tc.match, in)
	}
	zero := quantity(t, "input_token", "0")
	in, err := zero.InTier(&zero, nil)
	require.NoError(t, err)
	require.False(t, in)
	in, err = zero.InTier(nil, &hi)
	require.NoError(t, err)
	require.True(t, in)
	_, err = zero.InTier(&hi, &lo)
	require.Error(t, err)
	other := quantity(t, "output_token", "1")
	_, err = other.InTier(&lo, nil)
	require.ErrorIs(t, err, ErrUnit)
	r, err := NewRate("USD", "1", "input_token", "1")
	require.NoError(t, err)
	_, err = r.Cost(other)
	require.ErrorIs(t, err, ErrUnit)
}

func TestCurrenciesAndBounds(t *testing.T) {
	for _, c := range []Currency{"", "usd", "US", "US1", " USD", "円"} {
		_, err := NewMoney(c, "1")
		require.ErrorIs(t, err, ErrCurrency)
	}
	a, err := NewMoney("USD", "99999999999999999999.99999999")
	require.NoError(t, err)
	b, err := NewMoney("USD", "0.00000001")
	require.NoError(t, err)
	_, err = a.Add(b)
	require.ErrorIs(t, err, ErrBounds)
	c, err := a.Sub(b)
	require.NoError(t, err)
	require.Equal(t, "99999999999999999999.99999998", c.String())
	order, err := a.Compare(b)
	require.NoError(t, err)
	require.Equal(t, 1, order)
	b, err = NewMoney("EUR", "0")
	require.NoError(t, err)
	_, err = a.Add(b)
	require.ErrorIs(t, err, ErrCurrencyMismatch)
	_, err = a.Sub(b)
	require.ErrorIs(t, err, ErrCurrencyMismatch)
	_, err = a.Compare(b)
	require.ErrorIs(t, err, ErrCurrencyMismatch)
	eur, err := b.Amount().Exact(b.Currency())
	require.NoError(t, err)
	usd := exact(t, "1")
	_, err = CompareHistorical(usd, usd, b)
	require.ErrorIs(t, err, ErrCurrencyMismatch)
	_, err = eur.Add(usd)
	require.ErrorIs(t, err, ErrCurrencyMismatch)
	_, err = eur.Compare(usd)
	require.ErrorIs(t, err, ErrCurrencyMismatch)
	_, err = usd.Multiply(amount(t, "-1"))
	require.ErrorIs(t, err, ErrNegative)
	_, err = usd.Multiply(amount(t, "10000000000"))
	require.ErrorIs(t, err, ErrBounds)
	_, err = NewMoney("USD", "0.000000001")
	require.ErrorIs(t, err, ErrBounds)
}

func TestStrictWire(t *testing.T) {
	for _, raw := range []string{"null", "1", "-0", "true", "{}", `"1e-5"`, `"Inf"`, `"NaN"`, `"+1"`, `"1."`, `".1"`, `" 1"`, `"01"`, `"1_2"`, `"--1"`, `""`, `"1.2.3"`, `"１２"`, `"` + strings.Repeat("1", 1<<16) + `"`, `"100000000000000000000"`, `"0.0000000000000000001"`} {
		a := amount(t, "42")
		require.Error(t, json.Unmarshal([]byte(raw), &a))
		require.Equal(t, "42", a.String())
	}
	for _, raw := range []string{`{"currency":"USD","amount":1}`, `{"amount":"0"}`, `{"currency":"USD","amount":null}`, `{"Currency":"USD","amount":"1"}`, `{"currency":"USD","currency":"EUR","amount":"1"}`, `{"currency":"USD","amount":"1","extra":"1"}`, `{"currency":"USD","amount":"1e2"}`, `{"currency":"USD","amount":"0.123456789"}`, `{}`, `"1"`, `null`} {
		var m Money
		require.Error(t, json.Unmarshal([]byte(raw), &m), raw)
	}
	for _, tc := range []struct{ price, per string }{
		{"-1", "1"}, {"0", "0"}, {"0", "-1"}, {"1", "1.0"}, {"1", "1e2"}, {"1", strings.Repeat("9", 21)},
	} {
		_, err := NewRate("USD", tc.price, "token", tc.per)
		require.Error(t, err)
	}
	for _, raw := range []string{`{"currency":"USD","unit":"token","amount":0,"per":"1"}`, `{"currency":"USD","unit":"token","amount":"0","per":1}`, `{"currency":"USD","unit":"token","amount":"0"}`} {
		var r Rate
		require.Error(t, json.Unmarshal([]byte(raw), &r))
	}
	for _, raw := range []string{`{"kind":"sample","quantity":"1.0"}`, `{"kind":"byte","quantity":"-1"}`, `{"kind":"second","quantity":"0.00000000001"}`, `{"kind":"unknown","quantity":"0"}`, `{"kind":"token","quantity":1}`} {
		var q Quantity
		require.Error(t, json.Unmarshal([]byte(raw), &q))
	}
	a := amount(t, "-0.00000")
	b, err := json.Marshal(a)
	require.NoError(t, err)
	require.Equal(t, `"0"`, string(b))
}

func TestWireAndCacheRoundTrip(t *testing.T) {
	b, err := os.ReadFile("../../../../contracts/api/BE-02/values.json")
	require.NoError(t, err)
	type record struct {
		SchemaVersion string   `json:"schema_version"`
		Encoding      string   `json:"money_encoding"`
		Settled       Money    `json:"settled"`
		Adjustment    Money    `json:"adjustment"`
		Rate          Rate     `json:"rate"`
		Count         Quantity `json:"count"`
		Duration      Quantity `json:"duration"`
	}
	var rec record
	require.NoError(t, json.Unmarshal(b, &rec))
	encoded, err := json.Marshal(rec)
	require.NoError(t, err)
	require.JSONEq(t, string(b), string(encoded))
	require.Equal(t, "9007199254740993", rec.Count.Amount().String())
	for _, tc := range []struct {
		in  driver.Valuer
		dst sql.Scanner
	}{
		{rec.Settled, new(Money)}, {rec.Rate, new(Rate)}, {rec.Duration, new(Quantity)},
	} {
		raw, err := tc.in.Value()
		require.NoError(t, err)
		require.NoError(t, tc.dst.Scan([]byte(raw.(string))))
		v, err := tc.dst.(driver.Valuer).Value()
		require.NoError(t, err)
		require.Equal(t, raw, v)
	}
}
