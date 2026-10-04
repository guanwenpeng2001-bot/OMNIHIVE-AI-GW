package service

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
)

// Source: Microsoft Cognitive Services Usages List, API 2024-10-01.
// Count is the supplier's raw unit, not USD or a token-to-1000 conversion.
func (service *CNProviderBalanceService) queryAzureRegionalQuota(ctx context.Context, account *Account, token string, result *ManagementUsageResult) {
	subscription := strings.TrimSpace(account.GetCredential("usage_query_subscription_id"))
	region := strings.TrimSpace(account.GetCredential("usage_query_region"))
	if subscription == "" || region == "" {
		result.Reason = "missing_query_credentials"
		return
	}
	if !managementSubscriptionID.MatchString(subscription) || !managementRegion.MatchString(region) {
		result.Reason = "invalid_credentials"
		return
	}
	target := "https://management.azure.com/subscriptions/" + url.PathEscape(subscription) + "/providers/Microsoft.CognitiveServices/locations/" + url.PathEscape(region) + "/usages?api-version=2024-10-01"
	initial, _ := url.Parse(target)
	quota := &AzureRegionalQuota{SubscriptionID: subscription, Region: region, Value: make([]AzureQuotaItem, 0)}
	seen := make(map[string]bool)
	for page := 0; page < managementUsageMaxPages; page++ {
		if seen[target] {
			result.Reason = "invalid_response"
			break
		}
		seen[target] = true
		body, status, err := service.requestManagementUsage(ctx, account, target, token)
		result.StatusCode = status
		if err != nil {
			result.Reason = compatibleQueryReason(err)
			break
		}
		items, next, err := parseAzureRegionalQuota(body)
		if err != nil || len(quota.Value)+len(items) > managementUsageMaxItems {
			result.Reason = "invalid_response"
			break
		}
		quota.PagesFetched++
		quota.Value = append(quota.Value, items...)
		quota.HasMore = next != ""
		if next == "" {
			result.Success = true
			break
		}
		if !azureQuotaNextLinkAllowed(initial, next) {
			result.Reason = "invalid_response"
			break
		}
		target = next
	}
	if !result.Success && result.Reason == "" {
		result.Reason = "pagination_limit"
	}
	if quota.PagesFetched > 0 {
		result.Quota = quota
		result.Partial = !result.Success
	}
}

func azureQuotaNextLinkAllowed(initial *url.URL, raw string) bool {
	if len(raw) > 8192 {
		return false
	}
	next, err := url.Parse(raw)
	if err != nil || next.Scheme != "https" || next.Host != initial.Host || next.User != nil || next.Fragment != "" || next.Opaque != "" || next.EscapedPath() != initial.EscapedPath() {
		return false
	}
	query, err := url.ParseQuery(next.RawQuery)
	return err == nil && len(query["api-version"]) == 1 && query.Get("api-version") == "2024-10-01"
}

func parseAzureRegionalQuota(body []byte) ([]AzureQuotaItem, string, error) {
	invalid := errors.New("invalid Azure quota response")
	root, err := managementResponseRoot(body)
	if err != nil || !root.Get("value").IsArray() {
		return nil, "", invalid
	}
	next, valid := managementOptionalText(root.Get("nextLink"))
	// Continuation URLs may exceed ordinary label lengths.
	if continuation := root.Get("nextLink"); continuation.Type == gjson.String && len(continuation.Str) <= 8192 {
		value := continuation.Str
		next, valid = &value, true
	}
	if !valid {
		return nil, "", invalid
	}
	items := make([]AzureQuotaItem, 0)
	root.Get("value").ForEach(func(_, value gjson.Result) bool {
		if len(items) >= managementUsageMaxItems || !value.IsObject() || !value.Get("name").IsObject() || !compatibleRequiredText(value.Get("name.value")) {
			valid = false
			return false
		}
		item := AzureQuotaItem{Name: AzureQuotaName{Value: value.Get("name.value").Str}}
		for field, destination := range map[string]**string{"name.localizedValue": &item.Name.LocalizedValue, "unit": &item.Unit} {
			*destination, valid = managementOptionalText(value.Get(field))
			if !valid {
				return false
			}
		}
		for field, destination := range map[string]**float64{"currentValue": &item.CurrentValue, "limit": &item.Limit} {
			*destination, valid = managementOptionalNumber(value.Get(field))
			if !valid {
				return false
			}
		}
		items = append(items, item)
		return true
	})
	if !valid {
		return nil, "", invalid
	}
	if next == nil {
		return items, "", nil
	}
	return items, *next, nil
}
