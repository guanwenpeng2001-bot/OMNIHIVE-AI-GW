package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Key spending is distinct from account credits. Nil cap/remaining with
// Unlimited true means the provider explicitly reported a null limit.
type OpenRouterKeyUsage struct {
	Limit              *float64 `json:"limit"`
	Remaining          *float64 `json:"remaining"`
	Unlimited          bool     `json:"unlimited"`
	RemainingUnlimited bool     `json:"remaining_unlimited"`
	LimitReset         *string  `json:"limit_reset"`
	Usage              *float64 `json:"usage"`
	Daily              *float64 `json:"daily"`
	Weekly             *float64 `json:"weekly"`
	Monthly            *float64 `json:"monthly"`
}

func parseOpenRouterKeyUsage(body []byte) (*OpenRouterKeyUsage, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, fmt.Errorf("invalid OpenRouter key usage")
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(envelope.Data, &data) != nil {
		return nil, fmt.Errorf("invalid OpenRouter key usage")
	}
	result := &OpenRouterKeyUsage{}
	for name, dest := range map[string]**float64{"limit": &result.Limit, "limit_remaining": &result.Remaining, "usage": &result.Usage, "usage_daily": &result.Daily, "usage_weekly": &result.Weekly, "usage_monthly": &result.Monthly} {
		if value, ok := data[name]; ok && string(value) != "null" {
			var number float64
			if json.Unmarshal(value, &number) != nil || number < 0 {
				return nil, fmt.Errorf("invalid OpenRouter key usage field %s", name)
			}
			*dest = &number
		}
	}
	result.Unlimited = string(data["limit"]) == "null"
	result.RemainingUnlimited = string(data["limit_remaining"]) == "null"
	if value, ok := data["limit_reset"]; ok && string(value) != "null" {
		if json.Unmarshal(value, &result.LimitReset) != nil {
			return nil, fmt.Errorf("invalid OpenRouter key reset")
		}
	}
	if result.Usage == nil && result.Limit == nil && !result.Unlimited {
		return nil, fmt.Errorf("missing OpenRouter key usage")
	}
	return result, nil
}

func (s *AccountUsageService) getOpenRouterUsage(ctx context.Context, account *Account, force bool) (*UsageInfo, error) {
	if account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("OpenRouter usage requires an API key account")
	}
	if s.cache != nil && !force {
		if cached, ok := s.cache.load(&s.cache.apiCache, AccountRuntimeIdentityKey(account)); ok {
			if record, ok := cached.(*openRouterUsageCache); ok && time.Since(record.at) < 3*time.Minute {
				return record.usage, nil
			}
		}
	}
	gateway := s.openRouterGateway
	if gateway == nil || gateway.httpUpstream == nil {
		return nil, fmt.Errorf("OpenRouter usage transport unavailable")
	}
	target, err := gateway.validateUpstreamBaseURL(account.GetOpenAIBaseURL())
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(account.GetCredential("api_key"))
	if key == "" {
		return nil, fmt.Errorf("OpenRouter API key is missing")
	}
	callCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(ctx), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodGet, account.buildOpenAIEndpointURL(target, "/v1/key"), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(request.Header)
	proxy, err := account.EgressProxyURL(callCtx)
	if err != nil {
		return nil, err
	}
	probe := &AccountTestService{httpUpstream: gateway.httpUpstream, tlsFPProfileService: s.tlsFPProfileService}
	response, err := probe.doUpstreamModelsRequest(request, proxy, account)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			slog.Debug("provider response cleanup failed")
		}
	}()
	body, err := io.ReadAll(io.LimitReader(response.Body, 256*1024+1))
	if err != nil || len(body) > 256*1024 {
		return nil, fmt.Errorf("invalid OpenRouter usage response")
	}
	now := time.Now().UTC()
	result := &UsageInfo{Source: "active", UpdatedAt: &now}
	if response.StatusCode != http.StatusOK {
		result.Error = "OpenRouter key usage is unavailable"
		result.ErrorCode = "upstream_error"
		switch response.StatusCode {
		case 401:
			result.ErrorCode = "unauthenticated"
			result.NeedsReauth = true
		case 402:
			result.ErrorCode = "payment_required"
		case 403:
			result.ErrorCode = "forbidden"
		case 429:
			result.ErrorCode = "rate_limited"
		}
		return result, nil
	}
	result.OpenRouterKey, err = parseOpenRouterKeyUsage(body)
	if err != nil {
		return nil, err
	}
	if s.cache != nil {
		s.cache.store(&s.cache.apiCache, AccountRuntimeIdentityKey(account), &openRouterUsageCache{at: now, usage: result})
	}
	return result, nil
}

type openRouterUsageCache struct {
	at    time.Time
	usage *UsageInfo
}
