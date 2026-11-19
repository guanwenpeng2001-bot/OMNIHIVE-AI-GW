package runtime

import (
	"context"
	"net"
	"net/http"
	"sync"
)

// net/http.Shutdown does not join hijacked handlers. Track the handler lifetime
// including receipt writes before return.
type Admission struct {
	mu       sync.Mutex
	draining bool
	ctx      context.Context
	cancel   context.CancelFunc
	active   sync.WaitGroup
	hijacked map[net.Conn]struct{}
}

func NewAdmission() *Admission {
	ctx, cancel := context.WithCancel(context.Background())
	return &Admission{ctx: ctx, cancel: cancel, hijacked: map[net.Conn]struct{}{}}
}
func (a *Admission) BeginDrain() { a.mu.Lock(); a.draining = true; a.cancel(); a.mu.Unlock() }
func (a *Admission) Ready() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.draining }
func (a *Admission) Wait()       { a.active.Wait() }

type connKey struct{}

func (a *Admission) ConnContext(ctx context.Context, c net.Conn) context.Context {
	return context.WithValue(ctx, connKey{}, c)
}
func (a *Admission) ConnState(c net.Conn, state http.ConnState) {
	if state == http.StateHijacked {
		a.mu.Lock()
		a.hijacked[c] = struct{}{}
		a.mu.Unlock()
	}
}
func (a *Admission) CloseHijacked() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for c := range a.hijacked {
		_ = c.Close()
	}
}
func (a *Admission) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		if a.draining {
			a.mu.Unlock()
			http.Error(w, "draining", http.StatusServiceUnavailable)
			return
		}
		a.active.Add(1)
		a.mu.Unlock()
		defer a.active.Done()
		ctx, cancel := context.WithCancel(r.Context())
		stop := context.AfterFunc(a.ctx, cancel)
		defer func() {
			stop()
			cancel()
			if c, ok := ctx.Value(connKey{}).(net.Conn); ok {
				a.mu.Lock()
				delete(a.hijacked, c)
				a.mu.Unlock()
			}
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *Admission) Acquire() (context.Context, func(), bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.draining {
		return nil, nil, false
	}
	a.active.Add(1)
	return a.ctx, a.active.Done, true
}
