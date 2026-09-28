package handler

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestShouldRecordZhipuActionUsage(t *testing.T) {
	tests := []struct {
		name     string
		endpoint service.ZhipuActionEndpoint
		result   *service.OpenAIForwardResult
		want     bool
	}{
		{name: "image with output", endpoint: service.ZhipuEndpointImagesGenerations, result: &service.OpenAIForwardResult{ImageCount: 1}, want: true},
		{name: "image without output", endpoint: service.ZhipuEndpointImagesGenerations, result: &service.OpenAIForwardResult{}, want: false},
		{name: "embeddings with tokens", endpoint: service.ZhipuEndpointEmbeddings, result: &service.OpenAIForwardResult{Usage: service.OpenAIUsage{InputTokens: 7}}, want: true},
		{name: "embeddings zero tokens", endpoint: service.ZhipuEndpointEmbeddings, result: &service.OpenAIForwardResult{}, want: false},
		{name: "rerank with tokens", endpoint: service.ZhipuEndpointRerank, result: &service.OpenAIForwardResult{Usage: service.OpenAIUsage{InputTokens: 72}}, want: true},
		{name: "tts with audio units", endpoint: service.ZhipuEndpointAudioSpeech, result: &service.OpenAIForwardResult{AudioUsage: &service.AudioUsage{Mode: "tts"}}, want: true},
		{name: "stt with audio units", endpoint: service.ZhipuEndpointAudioTranscriptions, result: &service.OpenAIForwardResult{AudioUsage: &service.AudioUsage{Mode: "stt"}}, want: true},
		{name: "search with call", endpoint: service.ZhipuEndpointWebSearch, result: &service.OpenAIForwardResult{WebSearchCalls: 1}, want: true},
		{name: "search without call", endpoint: service.ZhipuEndpointWebSearch, result: &service.OpenAIForwardResult{}, want: false},
		{name: "video create defers billing", endpoint: service.ZhipuEndpointVideosGenerations, result: &service.OpenAIForwardResult{ResponseID: "task-1"}, want: false},
		{name: "video status claims separately", endpoint: service.ZhipuEndpointAsyncResult, result: &service.OpenAIForwardResult{VideoCount: 1}, want: false},
		{name: "video content claims separately", endpoint: service.ZhipuEndpointVideoContent, result: &service.OpenAIForwardResult{VideoCount: 1}, want: false},
		{name: "voice clone has no billable units", endpoint: service.ZhipuEndpointVoiceClone, result: &service.OpenAIForwardResult{}, want: false},
		{name: "voice list has no billable units", endpoint: service.ZhipuEndpointVoiceList, result: &service.OpenAIForwardResult{}, want: false},
		{name: "voice delete has no billable units", endpoint: service.ZhipuEndpointVoiceDelete, result: &service.OpenAIForwardResult{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.False(t, shouldRecordZhipuActionUsage(tt.endpoint, "model", nil), "nil result must never bill")
			require.Equal(t, tt.want, shouldRecordZhipuActionUsage(tt.endpoint, "model", tt.result))
		})
	}
}

func TestZhipuActionRequiredCapability(t *testing.T) {
	require.Equal(t, service.OpenAIEndpointCapabilityEmbeddings, zhipuActionRequiredCapability(service.ZhipuEndpointEmbeddings))
	for _, endpoint := range []service.ZhipuActionEndpoint{
		service.ZhipuEndpointImagesGenerations,
		service.ZhipuEndpointVideosGenerations,
		service.ZhipuEndpointAsyncResult,
		service.ZhipuEndpointVideoContent,
		service.ZhipuEndpointRerank,
		service.ZhipuEndpointAudioSpeech,
		service.ZhipuEndpointAudioTranscriptions,
		service.ZhipuEndpointVoiceClone,
		service.ZhipuEndpointWebSearch,
	} {
		require.Empty(t, zhipuActionRequiredCapability(endpoint), "endpoint %s stays model-evidence gated", endpoint)
	}
}

// zhipuHandlerCacheStub provides pending-billing and claim state for the
// completion-billing handler tests.
type zhipuHandlerCacheStub struct {
	mu              sync.Mutex
	sessionBindings map[string]int64
	pendingBilling  map[string][]byte
	claims          map[string]bool
}

func newZhipuHandlerCacheStub() *zhipuHandlerCacheStub {
	return &zhipuHandlerCacheStub{sessionBindings: map[string]int64{}, pendingBilling: map[string][]byte{}, claims: map[string]bool{}}
}

func (s *zhipuHandlerCacheStub) GetSessionAccountID(_ context.Context, _ int64, sessionHash string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.sessionBindings[sessionHash]; ok {
		return id, nil
	}
	return 0, service.ErrStickySessionNotFound
}
func (s *zhipuHandlerCacheStub) SetSessionAccountID(_ context.Context, _ int64, sessionHash string, accountID int64, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionBindings[sessionHash] = accountID
	return nil
}
func (s *zhipuHandlerCacheStub) RefreshSessionTTL(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}
func (s *zhipuHandlerCacheStub) DeleteSessionAccountID(_ context.Context, _ int64, _ string) error {
	return nil
}
func (s *zhipuHandlerCacheStub) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingBilling[key] = payload
	return nil
}
func (s *zhipuHandlerCacheStub) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingBilling[key], nil
}
func (s *zhipuHandlerCacheStub) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claims[key] {
		return false, nil
	}
	s.claims[key] = true
	return true, nil
}
func (s *zhipuHandlerCacheStub) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.claims, key)
	return nil
}
func (s *zhipuHandlerCacheStub) SetReasoningContent(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}
func (s *zhipuHandlerCacheStub) GetReasoningContent(_ context.Context, _ string) (string, error) {
	return "", service.ErrReasoningContentNotFound
}

func newZhipuVideoBillingTestService(t *testing.T) *service.OpenAIGatewayService {
	t.Helper()
	return service.ProvideOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil,
		newZhipuHandlerCacheStub(),
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
}

func TestPrepareZhipuVideoCompletionBilling(t *testing.T) {
	gatewayService := newZhipuVideoBillingTestService(t)
	h := &OpenAIGatewayHandler{gatewayService: gatewayService}
	apiKey := &service.APIKey{ID: 200, UserID: 100}
	subject := middleware2.AuthSubject{UserID: 100}
	ctx := context.Background()

	pending := service.BuildZhipuVideoPendingBilling(
		service.ZhipuActionRequestInfo{Model: "cogvideox-3", Size: "1920x1080", DurationSeconds: 10},
		"cogvideox-3", "cogvideox-3", "public-video", service.ZhipuVideoPendingCreatedAtNow(),
	)
	require.NoError(t, gatewayService.StoreZhipuVideoPendingBilling(ctx, "task-1", 100, 200, pending))

	t.Run("claims once for completed task", func(t *testing.T) {
		statusResult := &service.OpenAIForwardResult{ResponseID: "task-1", VideoCount: 1, Model: "cogvideox-3"}
		billResult := h.prepareZhipuVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-1", statusResult)
		require.NotNil(t, billResult)
		require.Equal(t, 1, billResult.VideoCount)
		require.Equal(t, 10, billResult.VideoDurationSeconds)
		require.Equal(t, service.VideoBillingResolution1080P, billResult.VideoResolution)
		require.Equal(t, "zhipu-video:task-1", billResult.RequestID)

		// Second poll for the same task: already claimed, no double billing.
		require.Nil(t, h.prepareZhipuVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-1",
			&service.OpenAIForwardResult{ResponseID: "task-1", VideoCount: 1}))
	})

	t.Run("unfinished task never bills", func(t *testing.T) {
		require.Nil(t, h.prepareZhipuVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-1",
			&service.OpenAIForwardResult{ResponseID: "task-1"}))
	})

	t.Run("missing create snapshot fails closed", func(t *testing.T) {
		require.Nil(t, h.prepareZhipuVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-404",
			&service.OpenAIForwardResult{ResponseID: "task-404", VideoCount: 1}))
	})

	t.Run("nil inputs never bill", func(t *testing.T) {
		require.Nil(t, h.prepareZhipuVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-1", nil))
		empty := &OpenAIGatewayHandler{}
		require.Nil(t, empty.prepareZhipuVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-1",
			&service.OpenAIForwardResult{VideoCount: 1}))
	})
}

func TestZhipuRealtimeBillingResult(t *testing.T) {
	require.Nil(t, zhipuRealtimeBillingResult("glm-realtime", time.Minute, false), "no audio observed means no billable session")
	require.Nil(t, zhipuRealtimeBillingResult("glm-realtime", 0, true))

	result := zhipuRealtimeBillingResult("glm-realtime-flash", 90*time.Second, true)
	require.NotNil(t, result)
	require.NotNil(t, result.AudioUsage)
	require.Equal(t, "realtime", result.AudioUsage.Mode)
	require.InDelta(t, 1.5, result.AudioUsage.DurationOrUnits, 1e-9)
	require.True(t, strings.HasPrefix(result.RequestID, "zhipu_realtime:"))
}

func TestIsExpectedZhipuRealtimeClose(t *testing.T) {
	require.True(t, isExpectedZhipuRealtimeClose(nil))
}
