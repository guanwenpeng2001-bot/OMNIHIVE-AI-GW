//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type realtimeDueRepo struct{ *fileBatchMemoryRepo }

func (r *realtimeDueRepo) Due(_ context.Context, now time.Time, limit int) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var jobs []*UpstreamJob
	for _, j := range r.jobs {
		if !j.Terminal() && !j.NextPollAt.After(now) {
			jobs = append(jobs, j)
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].NextPollAt.Equal(jobs[j].NextPollAt) {
			return jobs[i].PublicRequestID < jobs[j].PublicRequestID
		}
		return jobs[i].NextPollAt.Before(jobs[j].NextPollAt)
	})
	var ids []string
	for _, j := range jobs {
		if len(ids) == limit {
			break
		}
		ids = append(ids, j.PublicRequestID)
	}
	return ids, nil
}

func TestQianwenRealtimeFairRecoveryAndRetryBudget(t *testing.T) {
	now := time.Now().UTC()
	repo := &realtimeDueRepo{&fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}}
	for i := 0; i < 21; i++ {
		j := realtimeSessionFixture()
		j.Family = "realtime_turn"
		j.Status = "running"
		j.PublicRequestID = fmt.Sprintf("%02d", i)
		j.UpstreamTaskID = j.PublicRequestID
		j.UsageJSON = []byte(`{"characters":25}`)
		j.NextPollAt = now
		repo.jobs[j.PublicRequestID] = j
	}
	svc := NewUpstreamJobService(repo, nil, func(j *UpstreamJob) (*UsageLog, *UsageBillingCommand, error) {
		if j.PublicRequestID != "20" {
			return nil, nil, errors.New("injected unavailable billing")
		}
		return QianwenRealtimeCharge(j)
	})
	svc.now = func() time.Time { return now }
	require.Error(t, svc.PollDue(context.Background(), 20))
	require.NoError(t, svc.PollDue(context.Background(), 20))
	require.NotNil(t, repo.jobs["20"].ChargedUsageLogID, "healthy receipt advances on second scan")
	for attempt := 1; attempt < 20; attempt++ {
		next := repo.jobs["00"].NextPollAt
		require.LessOrEqual(t, next.Sub(now), 320*time.Second)
		now = next
		require.Error(t, svc.PollDue(context.Background(), 20))
	}
	for id, j := range repo.jobs {
		if id == "20" {
			continue
		}
		require.Equal(t, 20, j.AttemptCount)
		require.Equal(t, "settlement_needs_review", j.ErrorCode)
		require.Nil(t, j.ChargedUsageLogID)
		require.NotEmpty(t, j.UsageJSON)
	}
	ids, err := repo.Due(context.Background(), now.Add(48*time.Hour), 20)
	require.NoError(t, err)
	require.Empty(t, ids)
	require.Equal(t, 1, repo.charges)
}

func TestQianwenRealtimeRetryAgeStartsAtFirstAttempt(t *testing.T) {
	now := time.Now().UTC()
	j := realtimeSessionFixture()
	j.Family = "realtime_turn"
	j.UpstreamTaskID = "response"
	j.UsageJSON = []byte(`{"characters":25}`)
	j.SubmittedAt = now.Add(-48 * time.Hour)
	repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{j.PublicRequestID: j}, failCharge: true}
	svc := NewUpstreamJobService(repo, nil, QianwenRealtimeCharge)
	svc.now = func() time.Time { return now }
	_, err := svc.Poll(context.Background(), j.PublicRequestID)
	require.Error(t, err)
	stored := repo.jobs[j.PublicRequestID]
	require.Equal(t, now, *stored.LastPolledAt)
	require.Equal(t, "settlement_pending", stored.ErrorCode)
	now = now.Add(24 * time.Hour)
	stored, err = svc.Poll(context.Background(), j.PublicRequestID)
	require.NoError(t, err)
	require.Equal(t, "settlement_needs_review", stored.ErrorCode)
	require.Equal(t, 1, stored.AttemptCount)
}

func TestQianwenRealtimeStopAdmissionAndJoin(t *testing.T) {
	svc := NewUpstreamJobService(nil, nil, nil)
	ctx, release, err := svc.RegisterQianwenRealtime(context.Background())
	require.NoError(t, err)
	svc.BeginDrain()
	require.False(t, svc.Ready())
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	_, _, err = svc.RegisterQianwenRealtime(context.Background())
	require.ErrorIs(t, err, ErrRealtimeDraining)
	require.ErrorIs(t, svc.MarkQianwenRealtimePending(context.Background(), "new"), ErrRealtimeDraining)
	_, err = svc.Poll(context.Background(), "new")
	require.ErrorIs(t, err, ErrRealtimeDraining)
	wait, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.WaitDrain(wait), context.DeadlineExceeded)
	release()
	release()
	require.NoError(t, svc.WaitDrain(context.Background()))
}

func TestQianwenRealtimeErrorFramesDoNotLeak(t *testing.T) {
	raw := []byte(`{"type":"error","event_id":"secret-marker","error":{"message":"Bearer secret-marker https://internal/?key=secret-marker","code":"secret-marker","debug":"secret-marker"}}`)
	require.NotContains(t, string(sanitizeQianwenRealtimeError(raw)), "secret-marker")
	for _, raw := range []string{`{"type":"response.audio.delta","delta":"secret-marker"}`, `{"type":"response.done","response":{"usage":{"characters":25}}}`} {
		require.Equal(t, raw, string(sanitizeQianwenRealtimeError([]byte(raw))))
	}
}

// Inject a competing success after the failed money transaction releases its
// lock but before failure state is recorded.
type realtimeLateFailureRepo struct {
	*fileBatchMemoryRepo
	injected bool
}

func (r *realtimeLateFailureRepo) WithLocked(ctx context.Context, id string, fn func(*UpstreamJob, UpstreamJobTx) error) (*UpstreamJob, error) {
	j, err := r.fileBatchMemoryRepo.WithLocked(ctx, id, fn)
	if err != nil && !r.injected {
		r.injected = true
		r.mu.Lock()
		stored := r.jobs[id]
		anchor := int64(77)
		stored.ChargedUsageLogID = &anchor
		stored.Status = "succeeded"
		stored.ErrorCode = ""
		r.mu.Unlock()
	}
	return j, err
}
func TestQianwenRealtimeLateFailurePreservesSuccessAnchor(t *testing.T) {
	j := realtimeSessionFixture()
	j.Family = "realtime_turn"
	j.UpstreamTaskID = "response"
	j.UsageJSON = []byte(`{"characters":25}`)
	repo := &realtimeLateFailureRepo{fileBatchMemoryRepo: &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{j.PublicRequestID: j}, failCharge: true}}
	svc := NewUpstreamJobService(repo, nil, QianwenRealtimeCharge)
	stored, err := svc.Poll(context.Background(), j.PublicRequestID)
	require.Error(t, err)
	require.Equal(t, "succeeded", stored.Status)
	require.Equal(t, int64(77), *stored.ChargedUsageLogID)
	require.Empty(t, stored.ErrorCode)
}

func TestQianwenRealtimeMissingReceiptRemainsExplicitlyPending(t *testing.T) {
	j := realtimeSessionFixture()
	repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{j.PublicRequestID: j}}
	svc := NewUpstreamJobService(repo, nil, QianwenRealtimeCharge)
	require.NoError(t, svc.MarkQianwenRealtimePending(context.Background(), j.PublicRequestID))
	svc.BeginDrain()
	require.NoError(t, svc.FinishQianwenRealtime(context.Background(), j.PublicRequestID, "usage_pending"))
	stored := repo.jobs[j.PublicRequestID]
	require.Equal(t, "usage_pending", stored.ErrorCode)
	require.Nil(t, stored.ChargedUsageLogID)
	require.JSONEq(t, "null", string(stored.UsageJSON))
	require.NotEqual(t, "succeeded", stored.Status)
}
