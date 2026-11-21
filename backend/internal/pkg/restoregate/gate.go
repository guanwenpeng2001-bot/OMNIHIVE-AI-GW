package restoregate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lib/pq"
	"golang.org/x/sync/singleflight"
)

var ErrMaintenance = errors.New("restore maintenance")

type unavailableError struct{ err error }

func (e unavailableError) Error() string { return fmt.Sprintf("restore status unavailable: %v", e.err) }
func (e unavailableError) Unwrap() error { return e.err }
func IsUnavailable(err error) bool {
	var target unavailableError
	return errors.As(err, &target)
}

// Share a journal lock across concurrent users of a pool. A long stream must not consume a
// journal connection per request. The worker drains every holder before snapshot.
type Gate struct {
	mu          sync.Mutex
	journal     *sql.DB
	held        *sql.Tx
	users       int
	bootstrap   bool
	clearUntil  time.Time
	statusGroup singleflight.Group
	now         func() time.Time
	beforeHold  func()
}

func (g *Gate) hold(ctx context.Context) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held != nil {
		g.users++
		return false, nil
	}
	tx, err := g.journal.BeginTx(context.Background(), nil)
	if err != nil {
		return false, unavailableError{err}
	}
	var got bool
	if err = tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock_shared(298,3)").Scan(&got); err != nil {
		_ = tx.Rollback()
		return false, unavailableError{err}
	}
	if !got {
		_ = tx.Rollback()
		return false, ErrMaintenance
	}
	g.held = tx
	g.users++
	return true, nil
}

func (g *Gate) Enter(ctx context.Context) bool { return g.enter(ctx) == nil }

func (g *Gate) enter(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	now := time.Now()
	if g.now != nil {
		now = g.now()
	}
	g.mu.Lock()
	cached := g.held != nil && now.Before(g.clearUntil)
	g.mu.Unlock()
	if g.beforeHold != nil {
		g.beforeHold()
		g.mu.Lock()
		cached = g.held != nil && now.Before(g.clearUntil)
		g.mu.Unlock()
	}
	if cached {
		created, err := g.hold(ctx)
		if err != nil || !created {
			return err
		}
		return g.refresh(ctx)
	}
	value, err, _ := g.statusGroup.Do("status", func() (any, error) {
		now := time.Now()
		if g.now != nil {
			now = g.now()
		}
		g.mu.Lock()
		cached := g.held != nil && now.Before(g.clearUntil)
		g.mu.Unlock()
		if !cached {
			if err := g.refresh(ctx); err != nil {
				return nil, err
			}
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if refreshed, _ := value.(bool); refreshed {
		return nil
	}
	created, err := g.hold(ctx)
	if err != nil || !created {
		return err
	}
	return g.refresh(ctx)
}

func (g *Gate) refresh(ctx context.Context) error {
	_, err := g.hold(ctx)
	if err != nil {
		return err
	}
	var blocked bool
	// Use a pooled connection for the read. A cancelled request must not
	// invalidate the transaction fencing the other admitted handlers.
	err = g.journal.QueryRowContext(ctx, `SELECT EXISTS(SELECT FROM restore_journal.operations WHERE target='production' AND
 ((state='running' AND stage IN ('snapshot','applying','verify_counts','tx_apply','tx_verify','committing')) OR stage='manual_recovery' OR state='unknown'))`).Scan(&blocked)
	var pgErr *pq.Error
	bootstrap := g.bootstrap && errors.As(err, &pgErr) && pgErr.Code == "42P01"
	if (err != nil && !bootstrap) || blocked {
		g.Leave()
		if blocked {
			return ErrMaintenance
		}
		return unavailableError{err}
	}
	g.mu.Lock()
	now := time.Now()
	if g.now != nil {
		now = g.now()
	}
	g.clearUntil = now.Add(time.Second)
	g.mu.Unlock()
	return nil
}

func (g *Gate) Leave() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.users--
	if g.users == 0 {
		_ = g.held.Rollback()
		g.held = nil
		g.clearUntil = time.Time{}
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

func newRequestLease(g *Gate, ctx context.Context) (context.Context, func()) {
	lease := &requestLease{g, true}
	return context.WithValue(ctx, leaseKey{}, lease), func() {
		g.mu.Lock()
		lease.active = false
		g.mu.Unlock()
		g.Leave()
	}
}

func (g *Gate) AdmitRequest(ctx context.Context) (context.Context, func(), bool) {
	ctx, leave, err := g.Admit(ctx)
	return ctx, leave, err == nil
}

func (g *Gate) Admit(ctx context.Context) (context.Context, func(), error) {
	if err := g.enter(ctx); err != nil {
		return ctx, nil, err
	}
	ctx, leave := newRequestLease(g, ctx)
	return ctx, leave, nil
}

func (g *Gate) AdmitStatement(ctx context.Context) (func(), bool) {
	leave, err := g.Statement(ctx)
	return leave, err == nil
}

func (g *Gate) Statement(ctx context.Context) (func(), error) {
	if lease, ok := ctx.Value(leaseKey{}).(*requestLease); ok {
		owner := lease.gate
		owner.mu.Lock()
		if lease.active {
			owner.users++
			owner.mu.Unlock()
			return owner.Leave, nil
		}
		owner.mu.Unlock()
	}
	if err := g.enter(ctx); err != nil {
		return nil, err
	}
	return g.Leave, nil
}
