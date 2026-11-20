package money

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

var (
	ErrDecimal          = errors.New("invalid decimal")
	ErrBounds           = errors.New("value exceeds numeric precision or scale")
	ErrCurrency         = errors.New("invalid currency")
	ErrCurrencyMismatch = errors.New("currency mismatch")
	ErrUnit             = errors.New("incompatible or unknown unit")
	ErrNegative         = errors.New("negative amount or quantity")
	ErrEncoding         = errors.New("invalid money encoding")
)

// BoundsError identifies the SQL precision that would overflow or lose digits.
type BoundsError struct {
	Precision int
	Scale     int
}

func (e *BoundsError) Error() string {
	return fmt.Sprintf("value does not fit NUMERIC(%d,%d)", e.Precision, e.Scale)
}

func (e *BoundsError) Unwrap() error { return ErrBounds }

// Amount is a finite decimal with up to 20 whole and 18 fractional digits.
type Amount struct{ value decimal.Decimal }

func Parse(s string) (Amount, error)  { return RateNumeric.parse(s) }
func (a Amount) String() string       { return a.value.String() }
func (a Amount) Compare(b Amount) int { return a.value.Cmp(b.value) }

func (a Amount) MarshalJSON() ([]byte, error) { return json.Marshal(a.String()) }
func (a *Amount) UnmarshalJSON(data []byte) error {
	if len(data) > maxObjectSize {
		return ErrBounds
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return ErrEncoding
	}
	return RateNumeric.scan(s, a)
}

func (a Amount) Value() (driver.Value, error) { return a.String(), nil }
func (a *Amount) Scan(src any) error          { return RateNumeric.scan(src, a) }

// Numeric binds a scalar to the precision of its target SQL column.
type Numeric uint8

const (
	Wallet20_8 Numeric = iota + 1
	Usage20_10
	Payment20_2
	Ledger28_8
	RateNumeric     // NUMERIC(38,18)
	quantityNumeric // NUMERIC(30,10)
	countNumeric    // 20-digit integer
)

func (f Numeric) limits() (whole, fraction int) {
	switch f {
	case Wallet20_8:
		return 12, 8
	case Usage20_10:
		return 10, 10
	case Payment20_2:
		return 18, 2
	case Ledger28_8:
		return 20, 8
	case RateNumeric:
		return 20, 18
	case quantityNumeric:
		return 20, 10
	case countNumeric:
		return 20, 0
	default:
		return 0, 0
	}
}

func (f Numeric) parse(s string) (Amount, error) {
	whole, fraction := f.limits()
	if whole == 0 || len(s) > whole+fraction+2 {
		return Amount{}, &BoundsError{whole + fraction, fraction}
	}
	unsigned := strings.TrimPrefix(s, "-")
	i, frac, point := strings.Cut(unsigned, ".")
	if i == "" || (len(i) > 1 && i[0] == '0') || (point && frac == "") {
		return Amount{}, ErrDecimal
	}
	if len(i) > whole || len(frac) > fraction {
		return Amount{}, &BoundsError{whole + fraction, fraction}
	}
	for _, part := range []string{i, frac} {
		for j := 0; j < len(part); j++ {
			if part[j] < '0' || part[j] > '9' {
				return Amount{}, ErrDecimal
			}
		}
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Amount{}, ErrDecimal
	}
	return Amount{d}, nil
}

func (f Numeric) check(d decimal.Decimal) error {
	whole, scale := f.limits()
	if whole == 0 || d.Abs().Cmp(decimal.New(1, int32(whole))) >= 0 || !d.Equal(d.Truncate(int32(scale))) {
		return &BoundsError{whole + scale, scale}
	}
	return nil
}

// Bind checks for overflow or precision loss before yielding a SQL parameter.
func (f Numeric) Bind(a Amount) (driver.Value, error) {
	if err := f.check(a.value); err != nil {
		return nil, err
	}
	_, scale := f.limits()
	return a.value.StringFixed(int32(scale)), nil
}

func (f Numeric) Scanner(dst *Amount) sql.Scanner { return numericScanner{f, dst} }

type numericScanner struct {
	f   Numeric
	dst *Amount
}

func (s numericScanner) Scan(src any) error { return s.f.scan(src, s.dst) }

func (f Numeric) scan(src any, dst *Amount) error {
	s, err := textSource(src)
	if err != nil {
		return err
	}
	a, err := f.parse(s)
	if err == nil {
		*dst = a
	}
	return err
}

func textSource(src any) (string, error) {
	switch s := src.(type) {
	case string:
		return s, nil
	case []byte:
		if len(s) > maxObjectSize {
			return "", ErrBounds
		}
		return string(s), nil
	default:
		return "", ErrEncoding
	}
}
