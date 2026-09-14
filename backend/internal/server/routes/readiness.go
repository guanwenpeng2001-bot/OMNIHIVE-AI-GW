package routes

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// NewReadinessHandler checks only probes, independently of Ops being enabled.
// One in-flight check and a two-second cache bound dependency work. A stalled
// driver cannot accumulate goroutines or hold HTTP probes beyond one second.
func NewReadinessHandler(check func(context.Context) bool, accepting func() bool) http.HandlerFunc {
	var mu sync.Mutex
	var checkedAt time.Time
	var healthy bool
	var inFlight chan struct{}
	return func(w http.ResponseWriter, r *http.Request) {
		if !accepting() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		mu.Lock()
		if time.Since(checkedAt) >= 2*time.Second && inFlight == nil {
			inFlight = make(chan struct{})
			done := inFlight
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				ok := check(ctx) && ctx.Err() == nil
				mu.Lock()
				healthy, checkedAt = ok, time.Now()
				inFlight = nil
				close(done)
				mu.Unlock()
			}()
		}
		done := inFlight
		mu.Unlock()
		if done != nil {
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			select {
			case <-done:
			case <-r.Context().Done():
				return
			case <-timer.C:
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
		}
		mu.Lock()
		ok := healthy && time.Since(checkedAt) < 2*time.Second
		mu.Unlock()
		if !ok || !accepting() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
