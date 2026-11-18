package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

const atlasUsageMaxPages = 11 // First page plus at most ten follow-ups.

type AtlasUsageWindow struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type AtlasUsageBucket struct {
	Object       string          `json:"object"`
	Date         string          `json:"date"`
	StartAt      string          `json:"start_at"`
	EndAt        string          `json:"end_at"`
	CoveredUntil string          `json:"covered_until"`
	Partial      bool            `json:"partial"`
	Results      []AtlasUsageRow `json:"results"`
}

type AtlasUsageRow struct {
	Object string            `json:"object"`
	Usage  AtlasUsageMetrics `json:"usage"`
}

type AtlasUsageMetrics struct {
	Requests float64            `json:"requests"`
	Tokens   map[string]float64 `json:"tokens"`
	Images   *AtlasUsageImages  `json:"images"`
	Video    *AtlasUsageVideo   `json:"video"`
}

type AtlasUsageImages struct {
	Count float64 `json:"count"`
}
type AtlasUsageVideo struct {
	Seconds float64 `json:"seconds"`
}

type AtlasConsumedUsage struct {
	AtlasUsageWindow
	Scope        string             `json:"scope"`
	Data         []AtlasUsageBucket `json:"data"`
	HasMore      bool               `json:"has_more"`
	NextPage     *string            `json:"next_page"`
	PagesFetched int                `json:"pages_fetched"`
	Partial      bool               `json:"partial"`
}

type AtlasUsageResult struct {
	Provider    string              `json:"provider"`
	Kind        string              `json:"kind"`
	Success     bool                `json:"success"`
	Unsupported bool                `json:"unsupported,omitempty"`
	Reason      string              `json:"reason,omitempty"`
	StatusCode  int                 `json:"status_code,omitempty"`
	FetchedAt   int64               `json:"fetched_at"`
	Persisted   bool                `json:"persisted"`
	Error       string              `json:"error,omitempty"`
	Usage       *AtlasConsumedUsage `json:"usage,omitempty"`
}

func ValidateAtlasUsageWindow(window AtlasUsageWindow) error {
	start, startErr := time.Parse("2006-01-02", window.StartDate)
	end, endErr := time.Parse("2006-01-02", window.EndDate)
	if startErr != nil || endErr != nil || len(window.StartDate) != 10 || len(window.EndDate) != 10 || !end.After(start) || end.Sub(start) > 180*24*time.Hour {
		return infraerrors.New(http.StatusBadRequest, "USAGE_INVALID_WINDOW", "usage requires UTC start_date before exclusive end_date within 180 days")
	}
	return nil
}

// QueryProviderUsage dispatches using the stored provider, never query parameters.
// Qiniu retains its existing window validator and service implementation.
func (service *CNProviderBalanceService) QueryProviderUsage(ctx context.Context, accountID int64, query url.Values) (any, error) {
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
	for key, values := range query {
		allowed := key == "granularity" || key == "start" || key == "end" || key == "timezone"
		if account.Platform == "atlascloud" {
			allowed = key == "start_date" || key == "end_date"
		}
		if !allowed || len(values) != 1 {
			return nil, infraerrors.New(http.StatusBadRequest, "USAGE_INVALID_QUERY", "invalid usage query parameters")
		}
	}
	if account.Platform == "atlascloud" {
		window := AtlasUsageWindow{StartDate: query.Get("start_date"), EndDate: query.Get("end_date")}
		if err := ValidateAtlasUsageWindow(window); err != nil {
			return nil, err
		}
		return service.queryAtlasUsage(ctx, account, window)
	}
	return service.QueryAccountUsage(ctx, accountID, CompatibleUsageWindow{Granularity: query.Get("granularity"), Start: query.Get("start"), End: query.Get("end"), Timezone: query.Get("timezone")})
}

func (service *CNProviderBalanceService) queryAtlasUsage(ctx context.Context, account *Account, window AtlasUsageWindow) (*AtlasUsageResult, error) {
	result := &AtlasUsageResult{Provider: "atlascloud", Kind: "consumed_usage", FetchedAt: time.Now().UTC().Unix()}
	if _, err := compatibleQueryTarget(account); err != nil {
		result.Kind, result.Unsupported, result.Reason, result.Error = "unsupported", true, compatibleQueryReason(err), "no verified Atlas usage query for this account configuration"
		return result, nil
	}
	key := strings.TrimSpace(account.GetCredential("api_key"))
	if !strings.HasPrefix(key, "apikey-") || len(key) <= len("apikey-") {
		result.Reason, result.Error = "invalid_credentials", "usage requires an ordinary Atlas Cloud API key"
		return result, nil
	}
	target, err := cnValidateProbeURL(service.cfg, "https://api.atlascloud.ai/public/v1/model-usage")
	if err != nil {
		return nil, infraerrors.New(http.StatusForbidden, "USAGE_URL_REJECTED", "usage target rejected by URL security policy")
	}
	callCtx, cancel := context.WithTimeout(ctx, cnBalanceUpstreamTimeout)
	defer cancel()
	usage := &AtlasConsumedUsage{AtlasUsageWindow: window, Scope: "self", Data: []AtlasUsageBucket{}}
	query := url.Values{"start_date": {window.StartDate}, "end_date": {window.EndDate}}
	seenDates, seenPages := map[string]bool{}, map[string]bool{}
	for pageIndex := 0; pageIndex < atlasUsageMaxPages; pageIndex++ {
		body, status, requestErr := service.requestCompatibleQuery(callCtx, account, target+"?"+query.Encode(), key)
		result.StatusCode = status
		if requestErr != nil {
			result.Reason, result.Error = compatibleQueryReason(requestErr), requestErr.Error()
			return result, nil
		}
		page, parseErr := parseAtlasUsagePage(body, window)
		if parseErr != nil {
			result.Reason, result.Error = "invalid_response", parseErr.Error()
			return result, nil
		}
		for _, bucket := range page.Data {
			if seenDates[bucket.Date] {
				result.Reason, result.Error = "invalid_response", "duplicate Atlas daily bucket"
				return result, nil
			}
			seenDates[bucket.Date] = true
			usage.Data = append(usage.Data, bucket)
			usage.Partial = usage.Partial || bucket.Partial
		}
		usage.PagesFetched++
		usage.HasMore, usage.NextPage = page.HasMore, page.NextPage
		if !page.HasMore {
			break
		}
		if seenPages[*page.NextPage] {
			result.Reason, result.Error = "invalid_response", "repeated Atlas page cursor"
			return result, nil
		}
		seenPages[*page.NextPage] = true
		query.Set("page", *page.NextPage)
	}
	usage.Partial = usage.Partial || usage.HasMore
	sort.Slice(usage.Data, func(left, right int) bool { return usage.Data[left].Date < usage.Data[right].Date })
	result.Success, result.Usage = true, usage
	return result, nil
}

var atlasTokenFields = []string{"input", "output", "total", "cache_creation", "cache_creation_1h", "cache_read", "input_image", "output_image"}

// Source: https://www.atlascloud.ai/docs/public-api/model-usage (2026-10-04).
// No grouping is requested: one daily total row, with independent counters.
func parseAtlasUsagePage(body []byte, window AtlasUsageWindow) (*AtlasConsumedUsage, error) {
	invalid := errors.New("invalid Atlas model usage response")
	if len(body) > cnBalanceMaxBodyBytes || !gjson.ValidBytes(body) {
		return nil, invalid
	}
	root := gjson.ParseBytes(body)
	if root.Get("object").String() != "list" || root.Get("scope").String() != "self" || root.Get("error").Exists() || !root.Get("data").IsArray() {
		return nil, invalid
	}
	more, cursor := root.Get("has_more"), root.Get("next_page")
	if (more.Type != gjson.True && more.Type != gjson.False) || (cursor.Raw != "null" && !compatibleRequiredText(cursor)) || (more.Bool() && !compatibleRequiredText(cursor)) {
		return nil, invalid
	}
	page := &AtlasConsumedUsage{Scope: "self", Data: []AtlasUsageBucket{}, HasMore: more.Bool()}
	if cursor.Type == gjson.String {
		value := cursor.Str
		page.NextPage = &value
	}
	for _, item := range root.Get("data").Array() {
		date := item.Get("date").String()
		day, dateErr := time.Parse("2006-01-02", date)
		start, startErr := time.Parse(time.RFC3339, item.Get("start_at").String())
		end, endErr := time.Parse(time.RFC3339, item.Get("end_at").String())
		covered, coveredErr := time.Parse(time.RFC3339, item.Get("covered_until").String())
		partial := item.Get("partial")
		rows := item.Get("results")
		if item.Get("object").String() != "usage.bucket" || dateErr != nil || date < window.StartDate || date >= window.EndDate || startErr != nil || endErr != nil || coveredErr != nil || !start.Equal(day) || !end.Equal(day.Add(24*time.Hour)) || covered.Before(start) || covered.After(end) || (partial.Type != gjson.True && partial.Type != gjson.False) || (!partial.Bool() && !covered.Equal(end)) || !rows.IsArray() || len(rows.Array()) > 1 {
			return nil, invalid
		}
		if len(rows.Array()) == 0 {
			var bucket AtlasUsageBucket
			if err := json.Unmarshal([]byte(item.Raw), &bucket); err != nil {
				return nil, invalid
			}
			page.Data = append(page.Data, bucket)
			continue
		}
		row := rows.Array()[0]
		if row.Get("object").String() != "usage.result" {
			return nil, invalid
		}
		metrics := row.Get("usage")
		if !atlasUsageCounter(metrics.Get("requests")) {
			return nil, invalid
		}
		if metrics.Get("tokens").Raw != "null" {
			for _, field := range atlasTokenFields {
				value := metrics.Get("tokens." + field)
				if !atlasUsageCounter(value) {
					return nil, invalid
				}
			}
		}
		for field, counter := range map[string]string{"images": "count", "video": "seconds"} {
			value := metrics.Get(field)
			if value.Raw == "null" {
				continue
			}
			if !value.IsObject() {
				return nil, invalid
			}
			if !atlasUsageCounter(value.Get(counter)) {
				return nil, invalid
			}
		}
		var bucket AtlasUsageBucket
		if err := json.Unmarshal([]byte(item.Raw), &bucket); err != nil {
			return nil, invalid
		}
		// Whitelist token fields; never echo unrelated supplier response fields.
		if metrics.Get("tokens").Raw != "null" {
			bucket.Results[0].Usage.Tokens = map[string]float64{}
			for _, field := range atlasTokenFields {
				bucket.Results[0].Usage.Tokens[field] = metrics.Get("tokens." + field).Num
			}
		}
		page.Data = append(page.Data, bucket)
	}
	return page, nil
}

func atlasUsageCounter(value gjson.Result) bool {
	return compatibleFiniteNumber(value) && value.Num >= 0 && value.Num <= 9007199254740991 && math.Trunc(value.Num) == value.Num
}
