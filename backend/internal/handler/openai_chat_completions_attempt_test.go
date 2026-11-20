//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNativeChatResponsesMessagesAttemptParity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		committed bool
		wantHits  []int64
	}{
		{"429", 429, false, []int64{801, 802}},
		{"502", 502, false, []int64{801, 802}},
		{"committed", 429, true, []int64{801}},
		{"400", 400, false, []int64{801}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var baseline []attempt.AttemptDecision
			for i, ingress := range []struct{ path, body string }{
				{"responses", `{"model":"grok","input":"hello"}`},
				{"messages", `{"model":"grok","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`},
				{"chat/completions", `{"model":"grok","messages":[{"role":"user","content":"hello"}]}`},
			} {
				_, _, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "first_429")
				upstream.rateLimitIDs = nil
				upstream.failureStatus = map[int64]int{801: tc.status}
				if tc.committed {
					upstream.beforeReply = func(ctx context.Context, id int64) error {
						service.AttemptBudgetFromContext(ctx).Response().Semantic()
						return nil
					}
				}
				log := make(attemptDecisionLog, 8)
				r := httptest.NewRequest(http.MethodPost, "/openai/v1/"+ingress.path, strings.NewReader(ingress.body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r.WithContext(attempt.WithRecorder(r.Context(), log)))
				cleanup()
				require.Equal(t, tc.wantHits, upstream.accountHits(), ingress.path)
				close(log)
				var decisions []attempt.AttemptDecision
				for d := range log {
					d.Deadline = time.Time{}
					decisions = append(decisions, d)
				}
				if tc.status != 400 {
					require.Len(t, decisions, 1, ingress.path)
				}
				if i == 0 {
					baseline = decisions
				} else {
					require.Equal(t, baseline, decisions, ingress.path)
				}
				if tc.status == 400 {
					require.Equal(t, 400, w.Code, ingress.path)
				}
			}
		})
	}
}

func TestNativeChatRoutingPolicyDeniesBeforeUpstreamSend(t *testing.T) {
	_, _, upstream, inner, cleanup := newGrokCredentialFailoverHandler(t, "first_429")
	defer cleanup()
	router := gin.New()
	router.Use(NewMCPGrantHandler().GatewayExtensionMiddleware())
	router.POST("/openai/v1/chat/completions", func(c *gin.Context) { inner.ServeHTTP(c.Writer, c.Request) })
	for _, tc := range []struct {
		value  string
		status int
		reason string
	}{
		{"EU", http.StatusForbidden, "ROUTING_POLICY_DENIED"},
		{"EU,", http.StatusBadRequest, "INVALID_ROUTING_CONSTRAINT"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(`{"model":"grok","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-OmniHive-Regions", tc.value)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, tc.status, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), tc.reason)
		require.Empty(t, upstream.accountHits())
	}
}
