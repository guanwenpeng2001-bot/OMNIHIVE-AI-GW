package protocol

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
)

type attemptCommitWriter struct {
	gin.ResponseWriter
	response        *lifecycle.Response
	isHeartbeatData func(string) bool
	context         *gin.Context
	line            string
	event           string
	skipLine        bool
	skipLF          bool
	nativeTerminal  bool
}

func (w *attemptCommitWriter) Write(p []byte) (int, error) {
	terminal := w.observe(string(p))
	n, err := w.ResponseWriter.Write(p)
	w.finishWrite(terminal, n == len(p) && err == nil)
	return n, err
}

func (w *attemptCommitWriter) WriteString(s string) (int, error) {
	terminal := w.observe(s)
	n, err := w.ResponseWriter.WriteString(s)
	w.finishWrite(terminal, n == len(s) && err == nil)
	return n, err
}

func (w *attemptCommitWriter) finishWrite(terminalEnd, success bool) {
	if !success && terminalSSEEvent(w.event) {
		w.response.BeginTerminal()
	}
	if w.response.State() == lifecycle.EmittingTerminal && (!success || terminalEnd) {
		w.response.FinishTerminal(success)
		w.nativeTerminal = false
	}
}

func (w *attemptCommitWriter) Flush() {
	w.ResponseWriter.Flush()
	w.response.Heartbeat()
}

func (w *attemptCommitWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeaderNow()
	// The upgrade handshake precedes the first logical WebSocket turn.
	if w.Status() != http.StatusSwitchingProtocols {
		w.response.Heartbeat()
	}
}

func terminalSSEEvent(event string) bool {
	switch event {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "message_stop", "error":
		return true
	}
	return false
}

func (w *attemptCommitWriter) classifyLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		w.event = ""
		return w.nativeTerminal
	}
	if name, ok := strings.CutPrefix(line, "event:"); ok {
		w.event = strings.TrimSpace(name)
		return false
	}
	if strings.HasPrefix(line, ":") || strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "retry:") {
		return false
	}
	if data, ok := strings.CutPrefix(line, "data:"); ok {
		data = strings.TrimSpace(data)
		if terminalSSEEvent(w.event) || data == "[DONE]" || terminalSSEEvent(gjson.Get(data, "type").String()) {
			if w.response.BeginTerminal() {
				w.nativeTerminal = true
			}
			return false
		}
		if w.response.State() >= lifecycle.SemanticOutput || w.isHeartbeatData(data) {
			return false
		}
	}
	w.response.Semantic()
	return false
}

func (w *attemptCommitWriter) observe(content string) bool {
	if content == "" || (w.response.State() >= lifecycle.EmittingTerminal && !w.nativeTerminal) {
		return false
	}
	w.response.Heartbeat()
	if !strings.HasPrefix(strings.ToLower(w.Header().Get("Content-Type")), "text/event-stream") {
		if strings.TrimSpace(content) == "" {
			return false
		}
		w.context.Set("gateway_stream_only_heartbeats", false)
		return w.response.BeginTerminal()
	}

	wasSemantic := w.response.ReplayBlocked()
	terminal := false
	for len(content) > 0 {
		if w.skipLF {
			content = strings.TrimPrefix(content, "\n")
			w.skipLF = false
			if content == "" {
				break
			}
		}
		i := strings.IndexAny(content, "\r\n")
		fragment := content
		if i >= 0 {
			w.skipLF = content[i] == '\r'
			fragment, content = content[:i], content[i+1:]
		} else {
			content = ""
		}
		if !w.skipLine {
			if w.line == "" && strings.HasPrefix(fragment, ":") {
				w.skipLine = true
			} else if i >= 0 || len(w.line)+len(fragment) > 4096 {
				// Parse complete lines in place. Bound retained bytes for fragmented payloads.
				terminal = w.classifyLine(w.line+fragment) || terminal
				w.line = ""
				w.skipLine = i < 0
			} else {
				w.line += fragment
			}
		}
		if i >= 0 {
			w.skipLine = false
		}
	}
	w.response.SetPartial(strings.TrimSpace(w.line) != "")
	if wasSemantic != w.response.ReplayBlocked() {
		w.context.Set("gateway_stream_only_heartbeats", !w.response.ReplayBlocked())
	}
	return terminal
}

func TrackResponseWriter(c *gin.Context, response *lifecycle.Response, isHeartbeatData func(string) bool) gin.ResponseWriter {
	return &attemptCommitWriter{
		ResponseWriter: c.Writer, response: response, context: c, isHeartbeatData: isHeartbeatData,
	}
}
