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
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/attempt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
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
