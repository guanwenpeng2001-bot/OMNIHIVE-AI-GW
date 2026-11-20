package restore

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"sync"
	"time"
)

// One journal lock per active HTTP process. A long stream must not consume a
// journal connection per request. The worker drains every holder before snapshot.
type requestGate struct {
	mu      sync.Mutex
	journal *sql.DB
	held    *sql.Tx
	users   int
}

func (g *requestGate) hold(ctx context.Context) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held == nil {
		tx, err := g.journal.BeginTx(context.Background(), nil)
		if err != nil {
			return false
		}
		var got bool
		if tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock_shared(298,3)").Scan(&got) != nil || !got {
			tx.Rollback()
			return false
		}
		g.held = tx
	}
	g.users++
	return true
}
func (g *requestGate) enter(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !g.hold(ctx) {
		return false
	}
	// Use a pooled connection for the read. A cancelled request must not
	// invalidate the transaction fencing the other admitted handlers.
	var blocked bool
	err := g.journal.QueryRowContext(ctx, `SELECT EXISTS(SELECT FROM restore_journal.operations WHERE target='production' AND
 ((state='running' AND stage IN ('snapshot','applying','verify_counts','tx_apply','tx_verify','committing')) OR stage='manual_recovery' OR state='unknown'))`).Scan(&blocked)
	if err != nil || blocked {
		g.leave()
		return false
	}
	return true
}

func (g *requestGate) leave() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.users--
	if g.users == 0 {
		g.held.Rollback()
		g.held = nil
	}
}

func (s *Service) Maintenance(next http.Handler) http.Handler {
	if s.Journal == nil {
		return next
	}
	gate := requestGate{journal: s.Journal}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" && (r.Method == "GET" || r.Method == "HEAD") {
			next.ServeHTTP(w, r)
			return
		}
		if !gate.enter(r.Context()) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"code":503,"reason":"RESTORE_MAINTENANCE","message":"Production restore maintenance. Retry after recovery."}`)
			return
		}
		defer gate.leave()
		next.ServeHTTP(w, r)
	})
}
func (s *Service) drainRequests(ctx context.Context) error {
	_, err := s.Journal.ExecContext(ctx, "SELECT pg_advisory_xact_lock(298,3)")
	return err
}
