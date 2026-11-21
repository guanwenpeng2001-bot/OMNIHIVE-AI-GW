package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	ws "github.com/coder/websocket"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/tidwall/gjson"
)

const QianwenRealtimeFrameLimit = 256 << 10
const QianwenRealtimeAudioAppendLimit = 15 << 20
const QianwenRealtimeAudioFrameLimit = (QianwenRealtimeAudioAppendLimit * 4 / 3) + (16 << 10)
const QianwenRealtimeAudioSecondsLimit = 600
const QianwenRealtimeAudioTurnLimit = 50

type QianwenRealtimeUpstream struct {
	accountID int64
	Conn      *ws.Conn
	Created   []byte
}

type QianwenRealtimeDialError struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func (e *QianwenRealtimeDialError) Error() string { return "DashScope realtime handshake failed" }

func (u *QianwenRealtimeUpstream) Close() {
	if u != nil && u.Conn != nil {
		_ = u.Conn.CloseNow()
	}
}

func (s *OpenAIGatewayService) OpenQianwenRealtime(ctx context.Context, account *Account, model string) (*QianwenRealtimeUpstream, error) {
	if account != nil {
		if err := checkUnboundedTPMSession(ctx, account.ID); err != nil {
			return nil, err
		}
	}
	target, _, err := qianwenRealtimeEndpoint(account, model)
	if err != nil {
		return nil, err
	}
	// Preserve the configured gateway host policy in addition to the region pin.
	if _, err = s.validateUpstreamBaseURL(strings.Replace(target, "wss://", "https://", 1)); err != nil {
		return nil, err
	}
	credential := strings.TrimSpace(account.GetCNAPIKey())
	if credential == "" {
		return nil, errors.New("Qianwen API key is required")
	}
	baseTransport, _ := http.DefaultTransport.(*http.Transport)
	transport := baseTransport.Clone()
	pinned := httpclient.NewUpstreamTransport(transport, privateUpstreamHosts(s.cfg))
	defer pinned.CloseIdleConnections()
	proxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return nil, egressErr
	}
	if proxyURL != "" {
		proxy, parseErr := url.Parse(proxyURL)
		if parseErr != nil {
			return nil, parseErr
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	headers := http.Header{}
	account.ApplyHeaderOverrides(headers)
	// Never allow account header overrides to replace auth or enable a different
	// handshake; only the documented workspace header is carried through.
	headers = http.Header{"Authorization": {"Bearer " + credential}, "X-DashScope-WorkSpace": headers.Values("X-DashScope-WorkSpace")}
	client := &http.Client{Transport: pinned, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	upstream, err := dialQianwenRealtime(ctx, target, model, headers, client)
	var failure *QianwenRealtimeDialError
	if errors.As(err, &failure) && failure.StatusCode > 0 && s.rateLimitService != nil {
		s.rateLimitService.HandleUpstreamError(ctx, account, failure.StatusCode, failure.Headers, failure.Body, model)
	}
	if upstream != nil {
		upstream.accountID = account.ID
	}
	return upstream, err
}

func dialQianwenRealtime(ctx context.Context, target, model string, headers http.Header, client *http.Client) (*QianwenRealtimeUpstream, error) {
	conn, resp, err := ws.Dial(ctx, target, &ws.DialOptions{HTTPClient: client, HTTPHeader: headers, CompressionMode: ws.CompressionDisabled})
	if err != nil {
		failure := &QianwenRealtimeDialError{}
		if resp != nil {
			failure.StatusCode = resp.StatusCode
			failure.Headers = resp.Header.Clone()
			if resp.Body != nil {
				failure.Body, _ = io.ReadAll(io.LimitReader(resp.Body, 8192))
				_ = resp.Body.Close()
			}
		}
		return nil, failure
	}
	frameLimit := QianwenRealtimeFrameLimit
	if QianwenRealtimeFamily(model) != "tts" {
		frameLimit = QianwenRealtimeAudioFrameLimit
	}
	conn.SetReadLimit(int64(frameLimit))
	kind, created, err := conn.Read(ctx)
	if err != nil || kind != ws.MessageText || !json.Valid(created) || gjson.GetBytes(created, "type").String() != "session.created" || gjson.GetBytes(created, "session.id").String() == "" || gjson.GetBytes(created, "session.model").String() != model {
		_ = conn.CloseNow()
		return nil, errors.New("invalid DashScope session.created")
	}
	return &QianwenRealtimeUpstream{Conn: conn, Created: created}, nil
}

// All state transitions and writes run in one supervisor. Only readers run in
// goroutines; their queues contain one bounded frame each and are always joined.
type qianwenRealtimeState struct {
	family                                         string
	sampleRate                                     int
	vad, clearing                                  bool
	audioBytes, pendingBytes, acknowledgedBytes    int64
	commitBytes                                    int64
	speechEnds                                     map[string]int64
	itemBytes                                      map[string]int64
	clientEvents                                   map[string]bool
	beginTurn                                      bool
	configured, configuring, generating, finishing bool
	text                                           string
	characters, turns, events, windowEvents        int
	window                                         time.Time
	responseID                                     string
}

func (s *qianwenRealtimeState) client(raw []byte, now time.Time) error {
	limit := QianwenRealtimeFrameLimit
	if s.family == "omni" || s.family == "asr" {
		limit = QianwenRealtimeAudioFrameLimit
	}
	if len(raw) > limit || !json.Valid(raw) {
		return errors.New("invalid realtime JSON")
	}
	s.events++
	if now.Sub(s.window) >= time.Second {
		s.window = now
		s.windowEvents = 0
	}
	s.windowEvents++
	if s.events > 2000 || s.windowEvents > 50 {
		return errors.New("realtime event limit exceeded")
	}
	event := gjson.ParseBytes(raw)
	if s.family == "omni" || s.family == "asr" {
		return s.audioClient(event)
	}
	switch event.Get("type").String() {
	case "session.update":
		if s.configured || s.configuring || s.generating || s.finishing {
			return errors.New("session is already configured")
		}
		session := event.Get("session")
		valid := session.IsObject() && session.Get("mode").String() == "commit" && session.Get("response_format").String() == "pcm" && session.Get("sample_rate").Type == gjson.Number && session.Get("sample_rate").Float() == 24000
		session.ForEach(func(k, v gjson.Result) bool {
			switch k.String() {
			case "mode", "response_format", "sample_rate", "voice", "language_type":
			default:
				valid = false
			}
			return true
		})
		if !valid {
			return errors.New("only commit mode, PCM 24000Hz and built-in voices are supported")
		}
		s.configuring = true
	case "input_text_buffer.append":
		if !s.configured || s.generating || s.finishing || event.Get("text").Type != gjson.String {
			return errors.New("text is not accepted in this session state")
		}
		text := event.Get("text").String()
		n := utf8.RuneCountInString(text)
		if n == 0 || s.characters+n > 2000 {
			return errors.New("realtime character limit exceeded")
		}
		s.characters += n
		s.text += text
	case "input_text_buffer.clear":
		if !s.configured || s.generating || s.finishing {
			return errors.New("cannot clear a generating session")
		}
		s.text = ""
	case "input_text_buffer.commit":
		if !s.configured || s.generating || s.finishing || s.text == "" || s.turns >= 20 {
			return errors.New("only one nonempty turn may generate at a time")
		}
		s.generating = true
		s.turns++
	case "session.finish":
		if !s.configured || s.finishing || s.text != "" && !s.generating {
			return errors.New("commit or clear buffered text before finishing")
		}
		s.finishing = true
	default:
		return errors.New("unsupported DashScope realtime event or mode")
	}
	return nil
}

type QianwenRealtimeCallbacks struct {
	// BeforeCommit performs current eligibility/moderation and a durable pending
	// marker before any chargeable commit is sent to the provider.
	BeforeCommit func(context.Context, string) error
	Receipt      func(context.Context, string, []byte, string) error
}

func RelayQianwenRealtime(ctx context.Context, client *ws.Conn, upstream *QianwenRealtimeUpstream, cb QianwenRealtimeCallbacks) error {
	idle := 30 * time.Second
	if upstream != nil && QianwenRealtimeFamily(gjson.GetBytes(upstream.Created, "session.model").String()) != "tts" {
		idle = 60 * time.Second
	}
	return relayQianwenRealtime(ctx, client, upstream, cb, 120*time.Second, idle, 5*time.Second)
}

func relayQianwenRealtime(parent context.Context, client *ws.Conn, upstream *QianwenRealtimeUpstream, cb QianwenRealtimeCallbacks, lifetime, idle, drain time.Duration) error {
	if cb.BeforeCommit == nil || cb.Receipt == nil {
		return errors.New("durable realtime billing callbacks required")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	family := QianwenRealtimeFamily(gjson.GetBytes(upstream.Created, "session.model").String())
	limit := QianwenRealtimeFrameLimit
	if family == "omni" || family == "asr" {
		limit = QianwenRealtimeAudioFrameLimit
	}
	client.SetReadLimit(int64(limit))
	type frame struct {
		kind ws.MessageType
		data []byte
		err  error
	}
	read := func(conn *ws.Conn, out chan<- frame, done chan<- struct{}) {
		defer close(done)
		for {
			k, b, e := conn.Read(ctx)
			select {
			case out <- frame{k, b, e}:
			case <-ctx.Done():
				return
			}
			if e != nil {
				return
			}
		}
	}
	fromClient, fromUpstream := make(chan frame, 1), make(chan frame, 1)
	clientDone, upstreamDone := make(chan struct{}), make(chan struct{})
	go read(client, fromClient, clientDone)
	go read(upstream.Conn, fromUpstream, upstreamDone)
	defer func() { cancel(); _ = client.CloseNow(); upstream.Close(); <-clientDone; <-upstreamDone }()
	write := func(conn *ws.Conn, b []byte) error {
		wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
		defer wcancel()
		return conn.Write(wctx, ws.MessageText, b)
	}
	if err := write(client, upstream.Created); err != nil {
		return err
	}
	state := qianwenRealtimeState{family: family}
	logicalTurn := 0
	seen := map[string]bool{}
	absolute := time.NewTimer(lifetime)
	defer absolute.Stop()
	timer := time.NewTimer(idle)
	defer timer.Stop()
	disconnected := false
	stopGeneration := func() {
		disconnected = true
		cancel()
		upstream.Close()
		_ = client.CloseNow()
	}
	protocolError := func(err error) error {
		raw, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]string{"type": "unsupported_operation_error", "code": "qianwen_realtime_rejected", "message": "Realtime operation rejected"}})
		_ = write(client, raw)
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-parent.Done():
			stopGeneration()
			parent = context.Background()
		case <-absolute.C:
			return protocolError(errors.New("realtime lifetime exceeded"))
		case <-timer.C:
			return protocolError(errors.New("realtime idle limit exceeded"))
		case f := <-fromClient:
			if f.err != nil {
				stopGeneration()
				continue
			}
			if f.kind != ws.MessageText {
				_ = protocolError(errors.New("only JSON text frames supported"))
				stopGeneration()
				continue
			}
			canonical, err := CanonicalUpstreamJobBody(f.data)
			if err != nil {
				_ = protocolError(err)
				stopGeneration()
				continue
			}
			if err = state.client(canonical, time.Now()); err != nil {
				_ = protocolError(err)
				stopGeneration()
				continue
			}
			generating := gjson.GetBytes(canonical, "type").String() == "input_text_buffer.commit" || state.beginTurn
			if generating {
				logicalTurn++
				BeginAttemptStreamTurn(ctx, logicalTurn)
				if err := CheckAttemptBudget(ctx); err != nil {
					return err
				}
			}
			if generating && cb.BeforeCommit != nil {
				commitCtx, commitCancel := context.WithTimeout(ctx, 5*time.Second)
				err = cb.BeforeCommit(commitCtx, state.text)
				commitCancel()
				if err != nil {
					state.generating = false
					return protocolError(err)
				}
			}
			if generating {
				finish, admissionErr := BeginGenerationAttempt(ctx, upstream.accountID)
				if admissionErr != nil {
					return admissionErr
				}
				err = write(upstream.Conn, canonical)
				finish(0, err)
			} else {
				err = write(upstream.Conn, canonical)
			}
			if err != nil {
				return err
			}
			timer.Reset(idle)
		case f := <-fromUpstream:
			if f.err != nil {
				return f.err
			}
			if f.kind != ws.MessageText || !json.Valid(f.data) {
				return errors.New("invalid upstream realtime event")
			}
			event := gjson.ParseBytes(f.data)
			switch event.Get("type").String() {
			case "session.updated":
				if !state.configuring || (family == "tts" && event.Get("session.mode").String() != "commit") {
					return protocolError(errors.New("upstream did not acknowledge commit mode"))
				}
				if family != "tts" {
					rate := event.Get("session.sample_rate")
					if rate.Exists() && (rate.Type != gjson.Number || rate.Float() != float64(state.sampleRate)) {
						return protocolError(errors.New("upstream changed PCM sample rate"))
					}
				}
				state.configuring = false
				state.configured = true
			case "input_audio_buffer.cleared":
				if family != "tts" {
					if !state.clearing {
						return protocolError(errors.New("unexpected clear acknowledgment"))
					}
					state.clearing = false
				}
			case "input_audio_buffer.speech_stopped":
				if family != "tts" && state.vad {
					if err := state.audioSpeechStopped(event); err != nil {
						return protocolError(err)
					}
				}
			case "input_audio_buffer.committed":
				if family != "tts" {
					if err := state.audioCommitted(event); err != nil {
						return protocolError(err)
					}
				}
			case "conversation.item.input_audio_transcription.completed":
				if family == "asr" {
					id := event.Get("item_id").String()
					bytes, ok := state.itemBytes[id]
					if !ok || bytes <= 0 {
						return protocolError(errors.New("unacknowledged transcription"))
					}
					usage, _ := json.Marshal(map[string]any{"seconds": float64(bytes) / float64(state.sampleRate*2), "meter": "relay_pcm_bytes"})
					billingContext, finishBilling := detachedBillingContext(ctx)
					receiptErr := cb.Receipt(billingContext, id, usage, "completed")
					finishBilling()
					if receiptErr != nil {
						return receiptErr
					}
					if !seen[id] && !state.vad {
						state.generating = false
					}
					seen[id] = true
				}
			case "conversation.item.input_audio_transcription.failed":
				if family == "asr" {
					return protocolError(errors.New("upstream transcription failed"))
				}
			case "response.created":
				if !state.generating || state.responseID != "" {
					return errors.New("unexpected upstream response")
				}
				state.responseID = event.Get("response.id").String()
				if state.responseID == "" {
					return errors.New("missing response id")
				}
			case "response.done":
				id := event.Get("response.id").String()
				if id == "" {
					return errors.New("missing response id")
				}
				// Replays may arrive after a turn completed. The durable ledger decides
				// whether they match; no in-memory dedup can bypass its conflict check.
				replay := seen[id]
				if !replay && (!state.generating || state.responseID != "" && state.responseID != id) {
					return errors.New("response identity mismatch")
				}
				if cb.Receipt == nil {
					return errors.New("realtime billing unavailable")
				}
				billingContext, finishBilling := detachedBillingContext(ctx)
				receiptErr := cb.Receipt(billingContext, id, []byte(event.Get("response.usage").Raw), event.Get("response.status").String())
				finishBilling()
				if err := receiptErr; err != nil {
					return err
				}
				if !replay {
					seen[id] = true
					state.generating = false
					state.text = ""
					state.responseID = ""
				}
			}
			if !disconnected {
				if err := write(client, sanitizeQianwenRealtimeError(f.data)); err != nil {
					stopGeneration()
				}
			}
			if event.Get("type").String() == "session.finished" || event.Get("type").String() == "error" {
				return nil
			}
			if disconnected && !state.generating {
				return nil
			}
			timer.Reset(idle)
		}
	}
}

func (s *UpstreamJobService) MarkQianwenRealtimePending(ctx context.Context, id string) error {
	release, err := s.beginWork()
	if err != nil {
		return err
	}
	defer release()
	_, err = s.locked(ctx, id, func(j *UpstreamJob, _ UpstreamJobTx) error {
		if j.Terminal() || !j.ExpiresAt.After(s.now()) {
			return fmt.Errorf("realtime admission lease expired")
		}
		j.ErrorCode = "usage_pending"
		j.ErrorMessage = "A committed turn may incur usage; never estimate missing receipts"
		j.AttemptCount++
		limit := 20
		if QianwenRealtimeFamily(j.Billing.Model) != "tts" {
			limit = QianwenRealtimeAudioTurnLimit
		}
		if j.AttemptCount > limit {
			return errors.New("realtime turn limit exceeded")
		}
		return nil
	})
	return err
}

func (s *UpstreamJobService) AcknowledgeQianwenRealtimeReceipt(ctx context.Context, id string) error {
	_, err := s.locked(ctx, id, func(j *UpstreamJob, _ UpstreamJobTx) error { j.ErrorCode = ""; j.ErrorMessage = ""; return nil })
	return err
}

// Error messages may contain credentials, URLs or reflected input. Forward a
// fixed public envelope; audio, usage and every non-error frame stay byte exact.
func sanitizeQianwenRealtimeError(raw []byte) []byte {
	if gjson.GetBytes(raw, "type").String() != "error" {
		return raw
	}
	return []byte(`{"type":"error","error":{"type":"upstream_error","code":"qianwen_realtime_error","message":"Realtime upstream rejected the request"}}`)
}

// Audio uses PCM16 mono, so actual submitted bytes determine duration. Headers
// such as WAV nframes are never accepted as a duration source.
func (s *qianwenRealtimeState) audioClient(event gjson.Result) error {
	s.beginTurn = false
	id := event.Get("event_id").String()
	if id != "" {
		if len(id) > 256 {
			return errors.New("invalid event id")
		}
		if s.clientEvents == nil {
			s.clientEvents = map[string]bool{}
		}
		if s.clientEvents[id] {
			return errors.New("duplicate client event")
		}
		s.clientEvents[id] = true
	}
	kind := event.Get("type").String()
	if kind != "input_audio_buffer.append" && len(event.Raw) > QianwenRealtimeFrameLimit {
		return errors.New("realtime configuration too large")
	}
	allowed := map[string]bool{"type": true, "event_id": true}
	switch kind {
	case "session.update":
		allowed["session"] = true
	case "input_audio_buffer.append":
		allowed["audio"] = true
	case "response.create":
		allowed["response"] = true
	}
	valid := true
	event.ForEach(func(key, value gjson.Result) bool {
		if !allowed[key.String()] {
			valid = false
		}
		return true
	})
	if !valid {
		return errors.New("unsupported realtime event field")
	}
	if kind == "session.update" {
		if s.configured || s.configuring || s.finishing {
			return errors.New("session already configured")
		}
		session := event.Get("session")
		if !session.IsObject() {
			return errors.New("session configuration required")
		}
		fields := map[string]bool{"input_audio_format": true, "turn_detection": true}
		if s.family == "asr" {
			fields["sample_rate"] = true
			fields["input_audio_transcription"] = true
			s.sampleRate = 16000
		} else {
			fields["modalities"] = true
			fields["voice"] = true
			fields["output_audio_format"] = true
			s.sampleRate = 16000
		}
		session.ForEach(func(key, value gjson.Result) bool {
			if !fields[key.String()] {
				valid = false
			}
			return true
		})
		if value := session.Get("input_audio_format"); value.Exists() && value.String() != "pcm" && (s.family != "omni" || value.String() != "pcm16") {
			valid = false
		}
		if rate := session.Get("sample_rate"); rate.Exists() {
			if rate.Type != gjson.Number || (rate.Float() != 8000 && rate.Float() != 16000) {
				valid = false
			} else {
				s.sampleRate = int(rate.Int())
			}
		}
		if format := session.Get("output_audio_format"); format.Exists() && format.String() != "pcm16" && format.String() != "pcm" {
			valid = false
		}
		if modalities := session.Get("modalities"); modalities.Exists() {
			if !modalities.IsArray() || len(modalities.Array()) == 0 {
				valid = false
			}
			for _, modality := range modalities.Array() {
				if modality.String() != "text" && modality.String() != "audio" {
					valid = false
				}
			}
		}
		detection := session.Get("turn_detection")
		// Explicit null selects manual mode; omitted configuration uses provider VAD.
		s.vad = !detection.Exists() || detection.Type != gjson.Null
		if detection.Exists() && detection.Type != gjson.Null {
			if !detection.IsObject() || detection.Get("type").String() != "server_vad" {
				valid = false
			}
			detection.ForEach(func(key, value gjson.Result) bool {
				switch key.String() {
				case "type":
				case "threshold":
					if value.Type != gjson.Number || value.Float() < -1 || value.Float() > 1 {
						valid = false
					}
				case "silence_duration_ms":
					if value.Type != gjson.Number || value.Float() != float64(value.Int()) || value.Int() < 200 || value.Int() > 6000 {
						valid = false
					}
				default:
					valid = false
				}
				return true
			})
		}
		if transcription := session.Get("input_audio_transcription"); transcription.Exists() {
			if !transcription.IsObject() {
				valid = false
			}
			transcription.ForEach(func(key, value gjson.Result) bool {
				switch key.String() {
				case "language":
					if value.Type != gjson.String {
						valid = false
					}
				case "corpus":
					if !value.IsObject() || len(value.Map()) != 1 || value.Get("text").Type != gjson.String {
						valid = false
					}
				default:
					valid = false
				}
				return true
			})
		}
		if !valid {
			return errors.New("unsupported PCM session configuration")
		}
		s.configuring = true
		return nil
	}
	if !s.configured || s.finishing || s.clearing {
		return errors.New("audio not accepted in this session state")
	}
	switch kind {
	case "input_audio_buffer.append":
		if !s.vad && s.generating {
			return errors.New("wait for current audio turn")
		}
		encoded := event.Get("audio")
		if encoded.Type != gjson.String {
			return errors.New("base64 PCM required")
		}
		audio, err := base64.StdEncoding.Strict().DecodeString(encoded.String())
		if err != nil || len(audio) == 0 || len(audio)%2 != 0 || len(audio) > QianwenRealtimeAudioAppendLimit {
			return errors.New("invalid or oversized PCM append")
		}
		if len(audio) >= 4 && string(audio[:4]) == "RIFF" {
			return errors.New("raw PCM required, remove WAV container")
		}
		if s.audioBytes+int64(len(audio)) > int64(s.sampleRate*2*QianwenRealtimeAudioSecondsLimit) {
			return errors.New("session audio duration exceeded")
		}
		if s.vad && !s.generating {
			if err := s.startAudioTurn(); err != nil {
				return err
			}
		}
		s.audioBytes += int64(len(audio))
		s.pendingBytes += int64(len(audio))
	case "input_audio_buffer.clear":
		if !s.vad && s.generating {
			return errors.New("cannot clear committed audio")
		}
		s.pendingBytes = 0
		s.acknowledgedBytes = s.audioBytes
		s.generating = false
		s.clearing = true
	case "input_audio_buffer.commit":
		if s.vad || s.generating || s.pendingBytes == 0 {
			return errors.New("nonempty manual audio turn required")
		}
		if err := s.startAudioTurn(); err != nil {
			return err
		}
		s.commitBytes = s.pendingBytes
	case "response.create":
		if s.family != "omni" || s.vad || !s.generating || s.responseID != "" {
			return errors.New("response.create requires an Omni manual turn")
		}
		response := event.Get("response")
		if response.Exists() && (!response.IsObject() || len(response.Map()) != 0) {
			return errors.New("response overrides not supported")
		}
	case "session.finish":
		if !s.vad && s.pendingBytes > 0 && !s.generating {
			return errors.New("commit or clear buffered audio before finishing")
		}
		s.finishing = true
	default:
		return errors.New("unsupported DashScope audio event")
	}
	return nil
}
func (s *qianwenRealtimeState) startAudioTurn() error {
	if s.turns >= QianwenRealtimeAudioTurnLimit {
		return errors.New("realtime turn limit exceeded")
	}
	s.turns++
	s.generating = true
	s.beginTurn = true
	return nil
}
func (s *qianwenRealtimeState) audioSpeechStopped(event gjson.Result) error {
	id := event.Get("item_id").String()
	end := event.Get("audio_end_ms")
	if id == "" || len(id) > 256 || end.Type != gjson.Number || end.Float() != float64(end.Int()) || end.Int() < 0 || end.Int() > QianwenRealtimeAudioSecondsLimit*1000 {
		return errors.New("invalid VAD audio boundary")
	}
	bytes := end.Int() * int64(s.sampleRate*2) / 1000
	if prior, ok := s.speechEnds[id]; ok {
		if prior != bytes {
			return errors.New("conflicting VAD boundary")
		}
		return nil
	}
	if len(s.speechEnds) >= QianwenRealtimeAudioTurnLimit {
		return errors.New("VAD turn limit exceeded")
	}
	if bytes > s.audioBytes || bytes <= s.acknowledgedBytes {
		return errors.New("VAD boundary exceeds submitted PCM")
	}
	if s.speechEnds == nil {
		s.speechEnds = map[string]int64{}
	}
	if prior, ok := s.speechEnds[id]; ok && prior != bytes {
		return errors.New("conflicting VAD boundary")
	}
	s.speechEnds[id] = bytes
	return nil
}
func (s *qianwenRealtimeState) audioCommitted(event gjson.Result) error {
	id := event.Get("item_id").String()
	if id == "" || len(id) > 256 {
		return errors.New("missing committed audio identity")
	}
	if s.itemBytes == nil {
		s.itemBytes = map[string]int64{}
	}
	if _, ok := s.itemBytes[id]; ok {
		return nil
	}
	if len(s.itemBytes) >= QianwenRealtimeAudioTurnLimit {
		return errors.New("audio turn limit exceeded")
	}
	bytes := s.commitBytes
	if s.vad {
		end, ok := s.speechEnds[id]
		if !ok {
			return errors.New("VAD commit without bounded speech end")
		}
		bytes = end - s.acknowledgedBytes
	}
	if bytes <= 0 || bytes > s.pendingBytes {
		return errors.New("commit exceeds submitted audio")
	}
	s.itemBytes[id] = bytes
	s.pendingBytes -= bytes
	s.acknowledgedBytes += bytes
	s.commitBytes = 0
	if s.vad && s.family == "asr" {
		s.generating = false
	}
	return nil
}
