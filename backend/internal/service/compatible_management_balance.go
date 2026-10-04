package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/tidwall/gjson"
	"io"
	"math"
	"net/http"
	"time"
)

// Fixed management origins are independent of inference endpoints and keys.
func managementBalanceEndpoint(provider string) string {
	switch provider {
	case "aihubmix":
		return "https://aihubmix.com/api/user/self"
	case "dmxapi":
		return "https://www.dmxapi.cn/api/user/self"
	case "crazyrouter", "crazyrouter_intl":
		return "https://crazyrouter.com/api/user/self"
	case "therouter":
		return "https://api.therouter.ai/api/v1/credits"
	}
	return ""
}

type CompatibleAccountCredits struct {
	TotalCredits float64 `json:"total_credits"`
	TotalUsage   float64 `json:"total_usage"`
	Unit         string  `json:"unit"`
}

func (service *CNProviderBalanceService) queryManagementBalance(ctx context.Context, account *Account) (*CompatibleBalanceResult, error) {
	result := &CompatibleBalanceResult{Provider: account.Platform, Kind: "wallet_balance", Scope: "account", FetchedAt: time.Now().UTC().Unix()}
	if account.Platform == "therouter" {
		result.Kind = "account_credits"
	}
	target, err := compatibleQueryTarget(account)
	if err != nil {
		result.Reason, result.Error = compatibleQueryReason(err), "management query account is not eligible"
		result.Unsupported = true
		return result, nil
	}
	headers, err := managementQueryHeaders(account)
	if err != nil {
		result.Reason, result.Error = compatibleQueryReason(err), err.Error()
		return result, nil
	}
	target, err = cnValidateProbeURL(service.cfg, target)
	if err != nil {
		result.Reason, result.Error = "custom_origin", "management query target rejected"
		return result, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, cnBalanceUpstreamTimeout)
	defer cancel()
	body, status, err := service.requestManagementQuery(callCtx, account, target, headers)
	result.StatusCode = status
	if err != nil {
		result.Reason, result.Error = compatibleQueryReason(err), err.Error()
		return result, nil
	}
	var metadata []byte
	if account.Platform == "crazyrouter" || account.Platform == "crazyrouter_intl" {
		statusTarget, validationErr := cnValidateProbeURL(service.cfg, "https://crazyrouter.com/api/status")
		if validationErr != nil {
			result.Reason, result.Error = "custom_origin", "management query target rejected"
			return result, nil
		}
		// This is public metadata: do not send either management identity or inference key.
		metadata, status, err = service.requestManagementQuery(callCtx, account, statusTarget, nil)
		result.StatusCode = status
		if err != nil {
			result.Reason, result.Error = compatibleQueryReason(err), err.Error()
			return result, nil
		}
	}
	if err := parseManagementBalance(result, body, metadata); err != nil {
		result.Reason, result.Error = "invalid_response", "invalid management balance response"
		return result, nil
	}
	result.Success = true
	if result.Balance != nil {
		updates := map[string]any{
			cnExtraKey(account.Platform, cnBalanceExtraSuffixBalance):  *result.Balance,
			cnExtraKey(account.Platform, cnBalanceExtraSuffixCurrency): result.Currency,
			cnExtraKey(account.Platform, cnBalanceExtraSuffixBalances): result.Balances,
			cnExtraKey(account.Platform, cnBalanceExtraSuffixUpdated):  time.Unix(result.FetchedAt, 0).UTC().Format(time.RFC3339),
		}
		result.Persisted = persistAccountRuntimeSnapshot(callCtx, service.accountRepo, account, updates, nil) == nil
	}
	return result, nil
}

func parseManagementBalance(result *CompatibleBalanceResult, body, metadata []byte) error {
	invalid := errors.New("invalid management balance response")
	if !gjson.ValidBytes(body) {
		return invalid
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() || root.Get("error").Exists() {
		return invalid
	}
	if result.Provider == "therouter" {
		// https://therouter.ai/docs/api/api-reference/credits/get-credits/
		total, used := root.Get("data.total_credits"), root.Get("data.total_usage")
		if root.Get("success").Exists() && root.Get("success").Type != gjson.True {
			return invalid
		}
		if !compatibleFiniteNumber(total) || !compatibleFiniteNumber(used) {
			return invalid
		}
		result.Credits = &CompatibleAccountCredits{TotalCredits: total.Num, TotalUsage: used.Num, Unit: "credits"}
		return nil
	}
	if root.Get("success").Type != gjson.True || !compatibleFiniteNumber(root.Get("data.quota")) {
		return invalid
	}
	divisor, currency, includeUsed := float64(0), "USD", true
	switch result.Provider {
	case "aihubmix":
		// https://docs.aihubmix.com/cn/api/Cli: raw Authorization, USD quota.
		divisor = 500000
	case "dmxapi":
		// https://doc.dmxapi.cn/yuer.html: domestic account quota is CNY.
		divisor, currency, includeUsed = 500000, "CNY", false
	case "crazyrouter", "crazyrouter_intl":
		// https://docs.crazyrouter.com/en/token-management/logs-and-costs
		if !gjson.ValidBytes(metadata) {
			return invalid
		}
		status := gjson.ParseBytes(metadata)
		scale, display := status.Get("data.quota_per_unit"), status.Get("data.quota_display_type")
		if status.Get("success").Type != gjson.True || !compatibleFiniteNumber(scale) || scale.Num <= 0 || (display.Exists() && (display.Type != gjson.String || display.Str != "USD")) {
			return invalid
		}
		divisor = scale.Num
	default:
		return invalid
	}
	amount := root.Get("data.quota").Num / divisor
	if math.IsInf(amount, 0) || math.IsNaN(amount) {
		return invalid
	}
	var used *float64
	if includeUsed {
		value := root.Get("data.used_quota")
		if !compatibleFiniteNumber(value) {
			return invalid
		}
		normalized := value.Num / divisor
		if math.IsInf(normalized, 0) || math.IsNaN(normalized) {
			return invalid
		}
		used = &normalized
	}
	result.Balance, result.Used, result.Currency = &amount, used, currency
	result.Balances = []CNProviderBalanceEntry{{Currency: currency, Balance: amount}}
	return nil
}

func (service *CNProviderBalanceService) requestManagementQuery(ctx context.Context, account *Account, target string, headers http.Header) ([]byte, int, error) {
	proxyURL := ""
	var err error
	if account.ProxyGroupID != nil {
		proxyURL, err = account.EgressProxyURL(ctx)
	} else if account.ProxyID != nil {
		proxy := account.Proxy
		if proxy == nil && service.proxyRepo != nil {
			proxy, err = service.proxyRepo.GetByID(ctx, *account.ProxyID)
		}
		if err == nil && proxy == nil {
			err = errors.New("configured proxy is unavailable")
		}
		if err == nil {
			proxyURL = proxy.URL()
			if proxyURL == "" {
				err = errors.New("configured proxy is empty")
			}
		}
	}
	if err != nil {
		return nil, 0, compatibleQueryError{"proxy_unavailable", "configured query egress is unavailable"}
	}
	request, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, compatibleQueryError{"invalid_credentials", "invalid query request"}
	}
	for name, values := range headers {
		request.Header[name] = append([]string(nil), values...)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := accountHTTPUpstream(service.httpUpstream, account).Do(request, proxyURL, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, 0, compatibleQueryError{"upstream_error", "query upstream request failed"}
	}
	if response == nil || response.Body == nil {
		return nil, 0, compatibleQueryError{"invalid_response", "invalid query HTTP response"}
	}
	defer func() { _ = response.Body.Close() }()
	status := response.StatusCode
	if status < 200 || status >= 300 {
		reason := "upstream_error"
		switch status {
		case http.StatusUnauthorized:
			reason = "invalid_credentials"
		case http.StatusForbidden:
			reason = "permission_denied"
		case http.StatusTooManyRequests:
			reason = "rate_limited"
		}
		return nil, status, compatibleQueryError{reason, fmt.Sprintf("Query API error (HTTP %d)", status)}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, cnBalanceMaxBodyBytes+1))
	if err != nil || len(body) > cnBalanceMaxBodyBytes {
		return nil, status, compatibleQueryError{"invalid_response", "invalid or oversized query response"}
	}
	return body, status, nil
}
