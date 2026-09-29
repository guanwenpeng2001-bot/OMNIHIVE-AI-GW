package admin

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelPricingFactsCreateUpdateResponseRoundTrip(t *testing.T) {
	const payload = `{"name":"native channel","model_pricing":[{"platform":"qianwen","models":["native"],"billing_mode":"token","input_price":0.01,"output_price":0.02,"native_pricing_source":"qianwen","thinking_pricing":{"input_price":0.03,"output_price":0.04,"explicit_cache_pricing":{"cache_write_price":0.05,"cache_read_price":0.006},"batch_chat_pricing":{"input_price":0.007,"output_price":0.008}},"explicit_cache_pricing":{"cache_write_price":0.009,"cache_read_price":0},"batch_chat_pricing":{"input_price":0.001,"output_price":0.002},"price_variants":[{"id":"native","canonical":"canonical","aliases":["alias"],"expires_at":"2030-01-01T00:00:00Z","prices":[{"unit":"token","dimension":"input","currency":"USD","sales":1,"cost":0.5},{"unit":"character","dimension":"output","currency":"USD","sales":0,"cost":0.1}]}]}]}`
	var created createChannelRequest
	require.NoError(t, json.Unmarshal([]byte(payload), &created))
	serviceCards := pricingRequestToService(created.ModelPricing, true)
	require.Len(t, serviceCards, 1)
	response := channelToResponse(&service.Channel{Name: created.Name, ModelPricing: serviceCards})
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	var update updateChannelRequest
	require.NoError(t, json.Unmarshal(encoded, &update))
	require.NotNil(t, update.ModelPricing)
	updatedCards := pricingRequestToService(*update.ModelPricing, true)
	require.Equal(t, serviceCards, updatedCards)
	require.Equal(t, 0.006, *updatedCards[0].ThinkingPricing.ExplicitCachePricing.CacheReadPrice)
	require.Equal(t, 0.008, *updatedCards[0].ThinkingPricing.BatchChatPricing.OutputPrice)
	require.Zero(t, *updatedCards[0].ExplicitCachePricing.CacheReadPrice)
	require.Len(t, updatedCards[0].PriceVariants[0].Prices, 2)
}
