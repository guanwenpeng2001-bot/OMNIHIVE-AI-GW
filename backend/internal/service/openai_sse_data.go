package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/apicompat"

	"github.com/tidwall/gjson"
)

// boundedStreamContext bounds header waiting while preserving request cancellation
// and its total deadline throughout generation.
type boundedStreamContextKey struct{}
type responseHeaderTimerKey struct{}

func streamHeaderTimeout(cfg *config.Config, openAI bool) time.Duration {
	seconds := 120
	if cfg != nil {
		if openAI && cfg.Gateway.OpenAIResponseHeaderTimeout > 0 {
			seconds = cfg.Gateway.OpenAIResponseHeaderTimeout
		} else if cfg.Gateway.ResponseHeaderTimeout > 0 {
			seconds = cfg.Gateway.ResponseHeaderTimeout
		}
	}
	return time.Duration(seconds) * time.Second
}

func boundedStreamContext(parent context.Context, headerTimeout time.Duration) (context.Context, func(), func()) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	ctx = context.WithValue(ctx, boundedStreamContextKey{}, true)
	header := time.AfterFunc(headerTimeout, cancel)
	headersReceived := func() { header.Stop() }
	ctx = context.WithValue(ctx, responseHeaderTimerKey{}, headersReceived)
	return ctx, headersReceived, func() { header.Stop(); cancel() }
}

// upstreamFrameReader reuses the protocol parser and bounds the complete frame,
// including endless comments or unterminated data lines. Close joins the only
// watchdog goroutine and closes the upstream connection on every exit path.
type upstreamFrameReader struct {
	parent         context.Context
	timeout        chan error
	disconnected   chan struct{}
	disconnectOnce sync.Once
	semantic       chan struct{}
	semanticOnce   sync.Once
	frameSize      int
	maxSize        int
	scanner        *bufio.Scanner
	parser         openAICompatSSEFrameParser
	body           io.ReadCloser
	frame          openAICompatSSEFrame
	err            error
	progress       chan bool
	done           chan struct{}
	joined         chan struct{}
}

func newUpstreamFrameReader(body io.ReadCloser, maxSize int, parent context.Context, idle time.Duration) *upstreamFrameReader {
	if idle <= 0 {
		idle = 300 * time.Second
	}
	r := &upstreamFrameReader{parent: parent, body: body, timeout: make(chan error, 1), disconnected: make(chan struct{}), semantic: make(chan struct{}), maxSize: maxSize, progress: make(chan bool, 1), done: make(chan struct{}), joined: make(chan struct{})}
	r.scanner = bufio.NewScanner(body)
	r.scanner.Buffer(make([]byte, 0, 64*1024), maxSize)
	go func() {
		defer close(r.joined)
		first := time.NewTimer(idle)
		gap := time.NewTimer(idle)
		defer first.Stop()
		defer gap.Stop()
		disconnected := r.disconnected
		semanticOutput := r.semantic
		var canceled <-chan struct{}
		if parent != nil {
			canceled = parent.Done()
		}
		for {
			select {
			case <-r.progress:
				gap.Reset(idle)
			case <-semanticOutput:
				first.Stop()
				semanticOutput = nil
			case <-disconnected:
				r.timeout <- context.Canceled
				_ = body.Close()
				return
			case <-canceled:
				r.timeout <- parent.Err()
				_ = body.Close()
				return
			case <-first.C:
				r.timeout <- fmt.Errorf("upstream first semantic output timeout")
				_ = body.Close()
				return
			case <-gap.C:
				r.timeout <- fmt.Errorf("upstream frame idle timeout")
				_ = body.Close()
				return
			case <-r.done:
				return
			}
		}
	}()
	return r
}

func (r *upstreamFrameReader) Close()        { close(r.done); _ = r.body.Close(); <-r.joined }
func (r *upstreamFrameReader) Semantic()     { r.semanticOnce.Do(func() { close(r.semantic) }) }
func (r *upstreamFrameReader) Disconnected() { r.disconnectOnce.Do(func() { close(r.disconnected) }) }
func (r *upstreamFrameReader) Scan() bool {
	if r.parent != nil && r.parent.Err() != nil {
		r.err = r.parent.Err()
		return false
	}
	select {
	case <-r.disconnected:
		r.err = context.Canceled
		return false
	default:
	}
	for r.scanner.Scan() {
		r.frameSize += len(r.scanner.Text())
		if r.frameSize > r.maxSize {
			r.err = fmt.Errorf("upstream SSE frame too large")
			return false
		}
		if r.scanner.Text() == "" {
			r.frameSize = 0
		}
		if frame, ok := r.parser.AddLine(r.scanner.Text()); ok {
			select {
			case r.progress <- false:
			default:
			}
			r.frame = frame
			return true
		}
	}
	r.err = r.scanner.Err()
	// Preserve a final complete JSON event from compatible servers that omit
	// the trailing blank line, but never salvage a frame after a read error.
	if r.err == nil {
		if frame, ok := r.parser.Finish(); ok {
			r.frame = frame
			return true
		}
	}
	return false
}
func (r *upstreamFrameReader) Err() error {
	select {
	case err := <-r.timeout:
		r.err = err
	default:
	}
	return r.err
}

func hasAnthropicFinalOutputUsage(data string) bool {
	output := gjson.Get(data, "usage.output_tokens")
	return output.Type == gjson.Number && output.Float() >= 0 && output.Float() == float64(output.Int())
}

type anthropicFrameReader struct {
	*upstreamFrameReader
	event                          apicompat.AnthropicStreamEvent
	started, completed, finalUsage bool
	tools                          map[int]json.RawMessage
}

func (s *GatewayService) newAnthropicFrameReader(resp io.ReadCloser, parent context.Context) *anthropicFrameReader {
	maxSize, idle := defaultMaxLineSize, 300*time.Second
	if s.cfg != nil {
		if s.cfg.Gateway.MaxLineSize > 0 {
			maxSize = s.cfg.Gateway.MaxLineSize
		}
		if s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
			idle = time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
		}
	}
	return &anthropicFrameReader{upstreamFrameReader: newUpstreamFrameReader(resp, maxSize, parent, idle), tools: make(map[int]json.RawMessage)}
}

func (r *anthropicFrameReader) Scan() bool {
	if r.completed || r.err != nil {
		return false
	}
	for r.upstreamFrameReader.Scan() {
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(r.frame.Data), &event); err != nil {
			r.err = fmt.Errorf("invalid Anthropic SSE frame: %w", err)
			return false
		}
		if event.Type == "" {
			event.Type = r.frame.EventType
		}
		if r.frame.EventType != "" && event.Type != r.frame.EventType {
			r.err = fmt.Errorf("anthropic SSE event type mismatch")
			return false
		}
		if event.Type == "ping" {
			continue
		}
		if event.Type == "error" {
			r.err = fmt.Errorf("anthropic upstream stream error")
			return false
		}
		if event.Type == "message_start" {
			r.started = event.Message != nil && event.Message.ID != ""
		}
		if !r.started {
			r.err = fmt.Errorf("anthropic stream missing message_start")
			return false
		}
		if event.Index != nil && *event.Index < 0 {
			r.err = fmt.Errorf("invalid Anthropic content index")
			return false
		}
		if event.Type == "content_block_start" && event.Index != nil && event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
			r.tools[*event.Index] = event.ContentBlock.Input
		}
		if event.Type == "content_block_delta" && event.Index != nil && event.Delta != nil && event.Delta.Type == "input_json_delta" {
			r.tools[*event.Index] = appendRawJSON(r.tools[*event.Index], event.Delta.PartialJSON)
		}
		if event.Type == "message_delta" && event.Usage != nil {
			r.finalUsage = hasAnthropicFinalOutputUsage(r.frame.Data)
		}
		if event.Type == "message_stop" {
			if !r.finalUsage {
				r.err = fmt.Errorf("anthropic stream missing final output usage")
				return false
			}
			for _, input := range r.tools {
				if len(input) > 0 && !json.Valid(input) {
					r.err = fmt.Errorf("incomplete Anthropic tool arguments")
					return false
				}
			}
			r.completed = true
		}
		if anthropicEventHasOutput(&event) || r.completed {
			r.Semantic()
		}
		r.event = event
		return true
	}
	return false
}

func (r *anthropicFrameReader) Err() error {
	if err := r.upstreamFrameReader.Err(); err != nil {
		return err
	}
	if !r.completed {
		return fmt.Errorf("anthropic stream ended before message_stop")
	}
	return nil
}

type openAISSEDataAccumulator struct {
	lines []string
}

func (a *openAISSEDataAccumulator) AddLine(line string, fn func([]byte)) {
	if fn == nil {
		return
	}
	trimmedLine := strings.TrimRight(line, "\r\n")
	if data, ok := extractOpenAISSEDataLine(trimmedLine); ok {
		a.lines = append(a.lines, data)
		return
	}
	if strings.TrimSpace(trimmedLine) == "" {
		a.Flush(fn)
	}
}

func (a *openAISSEDataAccumulator) Flush(fn func([]byte)) {
	if fn == nil || len(a.lines) == 0 {
		return
	}
	emitOpenAISSEDataPayloads(a.lines, fn)
	a.lines = a.lines[:0]
}

func forEachOpenAISSEDataPayload(body string, fn func([]byte)) {
	if fn == nil || strings.TrimSpace(body) == "" {
		return
	}
	var acc openAISSEDataAccumulator
	for _, line := range strings.Split(body, "\n") {
		acc.AddLine(line, fn)
	}
	acc.Flush(fn)
}

func forEachOpenAISSEFrame(body string, fn func(string, []byte)) {
	if fn == nil || strings.TrimSpace(body) == "" {
		return
	}
	var parser openAICompatSSEFrameParser
	emit := func(frame openAICompatSSEFrame, ok bool) {
		if !ok {
			return
		}
		emitData := func(value string) {
			value = strings.TrimSpace(value)
			if value == "" || value == "[DONE]" {
				return
			}
			data := []byte(value)
			fn(effectiveOpenAISSEEventType(data, frame.EventType), data)
		}
		if gjson.Valid(frame.Data) {
			emitData(frame.Data)
			return
		}
		for _, value := range strings.Split(frame.Data, "\n") {
			emitData(value)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		emit(parser.AddLine(strings.TrimRight(line, "\r")))
	}
	emit(parser.Finish())
}

func emitOpenAISSEDataPayloads(lines []string, fn func([]byte)) {
	if fn == nil || len(lines) == 0 {
		return
	}
	if len(lines) == 1 {
		emitOpenAISSEDataPayload(lines[0], fn)
		return
	}
	joined := strings.Join(lines, "\n")
	if gjson.Valid(joined) {
		emitOpenAISSEDataPayload(joined, fn)
		return
	}
	for _, line := range lines {
		emitOpenAISSEDataPayload(line, fn)
	}
}

func emitOpenAISSEDataPayload(data string, fn func([]byte)) {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return
	}
	fn([]byte(data))
}

// Role and empty text-block announcements are not first semantic output.
func anthropicEventHasOutput(event *apicompat.AnthropicStreamEvent) bool {
	if event.ContentBlock != nil && (event.ContentBlock.Type == "tool_use" || event.ContentBlock.Text != "" || event.ContentBlock.Thinking != "") {
		return true
	}
	return event.Delta != nil && (event.Delta.Text != "" || event.Delta.Thinking != "" || event.Delta.PartialJSON != "")
}

// IsStreamHeartbeatData recognizes only empty keepalives, never content labeled as ping.
func IsStreamHeartbeatData(data string) bool {
	var payload map[string]any
	if json.Unmarshal([]byte(data), &payload) != nil {
		return false
	}
	if len(payload) == 1 && payload["type"] == "ping" {
		return true
	}
	if payload["type"] != "content_block_delta" || len(payload) != 3 {
		return false
	}
	delta, ok := payload["delta"].(map[string]any)
	if !ok || len(delta) != 2 {
		return false
	}
	fields := map[string]string{"text_delta": "text", "thinking_delta": "thinking", "input_json_delta": "partial_json"}
	kind, _ := delta["type"].(string)
	field, known := fields[kind]
	return known && delta[field] == ""
}
