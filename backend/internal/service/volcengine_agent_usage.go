package service

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func volcengineAgentUsageProfile(account *Account) bool {
	if account == nil || account.Platform != PlatformVolcengine || account.GetAccountMode() != AccountModeCoding {
		return false
	}
	profile := nativeAccountProfile(account)
	return profile != nil && profile.Chat == "https://ark.cn-beijing.volces.com/api/plan/v3"
}

func (s *CNProviderQuotaService) queryVolcengineAgentUsage(ctx context.Context, account *Account) *CNProviderQuotaProbeResult {
	result := &CNProviderQuotaProbeResult{Provider: account.Platform, Source: "agent_plan", FetchedAt: time.Now().UTC().Unix(), CredentialValid: true}
	if account.GetCredential("volcengine_plan_edition") != "team" {
		result.Status, result.Unsupported, result.Error = "no_verified_query", true, "Personal Agent Plan usage has no verified response contract"
		return result
	}
	seat := strings.TrimSpace(account.GetCredential("volcengine_seat_id"))
	if seat == "" || len(seat) > 1024 {
		result.Status, result.Error = "missing_configuration", "Agent enterprise quota requires an explicit seat identifier"
		return result
	}
	access, secret := strings.TrimSpace(account.GetCredential("volcengine_access_key_id")), strings.TrimSpace(account.GetCredential("volcengine_secret_access_key"))
	if !validateCloudQueryKeys(result, access, secret) {
		return result
	}
	endpoint, proxy, ok := s.cloudQueryEndpoint(ctx, account, "https://ark.cn-beijing.volcengineapi.com/", result)
	if !ok {
		return result
	}
	callCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(ctx), cnQuotaUpstreamTimeout)
	defer cancel()
	payload, _ := json.Marshal(struct{ SeatIDs []string }{[]string{seat}})
	body, ok := s.requestVolcengineUsage(callCtx, account, endpoint, proxy, access, secret, "GetSeatAFPUsage", payload, result)
	if !ok {
		return result
	}
	tiers, plan, ok := parseVolcengineAgentUsage(body, seat)
	if !ok {
		result.Status, result.Error = "invalid_response", "Agent quota response did not contain a unique matching seat with valid windows"
		return result
	}
	result.Tiers, result.PlanLevel, result.Success, result.Status = tiers, plan, true, "ok"
	updates := cnQuotaExtraUpdates(PlatformVolcengine, tiers, time.Now().UTC())
	for _, window := range []string{"5h", "daily", "weekly", "monthly"} {
		for _, suffix := range []string{"_used_percent", "_reset_at"} {
			key := cnExtraKey(PlatformVolcengine, window+suffix)
			if _, exists := updates[key]; !exists {
				updates[key] = nil
			}
		}
	}
	s.persistCloudQuery(ctx, account, result, updates)
	return result
}

// Source: volcengine/byted-supabase-cli internal/volcengine/agent_plan.go.
// ResetTime milliseconds are documented separately by ark-cli's usage skill.
// Missing amounts and nonpositive quota never become a zero-percent observation.
func parseVolcengineAgentUsage(body []byte, seat string) ([]CNQuotaTier, string, bool) {
	items := gjson.GetBytes(body, "Result.SeatAFPUsages")
	if !gjson.ValidBytes(body) || !items.IsArray() {
		return nil, "", false
	}
	var matched gjson.Result
	for _, item := range items.Array() {
		if item.Get("SeatID").Type == gjson.String && item.Get("SeatID").Str == seat {
			if matched.Exists() {
				return nil, "", false
			}
			matched = item
		}
	}
	if !matched.Exists() {
		return nil, "", false
	}
	var tiers []CNQuotaTier
	for _, field := range []struct{ source, window string }{{"AFPFiveHour", "5h"}, {"AFPDaily", "daily"}, {"AFPWeekly", "weekly"}, {"AFPMonthly", "monthly"}} {
		node := matched.Get(field.source)
		if !node.Exists() || node.Type == gjson.Null {
			continue
		}
		if !node.IsObject() {
			return nil, "", false
		}
		quota, used := node.Get("Quota"), node.Get("Used")
		if !quota.Exists() || !used.Exists() {
			continue
		}
		if quota.Type != gjson.Number || used.Type != gjson.Number || math.IsInf(quota.Num, 0) || math.IsInf(used.Num, 0) || used.Num < 0 {
			return nil, "", false
		}
		if quota.Num <= 0 {
			continue
		}
		percent := used.Num / quota.Num * 100
		if math.IsInf(percent, 0) || math.IsNaN(percent) {
			return nil, "", false
		}
		tier := CNQuotaTier{Window: field.window, UsedPercent: percent}
		reset := node.Get("ResetTime")
		if reset.Exists() && reset.Type != gjson.Null {
			if reset.Type != gjson.Number || math.Trunc(reset.Num) != reset.Num || reset.Num >= float64(math.MaxInt64) || reset.Num < float64(math.MinInt64) {
				return nil, "", false
			}
			if reset.Num > 0 {
				tier.ResetAt = time.UnixMilli(reset.Int()).UTC().Format(time.RFC3339)
				if _, err := time.Parse(time.RFC3339, tier.ResetAt); err != nil {
					return nil, "", false
				}
			}
		}
		tiers = append(tiers, tier)
	}
	return tiers, matched.Get("PlanType").String(), true
}
