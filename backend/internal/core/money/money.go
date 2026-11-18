package money

import (
	"database/sql/driver"
	"encoding/json"
)

type Currency string

func (c Currency) valid() bool {
	if len(c) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		if c[i] < 'A' || c[i] > 'Z' {
			return false
		}
	}
	return true
}

func matchingCurrencies(c, d Currency) error {
	if !c.valid() || !d.valid() {
		return ErrCurrency
	}
	if c != d {
		return ErrCurrencyMismatch
	}
	return nil
}

// Money is a settled amount on the eight-place ledger scale.
type Money struct {
	currency Currency
	amount   Amount
}

func NewMoney(currency Currency, amount string) (Money, error) {
	if !currency.valid() {
		return Money{}, ErrCurrency
	}
	a, err := Ledger28_8.parse(amount)
	if err != nil {
		return Money{}, err
	}
	return Money{currency, a}, nil
}

func (m Money) Currency() Currency { return m.currency }
func (m Money) Amount() Amount     { return m.amount }
func (m Money) String() string     { return m.amount.value.StringFixed(8) }

func (m Money) Add(other Money) (Money, error) {
	if err := matchingCurrencies(m.currency, other.currency); err != nil {
		return Money{}, err
	}
	return settled(m.currency, Amount{m.amount.value.Add(other.amount.value)})
}

func (m Money) Sub(other Money) (Money, error) {
	if err := matchingCurrencies(m.currency, other.currency); err != nil {
		return Money{}, err
	}
	return settled(m.currency, Amount{m.amount.value.Sub(other.amount.value)})
}

func (m Money) Compare(other Money) (int, error) {
	if err := matchingCurrencies(m.currency, other.currency); err != nil {
		return 0, err
	}
	return m.amount.Compare(other.amount), nil
}

func settled(c Currency, a Amount) (Money, error) {
	if err := Ledger28_8.check(a.value); err != nil {
		return Money{}, err
	}
	return Money{c, a}, nil
}

func (m Money) MarshalJSON() ([]byte, error) {
	if !m.currency.valid() {
		return nil, ErrCurrency
	}
	return json.Marshal(struct {
		Currency Currency `json:"currency"`
		Amount   string   `json:"amount"`
	}{m.currency, m.String()})
}

func (m *Money) UnmarshalJSON(data []byte) error {
	fields, err := stringFields(data, "currency", "amount")
	if err != nil {
		return err
	}
	next, err := NewMoney(Currency(fields[0]), fields[1])
	if err == nil {
		*m = next
	}
	return err
}

func (m Money) Value() (driver.Value, error) { return objectValue(m) }
func (m *Money) Scan(src any) error          { return scanObject(src, m) }
