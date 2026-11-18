//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type attemptDecisionLog chan attempt.AttemptDecision

func (ch attemptDecisionLog) RecordAttemptDecision(_ context.Context, d attempt.AttemptDecision) {
	ch <- d
}

func TestAttemptDecisionsAcrossGatewayLoops(t *testing.T) {
	for _, protocol := range []string{"responses", "messages", "websocket"} {
		var fixture struct {
			Path     string
			Request  json.RawMessage
			Terminal string
			Failures []struct {
				Mode  string
				Event string
				attempt.AttemptDecision
			}
		}
		data, err := os.ReadFile("../../../contracts/protocol/lifecycle/" + protocol + ".json")
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &fixture))
		for _, failure := range fixture.Failures {
			t.Run(protocol+"/"+failure.Mode+"/"+failure.Event, func(t *testing.T) {
				_, _, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, failure.Mode)
				defer cleanup()
				upstream.beforeReply = func(ctx context.Context, id int64) error {
					if id != 801 {
						return nil
					}
					r := service.AttemptBudgetFromContext(ctx).Response()
					switch failure.Event {
					case "deadline":
						<-ctx.Done()
					case "lost_ack":
						return io.ErrUnexpectedEOF
					case "heartbeat":
						r.Heartbeat()
					case "semantic":
						r.Semantic()
					case "terminal_failed", "terminal_twice":
						if r.BeginTerminal() {
							r.FinishTerminal(failure.Event == "terminal_twice")
						}
						if r.BeginTerminal() {
							t.Error("second terminal acquired")
						}
					}
					return nil
				}
				log := make(attemptDecisionLog, 8)
				h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					ctx := r.Context()
					if failure.Event == "deadline" {
						root, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
						defer cancel()
						if protocol == "websocket" {
							ctx = service.WithStreamAttemptBudget(root)
						} else {
							var cancelBudget context.CancelFunc
							ctx, cancelBudget = service.WithAttemptBudget(root, 4, time.Minute)
							defer cancelBudget()
						}
						ctx = context.WithoutCancel(ctx)
					}
					router.ServeHTTP(w, r.WithContext(attempt.WithRecorder(ctx, log)))
				})
				if protocol == "websocket" {
					srv := httptest.NewServer(h)
					defer srv.Close()
					ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
					defer cancel()
					conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+fixture.Path, nil)
					require.NoError(t, err)
					defer conn.CloseNow()
					require.NoError(t, conn.Write(ctx, websocket.MessageText, fixture.Request))
					_, payload, err := conn.Read(ctx)
					if failure.Action == attempt.Stop {
						require.Equal(t, websocket.StatusTryAgainLater, websocket.CloseStatus(err))
					} else {
						require.NoError(t, err)
						require.Contains(t, string(payload), fixture.Terminal)
					}
				} else {
					w := httptest.NewRecorder()
					req := httptest.NewRequest(http.MethodPost, fixture.Path, bytes.NewReader(fixture.Request))
					req.Header.Set("Content-Type", "application/json")
					h.ServeHTTP(w, req)
					if failure.Action == attempt.Stop {
						require.NotContains(t, w.Body.String(), fixture.Terminal)
					} else {
						require.Equal(t, http.StatusOK, w.Code, w.Body.String())
						require.Contains(t, w.Body.String(), fixture.Terminal)
					}
				}
				require.Len(t, log, 1)
				got := <-log
				require.False(t, got.Deadline.IsZero())
				expected := failure.AttemptDecision
				expected.Deadline = got.Deadline
				require.Equal(t, expected, got)
				sends := expected.AttemptNumber
				if expected.Action == attempt.Switch || expected.Action == attempt.RetrySame {
					sends++
				}
				hits := upstream.accountHits()
				require.Len(t, hits, sends)
				if expected.Action == attempt.RetrySame {
					require.Equal(t, hits[0], hits[1])
				}
			})
		}
	}
}

func TestWebSocketFirstOutputAttemptFixtures(t *testing.T) {
	var cases []struct {
		Name                       string
		Upstreams, Events, Reasons []string
		Close                      websocket.StatusCode
		Receipts                   []int64 `json:"receipt_accounts"`
	}
	data, err := os.ReadFile("../../../contracts/protocol/lifecycle/first-output-websocket.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &cases))
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			var accounts []service.Account
			var hits atomic.Int32
			firstClosed := make(chan struct{})
			for i := range 3 {
				accountID := int64(i + 1)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits.Add(1)
					conn, err := websocket.Accept(w, r, nil)
					if err != nil {
						return
					}
					defer conn.CloseNow()
					ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
					defer cancel()
					_, frame, err := conn.Read(ctx)
					if err != nil {
						return
					}
					if i >= len(tc.Upstreams) || gjson.GetBytes(frame, "type").String() != "response.create" {
						t.Errorf("unexpected send to account %d", accountID)
						return
					}
					if i > 0 {
						select {
						case <-firstClosed:
						case <-ctx.Done():
							t.Error("previous upstream still open during replacement")
							return
						}
					}
					switch tc.Upstreams[i] {
					case "silent":
						_, _, err = conn.Read(ctx)
						if ctx.Err() != nil || err == nil {
							t.Errorf("silent upstream %d was not closed by gateway", accountID)
						}
						if i == 0 {
							close(firstClosed)
						}
					case "complete":
						_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_replayed","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`))
						_, _, _ = conn.Read(ctx)
					case "delta_disconnect":
						_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.output_text.delta","response_id":"resp_partial","delta":"a"}`))
					case "partial_frame":
						// Flush a fragment and keep the message open across the first-output deadline.
						writer, err := conn.Writer(ctx, websocket.MessageText)
						if err != nil {
							t.Error(err)
							return
						}
						if _, err = io.WriteString(writer, `{"type":"response.output_text.delta","delta":"`+strings.Repeat("x", 64*1024)); err != nil {
							t.Error(err)
							return
						}
						_, _, _ = conn.Read(ctx)
					case "created":
						_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"resp_preamble","model":"gpt-5.1"}}`))
						_, _, _ = conn.Read(ctx)
					case "disconnect":
					default:
						t.Errorf("unknown upstream scenario %q", tc.Upstreams[i])
					}
				}))
				defer upstream.Close()
				accounts = append(accounts, service.Account{
					ID: accountID, Name: "fixture", Priority: i + 1, Concurrency: 1,
					Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true,
					Credentials: map[string]any{"api_key": "fixture", "base_url": upstream.URL},
					Extra: map[string]any{
						"openai_apikey_responses_websockets_v2_enabled": true,
						"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
					},
				})
			}
			decisions := make(attemptDecisionLog, 6)
			t.Cleanup(func() {
				if t.Failed() {
					for len(decisions) > 0 {
						t.Logf("decision: %+v", <-decisions)
					}
				}
			})
			receipts := &firstOutputFixtureReceipts{}
			router := firstOutputFixtureRouter(accounts, receipts)
			done := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				router.ServeHTTP(w, r.WithContext(attempt.WithRecorder(r.Context(), decisions)))
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/openai/v1/responses", nil)
			require.NoError(t, err)
			defer conn.CloseNow()
			require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1"}`)))
			for _, expected := range tc.Events {
				_, frame, err := conn.Read(ctx)
				require.NoError(t, err)
				require.Equal(t, expected, gjson.GetBytes(frame, "type").String())
			}
			if tc.Close == websocket.StatusNormalClosure {
				require.NoError(t, conn.Close(tc.Close, "done"))
			} else {
				_, _, err = conn.Read(ctx)
				require.Equal(t, tc.Close, websocket.CloseStatus(err))
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("handler did not finish")
			}
			require.EqualValues(t, len(tc.Upstreams), hits.Load())
			require.Len(t, decisions, len(tc.Reasons))
			var deadline time.Time
			for i, reason := range tc.Reasons {
				d := <-decisions
				require.Equal(t, reason, d.Reason)
				require.Equal(t, attempt.Unknown, d.Acceptance)
				require.Equal(t, i+1, d.AttemptNumber)
				require.Equal(t, 3-i, d.Remaining)
				require.Equal(t, tc.Upstreams[i] == "silent", d.AbandonedUncertain)
				if i == 0 {
					deadline = d.Deadline
					require.False(t, deadline.IsZero())
				} else {
					require.Equal(t, deadline, d.Deadline)
				}
				if reason == attempt.FirstOutputTimeoutAbandoned {
					require.Equal(t, attempt.Switch, d.Action)
					require.Equal(t, []int64{1}, d.Excluded)
				} else {
					require.Equal(t, attempt.Stop, d.Action)
				}
			}
			var billed []int64
			for _, r := range receipts.stored {
				billed = append(billed, r.Command.AccountID)
			}
			require.True(t, slices.Equal(tc.Receipts, billed), "receipt accounts %v", billed)
			if tc.Close == websocket.StatusNormalClosure {
				require.Equal(t, "complete", receipts.stored[0].CompletionState)
				require.Positive(t, receipts.stored[0].Command.BalanceCost)
			}
		})
	}
}

type firstOutputFixtureReceipts struct {
	unlimitedTPMReceiptStub
	stored []*service.UsageSettlementReceipt
}

func (repo *firstOutputFixtureReceipts) Persist(_ context.Context, r *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	repo.stored = append(repo.stored, r)
	return r, nil
}

func (repo *firstOutputFixtureReceipts) Settle(_ context.Context, key service.UsageSettlementKey, _ string) (*service.UsageSettlementResult, error) {
	for _, r := range repo.stored {
		if r.Key == key {
			settled := *r
			settled.State = service.UsageSettlementSettled
			return &service.UsageSettlementResult{Receipt: &settled, Billing: &service.UsageBillingApplyResult{}}, nil
		}
	}
	return nil, service.ErrUsageSettlementNotFound
}

func firstOutputFixtureRouter(accounts []service.Account, receipts *firstOutputFixtureReceipts) http.Handler {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.MaxAccountSwitches = 3
	repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil)
	gateway := service.ProvideOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), service.NewRateLimitService(repo, nil, cfg, nil, nil), billing,
		nil, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
	cache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	h := &OpenAIGatewayHandler{
		cfg: cfg, maxAccountSwitches: cfg.Gateway.MaxAccountSwitches,
		gatewayService: gateway, billingCacheService: billing,
		apiKeyService:     &service.APIKeyService{},
		concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second),
	}
	groupID := int64(42)
	key := &service.APIKey{
		ID: 18, UserID: 17, GroupID: &groupID,
		User:  &service.User{ID: 17, Status: service.StatusActive},
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1},
	}
	router := gin.New()
	router.GET("/openai/v1/responses", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.User.ID, Concurrency: 1})
		h.ResponsesWebSocket(c)
	})
	return router
}
