package payment

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

const DefaultPaymentCurrency = "CNY"

type paymentCurrencyAmountUnit struct {
	apiMinorUnit      int
	maxFractionDigits int
}

var (
	zeroDecimalAmountUnit  = paymentCurrencyAmountUnit{apiMinorUnit: 0, maxFractionDigits: 0}
	twoDecimalAmountUnit   = paymentCurrencyAmountUnit{apiMinorUnit: 2, maxFractionDigits: 2}
	threeDecimalAmountUnit = paymentCurrencyAmountUnit{apiMinorUnit: 3, maxFractionDigits: 3}
	stripeLegacyZeroAmount = paymentCurrencyAmountUnit{apiMinorUnit: 2, maxFractionDigits: 0}
)

var paymentCurrencyAmountUnits = map[string]paymentCurrencyAmountUnit{
	"BIF": zeroDecimalAmountUnit,
	"CLP": zeroDecimalAmountUnit,
	"DJF": zeroDecimalAmountUnit,
	"GNF": zeroDecimalAmountUnit,
	"JPY": zeroDecimalAmountUnit,
	"KMF": zeroDecimalAmountUnit,
	"KRW": zeroDecimalAmountUnit,
	"MGA": zeroDecimalAmountUnit,
	"PYG": zeroDecimalAmountUnit,
	"RWF": zeroDecimalAmountUnit,
	"VND": zeroDecimalAmountUnit,
	"VUV": zeroDecimalAmountUnit,
	"XAF": zeroDecimalAmountUnit,
	"XOF": zeroDecimalAmountUnit,
	"XPF": zeroDecimalAmountUnit,
	"ISK": stripeLegacyZeroAmount,
	"UGX": stripeLegacyZeroAmount,
	"BHD": threeDecimalAmountUnit,
	"IQD": threeDecimalAmountUnit,
	"JOD": threeDecimalAmountUnit,
	"KWD": threeDecimalAmountUnit,
	"LYD": threeDecimalAmountUnit,
	"OMR": threeDecimalAmountUnit,
	"TND": threeDecimalAmountUnit,
}

func NormalizePaymentCurrency(raw string) (string, error) {
	currency := strings.ToUpper(strings.TrimSpace(raw))
	if currency == "" {
		return DefaultPaymentCurrency, nil
	}
	if len(currency) != 3 {
		return "", fmt.Errorf("payment currency must be a 3-letter ISO currency code")
	}
	for _, ch := range currency {
		if ch < 'A' || ch > 'Z' {
			return "", fmt.Errorf("payment currency must be a 3-letter ISO currency code")
		}
	}
	return currency, nil
}

func CurrencyMinorUnit(currency string) int {
	return paymentCurrencyAmountUnitFor(currency).apiMinorUnit
}

// CurrencyMaxFractionDigits 返回支付金额允许展示和输入的小数位数。
func CurrencyMaxFractionDigits(currency string) int {
	return paymentCurrencyAmountUnitFor(currency).maxFractionDigits
}

// FormatAmountForCurrency 按币种允许的小数位格式化支付金额。
func FormatAmountForCurrency(amount float64, currency string) string {
	return decimal.NewFromFloat(amount).StringFixed(int32(CurrencyMaxFractionDigits(currency)))
}

func paymentCurrencyAmountUnitFor(currency string) paymentCurrencyAmountUnit {
	normalized, err := NormalizePaymentCurrency(currency)
	if err != nil {
		return twoDecimalAmountUnit
	}
	if amountUnit, ok := paymentCurrencyAmountUnits[normalized]; ok {
		return amountUnit
	}
	return twoDecimalAmountUnit
}

func AmountToMinorUnit(amountStr, currency string) (int64, error) {
	// Bound intermediate expansion before multiplying. Short scientific
	// notation from legacy clients retains the same numeric interpretation.
	amountStr = strings.TrimSpace(amountStr)
	if len(amountStr) > 128 {
		return 0, fmt.Errorf("invalid minor-unit amount")
	}
	d, err := decimal.NewFromString(amountStr)
	if err != nil || d.Exponent() < -128 || d.Exponent() > 128 {
		return 0, fmt.Errorf("invalid amount: %s", amountStr)
	}
	normalizedCurrency, err := NormalizePaymentCurrency(currency)
	if err != nil {
		return 0, err
	}
	amountUnit := paymentCurrencyAmountUnitFor(normalizedCurrency)
	precisionFactor := decimal.New(1, int32(amountUnit.maxFractionDigits))
	scaledForPrecision := d.Mul(precisionFactor)
	if !scaledForPrecision.Equal(scaledForPrecision.Truncate(0)) {
		if amountUnit.maxFractionDigits == 0 {
			return 0, fmt.Errorf("payment amount for %s must be a whole number", normalizedCurrency)
		}
		return 0, fmt.Errorf("payment amount for %s must not have more than %d decimal places", normalizedCurrency, amountUnit.maxFractionDigits)
	}
	factor := decimal.New(1, int32(amountUnit.apiMinorUnit))
	minorAmount := d.Mul(factor)
	if !minorAmount.BigInt().IsInt64() {
		return 0, fmt.Errorf("payment minor units exceed int64")
	}
	return minorAmount.IntPart(), nil
}

func MinorUnitToAmount(value int64, currency string) float64 {
	factor := decimal.New(1, int32(CurrencyMinorUnit(currency)))
	return decimal.NewFromInt(value).Div(factor).InexactFloat64()
}

// LegacyCallbackAmount projects verified money into the v1 float DTO only
// when that path can distinguish every submitted minor unit. Zero is the
// established invalid-paid-amount sentinel. Exact consumers use Minor.
func LegacyCallbackAmount(minor int64, currency string) float64 {
	amount := MinorUnitToAmount(minor, currency)
	back, err := AmountToMinorUnit(strconv.FormatFloat(amount, 'f', -1, 64), currency)
	if err != nil || back != minor {
		return 0
	}
	return amount
}
