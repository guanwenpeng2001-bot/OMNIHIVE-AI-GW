package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lifecycleJobRepository struct {
	UpstreamJobRepository
	job                     *UpstreamJob
	persistenceContextError error
}

func (r *lifecycleJobRepository) Create(_ context.Context, job *UpstreamJob) (*UpstreamJob, bool, error) {
	if r.job != nil {
		return r.job, false, nil
	}
	copy := *job
	r.job = &copy
	return r.job, true, nil
}

func (r *lifecycleJobRepository) WithLocked(ctx context.Context, _ string, update func(*UpstreamJob, UpstreamJobTx) error) (*UpstreamJob, error) {
	r.persistenceContextError = ctx.Err()
	return r.job, update(r.job, nil)
}

func TestJobSubmissionPlanCancellationDeadlineAndNoBlindResubmit(t *testing.T) {
	for _, outcome := range []string{"cancel", "deadline", "accepted"} {
		t.Run(outcome, func(t *testing.T) {
			repository := &lifecycleJobRepository{}
			jobs := NewUpstreamJobService(repository, nil, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			calls := 0
			job := &UpstreamJob{UserID: 1, APIKeyID: 2, AccountID: 3, Family: "video", Billing: UpstreamJobBilling{Model: "upstream", RequestedModel: "public", UnitPrice: 1, RateMultiplier: 1}}
			submit := func(network context.Context) (UpstreamJobUpdate, error) {
				calls++
				plan := GatewayRequestPlanFromContext(network)
				require.NotNil(t, plan)
				require.Equal(t, repository.job.PublicRequestID, plan.Snapshot().ExecutionID)
				require.Equal(t, "job", plan.Snapshot().ExecutionKind)
				_, bounded := network.Deadline()
				require.True(t, bounded)
				if outcome == "accepted" {
					return UpstreamJobUpdate{TaskID: "provider-task", Status: "running"}, nil
				}
				if outcome == "cancel" {
					cancel()
				}
				<-network.Done()
				return UpstreamJobUpdate{}, network.Err()
			}
			result, err := jobs.Submit(ctx, job, []byte(`{"prompt":"image"}`), submit)
			require.NoError(t, err)
			require.NoError(t, repository.persistenceContextError)
			if outcome == "accepted" {
				require.Equal(t, "provider-task", result.UpstreamTaskID)
				require.Empty(t, result.ErrorCode)
			} else {
				require.Equal(t, "submission_unknown", result.ErrorCode)
				require.Equal(t, "failed", result.Status)
				require.NotNil(t, result.FinishedAt)
			}
			duplicate, err := jobs.Submit(context.Background(), job, []byte(`{"prompt":"image"}`), submit)
			require.NoError(t, err)
			require.Equal(t, result.PublicRequestID, duplicate.PublicRequestID)
			require.Equal(t, 1, calls)
		})
	}
}
