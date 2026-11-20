//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func cursorIdentityRedisClient(t *testing.T, server *miniredis.Miniredis) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func assertCursorIdentityRestored(t *testing.T, gateway *CursorGatewayService, id string, original cursor.ToolCall) {
	t.Helper()
	history, err := json.Marshal([]map[string]any{{"id": id, "type": "function", "cursor_exec_id": "untrusted", "function": map[string]any{"name": "weather", "arguments": "{}"}}})
	require.NoError(t, err)
	messages := []cursor.ChatMessage{{Role: "assistant", ToolCalls: history}, {Role: "tool", ToolCallID: id, ToolExecID: "untrusted"}}
	require.NoError(t, gateway.restoreToolIDs(context.Background(), messages))
	require.Equal(t, original.CallID, messages[1].ToolCallID)
	require.Equal(t, original.ExecID, messages[1].ToolExecID)
	var calls []map[string]any
	require.NoError(t, json.Unmarshal(messages[0].ToolCalls, &calls))
	require.Equal(t, original.CallID, calls[0]["id"])
	require.Equal(t, original.ExecID, calls[0]["cursor_exec_id"])
}

func TestCursorToolIdentityRedisRestartAndCrossInstance(t *testing.T) {
	server := miniredis.RunT(t)
	first := ProvideCursorGatewayService(nil, nil, cursorIdentityRedisClient(t, server))
	second := ProvideCursorGatewayService(nil, nil, cursorIdentityRedisClient(t, server))
	call := cursor.ToolCall{CallID: "opaque\ncall", ExecID: "opaque\nexec", Arguments: json.RawMessage(`{"large":"not retained"}`)}
	id := first.publicToolID(context.Background(), call)
	require.Len(t, id, 55)
	require.Regexp(t, `^cursor_call_[A-Za-z0-9_-]{43}$`, id)
	require.Len(t, server.Keys(), 1)
	key := server.Keys()[0]
	require.True(t, strings.HasPrefix(key, "cursor:tool-identity:v1:"))
	require.Positive(t, server.TTL(key))
	require.LessOrEqual(t, server.TTL(key), cursor.ToolIdentityTTL)
	raw, err := server.Get(key)
	require.NoError(t, err)
	require.NotContains(t, raw, "not retained")
	t.Run("cross-instance-both-directions", func(t *testing.T) {
		assertCursorIdentityRestored(t, second, id, call)
		other := cursor.ToolCall{CallID: call.CallID, ExecID: "different-execution"}
		otherID := second.publicToolID(context.Background(), other)
		require.NotEqual(t, id, otherID)
		assertCursorIdentityRestored(t, first, otherID, other)
	})
	t.Run("restart-empty-L1", func(t *testing.T) {
		restarted := ProvideCursorGatewayService(nil, nil, cursorIdentityRedisClient(t, server))
		require.Empty(t, restarted.toolIDs)
		assertCursorIdentityRestored(t, restarted, id, call)
	})
	t.Run("L1-eviction-falls-back-to-Redis", func(t *testing.T) {
		for i := range cursorToolIDLimit {
			first.cacheToolIdentity(fmt.Sprint(i), cursor.ToolIdentity{CallID: "filler", ExecID: "filler", ExpiresAt: time.Now().Add(cursor.ToolIdentityTTL)})
		}
		require.NotContains(t, first.toolIDs, id)
		assertCursorIdentityRestored(t, first, id, call)
		require.Len(t, first.toolIDs, cursorToolIDLimit)
	})
}

func TestCursorToolIdentityRedisTTLReturns400ThreeProtocols(t *testing.T) {
	server := miniredis.RunT(t)
	client := cursorIdentityRedisClient(t, server)
	first := ProvideCursorGatewayService(nil, nil, client)
	id := first.publicToolID(context.Background(), cursor.ToolCall{CallID: "call\nopaque", ExecID: "exec"})
	server.FastForward(cursor.ToolIdentityTTL + time.Second)
	require.Empty(t, server.Keys())
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			gateway := ProvideCursorGatewayService(nil, nil, client)
			writer := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(writer)
			account := &Account{Platform: PlatformCursor, Type: AccountTypeAPIKey}
			body := []byte(strings.ReplaceAll(string(cursorMCPRequest(protocol, false, true)), "call-1", id))
			_, err := callCursorProtocol(gateway, requestContext, account, body, protocol)
			require.ErrorContains(t, err, "identity expired")
			require.Equal(t, http.StatusBadRequest, writer.Code)
			require.Contains(t, writer.Body.String(), "identity expired")
		})
	}
}

func TestCursorToolIdentityL1TTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gateway := NewCursorGatewayService(nil, nil)
		call := cursor.ToolCall{CallID: "call\nopaque", ExecID: "exec"}
		id := gateway.publicToolID(context.Background(), call)
		assertCursorIdentityRestored(t, gateway, id, call)
		// synctest advances virtual time; no wall-clock sleep or polling.
		time.Sleep(cursor.ToolIdentityTTL)
		require.ErrorContains(t, gateway.restoreToolIDs(context.Background(), []cursor.ChatMessage{{Role: "tool", ToolCallID: id}}), "identity expired")
	})
}

func TestCursorToolIdentityRedisUnavailableWarnsOnceAndRecovers(t *testing.T) {
	server := miniredis.RunT(t)
	client := cursorIdentityRedisClient(t, server)
	gateway := ProvideCursorGatewayService(nil, nil, client)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	server.SetError("ERR synthetic unavailable")
	for i := range 3 {
		call := cursor.ToolCall{CallID: fmt.Sprintf("call\n%d", i), ExecID: "secret-execution"}
		id := gateway.publicToolID(context.Background(), call)
		assertCursorIdentityRestored(t, gateway, id, call)
		require.ErrorContains(t, gateway.restoreToolIDs(context.Background(), []cursor.ChatMessage{{Role: "tool", ToolCallID: cursor.NormalizeToolCallID("missing\n")}}), "identity expired")
	}
	require.Equal(t, 1, strings.Count(logs.String(), "tool identity Redis unavailable"))
	require.NotContains(t, logs.String(), "secret-execution")
	require.NotContains(t, logs.String(), "synthetic unavailable")
	server.SetError("")
	call := cursor.ToolCall{CallID: "recovered\ncall", ExecID: "exec"}
	id := gateway.publicToolID(context.Background(), call)
	assertCursorIdentityRestored(t, ProvideCursorGatewayService(nil, nil, client), id, call)
	require.Equal(t, 1, strings.Count(logs.String(), "tool identity Redis unavailable"))
}
