//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ws "github.com/coder/websocket"

	"github.com/stretchr/testify/require"
)

type openAIRealtimeMemoryRepository struct{ *fileBatchMemoryRepo }

func (repository *openAIRealtimeMemoryRepository) CreateRealtimeSession(ctx context.Context, session *UpstreamJob) (*UpstreamJob, error) {
	stored, _, err := repository.Create(ctx, session)
	return stored, err
}

func TestOpenAIRealtimeFakeUpstreamTurnAndDisconnect(t *testing.T) {
	for _, priced := range []bool{false, true} {
		t.Run(map[bool]string{false: "needs review", true: "priced"}[priced], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			providerClosed := make(chan struct{})
			clientEvent := []byte(`{ "type": "response.create", "response": { "output_modalities": ["text"] } }`)
			declaredModel := ""
			sessionEvent := []byte(`{"type":"session.created","session":{"id":"provider-session"}}`)
			providerEvent := []byte(`{"type":"response.done","response":{"id":"response-example","status":"completed","usage":` + openAIRealtimeUsageFixture + `}}`)
			if priced {
				declaredModel = "provider-declared-snapshot"
				sessionEvent = []byte(`{"type":"session.created","session":{"id":"provider-session","model":"session-declaration"}}`)
				providerEvent = []byte(`{"type":"response.done","response":{"id":"response-example","model":"provider-declared-snapshot","status":"completed","usage":` + openAIRealtimeUsageFixture + `}}`)
			}
			received := make(chan []byte, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer close(providerClosed)
				connection, err := ws.Accept(writer, request, nil)
				if err != nil {
					return
				}
				defer connection.CloseNow()
				_ = connection.Write(ctx, ws.MessageText, sessionEvent)
				_, raw, err := connection.Read(ctx)
				if err != nil {
					return
				}
				received <- raw
				_ = connection.Write(ctx, ws.MessageText, providerEvent)
				_, _, _ = connection.Read(ctx)
			}))
			defer provider.Close()
			account := openAIActionAccount()
			account.Credentials["base_url"] = provider.URL
			gateway := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig()}
			repository := &openAIRealtimeMemoryRepository{&fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}}
			jobs := NewUpstreamJobService(repository, nil, GatewayResourceJobCharge)
			session, err := jobs.StartOpenAIRealtime(ctx, openAIRealtimeSessionFixture(priced))
			require.NoError(t, err)
			receipts := make(chan *UpstreamJob, 1)
			relayDone := make(chan struct{})
			proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer close(relayDone)
				lifetime, release, err := jobs.RegisterQianwenRealtime(request.Context())
				if err != nil {
					return
				}
				defer release()
				upstream, err := gateway.OpenOpenAIRealtime(lifetime, account, "gpt-realtime-2.1")
				if err != nil {
					return
				}
				defer upstream.CloseNow()
				client, err := ws.Accept(writer, request, nil)
				if err != nil {
					return
				}
				defer client.CloseNow()
				_ = RelayOpenAIRealtime(lifetime, client, upstream, OpenAIRealtimeCallbacks{
					BeforeEvent: func(context.Context, []byte) error { return nil },
					Receipt: func(receiptContext context.Context, id string, raw []byte, status, declaredModel string) error {
						job, err := jobs.RecordOpenAIRealtimeTurn(receiptContext, session, id, raw, status, declaredModel)
						if err == nil {
							receipts <- job
						}
						return err
					},
				})
				_ = jobs.FinishOpenAIRealtime(context.Background(), session.PublicRequestID, "session_closed")
			}))
			defer proxy.Close()
			client, _, err := ws.Dial(ctx, proxy.URL, nil)
			require.NoError(t, err)
			defer client.CloseNow()
			_, _, err = client.Read(ctx)
			require.NoError(t, err)
			require.NoError(t, client.Write(ctx, ws.MessageText, clientEvent))
			_, raw, err := client.Read(ctx)
			require.NoError(t, err)
			require.Equal(t, providerEvent, raw)
			require.Equal(t, clientEvent, <-received)
			job := <-receipts
			require.Equal(t, declaredModel, job.Billing.Realtime.UpstreamResponseModel)
			require.JSONEq(t, openAIRealtimeUsageFixture, string(job.UsageJSON))
			if priced {
				require.NotNil(t, job.ChargedUsageLogID)
			} else {
				require.Nil(t, job.ChargedUsageLogID)
				require.Equal(t, PricingStateUnpriced, job.Billing.PricingState)
			}
			require.NoError(t, client.CloseNow())
			select {
			case <-providerClosed:
			case <-time.After(time.Second):
				t.Fatal("client disconnect did not close upstream")
			}
			select {
			case <-relayDone:
			case <-time.After(time.Second):
				t.Fatal("relay did not join readers")
			}
			stored, err := repository.Get(ctx, session.PublicRequestID, session.UserID, session.APIKeyID)
			require.NoError(t, err)
			require.True(t, stored.Terminal())
		})
	}
}

func TestOpenAIRealtimeHandshakePreservesUpstreamHTTPFailure(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Retry-After", "9")
			writer.Header().Set("Authorization", "do-not-forward")
			writer.Header().Set("Set-Cookie", "do-not-forward")
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(`{"error":{"code":"not_entitled"}}`))
		}))
		account := openAIActionAccount()
		account.Credentials["base_url"] = provider.URL
		gateway := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig()}
		_, err := gateway.OpenOpenAIRealtime(context.Background(), account, "gpt-realtime-2.1")
		var upstream *UpstreamFailoverError
		require.ErrorAs(t, err, &upstream)
		require.Equal(t, status, upstream.StatusCode)
		require.JSONEq(t, `{"error":{"code":"not_entitled"}}`, string(upstream.ResponseBody))
		require.Equal(t, "9", upstream.ResponseHeaders.Get("Retry-After"))
		require.Empty(t, upstream.ResponseHeaders.Get("Authorization"))
		require.Empty(t, upstream.ResponseHeaders.Get("Set-Cookie"))
		provider.Close()
	}
}

func TestOpenAIRealtimeReceiptSurvivesSettlementFailure(t *testing.T) {
	repository := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}, failCharge: true}
	jobs := NewUpstreamJobService(repository, nil, GatewayResourceJobCharge)
	job, err := jobs.RecordOpenAIRealtimeTurn(context.Background(), openAIRealtimeSessionFixture(true), "response-retry", []byte(openAIRealtimeUsageFixture), "cancelled")
	require.Error(t, err)
	require.NotNil(t, job)
	require.Nil(t, job.ChargedUsageLogID)
	require.JSONEq(t, openAIRealtimeUsageFixture, string(job.UsageJSON))
	repository.failCharge = false
	jobs.now = func() time.Time { return job.NextPollAt }
	settled, err := jobs.Poll(context.Background(), job.PublicRequestID)
	require.NoError(t, err)
	require.NotNil(t, settled.ChargedUsageLogID)
	require.Equal(t, 1, repository.charges)
}

func TestOpenAIRealtimeFrozenPricesSurviveJSONRoundTrip(t *testing.T) {
	job := openAIRealtimeSessionFixture(true)
	encoded, err := json.Marshal(job)
	require.NoError(t, err)
	var restored UpstreamJob
	require.NoError(t, json.Unmarshal(encoded, &restored))
	job.UsageJSON = []byte(openAIRealtimeUsageFixture)
	restored.UsageJSON = job.UsageJSON
	require.True(t, EqualQianwenRealtimeReceipt(job, &restored))
	require.False(t, strings.Contains(string(encoded), "api_key\""))
}

func TestOpenAIRealtimeDisconnectWhilePersistingClosesUpstream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	providerClosed := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer close(providerClosed)
		connection, err := ws.Accept(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.CloseNow()
		_ = connection.Write(ctx, ws.MessageText, []byte(`{"type":"response.done","response":{"id":"persisting","status":"completed","usage":{}}}`))
		_, _, _ = connection.Read(ctx)
	}))
	defer provider.Close()
	started, finishReceipt, relayDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseReceipt := func() { releaseOnce.Do(func() { close(finishReceipt) }) }
	defer releaseReceipt()
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer close(relayDone)
		upstream, _, err := ws.Dial(ctx, provider.URL, nil)
		if err != nil {
			return
		}
		defer upstream.CloseNow()
		client, err := ws.Accept(writer, request, nil)
		if err != nil {
			return
		}
		defer client.CloseNow()
		_ = RelayOpenAIRealtime(ctx, client, upstream, OpenAIRealtimeCallbacks{
			BeforeEvent: func(context.Context, []byte) error { return nil },
			Receipt: func(receiptContext context.Context, _ string, _ []byte, _ string, _ string) error {
				close(started)
				select {
				case <-finishReceipt:
					return receiptContext.Err()
				case <-receiptContext.Done():
					return receiptContext.Err()
				}
			},
		})
	}))
	defer proxy.Close()
	client, _, err := ws.Dial(ctx, proxy.URL, nil)
	require.NoError(t, err)
	defer client.CloseNow()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("receipt did not start")
	}
	require.NoError(t, client.CloseNow())
	select {
	case <-providerClosed:
	case <-time.After(time.Second):
		releaseReceipt()
		t.Fatal("slow persistence kept upstream alive after disconnect")
	}
	releaseReceipt()
	select {
	case <-relayDone:
	case <-ctx.Done():
		t.Fatal("relay did not finish")
	}
}
