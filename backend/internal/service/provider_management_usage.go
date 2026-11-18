package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

const managementUsageMaxPages = 10
const managementUsageMaxItems = 4096

type ManagementUsageResult struct {
	Provider   string                 `json:"provider"`
	Kind       string                 `json:"kind"`
	Scope      string                 `json:"scope"`
	Success    bool                   `json:"success"`
	Partial    bool                   `json:"partial"`
	Reason     string                 `json:"reason,omitempty"`
	StatusCode int                    `json:"status_code,omitempty"`
	FetchedAt  int64                  `json:"fetched_at"`
	Persisted  bool                   `json:"persisted"`
	Quota      *AzureRegionalQuota    `json:"quota,omitempty"`
	Billing    *GitHubPersonalBilling `json:"billing,omitempty"`
}

type AzureQuotaName struct {
	Value          string  `json:"value"`
	LocalizedValue *string `json:"localizedValue"`
}
type AzureQuotaItem struct {
	Name         AzureQuotaName `json:"name"`
	CurrentValue *float64       `json:"currentValue"`
	Limit        *float64       `json:"limit"`
	Unit         *string        `json:"unit"`
}
type AzureRegionalQuota struct {
	SubscriptionID string           `json:"subscription_id"`
	Region         string           `json:"region"`
	Value          []AzureQuotaItem `json:"value"`
	PagesFetched   int              `json:"pages_fetched"`
	HasMore        bool             `json:"has_more"`
}
type GitHubBillingPeriod struct {
	Year  int  `json:"year"`
	Month *int `json:"month"`
}
type GitHubBillingItem struct {
	Product          *string  `json:"product"`
	SKU              *string  `json:"sku"`
	Model            *string  `json:"model"`
	UnitType         *string  `json:"unitType"`
	PricePerUnit     *float64 `json:"pricePerUnit"`
	GrossQuantity    *float64 `json:"grossQuantity"`
	GrossAmount      *float64 `json:"grossAmount"`
	DiscountQuantity *float64 `json:"discountQuantity"`
	DiscountAmount   *float64 `json:"discountAmount"`
	NetQuantity      *float64 `json:"netQuantity"`
	NetAmount        *float64 `json:"netAmount"`
}
type GitHubBillingReport struct {
	Report     string               `json:"report"`
	Success    bool                 `json:"success"`
	Reason     string               `json:"reason,omitempty"`
	StatusCode int                  `json:"status_code,omitempty"`
	TimePeriod *GitHubBillingPeriod `json:"timePeriod,omitempty"`
	User       string               `json:"user,omitempty"`
	UsageItems []GitHubBillingItem  `json:"usageItems"`
}
type GitHubPersonalBilling struct {
	Username string                `json:"username"`
	Year     int                   `json:"year"`
	Month    int                   `json:"month"`
	Reports  []GitHubBillingReport `json:"reports"`
}

var managementSubscriptionID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,128}$`)
var managementRegion = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)
var managementUsername = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)

// QueryManagementUsage is an admin read-only query on the existing injected
// service. The integration owner dispatches here using the stored platform.
// It never writes wallet snapshots, account limits, or scheduling state.
func (service *CNProviderBalanceService) QueryManagementUsage(ctx context.Context, accountID int64, query url.Values) (*ManagementUsageResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if service == nil || service.accountRepo == nil || service.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "USAGE_NOT_CONFIGURED", "usage service is not configured")
	}
	account, err := service.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, infraerrors.New(http.StatusNotFound, "USAGE_ACCOUNT_NOT_FOUND", "account not found")
	}
	result := &ManagementUsageResult{Provider: account.Platform, Kind: "unsupported", FetchedAt: time.Now().UTC().Unix()}
	switch account.Platform {
	case "azure_openai":
		result.Kind, result.Scope = "subscription_regional_quota", "subscription_region"
	case "github_copilot":
		result.Kind, result.Scope = "personal_billing_usage", "personal_account"
	default:
		result.Reason = "no_verified_query"
		return result, nil
	}
	for key, values := range query {
		if account.Platform != "github_copilot" || (key != "year" && key != "month") || len(values) != 1 {
			return nil, infraerrors.BadRequest("USAGE_INVALID_QUERY", "invalid management usage query parameters")
		}
	}
	if reason := managementAccountReason(account); reason != "" {
		result.Reason = reason
		return result, nil
	}
	token := strings.TrimSpace(account.GetCredential("usage_query_token"))
	if token == "" {
		result.Reason = "missing_query_credentials"
		return result, nil
	}
	for _, character := range token {
		if character < 33 || character > 126 {
			result.Reason = "invalid_credentials"
			return result, nil
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, cnBalanceUpstreamTimeout)
	defer cancel()
	if account.Platform == "azure_openai" {
		service.queryAzureRegionalQuota(callCtx, account, token, result)
	} else {
		year, month, err := managementBillingMonth(query, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		service.queryGitHubPersonalBilling(callCtx, account, token, year, month, result)
	}
	return result, nil
}

func managementAccountReason(account *Account) string {
	if account.Type != AccountTypeAPIKey {
		return "mode_unsupported"
	}
	provider, exists := domain.LookupCompatibleProvider(account.Platform)
	mode, valid := provider.ResolveMode(strings.TrimSpace(account.GetCredential("account_mode")))
	if !exists || !valid || mode.Mode != AccountModePayG {
		return "mode_unsupported"
	}
	if err := provider.ValidateBillingPlan(account.Credentials); err != nil {
		return "mode_unsupported"
	}
	for _, field := range []string{"account_mode", "api_protocol", "auth_mode", "base_url", "usage_query_token", "usage_query_subscription_id", "usage_query_region", "usage_query_username"} {
		if value, exists := account.Credentials[field]; exists && value != nil {
			if _, valid := value.(string); !valid {
				return "invalid_credentials"
			}
		}
	}
	protocol := strings.TrimSpace(account.GetCredential("api_protocol"))
	if protocol != "" && protocol != "adaptive" {
		if _, valid := mode.LookupEndpoint(protocol); !valid {
			return "mode_unsupported"
		}
	}
	if account.Platform == "github_copilot" {
		auth := account.GetCredential("auth_mode")
		if auth != "" && auth != CopilotAuthFineGrainedPAT {
			return "mode_unsupported"
		}
		if plan := account.GetCredential("billing_plan"); plan == "business" || plan == "enterprise" {
			return "mode_unsupported"
		}
	}
	if account.Platform == "azure_openai" {
		// Public Azure only: never infer sovereign-cloud management endpoints.
		publicBase := func(raw string) bool {
			if raw == "" {
				return true
			}
			parsed, err := url.Parse(raw)
			return err == nil && parsed.Scheme == "https" && parsed.User == nil && parsed.Port() == "" && parsed.RawQuery == "" && parsed.Fragment == "" &&
				(strings.HasSuffix(parsed.Hostname(), ".openai.azure.com") || strings.HasSuffix(parsed.Hostname(), ".cognitiveservices.azure.com") || strings.HasSuffix(parsed.Hostname(), ".services.ai.azure.com"))
		}
		if !publicBase(account.GetCredential("base_url")) {
			return "custom_origin"
		}
		if raw, exists := account.Credentials["api_base_urls"]; exists && raw != nil {
			bases, valid := raw.(map[string]any)
			if !valid {
				return "custom_origin"
			}
			for _, value := range bases {
				base, valid := value.(string)
				if !valid || !publicBase(base) {
					return "custom_origin"
				}
			}
		}
	}
	return ""
}

func managementBillingMonth(query url.Values, now time.Time) (int, int, error) {
	year, month := now.Year(), int(now.Month())
	invalid := infraerrors.BadRequest("USAGE_INVALID_WINDOW", "billing month must be within the current and preceding 23 UTC months")
	for key, destination := range map[string]*int{"year": &year, "month": &month} {
		if values, exists := query[key]; exists {
			if len(values) != 1 || values[0] == "" {
				return 0, 0, invalid
			}
			value, err := strconv.Atoi(values[0])
			if err != nil || strconv.Itoa(value) != values[0] {
				return 0, 0, invalid
			}
			*destination = value
		}
	}
	age := now.Year()*12 + int(now.Month()) - year*12 - month
	if year < 1000 || year > 9999 || month < 1 || month > 12 || age < 0 || age >= 24 {
		return 0, 0, invalid
	}
	return year, month, nil
}

// Same upstream, proxy binding, policy, timeout, redirect and body-size limits
// as other compatible queries. Additional fixed headers are needed for GitHub.
func (service *CNProviderBalanceService) requestManagementUsage(ctx context.Context, account *Account, target, token string) ([]byte, int, error) {
	validated, err := cnValidateProbeURL(service.cfg, target)
	if err != nil {
		return nil, 0, compatibleQueryError{"url_rejected", "query target rejected by URL security policy"}
	}
	proxy, err := service.resolveProxyURL(ctx, account)
	if err != nil || ((account.ProxyID != nil || account.ProxyGroupID != nil) && proxy == "") {
		return nil, 0, compatibleQueryError{"proxy_unavailable", "configured query egress is unavailable"}
	}
	request, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), http.MethodGet, validated, nil)
	if err != nil {
		return nil, 0, compatibleQueryError{"invalid_credentials", "invalid query request"}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if account.Platform == "github_copilot" {
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
		request.Header.Set("User-Agent", "sub2api-management-usage")
	}
	// accountHTTPUpstream adds inference-only Copilot authentication. Management
	// calls deliberately use the independent token and only reuse group egress.
	upstream := service.httpUpstream
	if account.ProxyGroupID != nil {
		upstream = &groupAccountUpstream{base: upstream, account: account}
	}
	response, err := upstream.Do(request, proxy, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, 0, compatibleQueryError{"upstream_error", "query upstream request failed"}
	}
	if response == nil || response.Body == nil {
		return nil, 0, compatibleQueryError{"invalid_response", "invalid query response"}
	}
	defer func() { _ = response.Body.Close() }()
	status := response.StatusCode
	if status != http.StatusOK {
		reason := "upstream_error"
		switch status {
		case 401:
			reason = "invalid_credentials"
		case 403:
			reason = "permission_denied"
		case 404:
			reason = "not_found"
		case 429:
			reason = "rate_limited"
		}
		return nil, status, compatibleQueryError{reason, "management query failed"}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, cnBalanceMaxBodyBytes+1))
	if err != nil || len(body) > cnBalanceMaxBodyBytes {
		return nil, status, compatibleQueryError{"invalid_response", "invalid or oversized query response"}
	}
	return body, status, nil
}

func managementOptionalText(value gjson.Result) (*string, bool) {
	if !value.Exists() || value.Type == gjson.Null {
		return nil, true
	}
	if value.Type != gjson.String || len(value.Str) > 512 {
		return nil, false
	}
	text := value.Str
	return &text, true
}
func managementOptionalNumber(value gjson.Result) (*float64, bool) {
	if !value.Exists() || value.Type == gjson.Null {
		return nil, true
	}
	if !compatibleFiniteNumber(value) {
		return nil, false
	}
	number := value.Num
	return &number, true
}
func managementResponseRoot(body []byte) (gjson.Result, error) {
	if len(body) > cnBalanceMaxBodyBytes || !gjson.ValidBytes(body) {
		return gjson.Result{}, errors.New("invalid management usage response")
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() || root.Get("error").Exists() || root.Get("message").Exists() {
		return gjson.Result{}, errors.New("invalid management usage response")
	}
	return root, nil
}
