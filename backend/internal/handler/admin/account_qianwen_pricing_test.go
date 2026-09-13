//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type qianwenPricingAdminStub struct {
	service.AdminService
	snapshot    *service.QianwenPricingResult
	groupWrites int
}

func (s *qianwenPricingAdminStub) GetAccount(context.Context, int64) (*service.Account, error) {
	return &service.Account{ID: 19, Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey}, nil
}
func (s *qianwenPricingAdminStub) GetGroup(context.Context, int64) (*service.Group, error) {
	return &service.Group{ID: 7, Platform: service.PlatformQianwen}, nil
}
func (s *qianwenPricingAdminStub) ListAccounts(context.Context, int, int, string, string, string, string, int64, string, string, string) ([]service.Account, int64, error) {
	return []service.Account{{ID: 19}}, 1, nil
}
func (s *qianwenPricingAdminStub) UpdateAccountExtra(_ context.Context, _ int64, extra map[string]any) error {
	s.snapshot = extra["qianwen_pricing_sync"].(*service.QianwenPricingResult)
	return nil
}
func (s *qianwenPricingAdminStub) UpdateGroup(context.Context, int64, *service.UpdateGroupInput) (*service.Group, error) {
	s.groupWrites++
	return nil, nil
}

func TestSyncQianwenPricingUnsetFXWritesNoGroupPrices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	admin := &qianwenPricingAdminStub{}
	h := &AccountHandler{adminService: admin}
	router := gin.New()
	router.POST("/accounts/:id/pricing/sync-qianwen", h.SyncQianwenPricing)
	req := httptest.NewRequest(http.MethodPost, "/accounts/19/pricing/sync-qianwen", strings.NewReader(`{"region":"asia-pacific-china","group_id":7}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Zero(t, admin.groupWrites)
	require.NotNil(t, admin.snapshot)
	require.Equal(t, "unsupported", admin.snapshot.Status)
	require.Contains(t, admin.snapshot.Reason, "未配置汇率")
	require.Zero(t, admin.snapshot.Imported)
	require.Contains(t, w.Body.String(), `"written":0`)
}

func TestMergeQianwenPricingPreservesOtherCards(t *testing.T) {
	in, out := 1e-6, 2e-6
	old := []service.ChannelModelPricing{{Platform: service.PlatformQianwen, NativePricingSource: service.QianwenPricingSource, Models: []string{"qwen3.8-max", "other"}, InputPrice: &out}, {Models: []string{"*"}, OutputPrice: &out}}
	result := &service.QianwenPricingResult{Models: []service.QianwenPricingModelResult{{QianwenModelPrices: service.QianwenModelPrices{Model: "qwen3.8-max"}, Pricing: &service.ChannelModelPricing{Models: []string{"qwen3.8-max"}, InputPrice: &in, OutputPrice: &out}}}}
	cards := mergeQianwenPricing(old, result)
	require.Len(t, cards, 3)
	require.Equal(t, []string{"qwen3.8-max"}, cards[0].Models)
	require.Equal(t, []string{"other"}, cards[1].Models)
	require.Equal(t, []string{"*"}, cards[2].Models)
	require.Equal(t, []string{"qwen3.8-max", "other"}, old[0].Models)
}

func TestQianwenImportProtectsOperatorAndOtherPlatformPrices(t *testing.T) {
	price, newer := 2e-6, 5e-6
	existing := []service.ChannelModelPricing{
		{Platform: service.PlatformCommandCode, Models: []string{"shared"}, InputPrice: &price},
		{Platform: service.PlatformQianwen, Models: []string{"manual"}, InputPrice: &price},
	}
	snapshot := &service.QianwenPricingResult{}
	for _, model := range []string{"shared", "manual", "conditional"} {
		snapshot.Models = append(snapshot.Models, service.QianwenPricingModelResult{QianwenModelPrices: service.QianwenModelPrices{Model: model}, Pricing: &service.ChannelModelPricing{Platform: service.PlatformQianwen, NativePricingSource: service.QianwenPricingSource, Models: []string{model}, InputPrice: &newer, ThinkingPricing: &service.QianwenModePricing{OutputPrice: &newer}}})
	}
	cards := mergeQianwenPricing(existing, snapshot)
	require.Len(t, cards, 4)
	require.Equal(t, "shared", cards[0].Models[0])
	require.NotNil(t, cards[1].ThinkingPricing)
	require.Equal(t, existing[0], cards[2])
	require.Equal(t, existing[1], cards[3])
	require.Equal(t, cards, mergeQianwenPricing(cards, snapshot), "repeated apply must be idempotent")
}

func TestQianwenImportSelectsActiveTargetRoutes(t *testing.T) {
	models := []service.QianwenPricingModelResult{}
	for _, name := range []string{"qwen-plus", "qwen-turbo", "other", "disabled"} {
		models = append(models, service.QianwenPricingModelResult{QianwenModelPrices: service.QianwenModelPrices{Model: name}})
	}
	routes := []service.CompositeModelRoute{
		{PublicModel: "alias", UpstreamModel: "qwen-plus", Enabled: true, MatchType: "exact", TargetPlatform: service.PlatformQianwen},
		{PublicModel: "qwen-turbo", Enabled: true, MatchType: "exact", TargetPlatform: service.PlatformQianwen},
		{PublicModel: "other", Enabled: true, MatchType: "exact", TargetPlatform: service.PlatformCommandCode},
		{PublicModel: "disabled", MatchType: "exact", TargetPlatform: service.PlatformQianwen},
	}
	selected := routedQianwenPrices(models, routes)
	require.Equal(t, models[:2], selected)
}
