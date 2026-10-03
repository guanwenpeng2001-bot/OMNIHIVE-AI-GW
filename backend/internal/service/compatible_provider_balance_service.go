package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

// CompatibleBalanceResult keeps unknown amounts absent, rather than reporting
// zero. Balances uses the existing balance cell's multi-currency contract.
type CompatibleBalanceResult struct {
	Provider    string                   `json:"provider"`
	Success     bool                     `json:"success"`
	Unsupported bool                     `json:"unsupported,omitempty"`
	Balance     *float64                 `json:"balance,omitempty"`
	Currency    string                   `json:"currency,omitempty"`
	Balances    []CNProviderBalanceEntry `json:"balances,omitempty"`
	StatusCode  int                      `json:"status_code,omitempty"`
	FetchedAt   int64                    `json:"fetched_at"`
	Persisted   bool                     `json:"persisted"`
	Error       string                   `json:"error,omitempty"`
}

// QueryAccountBalance extends the existing injected admin balance service without
// changing native quota/balance behavior or probing management-only endpoints.
func (service *CNProviderBalanceService) QueryAccountBalance(ctx context.Context, accountID int64) (any, error) {
	if service == nil || service.accountRepo == nil || service.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "BALANCE_NOT_CONFIGURED", "balance service is not configured")
	}
	account, err := service.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, infraerrors.New(http.StatusNotFound, "BALANCE_ACCOUNT_NOT_FOUND", "account not found")
	}
	if _, compatible := domain.LookupCompatibleProvider(account.Platform); !compatible {
		return service.QueryBalanceForAccount(ctx, account)
	}
	return service.queryCompatibleBalance(ctx, account)
}

// These endpoints have documented inference-key authentication. Keep this small
// allowlist independent of inference/capability metadata: a model catalog never
// proves balance access. Sources and field contracts are recorded beside parser cases.
func compatibleBalanceEndpoint(provider string) string {
	switch provider {
	case "novita":
		return "https://api.novita.ai/openapi/v1/billing/balance/detail"
	case "ppio":
		return "https://api.ppio.com/openapi/v1/billing/balance/detail"
	case "siliconflow_cn":
		return "https://api.siliconflow.cn/v1/user/info"
	case "siliconflow_global":
		return "https://api.siliconflow.com/v1/user/info"
	case "shengsuanyun":
		return "https://router.shengsuanyun.com/api/v1/balance"
	case "soleapi":
		return "https://soleapi.com/v1/usage"
	default:
		return ""
	}
}

func compatibleBalanceTarget(account *Account) (string, error) {
	if account == nil || account.Type != AccountTypeAPIKey {
		return "", errors.New("balance query requires an API key account")
	}
	for _, field := range []string{"account_mode", "api_protocol", "base_url", "api_key"} {
		if value := account.Credentials[field]; value != nil {
			if _, valid := value.(string); !valid {
				return "", errors.New("invalid balance account credentials")
			}
		}
	}
	provider, found := domain.LookupCompatibleProvider(account.Platform)
	mode, valid := provider.ResolveMode(strings.TrimSpace(account.GetCredential("account_mode")))
	target := compatibleBalanceEndpoint(account.Platform)
	if !found || !valid || mode.Mode != AccountModePayG || target == "" {
		return "", errors.New("no documented inference-key balance endpoint for this provider and mode")
	}
	// A later inference registry change must not silently rebind a regional key
	// to this balance registry's fixed origin, including when no override is set.
	targetURL, _ := url.Parse(target)
	for _, endpoint := range mode.Endpoints {
		preset, err := url.Parse(endpoint.BaseURL)
		if err != nil || preset.Scheme != targetURL.Scheme || !strings.EqualFold(preset.Host, targetURL.Host) {
			return "", errors.New("balance and inference origins do not match")
		}
	}
	protocol := strings.TrimSpace(account.GetCredential("api_protocol"))
	if protocol != "" && protocol != "adaptive" {
		if _, valid := mode.LookupEndpoint(protocol); !valid {
			return "", errors.New("unsupported account protocol")
		}
	}
	// Only documented bases for this exact provider/mode may select its billing
	// endpoint. Never reinterpret a custom relay's API key as a preset vendor key.
	approved := func(raw string) bool {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return false
		}
		for _, endpoint := range mode.Endpoints {
			preset, err := url.Parse(endpoint.BaseURL)
			if err == nil && strings.EqualFold(parsed.Host, preset.Host) && strings.TrimRight(parsed.EscapedPath(), "/") == strings.TrimRight(preset.EscapedPath(), "/") {
				return true
			}
		}
		return false
	}
	if raw := account.GetCredential("base_url"); strings.TrimSpace(raw) != "" && !approved(raw) {
		return "", errors.New("balance query is unsupported for a custom base URL")
	}
	if value, exists := account.Credentials["api_base_urls"]; exists && value != nil {
		bases, valid := value.(map[string]any)
		if !valid {
			return "", errors.New("invalid protocol base URLs")
		}
		for _, value := range bases {
			raw, valid := value.(string)
			if !valid || (strings.TrimSpace(raw) != "" && !approved(raw)) {
				return "", errors.New("balance query is unsupported for custom protocol base URLs")
			}
		}
	}
	if account.IsCustomBaseURLEnabled() {
		return "", errors.New("balance query is unsupported for a custom relay")
	}
	return target, nil
}

func (service *CNProviderBalanceService) queryCompatibleBalance(ctx context.Context, account *Account) (*CompatibleBalanceResult, error) {
	result := &CompatibleBalanceResult{Provider: account.Platform, FetchedAt: time.Now().UTC().Unix()}
	target, err := compatibleBalanceTarget(account)
	if err != nil {
		result.Unsupported = true
		result.Error = err.Error()
		return result, nil
	}
	key := strings.TrimSpace(account.GetCredential("api_key"))
	if key == "" {
		result.Error = "account API key is empty"
		return result, nil
	}
	target, err = cnValidateProbeURL(service.cfg, target)
	if err != nil {
		return nil, infraerrors.New(http.StatusForbidden, "BALANCE_URL_REJECTED", "balance target rejected by URL security policy")
	}
	callCtx, cancel := context.WithTimeout(ctx, cnBalanceUpstreamTimeout)
	defer cancel()
	// Missing single-proxy configuration must not silently fall back to direct.
	proxyURL := ""
	if account.ProxyGroupID != nil {
		proxyURL, err = account.EgressProxyURL(callCtx)
	} else if account.ProxyID != nil {
		proxy := account.Proxy
		if proxy == nil && service.proxyRepo != nil {
			proxy, err = service.proxyRepo.GetByID(callCtx, *account.ProxyID)
		}
		if err == nil && proxy == nil {
			err = errors.New("configured proxy is unavailable")
		}
		if err == nil {
			proxyURL = proxy.URL()
		}
	}
	if err != nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "BALANCE_PROXY_UNAVAILABLE", "configured balance egress is unavailable")
	}
	request, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(callCtx), http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Accept", "application/json")
	response, err := accountHTTPUpstream(service.httpUpstream, account).Do(request, proxyURL, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		result.Error = "balance upstream request failed"
		return result, nil
	}
	if response == nil || response.Body == nil {
		result.Error = "invalid balance HTTP response"
		return result, nil
	}
	defer response.Body.Close()
	result.StatusCode = response.StatusCode
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Do not echo upstream bodies/errors: they can contain credentials or PII.
		result.Error = fmt.Sprintf("Balance API error (HTTP %d)", response.StatusCode)
		return result, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, cnBalanceMaxBodyBytes+1))
	if err != nil || len(body) > cnBalanceMaxBodyBytes {
		result.Error = "invalid or oversized balance response"
		return result, nil
	}
	entries, err := parseCompatibleBalance(account.Platform, body)
	if err != nil {
		result.Unsupported = errors.Is(err, errCompatibleBalanceUnsupported)
		result.Error = err.Error()
		return result, nil
	}
	result.Success = true
	result.Balances = entries
	result.Balance = &entries[0].Balance
	result.Currency = entries[0].Currency
	updates := map[string]any{
		cnExtraKey(account.Platform, cnBalanceExtraSuffixBalance):  entries[0].Balance,
		cnExtraKey(account.Platform, cnBalanceExtraSuffixCurrency): entries[0].Currency,
		cnExtraKey(account.Platform, cnBalanceExtraSuffixBalances): entries,
		cnExtraKey(account.Platform, cnBalanceExtraSuffixUpdated):  time.Unix(result.FetchedAt, 0).UTC().Format(time.RFC3339),
	}
	result.Persisted = persistAccountRuntimeSnapshot(callCtx, service.accountRepo, account, updates, nil) == nil
	return result, nil
}

var errCompatibleBalanceUnsupported = errors.New("response represents an unsupported balance scope")

func clearCompatibleBalanceSnapshots(extra map[string]any) {
	for _, provider := range []string{"novita", "ppio", "siliconflow_cn", "siliconflow_global", "shengsuanyun", "soleapi"} {
		for _, suffix := range []string{cnBalanceExtraSuffixBalance, cnBalanceExtraSuffixCurrency, cnBalanceExtraSuffixBalances, cnBalanceExtraSuffixUpdated} {
			delete(extra, cnExtraKey(provider, suffix))
		}
	}
}

func parseCompatibleBalance(provider string, body []byte) ([]CNProviderBalanceEntry, error) {
	invalid := errors.New("invalid balance response: missing or malformed amount or success status")
	if !gjson.ValidBytes(body) {
		return nil, invalid
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return nil, invalid
	}
	if root.Get("success").Type == gjson.False || root.Get("error").Exists() {
		return nil, invalid
	}
	path, currency, scale := "", "", float64(1)
	switch provider {
	case "novita", "ppio":
		// novita.ai/docs/api-reference/basic-get-user-balance.md
		// ppio.com/docs/management/reference-billing-get-balance-detail.md
		path, currency, scale = "availableBalance", "USD", 10000
		if provider == "ppio" {
			currency = "CNY"
		}
		if root.Get("error").Exists() || root.Get("code").Exists() {
			return nil, invalid
		}
	case "siliconflow_cn", "siliconflow_global":
		// docs.siliconflow.com/en/api-reference/userinfo/get-user-info
		if root.Get("code").Type != gjson.Number || root.Get("code").Num != 20000 || root.Get("status").Type != gjson.True {
			return nil, invalid
		}
		path, currency = "data.totalBalance", "CNY"
		if provider == "siliconflow_global" {
			currency = "USD"
		}
	case "shengsuanyun":
		// lean.shengsuanyun.com/apidocs/api/balance-api: enterprise responses
		// describe project budgets, not account balances (including unlimited).
		if root.Get("success").Type != gjson.True {
			return nil, invalid
		}
		if root.Get("data.gateway_type").String() == "enterprise" {
			return nil, errCompatibleBalanceUnsupported
		}
		if root.Get("data.gateway_type").String() != "common" {
			return nil, invalid
		}
		path, currency = "data.account_balance_cny", "CNY"
	case "soleapi":
		// soleapi.com/ja/docs/config/ccswitch and /ru/docs/guides/billing:
		// remaining is the balance, expressed in Credits, not a cash balance.
		if root.Get("unit").String() != "Credits" || root.Get("error").Exists() {
			return nil, invalid
		}
		path, currency = "remaining", "Credits"
	default:
		return nil, errCompatibleBalanceUnsupported
	}
	value := root.Get(path)
	if value.Type != gjson.Number && value.Type != gjson.String {
		return nil, invalid
	}
	if !gjson.Valid(value.String()) || gjson.Parse(value.String()).Type != gjson.Number {
		return nil, invalid
	}
	amount, err := strconv.ParseFloat(value.String(), 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return nil, invalid
	}
	return []CNProviderBalanceEntry{{Currency: currency, Balance: amount / scale}}, nil
}
