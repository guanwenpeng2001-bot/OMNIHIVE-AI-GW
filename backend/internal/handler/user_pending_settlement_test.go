//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
	middleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

type pendingUsageRepository struct{ service.UsageLogRepository }

func (*pendingUsageRepository) ListUserPendingSettlement(_ context.Context, userID int64, _ usagestats.UsageLogFilters, cursor service.UserPendingSettlementCursor) (*service.UserPendingSettlementPage, error) {
	page := &service.UserPendingSettlementPage{}
	if userID == 7 {
		page.Items = []*service.UserPendingSettlementItem{{UsageSettlementReceipt: &service.UsageSettlementReceipt{ID: 11, Key: service.UsageSettlementKey{UserID: 7, RequestID: "owned"}, Usage: service.UsageLog{Model: "must-not-fill", PricingState: service.PricingStateUnpriced}}}}
		page.Total = 1
	}
	return page, nil
}
func TestUserPendingSettlementHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewUsageHandler(service.NewUsageService(&pendingUsageRepository{}, nil, nil, nil), nil, nil, nil)
	for _, userID := range []int64{7, 8, 0} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest("GET", "/api/v1/usage/pending-settlement", nil)
		if userID > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		}
		handler.ListPendingSettlement(c)
		if userID == 0 {
			require.Equal(t, 401, recorder.Code)
			continue
		}
		require.Equal(t, 200, recorder.Code)
		var body struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
		if userID == 8 {
			require.Empty(t, body.Data.Items)
			continue
		}
		require.Len(t, body.Data.Items, 1)
		item := body.Data.Items[0]
		require.Contains(t, item, "amount")
		require.Nil(t, item["amount"])
		require.Equal(t, "pending_pricing", item["amount_state"])
		require.Equal(t, "", item["requested_model"])
		require.Nil(t, item["upstream_response_model"])
		require.Equal(t, "receipt", item["source"])
	}
	for _, query := range []string{"cursor=-1", "cursor=bad", "page_size=0", "source=bad"} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest("GET", "/api/v1/usage/pending-settlement?"+query, nil)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		handler.ListPendingSettlement(c)
		require.Equal(t, 400, recorder.Code)
	}
}
