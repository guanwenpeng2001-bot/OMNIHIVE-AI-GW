package service

import (
	"context"
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
	"github.com/tidwall/gjson"
)

const QianwenRealtimeFrameLimit = 256 << 10

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
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	if account.ProxyID != nil && account.Proxy != nil {
		proxy, parseErr := url.Parse(account.Proxy.URL())
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
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
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
	conn.SetReadLimit(QianwenRealtimeFrameLimit)
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
	configured, configuring, generating, finishing bool
	text                                           string
	characters, turns, events, windowEvents        int
	window                                         time.Time
	responseID                                     string
}

func (s *qianwenRealtimeState) client(raw []byte, now time.Time) error {
	if len(raw) > QianwenRealtimeFrameLimit || !json.Valid(raw) {
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
	return relayQianwenRealtime(ctx, client, upstream, cb, 120*time.Second, 30*time.Second, 5*time.Second)
}

func relayQianwenRealtime(parent context.Context, client *ws.Conn, upstream *QianwenRealtimeUpstream, cb QianwenRealtimeCallbacks, lifetime, idle, drain time.Duration) error {
	if cb.BeforeCommit == nil || cb.Receipt == nil {
		return errors.New("durable realtime billing callbacks required")
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), lifetime+drain)
	defer cancel()
	client.SetReadLimit(QianwenRealtimeFrameLimit)
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
	state := qianwenRealtimeState{}
	logicalTurn := 0
	seen := map[string]bool{}
	absolute := time.NewTimer(lifetime)
	defer absolute.Stop()
	timer := time.NewTimer(idle)
	defer timer.Stop()
	var drainTimer *time.Timer
	var drainCh <-chan time.Time
	defer func() {
		if drainTimer != nil {
			drainTimer.Stop()
		}
	}()
	disconnected := false
	startDrain := func() {
		if !disconnected {
			disconnected = true
			fromClient = nil
			drainTimer = time.NewTimer(drain)
			drainCh = drainTimer.C
			_ = client.CloseNow()
			// finish can flush buffered text. Only send it after an accepted
			// commit, or when the provider has no uncommitted text to synthesize.
			if state.configured && (state.generating || state.text == "") && !state.finishing {
				_ = write(upstream.Conn, []byte(`{"type":"session.finish"}`))
				state.finishing = true
			}
		}
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
			startDrain()
			parent = context.Background()
		case <-absolute.C:
			startDrain()
		case <-timer.C:
			startDrain()
		case <-drainCh:
			return errors.New("realtime disconnect drain expired")
		case f := <-fromClient:
			if f.err != nil {
				startDrain()
				continue
			}
			if f.kind != ws.MessageText {
				protocolError(errors.New("only JSON text frames supported"))
				startDrain()
				continue
			}
			canonical, err := CanonicalUpstreamJobBody(f.data)
			if err != nil {
				protocolError(err)
				startDrain()
				continue
			}
			if err = state.client(canonical, time.Now()); err != nil {
				protocolError(err)
				startDrain()
				continue
			}
			generating := gjson.GetBytes(canonical, "type").String() == "input_text_buffer.commit"
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
				if !state.configuring || event.Get("session.mode").String() != "commit" {
					return protocolError(errors.New("upstream did not acknowledge commit mode"))
				}
				state.configuring = false
				state.configured = true
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
				if err := cb.Receipt(ctx, id, []byte(event.Get("response.usage").Raw), event.Get("response.status").String()); err != nil {
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
					startDrain()
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
		if j.AttemptCount > 20 {
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
