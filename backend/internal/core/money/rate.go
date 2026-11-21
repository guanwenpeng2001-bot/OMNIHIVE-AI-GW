package money

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
)

type Unit string

func (u Unit) seconds() int64 {
	switch u {
	case "second":
		return 1
	case "minute":
		return 60
	case "hour":
		return 3600
	default:
		return 0
	}
}

func (u Unit) valid() bool {
	switch u {
	case "token", "input_token", "output_token", "cache_read_token", "cache_write_token", "reasoning_token",
		"image_input_token", "image_output_token", "audio_input_token", "audio_output_token",
		"byte", "sample", "request", "image", "character", "call", "page":
		return true
	default:
		return u.seconds() != 0
	}
}

type Quantity struct {
	unit   Unit
	amount Amount
}

func NewQuantity(unit Unit, value string) (Quantity, error) {
	if !unit.valid() {
		return Quantity{}, ErrUnit
	}
	if unit.seconds() == 0 && strings.HasPrefix(value, "-") {
		return Quantity{}, ErrNegative
	}
	format := countNumeric
	if unit.seconds() != 0 {
		format = quantityNumeric
	}
	a, err := format.parse(value)
	if err != nil {
		return Quantity{}, err
	}
	if a.value.IsNegative() {
		return Quantity{}, ErrNegative
	}
	return Quantity{unit, a}, nil
}

func (q Quantity) Unit() Unit     { return q.unit }
func (q Quantity) Amount() Amount { return q.amount }

// InTier matches the existing (min,max] interval. A nil bound is open-ended.
func (q Quantity) InTier(minExclusive, maxInclusive *Quantity) (bool, error) {
	if !q.unit.valid() {
		return false, ErrUnit
	}
	for _, bound := range []*Quantity{minExclusive, maxInclusive} {
		if bound != nil && bound.unit != q.unit {
			return false, ErrUnit
		}
	}
	if minExclusive != nil && maxInclusive != nil && minExclusive.amount.Compare(maxInclusive.amount) >= 0 {
		return false, errors.New("invalid tier bounds")
	}
	return (minExclusive == nil || q.amount.Compare(minExclusive.amount) > 0) && (maxInclusive == nil || q.amount.Compare(maxInclusive.amount) <= 0), nil
}

func (q Quantity) MarshalJSON() ([]byte, error) {
	if !q.unit.valid() {
		return nil, ErrUnit
	}
	return json.Marshal(struct {
		Kind     Unit   `json:"kind"`
		Quantity string `json:"quantity"`
	}{q.unit, q.amount.String()})
}

func (q *Quantity) UnmarshalJSON(data []byte) error {
	fields, err := stringFields(data, "kind", "quantity")
	if err != nil {
		return err
	}
	next, err := NewQuantity(Unit(fields[0]), fields[1])
	if err == nil {
		*q = next
	}
	return err
}

func (q Quantity) Value() (driver.Value, error) { return objectValue(q) }
func (q *Quantity) Scan(src any) error          { return scanObject(src, q) }

type Rate struct {
	currency Currency
	amount   Amount
	unit     Unit
	per      Amount
}

func NewRate(currency Currency, amount string, unit Unit, per string) (Rate, error) {
	if !currency.valid() {
		return Rate{}, ErrCurrency
	}
	if !unit.valid() {
		return Rate{}, ErrUnit
	}
	a, err := Parse(amount)
	if err != nil {
		return Rate{}, err
	}
	if a.value.IsNegative() {
		return Rate{}, ErrNegative
	}
	den, err := countNumeric.parse(per)
	if err != nil {
		return Rate{}, err
	}
	if den.value.Sign() <= 0 {
		return Rate{}, errors.New("rate per must be a positive integer")
	}
	return Rate{currency, a, unit, den}, nil
}

func (r Rate) Currency() Currency { return r.currency }
func (r Rate) Amount() Amount     { return r.amount }
func (r Rate) Unit() Unit         { return r.unit }
func (r Rate) Per() Amount        { return r.per }

func (r Rate) Cost(q Quantity) (Exact, error) {
	if !r.currency.valid() {
		return Exact{}, ErrCurrency
	}
	quantity := q.amount.value.Rat()
	if r.unit != q.unit {
		from, to := q.unit.seconds(), r.unit.seconds()
		if from == 0 || to == 0 {
			return Exact{}, ErrUnit
		}
		quantity.Mul(quantity, big.NewRat(from, to))
	}
	total := new(big.Rat).Mul(r.amount.value.Rat(), quantity)
	return Exact{r.currency, total.Quo(total, r.per.value.Rat())}, nil
}

func (r Rate) MarshalJSON() ([]byte, error) {
	if !r.currency.valid() {
		return nil, ErrCurrency
	}
	return json.Marshal(struct {
		Currency Currency `json:"currency"`
		Amount   string   `json:"amount"`
		Unit     Unit     `json:"unit"`
		Per      string   `json:"per"`
	}{r.currency, r.amount.String(), r.unit, r.per.String()})
}

func (r *Rate) UnmarshalJSON(data []byte) error {
	fields, err := stringFields(data, "currency", "amount", "unit", "per")
	if err != nil {
		return err
	}
	next, err := NewRate(Currency(fields[0]), fields[1], Unit(fields[2]), fields[3])
	if err == nil {
		*r = next
	}
	return err
}

func (r Rate) Value() (driver.Value, error) { return objectValue(r) }
func (r *Rate) Scan(src any) error          { return scanObject(src, r) }
