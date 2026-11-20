package money

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// ParseV1Number reads a historical JSON number as recorded, including the
// exponent form emitted by encoding/json for very small finite float64s.
func ParseV1Number(currency Currency, n json.Number) (Exact, error) {
	if !currency.valid() {
		return Exact{}, ErrCurrency
	}
	s := string(n)
	if len(s) > 64 {
		return Exact{}, ErrBounds
	}
	if s == "" || (s[0] != '-' && (s[0] < '0' || s[0] > '9')) || !json.Valid([]byte(s)) {
		return Exact{}, ErrDecimal
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exp, err := strconv.Atoi(s[i+1:])
		if err != nil || exp < -324 || exp > 308 {
			return Exact{}, ErrBounds
		}
	}
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return Exact{}, ErrBounds
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Exact{}, ErrDecimal
	}
	return Exact{currency, d.Rat()}, nil
}

// Comparison reports proposed minus historical values before and after rounding.
type Comparison struct {
	RawDifference    Exact
	ChargeDifference Money
}

func CompareHistorical(proposed, frozen Exact, posted Money) (Comparison, error) {
	raw, err := proposed.Sub(frozen)
	if err != nil {
		return Comparison{}, err
	}
	charge, err := proposed.RoundCharge()
	if err != nil {
		return Comparison{}, err
	}
	delta, err := charge.Sub(posted)
	if err != nil {
		return Comparison{}, err
	}
	return Comparison{raw, delta}, nil
}
