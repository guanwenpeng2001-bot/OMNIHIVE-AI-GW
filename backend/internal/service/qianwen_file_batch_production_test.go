//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const paidBatchLine = `{"custom_id":"one","method":"POST","url":"/v1/chat/completions","body":{"model":"qwen-long","messages":[{"role":"user","content":"Hello"}],"enable_thinking":false}}`

func paidBatchFixture(t *testing.T) (*UpstreamJob, *Account) {
	t.Helper()
	_, f, err := ValidateQianwenBatchFile([]byte(paidBatchLine))
	require.NoError(t, err)
	a := qianwenTestAccount()
	a.Status = StatusActive
	a.Extra = map[string]any{"qianwen_pricing_sync": map[string]any{"region": "asia-pacific-china", "usd_per_cny": 1.0 / 7, "fx_source": "operator fixture", "fx_effective_at": "2026-09-13T00:00:00Z"}}
	g := &Group{ID: 13, RateMultiplier: 2}
	b, err := QianwenFileBatchBillingSnapshot(g, a, nil, nil, f)
	require.NoError(t, err)
	return &UpstreamJob{Platform: PlatformQianwen, Family: "batch", UserID: 1, APIKeyID: 2, GroupID: &g.ID, AccountID: a.ID, Billing: b}, a
}

func TestQianwenBatchProductionTariffAndSettlement(t *testing.T) {
	j, _ := paidBatchFixture(t)
	result := strings.ReplaceAll(testBatchResult, QianwenBatchTestModel, "qwen-long")
	usage, err := validateQianwenBatchResult(j, []byte(result))
	require.NoError(t, err)
	j.UsageJSON, j.ResultRef = usage, "output"
	j.Billing.KeyQuota, j.Billing.KeyRateLimit, j.Billing.AccountQuota = true, true, true
	log, cmd, err := QianwenJobCharge(j)
	require.NoError(t, err)
	want := QuantizeUsageBillingAmount((20*.25 + 6*1) / 7 / 1e6)
	require.Equal(t, want, log.TotalCost)
	require.Equal(t, QuantizeUsageBillingAmount(want*2), cmd.BalanceCost)
	require.Equal(t, cmd.BalanceCost, cmd.APIKeyQuotaCost)
	require.Equal(t, cmd.BalanceCost, cmd.APIKeyRateLimitCost)
	require.Equal(t, want, cmd.AccountQuotaCost)
	subID := int64(7)
	j.Billing.SubscriptionID = &subID
	log, cmd, err = QianwenJobCharge(j)
	require.NoError(t, err)
	require.Zero(t, cmd.BalanceCost)
	require.Equal(t, log.ActualCost, cmd.SubscriptionCost)
	j.Family, j.UpstreamTaskID, j.ResultRef = "files", "file", "file"
	log, cmd, err = QianwenJobCharge(j)
	require.NoError(t, err)
	require.Zero(t, log.TotalCost)
	require.Zero(t, cmd.SubscriptionCost)
	// The same persisted price must survive later changes to account FX.
	j.Family, j.ResultRef = "batch", "output"
	j.Billing.FileBatch.InputPrice *= 2
	_, _, err = QianwenJobCharge(j)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
}

func TestQianwenBatchProductionExactlyOnce(t *testing.T) {
	j, _ := paidBatchFixture(t)
	usage, err := validateQianwenBatchResult(j, []byte(strings.ReplaceAll(testBatchResult, QianwenBatchTestModel, "qwen-long")))
	require.NoError(t, err)
	repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
	evidence := UpstreamJobUpdate{TaskID: "b", Status: "succeeded", ResultRef: "out", Usage: usage}
	svc := NewUpstreamJobService(repo, func(context.Context, *UpstreamJob) (UpstreamJobUpdate, error) { return evidence, nil }, QianwenJobCharge)
	j, err = svc.Submit(context.Background(), j, []byte(`{"input":"one"}`), func(context.Context) (UpstreamJobUpdate, error) {
		return UpstreamJobUpdate{TaskID: "b", Status: "submitted"}, nil
	})
	require.NoError(t, err)
	repo.failCharge = true
	_, err = svc.Settle(context.Background(), j.PublicRequestID, evidence)
	require.Error(t, err)
	require.Nil(t, repo.jobs[j.PublicRequestID].ChargedUsageLogID)
	repo.failCharge = false
	svc = NewUpstreamJobService(repo, nil, QianwenJobCharge)
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Settle(context.Background(), j.PublicRequestID, evidence)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, repo.charges)
	require.NotNil(t, repo.jobs[j.PublicRequestID].ChargedUsageLogID)
}

func TestQianwenBatchProductionRejectsUnpricedDimensions(t *testing.T) {
	for _, line := range []string{
		strings.ReplaceAll(paidBatchLine, `"enable_thinking":false`, `"enable_thinking":true`),
		strings.ReplaceAll(paidBatchLine, `"enable_thinking":false`, `"stream":true`),
		strings.ReplaceAll(paidBatchLine, "qwen-long", "qwen-plus"),
		strings.ReplaceAll(paidBatchLine, `"content":"Hello"`, `"content":[{"type":"image_url"}]`),
		strings.ReplaceAll(paidBatchLine, `"content":"Hello"`, `"content":"fileid://foreign-provider-file"`),
		paidBatchLine + "\n" + strings.ReplaceAll(strings.ReplaceAll(paidBatchLine, "one", "two"), "qwen-long", "qwen-long-latest"),
	} {
		_, _, err := ValidateQianwenBatchFile([]byte(line))
		require.Error(t, err)
	}
	j, a := paidBatchFixture(t)
	a.Extra = nil
	_, err := QianwenFileBatchBillingSnapshot(&Group{RateMultiplier: 1}, a, nil, nil, j.Billing.FileBatch)
	require.Error(t, err)
	result := strings.ReplaceAll(testBatchResult, QianwenBatchTestModel, "qwen-long")
	for _, bad := range []string{strings.ReplaceAll(result, `"total_tokens":26`, `"total_tokens":26,"prompt_tokens_details":{"cached_tokens":1}`), strings.ReplaceAll(result, `"total_tokens":26`, `"total_tokens":26,"completion_tokens_details":{"reasoning_tokens":1}`), result + "\n" + result} {
		_, err := validateQianwenBatchResult(j, []byte(bad))
		require.Error(t, err)
	}
}

func TestQianwenBatchCancelledPartialUsage(t *testing.T) {
	j, a := paidBatchFixture(t)
	j.UpstreamTaskID = "batch-1"
	j.Billing.FileBatch.CustomIDHashes = append(j.Billing.FileBatch.CustomIDHashes, fileBatchHash([]byte("not-started")))
	up := &httpUpstreamRecorder{responses: []*http.Response{
		qianwenModelPage(`{"id":"batch-1","status":"cancelled","output_file_id":"out","request_counts":{"total":2,"completed":1,"failed":0}}`, 200),
		qianwenModelPage(strings.ReplaceAll(testBatchResult, QianwenBatchTestModel, "qwen-long"), 200),
	}}
	s := &OpenAIGatewayService{httpUpstream: up, accountRepo: &fileBatchAccountRepo{account: a}}
	u, err := s.PollQianwenJob(context.Background(), j)
	require.NoError(t, err)
	require.Equal(t, "succeeded", u.Status)
	require.Equal(t, "cancelled", u.ProviderStatus)
	j.UsageJSON, j.ResultRef = u.Usage, u.ResultRef
	log, _, err := QianwenJobCharge(j)
	require.NoError(t, err)
	require.Positive(t, log.ActualCost)
	up.responses = []*http.Response{qianwenModelPage(`{"id":"batch-1","status":"cancelled","request_counts":{"total":2,"completed":0,"failed":0}}`, 200)}
	u, err = s.PollQianwenJob(context.Background(), j)
	require.NoError(t, err)
	j.UsageJSON, j.ResultRef = u.Usage, u.ResultRef
	log, _, err = QianwenJobCharge(j)
	require.NoError(t, err)
	require.Zero(t, log.ActualCost)
}

type resourceMemoryRepo struct {
	*fileBatchMemoryRepo
	inUse bool
}

func (r *resourceMemoryRepo) FileInUse(context.Context, string) (bool, error) { return r.inUse, nil }
func (r *resourceMemoryRepo) ListResources(_ context.Context, u, k, g int64, f, after string, limit int) ([]*UpstreamJob, error) {
	list := []*UpstreamJob{}
	for _, j := range r.jobs {
		if resourceOwned(j, u, k, g) && j.Family == f && !j.ResourceState.Deleted {
			list = append(list, cloneFileBatchJob(j))
		}
	}
	return list, nil
}

func TestQianwenFileBatchResourceOwnershipAndIdempotency(t *testing.T) {
	j, _ := paidBatchFixture(t)
	j.PublicRequestID, j.UpstreamTaskID, j.Status = "public", "secret", "running"
	repo := &resourceMemoryRepo{fileBatchMemoryRepo: &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{"public": j}, claims: map[string]string{}}}
	svc := NewUpstreamJobService(repo, nil, QianwenJobCharge)
	ctx := context.Background()
	calls := 0
	remote := func(context.Context, *UpstreamJob, string) error { calls++; return nil }
	_, err := svc.ManageResource(ctx, "public", 1, 3, 13, "cancel", remote)
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
	_, err = svc.ManageResource(ctx, "public", 1, 2, 14, "cancel", remote)
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
	require.Zero(t, calls)
	for range 2 {
		_, err = svc.ManageResource(ctx, "public", 1, 2, 13, "cancel", remote)
		require.NoError(t, err)
	}
	require.Equal(t, 1, calls)
	require.False(t, repo.jobs["public"].Terminal())
	j = repo.jobs["public"]
	j.Family, j.Status = "files", "succeeded"
	repo.inUse = true
	_, err = svc.ManageResource(ctx, "public", 1, 2, 13, "delete", remote)
	require.ErrorIs(t, err, ErrUpstreamResourceConflict)
	repo.inUse = false
	_, err = svc.ManageResource(ctx, "public", 1, 2, 13, "delete", func(context.Context, *UpstreamJob, string) error { return errors.New("timeout") })
	require.Error(t, err)
	require.False(t, repo.jobs["public"].ResourceState.Deleted)
	for range 2 {
		_, err = svc.ManageResource(ctx, "public", 1, 2, 13, "delete", remote)
		require.NoError(t, err)
	}
	require.Equal(t, 2, calls)
	_, err = svc.WithInputFile(ctx, "public", 1, 2, 13, func() (*UpstreamJob, error) { t.Fatal("deleted file must never submit"); return nil, nil })
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
	list, err := svc.ListResources(ctx, 1, 2, 13, "files", "", 20)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestQianwenFileBatchRemoteManagement(t *testing.T) {
	j, a := paidBatchFixture(t)
	j.UpstreamTaskID, j.ResultRef = "batch-1", "file-out"
	up := &httpUpstreamRecorder{responses: []*http.Response{
		qianwenModelPage(`{"id":"batch-1","status":"cancelling"}`, 200),
		qianwenModelPage(`{"id":"file-out","deleted":true}`, 200),
		qianwenModelPage(`{"error":"not found"}`, 404),
		qianwenModelPage(`{"id":"foreign-file","deleted":true}`, 200),
	}}
	s := &OpenAIGatewayService{httpUpstream: up, accountRepo: &fileBatchAccountRepo{account: a}}
	require.NoError(t, s.ManageQianwenResource(context.Background(), j, "cancel"))
	require.Equal(t, http.MethodPost, up.lastReq.Method)
	require.Equal(t, "/compatible-mode/v1/batches/batch-1/cancel", up.lastReq.URL.Path)
	require.NoError(t, s.ManageQianwenResource(context.Background(), j, "delete_output"))
	require.Equal(t, http.MethodDelete, up.lastReq.Method)
	require.Equal(t, "/compatible-mode/v1/files/file-out", up.lastReq.URL.Path)
	require.NoError(t, s.ManageQianwenResource(context.Background(), j, "delete_output"))
	require.Error(t, s.ManageQianwenResource(context.Background(), j, "delete_output"))
}
