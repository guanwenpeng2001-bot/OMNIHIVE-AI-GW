package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type compatibleQueryError struct{ reason, message string }

func (failure compatibleQueryError) Error() string { return failure.message }

func compatibleQueryReason(err error) string {
	var failure compatibleQueryError
	if errors.As(err, &failure) {
		return failure.reason
	}
	if strings.Contains(err.Error(), "origin") || strings.Contains(err.Error(), "custom") {
		return "custom_origin"
	}
	return "invalid_credentials"
}

// Only the fixed target selected by compatibleQueryTarget may reach this helper.
// Neither header overrides nor upstream error bodies are forwarded.
func (service *CNProviderBalanceService) requestCompatibleQuery(ctx context.Context, account *Account, target, key string) ([]byte, int, error) {
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
	request.Header.Set("Authorization", "Bearer "+key)
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

// Project budgets are ephemeral readings. These fields must never be written to
// the wallet snapshot or used to derive a remaining account cash amount.
type CompatibleProjectBudget struct {
	Scope               string   `json:"scope"`
	ProjectID           int64    `json:"project_id"`
	RAMUserID           int64    `json:"ram_user_id"`
	BudgetKind          string   `json:"budget_kind"`
	PeriodKind          string   `json:"period_kind"`
	PeriodStart         string   `json:"period_start"`
	PeriodEnd           string   `json:"period_end"`
	Consumed            float64  `json:"consumed"`
	Limit               *float64 `json:"limit"`
	RemainingPercentage *float64 `json:"remaining_percentage"`
	Unlimited           bool     `json:"unlimited"`
	Currency            string   `json:"currency"`
}

func compatibleFiniteNumber(value gjson.Result) bool {
	return value.Type == gjson.Number && !math.IsNaN(value.Num) && !math.IsInf(value.Num, 0)
}

func compatibleRequiredText(value gjson.Result) bool {
	return value.Type == gjson.String && strings.TrimSpace(value.Str) != "" && len(value.Str) <= 512
}

func parseCompatibleProjectBudget(body []byte) (*CompatibleProjectBudget, error) {
	invalid := errors.New("invalid project budget response")
	if !gjson.ValidBytes(body) {
		return nil, invalid
	}
	root := gjson.ParseBytes(body)
	data := root.Get("data")
	if root.Get("success").Type != gjson.True || root.Get("error").Exists() || data.Get("gateway_type").String() != "enterprise" || data.Get("scope").String() != "account_in_project" {
		return nil, invalid
	}
	budget := &CompatibleProjectBudget{Scope: "account_in_project", Currency: "CNY"}
	for field, destination := range map[string]*int64{"project_id": &budget.ProjectID, "ram_user_id": &budget.RAMUserID} {
		value := data.Get(field)
		identity, err := strconv.ParseInt(value.Raw, 10, 64)
		if value.Type != gjson.Number || err != nil || identity < 0 {
			return nil, invalid
		}
		*destination = identity
	}
	budget.BudgetKind, budget.PeriodKind = data.Get("budget_kind").String(), data.Get("period_kind").String()
	if budget.PeriodKind != "daily" && budget.PeriodKind != "monthly" {
		return nil, invalid
	}
	if budget.BudgetKind != "unlimited" && budget.BudgetKind != budget.PeriodKind {
		return nil, invalid
	}
	budget.PeriodStart, budget.PeriodEnd = data.Get("period_start").String(), data.Get("period_end").String()
	if !compatibleQueryTimestamp.MatchString(budget.PeriodStart) || !compatibleQueryTimestamp.MatchString(budget.PeriodEnd) {
		return nil, invalid
	}
	start, startErr := time.Parse(time.RFC3339, budget.PeriodStart)
	end, endErr := time.Parse(time.RFC3339, budget.PeriodEnd)
	if startErr != nil || endErr != nil || !end.After(start) {
		return nil, invalid
	}
	consumed := data.Get("period_consumed_cny")
	unlimited := data.Get("unlimited")
	if !compatibleFiniteNumber(consumed) || consumed.Num < 0 || (unlimited.Type != gjson.True && unlimited.Type != gjson.False) {
		return nil, invalid
	}
	budget.Consumed, budget.Unlimited = consumed.Num, unlimited.Bool()
	if budget.Unlimited != (budget.BudgetKind == "unlimited") {
		return nil, invalid
	}
	for field, destination := range map[string]**float64{"period_limit_cny": &budget.Limit, "remaining_percentage": &budget.RemainingPercentage} {
		value := data.Get(field)
		if budget.Unlimited {
			if value.Raw != "null" {
				return nil, invalid
			}
		} else {
			if !compatibleFiniteNumber(value) || (field == "period_limit_cny" && value.Num < 0) {
				return nil, invalid
			}
			amount := value.Num
			*destination = &amount
		}
	}
	return budget, nil
}
