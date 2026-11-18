package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // Consistent IANA validation in minimal containers and Windows.

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/tidwall/gjson"
	"golang.org/x/time/rate"
)

type CompatibleUsageWindow struct {
	Granularity string `json:"granularity"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Timezone    string `json:"timezone"`
}

var compatibleQueryTimestamp = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

// ValidateCompatibleUsageWindow preserves the supplied instants. The supplier
// documents a <=31-day span but does not specify inclusive/exclusive boundaries;
// do not round days or manufacture a final second (especially across DST).
func ValidateCompatibleUsageWindow(window CompatibleUsageWindow) (CompatibleUsageWindow, error) {
	invalid := infraerrors.New(http.StatusBadRequest, "USAGE_INVALID_WINDOW", "usage requires day granularity, RFC3339 start before end within 31 days, and a valid IANA timezone")
	if window.Granularity != "day" || len(window.Start) > 64 || len(window.End) > 64 || len(window.Timezone) > 128 || !compatibleQueryTimestamp.MatchString(window.Start) || !compatibleQueryTimestamp.MatchString(window.End) {
		return window, invalid
	}
	start, startErr := time.Parse(time.RFC3339, window.Start)
	end, endErr := time.Parse(time.RFC3339, window.End)
	if startErr != nil || endErr != nil || !end.After(start) || end.Sub(start) > 31*24*time.Hour {
		return window, invalid
	}
	if window.Timezone == "" {
		window.Timezone = "Asia/Shanghai"
	}
	if window.Timezone == "Local" {
		return window, invalid
	}
	if _, err := time.LoadLocation(window.Timezone); err != nil {
		return window, invalid
	}
	return window, nil
}

type CompatibleUsageItem struct {
	Name  string  `json:"name"`
	Unit  string  `json:"unit"`
	Total float64 `json:"total"`
}

type CompatibleUsageModel struct {
	ID    string                `json:"id"`
	Name  string                `json:"name"`
	Items []CompatibleUsageItem `json:"items"`
}

type CompatibleConsumedUsage struct {
	CompatibleUsageWindow
	Models []CompatibleUsageModel `json:"models"`
}

type CompatibleUsageResult struct {
	Provider    string                   `json:"provider"`
	Kind        string                   `json:"kind"`
	Success     bool                     `json:"success"`
	Unsupported bool                     `json:"unsupported,omitempty"`
	Reason      string                   `json:"reason,omitempty"`
	StatusCode  int                      `json:"status_code,omitempty"`
	FetchedAt   int64                    `json:"fetched_at"`
	Persisted   bool                     `json:"persisted"`
	Error       string                   `json:"error,omitempty"`
	Usage       *CompatibleConsumedUsage `json:"usage,omitempty"`
}

// Qiniu permits five requests/sec/IP. A process-wide 4/sec, burst-one gate is
// deliberately shared across accounts and proxies, since different proxy URLs
// can exit at the same IP. Reject instead of building an unbounded waiter queue.
// Deployments sharing an egress IP across processes must also enforce that limit
// at the egress; 429 is returned explicitly, with no automatic retry.
var compatibleQiniuQueries = rate.NewLimiter(rate.Every(250*time.Millisecond), 1)

func (service *CNProviderBalanceService) QueryAccountUsage(ctx context.Context, accountID int64, window CompatibleUsageWindow) (*CompatibleUsageResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	window, err := ValidateCompatibleUsageWindow(window)
	if err != nil {
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
	result := &CompatibleUsageResult{Provider: account.Platform, Kind: "unsupported", FetchedAt: time.Now().UTC().Unix()}
	target, kind, err := compatibleUsageTarget(account)
	if err != nil || kind != "consumed_usage" {
		result.Unsupported, result.Reason = true, "no_verified_query"
		result.Error = "no verified consumed usage query for this provider and mode"
		if err != nil {
			result.Reason = compatibleQueryReason(err)
			result.Error = err.Error()
		}
		return result, nil
	}
	result.Kind = kind
	key := strings.TrimSpace(account.GetCredential("api_key"))
	if (!strings.HasPrefix(key, "sk-") && !strings.HasPrefix(key, "tk-")) || len(key) <= 3 {
		result.Reason, result.Error = "invalid_credentials", "usage requires an ordinary Qiniu API key"
		return result, nil
	}
	target, err = cnValidateProbeURL(service.cfg, target)
	if err != nil {
		return nil, infraerrors.New(http.StatusForbidden, "USAGE_URL_REJECTED", "usage target rejected by URL security policy")
	}
	query := url.Values{"granularity": {window.Granularity}, "start": {window.Start}, "end": {window.End}, "timezone": {window.Timezone}}
	target += "?" + query.Encode()
	// Identity includes credentials, revision and proxy binding; never share a
	// reading across rotations, modes, accounts or query windows. No stale cache.
	flight := service.flight.DoChan("compatible_usage:"+AccountRuntimeIdentityKey(account)+":"+query.Encode(), func() (any, error) {
		if !compatibleQiniuQueries.Allow() {
			result.Reason, result.Error = "rate_limited", "usage query rate limit reached; retry later"
			return result, nil
		}
		callCtx, cancel := context.WithTimeout(context.Background(), cnBalanceUpstreamTimeout)
		defer cancel()
		body, status, requestErr := service.requestCompatibleQuery(callCtx, account, target, key)
		result.StatusCode = status
		if requestErr != nil {
			result.Reason, result.Error = compatibleQueryReason(requestErr), requestErr.Error()
			return result, nil
		}
		models, parseErr := parseCompatibleConsumedUsage(body)
		if parseErr != nil {
			result.Reason, result.Error = "invalid_response", parseErr.Error()
			return result, nil
		}
		result.Usage = &CompatibleConsumedUsage{CompatibleUsageWindow: window, Models: models}
		result.Success = true
		return result, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case response := <-flight:
		if response.Err != nil {
			return nil, response.Err
		}
		result, valid := response.Val.(*CompatibleUsageResult)
		if !valid || result == nil {
			return nil, infraerrors.New(http.StatusInternalServerError, "USAGE_INVALID_RESULT", "invalid usage query result")
		}
		return result, nil
	}
}

func parseCompatibleConsumedUsage(body []byte) ([]CompatibleUsageModel, error) {
	invalid := errors.New("invalid consumed usage response")
	if len(body) > cnBalanceMaxBodyBytes || !gjson.ValidBytes(body) {
		return nil, invalid
	}
	root := gjson.ParseBytes(body)
	data := root.Get("data")
	if root.Get("status").Type != gjson.True || root.Get("error").Exists() || !data.IsArray() {
		return nil, invalid
	}
	models := make([]CompatibleUsageModel, 0)
	valid, itemCount := true, 0
	// Iterate in-place rather than allocating an array proportional to hostile
	// input. Category time series are not part of this summary contract.
	data.ForEach(func(_, model gjson.Result) bool {
		if len(models) >= 256 || !model.IsObject() || !compatibleRequiredText(model.Get("id")) || !compatibleRequiredText(model.Get("name")) || !model.Get("items").IsArray() {
			valid = false
			return false
		}
		entry := CompatibleUsageModel{ID: model.Get("id").Str, Name: model.Get("name").Str, Items: make([]CompatibleUsageItem, 0)}
		model.Get("items").ForEach(func(_, item gjson.Result) bool {
			itemCount++
			if itemCount > 2048 || !item.IsObject() || !compatibleRequiredText(item.Get("name")) || !compatibleRequiredText(item.Get("unit")) || !compatibleFiniteNumber(item.Get("total")) || item.Get("total").Num < 0 {
				valid = false
				return false
			}
			entry.Items = append(entry.Items, CompatibleUsageItem{Name: item.Get("name").Str, Unit: item.Get("unit").Str, Total: item.Get("total").Num})
			return true
		})
		if !valid {
			return false
		}
		models = append(models, entry)
		return true
	})
	if !valid {
		return nil, invalid
	}
	return models, nil
}
