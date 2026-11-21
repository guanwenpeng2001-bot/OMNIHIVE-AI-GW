//go:build unit

package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCacheHitPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated job_test database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	var database string
	require.NoError(t, db.QueryRow("select current_database()").Scan(&database))
	require.Equal(t, "job_test", database)
	require.NoError(t, repository.ApplyMigrations(t.Context(), db))
	amount := 0.25
	var userID, keyID, accountID int64
	require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, uuid.NewString()+"@example.test").Scan(&userID))
	require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'receipt') RETURNING id`, userID, uuid.NewString()).Scan(&keyID))
	require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES('receipt','qianwen','apikey') RETURNING id`).Scan(&accountID))
	state := service.PricingStatePriced
	snapshot, err := json.Marshal(service.PricingSnapshot{PricingState: state, Model: "receipt-model", Provider: "qianwen", Mode: "image", Source: "configured", UnitPrice: &amount, Units: 1, RateMultiplier: 1, TimeMultiplier: 1, ReasoningMultiplier: 1, TotalCost: amount, ActualCost: amount, ChargedAmount: service.QuantizeUsageBillingAmount(amount)})
	require.NoError(t, err)
	requestID := uuid.NewString()
	source := &service.UsageSettlementReceipt{
		Key:             service.UsageSettlementKey{RequestID: requestID, UserID: userID, APIKeyID: keyID},
		Command:         service.UsageBillingCommand{RequestID: requestID, APIKeyID: keyID, UserID: userID, AccountID: accountID, AccountType: service.AccountTypeAPIKey, Model: "receipt-model", InputTokens: 100, OutputTokens: 20, BalanceCost: amount, APIKeyQuotaCost: amount, APIKeyRateLimitCost: amount},
		Usage:           service.UsageLog{RequestID: requestID, UserID: userID, APIKeyID: keyID, AccountID: accountID, Model: "receipt-model", InputTokens: 100, OutputTokens: 20, TotalCost: amount, ActualCost: amount, RateMultiplier: 1, PricingState: state, PricingSnapshot: string(snapshot)},
		ResolutionState: "resolved", CompletionState: "complete",
	}
	settlement := service.NewUsageSettlementService(repository.NewUsageSettlementReceiptRepository(db), nil, nil)
	h := NewMCPGrantHandler()
	h.extensions = defaultPluginExtensions()
	for i := range h.extensions {
		h.extensions[i].Enabled = h.extensions[i].Key == "cache-layer"
	}
	h.SetCacheUsageRecorder(settlement)
	router := gin.New()
	sends := 0
	hitID := uuid.NewString()
	router.POST("/v1/chat/completions", h.GatewayExtensionMiddleware(), func(c *gin.Context) {
		id := source.Key.RequestID
		if sends != 0 {
			id = hitID
		}
		plan := service.NewGatewayRequestPlan(service.GatewayRequestSpec{UserID: source.Key.UserID, APIKeyID: source.Key.APIKeyID, Route: c.Request.URL.Path, ExecutionID: id})
		c.Request = c.Request.WithContext(service.WithGatewayRequestPlan(c.Request.Context(), plan))
		if !reserveGatewayRequestBudget(c, &service.APIKey{ID: source.Key.APIKeyID}, "receipt-model", nil) {
			return
		}
		sends++
		require.NoError(t, settlement.Submit(c.Request.Context(), source, nil))
		c.JSON(200, gin.H{"message": "answer", "usage": gin.H{"input_tokens": 100, "output_tokens": 20}})
	})
	for _, want := range []string{"MISS", "HIT"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"receipt-model"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "test")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, want, w.Header().Get("X-OmniHive-Cache"))
	}
	require.Equal(t, 1, sends)
	key := source.Key
	key.RequestID = hitID
	receipt, err := settlement.LoadReceipt(t.Context(), key)
	require.NoError(t, err)
	require.Equal(t, service.UsageSettlementSettled, receipt.State)
	require.True(t, receipt.Usage.BillingEvidence.CacheHit)
	require.Equal(t, 100, receipt.Usage.InputTokens)
	require.Equal(t, 20, receipt.Usage.OutputTokens)
	require.NotNil(t, receipt.UsageLogID)
	log, err := repository.NewUsageLogRepository(nil, db).GetByID(t.Context(), *receipt.UsageLogID)
	require.NoError(t, err)
	require.True(t, log.BillingEvidence.CacheHit)
	require.JSONEq(t, source.Usage.PricingSnapshot, receipt.Usage.PricingSnapshot)
	var balance float64
	require.NoError(t, db.QueryRow("select balance from users where id=$1", key.UserID).Scan(&balance))
	require.Equal(t, 99.5, balance)
}
