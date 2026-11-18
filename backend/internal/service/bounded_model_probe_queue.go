package service

import (
	"context"
	"errors"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
)

// SetBoundedModelProbesEnabled is an explicit pre-Start opt-in. Catalog refresh
// and ordinary scheduled tests do not implicitly authorize billable probes.
func (runner *ScheduledTestRunnerService) SetBoundedModelProbesEnabled(enabled bool) {
	runner.boundedModelProbesEnabled = enabled
}

func (runner *ScheduledTestRunnerService) runBoundedModelProbeQueue() {
	if !runner.boundedModelProbesEnabled || runner.accountTestSvc == nil || !runner.boundedModelProbesMu.TryLock() {
		return
	}
	defer runner.boundedModelProbesMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	runner.processBoundedModelProbeQueue(ctx, time.Now().UTC())
}

func (runner *ScheduledTestRunnerService) processBoundedModelProbeQueue(ctx context.Context, now time.Time) {
	accounts, err := runner.accountTestSvc.accountRepo.ListActive(ctx)
	if err != nil {
		logger.LegacyPrintf("service.bounded_model_probe", "list_accounts_failed")
		return
	}
	for index := range accounts {
		if ctx.Err() != nil {
			return
		}
		if len(boundedModelProbeTargets(&accounts[index], now)) == 0 {
			continue
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := runner.accountTestSvc.runBoundedModelProbes(attemptCtx, accounts[index].ID, now)
		cancel()
		if err != nil && !errors.Is(err, ErrModelProbeBudgetExceeded) {
			// Never log upstream bodies, request URLs or credentials.
			logger.LegacyPrintf("service.bounded_model_probe", "account=%d probe_deferred", accounts[index].ID)
		}
	}
}
