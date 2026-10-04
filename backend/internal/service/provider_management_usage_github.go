package service

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// GitHub billing usage API 2026-03-10: these reports cover personal billing
// only. No currency is documented, and the reports must never be summed.
func (service *CNProviderBalanceService) queryGitHubPersonalBilling(ctx context.Context, account *Account, token string, year, month int, result *ManagementUsageResult) {
	username := strings.TrimSpace(account.GetCredential("usage_query_username"))
	if username == "" {
		result.Reason = "missing_query_credentials"
		return
	}
	if !managementUsername.MatchString(username) {
		result.Reason = "invalid_credentials"
		return
	}
	billing := &GitHubPersonalBilling{Username: username, Year: year, Month: month, Reports: make([]GitHubBillingReport, 0, 2)}
	query := url.Values{"year": {strconv.Itoa(year)}, "month": {strconv.Itoa(month)}}
	successes := 0
	for _, kind := range []string{"premium_request", "ai_credit"} {
		target := "https://api.github.com/users/" + url.PathEscape(username) + "/settings/billing/" + kind + "/usage?" + query.Encode()
		body, status, err := service.requestManagementUsage(ctx, account, target, token)
		report := GitHubBillingReport{Report: kind, StatusCode: status}
		if err != nil {
			report.Reason = compatibleQueryReason(err)
		} else {
			parsed, parseErr := parseGitHubBilling(body, username, year, month)
			if parseErr != nil {
				report.Reason = "invalid_response"
			} else {
				report.TimePeriod, report.User, report.UsageItems, report.Success = parsed.TimePeriod, parsed.User, parsed.UsageItems, true
				successes++
			}
		}
		billing.Reports = append(billing.Reports, report)
	}
	result.Billing = billing
	result.Success, result.Partial = successes == 2, successes == 1
	if !result.Success {
		result.Reason = "report_failed"
	}
}

func parseGitHubBilling(body []byte, username string, year, month int) (*GitHubBillingReport, error) {
	invalid := errors.New("invalid GitHub billing response")
	root, err := managementResponseRoot(body)
	if err != nil || !root.Get("usageItems").IsArray() || !root.Get("timePeriod").IsObject() || !compatibleRequiredText(root.Get("user")) || !strings.EqualFold(root.Get("user").Str, username) {
		return nil, invalid
	}
	periodYear := root.Get("timePeriod.year")
	if !compatibleFiniteNumber(periodYear) || periodYear.Num != float64(year) {
		return nil, invalid
	}
	period := &GitHubBillingPeriod{Year: year}
	periodMonth := root.Get("timePeriod.month")
	if periodMonth.Exists() && periodMonth.Type != gjson.Null {
		if !compatibleFiniteNumber(periodMonth) || periodMonth.Num != float64(month) {
			return nil, invalid
		}
		period.Month = &month
	}
	if root.Get("timePeriod.day").Exists() {
		return nil, invalid
	}
	report := &GitHubBillingReport{TimePeriod: period, User: root.Get("user").Str, UsageItems: make([]GitHubBillingItem, 0)}
	valid := true
	root.Get("usageItems").ForEach(func(_, value gjson.Result) bool {
		if !value.IsObject() || len(report.UsageItems) >= managementUsageMaxItems {
			valid = false
			return false
		}
		item := GitHubBillingItem{}
		for field, destination := range map[string]**string{"product": &item.Product, "sku": &item.SKU, "model": &item.Model, "unitType": &item.UnitType} {
			*destination, valid = managementOptionalText(value.Get(field))
			if !valid {
				return false
			}
		}
		for field, destination := range map[string]**float64{"pricePerUnit": &item.PricePerUnit, "grossQuantity": &item.GrossQuantity, "grossAmount": &item.GrossAmount, "discountQuantity": &item.DiscountQuantity, "discountAmount": &item.DiscountAmount, "netQuantity": &item.NetQuantity, "netAmount": &item.NetAmount} {
			*destination, valid = managementOptionalNumber(value.Get(field))
			if !valid {
				return false
			}
		}
		report.UsageItems = append(report.UsageItems, item)
		return true
	})
	if !valid {
		return nil, invalid
	}
	return report, nil
}
