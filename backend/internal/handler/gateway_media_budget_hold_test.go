//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// mediaHoldReceiptStub 既实现结算回执仓储，也实现请求预留口——与生产 SQL 仓储同构。
type mediaHoldReceiptStub struct {
	unlimitedTPMRepository
	reservations []service.KeyBudgetReservationCommand
}

func (s *mediaHoldReceiptStub) Persist(context.Context, *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	return nil, nil
}

func (s *mediaHoldReceiptStub) Get(context.Context, service.UsageSettlementKey) (*service.UsageSettlementReceipt, error) {
	return nil, nil
}

func (s *mediaHoldReceiptStub) Settle(context.Context, service.UsageSettlementKey, string) (*service.UsageSettlementResult, error) {
	return nil, nil
}

func (s *mediaHoldReceiptStub) ListPending(context.Context, int64, int) ([]*service.UsageSettlementReceipt, error) {
	return nil, nil
}

func (s *mediaHoldReceiptStub) ReserveKeyBudget(_ context.Context, cmd *service.KeyBudgetReservationCommand) (*service.KeyBudgetReservationOutcome, error) {
	s.reservations = append(s.reservations, *cmd)
	return &service.KeyBudgetReservationOutcome{Reservation: &service.KeyBudgetReservation{
		RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID, UserID: cmd.UserID, HoldAmount: cmd.HoldAmount,
	}}, nil
}

func (s *mediaHoldReceiptStub) ReleaseKeyBudgetReservation(context.Context, int64, string) error {
	return nil
}

func (s *mediaHoldReceiptStub) ReleaseExpiredKeyBudgetReservations(context.Context, time.Time, int) (int, error) {
	return 0, nil
}

// mediaHoldGatewayService 构造带结算能力的网关服务，让入口 hold 真的落到预留口上。
func mediaHoldGatewayService(settlement *service.UsageSettlementService) *service.OpenAIGatewayService {
	cfg := &config.Config{RunMode: config.RunModeSimple}
	return service.ProvideOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		settlement,
	)
}

func newMediaHoldContext(path, body string, key *service.APIKey) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func TestReserveGatewayRequestBudget_MediaEntryHoldsBeforeUpstream(t *testing.T) {
	stub := &mediaHoldReceiptStub{}
	settlement := service.NewUsageSettlementService(stub, nil, nil)
	svc := mediaHoldGatewayService(settlement)

	key := &service.APIKey{
		ID: 7, UserID: 3, Name: "media-key",
		BudgetPolicy: service.APIKeyBudgetPolicy{Mode: service.APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 600},
	}
	group := &service.Group{ID: 3, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}
	key.GroupID = &group.ID
	key.Group = group

	// 媒体请求体没有 token 用量，只有真实的生成参数：hold 用声明上界估算，
	// 结算阶段再按实际计量修正，不会伪造成 token。
	body := `{"model":"gpt-image-2","prompt":"a quiet harbour","n":2,"size":"1024x1024"}`
	c, _ := newMediaHoldContext("/v1/images/generations", body, key)

	finish := beginGatewayRequestPlan(c, svc, key, "gpt-image-2", "images", 1)
	defer finish()

	require.True(t, reserveGatewayRequestBudget(c, key, "gpt-image-2", []byte(body)))
	require.Len(t, stub.reservations, 1, "the media entry must hold budget before calling upstream")
	require.Equal(t, int64(7), stub.reservations[0].APIKeyID)
	require.Equal(t, int64(3), stub.reservations[0].UserID)
	require.NotEmpty(t, stub.reservations[0].RequestID)

	// 媒体请求体没有 token 用量：预留用的是文档化的有界上界，不是把图片参数
	// 折算成 token 数冒充用量（T3-2.2）。同一协议下，不同的媒体 body 得到同一个
	// 上界，进一步证明估算不来自图片参数。
	require.True(t, budgetEstimateIgnoresBody("images"))
	estimate := gatewayRequestBudgetEstimate(service.GatewayRequestPlanFromContext(c.Request.Context()), key, "gpt-image-2", []byte(body))
	require.Zero(t, estimate.InputTokens, "media bodies carry no input tokens")
	require.Zero(t, estimate.OutputTokens, "media bodies carry no output tokens")

	other := gatewayRequestBudgetEstimate(service.GatewayRequestPlanFromContext(c.Request.Context()), key, "gpt-image-2",
		[]byte(`{"model":"gpt-image-2","prompt":"`+strings.Repeat("x", 4000)+`"}`))
	require.Equal(t, estimate.InputTokens, other.InputTokens,
		"a longer image prompt must not be converted into input tokens")

	input, output, multiplier := service.NormalizeBudgetEstimateBounds(estimate)
	require.Equal(t, 1024, input, "documented conservative input bound")
	require.Equal(t, 4096, output, "documented conservative output bound")
	require.InDelta(t, 1, multiplier, 1e-9)

	// 同一逻辑请求内的账号切换/重试复用同一个 hold，不会重复占用。
	require.True(t, reserveGatewayRequestBudget(c, key, "gpt-image-2", []byte(body)))
	require.Len(t, stub.reservations, 1, "failover must reuse the first hold")

	finish()
	require.False(t, c.IsAborted(), "the hold helper never aborts a request on the happy path")
}

func TestReserveGatewayRequestBudget_KeyWithoutPeriodBudgetKeepsPreviousPath(t *testing.T) {
	stub := &mediaHoldReceiptStub{}
	settlement := service.NewUsageSettlementService(stub, nil, nil)
	svc := mediaHoldGatewayService(settlement)

	key := &service.APIKey{ID: 9, UserID: 3, Name: "unbudgeted"}
	c, _ := newMediaHoldContext("/v1/images/generations", `{"model":"gpt-image-2"}`, key)
	finish := beginGatewayRequestPlan(c, svc, key, "gpt-image-2", "images", 1)
	defer finish()

	require.True(t, reserveGatewayRequestBudget(c, key, "gpt-image-2", nil))
	require.Empty(t, stub.reservations, "keys without a period budget keep zero extra work")
}

func TestReserveGatewayRequestBudget_FailsClosedWithoutAPlan(t *testing.T) {
	key := &service.APIKey{
		ID: 7, UserID: 3,
		BudgetPolicy: service.APIKeyBudgetPolicy{Mode: service.APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 600},
	}
	c, recorder := newMediaHoldContext("/v1/images/generations", `{}`, key)

	require.False(t, reserveGatewayRequestBudget(c, key, "gpt-image-2", nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code,
		"a budgeted request without an admission plan must not be forwarded unbudgeted")
}

// TestMediaEntryPointsReserveBudgetBeforeUpstream 锁定接线：每个创建请求计划的
// 媒体/音频/realtime/搜索入口都必须在转发之前调用入口 hold。
func TestMediaEntryPointsReserveBudgetBeforeUpstream(t *testing.T) {
	for _, tc := range []struct {
		file      string
		planCalls int
	}{
		{"openai_images.go", 1},
		{"grok_audio.go", 2},
		{"grok_media.go", 1},
		{"qianwen_realtime_relay.go", 1},
		{"minimax_actions.go", 2},
		{"antigravity_actions.go", 1},
		{"volcengine_actions.go", 1},
		{"hunyuan_actions.go", 1},
		{"qianfan_actions.go", 1},
		{"zhipu_actions.go", 2},
		{"openai_live.go", 1},
		{"gateway_web_search.go", 1},
		{"opencode_google_ingress.go", 1},
	} {
		source := readHandlerSource(t, tc.file)
		require.Equal(t, tc.planCalls, strings.Count(source, "beginGatewayRequestPlan("),
			"%s: one plan per ingress", tc.file)
		require.Equal(t, tc.planCalls, strings.Count(source, "reserveGatewayRequestBudget("),
			"%s: every media/audio/realtime/search ingress holds budget before forwarding", tc.file)
	}
}

// readHandlerSource 读取被测 handler 的源码，用于锁定"入口 hold 已接线"这一事实。
func readHandlerSource(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(name)
	require.NoError(t, err)
	return string(body)
}

func TestBudgetEstimateIgnoresBody_OnlyTokenLessProtocols(t *testing.T) {
	for _, protocol := range []string{"images", "media", "audio", "realtime"} {
		require.True(t, budgetEstimateIgnoresBody(protocol), protocol)
	}
	for _, protocol := range []string{"responses", "messages", "chat_completions", "gemini", "search", "live", "embeddings"} {
		require.False(t, budgetEstimateIgnoresBody(protocol), protocol)
	}
}

func TestGatewayRequestBudgetEstimate_TextProtocolsKeepBodyDerivedBounds(t *testing.T) {
	key := &service.APIKey{ID: 7, UserID: 3}
	c, _ := newMediaHoldContext("/v1/messages", `{}`, key)
	plan := service.NewGatewayRequestPlan(service.GatewayRequestSpec{Protocol: "messages", UserID: 3, APIKeyID: 7})
	c.Request = c.Request.WithContext(service.WithGatewayRequestPlan(c.Request.Context(), plan))

	estimate := gatewayRequestBudgetEstimate(plan, key, "claude-sonnet-4", []byte(
		`{"model":"claude-sonnet-4","max_tokens":2048,"messages":[{"role":"user","content":"hello there budget"}]}`))
	require.Positive(t, estimate.InputTokens)
	require.Equal(t, 2048, estimate.OutputTokens)
}
