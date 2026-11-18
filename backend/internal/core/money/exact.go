package money

import (
	"math/big"

	"github.com/shopspring/decimal"
)

// Exact keeps the unrounded result of pricing and unit conversions.
type Exact struct {
	currency Currency
	value    *big.Rat
}

func (a Amount) Exact(currency Currency) (Exact, error) {
	if !currency.valid() {
		return Exact{}, ErrCurrency
	}
	return Exact{currency, a.value.Rat()}, nil
}

func (e Exact) Currency() Currency { return e.currency }

// Ratio returns an independent copy of the exact value for audit and replay.
func (e Exact) Ratio() *big.Rat {
	if e.value == nil {
		return new(big.Rat)
	}
	return new(big.Rat).Set(e.value)
}

func (e Exact) Diagnostic() string { return decimal.NewFromBigRat(e.Ratio(), 18).StringFixed(18) }
func (e Exact) Add(other Exact) (Exact, error) {
	if err := matchingCurrencies(e.currency, other.currency); err != nil {
		return Exact{}, err
	}
	return Exact{e.currency, new(big.Rat).Add(e.value, other.value)}, nil
}

func (e Exact) Sub(other Exact) (Exact, error) {
	if err := matchingCurrencies(e.currency, other.currency); err != nil {
		return Exact{}, err
	}
	return Exact{e.currency, new(big.Rat).Sub(e.value, other.value)}, nil
}

func (e Exact) Compare(other Exact) (int, error) {
	if err := matchingCurrencies(e.currency, other.currency); err != nil {
		return 0, err
	}
	return e.value.Cmp(other.value), nil
}

// Multiply applies a nonnegative frozen NUMERIC(20,10) multiplier.
func (e Exact) Multiply(multiplier Amount) (Exact, error) {
	if !e.currency.valid() {
		return Exact{}, ErrCurrency
	}
	if err := Usage20_10.check(multiplier.value); err != nil {
		return Exact{}, err
	}
	if multiplier.value.IsNegative() {
		return Exact{}, ErrNegative
	}
	return Exact{e.currency, new(big.Rat).Mul(e.value, multiplier.value.Rat())}, nil
}

func (e Exact) RoundCharge() (Money, error) {
	if !e.currency.valid() {
		return Money{}, ErrCurrency
	}
	return settled(e.currency, Amount{decimal.NewFromBigRat(e.value, 8)})
}

func (e Exact) ReserveCeil() (Money, error) {
	if !e.currency.valid() {
		return Money{}, ErrCurrency
	}
	if e.value.Sign() < 0 {
		return Money{}, ErrNegative
	}
	n := new(big.Int).Mul(e.value.Num(), big.NewInt(100000000))
	units, rem := new(big.Int), new(big.Int)
	units.QuoRem(n, e.value.Denom(), rem)
	if rem.Sign() > 0 {
		units.Add(units, big.NewInt(1))
	}
	return settled(e.currency, Amount{decimal.NewFromBigInt(units, -8)})
}
