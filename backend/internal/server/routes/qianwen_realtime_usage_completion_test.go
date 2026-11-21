package routes

import (
	"context"
	"crypto/tls"
	"errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testassert"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ws "github.com/coder/websocket"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProductionQianwenRealtimeUsageCompletion(test *testing.T) {
	observedUsage := `{"characters":5,"input_tokens":2,"total_tokens":10,"output_tokens":8,"input_tokens_details":{"text_tokens":2},"output_tokens_details":{"text_tokens":0,"audio_tokens":8}}`
	for _, scenario := range []struct {
		name, usage                string
		review, persistenceFailure bool
	}{
		{name: "observed character and token usage", usage: observedUsage},
		{name: "unknown quantity retained", usage: `{"audio_seconds":3}`, review: true},
		{name: "malformed known quantity retained", usage: `{"characters":-1}`, review: true},
		{name: "unconfirmed receipt stops relay", usage: observedUsage, persistenceFailure: true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			wireModel := "qwen3-tts-flash-realtime"
			done := `{"type":"response.done","response":{"id":"observed-turn","status":"completed","usage":` + scenario.usage + `}}`
			audioDelta := `{"type":"response.audio.delta","response_id":"observed-turn","delta":"AAAA"}`
			audioDone := `{"type":"response.audio.done","response_id":"observed-turn"}`
			provider := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				connection, err := ws.Accept(writer, request, nil)
				if err != nil {
					return
				}
				defer func() {
					if err := connection.CloseNow(); !errors.Is(err, net.ErrClosed) {
						require.NoError(test, err)
					}
				}()
				_ = connection.Write(ctx, ws.MessageText, []byte(`{"type":"session.created","session":{"id":"fixture-session","model":"`+wireModel+`"}}`))
				if _, _, err = connection.Read(ctx); err != nil {
					return
				}
				_ = connection.Write(ctx, ws.MessageText, []byte(`{"type":"session.updated","session":{"mode":"commit"}}`))
				if _, _, err = connection.Read(ctx); err != nil {
					return
				} // text append
				if _, _, err = connection.Read(ctx); err != nil {
					return
				} // text commit
				for _, event := range []string{`{"type":"response.created","response":{"id":"observed-turn"}}`, audioDelta, audioDone, done, done} {
					if err = connection.Write(ctx, ws.MessageText, []byte(event)); err != nil {
						return
					}
				}
				if _, _, err = connection.Read(ctx); err != nil {
					return
				} // session.finish
				_ = connection.Write(ctx, ws.MessageText, []byte(`{"type":"session.finished"}`))
			}))
			defer provider.Close()
			original := http.DefaultTransport
			transport := testassert.Type[*http.Transport](test, original).Clone()
			transport.Proxy = nil
			transport.DialTLSContext = func(dialContext context.Context, network, address string) (net.Conn, error) {
				if address != "127.0.0.1:443" {
					return nil, errors.New("unexpected external destination")
				}
				dialer := tls.Dialer{Config: testassert.Type[*http.Transport](test, provider.Client().Transport).TLSClientConfig}
				return dialer.DialContext(dialContext, network, provider.Listener.Addr().String())
			}
			http.DefaultTransport = transport
			defer func() { http.DefaultTransport = original; transport.CloseIdleConnections() }()
			account := service.Account{ID: 7, Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{wireModel: wireModel}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}}
			jobs := &realtimeRouteJobs{receipt: make(chan *service.UpstreamJob, 1), finished: make(chan struct{})}
			if scenario.persistenceFailure {
				jobs.receiptError = errors.New("fixture persistence failure")
			}
			server := httptest.NewServer(realtimeProductionRouter(test, account, service.PlatformQianwen, wireModel, "", "", jobs))
			defer server.Close()
			client, response, err := ws.Dial(ctx, server.URL+"/v1/realtime?model="+wireModel, nil)
			if err != nil && response != nil {
				body, _ := io.ReadAll(response.Body)
				test.Fatalf("upgrade: %v %s", err, body)
			}
			require.NoError(test, err)
			defer func() {
				if err := client.CloseNow(); !errors.Is(err, net.ErrClosed) {
					require.NoError(test, err)
				}
			}()
			read := func() string {
				_, body, readErr := client.Read(ctx)
				require.NoError(test, readErr)
				return string(body)
			}
			require.Contains(test, read(), "session.created")
			require.NoError(test, client.Write(ctx, ws.MessageText, []byte(`{"type":"session.update","session":{"mode":"commit","voice":"Cherry","response_format":"pcm","sample_rate":24000}}`)))
			require.Contains(test, read(), "session.updated")
			// The sent text length deliberately differs from the authoritative characters.
			require.NoError(test, client.Write(ctx, ws.MessageText, []byte(`{"type":"input_text_buffer.append","text":"Hi"}`)))
			require.NoError(test, client.Write(ctx, ws.MessageText, []byte(`{"type":"input_text_buffer.commit"}`)))
			require.Contains(test, read(), "response.created")
			require.Equal(test, audioDelta, read())
			require.Equal(test, audioDone, read())
			if scenario.persistenceFailure {
				_, _, readErr := client.Read(ctx)
				require.Error(test, readErr, "unconfirmed durable receipt must still stop relay")
			} else {
				require.Equal(test, done, read())
				require.Equal(test, done, read(), "replay is delivered without a second charge")
				require.NoError(test, client.Write(ctx, ws.MessageText, []byte(`{"type":"session.finish"}`)))
				require.Contains(test, read(), "session.finished")
			}
			_ = client.CloseNow()
			select {
			case <-jobs.finished:
			case <-ctx.Done():
				test.Fatal("handler did not finish")
			}
			if scenario.persistenceFailure {
				require.Nil(test, jobs.storedReceipt)
				require.Zero(test, jobs.charges)
				return
			}
			require.JSONEq(test, scenario.usage, string(jobs.storedReceipt.UsageJSON))
			if scenario.review {
				require.Nil(test, jobs.storedReceipt.ChargedUsageLogID)
				require.Zero(test, jobs.charges)
				require.Equal(test, service.PricingStateError, jobs.storedReceipt.Billing.PricingState)
				require.Equal(test, "unsupported_usage", jobs.storedReceipt.ErrorCode)
				require.Equal(test, "settlement_needs_review", jobs.session.ErrorCode)
			} else {
				require.Equal(test, 1, jobs.charges)
				require.NotNil(test, jobs.storedReceipt.ChargedUsageLogID)
				require.Equal(test, service.QuantizeUsageBillingAmount(5*jobs.session.Billing.UnitPrice), jobs.chargedCommand.BalanceCost)
				require.Equal(test, "succeeded", jobs.storedReceipt.Status)
				require.Equal(test, "session_closed", jobs.session.ErrorCode)
			}
		})
	}
}
