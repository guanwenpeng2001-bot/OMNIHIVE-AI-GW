package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// AccountModelCatalogLocker excludes apply calls across server instances.
// Preview deliberately does not acquire this lock or mutate persistent state.
type AccountModelCatalogLocker interface {
	TryLockAccountModelCatalog(context.Context, int64) (func(), error)
}

type catalogScheduledRefreshKey struct{}

// ModelCatalogFreshness reports age without removing published members.
func (account *Account) ModelCatalogFreshness(now time.Time) (staleness, lastSuccess string) {
	state := account.ModelCatalogState()
	snapshot := account.GetUpstreamModelCatalog()
	if snapshot == nil || snapshot.IdentityRevision != state.IdentityRevision {
		return "unknown", ""
	}
	lastSuccess = state.LastSuccessAt
	if lastSuccess == "" {
		lastSuccess = snapshot.ObservedAt
	}
	observed, err := time.Parse(time.RFC3339Nano, lastSuccess)
	if err != nil {
		return "unknown", ""
	}
	if now.Sub(observed) > 24*time.Hour && (state.Result == "failed" || state.Result == "credentials_invalid") {
		return "stale", lastSuccess
	}
	return "fresh", lastSuccess
}

func accountCatalogRefreshDue(account *Account, now time.Time) bool {
	if account == nil || account.Status != StatusActive {
		return false
	}
	state := account.ModelCatalogState()
	latest := state.AttemptedAt
	if latest == "" {
		if snapshot := account.GetUpstreamModelCatalog(); snapshot != nil {
			latest = snapshot.ObservedAt
		}
	}
	at, err := time.Parse(time.RFC3339Nano, latest)
	// Stable per-account jitter spreads work across a 30 minute window on all replicas.
	interval := 6*time.Hour + time.Duration((account.ID*2654435761)%1801-900)*time.Second
	return err != nil || !now.Before(at.Add(interval))
}

func (s *ScheduledTestRunnerService) runCatalogRefresh() {
	if s.accountTestSvc == nil || !s.catalogRefreshMu.TryLock() {
		return
	}
	defer s.catalogRefreshMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	s.refreshDueCatalogs(ctx, time.Now())
}

func (s *ScheduledTestRunnerService) refreshDueCatalogs(ctx context.Context, now time.Time) {
	accounts, err := s.accountTestSvc.accountRepo.ListActive(ctx)
	if err != nil {
		logger.LegacyPrintf("service.model_catalog_refresh", "list accounts failed: %v", err)
		return
	}
	// Discovery holds one transaction connection and CAS needs another.
	workers := 2
	if s.cfg != nil && s.cfg.Database.MaxOpenConns > 0 {
		workers = min(workers, s.cfg.Database.MaxOpenConns/2)
	}
	if workers == 0 {
		return
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	defer wg.Wait()
	for i := range accounts {
		account := &accounts[i]
		if !accountCatalogRefreshDue(account, now) {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		wg.Add(1)
		go func(account *Account) {
			defer wg.Done()
			defer func() { <-sem }()
			attemptCtx, cancel := context.WithTimeout(context.WithValue(ctx, catalogScheduledRefreshKey{}, true), 2*time.Minute)
			defer cancel()
			_, err := s.accountTestSvc.ApplyAccountModelCatalog(attemptCtx, account, nil)
			if err != nil && !errors.Is(err, ErrAccountModelCatalogConflict) {
				staleness, _ := account.ModelCatalogFreshness(time.Now())
				// Log only a stable error code, never upstream responses or credentials.
				logger.LegacyPrintf("service.model_catalog_refresh", "account=%d refresh_failed previous_staleness=%s", account.ID, staleness)
			}
		}(account)
	}
}
