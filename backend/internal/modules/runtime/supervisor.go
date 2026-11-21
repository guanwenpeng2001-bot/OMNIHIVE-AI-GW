package runtime

import (
	"context"
	"sync"
	"time"
)

// A supervisor belongs to exactly one application graph. Register all loops
// during composition. Start and Stop are idempotent, including Stop before Start.
type Supervisor struct {
	Mode             Mode
	mu               sync.Mutex
	runs             []func(context.Context)
	started, stopped bool
	cancel           context.CancelFunc
	done             chan struct{}
	wg               sync.WaitGroup
}

func New(mode Mode) *Supervisor { return &Supervisor{Mode: mode, done: make(chan struct{})} }
func (s *Supervisor) Add(run func(context.Context)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		panic("worker registered after startup")
	}
	s.runs = append(s.runs, run)
}
func (s *Supervisor) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		return
	}
	s.started = true
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	for _, run := range s.runs {
		s.wg.Add(1)
		go func() { defer s.wg.Done(); run(ctx) }()
	}
}
func (s *Supervisor) Stop() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped {
		s.stopped = true
		if s.cancel != nil {
			s.cancel()
		}
		go func() { s.wg.Wait(); close(s.done) }()
	}
	return s.done
}

// Every runs immediately, then waits an interval between attempts. A slow sweep
// cannot overlap itself. The module owns its durable claim and retry rules.
func Every(ctx context.Context, interval time.Duration, run func(context.Context)) {
	for ctx.Err() == nil {
		run(ctx)
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// WorkerStart delays legacy service startup until the listener and dependencies
// are ready. The Wire cleanup for the shared service owns its Stop call.
func (s *Supervisor) WorkerStart(start func()) {
	if s.Mode.Workers() {
		s.Add(func(context.Context) { start() })
	}
}
