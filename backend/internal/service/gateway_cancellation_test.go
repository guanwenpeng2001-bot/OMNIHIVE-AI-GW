package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cancellationWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
	fail   bool
}

func (writer *cancellationWriter) Write(data []byte) (int, error) {
	if strings.Contains(string(data), "message_start") {
		writer.cancel()
		if writer.fail {
			return 0, io.ErrClosedPipe
		}
	}
	return writer.ResponseWriter.Write(data)
}

func (writer *cancellationWriter) WriteString(data string) (int, error) {
	return writer.Write([]byte(data))
}

func TestStreamingCancellationPreservesObservedReceipt(test *testing.T) {
	for _, mode := range []string{"client_cancel", "write_failure", "deadline"} {
		test.Run(mode, func(test *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "deadline" {
				var deadlineCancel context.CancelFunc
				parent, deadlineCancel = context.WithTimeout(parent, 150*time.Millisecond)
				defer deadlineCancel()
			}
			upstreamCanceled := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(response, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\n\n")
				if flusher, ok := response.(http.Flusher); ok {
					flusher.Flush()
				}
				<-request.Context().Done()
				close(upstreamCanceled)
			}))
			defer upstream.Close()
			upstreamContext, release := detachStreamUpstreamContext(parent, true)
			// Existing builders release before send; it must not cancel generation.
			release()
			request, err := http.NewRequestWithContext(upstreamContext, http.MethodPost, upstream.URL, nil)
			require.NoError(test, err)
			response, err := generationHTTP(request, 1, http.DefaultClient.Do)
			require.NoError(test, err)
			defer func() { _ = response.Body.Close() }()
			client, _ := gin.CreateTestContext(httptest.NewRecorder())
			client.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(parent)
			writerCancel := cancel
			if mode != "client_cancel" {
				writerCancel = func() {}
			}
			client.Writer = &cancellationWriter{ResponseWriter: client.Writer, cancel: writerCancel, fail: mode == "write_failure"}
			gateway := newStreamingResponseTestGatewayService()
			result, streamErr := gateway.handleStreamingResponse(parent, response, client, &Account{ID: 1}, time.Now(), "model", "model", false)
			require.Error(test, streamErr)
			require.NotNil(test, result)
			require.True(test, result.clientDisconnect)
			require.Equal(test, 7, result.usage.InputTokens)
			require.Zero(test, result.usage.OutputTokens)
			select {
			case <-upstreamCanceled:
			case <-time.After(time.Second):
				test.Fatal("upstream generation was not canceled")
			}
			partial := partialStreamUsageResult(client, response, result, "model", "model", time.Now(), streamErr)
			require.NotNil(test, partial)
			receipts := &settlementCapture{}
			settlement := NewUsageSettlementService(receipts, nil, nil)
			price := 0.0
			snapshot, err := json.Marshal(PricingSnapshot{PricingState: PricingStateFree, UnitPrice: &price, Units: 1, RateMultiplier: 1})
			require.NoError(test, err)
			billingContext, finishBilling := detachedBillingContext(parent)
			defer finishBilling()
			require.NoError(test, billingContext.Err())
			usage := &UsageLog{RequestID: mode, UserID: 1, APIKeyID: 2, AccountID: 3, Model: "model", InputTokens: partial.Usage.InputTokens}
			params := &postUsageBillingParams{Cost: &CostBreakdown{PricingState: PricingStateFree, PricingSnapshot: string(snapshot)}, User: &User{ID: 1}, APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3, Platform: PlatformAnthropic}, AccountRateMultiplier: 1}
			require.NoError(test, submitUsageSettlement(billingContext, settlement, usage, params, &billingDeps{}, partial.ClientDisconnect))
			require.Equal(test, "interrupted", receipts.receipt.CompletionState)
			require.Equal(test, 7, receipts.receipt.Usage.InputTokens)
			require.Zero(test, receipts.receipt.Usage.OutputTokens)
		})
	}
}

func TestBoundedStreamContextRetainsDeadline(test *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	upstream, headersReceived, release := boundedStreamContext(parent, time.Hour)
	defer release()
	headersReceived()
	select {
	case <-upstream.Done():
		require.True(test, errors.Is(upstream.Err(), context.DeadlineExceeded))
	case <-time.After(time.Second):
		test.Fatal("response headers removed the total deadline")
	}
}

func TestGatewayHeartbeatDoesNotCreateUsage(test *testing.T) {
	client, _ := gin.CreateTestContext(httptest.NewRecorder())
	client.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	response := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(
		": keepalive\n\nevent: ping\ndata: {\"type\":\"ping\"}\n\n",
	))}
	result, err := newStreamingResponseTestGatewayService().handleStreamingResponse(
		client.Request.Context(), response, client, &Account{ID: 1}, time.Now(), "model", "model", false,
	)
	require.Error(test, err, "heartbeats cannot complete a generation")
	require.NotNil(test, result)
	require.Equal(test, ClaudeUsage{}, *result.usage)
	require.Nil(test, result.firstTokenMs)
}

func TestAnthropicHeartbeatFirstSemanticLatency(test *testing.T) {
	for _, route := range []string{"passthrough", "native"} {
		for _, semantic := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/semantic=%v", route, semantic), func(test *testing.T) {
				payload := "event: ping\ndata: {\"type\":\"ping\"}\n\n"
				payload += "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"\"}}\n\n"
				if semantic {
					payload += "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n"
				}
				client, _ := gin.CreateTestContext(httptest.NewRecorder())
				client.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				response := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload))}
				var firstToken *int
				if route == "passthrough" {
					result, err := newStreamingResponseTestGatewayService().handleStreamingResponseAnthropicAPIKeyPassthrough(client.Request.Context(), response, client, &Account{ID: 1}, time.Now(), "model")
					require.Error(test, err)
					require.NotNil(test, result)
					firstToken = result.firstTokenMs
				} else {
					result, err := (&OpenAIGatewayService{}).handleNativeAnthropicStreamingResponse(client.Request.Context(), response, client, &Account{ID: 1}, "model", "model", "model", nil, time.Now())
					require.Error(test, err)
					require.NotNil(test, result)
					firstToken = result.FirstTokenMs
				}
				if semantic {
					require.NotNil(test, firstToken)
				} else {
					require.Nil(test, firstToken)
				}
			})
		}
	}
}
