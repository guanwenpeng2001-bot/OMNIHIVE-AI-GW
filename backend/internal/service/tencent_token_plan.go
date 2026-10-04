package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Amounts retain the provider's decimal representation. A missing property is
// unknown, not zero. TokenSummary is a breakdown, not a quota window or wallet.
type CNTokenPlanQuota struct {
	ProductType  string              `json:"product_type"`
	Unit         string              `json:"unit"`
	PackageInfo  *CNTokenPlanPackage `json:"package_info,omitempty"`
	TokenSummary *CNTokenPlanSummary `json:"token_summary,omitempty"`
}

type CNTokenPlanPackage struct {
	TotalQuota         *string `json:"total_quota,omitempty"`
	TotalUsed          *string `json:"total_used,omitempty"`
	CycleQuota         *string `json:"cycle_quota,omitempty"`
	SharedPool         *string `json:"shared_pool,omitempty"`
	SharedUsed         *string `json:"shared_used,omitempty"`
	ExclusiveAllocated *string `json:"exclusive_allocated,omitempty"`
	ExclusiveUsed      *string `json:"exclusive_used,omitempty"`
	StartTime          *string `json:"start_time,omitempty"`
	ExpireTime         *string `json:"expire_time,omitempty"`
	CycleUnit          *string `json:"cycle_unit,omitempty"`
	CurrentCycle       *int64  `json:"current_cycle,omitempty"`
	RemainCycles       *int64  `json:"remain_cycles,omitempty"`
	TotalCycles        *int64  `json:"total_cycles,omitempty"`
}

type CNTokenPlanSummary struct {
	CycleSeq       *int64                   `json:"cycle_seq,omitempty"`
	CycleStartTime *string                  `json:"cycle_start_time,omitempty"`
	CycleEndTime   *string                  `json:"cycle_end_time,omitempty"`
	BillingItems   []CNTokenPlanBillingItem `json:"billing_items,omitempty"`
}

type CNTokenPlanBillingItem struct {
	BillingItem *string `json:"billing_item,omitempty"`
	TotalQty    *string `json:"total_qty,omitempty"` // Exact decimal encoding of upstream int64 for JSON clients.
}

func tencentTokenPlanProfile(account *Account) bool {
	if account == nil || account.Platform != PlatformHunyuan || account.GetAccountMode() != AccountModeCoding {
		return false
	}
	profile := nativeAccountProfile(account)
	return profile != nil && profile.Chat == "https://tokenhub.tencentmaas.com/plan/v3"
}

var tencentQueryRegionPattern = regexp.MustCompile(`^[a-z]+(?:-[a-z0-9]+)+$`)

func (s *CNProviderQuotaService) queryTencentTokenPlan(ctx context.Context, account *Account) *CNProviderQuotaProbeResult {
	result := &CNProviderQuotaProbeResult{Provider: PlatformHunyuan, Source: "token_plan", FetchedAt: time.Now().UTC().Unix(), CredentialValid: true}
	if !tencentTokenPlanProfile(account) {
		result.Status, result.Unsupported, result.Error = "unsupported_profile", true, "Only the domestic enterprise Token Plan profile has a verified cloud quota query"
		return result
	}
	access, secret := strings.TrimSpace(account.GetCredential("tencent_query_secret_id")), strings.TrimSpace(account.GetCredential("tencent_query_secret_key"))
	if !validateCloudQueryKeys(result, access, secret) {
		return result
	}
	region, team := strings.TrimSpace(account.GetCredential("tencent_query_region")), strings.TrimSpace(account.GetCredential("tencent_query_team_id"))
	if region == "" || team == "" {
		result.Status, result.Error = "missing_configuration", "Tencent quota requires an explicit region and TeamId"
		return result
	}
	if len(region) > 64 || !tencentQueryRegionPattern.MatchString(region) || len(team) > 1024 {
		result.Status, result.Error = "invalid_configuration", "Invalid Tencent quota region or TeamId"
		return result
	}
	endpoint, proxy, ok := s.cloudQueryEndpoint(ctx, account, "https://tokenhub.tencentcloudapi.com/", result)
	if !ok {
		return result
	}
	callCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(ctx), cnQuotaUpstreamTimeout)
	defer cancel()
	payload, _ := json.Marshal(struct{ TeamId string }{team})
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		result.Status, result.Error = "upstream_error", "Quota request construction failed"
		return result
	}
	signTencentTokenPlan(req, payload, access, secret, region, time.Now())
	req.Header.Set("Accept", "application/json")
	resp, err := accountHTTPUpstream(s.httpUpstream, account).Do(req, proxy, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		result.Status, result.Error = "upstream_error", "Tencent quota request failed"
		return result
	}
	if resp == nil || resp.Body == nil {
		result.Status, result.Error = "invalid_response", "Empty Tencent quota response"
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, cnQuotaMaxBodyBytes+1))
	if err != nil || len(body) > cnQuotaMaxBodyBytes {
		result.Status, result.Error = "invalid_response", "Tencent quota response unreadable or too large"
		return result
	}
	var envelope struct {
		Response *struct {
			Error       *struct{ Code string }
			TeamId      string
			ProductType string
			PackageInfo *struct {
				TotalQuota, TotalUsed, CycleQuota, SharedPool, SharedUsed, ExclusiveAllocated, ExclusiveUsed *string
				StartTime, ExpireTime, CycleUnit                                                             *string
				CurrentCycle, RemainCycles, TotalCycles                                                      *int64
			}
			TokenSummary *struct {
				CycleSeq                     *int64
				CycleStartTime, CycleEndTime *string
				BillingItems                 []struct {
					BillingItem *string
					TotalQty    *int64
				}
			}
		}
	}
	decodeErr := json.Unmarshal(body, &envelope)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (envelope.Response != nil && envelope.Response.Error != nil) {
		code := ""
		if envelope.Response != nil && envelope.Response.Error != nil {
			code = strings.ToLower(envelope.Response.Error.Code)
		}
		result.Status = "upstream_error"
		switch {
		case resp.StatusCode == 403 || strings.HasPrefix(code, "unauthorizedoperation"):
			result.Status = "permission_denied"
		case resp.StatusCode == 401 || strings.HasPrefix(code, "authfailure"):
			result.Status, result.CredentialValid = "invalid_credentials", false
		}
		result.Error = "Tencent quota query " + result.Status
		return result
	}
	response := envelope.Response
	if decodeErr != nil || response == nil || response.TeamId != team || (response.ProductType != "enterprise" && response.ProductType != "enterprise-auto") || (response.PackageInfo == nil && response.TokenSummary == nil) {
		result.Status, result.Error = "invalid_response", "Unrecognized Tencent enterprise quota response"
		return result
	}
	plan := &CNTokenPlanQuota{ProductType: response.ProductType, Unit: "credits"}
	if response.ProductType == "enterprise-auto" {
		plan.Unit = "tokens"
	}
	if response.PackageInfo != nil {
		plan.PackageInfo = (*CNTokenPlanPackage)(response.PackageInfo)
		for _, cycle := range []*int64{plan.PackageInfo.CurrentCycle, plan.PackageInfo.RemainCycles, plan.PackageInfo.TotalCycles} {
			if !validTokenPlanCycle(cycle) {
				result.Status, result.Error = "invalid_response", "Invalid Tencent quota cycle number"
				return result
			}
		}
	}
	if response.TokenSummary != nil {
		summary := response.TokenSummary
		if !validTokenPlanCycle(summary.CycleSeq) {
			result.Status, result.Error = "invalid_response", "Invalid Tencent quota cycle number"
			return result
		}
		plan.TokenSummary = &CNTokenPlanSummary{CycleSeq: summary.CycleSeq, CycleStartTime: summary.CycleStartTime, CycleEndTime: summary.CycleEndTime}
		for _, item := range summary.BillingItems {
			billingItem := CNTokenPlanBillingItem{BillingItem: item.BillingItem}
			if item.TotalQty != nil {
				quantity := strconv.FormatInt(*item.TotalQty, 10)
				billingItem.TotalQty = &quantity
			}
			plan.TokenSummary.BillingItems = append(plan.TokenSummary.BillingItems, billingItem)
		}
	}
	result.TokenPlan, result.Success, result.Status = plan, true, "ok"
	s.persistCloudQuery(ctx, account, result, map[string]any{"hunyuan_token_plan": plan, "hunyuan_usage_updated_at": time.Now().UTC().Format(time.RFC3339)})
	return result
}

func validTokenPlanCycle(cycle *int64) bool {
	// Cycle indices remain JSON numbers and must be exact in browser clients.
	return cycle == nil || (*cycle >= 0 && *cycle <= 9007199254740991)
}

// TC3-HMAC-SHA256, following TencentCloud/tencentcloud-sdk-go common/client.go.
// Signing and transmission use the exact same host, content type and body bytes.
func signTencentTokenPlan(req *http.Request, body []byte, access, secret, region string, now time.Time) {
	hash := func(data []byte) string { digest := sha256.Sum256(data); return hex.EncodeToString(digest[:]) }
	mac := func(key []byte, data string) []byte {
		digest := hmac.New(sha256.New, key)
		_, _ = digest.Write([]byte(data))
		return digest.Sum(nil)
	}
	date, timestamp := now.UTC().Format("2006-01-02"), strconv.FormatInt(now.Unix(), 10)
	const contentType = "application/json"
	const signedHeaders = "content-type;host"
	canonical := strings.Join([]string{req.Method, req.URL.EscapedPath(), req.URL.RawQuery, "content-type:" + contentType + "\nhost:" + req.URL.Host + "\n", signedHeaders, hash(body)}, "\n")
	scope := date + "/tokenhub/tc3_request"
	key := mac(mac(mac([]byte("TC3"+secret), date), "tokenhub"), "tc3_request")
	signature := hex.EncodeToString(mac(key, "TC3-HMAC-SHA256\n"+timestamp+"\n"+scope+"\n"+hash([]byte(canonical))))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-TC-Action", "DescribeTokenPlan")
	req.Header.Set("X-TC-Version", "2026-03-22")
	req.Header.Set("X-TC-Timestamp", timestamp)
	req.Header.Set("X-TC-Region", region)
	req.Header.Set("Authorization", "TC3-HMAC-SHA256 Credential="+access+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}
