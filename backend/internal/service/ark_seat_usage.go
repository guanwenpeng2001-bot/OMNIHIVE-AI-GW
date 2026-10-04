package service

import (
	"context"
	"encoding/json"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func byteplusSeatUsageProfile(account *Account) bool {
	if account.Platform != "byteplus" || account.Type != AccountTypeAPIKey || account.GetAccountMode() != AccountModeCoding {
		return false
	}
	// BytePlus is a compatible-provider registry entry, not a native profile.
	// Validate every saved protocol root before selecting its control plane.
	bases := []string{strings.TrimSpace(account.GetCredential("base_url"))}
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		bases = append(bases, nativeProtocolOverride(account, protocol))
	}
	configured := false
	for _, base := range bases {
		if base == "" {
			continue
		}
		configured = true
		switch nativeProfileURL(base) {
		case "https://ark.ap-southeast.bytepluses.com/api/coding/v3", "https://ark.ap-southeast.bytepluses.com/api/coding/v1":
		default:
			return false
		}
	}
	if !configured {
		return nativeProfileURL(account.GetOpenAIBaseURL()) == "https://ark.ap-southeast.bytepluses.com/api/coding/v3"
	}
	return true
}

func (service *CNProviderQuotaService) queryArkSeatUsage(ctx context.Context, account *Account) *CNProviderQuotaProbeResult {
	result := &CNProviderQuotaProbeResult{Provider: account.Platform, Source: "coding_plan", FetchedAt: time.Now().UTC().Unix(), CredentialValid: true}
	fail := func(status, message string) *CNProviderQuotaProbeResult {
		result.Status, result.Error = status, message
		return result
	}
	prefix, target := "volcengine", "https://ark.cn-beijing.volcengineapi.com/"
	if account.Platform == "byteplus" {
		if account.GetCredential("billing_plan") != "team" {
			result.Unsupported = true
			return fail("no_verified_query", "BytePlus personal quota has no verified management query contract")
		}
		prefix, target = "byteplus", "https://ark.ap-southeast-1.byteplusapi.com/"
		if !byteplusSeatUsageProfile(account) {
			result.Unsupported = true
			return fail("unsupported_profile", "No verified quota contract for this regional or plan profile")
		}
	} else if account.Type != AccountTypeAPIKey || !volcengineCodingUsageProfile(account) || account.GetCredential("volcengine_plan_edition") != "team" {
		result.Unsupported = true
		return fail("unsupported_profile", "No verified team quota contract for this profile")
	}
	result.PlanLevel = "team"
	seat := strings.TrimSpace(account.GetCredential(prefix + "_seat_id"))
	if seat == "" {
		return fail("no_seat", "Team quota requires an explicit saved SeatID")
	}
	if len(seat) > 1024 {
		return fail("invalid_credentials", "Invalid team seat identifier")
	}
	accessKey, secretKey := strings.TrimSpace(account.GetCredential(prefix+"_access_key_id")), strings.TrimSpace(account.GetCredential(prefix+"_secret_access_key"))
	if !validateCloudQueryKeys(result, accessKey, secretKey) {
		return result
	}
	endpoint, proxy, ok := service.cloudQueryEndpoint(ctx, account, target, result)
	if !ok {
		return result
	}
	callCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(ctx), cnQuotaUpstreamTimeout)
	defer cancel()
	payload, _ := json.Marshal(struct{ SeatID string }{seat})
	body, ok := service.requestVolcengineUsage(callCtx, account, endpoint, proxy, accessKey, secretKey, "GetSeatInfoUsage", payload, result)
	if !ok {
		return result
	}
	tiers, status := parseArkSeatUsage(body, seat)
	if status != "ok" {
		return fail(status, "Unrecognized team seat quota response")
	}
	result.Tiers, result.Success, result.Status = tiers, true, "ok"
	updates := cnQuotaExtraUpdates(account.Platform, tiers, time.Now().UTC())
	for _, suffix := range []string{cnExtraSuffix5hUsed, cnExtraSuffix5hReset, cnExtraSuffixWeeklyUsed, cnExtraSuffixWeeklyReset, cnExtraSuffixMonthlyUsed, cnExtraSuffixMonthlyReset} {
		key := cnExtraKey(account.Platform, suffix)
		if _, exists := updates[key]; !exists {
			updates[key] = nil
		}
	}
	service.persistCloudQuery(ctx, account, result, updates)
	return result
}

// Public GetSeatInfoUsage schemas put windows in Result (explorer) or
// Result.SeatInfoUsage (BytePlus documentation). Known overlapping fields must
// agree, except UserID metadata whose incompatible official types are not quota.
func parseArkSeatUsage(body []byte, seat string) ([]CNQuotaTier, string) {
	invalid := func() ([]CNQuotaTier, string) { return nil, "invalid_response" }
	if !gjson.ValidBytes(body) {
		return invalid()
	}
	flat := gjson.GetBytes(body, "Result")
	if !flat.IsObject() {
		return invalid()
	}
	nested := flat.Get("SeatInfoUsage")
	if nested.Exists() && !nested.IsObject() {
		return invalid()
	}
	merged := map[string]gjson.Result{}
	for _, field := range []string{"SeatID", "AccountID", "ProjectName", "UserName", "MonthlySubscribeMilestone", "ShortTermUsage", "WeeklyUsage", "MonthlyUsage", "ShortTermResetMilestone", "WeeklyResetMilestone", "MonthlyResetMilestone"} {
		flatValue, nestedValue := flat.Get(field), nested.Get(field)
		if flatValue.Exists() && nestedValue.Exists() {
			equal := flatValue.Type == nestedValue.Type && flatValue.Raw == nestedValue.Raw
			if flatValue.Type == gjson.String && nestedValue.Type == gjson.String {
				equal = flatValue.Str == nestedValue.Str
			}
			if flatValue.Type == gjson.Number && nestedValue.Type == gjson.Number {
				flatNumber, flatNumberOK := arkFiniteNumber(flatValue)
				nestedNumber, nestedNumberOK := arkFiniteNumber(nestedValue)
				equal = flatNumberOK && nestedNumberOK && flatNumber.Cmp(nestedNumber) == 0
			}
			if !equal {
				return invalid()
			}
		}
		if flatValue.Exists() {
			merged[field] = flatValue
		} else {
			merged[field] = nestedValue
		}
	}
	if id := merged["SeatID"]; id.Exists() && id.Type != gjson.String {
		return invalid()
	}
	tiers := []CNQuotaTier{}
	for _, window := range []struct{ field, window string }{{"ShortTerm", "5h"}, {"Weekly", "weekly"}, {"Monthly", "monthly"}} {
		usage, reset := merged[window.field+"Usage"], merged[window.field+"ResetMilestone"]
		resetAt := ""
		if reset.Exists() && reset.Type != gjson.Null {
			number, ok := arkFiniteNumber(reset)
			if !ok || !number.IsInt() || !number.Num().IsInt64() {
				return invalid()
			}
			milliseconds := number.Num().Int64()
			if milliseconds > 0 {
				// These fields explicitly use milliseconds, never seconds heuristics.
				resetTime := time.UnixMilli(milliseconds).UTC()
				if resetTime.Year() < 1 || resetTime.Year() > 9999 {
					return invalid()
				}
				resetAt = resetTime.Format(time.RFC3339Nano)
			}
		}
		if !usage.Exists() {
			continue
		}
		if number, ok := arkFiniteNumber(usage); !ok || number.Sign() < 0 {
			return invalid()
		}
		tiers = append(tiers, CNQuotaTier{Window: window.window, UsedPercent: usage.Num, ResetAt: resetAt})
	}
	if len(tiers) == 0 {
		return invalid()
	}
	if id := merged["SeatID"]; id.Exists() && id.Str != seat {
		return nil, "no_seat"
	}
	return tiers, "ok"
}

func arkFiniteNumber(value gjson.Result) (*big.Rat, bool) {
	if value.Type != gjson.Number || math.IsNaN(value.Num) || math.IsInf(value.Num, 0) {
		return nil, false
	}
	// Limit exponent/precision work on untrusted input before exact comparison.
	if len(value.Raw) > 128 {
		return nil, false
	}
	if exponentIndex := strings.IndexAny(value.Raw, "eE"); exponentIndex >= 0 {
		exponent, err := strconv.ParseInt(value.Raw[exponentIndex+1:], 10, 32)
		if err != nil || exponent < -400 || exponent > 400 {
			return nil, false
		}
	}
	number, ok := new(big.Rat).SetString(value.Raw)
	return number, ok
}
