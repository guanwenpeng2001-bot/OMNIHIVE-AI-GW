package service

import (
	"net/http"
	"time"
)

// UsageEvidence carries protocol observations after cache semantics and model
// selection have been resolved. Both gateways use the same projection for the
// public log and the immutable usage portion of the settlement receipt.
type UsageEvidence struct {
	RequestID                string
	Model                    string
	RequestedModel           string
	UpstreamModel            string
	ResponseModel            string
	Key                      *APIKey
	User                     *User
	Account                  *Account
	Subscription             *UserSubscription
	Cost                     *CostBreakdown
	Tokens                   UsageTokens
	UpstreamHeaders          http.Header
	ServiceTier              *string
	ReasoningEffort          *string
	RequestedReasoningEffort *string
	RateMultiplier           float64
	AccountRateMultiplier    float64
	BillingType              int8
	Mode                     BillingMode
	ImageCount               int
	ImageSize                string
	ImageInputSize           string
	ImageOutputSize          string
	ImageSizeSource          string
	ImageSizeBreakdown       map[string]int
	Stream                   bool
	WebSocket                bool
	Duration                 time.Duration
	FirstTokenMs             *int
	ChannelID                int64
	InboundEndpoint          string
	UpstreamEndpoint         string
	ModelMappingChain        string
	UserAgent                string
	IPAddress                string
	SessionID                string
}

func (e UsageEvidence) usageLog() *UsageLog {
	duration := int(e.Duration.Milliseconds())
	mode := string(e.Mode)
	if e.Cost.BillingMode != "" {
		mode = e.Cost.BillingMode
	}
	return &UsageLog{
		RequestID:                 e.RequestID,
		Model:                     e.Model,
		RequestedModel:            e.RequestedModel,
		UpstreamModel:             optionalTrimmedStringPtr(e.UpstreamModel),
		UpstreamResponseModel:     optionalTrimmedStringPtr(e.ResponseModel),
		UpstreamModelMismatch:     upstreamModelMismatch(upstreamSentModel(e.Model, e.UpstreamModel), e.ResponseModel),
		UserID:                    e.User.ID,
		APIKeyID:                  e.Key.ID,
		AccountID:                 e.Account.ID,
		GroupID:                   e.Key.GroupID,
		SubscriptionID:            optionalSubscriptionID(e.Subscription),
		UpstreamRequestID:         usageUpstreamRequestIDPtr(e.Account, e.UpstreamHeaders, e.WebSocket),
		PricingState:              e.Cost.PricingState,
		PricingSnapshot:           e.Cost.PricingSnapshot,
		PriceSource:               optionalTrimmedStringPtr(e.Cost.PriceSource),
		PriceCardVersion:          optionalTrimmedStringPtr(e.Cost.PriceCardVersion),
		TotalCost:                 e.Cost.TotalCost,
		ActualCost:                QuantizeUsageBillingAmount(e.Cost.ActualCost),
		InputCost:                 e.Cost.InputCost,
		OutputCost:                e.Cost.OutputCost,
		ImageInputCost:            e.Cost.ImageInputCost,
		ImageOutputCost:           e.Cost.ImageOutputCost,
		CacheCreationCost:         e.Cost.CacheCreationCost,
		CacheReadCost:             e.Cost.CacheReadCost,
		LongContextBillingApplied: e.Cost.LongContextBillingApplied,
		InputTokens:               e.Tokens.InputTokens,
		OutputTokens:              e.Tokens.OutputTokens,
		ImageInputTokens:          e.Tokens.ImageInputTokens,
		ImageOutputTokens:         e.Tokens.ImageOutputTokens,
		CacheReadTokens:           e.Tokens.CacheReadTokens,
		CacheCreationTokens:       e.Tokens.CacheCreationTokens,
		CacheCreation5mTokens:     e.Tokens.CacheCreation5mTokens,
		CacheCreation1hTokens:     e.Tokens.CacheCreation1hTokens,
		RateMultiplier:            e.RateMultiplier,
		AccountRateMultiplier:     &e.AccountRateMultiplier,
		BillingType:               e.BillingType,
		BillingMode:               &mode,
		ImageCount:                e.ImageCount,
		ImageSize:                 optionalTrimmedStringPtr(e.ImageSize),
		ImageInputSize:            optionalTrimmedStringPtr(e.ImageInputSize),
		ImageOutputSize:           optionalTrimmedStringPtr(e.ImageOutputSize),
		ImageSizeSource:           optionalTrimmedStringPtr(e.ImageSizeSource),
		ImageSizeBreakdown:        e.ImageSizeBreakdown,
		ServiceTier:               e.ServiceTier,
		ReasoningEffort:           e.ReasoningEffort,
		RequestedReasoningEffort:  optionalTrimmedStringPtr(optionalStringValue(e.RequestedReasoningEffort)),
		Stream:                    e.Stream,
		OpenAIWSMode:              e.WebSocket,
		DurationMs:                &duration,
		FirstTokenMs:              e.FirstTokenMs,
		CreatedAt:                 time.Now(),
		ChannelID:                 optionalInt64Ptr(e.ChannelID),
		ModelMappingChain:         optionalTrimmedStringPtr(e.ModelMappingChain),
		InboundEndpoint:           optionalTrimmedStringPtr(e.InboundEndpoint),
		UpstreamEndpoint:          optionalTrimmedStringPtr(e.UpstreamEndpoint),
		UserAgent:                 optionalTrimmedStringPtr(e.UserAgent),
		IPAddress:                 optionalTrimmedStringPtr(e.IPAddress),
		SessionID:                 optionalTrimmedStringPtr(e.SessionID),
	}
}
