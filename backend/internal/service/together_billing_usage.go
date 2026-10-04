package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

const togetherBillingEndpoint = "https://api.together.ai/v1/billing/usage"
const togetherBillingMaxPages = 10

var togetherBillingMonth = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)
var togetherBillingDecimal = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

type TogetherBillingLine struct {
	ProductName       string                     `json:"product_name"`
	Quantity          string                     `json:"quantity"`
	UnitPrice         string                     `json:"unit_price"`
	Cost              string                     `json:"cost"`
	PricingDimensions map[string]json.RawMessage `json:"pricing_dimensions"`
	Attributes        map[string]json.RawMessage `json:"attributes"`
}
type TogetherBillingWindow struct {
	Date      string                `json:"date"`
	StartTime string                `json:"start_time"`
	EndTime   string                `json:"end_time"`
	LineItems []TogetherBillingLine `json:"line_items"`
}
type TogetherBillingUsage struct {
	Object              string                  `json:"object"`
	Scope               string                  `json:"scope"`
	OrganizationID      string                  `json:"organization_id"`
	BillingPeriod       string                  `json:"billing_period"`
	Granularity         string                  `json:"granularity"`
	EarliestWindowStart *string                 `json:"earliest_window_start"`
	LatestWindowEnd     *string                 `json:"latest_window_end"`
	Currency            string                  `json:"currency"`
	Data                []TogetherBillingWindow `json:"data"`
	NextCursor          *string                 `json:"next_cursor"`
	PagesFetched        int                     `json:"pages_fetched"`
	Partial             bool                    `json:"partial"`
}
type TogetherBillingResult struct {
	Provider    string                `json:"provider"`
	Kind        string                `json:"kind"`
	Success     bool                  `json:"success"`
	Unsupported bool                  `json:"unsupported,omitempty"`
	Reason      string                `json:"reason,omitempty"`
	StatusCode  int                   `json:"status_code,omitempty"`
	FetchedAt   int64                 `json:"fetched_at"`
	Persisted   bool                  `json:"persisted"`
	Error       string                `json:"error,omitempty"`
	Usage       *TogetherBillingUsage `json:"usage,omitempty"`
}

// QueryBillingUsage selects by the stored platform. Existing provider dispatch
// remains unchanged; callers cannot select another organization or upstream.
func (service *CNProviderBalanceService) QueryBillingUsage(ctx context.Context, accountID int64, query url.Values) (any, error) {
	if service == nil || service.accountRepo == nil || service.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "USAGE_NOT_CONFIGURED", "usage service is not configured")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	account, err := service.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, infraerrors.New(http.StatusNotFound, "USAGE_ACCOUNT_NOT_FOUND", "account not found")
	}
	if account.Platform == "azure_openai" || account.Platform == "github_copilot" {
		return service.QueryManagementUsage(ctx, accountID, query)
	}
	if account.Platform != "together" {
		return service.QueryProviderUsage(ctx, accountID, query)
	}
	for field, values := range query {
		if (field != "month" && field != "granularity") || len(values) != 1 {
			return nil, infraerrors.New(400, "USAGE_INVALID_QUERY", "invalid billing usage query parameters")
		}
	}
	if !togetherBillingMonth.MatchString(query.Get("month")) || query.Get("granularity") != "day" {
		return nil, infraerrors.New(400, "USAGE_INVALID_QUERY", "billing usage requires month YYYY-MM and granularity day")
	}
	return service.queryTogetherBilling(ctx, account, query.Get("month"))
}

// Keep this independent of wallet eligibility: Together has no verified wallet
// endpoint. Only registry-backed official payg configurations may use its key.
func togetherBillingAccount(account *Account) error {
	invalid := compatibleQueryError{"invalid_credentials", "invalid Together billing credentials"}
	if account.Type != AccountTypeAPIKey {
		return invalid
	}
	for _, field := range []string{"account_mode", "api_protocol", "base_url", "api_key"} {
		if value, exists := account.Credentials[field]; exists {
			if _, valid := value.(string); !valid {
				return invalid
			}
		}
	}
	key := strings.TrimSpace(account.GetCredential("api_key"))
	if key == "" {
		return invalid
	}
	for _, character := range key {
		if character < 33 || character > 126 {
			return invalid
		}
	}
	allowed, err := PaygBalanceAllowed(account.Platform, account.Credentials)
	provider, found := domain.LookupCompatibleProvider(account.Platform)
	mode, valid := provider.ResolveMode(strings.TrimSpace(account.GetCredential("account_mode")))
	if err != nil || !allowed || !found || !valid || mode.Mode != AccountModePayG {
		return compatibleQueryError{"mode_unsupported", "billing usage requires an official payg account"}
	}
	protocol := strings.TrimSpace(account.GetCredential("api_protocol"))
	if protocol != "" && protocol != "adaptive" {
		if _, valid := mode.LookupEndpoint(protocol); !valid {
			return invalid
		}
	}
	originError := compatibleQueryError{"custom_origin", "billing usage is unavailable for custom origins"}
	if enabled := account.Extra["custom_base_url_enabled"]; enabled != nil && enabled != false {
		return originError
	}
	for _, endpoint := range mode.Endpoints {
		preset, err := url.Parse(endpoint.BaseURL)
		if err != nil || preset.Scheme != "https" || preset.Host != "api.together.ai" {
			return originError
		}
	}
	approved := func(raw, protocol string) bool {
		if strings.TrimSpace(raw) == "" {
			return true
		}
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "api.together.ai") || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
			return false
		}
		for _, endpoint := range mode.Endpoints {
			preset, _ := url.Parse(endpoint.BaseURL)
			if (protocol == "" || protocol == endpoint.Protocol) && strings.TrimRight(parsed.EscapedPath(), "/") == strings.TrimRight(preset.EscapedPath(), "/") {
				return true
			}
		}
		return false
	}
	if !approved(account.GetCredential("base_url"), "") {
		return originError
	}
	if value, exists := account.Credentials["api_base_urls"]; exists {
		bases, valid := value.(map[string]any)
		if !valid {
			return originError
		}
		for protocol, value := range bases {
			if _, valid := mode.LookupEndpoint(protocol); !valid {
				return originError
			}
			raw, valid := value.(string)
			if !valid || !approved(raw, protocol) {
				return originError
			}
		}
	}
	return nil
}

func (service *CNProviderBalanceService) queryTogetherBilling(ctx context.Context, account *Account, month string) (*TogetherBillingResult, error) {
	result := &TogetherBillingResult{Provider: "together", Kind: "billing_usage", FetchedAt: time.Now().UTC().Unix()}
	if err := togetherBillingAccount(account); err != nil {
		result.Unsupported, result.Reason, result.Error = true, compatibleQueryReason(err), err.Error()
		return result, nil
	}
	target, err := cnValidateProbeURL(service.cfg, togetherBillingEndpoint)
	if err != nil {
		return nil, infraerrors.New(403, "USAGE_URL_REJECTED", "billing usage target rejected by URL policy")
	}
	callCtx, cancel := context.WithTimeout(ctx, cnBalanceUpstreamTimeout)
	defer cancel()
	query := url.Values{"month": {month}, "granularity": {"day"}, "limit": {"100"}}
	seenCursors, seenDates := map[string]bool{}, map[string]bool{}
	var usage *TogetherBillingUsage
	for pageIndex := 0; pageIndex < togetherBillingMaxPages; pageIndex++ {
		body, status, err := service.requestCompatibleQuery(callCtx, account, target+"?"+query.Encode(), strings.TrimSpace(account.GetCredential("api_key")))
		result.StatusCode = status
		if err != nil {
			result.Reason, result.Error = compatibleQueryReason(err), err.Error()
			if status == 404 {
				result.Reason, result.Error = "not_enabled", "Together billing beta is not enabled for this organization"
			}
			return result, nil
		}
		page, err := parseTogetherBillingPage(body, month)
		if err != nil {
			result.Reason, result.Error = "invalid_response", err.Error()
			return result, nil
		}
		if usage == nil {
			snapshot := *page
			usage = &snapshot
			usage.Data = []TogetherBillingWindow{}
		} else if usage.OrganizationID != page.OrganizationID {
			result.Reason, result.Error = "invalid_response", "billing organization changed during pagination"
			return result, nil
		}
		for _, window := range page.Data {
			if seenDates[window.Date] {
				result.Reason, result.Error = "invalid_response", "duplicate billing window"
				return result, nil
			}
			seenDates[window.Date] = true
			usage.Data = append(usage.Data, window)
		}
		usage.PagesFetched++
		usage.NextCursor = page.NextCursor
		usage.EarliestWindowStart, usage.LatestWindowEnd = page.EarliestWindowStart, page.LatestWindowEnd
		if page.NextCursor == nil {
			break
		}
		if seenCursors[*page.NextCursor] {
			result.Reason, result.Error = "invalid_response", "repeated billing cursor"
			return result, nil
		}
		seenCursors[*page.NextCursor] = true
		query.Set("after", *page.NextCursor)
	}
	usage.Partial = usage.NextCursor != nil
	result.Success, result.Usage = true, usage
	return result, nil
}

// Synthetic tests follow https://docs.together.ai/reference/billing-usage.
// Decimal amounts stay strings; unknown/missing amounts never become zero.
func parseTogetherBillingPage(body []byte, month string) (*TogetherBillingUsage, error) {
	invalid := errors.New("invalid Together billing usage response")
	if len(body) > cnBalanceMaxBodyBytes || !gjson.ValidBytes(body) {
		return nil, invalid
	}
	root := gjson.ParseBytes(body)
	if root.Get("object").String() != "list" || !compatibleRequiredText(root.Get("organization_id")) || root.Get("billing_period").String() != month || root.Get("currency").String() != "USD" || !root.Get("data").IsArray() || root.Get("error").Exists() {
		return nil, invalid
	}
	cursor := root.Get("next_cursor")
	if cursor.Raw != "null" && !compatibleRequiredText(cursor) {
		return nil, invalid
	}
	for _, field := range []string{"earliest_window_start", "latest_window_end"} {
		value := root.Get(field)
		if value.Raw == "null" {
			continue
		}
		if value.Type != gjson.String {
			return nil, invalid
		}
		if _, err := time.Parse(time.RFC3339, value.Str); err != nil {
			return nil, invalid
		}
	}
	earliest, latest := root.Get("earliest_window_start"), root.Get("latest_window_end")
	if (earliest.Raw == "null") != (latest.Raw == "null") || (len(root.Get("data").Array()) > 0 && earliest.Raw == "null") {
		return nil, invalid
	}
	if earliest.Raw != "null" {
		start, _ := time.Parse(time.RFC3339, earliest.Str)
		end, _ := time.Parse(time.RFC3339, latest.Str)
		if !end.After(start) {
			return nil, invalid
		}
	}
	for _, window := range root.Get("data").Array() {
		date := window.Get("date")
		day, dateErr := time.Parse("2006-01-02", date.String())
		start, startErr := time.Parse(time.RFC3339, window.Get("start_time").String())
		end, endErr := time.Parse(time.RFC3339, window.Get("end_time").String())
		if date.Type != gjson.String || dateErr != nil || !strings.HasPrefix(date.Str, month+"-") || startErr != nil || endErr != nil || !start.Equal(day) || !end.Equal(day.Add(24*time.Hour)) || !window.Get("line_items").IsArray() {
			return nil, invalid
		}
		for _, line := range window.Get("line_items").Array() {
			if !compatibleRequiredText(line.Get("product_name")) || !line.Get("pricing_dimensions").IsObject() || !line.Get("attributes").IsObject() {
				return nil, invalid
			}
			for _, field := range []string{"quantity", "unit_price", "cost"} {
				value := line.Get(field)
				if value.Type != gjson.String || len(value.Str) > 256 || !togetherBillingDecimal.MatchString(value.Str) {
					return nil, invalid
				}
			}
		}
	}
	var page TogetherBillingUsage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, invalid
	}
	page.Scope, page.Granularity = "organization", "day"
	page.PagesFetched, page.Partial = 0, false
	return &page, nil
}
