package service

import (
	"math"
	"strings"

	"github.com/tidwall/gjson"
)

const miniMaxTokenPlanURL = "https://api.minimax.cn/v1/token_plan/remains"

func isMiniMaxCNTokenPlan(account *Account) bool {
	profile := nativeAccountProfile(account)
	return profile != nil && profile.Platform == PlatformMiniMax &&
		profile.Mode == AccountModeCoding && profile.Chat == "https://api.minimax.cn/v1"
}

// MiniMax-AI/cli 06e47c70 src/utils/quota.ts and src/output/quota-table.ts:
// remaining_percent is authoritative; without it usage_count means REMAINING,
// despite its name. The new endpoint includes MiniMax-M* model buckets, not
// just the legacy "general" bucket. Multimedia buckets are not coding quota.
func parseMiniMaxTokenPlanTiers(body []byte) []CNQuotaTier {
	if !gjson.ValidBytes(body) {
		return nil
	}
	var tiers []CNQuotaTier
	for _, window := range []struct{ name, prefix, reset string }{
		{"5h", "current_interval_", "end_time"},
		{"weekly", "current_weekly_", "weekly_end_time"},
	} {
		var selected *CNQuotaTier
		for _, model := range gjson.GetBytes(body, "model_remains").Array() {
			name := strings.ToLower(strings.TrimSpace(model.Get("model_name").String()))
			if name != "general" && !strings.HasPrefix(name, "minimax-m") {
				continue
			}
			// Status 3 denotes unlimited or an unavailable bucket, not zero usage.
			if model.Get(window.prefix+"status").Int() == 3 {
				continue
			}
			remaining, valid := miniMaxTokenPlanNumber(model.Get(window.prefix + "remaining_percent"))
			if !valid {
				// Invalid explicit percentages must not fall back to ambiguous counts.
				// Missing/null percentages use the official legacy count fallback.
				if model.Get(window.prefix+"remaining_percent").Type != gjson.Null {
					continue
				}
				total, totalOK := miniMaxTokenPlanNumber(model.Get(window.prefix + "total_count"))
				count, countOK := miniMaxTokenPlanNumber(model.Get(window.prefix + "usage_count"))
				if !totalOK || !countOK || total <= 0 || count < 0 || count > total {
					continue
				}
				remaining = count / total * 100
			}
			if remaining < 0 || remaining > 100 {
				continue
			}
			tier := CNQuotaTier{Window: window.name, UsedPercent: 100 - remaining,
				ResetAt: cnNormalizeResetTime(model.Get(window.reset).Value())}
			// Standard tiers have one slot per window: report the most consumed
			// coding bucket, keeping its reset attached to the same observation.
			if selected == nil || tier.UsedPercent > selected.UsedPercent {
				selected = &tier
			}
		}
		if selected != nil {
			tiers = append(tiers, *selected)
		}
	}
	return tiers
}

func miniMaxTokenPlanNumber(value gjson.Result) (float64, bool) {
	number, valid := cnParseF64(value.Value())
	return number, valid && !math.IsNaN(number) && !math.IsInf(number, 0)
}
