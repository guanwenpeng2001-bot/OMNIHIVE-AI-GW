package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const volcengineUsageEndpoint = "https://open.volcengineapi.com/"

// Only the exact domestic Coding profile can select the domestic management
// host. Unknown, mixed, Agent Plan and BytePlus roots must never borrow it.
func volcengineCodingUsageProfile(account *Account) bool {
	if account.Platform != PlatformVolcengine || account.GetAccountMode() != AccountModeCoding {
		return false
	}
	p := nativeAccountProfile(account)
	return p != nil && p.Chat == "https://ark.cn-beijing.volces.com/api/coding/v3"
}

func (s *CNProviderQuotaService) queryVolcengineCodingUsage(ctx context.Context, account *Account) *CNProviderQuotaProbeResult {
	r := &CNProviderQuotaProbeResult{Provider: account.Platform, Source: "coding_plan", FetchedAt: time.Now().UTC().Unix(), CredentialValid: true}
	fail := func(status, message string) *CNProviderQuotaProbeResult {
		r.Status, r.Error = status, message
		return r
	}
	if account.Platform == "byteplus" {
		r.Unsupported = true
		return fail("no_verified_query", "BytePlus subscription quota has no verified management query contract")
	}
	if !volcengineCodingUsageProfile(account) {
		r.Unsupported = true
		return fail("unsupported_profile", "No verified quota response contract for this regional or plan profile")
	}
	edition := account.GetCredential("volcengine_plan_edition")
	if edition == "" {
		edition = "personal"
	}
	if edition != "personal" && edition != "team" {
		return fail("unsupported_profile", "Unknown Coding Plan edition")
	}
	r.PlanLevel = edition
	ak, sk := strings.TrimSpace(account.GetCredential("volcengine_access_key_id")), strings.TrimSpace(account.GetCredential("volcengine_secret_access_key"))
	if ak == "" || sk == "" {
		r.CredentialValid = false
		return fail("missing_credentials", "Quota query requires Volcengine AccessKey and SecretKey; inference API keys cannot replace them")
	}
	if len(ak) > 512 || len(sk) > 4096 || strings.ContainsAny(ak, "\r\n\t ,/=") || strings.ContainsAny(sk, "\r\n") {
		r.CredentialValid = false
		return fail("invalid_credentials", "Invalid quota query credentials")
	}
	endpoint, err := cnValidateProbeURL(s.cfg, volcengineUsageEndpoint)
	if err != nil {
		return fail("url_rejected", "Quota management host rejected by outbound URL policy")
	}
	// The URL policy normalizer removes trailing slashes. OpenAPI signs and
	// transmits the root path '/', so restore it before signing.
	endpoint = strings.TrimRight(endpoint, "/") + "/"
	proxy, err := s.resolveProxyURL(ctx, account)
	if err != nil {
		return fail("upstream_error", "Quota proxy resolution failed")
	}
	// A saved binding must not silently fall back to the default egress.
	if account.ProxyID != nil && proxy == "" {
		return fail("upstream_error", "Quota proxy binding could not be resolved")
	}
	callCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(ctx), cnQuotaUpstreamTimeout)
	defer cancel()
	action, payload := "GetCodingPlanUsage", []byte(`{}`)
	if edition == "team" {
		seat := strings.TrimSpace(account.GetCredential("volcengine_seat_id"))
		if seat == "" {
			body, ok := s.requestVolcengineUsage(callCtx, account, endpoint, proxy, ak, sk, "GetSeatInfo", []byte(`{"Scene":""}`), r)
			if !ok {
				return r
			}
			value := gjson.GetBytes(body, "Result.SeatID")
			if value.Type != gjson.String || strings.TrimSpace(value.Str) == "" {
				return fail("no_seat", "No Coding Plan seat bound to this identity")
			}
			seat = value.Str
		}
		if len(seat) > 1024 {
			return fail("invalid_response", "Invalid Coding Plan seat identifier")
		}
		payload, _ = json.Marshal(struct {
			SeatID string
			Scene  string
		}{seat, ""})
		action = "GetSeatInfoUsage"
	}
	body, ok := s.requestVolcengineUsage(callCtx, account, endpoint, proxy, ak, sk, action, payload, r)
	if !ok {
		return r
	}
	tiers, status := parseVolcengineCodingUsage(body)
	if status != "ok" {
		if status == "not_subscribed" {
			return fail(status, "No active Coding Plan subscription")
		}
		return fail(status, "Unrecognized Coding Plan quota response")
	}
	r.Tiers, r.Success, r.Status = tiers, true, "ok"
	updates := cnQuotaExtraUpdates(PlatformVolcengine, tiers, time.Now().UTC())
	// Remove absent windows and sentinel resets from the previous snapshot; a
	// successful partial response must not revive stale percentages or resets.
	for _, suffix := range []string{cnExtraSuffix5hUsed, cnExtraSuffix5hReset, cnExtraSuffixWeeklyUsed, cnExtraSuffixWeeklyReset, cnExtraSuffixMonthlyUsed, cnExtraSuffixMonthlyReset} {
		key := cnExtraKey(PlatformVolcengine, suffix)
		if _, exists := updates[key]; !exists {
			updates[key] = nil
		}
	}
	if err := persistAccountRuntimeSnapshot(ctx, s.accountRepo, account, updates, nil); errors.Is(err, ErrAccountRuntimeIdentityChanged) {
		r.Tiers, r.Success = nil, false
		return fail("upstream_error", "Account query identity changed; query quota again")
	} else if err == nil {
		r.Persisted = true
	}
	return r
}

func (s *CNProviderQuotaService) requestVolcengineUsage(ctx context.Context, account *Account, endpoint, proxy, ak, sk, action string, payload []byte, r *CNProviderQuotaProbeResult) ([]byte, bool) {
	query := url.Values{"Action": {action}, "Version": {"2024-01-01"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(), bytes.NewReader(payload))
	if err != nil {
		r.Status, r.Error = "upstream_error", "Quota request construction failed"
		return nil, false
	}
	signVolcengineUsage(req, payload, ak, sk, time.Now())
	req.Header.Set("Accept", "application/json")
	// Never apply inference headers to this control-plane request.
	resp, err := accountHTTPUpstream(s.httpUpstream, account).Do(req, proxy, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		r.Status, r.Error = "upstream_error", "Quota management request failed"
		return nil, false
	}
	if resp == nil || resp.Body == nil {
		r.Status, r.Error = "invalid_response", "Empty quota response"
		return nil, false
	}
	defer resp.Body.Close()
	r.StatusCode = resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, cnQuotaMaxBodyBytes+1))
	if err != nil || len(body) > cnQuotaMaxBodyBytes {
		r.Status, r.Error = "invalid_response", "Quota response unreadable or too large"
		return nil, false
	}
	code := ""
	businessError := false
	if gjson.ValidBytes(body) {
		for _, path := range []string{"ResponseMetadata.Error", "Error"} {
			if node := gjson.GetBytes(body, path); node.Exists() {
				businessError = true
				code = strings.ToLower(node.Get("Code").String())
				break
			}
		}
	}
	if businessError || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		r.Status = "upstream_error"
		switch {
		case resp.StatusCode == 401 || strings.Contains(code, "signature") || strings.Contains(code, "unauthorized") || strings.Contains(code, "invalidauth") || strings.Contains(code, "invalidaccesskey") || strings.Contains(code, "credential"):
			r.Status, r.CredentialValid = "invalid_credentials", false
		case resp.StatusCode == 403 || strings.Contains(code, "denied") || strings.Contains(code, "forbidden"):
			r.Status = "permission_denied"
		case strings.Contains(code, "auth"):
			r.Status, r.CredentialValid = "invalid_credentials", false
		}
		// Error codes and messages are untrusted and may echo credentials. Emit
		// only our classified status and the HTTP status, never upstream strings.
		r.Error = fmt.Sprintf("Quota query %s (HTTP %d)", r.Status, resp.StatusCode)
		return nil, false
	}
	if !gjson.ValidBytes(body) {
		r.Status, r.Error = "invalid_response", "Invalid quota JSON response"
		return nil, false
	}
	return body, true
}

// Field paths follow cc-switch 793e67d9's observed Level/Percent/ResetTimestamp
// fixture, NOT ark-cli's normalized CLI output. Raw live validation is pending.
func parseVolcengineCodingUsage(body []byte) ([]CNQuotaTier, string) {
	if !gjson.ValidBytes(body) {
		return nil, "invalid_response"
	}
	items := gjson.GetBytes(body, "Result.QuotaUsage")
	if !items.IsArray() {
		return nil, "invalid_response"
	}
	byWindow := map[string]CNQuotaTier{}
	count, valid := 0, true
	items.ForEach(func(_, item gjson.Result) bool {
		count++
		window := strings.ToLower(item.Get("Level").String())
		switch window {
		case "session":
			window = "5h"
		case "weekly", "monthly":
		default:
			return true
		}
		percent, reset := item.Get("Percent"), item.Get("ResetTimestamp")
		if percent.Type != gjson.Number || math.IsNaN(percent.Num) || math.IsInf(percent.Num, 0) || reset.Type != gjson.Number || math.IsNaN(reset.Num) || math.IsInf(reset.Num, 0) || math.Trunc(reset.Num) != reset.Num || reset.Num >= float64(math.MaxInt64) {
			valid = false
			return false
		}
		if _, duplicate := byWindow[window]; duplicate {
			valid = false
			return false
		}
		resetAt := ""
		if reset.Num > 0 {
			resetAt = cnMillisToRFC3339(reset.Int())
			if _, err := time.Parse(time.RFC3339, resetAt); err != nil {
				valid = false
				return false
			}
		}
		byWindow[window] = CNQuotaTier{Window: window, UsedPercent: percent.Num, ResetAt: resetAt}
		return true
	})
	if count == 0 {
		return nil, "not_subscribed"
	}
	if !valid || len(byWindow) == 0 {
		return nil, "invalid_response"
	}
	tiers := make([]CNQuotaTier, 0, len(byWindow))
	for _, window := range []string{"5h", "weekly", "monthly"} {
		if tier, ok := byWindow[window]; ok {
			tiers = append(tiers, tier)
		}
	}
	return tiers, "ok"
}
