//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	ws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const omniUsage = `{"input_tokens":5,"output_tokens":9,"input_tokens_details":{"text_tokens":2,"audio_tokens":3},"output_tokens_details":{"text_tokens":4,"audio_tokens":5},"total_tokens":14}`

func TestQianwenRealtimeFamilySnapshots(t *testing.T) {
	for _, generation := range []string{"qwen3-", "qwen3.8-"} {
		for _, family := range []string{"tts", "omni", "asr"} {
			for _, suffix := range []string{"", "-2025-09-15", "-2025-12-01", "-2025-10-27", "-2026-02-10", "-future-selected"} {
				model := generation + family + "-flash-realtime" + suffix
				require.Equal(t, family, QianwenRealtimeFamily(model), model)
				require.True(t, IsQianwenRealtimeModel(model), model)
			}
		}
	}
	for _, model := range []string{"", "qwen3.5-omni-plus-realtime", "qwen3.9-omni-flash-realtime", "qwen3-asr-flash", "qwen3.8-omni-flash", "qwen3.8-omni-plus-realtime", "qwen3-omni-flash-realtimeevil", "qwen3.8-omni-flash-realtimeevil"} {
		require.Empty(t, QianwenRealtimeFamily(model))
		require.False(t, IsQianwenRealtimeModel(model), model)
	}
}

func audioTestState(t *testing.T, family string, vad bool) *qianwenRealtimeState {
	t.Helper()
	state := &qianwenRealtimeState{family: family}
	detection := "null"
	if vad {
		detection = `{"type":"server_vad"}`
	}
	require.NoError(t, state.client([]byte(`{"type":"session.update","session":{"input_audio_format":"pcm","turn_detection":`+detection+`}}`), time.Now()))
	state.configuring = false
	state.configured = true
	return state
}
func audioAppend(bytes int) []byte {
	raw, _ := json.Marshal(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(make([]byte, bytes))})
	return raw
}

func TestQianwenRealtimeAudioEventGates(t *testing.T) {
	for _, family := range []string{"asr", "omni"} {
		state := audioTestState(t, family, false)
		require.NoError(t, state.client(audioAppend(32000), time.Now()))
		require.NoError(t, state.client([]byte(`{"type":"input_audio_buffer.commit"}`), time.Now()))
		require.True(t, state.beginTurn)
		err := state.client([]byte(`{"type":"response.create"}`), time.Now())
		if family == "omni" {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
		require.Error(t, state.client([]byte(`{"type":"input_text_buffer.append","text":"x"}`), time.Now()))
		require.Error(t, state.client([]byte(`{"type":"conversation.item.create"}`), time.Now()))
		require.NoError(t, state.client([]byte(`{"type":"session.finish"}`), time.Now()))
	}
	for _, config := range []string{`{"input_audio_format":"opus"}`, `{"sample_rate":24000}`, `{"sample_rate":8000.1}`, `{"voice":"Cherry"}`, `{"modalities":["text"]}`, `{"turn_detection":{"type":"server_vad","tools":[]}}`} {
		state := &qianwenRealtimeState{family: "asr"}
		require.Error(t, state.client([]byte(`{"type":"session.update","session":`+config+`}`), time.Now()), config)
	}
	for _, raw := range []string{`{"type":"input_audio_buffer.append","audio":"AA=="}`, `{"type":"input_audio_buffer.append","audio":"not base64"}`, `{"type":"input_audio_buffer.append","audio":"AAAA","model":"other"}`} {
		state := audioTestState(t, "asr", false)
		require.Error(t, state.client([]byte(raw), time.Now()))
	}
	state := audioTestState(t, "asr", false)
	raw := []byte(`{"event_id":"same","type":"input_audio_buffer.append","audio":"AAA="}`)
	require.NoError(t, state.client(raw, time.Now()))
	require.Error(t, state.client(raw, time.Now()))
}

func TestQianwenRealtimePCMSecondsBoundariesAndClear(t *testing.T) {
	state := audioTestState(t, "asr", false)
	require.NoError(t, state.client(audioAppend(32000), time.Now()))
	require.NoError(t, state.client([]byte(`{"type":"input_audio_buffer.clear"}`), time.Now()))
	require.Zero(t, state.pendingBytes)
	require.Error(t, state.client(audioAppend(2), time.Now()), "wait for clear acknowledgment")
	state.clearing = false
	require.NoError(t, state.client(audioAppend(16000), time.Now()))
	require.NoError(t, state.client([]byte(`{"type":"input_audio_buffer.commit"}`), time.Now()))
	committed := gjson.Parse(`{"item_id":"manual"}`)
	require.NoError(t, state.audioCommitted(committed))
	require.NoError(t, state.audioCommitted(committed))
	require.EqualValues(t, 16000, state.itemBytes["manual"])
	require.InDelta(t, .5, float64(state.itemBytes["manual"])/float64(state.sampleRate*2), 0)
	require.Error(t, state.audioCommitted(gjson.Parse(`{"item_id":"invented"}`)))
	state = audioTestState(t, "asr", false)
	state.audioBytes = 16000*2*600 - 2
	require.NoError(t, state.client(audioAppend(2), time.Now()))
	require.Error(t, state.client(audioAppend(2), time.Now()))
	state = audioTestState(t, "asr", false)
	state.turns = 50
	require.NoError(t, state.client(audioAppend(2), time.Now()))
	require.Error(t, state.client([]byte(`{"type":"input_audio_buffer.commit"}`), time.Now()))
	state = audioTestState(t, "asr", false)
	require.NoError(t, state.client(audioAppend(QianwenRealtimeAudioAppendLimit), time.Now()))
	state = audioTestState(t, "asr", false)
	require.Error(t, state.client(audioAppend(QianwenRealtimeAudioAppendLimit+2), time.Now()))
}

func TestQianwenRealtimeVADAcknowledgedPCMBounds(t *testing.T) {
	state := audioTestState(t, "asr", true)
	require.NoError(t, state.client(audioAppend(64000), time.Now()))
	require.True(t, state.beginTurn)
	stopped := gjson.Parse(`{"item_id":"vad","audio_end_ms":1000}`)
	require.NoError(t, state.audioSpeechStopped(stopped))
	require.NoError(t, state.audioCommitted(gjson.Parse(`{"item_id":"vad"}`)))
	require.EqualValues(t, 32000, state.itemBytes["vad"])
	require.EqualValues(t, 32000, state.pendingBytes)
	require.NoError(t, state.audioSpeechStopped(stopped))
	require.NoError(t, state.audioCommitted(gjson.Parse(`{"item_id":"vad"}`)))
	require.Error(t, state.audioSpeechStopped(gjson.Parse(`{"item_id":"vad","audio_end_ms":1100}`)))
	require.Error(t, state.audioSpeechStopped(gjson.Parse(`{"item_id":"other","audio_end_ms":2001}`)))
	require.Error(t, state.audioCommitted(gjson.Parse(`{"item_id":"unacknowledged"}`)))
}

func audioBillingFixture(t *testing.T, family string) *UpstreamJob {
	t.Helper()
	job := realtimeSessionFixture()
	job.Billing.Model = "qwen3-" + family + "-flash-realtime"
	hourly := 3.6
	billing, err := QianwenRealtimeBillingSnapshot(context.Background(), &Group{RateMultiplier: 1, AudioSTTPricePerHour: &hourly}, qianwenTestAccount(), &APIKey{}, job.Billing.Model, nil)
	require.NoError(t, err)
	billing.Realtime.SessionID = job.PublicRequestID
	job.Billing = billing
	return job
}

func TestQianwenRealtimeAudioUsageSchemasAndPricing(t *testing.T) {
	for _, raw := range []string{`{"seconds":null,"meter":"relay_pcm_bytes"}`, `{"seconds":-1,"meter":"relay_pcm_bytes"}`, `{"seconds":600.01,"meter":"relay_pcm_bytes"}`, `{"seconds":1,"meter":"upstream"}`, `{"seconds":1,"meter":"relay_pcm_bytes","characters":1}`, `{"seconds":"1","meter":"relay_pcm_bytes"}`} {
		_, err := qianwenRealtimeQuantities("qwen3-asr-flash-realtime", []byte(raw))
		require.Error(t, err, raw)
	}
	for _, raw := range []string{`{}`, `{"characters":1}`, strings.Replace(omniUsage, `"input_tokens":5`, `"input_tokens":6`, 1), strings.Replace(omniUsage, `"text_tokens":2`, `"text_tokens":null`, 1), strings.Replace(omniUsage, `"total_tokens":14`, `"total_tokens":15`, 1)} {
		_, err := qianwenRealtimeQuantities("qwen3-omni-flash-realtime", []byte(raw))
		require.Error(t, err, raw)
	}
	job := audioBillingFixture(t, "asr")
	job.UsageJSON = []byte(`{"seconds":0.5,"meter":"relay_pcm_bytes"}`)
	log, _, err := QianwenRealtimeCharge(job)
	require.NoError(t, err)
	require.InDelta(t, .0005, log.TotalCost, 1e-9)
	require.Zero(t, log.InputTokens)
	recalculated, err := RecalculatePricingSnapshot(job.Billing.PricingSnapshot)
	require.NoError(t, err)
	require.Equal(t, log.ActualCost, recalculated.ActualCost)
	job = audioBillingFixture(t, "omni")
	job.UsageJSON = []byte(omniUsage)
	_, _, err = QianwenRealtimeCharge(job)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, PricingStateUnpriced, job.Billing.PricingState)
	price, cost := .01, .005
	descriptor := &ModelPricingDescriptor{Model: job.Billing.Model, Variants: []ModelPriceVariant{{ID: job.Billing.Model}}}
	for _, dimension := range []string{"input_text", "input_audio", "output_text", "output_audio"} {
		descriptor.Variants[0].Prices = append(descriptor.Variants[0].Prices, ModelUnitPrice{Unit: "token", Dimension: dimension, Currency: "USD", Sales: &price, Cost: &cost})
	}
	job.Billing.Realtime.TokenPricing = descriptor
	log, _, err = QianwenRealtimeCharge(job)
	require.NoError(t, err)
	require.Equal(t, .14, log.TotalCost)
	require.Equal(t, 5, log.InputTokens)
	require.Equal(t, 9, log.OutputTokens)
	require.Equal(t, .07, *log.AccountStatsCost)
}

func TestQianwenRealtimeOmniUnpricedReceiptRetained(t *testing.T) {
	repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
	svc := NewUpstreamJobService(repo, nil, QianwenAsyncJobCharge)
	session := audioBillingFixture(t, "omni")
	job, err := svc.RecordQianwenRealtimeTurn(context.Background(), session, "omni-turn", []byte(omniUsage), "completed")
	require.NoError(t, err)
	require.Equal(t, "settlement_needs_review", job.ErrorCode)
	require.Equal(t, PricingStateUnpriced, job.Billing.PricingState)
	require.Nil(t, job.ChargedUsageLogID)
	require.Zero(t, repo.charges)
	_, err = svc.RecordQianwenRealtimeTurn(context.Background(), session, "omni-turn", []byte(omniUsage), "completed")
	require.NoError(t, err)
	_, err = svc.RecordQianwenRealtimeTurn(context.Background(), session, "omni-turn", []byte(strings.ReplaceAll(omniUsage, `"audio_tokens":3`, `"audio_tokens":4`)), "completed")
	require.ErrorIs(t, err, ErrQianwenRealtimeConflict)
}

func TestQianwenRealtimeASRRelayAcknowledgmentAndReplay(t *testing.T) {
	for _, mode := range []string{"completed", "disconnect", "unacknowledged"} {
		t.Run(mode, func(t *testing.T) {
			client, gatewayClient := realtimeSocketPair(t)
			gatewayUpstream, provider := realtimeSocketPair(t)
			provider.SetReadLimit(QianwenRealtimeAudioFrameLimit)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			receipts := make(chan []byte, 3)
			finished := make(chan error, 1)
			go func() {
				finished <- relayQianwenRealtime(ctx, gatewayClient, &QianwenRealtimeUpstream{Conn: gatewayUpstream, Created: []byte(strings.ReplaceAll(realtimeCreated, "tts", "asr"))}, QianwenRealtimeCallbacks{BeforeCommit: func(context.Context, string) error { return nil }, Receipt: func(_ context.Context, _ string, usage []byte, _ string) error { receipts <- usage; return nil }}, time.Second, time.Second, time.Second)
			}()
			defer func() { cancel(); <-finished }()
			read := func(conn *ws.Conn) []byte { _, raw, err := conn.Read(ctx); require.NoError(t, err); return raw }
			write := func(conn *ws.Conn, raw []byte) { require.NoError(t, conn.Write(ctx, ws.MessageText, raw)) }
			read(client)
			write(client, []byte(`{"type":"session.update","session":{"input_audio_format":"pcm","sample_rate":8000,"turn_detection":null}}`))
			read(provider)
			write(provider, []byte(`{"type":"session.updated","session":{"sample_rate":8000}}`))
			read(client)
			write(client, audioAppend(16000))
			read(provider)
			write(client, []byte(`{"type":"input_audio_buffer.commit"}`))
			read(provider)
			if mode == "disconnect" {
				client.CloseNow()
				_, _, err := provider.Read(ctx)
				require.Error(t, err)
				require.Empty(t, receipts)
				return
			}
			if mode == "completed" {
				write(provider, []byte(`{"type":"input_audio_buffer.committed","item_id":"audio"}`))
				read(client)
			}
			complete := []byte(`{"type":"conversation.item.input_audio_transcription.completed","item_id":"audio","transcript":"hello"}`)
			write(provider, complete)
			if mode == "unacknowledged" {
				require.Equal(t, "error", gjson.GetBytes(read(client), "type").String())
				require.Empty(t, receipts)
				return
			}
			read(client)
			require.JSONEq(t, `{"seconds":1,"meter":"relay_pcm_bytes"}`, string(<-receipts))
			write(provider, complete)
			read(client)
			require.JSONEq(t, `{"seconds":1,"meter":"relay_pcm_bytes"}`, string(<-receipts))
		})
	}
}

func TestQianwenRealtimeOmniFrozenDescriptorRoundtripReplay(t *testing.T) {
	model := "qwen3-omni-flash-realtime"
	price, cost := .01, .005
	variant := ModelPriceVariant{ID: model, Canonical: model}
	for _, dimension := range []string{"input_text", "input_audio", "output_text", "output_audio"} {
		variant.Prices = append(variant.Prices, ModelUnitPrice{Unit: "token", Dimension: dimension, Currency: "USD", Sales: &price, Cost: &cost})
	}
	group := &Group{Platform: PlatformQianwen, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Platform: PlatformQianwen, Models: []string{model}, PriceVariants: []ModelPriceVariant{variant}}}}
	billing, err := QianwenRealtimeBillingSnapshot(context.Background(), group, qianwenTestAccount(), &APIKey{}, model, nil)
	require.NoError(t, err)
	require.NotNil(t, billing.Realtime.TokenPricing)
	session := realtimeSessionFixture()
	billing.Realtime.SessionID = session.PublicRequestID
	session.Billing = billing
	price = 100 // Mutating operator state cannot alter the admitted descriptor.
	repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
	svc := NewUpstreamJobService(repo, nil, QianwenAsyncJobCharge)
	job, err := svc.RecordQianwenRealtimeTurn(context.Background(), session, "frozen", []byte(omniUsage), "completed")
	require.NoError(t, err)
	require.NotNil(t, job.ChargedUsageLogID)
	encoded, err := json.Marshal(job)
	require.NoError(t, err)
	var restored UpstreamJob
	require.NoError(t, json.Unmarshal(encoded, &restored))
	repo.jobs[job.PublicRequestID] = &restored
	_, err = svc.RecordQianwenRealtimeTurn(context.Background(), session, "frozen", []byte(omniUsage), "completed")
	require.NoError(t, err)
	require.Equal(t, 1, repo.charges)
	changed := strings.ReplaceAll(strings.ReplaceAll(omniUsage, `"output_tokens":9`, `"output_tokens":10`), `"total_tokens":14`, `"total_tokens":15`)
	changed = strings.ReplaceAll(changed, `"audio_tokens":5`, `"audio_tokens":6`)
	_, err = svc.RecordQianwenRealtimeTurn(context.Background(), session, "frozen", []byte(changed), "completed")
	require.ErrorIs(t, err, ErrQianwenRealtimeConflict)
	require.Equal(t, 1, repo.charges)
}

func TestQianwenRealtimeOmniRelayNativeEvents(t *testing.T) {
	client, gatewayClient := realtimeSocketPair(t)
	gatewayUpstream, provider := realtimeSocketPair(t)
	provider.SetReadLimit(QianwenRealtimeAudioFrameLimit)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	receipts := make(chan []byte, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- relayQianwenRealtime(ctx, gatewayClient, &QianwenRealtimeUpstream{Conn: gatewayUpstream, Created: []byte(strings.ReplaceAll(realtimeCreated, "tts", "omni"))}, QianwenRealtimeCallbacks{BeforeCommit: func(context.Context, string) error { return nil }, Receipt: func(_ context.Context, _ string, usage []byte, _ string) error { receipts <- usage; return nil }}, time.Second, time.Second, time.Second)
	}()
	defer func() { cancel(); <-finished }()
	read := func(conn *ws.Conn) []byte { _, raw, err := conn.Read(ctx); require.NoError(t, err); return raw }
	write := func(conn *ws.Conn, raw string) { require.NoError(t, conn.Write(ctx, ws.MessageText, []byte(raw))) }
	read(client)
	config := `{"type":"session.update","session":{"modalities":["text","audio"],"voice":"Cherry","input_audio_format":"pcm16","output_audio_format":"pcm16","turn_detection":null}}`
	write(client, config)
	require.JSONEq(t, config, string(read(provider)))
	write(provider, `{"type":"session.updated","session":{}}`)
	read(client)
	write(client, string(audioAppend(32000)))
	read(provider)
	write(client, `{"type":"input_audio_buffer.commit"}`)
	read(provider)
	write(provider, `{"type":"input_audio_buffer.committed","item_id":"audio"}`)
	read(client)
	write(client, `{"type":"response.create"}`)
	read(provider)
	write(provider, `{"type":"response.created","response":{"id":"omni"}}`)
	read(client)
	write(provider, `{"type":"response.audio.delta","delta":"AAA="}`)
	require.Equal(t, "AAA=", gjson.GetBytes(read(client), "delta").String())
	write(provider, `{"type":"response.done","response":{"id":"omni","status":"completed","usage":`+omniUsage+`}}`)
	read(client)
	require.JSONEq(t, omniUsage, string(<-receipts))
	write(client, `{"type":"session.finish"}`)
	read(provider)
	write(provider, `{"type":"session.finished"}`)
	read(client)
}

type realtimeAdmissionMemoryRepo struct{ *fileBatchMemoryRepo }

func (repo *realtimeAdmissionMemoryRepo) CreateRealtimeSession(_ context.Context, job *UpstreamJob) (*UpstreamJob, error) {
	return job, nil
}

func TestQianwenRealtimeAdmissionValidatesEachFamily(t *testing.T) {
	for _, family := range []string{"tts", "omni", "asr"} {
		job := realtimeSessionFixture()
		if family != "tts" {
			job = audioBillingFixture(t, family)
		}
		repo := &realtimeAdmissionMemoryRepo{&fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}}
		svc := NewUpstreamJobService(repo, nil, QianwenAsyncJobCharge)
		admitted, err := svc.StartQianwenRealtime(context.Background(), job)
		require.NoError(t, err, family)
		require.Equal(t, "realtime_session", admitted.Family)
		require.Equal(t, admitted.PublicRequestID, admitted.Billing.Realtime.SessionID)
		require.Equal(t, 150*time.Second, admitted.ExpiresAt.Sub(admitted.SubmittedAt))
	}
}
