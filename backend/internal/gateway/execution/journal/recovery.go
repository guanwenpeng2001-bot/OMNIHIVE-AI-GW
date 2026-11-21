package journal

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// RecoverAbandoned marks sends whose owning process died or whose receipt
// persistence lost its database connection. The existing reconciliation view
// remains the authoritative needs_review source.
func (s *Postgres) RecoverAbandoned(ctx context.Context) error {
	if s == nil || s.db == nil {
		return ErrDispatch
	}
	// Publish a recovery boundary without changing the acceptance evidence.
	// The view removes provisional cases when a live peer supplies its receipt.
	_, err := s.db.ExecContext(ctx, `INSERT INTO gateway_execution_recovery(singleton,observed_at)
 VALUES(true, clock_timestamp()) ON CONFLICT(singleton) DO UPDATE
 SET observed_at=GREATEST(gateway_execution_recovery.observed_at,excluded.observed_at)`)
	return err
}

type RecoveryStore interface {
	RecoverAbandoned(context.Context) error
	Ping(context.Context) error
}

func (s *Postgres) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

type Recovery struct {
	Store      RecoveryStore
	Interval   time.Duration
	mu         sync.Mutex
	recovering bool
}

func (r *Recovery) Run(ctx context.Context) error {
	if r == nil || r.Store == nil {
		return errors.New("execution recovery store unavailable")
	}
	r.mu.Lock()
	if r.recovering {
		r.mu.Unlock()
		return nil
	}
	r.recovering = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.recovering = false
		r.mu.Unlock()
	}()
	return r.Store.RecoverAbandoned(ctx)
}

func (r *Recovery) Start(ctx context.Context) {
	err := r.Run(ctx)
	for ctx.Err() == nil {
		interval := r.Interval
		if interval <= 0 {
			interval = time.Minute
		}
		if err != nil && ctx.Err() == nil {
			slog.Error("gateway execution recovery unavailable", "component", "billing", "error", err)
			interval = min(interval, time.Second)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err != nil {
				err = r.Run(ctx)
			} else {
				err = r.Store.Ping(ctx)
			}
		}
	}
}
