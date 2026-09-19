//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Embedded nil interfaces make ANY unexpected repository write panic. Only
// the reads needed by the real resolver are implemented; no PostgreSQL claim.
type pricingPreviewChannelReads struct {
	service.ChannelRepository
	channel service.Channel
}

func (repo *pricingPreviewChannelReads) ListAll(context.Context) ([]service.Channel, error) {
	return []service.Channel{repo.channel}, nil
}
func (repo *pricingPreviewChannelReads) GetGroupPlatforms(context.Context, []int64) (map[int64]string, error) {
	return map[int64]string{7: service.PlatformComposite}, nil
}

type pricingPreviewGroupReads struct {
	service.GroupRepository
	group service.Group
}

func (repo *pricingPreviewGroupReads) GetByID(context.Context, int64) (*service.Group, error) {
	return &repo.group, nil
}

func TestChannelPricingPreviewHasNoWrites(test *testing.T) {
	gin.SetMode(gin.TestMode)
	dataDir := test.TempDir()
	cfg := &config.Config{Pricing: config.PricingConfig{DataDir: dataDir}}
	pricing := service.NewPricingService(cfg, nil)
	groups := &pricingPreviewGroupReads{group: service.Group{ID: 7, Platform: service.PlatformComposite, RateMultiplier: 2}}
	channels := &pricingPreviewChannelReads{channel: service.Channel{ID: 8, Status: service.StatusActive, GroupIDs: []int64{7}, ModelPricing: []service.ChannelModelPricing{{Platform: "openai", Models: []string{"model"}, BillingMode: service.BillingModeToken, InputPrice: float64Ptr(7e-6), OutputPrice: float64Ptr(9e-6)}}}}
	channelBefore, err := json.Marshal(channels.channel)
	require.NoError(test, err)
	groupBefore, err := json.Marshal(groups.group)
	require.NoError(test, err)
	capabilityBefore := pricing.CapabilitySnapshot()
	handler := NewChannelHandler(service.NewChannelService(channels, groups, nil, pricing, nil), service.NewBillingService(cfg, pricing), pricing)
	router := gin.New()
	router.POST("/channels/pricing/preview", handler.PreviewPricing)
	for _, scenario := range []struct {
		name, extra string
		status      int
	}{
		{"quote read", "", http.StatusOK},
		{"candidate sample read", `,"group_id":7,"input_tokens":1000,"output_tokens":500,"candidate":{"model":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.000002,"output_cost_per_token":0.000004}}`, http.StatusOK},
		{"source requires group", `,"candidate":{}`, http.StatusBadRequest},
		{"invalid candidate", `,"group_id":7,"candidate":[]`, http.StatusBadRequest},
		{"negative usage", `,"input_tokens":-1`, http.StatusBadRequest},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/channels/pricing/preview", strings.NewReader(`{"model":"model","profile":"openai","task_type":"text_generation","pricing":{"platform":"openai","models":["model"],"billing_mode":"token","input_price":0.000003}`+scenario.extra+`}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(test, scenario.status, recorder.Code, recorder.Body.String())
			if scenario.name == "candidate sample read" {
				var envelope struct {
					Data service.ChannelPricingPreview `json:"data"`
				}
				require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
				require.Len(test, envelope.Data.Impacts, 1)
				impact := envelope.Data.Impacts[0]
				require.True(test, impact.Comparable)
				require.InDelta(test, 0.023, impact.Before.ActualCost, 1e-12)
				require.Equal(test, impact.Before.ActualCost, impact.After.ActualCost)
				require.False(test, impact.ChargeChanged)
			}
		})
	}
	channelAfter, err := json.Marshal(channels.channel)
	require.NoError(test, err)
	require.Equal(test, channelBefore, channelAfter)
	groupAfter, err := json.Marshal(groups.group)
	require.NoError(test, err)
	require.Equal(test, groupBefore, groupAfter)
	require.Same(test, capabilityBefore, pricing.CapabilitySnapshot())
	require.Nil(test, pricing.GetModelPricing("model"))
	files, err := os.ReadDir(dataDir)
	require.NoError(test, err)
	require.Empty(test, files)
}
