package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	ws "github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// OpenAI sessions share the durable admission lease and process drain registry
// with the other realtime adapters. The relay deadline must remain below the
// repository's 150 second admission lease.
func (s *UpstreamJobService) StartOpenAIRealtime(ctx context.Context, job *UpstreamJob) (*UpstreamJob, error) {
	release, err := s.beginWork()
	if err != nil {
		return nil, err
	}
	defer release()
	repository, ok := s.repo.(QianwenRealtimeSessionRepository)
	if !ok || job == nil || job.Platform != PlatformOpenAI || job.Billing.Realtime == nil || job.Billing.Model == "" || job.UserID <= 0 || job.APIKeyID <= 0 || job.AccountID <= 0 {
		return nil, errors.New("realtime session store or admission unavailable")
	}
	job.PublicRequestID = uuid.NewString()
	job.Family, job.Status = "realtime_session", "submitted"
	job.Billing.Realtime.SessionID = job.PublicRequestID
	job.RequestFingerprint, job.SubmissionKey = job.PublicRequestID, job.PublicRequestID
	job.SubmittedAt = s.now().UTC()
	job.ExpiresAt = job.SubmittedAt.Add(150 * time.Second)
	return repository.CreateRealtimeSession(ctx, job)
}

func (s *UpstreamJobService) FinishOpenAIRealtime(ctx context.Context, id, code string) error {
	return s.FinishQianwenRealtime(ctx, id, code)
}

// Preserve a response receipt before trying settlement, including unknown usage
// schemas. Replay identity and conflict handling use the existing repository.
func (s *UpstreamJobService) RecordOpenAIRealtimeTurn(ctx context.Context, session *UpstreamJob, responseID string, raw []byte, status string, declaredModel ...string) (*UpstreamJob, error) {
	if responseID == "" || len(responseID) > 256 || session == nil || session.Platform != PlatformOpenAI || session.Billing.Realtime == nil {
		return nil, ErrUpstreamJobUnsupportedUsage
	}
	repository, ok := s.repo.(QianwenRealtimeReceiptRepository)
	if !ok {
		return nil, errors.New("realtime receipt store unavailable")
	}
	if !json.Valid(raw) {
		raw = []byte("null")
	}
	sum := sha256.Sum256([]byte(session.PublicRequestID + "\x00" + responseID))
	identity := hex.EncodeToString(sum[:])
	if status == "usage_pending" && string(raw) == "null" {
		// Local review evidence has its own identity, so an authoritative
		// provider receipt can still be reconciled for this response.
		sum = sha256.Sum256([]byte("unfinished\x00" + identity))
		identity = hex.EncodeToString(sum[:])
		if err := s.FinishOpenAIRealtime(ctx, session.PublicRequestID, "usage_pending"); err != nil {
			return nil, err
		}
	}
	now := s.now().UTC()
	job := &UpstreamJob{PublicRequestID: uuid.NewString(), Family: "realtime_turn", Status: "running",
		Platform: PlatformOpenAI, UserID: session.UserID, APIKeyID: session.APIKeyID, AccountID: session.AccountID,
		GroupID: session.GroupID, Billing: session.Billing, RequestFingerprint: identity, SubmissionKey: identity,
		SubmittedAt: now, NextPollAt: now, ExpiresAt: now.Add(24 * time.Hour), UpstreamTaskID: responseID,
		UsageJSON: append(json.RawMessage(nil), raw...), ResultRef: status}
	// A session declaration is provider evidence, not a replacement for the
	// requested or wire model. Every receipt owns its own immutable snapshot.
	realtime := *session.Billing.Realtime
	realtime.UpstreamResponseModel = ""
	if len(declaredModel) > 0 {
		realtime.UpstreamResponseModel = declaredModel[0]
	}
	job.Billing.Realtime = &realtime
	_, usageErr := openAIRealtimeUsage(raw)
	switch status {
	case "completed", "cancelled", "failed", "incomplete":
	default:
		usageErr = ErrUpstreamJobUnsupportedUsage
	}
	if usageErr != nil {
		recordJobPricingFailure(job, usageErr)
		if status == "usage_pending" {
			s.finish(job, "failed", "settlement_needs_review", "Realtime turn ended without terminal usage")
		} else {
			s.finish(job, "failed", "unsupported_usage", "Unexplained realtime usage retained for review; not charged")
		}
	}
	stored, err := repository.InsertRealtimeReceipt(ctx, job)
	if err != nil || stored.Terminal() || !s.Ready() {
		return stored, err
	}
	return s.Poll(ctx, stored.PublicRequestID)
}

// Realtime can interleave several responses with audio for the next turn.
type realtimePendingTurns struct {
	active            map[string]bool
	done              map[string]bool
	requested         int
	committed         int
	awaitingCommitAck int
	audio             bool
}

func (p *realtimePendingTurns) clientEvent(kind string) {
	switch kind {
	case "response.create":
		p.requested++
		if p.committed > 0 {
			p.committed--
		}
	case "input_audio_buffer.commit":
		p.audio = false
		p.committed++
		p.awaitingCommitAck++
	case "input_audio_buffer.append":
		p.audio = true
	case "input_audio_buffer.clear":
		p.audio = false
	}
}

func (p *realtimePendingTurns) observe(id string) {
	if id == "" || p.active[id] || p.done[id] {
		return
	}
	p.active[id] = true
	if p.requested > 0 {
		p.requested--
	} else if p.committed > 0 {
		p.committed--
	}
}

func (p *realtimePendingTurns) count() int {
	count := p.requested + p.committed + len(p.active)
	if p.audio {
		count++
	}
	return count
}

// BeforeEvent rechecks current eligibility before forwarding client input.
// Receipt runs for every response.done, including provider-driven VAD turns and
// replays. A replay is always checked by the durable repository, never skipped.
type OpenAIRealtimeCallbacks struct {
	BeforeEvent func(context.Context, []byte) error
	Receipt     func(context.Context, string, []byte, string, string) error
}

func RelayOpenAIRealtime(parent context.Context, client, upstream *ws.Conn, callbacks OpenAIRealtimeCallbacks) (relayErr error) {
	if callbacks.BeforeEvent == nil || callbacks.Receipt == nil {
		return errors.New("durable realtime callbacks required")
	}
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	client.SetReadLimit(16 << 20)
	upstream.SetReadLimit(16 << 20)
	type frame struct {
		kind ws.MessageType
		body []byte
		err  error
	}
	// The reader arms the deadline itself because persistence can occupy the
	// supervisor. Provider reads remain active briefly to capture a late done.
	disconnected := make(chan struct{})
	stopDownstream := sync.OnceFunc(func() {
		close(disconnected)
		time.AfterFunc(250*time.Millisecond, func() { _ = upstream.CloseNow() })
	})
	read := func(connection *ws.Conn, frames chan<- frame, done chan<- struct{}) {
		defer close(done)
		for {
			kind, body, err := connection.Read(ctx)
			if err != nil && connection == client {
				stopDownstream()
			}
			select {
			case frames <- frame{kind, body, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}
	clientFrames, upstreamFrames := make(chan frame, 1), make(chan frame, 1)
	clientDone, upstreamDone := make(chan struct{}), make(chan struct{})
	go read(client, clientFrames, clientDone)
	go read(upstream, upstreamFrames, upstreamDone)
	defer func() { cancel(); _ = client.CloseNow(); _ = upstream.CloseNow(); <-clientDone; <-upstreamDone }()
	write := func(connection *ws.Conn, body []byte) error {
		writeContext, finish := context.WithTimeout(ctx, 5*time.Second)
		defer finish()
		return connection.Write(writeContext, ws.MessageText, body)
	}
	idle := time.NewTimer(60 * time.Second)
	defer idle.Stop()
	pending := realtimePendingTurns{active: make(map[string]bool), done: make(map[string]bool)}
	declaredModel := ""
	defer func() {
		if pending.count() == 0 {
			return
		}
		// Stop generation before potentially slow persistence. Give the whole
		// batch a single detached deadline.
		_ = upstream.CloseNow()
		persist, finish := detachedBillingContext(ctx)
		defer finish()
		record := func(id string) {
			if err := callbacks.Receipt(persist, id, nil, "usage_pending", declaredModel); err != nil {
				relayErr = errors.Join(relayErr, err)
			}
		}
		for id := range pending.active {
			record(id)
		}
		for range pending.requested + pending.committed {
			record("unacknowledged-" + uuid.NewString())
		}
		if pending.audio {
			record("uncommitted-audio-" + uuid.NewString())
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idle.C:
			return errors.New("realtime idle timeout")
		case event := <-clientFrames:
			select {
			case <-disconnected:
				clientFrames = nil
				continue
			default:
			}
			if event.kind != ws.MessageText || !json.Valid(event.body) || gjson.GetBytes(event.body, "type").Type != gjson.String {
				return errors.New("realtime requires JSON events")
			}
			// Realtime closes by closing the transport; session.finish belongs to
			// DashScope, and is deliberately not synthesized or translated here.
			if err := callbacks.BeforeEvent(ctx, event.body); err != nil {
				return err
			}
			pending.clientEvent(gjson.GetBytes(event.body, "type").String())
			if pending.count() > 50 {
				return errors.New("realtime turn limit reached")
			}
			if err := write(upstream, event.body); err != nil {
				return err
			}
			idle.Reset(60 * time.Second)
		case event := <-upstreamFrames:
			if event.err != nil {
				return event.err
			}
			if event.kind != ws.MessageText || !json.Valid(event.body) {
				return errors.New("invalid upstream realtime event")
			}
			eventType := gjson.GetBytes(event.body, "type").String()
			if eventType == "session.created" || eventType == "session.updated" {
				if model := gjson.GetBytes(event.body, "session.model"); model.Type == gjson.String {
					declaredModel = model.String()
				}
			}
			if eventType == "input_audio_buffer.committed" {
				if pending.awaitingCommitAck > 0 {
					pending.awaitingCommitAck--
				} else {
					pending.committed++
					pending.audio = false
				}
				if pending.count() > 50 {
					return errors.New("realtime turn limit reached")
				}
			}
			id := gjson.GetBytes(event.body, "response.id").String()
			if id == "" {
				id = gjson.GetBytes(event.body, "response_id").String()
			}
			if strings.HasPrefix(eventType, "response.") {
				pending.observe(id)
				if pending.count() > 50 {
					return errors.New("realtime turn limit reached")
				}
			}
			if eventType == "response.done" {
				response := gjson.GetBytes(event.body, "response")
				id := response.Get("id").String()
				if id == "" {
					return errors.New("missing realtime response identity")
				}
				persist, finish := detachedBillingContext(ctx)
				responseModel := declaredModel
				if model := response.Get("model"); model.Type == gjson.String {
					responseModel = model.String()
				}
				status := response.Get("status").String()
				// Reserve usage_pending for locally observed interruptions.
				if status == "usage_pending" {
					status = "unknown"
				}
				err := callbacks.Receipt(persist, id, []byte(response.Get("usage").Raw), status, responseModel)
				finish()
				if err != nil {
					return err
				}
				delete(pending.active, id)
				pending.done[id] = true
			}
			select {
			case <-disconnected:
			default:
				if err := write(client, event.body); err != nil {
					stopDownstream()
					clientFrames = nil
				}
			}
			if len(pending.done) >= 50 {
				return errors.New("realtime turn limit reached")
			}
			idle.Reset(60 * time.Second)
		}
	}
}
