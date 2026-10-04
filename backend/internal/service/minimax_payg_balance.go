package service

import (
	"errors"
	"math"
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
	invalid := errors.New("Invalid MiniMax balance response")
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
	balance, err := strconv.ParseFloat(amount.String(), 64)
	if err != nil || math.IsNaN(balance) || math.IsInf(balance, 0) {
		return CNProviderBalanceEntry{}, invalid
	}
	return CNProviderBalanceEntry{Balance: balance}, nil
}
