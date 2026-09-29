package repository

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelPricingFactsStorageRoundTrip(t *testing.T) {
	const payload = `{"models":["qwen"],"native_pricing_source":"qianwen","thinking_pricing":{"input_price":1,"output_price":2,"batch_chat_pricing":{"input_price":3,"output_price":4}},"explicit_cache_pricing":{"cache_write_price":5,"cache_read_price":0},"batch_chat_pricing":{"input_price":6,"output_price":7},"price_variants":[{"id":"qwen","canonical":"qwen","aliases":["alias"],"prices":[{"unit":"character","dimension":"output","currency":"USD","sales":0.1,"cost":0.01},{"unit":"second","dimension":"output","currency":"USD","sales":0.2,"cost":0.02}]}]}`
	for _, withSchedule := range []bool{false, true} {
		var original service.ChannelModelPricing
		require.NoError(t, json.Unmarshal([]byte(payload), &original))
		if withSchedule {
			original.TimePricing = &service.ChannelTimePricing{Timezone: "UTC", Periods: []service.ChannelTimePricingPeriod{{StartTime: "09:00", EndTime: "10:00", Multiplier: 2}}}
		}
		stored, err := marshalChannelPricingExtensions(&original)
		require.NoError(t, err)
		var restored service.ChannelModelPricing
		require.NoError(t, unmarshalChannelPricingExtensions([]byte(stored.(string)), &restored))
		require.Equal(t, original.PriceVariants, restored.PriceVariants)
		require.Equal(t, original.ThinkingPricing, restored.ThinkingPricing)
		require.Equal(t, original.ExplicitCachePricing, restored.ExplicitCachePricing)
		require.Equal(t, original.BatchChatPricing, restored.BatchChatPricing)
		require.Equal(t, original.TimePricing, restored.TimePricing)
		statsStored, err := marshalAccountStatsPricingModels(&original)
		require.NoError(t, err)
		var statsRestored service.ChannelModelPricing
		require.NoError(t, unmarshalAccountStatsPricingModels(statsStored, &statsRestored))
		require.Equal(t, original, statsRestored)
	}
	var legacy service.ChannelModelPricing
	require.NoError(t, unmarshalAccountStatsPricingModels([]byte(`["legacy"]`), &legacy))
	require.Equal(t, []string{"legacy"}, legacy.Models)
}
