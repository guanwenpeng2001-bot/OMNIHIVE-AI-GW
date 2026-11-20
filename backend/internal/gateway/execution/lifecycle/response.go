package lifecycle

import "sync/atomic"

type State uint32

const (
	PreOutput State = iota
	HeartbeatOnly
	SemanticOutput
	EmittingTerminal
	Terminal
	TerminalFailed
)

type Response struct {
	state   atomic.Uint32
	partial atomic.Bool
}

func (r *Response) State() State {
	return State(r.state.Load())
}

func (r *Response) Heartbeat() {
	r.state.CompareAndSwap(uint32(PreOutput), uint32(HeartbeatOnly))
}

func (r *Response) Semantic() {
	for current := r.State(); current < SemanticOutput; current = r.State() {
		if r.state.CompareAndSwap(uint32(current), uint32(SemanticOutput)) {
			return
		}
	}
}

// BeginTerminal reserves the one terminal emission before entering the native encoder.
func (r *Response) BeginTerminal() bool {
	for current := r.State(); current < EmittingTerminal; current = r.State() {
		if r.state.CompareAndSwap(uint32(current), uint32(EmittingTerminal)) {
			return true
		}
	}
	return false
}

func (r *Response) FinishTerminal(sent bool) {
	next := TerminalFailed
	if sent {
		next = Terminal
	}
	r.state.CompareAndSwap(uint32(EmittingTerminal), uint32(next))
}

func (r *Response) SetPartial(partial bool) { r.partial.Store(partial) }
func (r *Response) ReplayBlocked() bool {
	return r.partial.Load() || r.State() >= SemanticOutput
}
