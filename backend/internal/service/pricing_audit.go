package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// The content hash identifies the exact resolved card, including conditional
// rates and channel scheduling. It is not a guessed upstream publication date.
func pricingAuditIdentity(resolved *ResolvedPricing) (string, string) {
	if resolved == nil || resolved.Source == "" {
		return "", ""
	}
	payload := struct {
		Resolved    *ResolvedPricing
		Config      *ChannelModelPricing
		LongContext bool
	}{resolved, resolved.channelPricing, resolved.longContextPricingEnabled}
	data, err := json.Marshal(payload)
	if err != nil {
		return resolved.Source, ""
	}
	digest := sha256.Sum256(data)
	source := resolved.Source
	if resolved.channelPricing != nil && resolved.channelPricing.NativePricingSource != "" {
		source += ":" + resolved.channelPricing.NativePricingSource
	}
	return source, "sha256:" + hex.EncodeToString(digest[:])
}
