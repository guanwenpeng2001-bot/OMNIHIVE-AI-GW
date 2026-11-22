package restoregate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lib/pq"
	"golang.org/x/sync/singleflight"
)

var ErrMaintenance = errors.New("restore maintenance")
var errFenceLost = unavailableError{errors.New("restore fence lost")}

const (
	admissionTimeout = 5 * time.Second
	healthInterval   = time.Second
)

type unavailableError struct{ err error }

func (e unavailableError) Error() string { return fmt.Sprintf("restore status unavailable: %v", e.err) }
func (e unavailableError) Unwrap() error { return e.err }
func IsUnavailable(err error) bool {
	var target unavailableError
	return errors.As(err, &target)
}

// A long stream holds one reference on a fence shared across the journal pool.
type Gate struct {
	mu        sync.Mutex
	journal   *sql.DB
	held      *fence
	opening   chan struct{}
	bootstrap bool
	now       func() time.Time
}

type fence struct {
	conn        *sql.Conn
	lost        atomic.Bool
	ctx         context.Context
	invalidate  context.CancelFunc
	users       int       // protected by Gate.mu
	clearUntil  time.Time // protected by Gate.mu
	statusGroup singleflight.Group
	healthGroup singleflight.Group
}

func (g *Gate) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

func (g *Gate) hold(ctx context.Context) (*fence, error) {
	for {
		g.mu.Lock()
		if held := g.held; held != nil {
			if held.ctx.Err() != nil {
				g.mu.Unlock()
				return nil, errFenceLost
			}
			held.users++
			g.mu.Unlock()
			return held, nil
		}
		if opening := g.opening; opening != nil {
			g.mu.Unlock()
			select {
			case <-opening:
				continue
			case <-ctx.Done():
				return nil, unavailableError{ctx.Err()}
			}
		}
		g.opening = make(chan struct{})
		g.mu.Unlock()

		held, err := g.openFence(ctx)

		g.mu.Lock()
		if err == nil {
			g.held = held
		}
		close(g.opening)
		g.opening = nil
		g.mu.Unlock()
		if err == nil {
			go held.monitor()
		}
		return held, err
	}
}

func (g *Gate) openFence(ctx context.Context) (*fence, error) {
	// Use a dedicated connection so BEGIN has an admission deadline
	// independent of the lifetime of the shared transaction.
	conn, err := g.journal.Conn(ctx)
	if err != nil {
		return nil, unavailableError{err}
	}
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		discardConn(conn)
		return nil, unavailableError{err}
	}
	var got bool
	err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock_shared(298,3)").Scan(&got)
	if err != nil || !got {
		rollbackFence(ctx, conn)
		if err != nil {
			return nil, unavailableError{err}
		}
		return nil, ErrMaintenance
	}
	lifetime, invalidate := context.WithCancel(context.Background())
	return &fence{conn: conn, ctx: lifetime, invalidate: invalidate, users: 1}, nil
}

func discardConn(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

func rollbackFence(ctx context.Context, conn *sql.Conn) {
	ctx, cancel := context.WithTimeout(ctx, admissionTimeout)
	defer cancel()
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		discardConn(conn)
		return
	}
	_ = conn.Close()
}

func (g *Gate) enter(ctx context.Context) (*fence, error) {
	ctx, cancel := context.WithTimeout(ctx, admissionTimeout)
	defer cancel()
	held, err := g.hold(ctx)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	fresh := g.clock().Before(held.clearUntil)
	g.mu.Unlock()
	if !fresh {
		result := held.statusGroup.DoChan("journal", func() (any, error) {
			g.mu.Lock()
			fresh := g.clock().Before(held.clearUntil)
			g.mu.Unlock()
			if fresh {
				return nil, nil
			}
			checkCtx, cancel := context.WithTimeout(held.ctx, admissionTimeout)
			defer cancel()
			return nil, g.check(checkCtx, held)
		})
		select {
		case state := <-result:
			err = state.Err
		case <-ctx.Done():
			err = unavailableError{ctx.Err()}
		}
	}
	if err == nil && held.ctx.Err() != nil {
		err = errFenceLost
	}
	if err != nil {
		g.leave(ctx, held)
		return nil, err
	}
	return held, nil
}

// Query state on a different connection so a failed journal query cannot abort
// the fencing transaction shared by admitted requests.
func (g *Gate) check(ctx context.Context, held *fence) error {
	var blocked bool
	err := g.journal.QueryRowContext(ctx, `SELECT EXISTS(SELECT FROM restore_journal.operations WHERE target='production' AND
 ((state='running' AND stage IN ('snapshot','applying','verify_counts','tx_apply','tx_verify','committing')) OR stage='manual_recovery' OR state='unknown'))`).Scan(&blocked)
	var pgErr *pq.Error
	bootstrap := g.bootstrap && errors.As(err, &pgErr) && pgErr.Code == "42P01"
	if err != nil && !bootstrap {
		return unavailableError{err}
	}
	if blocked {
		return ErrMaintenance
	}
	g.mu.Lock()
	held.clearUntil = g.clock().Add(time.Second)
	g.mu.Unlock()
	return nil
}

func (held *fence) checkHealth(ctx context.Context) error {
	if held.ctx.Err() != nil {
		return errFenceLost
	}
	if err := ctx.Err(); err != nil {
		return unavailableError{err}
	}
	result := held.healthGroup.DoChan("lock", func() (any, error) {
		probe, cancel := context.WithTimeout(held.ctx, healthInterval)
		defer cancel()
		var locked bool
		err := held.conn.QueryRowContext(probe, `
			SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_locks
			 WHERE pid=pg_backend_pid() AND locktype='advisory' AND classid=298 AND objid=3
			   AND objsubid=2 AND mode='ShareLock' AND granted)`).Scan(&locked)
		if held.ctx.Err() != nil {
			return nil, errFenceLost
		}
		if err != nil || !locked {
			held.lost.Store(true)
			held.invalidate()
			return nil, errFenceLost
		}
		return nil, nil
	})
	select {
	case state := <-result:
		return state.Err
	case <-held.ctx.Done():
		return errFenceLost
	case <-ctx.Done():
		return unavailableError{ctx.Err()}
	}
}

func (held *fence) monitor() {
	tick := time.NewTicker(healthInterval)
	defer tick.Stop()
	for {
		select {
		case <-held.ctx.Done():
			return
		case <-tick.C:
			if held.checkHealth(held.ctx) != nil {
				return
			}
		}
	}
}

func (g *Gate) leave(ctx context.Context, held *fence) {
	g.mu.Lock()
	held.users--
	last := held.users == 0
	if last {
		g.held = nil
	}
	g.mu.Unlock()
	if last {
		held.invalidate()
		rollbackFence(ctx, held.conn)
	}
}

func New(journal *sql.DB) *Gate { return &Gate{journal: journal} }

// NewBusinessPool also admits first-boot migrations before the journal exists.
func NewBusinessPool(journal *sql.DB) *Gate { return &Gate{journal: journal, bootstrap: true} }

// Each statement started by an admitted handler joins its fence generation.
// Detached tasks using a finished handler's context enter afresh.
type requestLease struct {
	gate   *Gate
	fence  *fence
	active bool // protected by gate.mu
}
type leaseKey struct{}

func (g *Gate) Admit(ctx context.Context) (context.Context, func(), error) {
	held, err := g.enter(ctx)
	if err != nil {
		return ctx, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(held.ctx, cancel)
	lease := &requestLease{g, held, true}
	return context.WithValue(ctx, leaseKey{}, lease), sync.OnceFunc(func() {
		stop()
		cancel()
		g.mu.Lock()
		lease.active = false
		g.mu.Unlock()
		g.leave(context.Background(), held)
	}), nil
}

func (g *Gate) Statement(ctx context.Context) (func(), error) {
	if lease, ok := ctx.Value(leaseKey{}).(*requestLease); ok {
		owner, held := lease.gate, lease.fence
		owner.mu.Lock()
		if lease.active {
			held.users++
			owner.mu.Unlock()
			if err := held.checkHealth(ctx); err != nil {
				owner.leave(ctx, held)
				return nil, err
			}
			return func() { owner.leave(context.Background(), held) }, nil
		}
		owner.mu.Unlock()
		if held.lost.Load() {
			return nil, errFenceLost
		}
	}
	held, err := g.enter(ctx)
	if err != nil {
		return nil, err
	}
	if err := held.checkHealth(ctx); err != nil {
		g.leave(ctx, held)
		return nil, err
	}
	return func() { g.leave(context.Background(), held) }, nil
}
