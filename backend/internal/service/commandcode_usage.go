package service

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// Official upstream alpha endpoints, not a stable public API; may change at any
// time. Probe lazily through the existing quota UI, never through quota cron.
// Preserve the configured origin (including custom proxies), never substitute a host.
func commandCodeAlphaURL(base, path string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("invalid Command Code base URL")
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/provider/v1") + "/alpha/" + path
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	return u.String(), nil
}

func commandCodeNumber(node gjson.Result) (float64, bool) {
	n, ok := cnParseF64(node.Value())
	return n, ok && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0
}

func parseCommandCodeUsageTiers(body []byte) []CNQuotaTier {
	if !gjson.ValidBytes(body) || gjson.GetBytes(body, "windowLimits.limited").Type != gjson.True {
		return nil
	}
	var tiers []CNQuotaTier
	for _, item := range []struct{ key, window string }{{"fiveHour", "5h"}, {"weekly", "weekly"}} {
		node := gjson.GetBytes(body, "windowLimits."+item.key)
		used, usedOK := commandCodeNumber(node.Get("used"))
		cap, capOK := commandCodeNumber(node.Get("cap"))
		if !usedOK || !capOK || cap <= 0 {
			continue
		}
		percent := used / cap * 100
		if math.IsInf(percent, 0) || math.IsNaN(percent) {
			continue
		}
		reset := ""
		// This contract uses Unix milliseconds, not seconds. Missing reset is unknown.
		if ms, ok := commandCodeNumber(node.Get("resetAt")); ok && ms >= 1e12 && ms < 253402300800000 {
			reset = time.UnixMilli(int64(ms)).UTC().Format(time.RFC3339)
		}
		tiers = append(tiers, CNQuotaTier{Window: item.window, UsedPercent: percent, ResetAt: reset})
	}
	return tiers
}

func (s *CNProviderQuotaService) queryCommandCodeUsage(ctx context.Context, account *Account) (*CNProviderQuotaProbeResult, error) {
	now := time.Now().UTC()
	result := &CNProviderQuotaProbeResult{Provider: PlatformCommandCode, Source: "upstream_alpha", FetchedAt: now.Unix(), Unsupported: true}
	// These errors describe observability only, never account/key health.
	unsupported := func() (*CNProviderQuotaProbeResult, error) {
		result.Error = "Command Code alpha usage unsupported or temporarily unavailable"
		_ = s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{"commandcode_usage_supported": false, "commandcode_usage_checked_at": now.Format(time.RFC3339)})
		return result, nil
	}
	key := strings.TrimSpace(account.GetCredential("api_key"))
	if key == "" {
		return unsupported()
	}
	callCtx, cancel := context.WithTimeout(ctx, cnQuotaUpstreamTimeout)
	defer cancel()
	proxy := s.resolveProxyURL(ctx, account)
	fetch := func(path string) ([]byte, error) {
		target, err := commandCodeAlphaURL(account.GetOpenAIBaseURL(), path)
		if err != nil {
			return nil, err
		}
		target, err = cnValidateProbeURL(s.cfg, target)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(callCtx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", key)
		req.Header.Set("Accept", "application/json")
		account.ApplyHeaderOverrides(req.Header)
		resp, err := s.httpUpstream.Do(req, proxy, account.ID, maxInt(account.Concurrency, 1))
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		result.StatusCode = resp.StatusCode
		body, err := io.ReadAll(io.LimitReader(resp.Body, cnQuotaMaxBodyBytes+1))
		if err != nil || len(body) > cnQuotaMaxBodyBytes || resp.StatusCode != http.StatusOK || !gjson.ValidBytes(body) {
			return nil, fmt.Errorf("alpha response unavailable")
		}
		return body, nil
	}
	credits, err := fetch("billing/credits")
	if err != nil {
		return unsupported()
	}
	result.Tiers = parseCommandCodeUsageTiers(credits)
	if len(result.Tiers) == 0 {
		return unsupported()
	}
	result.Credits = map[string]float64{}
	updates := cnQuotaExtraUpdates(PlatformCommandCode, result.Tiers, now)
	updates["commandcode_usage_supported"] = true
	updates["commandcode_usage_checked_at"] = now.Format(time.RFC3339)
	// Clear absent windows/resets on a valid new sample, avoiding stale numeric data.
	for _, window := range []string{"5h", "weekly"} {
		for _, suffix := range []string{"used_percent", "reset_at"} {
			k := "commandcode_" + window + "_" + suffix
			if _, ok := updates[k]; !ok {
				updates[k] = nil
			}
		}
	}
	for _, bucket := range []string{"monthlyCredits", "purchasedCredits", "freeCredits"} {
		updates["commandcode_"+bucket] = nil
		if n, ok := commandCodeNumber(gjson.GetBytes(credits, "credits."+bucket)); ok {
			result.Credits[bucket] = n
			updates["commandcode_"+bucket] = n
		}
	}
	// Optional independent metadata failures do not invalidate real window data.
	updates["commandcode_plan_level"] = nil
	if subscription, err := fetch("billing/subscriptions"); err == nil && gjson.GetBytes(subscription, "success").Type == gjson.True {
		plan := gjson.GetBytes(subscription, "data.planId")
		if plan.Type == gjson.String {
			result.PlanLevel = plan.String()
			updates["commandcode_plan_level"] = result.PlanLevel
		}
	}
	for _, field := range []string{"period_basis", "totalCount", "totalTokens", "totalCredits"} {
		updates["commandcode_"+field] = nil
	}
	if summary, err := fetch("usage/summary"); err == nil {
		basis := gjson.GetBytes(summary, "periodBasis")
		if basis.Type == gjson.String {
			updates["commandcode_period_basis"] = basis.String()
		}
		for _, field := range []string{"totalCount", "totalTokens", "totalCredits"} {
			if n, ok := commandCodeNumber(gjson.GetBytes(summary, field)); ok {
				updates["commandcode_"+field] = n
			}
		}
	}
	result.Success, result.CredentialValid, result.Unsupported = true, true, false
	result.StatusCode = http.StatusOK
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err == nil {
		result.Persisted = true
	}
	return result, nil
}
