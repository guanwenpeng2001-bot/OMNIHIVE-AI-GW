package service

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const QianwenFileBatchPriceVersion = "dashscope-cn-file-batch-2026-09-14"

// Beijing prices in CNY per million tokens, already discounted by 50%.
// https://help.aliyun.com/zh/model-studio/model-pricing
// https://help.aliyun.com/zh/model-studio/batch-inference
// Only flat, non-thinking text tariffs are admitted; never infer a batch price
// from a synchronous Batch Chat card or a similarly named model.
func qianwenFileBatchTariff(model string) (float64, float64, bool) {
	switch model {
	case "qwen-long", "qwen-long-latest":
		return .25, 1, true
	}
	return 0, 0, false
}

func (f *QianwenFileBatchSnapshot) BatchModel() string {
	if f == nil || f.Model == "" {
		return QianwenBatchTestModel
	}
	return f.Model
}
func (f *QianwenFileBatchSnapshot) BatchEndpoint() string {
	if f == nil || f.Endpoint == "" {
		return "/v1/chat/ds-test"
	}
	return f.Endpoint
}

func QianwenFileBatchBillingSnapshot(group *Group, account *Account, key *APIKey, subscription *UserSubscription, f *QianwenFileBatchSnapshot) (UpstreamJobBilling, error) {
	b := UpstreamJobBilling{Model: f.BatchModel(), RequestedModel: f.BatchModel(), RateMultiplier: 1, AccountRateMultiplier: 1, FileBatch: f}
	if b.Model == QianwenBatchTestModel {
		return b, nil
	}
	in, out, ok := qianwenFileBatchTariff(b.Model)
	if !ok || group == nil || account == nil {
		return b, ErrUpstreamJobTariff
	}
	base, err := url.Parse(account.GetCNProtocolBaseURL(APIProtocolChatCompletions))
	if err != nil || base.Scheme != "https" || (base.Host != "dashscope.aliyuncs.com" && base.Host != "batch.dashscope.aliyuncs.com") || strings.TrimRight(base.Path, "/") != "/compatible-mode/v1" {
		return b, fmt.Errorf("file batch tariffs require the Beijing DashScope endpoint")
	}
	var opts QianwenPricingOptions
	raw, _ := json.Marshal(account.Extra["qianwen_pricing_sync"])
	if json.Unmarshal(raw, &opts) != nil || opts.Region != "asia-pacific-china" || !finitePositive(opts.USDPerCNY) || strings.TrimSpace(opts.FXSource) == "" {
		return b, fmt.Errorf("file batch pricing requires explicit Beijing FX provenance from pricing sync")
	}
	if _, err := time.Parse(time.RFC3339, opts.FXEffectiveAt); err != nil {
		return b, ErrUpstreamJobTariff
	}
	f.InputPrice, f.OutputPrice = in*opts.USDPerCNY/1e6, out*opts.USDPerCNY/1e6
	f.PriceVersion, f.FX = QianwenFileBatchPriceVersion, &opts
	b.RateMultiplier = group.RateMultiplier * group.PeakMultiplierAt(time.Now())
	b.AccountRateMultiplier = account.BillingRateMultiplier()
	if !finitePositive(b.RateMultiplier) || !finitePositive(b.AccountRateMultiplier) {
		return b, ErrUpstreamJobTariff
	}
	b.AccountQuota = account.HasAnyQuotaLimit()
	if key != nil {
		b.KeyQuota, b.KeyRateLimit = key.Quota > 0, key.HasRateLimits()
	}
	if subscription != nil {
		b.SubscriptionID = &subscription.ID
	}
	return b, nil
}

func chargeQianwenFileBatch(j *UpstreamJob, input, output int) (*UsageLog, *UsageBillingCommand, error) {
	b, f := j.Billing, j.Billing.FileBatch
	mode, source, version := "file_batch", "dashscope_free_test_model", "file-batch-test-v1"
	var inputCost, outputCost float64
	if b.Model != QianwenBatchTestModel {
		in, out, ok := qianwenFileBatchTariff(b.Model)
		if !ok || f.FX == nil || f.PriceVersion != QianwenFileBatchPriceVersion || !finitePositive(f.InputPrice) || !finitePositive(f.OutputPrice) || !finitePositive(b.RateMultiplier) || !finitePositive(b.AccountRateMultiplier) || f.FX.Region != "asia-pacific-china" || !finitePositive(f.FX.USDPerCNY) || f.InputPrice != in*f.FX.USDPerCNY/1e6 || f.OutputPrice != out*f.FX.USDPerCNY/1e6 {
			return nil, nil, ErrUpstreamJobTariff
		}
		source, version = "dashscope_cn_file_batch", f.PriceVersion
		if j.Family == "batch" {
			inputCost, outputCost = float64(input)*f.InputPrice, float64(output)*f.OutputPrice
		}
	}
	total := QuantizeUsageBillingAmount(inputCost + outputCost)
	multiplier := b.RateMultiplier
	if b.Model == QianwenBatchTestModel {
		multiplier = 1
	}
	actual := QuantizeUsageBillingAmount(total * multiplier)
	accountCost := QuantizeUsageBillingAmount(total * b.AccountRateMultiplier)
	log := &UsageLog{UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, GroupID: j.GroupID, RequestID: j.PublicRequestID, Model: b.Model, RequestedModel: b.RequestedModel, UpstreamModel: &b.Model, InputTokens: input, OutputTokens: output, InputCost: inputCost, OutputCost: outputCost, TotalCost: total, ActualCost: actual, RateMultiplier: multiplier, AccountRateMultiplier: &b.AccountRateMultiplier, AccountStatsCost: &accountCost, SubscriptionID: b.SubscriptionID, BillingMode: &mode, PriceSource: &source, PriceCardVersion: &version, RequestType: RequestTypeSync, CreatedAt: time.Now().UTC()}
	cmd := &UsageBillingCommand{RequestID: j.PublicRequestID, RequestFingerprint: j.RequestFingerprint, UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, AccountType: AccountTypeAPIKey, Model: b.Model, BalanceCost: actual}
	if b.SubscriptionID != nil {
		log.BillingType = BillingTypeSubscription
		cmd.BillingType = BillingTypeSubscription
		cmd.SubscriptionID = b.SubscriptionID
		cmd.SubscriptionCost = actual
		cmd.BalanceCost = 0
	}
	if b.KeyQuota {
		cmd.APIKeyQuotaCost = actual
	}
	if b.KeyRateLimit {
		cmd.APIKeyRateLimitCost = actual
	}
	if b.AccountQuota {
		cmd.AccountQuotaCost = accountCost
	}
	return log, cmd, nil
}
