package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const WorkBuddyUsageSnapshotExtraKey = "workbuddy_usage_snapshot"

type WorkBuddyUsageSnapshot struct {
	Site           string  `json:"site"`
	Plan           string  `json:"plan"`
	BaseUsed       float64 `json:"base_used"`
	BaseTotal      float64 `json:"base_total"`
	BaseRemaining  float64 `json:"base_remaining"`
	BaseResetAt    string  `json:"base_reset_at,omitempty"`
	ExtraUsed      float64 `json:"extra_used"`
	ExtraTotal     float64 `json:"extra_total"`
	ExtraRemaining float64 `json:"extra_remaining"`
	TotalRemaining float64 `json:"total_remaining"`
	UpdatedAt      string  `json:"updated_at"`
}

func workBuddyCapacity(v any) float64 {
	var n float64
	switch v := v.(type) {
	case float64:
		n = v
	case string:
		n, _ = strconv.ParseFloat(v, 64)
	case json.Number:
		n, _ = v.Float64()
	}
	if math.IsInf(n, 0) || math.IsNaN(n) || n < 0 {
		return 0
	}
	return n
}
func workBuddyDate(v any) *time.Time {
	if s, ok := v.(string); ok {
		if t, err := time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(s)+" +0800"); err == nil {
			return &t
		}
	}
	return workBuddyTime(v)
}
func workBuddyMaps(v any) []map[string]any {
	out := []map[string]any{}
	if entries, ok := v.([]any); ok {
		for _, e := range entries {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}
func workBuddyString(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func workBuddyPlanCode(code, subscription, site string) bool {
	return code != "" && (code == subscription || slices.Contains(workBuddyPlanCodes, code) || (site == "cn" && code == "TCACA_code_008_cfWoLwvjU4"))
}
func parseWorkBuddyUsage(sum map[string]any, details []map[string]any, site string) *WorkBuddyUsageSnapshot {
	snap := &WorkBuddyUsageSnapshot{Site: site, Plan: firstNonEmpty(workBuddyString(sum, "SubscriptionPackageName"), workBuddyString(sum, "PlanName")), UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	if snap.Plan == "" {
		if paid, _ := sum["IsPaidUser"].(bool); paid {
			snap.Plan = "Pro"
		} else {
			snap.Plan = "Free plan"
		}
	}
	sub := workBuddyString(sum, "SubscriptionPackageCode")
	groups := workBuddyMaps(sum["CreditGroups"])
	packages := workBuddyMaps(sum["Packages"])
	add := func(base bool, total, used, left float64) {
		if base {
			snap.BaseTotal += total
			snap.BaseUsed += used
			snap.BaseRemaining += left
		} else {
			snap.ExtraTotal += total
			snap.ExtraUsed += used
			snap.ExtraRemaining += left
		}
	}
	countedGroups := false
	for _, g := range groups {
		if g["CycleTotalCapacity"] == nil || g["CycleTotalCapacity"] == "" {
			continue
		}
		total := workBuddyCapacity(g["CycleTotalCapacity"])
		used := workBuddyCapacity(g["CycleUsedCapacity"])
		add(g["Kind"] == "plan_base", total, used, math.Max(0, total-used))
		countedGroups = true
	}
	seen := map[string]bool{}
	for _, p := range packages {
		code := workBuddyString(p, "PackageCode")
		seen[code] = true
		base := workBuddyPlanCode(code, sub, site)
		if p["CycleTotalCapacity"] == nil || p["CycleTotalCapacity"] == "" {
			if p["CycleRemainCapacity"] != nil {
				add(base, 0, 0, workBuddyCapacity(p["CycleRemainCapacity"]))
			}
		} else if !countedGroups {
			total := workBuddyCapacity(p["CycleTotalCapacity"])
			used := workBuddyCapacity(p["CycleUsedCapacity"])
			add(base, total, used, math.Max(0, total-used))
		}
	}
	hasSummaryExtra := snap.ExtraTotal > 0
	for _, p := range details {
		code := workBuddyString(p, "PackageCode")
		base := workBuddyPlanCode(code, sub, site)
		var end *time.Time
		for _, k := range []string{"DeductionEndTime", "ExpiredTime", "CycleEndTime"} {
			if end = workBuddyDate(p[k]); end != nil {
				break
			}
		}
		if end != nil && time.Now().After(*end) {
			continue
		}
		if (sub != "" && code == sub) || (sub == "" && workBuddyCapacity(p["CapacityType"]) == 4) {
			if reset := workBuddyDate(p["CycleEndTime"]); reset != nil && reset.After(time.Now()) {
				if snap.BaseResetAt == "" || reset.UTC().Format(time.RFC3339) < snap.BaseResetAt {
					snap.BaseResetAt = reset.UTC().Format(time.RFC3339)
				}
			}
		}
		// Summary groups already aggregate these packages. Detail lists only add
		// omitted extra kinds; do not double-count a group or an existing code.
		if seen[code] || base || snap.BaseTotal <= 0 || hasSummaryExtra {
			continue
		}
		extra := slices.Contains(workBuddyExtraCodes, code) || (site == "global" && (code == "TCACA_code_008_cfWoLwvjU4" || code == "TCACA_code_006_DbXS0lrypC"))
		if !extra {
			continue
		}
		total := workBuddyCapacity(p["CycleCapacitySizePrecise"])
		left := workBuddyCapacity(p["CycleCapacityRemainPrecise"])
		if workBuddyCapacity(p["CapacityType"]) == 4 {
			if slices := workBuddyMaps(p["SlicePeriodUsageDetails"]); len(slices) > 0 {
				total = workBuddyCapacity(slices[0]["SlicePeriodCapacitySizePrecise"])
				left = workBuddyCapacity(slices[0]["SlicePeriodCapacityRemainPrecise"])
			}
		}
		left = math.Min(left, total)
		add(false, total, total-left, left)
	}
	snap.TotalRemaining = snap.BaseRemaining + snap.ExtraRemaining
	return snap
}
func (s *WorkBuddyOAuthService) FetchWorkBuddyUsage(ctx context.Context, a *Account) (*WorkBuddyUsageSnapshot, error) {
	if a == nil || a.Platform != PlatformWorkBuddy || workBuddyToken(a) == "" {
		return nil, errors.New("WorkBuddy credential is required")
	}
	proxy, err := a.EgressProxyURL(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := s.call(ctx, a, proxy, http.MethodPost, "/billing/meter/get-user-resource-summary", map[string]any{}, nil)
	if err != nil {
		return nil, err
	}
	var sum map[string]any
	if err = json.Unmarshal(raw, &sum); err != nil {
		return nil, err
	}
	snap := parseWorkBuddyUsage(sum, nil, workBuddySiteFor(a).ID)
	if a.GetCredential("enterprise_id") != "" && snap.BaseTotal == 0 && snap.ExtraTotal == 0 && snap.TotalRemaining == 0 {
		raw, e := s.call(ctx, a, proxy, http.MethodPost, "/billing/meter/get-enterprise-user-usage", map[string]any{}, nil)
		if e != nil {
			return nil, e
		}
		var ent map[string]any
		if e = json.Unmarshal(raw, &ent); e != nil {
			return nil, e
		}
		snap.BaseTotal = workBuddyCapacity(ent["limitNum"])
		snap.BaseUsed = workBuddyCapacity(ent["credit"])
		snap.BaseRemaining = math.Max(0, snap.BaseTotal-snap.BaseUsed)
		snap.TotalRemaining = snap.BaseRemaining + snap.ExtraRemaining
		if reset := workBuddyDate(ent["cycleResetTime"]); reset != nil {
			snap.BaseResetAt = reset.UTC().Format(time.RFC3339)
		}
		if snap.BaseTotal <= 0 {
			return nil, errors.New("WorkBuddy enterprise credit allowance is unavailable")
		}
		snap.Plan = "Enterprise"
		return snap, nil
	}
	details := []map[string]any{}
	day := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	for _, kind := range []string{"paid", "free"} {
		for page := 1; page <= 10; page++ {
			body := map[string]any{"PageNumber": page, "PageSize": 200, "Status": []int{0, 3}}
			if kind == "paid" {
				body["PackageCodes"] = workBuddyPaidCodes
				body["NeedRenewInfo"] = true
			} else {
				body["PackageCodes"] = workBuddyFreeCodes
				body["SlicePeriodStartTime"] = day + " 00:00:00"
				body["SlicePeriodEndTime"] = day + " 23:59:59"
			}
			raw, e := s.call(ctx, a, proxy, http.MethodPost, "/billing/meter/get-user-resource-"+kind+"-packages", body, nil)
			if e != nil {
				break // Detail meters are optional; retain the authoritative summary.
			}
			var list map[string]any
			if e = json.Unmarshal(raw, &list); e != nil {
				break
			}
			entries := workBuddyMaps(list["Accounts"])
			details = append(details, entries...)
			if len(entries) < 200 || page*200 >= int(workBuddyCapacity(list["TotalCount"])) {
				break
			}
		}
	}
	snap = parseWorkBuddyUsage(sum, details, workBuddySiteFor(a).ID)

	if len(workBuddyMaps(sum["Packages"])) == 0 && len(workBuddyMaps(sum["CreditGroups"])) == 0 && snap.BaseTotal == 0 && snap.ExtraTotal == 0 {
		return nil, errors.New("WorkBuddy credit allowance is unavailable")
	}
	return snap, nil
}
func (s *AccountUsageService) getWorkBuddyUsage(ctx context.Context, a *Account, force bool) (*UsageInfo, error) {
	if !force && a.Extra != nil {
		if raw, ok := a.Extra[WorkBuddyUsageSnapshotExtraKey]; ok {
			body, _ := json.Marshal(raw)
			var snapshot WorkBuddyUsageSnapshot
			if json.Unmarshal(body, &snapshot) == nil {
				if t := workBuddyTime(snapshot.UpdatedAt); t != nil && time.Since(*t) < time.Minute {
					return &UsageInfo{WorkBuddy: &snapshot, UpdatedAt: t, Source: "active"}, nil
				}
			}
		}
	}
	if s.workBuddyOAuth == nil {
		return nil, errors.New("WorkBuddy usage service is not configured")
	}
	snapshot, err := s.workBuddyOAuth.FetchWorkBuddyUsage(ctx, a)
	if err != nil {
		return nil, err
	}
	if s.accountRepo != nil {
		if err = s.accountRepo.UpdateExtra(ctx, a.ID, map[string]any{WorkBuddyUsageSnapshotExtraKey: snapshot}); err != nil {
			return nil, err
		}
	}
	return &UsageInfo{WorkBuddy: snapshot, UpdatedAt: workBuddyTime(snapshot.UpdatedAt), Source: "active"}, nil
}

var workBuddyPaidCodes = []string{"TCACA_code_002_AkiJS3ZHF5", "TCACA_code_005_maRGyrHhw1", "TCACA_code_003_FAnt7lcmRT", "TCACA_code_023_4xbGhMrE6q", "TCACA_code_026_BaESVICNoi", "TCACA_code_027_0FCGVA6vSa", "TCACA_code_043_6oIkxT7Z0S", "TCACA_code_009_0XmEQc2xOf", "TCACA_code_038_OhvqZtiPKr", "TCACA_code_036_lupO5WgNdG"}

var workBuddyFreeCodes = []string{"TCACA_code_001_PqouKr6QWV", "TCACA_code_008_cfWoLwvjU4", "TCACA_code_035_ArVxJcGDsm", "TCACA_code_006_DbXS0lrypC", "TCACA_code_039_KRcQj7wUat", "TCACA_code_040_mi9rCYg46x", "TCACA_code_007_nzdH5h4Nl0", "TCACA_code_028_NtpWi0jzXs", "TCACA_code_037_WxOD3MpI2o", "TCACA_code_029_6wCGEWquYy", "TCACA_code_030_BjSt89qTvr"}

var workBuddyPlanCodes = []string{"TCACA_code_002_AkiJS3ZHF5", "TCACA_code_005_maRGyrHhw1", "TCACA_code_003_FAnt7lcmRT", "TCACA_code_023_4xbGhMrE6q", "TCACA_code_026_BaESVICNoi", "TCACA_code_027_0FCGVA6vSa", "TCACA_code_043_6oIkxT7Z0S", "TCACA_code_035_ArVxJcGDsm", "TCACA_code_039_KRcQj7wUat", "TCACA_code_040_mi9rCYg46x"}

var workBuddyExtraCodes = []string{"TCACA_code_028_NtpWi0jzXs", "TCACA_code_037_WxOD3MpI2o", "TCACA_code_009_0XmEQc2xOf", "TCACA_code_038_OhvqZtiPKr", "TCACA_code_036_lupO5WgNdG", "TCACA_code_007_nzdH5h4Nl0", "TCACA_code_029_6wCGEWquYy", "TCACA_code_030_BjSt89qTvr"}
