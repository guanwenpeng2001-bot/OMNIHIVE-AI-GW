package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

var ErrRealtimeDraining = errors.New("application is draining")

type realtimeRegistration struct{ cancel context.CancelFunc }

// Registration and shutdown share one lock: after BeginDrain returns no new
// session, commit or poll can be admitted. Accepted work remains joined.
func (s *UpstreamJobService) RegisterQianwenRealtime(parent context.Context) (context.Context, func(), error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.draining {
		return nil, nil, ErrRealtimeDraining
	}
	ctx, cancel := context.WithCancel(parent)
	registration := &realtimeRegistration{cancel: cancel}
	if s.sessions == nil {
		s.sessions = make(map[*realtimeRegistration]struct{})
	}
	s.sessions[registration] = struct{}{}
	s.active.Add(1)
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			cancel()
			s.lifecycleMu.Lock()
			delete(s.sessions, registration)
			s.lifecycleMu.Unlock()
			s.active.Done()
		})
	}, nil
}

func (s *UpstreamJobService) beginWork() (func(), error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.draining {
		return nil, ErrRealtimeDraining
	}
	s.active.Add(1)
	return s.active.Done, nil
}

func (s *UpstreamJobService) Ready() bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	return !s.draining
}

func (s *UpstreamJobService) BeginDrain() {
	s.lifecycleMu.Lock()
	if s.draining {
		s.lifecycleMu.Unlock()
		return
	}
	s.draining = true
	for registration := range s.sessions {
		registration.cancel()
	}
	stopPolling := s.stopPolling
	s.lifecycleMu.Unlock()
	if stopPolling != nil {
		stopPolling()
	}
}

func (s *UpstreamJobService) WaitDrain(ctx context.Context) error {
	done := make(chan struct{})
	go func() { s.active.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		slog.Error("billing drain timed out", "component", "billing", "failure_category", "drain_timeout")
		return ctx.Err()
	}
}
