package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type scheduledResultsProbe struct {
	listLimit int
	keep      int
}

func (p *scheduledResultsProbe) Create(_ context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error) {
	return result, nil
}

func (p *scheduledResultsProbe) ListByPlanID(_ context.Context, _ int64, limit int) ([]*ScheduledTestResult, error) {
	p.listLimit = limit
	return nil, nil
}

func (p *scheduledResultsProbe) PruneOldResults(_ context.Context, _ int64, keep int) error {
	p.keep = keep
	return nil
}

type scheduledPlanProbe struct{ ScheduledTestPlanRepository }

func (scheduledPlanProbe) Create(_ context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	return plan, nil
}

func (scheduledPlanProbe) Update(_ context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	return plan, nil
}

func TestScheduledTestServiceBoundsPlansAndLegacyResultRetention(t *testing.T) {
	results := &scheduledResultsProbe{}
	svc := NewScheduledTestService(scheduledPlanProbe{}, results)
	const large = 1000000
	plan := &ScheduledTestPlan{CronExpression: "0 * * * *", MaxResults: large}
	created, err := svc.CreatePlan(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, MaxScheduledTestResults, created.MaxResults)
	plan.MaxResults = large
	updated, err := svc.UpdatePlan(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, MaxScheduledTestResults, updated.MaxResults)
	_, err = svc.ListResults(t.Context(), 1, large)
	require.NoError(t, err)
	require.Equal(t, MaxScheduledTestResults, results.listLimit)
	require.NoError(t, svc.SaveResult(t.Context(), 1, large, &ScheduledTestResult{}))
	require.Equal(t, MaxScheduledTestResults, results.keep)
}
