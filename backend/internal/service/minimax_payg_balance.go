package service

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// MiniMax-AI/cli 06e47c70: src/config/schema.ts and src/client/endpoints.ts
// select GET /account/query_balance only for API keys beginning with sk-api-.
// Legacy minimaxi.com is not a current CLI region and must not be rewritten.
func miniMaxPayGBalanceURL(account *Account) string {
	if account.Type != AccountTypeAPIKey || account.GetCredential("account_mode") != AccountModePayG ||
		!strings.HasPrefix(strings.TrimSpace(account.GetCNAPIKey()), "sk-api-") {
		return ""
	}
	bases := []string{strings.TrimSpace(account.GetCredential("base_url"))}
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		bases = append(bases, nativeProtocolOverride(account, protocol))
	}
	selectedRoot := ""
	for _, base := range bases {
		if base == "" {
			continue
		}
		root := nativeProfileURL(base)
		if profile := nativeProfileForURL(account, base); profile != nil {
			root = strings.TrimSuffix(profile.Chat, "/v1")
		}
		if root != "https://api.minimax.cn" && root != "https://api.minimax.io" {
			return ""
		}
		if selectedRoot != "" && selectedRoot != root {
			return ""
		}
		selectedRoot = root
	}
	if selectedRoot == "" {
		return ""
	}
	return selectedRoot + "/account/query_balance"
}

// Official src/types/api.ts, src/sdk/quota/index.ts, test/sdk/quota.test.ts
// define a top-level string available_amount. src/output/balance.ts renders it
// unchanged, with no currency or scale specified. Preserve that magnitude in
// the existing numeric balance contract and leave currency unknown. The other
// wallet components are not additive to available_amount.
func parseMiniMaxPayGBalance(body []byte) (CNProviderBalanceEntry, error) {
	invalid := errors.New("Invalid MiniMax balance response") //nolint:staticcheck // ST1005: preserve the existing externally surfaced provider error text.
	if !gjson.ValidBytes(body) {
		return CNProviderBalanceEntry{}, invalid
	}
	status := gjson.GetBytes(body, "base_resp.status_code")
	if status.Type != gjson.Number || status.Float() != 0 {
		return CNProviderBalanceEntry{}, invalid
	}
	amount := gjson.GetBytes(body, "available_amount")
	if amount.Type != gjson.String {
		return CNProviderBalanceEntry{}, invalid
	}
	text := amount.String()
	// Bound exact decimal work before allocating big numbers from upstream input.
	if len(text) > 128 || !gjson.Valid(text) || gjson.Parse(text).Type != gjson.Number {
		return CNProviderBalanceEntry{}, invalid
	}
	if exponentIndex := strings.IndexAny(text, "eE"); exponentIndex >= 0 {
		exponent, err := strconv.ParseInt(text[exponentIndex+1:], 10, 32)
		if err != nil || exponent < -400 || exponent > 400 {
			return CNProviderBalanceEntry{}, invalid
		}
	}
	balance, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(balance) || math.IsInf(balance, 0) || math.Abs(balance) > 9007199254740991 {
		return CNProviderBalanceEntry{}, invalid
	}
	// Preserve the numeric JSON contract without silently rounding the supplier's
	// decimal value. Trailing zeros are insignificant; additional precision is not.
	original, valid := new(big.Rat).SetString(text)
	roundTrip, roundTripValid := new(big.Rat).SetString(strconv.FormatFloat(balance, 'g', -1, 64))
	if !valid || !roundTripValid || original.Cmp(roundTrip) != 0 {
		return CNProviderBalanceEntry{}, invalid
	}
	return CNProviderBalanceEntry{Balance: balance}, nil
}
