//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cursorPolishDriver struct{ cursorDriverStub }

func (cursorPolishDriver) AvailableModels(context.Context, cursor.Credentials) ([]cursor.AvailableModel, error) {
	return cursor.FallbackModels(), nil
}

func TestCursorNativeConcurrencyConfiguredPeakAndProbe(t *testing.T) {
	for _, limit := range []int{0, 1, 3} {
		for _, probeFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("limit=%d/probe=%t", limit, probeFirst), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					gateway, account, _ := cursorTestGateway()
					account.Concurrency = limit
					capacity := max(1, limit)
					releaseRuns := make(chan struct{})
					var active, peak, completed atomic.Int32
					gateway.native = cursorPolishDriver{cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, _ []cursor.ChatMessage, _ string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
						count := active.Add(1)
						defer active.Add(-1)
						for prev := peak.Load(); count > prev; prev = peak.Load() {
							if peak.CompareAndSwap(prev, count) {
								break
							}
						}
						<-releaseRuns
						return cursor.TokenUsage{InputTokens: 1, OutputTokens: 1}, emit(cursor.StreamEvent{Type: "text", Text: "ok"})
					}}}
					failures := make(chan error, 13)
					total := 12
					if probeFirst {
						total++
						go func() {
							c, _ := gin.CreateTestContext(httptest.NewRecorder())
							c.Request = httptest.NewRequest("POST", "/test", nil)
							failures <- (&AccountTestService{cursorGatewayService: gateway}).testCursorAccountConnection(c, account, "", "")
							completed.Add(1)
						}()
						synctest.Wait()
						require.Equal(t, int32(1), active.Load())
					}
					for range 12 {
						go func() {
							c, _ := gin.CreateTestContext(httptest.NewRecorder())
							_, err := gateway.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"default","messages":[{"role":"user","content":"hi"}]}`))
							failures <- err
							completed.Add(1)
						}()
					}
					// Every goroutine is durably blocked: admitted Runs at releaseRuns and
					// excess callers at the semaphore. No timing guesses or sleeps.
					synctest.Wait()
					require.Equal(t, int32(capacity), active.Load())
					require.Equal(t, int32(capacity), peak.Load())
					require.Zero(t, completed.Load())
					close(releaseRuns)
					synctest.Wait()
					require.Equal(t, int32(total), completed.Load())
					require.Equal(t, int32(capacity), peak.Load())
					require.Zero(t, active.Load())
					for range total {
						require.NoError(t, <-failures)
					}
				})
			})
		}
	}
}

func TestCursorNativeConcurrencyCancelResizeAndAccounts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gateway, account, _ := cursorTestGateway()
		release, err := gateway.acquireNativeSlot(context.Background(), account)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { _, err := gateway.acquireNativeSlot(ctx, account); result <- err }()
		synctest.Wait()
		cancel()
		synctest.Wait()
		require.ErrorIs(t, <-result, context.Canceled)
		// A new account snapshot raises the limit while the first slot is occupied.
		updated := *account
		updated.Concurrency = 2
		second, err := gateway.acquireNativeSlot(context.Background(), &updated)
		require.NoError(t, err)
		another := *account
		another.ID++
		independent, err := gateway.acquireNativeSlot(context.Background(), &another)
		require.NoError(t, err)
		independent()
		// Lowering the limit must retain both existing occupants until they leave.
		acquired := make(chan func(), 1)
		go func() {
			slot, err := gateway.acquireNativeSlot(context.Background(), account)
			if err == nil {
				acquired <- slot
			}
		}()
		synctest.Wait()
		require.Empty(t, acquired)
		second()
		synctest.Wait()
		require.Empty(t, acquired)
		release()
		synctest.Wait()
		require.Len(t, acquired, 1)
		(<-acquired)()
	})
}

func TestCursorToolIdentityMappingStableDistinctAndBounded(t *testing.T) {
	gateway := NewCursorGatewayService(nil, nil)
	first := cursor.ToolCall{CallID: "call-raw\nfc_1", ExecID: "exec-original\n1"}
	id := gateway.publicToolID(context.Background(), first)
	require.Equal(t, id, gateway.publicToolID(context.Background(), first))
	require.Len(t, gateway.toolIDs, 1)
	second := first
	second.ExecID = "exec-original\n2"
	require.NotEqual(t, id, gateway.publicToolID(context.Background(), second))
	messages := []cursor.ChatMessage{{Role: "tool", Content: "result", ToolCallID: id, ToolExecID: "untrusted"}}
	require.NoError(t, gateway.restoreToolIDs(context.Background(), messages))
	require.Equal(t, first.CallID, messages[0].ToolCallID)
	require.Equal(t, first.ExecID, messages[0].ToolExecID)
	require.Empty(t, gateway.toolIDs[id].Arguments)
	for i := range cursorToolIDLimit {
		gateway.publicToolID(context.Background(), cursor.ToolCall{CallID: fmt.Sprintf("call\n%d", i), ExecID: fmt.Sprintf("exec-%d", i)})
	}
	require.Len(t, gateway.toolIDs, cursorToolIDLimit)
	require.Len(t, gateway.toolIDOrder, cursorToolIDLimit)
	require.ErrorContains(t, gateway.restoreToolIDs(context.Background(), []cursor.ChatMessage{{Role: "tool", ToolCallID: id}}), "identity expired")
	// Plain legacy IDs remain valid, and clients cannot inject execution metadata.
	plain := []cursor.ChatMessage{{Role: "tool", ToolCallID: "call-legacy", ToolExecID: "untrusted"}}
	require.NoError(t, gateway.restoreToolIDs(context.Background(), plain))
	require.Equal(t, "call-legacy", plain[0].ToolCallID)
	require.Empty(t, plain[0].ToolExecID)
}

func TestCursorNativeConcurrencyReleasesOnFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gateway, account, _ := cursorTestGateway()
		var attempts atomic.Int32
		gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, _ []cursor.ChatMessage, _ string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
			if attempts.Add(1) == 1 {
				return cursor.TokenUsage{}, fmt.Errorf("synthetic Run failure")
			}
			return cursor.TokenUsage{InputTokens: 1, OutputTokens: 1}, emit(cursor.StreamEvent{Text: "ok"})
		}}
		for i := range 2 {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			_, err := gateway.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"default","messages":[{"role":"user","content":"hi"}]}`))
			if i == 0 {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		}
		require.Equal(t, int32(2), attempts.Load())
	})
}
