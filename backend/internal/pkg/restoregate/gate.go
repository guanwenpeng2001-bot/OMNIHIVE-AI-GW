package restoregate

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/lib/pq"
)

// Share a journal lock across concurrent users of a pool. A long stream must not consume a
// journal connection per request. The worker drains every holder before snapshot.
type Gate struct {
	mu        sync.Mutex
	journal   *sql.DB
	held      *sql.Tx
	users     int
	bootstrap bool
}

func (g *Gate) hold(ctx context.Context) bool {
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
func (g *Gate) Enter(ctx context.Context) bool {
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
	// On first boot the business DB migrates before the journal schema exists.
	// No restore can be claimed yet. Retain the shared fence across bootstrap.
	var pgErr *pq.Error
	if g.bootstrap && errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		return true
	}
	if err != nil || blocked {
		g.Leave()
		return false
	}
	return true
}

func (g *Gate) Leave() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.users--
	if g.users == 0 {
		g.held.Rollback()
		g.held = nil
	}
}

func New(journal *sql.DB) *Gate { return &Gate{journal: journal} }

// NewBusinessPool also admits first-boot migrations before the journal exists.
func NewBusinessPool(journal *sql.DB) *Gate { return &Gate{journal: journal, bootstrap: true} }

// AdmitRequest lets statements started by an admitted handler finish during
// drain. A detached task using the context after the handler returns must enter
// again. Each statement already running keeps its own reference to the fence.
type requestLease struct {
	gate   *Gate
	active bool // protected by gate.mu
}
type leaseKey struct{}

func (g *Gate) AdmitRequest(ctx context.Context) (context.Context, func(), bool) {
	if !g.Enter(ctx) {
		return ctx, nil, false
	}
	lease := &requestLease{g, true}
	return context.WithValue(ctx, leaseKey{}, lease), func() {
		g.mu.Lock()
		lease.active = false
		g.mu.Unlock()
		g.Leave()
	}, true
}

func (g *Gate) AdmitStatement(ctx context.Context) (func(), bool) {
	if lease, ok := ctx.Value(leaseKey{}).(*requestLease); ok {
		owner := lease.gate
		owner.mu.Lock()
		if lease.active {
			owner.users++
			owner.mu.Unlock()
			return owner.Leave, true
		}
		owner.mu.Unlock()
	}
	if !g.Enter(ctx) {
		return nil, false
	}
	return g.Leave, true
}
