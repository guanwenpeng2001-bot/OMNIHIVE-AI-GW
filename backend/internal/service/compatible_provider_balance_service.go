package service

import (
	"context"
	"errors"
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
	Kind          string                    `json:"kind"`
	Reason        string                    `json:"reason,omitempty"`
	Scope         string                    `json:"scope,omitempty"`
	ProjectBudget *CompatibleProjectBudget  `json:"project_budget,omitempty"`
	Provider      string                    `json:"provider"`
	Success       bool                      `json:"success"`
	Unsupported   bool                      `json:"unsupported,omitempty"`
	Used          *float64                  `json:"used,omitempty"`
	Credits       *CompatibleAccountCredits `json:"credits,omitempty"`
	Balance       *float64                  `json:"balance,omitempty"`
	Currency      string                    `json:"currency,omitempty"`
	Balances      []CNProviderBalanceEntry  `json:"balances,omitempty"`
	StatusCode    int                       `json:"status_code,omitempty"`
	FetchedAt     int64                     `json:"fetched_at"`
	Persisted     bool                      `json:"persisted"`
	Error         string                    `json:"error,omitempty"`
}

// QueryAccountBalance extends the existing injected admin balance service without
// changing native quota/balance behavior. Management queries use their own credentials.
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
		result, err := service.QueryBalanceForAccount(ctx, account)
		if err != nil {
			if errors.Is(err, ErrAccountRuntimeIdentityChanged) {
				publicResult := &CompatibleBalanceResult{
					Kind: "wallet_balance", Provider: account.Platform, FetchedAt: time.Now().UTC().Unix(),
				}
				publicResult.applyPersistenceOutcome(err)
				return publicResult, nil
			}
			return nil, err
		}
		if account.Platform != PlatformMiniMax {
			return result, nil
		}
		// MiniMax provides no availability signal. Project the native observation
		// into the existing optional-amount contract instead of exposing its
		// internal bool placeholder or an unknown balance as a synthetic zero.
		publicResult := &CompatibleBalanceResult{
			Kind: "wallet_balance", Provider: result.Provider, Success: result.Success,
			StatusCode: result.StatusCode, FetchedAt: result.FetchedAt,
			Persisted: result.Persisted, Error: result.Error,
		}
		if result.Success {
			publicResult.Balance = &result.Balance
			publicResult.Currency = result.Currency
			publicResult.Balances = result.Balances
		}
		return publicResult, nil
	}
	return service.queryCompatibleBalance(ctx, account)
}

// These endpoints have documented inference-key authentication. Keep this small
// allowlist independent of inference/capability metadata: a model catalog never
// proves balance access. Sources and field contracts are recorded beside parser cases.
func compatibleBalanceEndpoint(provider string) string {
	switch provider {
	case "stepfun_api":
		return "https://api.stepfun.com/v1/accounts"
	case "stepfun_api_intl":
		return "https://api.stepfun.ai/v1/accounts"
	case "teamorouter":
		return "https://teamorouter.cn/v1/billing/balance"
	case "teamorouter_com":
		return "https://teamorouter.com/v1/billing/balance"
	case "sudocode_chat":
		return "https://api.sudocode.chat/api/account/balance"
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
	case "atlascloud":
		return "https://api.atlascloud.ai/public/v1/balance"
	default:
		return ""
	}
}

func compatibleBalanceTarget(account *Account) (string, error) {
	target, kind, err := compatibleUsageTarget(account)
	if err == nil && kind != "wallet_balance" {
		return "", compatibleQueryError{"no_verified_query", "this provider exposes consumed usage, not a wallet balance"}
	}
	return target, err
}

func compatibleUsageTarget(account *Account) (string, string, error) {
	target, err := compatibleQueryTarget(account)
	kind := "wallet_balance"
	if account != nil && account.Platform == "qiniu" {
		kind = "consumed_usage"
	}
	if err != nil {
		kind = "unsupported"
	}
	return target, kind, err
}

func compatibleQueryTarget(account *Account) (string, error) {
	if account == nil || account.Type != AccountTypeAPIKey {
		return "", errors.New("balance query requires an API key account")
	}
	allowed, err := PaygBalanceAllowed(account.Platform, account.Credentials)
	if err != nil {
		return "", compatibleQueryError{"invalid_credentials", "invalid billing plan selection"}
	}
	if !allowed {
		return "", compatibleQueryError{"mode_unsupported", "no verified query for the selected billing plan"}
	}
	management := managementBalanceEndpoint(account.Platform) != ""
	for _, field := range []string{"account_mode", "api_protocol", "base_url", "api_key"} {
		if management && field == "api_key" {
			continue
		}
		if value, exists := account.Credentials[field]; exists {
			if _, valid := value.(string); !valid {
				return "", errors.New("invalid balance account credentials")
			}
		}
	}
	if !management {
		key := strings.TrimSpace(account.GetCredential("api_key"))
		if key == "" || strings.ContainsAny(key, "\r\n\t ") {
			return "", errors.New("invalid query API key")
		}
		for _, character := range key {
			if character < 33 || character > 126 {
				return "", errors.New("invalid query API key")
			}
		}
	}
	provider, found := domain.LookupCompatibleProvider(account.Platform)
	mode, valid := provider.ResolveMode(strings.TrimSpace(account.GetCredential("account_mode")))
	target := compatibleBalanceEndpoint(account.Platform)
	if management {
		target = managementBalanceEndpoint(account.Platform)
	}
	if account.Platform == "qiniu" {
		target = "https://api.qnaigc.com/v3/stat/usage"
	}
	if !found || !valid || mode.Mode != AccountModePayG || target == "" {
		reason := "no_verified_query"
		if found && (!valid || mode.Mode != AccountModePayG) {
			reason = "mode_unsupported"
		}
		return "", compatibleQueryError{reason, "no verified inference-key query for this provider and mode"}
	}
	// Management queries retain the preset inference-origin guard below, but their
	// fixed management host need not equal the inference host. Inference-key
	// queries bind inference to the fixed query origin. TeamoRouter documents
	// billing on the regional apex, separately from its api. inference host.
	targetURL, _ := url.Parse(target)
	inferenceHost := targetURL.Host
	switch account.Platform {
	case "teamorouter":
		inferenceHost = "api.teamorouter.cn"
	case "teamorouter_com":
		inferenceHost = "api.teamorouter.com"
	}
	for _, endpoint := range mode.Endpoints {
		preset, err := url.Parse(endpoint.BaseURL)
		if err != nil || preset.Scheme != targetURL.Scheme || (!management && !strings.EqualFold(preset.Host, inferenceHost)) {
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
		if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
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
	if value, exists := account.Credentials["api_base_urls"]; exists {
		bases, valid := value.(map[string]any)
		if !valid {
			return "", errors.New("invalid protocol base URLs")
		}
		for protocol, value := range bases {
			endpoint, valid := mode.LookupEndpoint(protocol)
			if !valid {
				return "", errors.New("invalid protocol base URL key")
			}
			raw, valid := value.(string)
			if !valid || (strings.TrimSpace(raw) != "" && !approved(raw)) {
				return "", errors.New("balance query is unsupported for custom protocol base URLs")
			}
			if strings.TrimSpace(raw) != "" {
				parsed, _ := url.Parse(strings.TrimSpace(raw))
				preset, _ := url.Parse(endpoint.BaseURL)
				if strings.TrimRight(parsed.EscapedPath(), "/") != strings.TrimRight(preset.EscapedPath(), "/") {
					return "", errors.New("query is unsupported for custom protocol paths")
				}
			}
		}
	}
	if enabled, exists := account.Extra["custom_base_url_enabled"]; exists && enabled != false && enabled != nil {
		return "", errors.New("balance query is unsupported for a custom relay")
	}
	return target, nil
}

func (service *CNProviderBalanceService) queryCompatibleBalance(ctx context.Context, account *Account) (*CompatibleBalanceResult, error) {
	if managementBalanceEndpoint(account.Platform) != "" {
		return service.queryManagementBalance(ctx, account)
	}
	result := &CompatibleBalanceResult{Provider: account.Platform, Kind: "wallet_balance", FetchedAt: time.Now().UTC().Unix()}
	target, err := compatibleBalanceTarget(account)
	if err != nil {
		result.Unsupported = true
		result.Kind = "unsupported"
		result.Reason = compatibleQueryReason(err)
		result.Error = err.Error()
		return result, nil
	}
	key := strings.TrimSpace(account.GetCredential("api_key"))
	if account.Platform == "atlascloud" && (!strings.HasPrefix(key, "apikey-") || len(key) <= len("apikey-")) {
		result.Error = "balance requires an ordinary Atlas Cloud API key"
		result.Reason = "invalid_credentials"
		return result, nil
	}
	target, err = cnValidateProbeURL(service.cfg, target)
	if err != nil {
		return nil, infraerrors.New(http.StatusForbidden, "BALANCE_URL_REJECTED", "balance target rejected by URL security policy")
	}
	callCtx, cancel := context.WithTimeout(ctx, cnBalanceUpstreamTimeout)
	defer cancel()
	body, status, err := service.requestCompatibleQuery(callCtx, account, target, key)
	result.StatusCode = status
	if err != nil {
		result.Error = err.Error()
		result.Reason = compatibleQueryReason(err)
		return result, nil
	}
	if account.Platform == "shengsuanyun" && gjson.GetBytes(body, "data.gateway_type").String() == "enterprise" {
		result.Kind = "project_budget"
		result.ProjectBudget, err = parseCompatibleProjectBudget(body)
		if err != nil {
			result.Reason, result.Error = "invalid_response", err.Error()
			return result, nil
		}
		result.Success = true
		return result, nil
	}
	entries, err := parseCompatibleBalance(account.Platform, body)
	if err != nil {
		result.Unsupported = errors.Is(err, errCompatibleBalanceUnsupported)
		result.Error = err.Error()
		result.Reason = "invalid_response"
		return result, nil
	}
	result.Success = true
	result.Balances = entries
	result.Balance = &entries[0].Balance
	result.Currency = entries[0].Currency
	switch account.Platform {
	case "atlascloud", "stepfun_api", "stepfun_api_intl", "teamorouter", "teamorouter_com":
		result.Scope = "account"
	}
	updates := map[string]any{
		cnExtraKey(account.Platform, cnBalanceExtraSuffixBalance):  entries[0].Balance,
		cnExtraKey(account.Platform, cnBalanceExtraSuffixCurrency): entries[0].Currency,
		cnExtraKey(account.Platform, cnBalanceExtraSuffixBalances): entries,
		cnExtraKey(account.Platform, cnBalanceExtraSuffixUpdated):  time.Unix(result.FetchedAt, 0).UTC().Format(time.RFC3339),
	}
	result.applyPersistenceOutcome(persistAccountRuntimeSnapshot(callCtx, service.accountRepo, account, updates, nil))
	return result, nil
}

// A rejected identity means these observations belong to an obsolete account
// configuration. Ordinary storage failures do not invalidate the upstream read.
func (result *CompatibleBalanceResult) applyPersistenceOutcome(err error) {
	result.Persisted = err == nil
	if !errors.Is(err, ErrAccountRuntimeIdentityChanged) {
		return
	}
	result.Success = false
	result.Reason = "identity_changed"
	result.Error = "Account configuration changed; query again."
	result.Balance = nil
	result.Balances = nil
	result.Used = nil
	result.Credits = nil
	result.ProjectBudget = nil
	result.Currency = ""
}

var errCompatibleBalanceUnsupported = errors.New("response represents an unsupported balance scope")

func clearCompatibleBalanceSnapshots(extra map[string]any) {
	for _, provider := range []string{"novita", "ppio", "siliconflow_cn", "siliconflow_global", "shengsuanyun", "soleapi", "atlascloud", "stepfun_api", "stepfun_api_intl", "teamorouter", "teamorouter_com", "sudocode_chat", "aihubmix", "dmxapi", "crazyrouter", "crazyrouter_intl", "therouter"} {
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
	case "teamorouter", "teamorouter_com":
		// https://teamorouter.cn/docs/open-api and https://teamorouter.com/docs/open-api
		if root.Get("balance.value").Type != gjson.String || root.Get("balance.currency").Type != gjson.String {
			return nil, invalid
		}
		path, currency = "balance.value", root.Get("balance.currency").String()
		if len(currency) != 3 || strings.Trim(currency, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
			return nil, invalid
		}
	case "sudocode_chat":
		// https://sudocode.chat/docs/claude-code: the display balance includes
		// wallet and subscriptions; token.remaining must not be added again.
		if root.Get("success").Type != gjson.True || root.Get("data.ccswitch.remaining").Type != gjson.Number || root.Get("data.ccswitch.unit").Type != gjson.String {
			return nil, invalid
		}
		path, currency = "data.ccswitch.remaining", root.Get("data.ccswitch.unit").String()
		if strings.TrimSpace(currency) == "" {
			return nil, invalid
		}
	case "stepfun_api", "stepfun_api_intl":
		// https://platform.stepfun.com/docs/zh/api-reference/accounts/get.md
		// https://platform.stepfun.ai/docs/en/api-reference/accounts/get.md
		// Only balance is available funds; cash/voucher totals are cumulative.
		// Neither regional contract specifies a currency.
		accountType := root.Get("type").String()
		if root.Get("object").String() != "account" || (accountType != "prepaid" && accountType != "postpaid") || root.Get("balance").Type != gjson.Number {
			return nil, invalid
		}
		path = "balance"
	case "atlascloud":
		// https://www.atlascloud.ai/docs/public-api/balance
		if root.Get("object").String() != "balance" || root.Get("scope").String() != "account" || root.Get("available.value").Type != gjson.String {
			return nil, invalid
		}
		path, currency = "available.value", root.Get("available.currency").String()
		if len(currency) != 3 || strings.Trim(currency, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
			return nil, invalid
		}
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
