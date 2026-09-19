//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testBatchLine = `{"custom_id":"one","method":"POST","url":"/v1/chat/ds-test","body":{"model":"batch-test-model","messages":[{"role":"user","content":"Hello"}]}}`
const testBatchResult = `{"custom_id":"one","error":null,"response":{"status_code":200,"body":{"model":"batch-test-model","usage":{"prompt_tokens":20,"completion_tokens":6,"total_tokens":26}}}}`

func TestQianwenResourceFailureTransport(tester *testing.T) {
	const failureBody = `{"error":{"message":"The user information is not completed."}}`
	const failureMessage = "DashScope rejected resource request (HTTP 400): The user information is not completed."
	account := qianwenTestAccount()
	account.Status = StatusActive
	for _, status := range []int{429, 500, 503} {
		gateway := &OpenAIGatewayService{httpUpstream: &httpUpstreamRecorder{responses: []*http.Response{qianwenModelPage(failureBody, status)}}}
		update, requestErr := gateway.SubmitQianwenFile(context.Background(), account, []byte(testBatchLine))
		require.Empty(tester, update.Status)
		require.EqualError(tester, requestErr, "resource upstream temporarily unavailable: The user information is not completed.")
	}
	_, snapshot, err := ValidateQianwenBatchFile([]byte(testBatchLine))
	require.NoError(tester, err)
	job := &UpstreamJob{Platform: PlatformQianwen, Family: "batch", AccountID: account.ID, UpstreamTaskID: "batch-1", Billing: UpstreamJobBilling{Model: QianwenBatchTestModel, FileBatch: snapshot}}
	for _, operation := range []string{"upload", "submit", "poll", "result", "content"} {
		tester.Run(operation, func(tester *testing.T) {
			upstream := &httpUpstreamRecorder{}
			if operation == "result" {
				upstream.responses = append(upstream.responses, qianwenModelPage(`{"id":"batch-1","status":"completed","output_file_id":"result-1","request_counts":{"total":1,"completed":1,"failed":0}}`, 200))
			}
			upstream.responses = append(upstream.responses, qianwenModelPage(failureBody, 400))
			gateway := &OpenAIGatewayService{httpUpstream: upstream, accountRepo: &fileBatchAccountRepo{account: account}}
			var update UpstreamJobUpdate
			var requestErr error
			switch operation {
			case "upload":
				update, requestErr = gateway.SubmitQianwenFile(context.Background(), account, []byte(testBatchLine))
			case "submit":
				update, requestErr = gateway.SubmitQianwenBatch(context.Background(), account, job)
			case "poll", "result":
				update, requestErr = gateway.PollQianwenJob(context.Background(), job)
			case "content":
				contentJob := *job
				contentJob.Status, contentJob.ResultRef = "succeeded", "result-1"
				_, requestErr = gateway.QianwenResourceContent(context.Background(), &contentJob)
				require.EqualError(tester, requestErr, "resource content unavailable: The user information is not completed.")
				return
			}
			require.NoError(tester, requestErr)
			require.Equal(tester, "failed", update.Status)
			require.Equal(tester, "upstream_rejected", update.ErrorCode)
			require.Equal(tester, failureMessage, update.ErrorMessage)
		})
	}
}

func TestQianwenBatchFileValidation(t *testing.T) {
	canonical, snapshot, err := ValidateQianwenBatchFile([]byte(testBatchLine))
	require.NoError(t, err)
	again, other, err := ValidateQianwenBatchFile([]byte(" \n" + testBatchLine + "\r\n"))
	require.NoError(t, err)
	require.Equal(t, canonical, again)
	require.Equal(t, snapshot, other)
	raw, _ := json.Marshal(snapshot)
	require.NotContains(t, string(raw), "Hello")
	require.NotContains(t, string(raw), "one")
	for _, body := range []string{testBatchLine + "\n" + testBatchLine, strings.ReplaceAll(testBatchLine, "batch-test-model", "qwen-plus"), strings.ReplaceAll(testBatchLine, "/v1/chat/ds-test", "/v1/chat/completions"), strings.ReplaceAll(testBatchLine, `"method":"POST"`, `"method":"GET"`), testBatchLine + "garbage", strings.Repeat("x", QianwenFileLimit+1)} {
		_, _, err = ValidateQianwenBatchFile([]byte(body))
		require.Error(t, err)
	}
}
func TestQianwenBatchResultUsageRejectsUnexplainedEvidence(t *testing.T) {
	_, snapshot, err := ValidateQianwenBatchFile([]byte(testBatchLine))
	require.NoError(t, err)
	job := &UpstreamJob{Platform: PlatformQianwen, Family: "batch", ResultRef: "result-file", Billing: UpstreamJobBilling{Model: QianwenBatchTestModel, FileBatch: snapshot}}
	job.UsageJSON, err = validateQianwenBatchResult(job, []byte(testBatchResult))
	require.NoError(t, err)
	log, cmd, err := QianwenJobCharge(job)
	require.NoError(t, err)
	require.Equal(t, 20, log.InputTokens)
	require.Equal(t, 6, log.OutputTokens)
	require.Zero(t, log.TotalCost)
	require.Zero(t, cmd.BalanceCost)
	for _, bad := range []string{testBatchResult + "\n" + testBatchResult, "", strings.ReplaceAll(testBatchResult, `"one"`, `"foreign"`), strings.ReplaceAll(testBatchResult, `"total_tokens":26`, `"total_tokens":25`), strings.ReplaceAll(testBatchResult, `"prompt_tokens":20`, `"prompt_tokens":"20"`), strings.ReplaceAll(testBatchResult, "batch-test-model", "qwen-plus"), strings.ReplaceAll(testBatchResult, `"status_code":200`, `"status_code":500`)} {
		_, err = validateQianwenBatchResult(job, []byte(bad))
		require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)
	}
	job.Billing.Model = "qwen-plus"
	_, _, err = QianwenJobCharge(job)
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)
}
func TestQianwenFileBatchTransport(t *testing.T) {
	a := qianwenTestAccount()
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		qianwenModelPage(`{"id":"file-1","purpose":"batch","status":"processed"}`, 200),
		qianwenModelPage(`{"id":"batch-1","status":"validating"}`, 200),
		qianwenModelPage(`{"code":"secret-provider-error"}`, 403),
	}}
	s := &OpenAIGatewayService{httpUpstream: upstream}
	canonical, _, err := ValidateQianwenBatchFile([]byte(testBatchLine))
	require.NoError(t, err)
	u, err := s.SubmitQianwenFile(context.Background(), a, canonical)
	require.NoError(t, err)
	require.Equal(t, "succeeded", u.Status)
	require.Equal(t, "file-1", u.TaskID)
	require.Equal(t, "/compatible-mode/v1/files", upstream.lastReq.URL.Path)
	require.Contains(t, upstream.lastReq.Header.Get("Content-Type"), "multipart/form-data")
	require.Empty(t, upstream.lastReq.Header.Get("X-DashScope-Async"))
	require.Contains(t, string(upstream.lastBody), "batch-test-model")
	u, err = s.SubmitQianwenBatch(context.Background(), a, &UpstreamJob{UpstreamTaskID: "file-1"})
	require.NoError(t, err)
	require.Equal(t, "submitted", u.Status)
	require.Equal(t, "/compatible-mode/v1/batches", upstream.lastReq.URL.Path)
	require.JSONEq(t, `{"input_file_id":"file-1","endpoint":"/v1/chat/ds-test","completion_window":"24h"}`, string(upstream.lastBody))
	u, err = s.SubmitQianwenBatch(context.Background(), a, &UpstreamJob{UpstreamTaskID: "file-1"})
	require.NoError(t, err)
	require.Equal(t, "upstream_rejected", u.ErrorCode)
	require.NotContains(t, u.ErrorMessage, "secret")
}

// An in-memory transactional repository exercises actual Submit/Poll/Settle
// concurrency and rollback semantics without writing the shared database.
type fileBatchMemoryRepo struct {
	mu         sync.Mutex
	jobs       map[string]*UpstreamJob
	claims     map[string]string
	charges    int
	failCharge bool
}

func cloneFileBatchJob(j *UpstreamJob) *UpstreamJob {
	raw, _ := json.Marshal(j)
	var copy UpstreamJob
	_ = json.Unmarshal(raw, &copy)
	return &copy
}
func (r *fileBatchMemoryRepo) Create(_ context.Context, j *UpstreamJob) (*UpstreamJob, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id := r.claims[j.SubmissionKey]; id != "" {
		return cloneFileBatchJob(r.jobs[id]), false, nil
	}
	r.claims[j.SubmissionKey] = j.PublicRequestID
	r.jobs[j.PublicRequestID] = cloneFileBatchJob(j)
	return cloneFileBatchJob(j), true, nil
}
func (r *fileBatchMemoryRepo) Get(_ context.Context, id string, user, key int64) (*UpstreamJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.jobs[id]
	if j == nil || j.UserID != user || j.APIKeyID != key {
		return nil, ErrUpstreamJobNotFound
	}
	return cloneFileBatchJob(j), nil
}
func (r *fileBatchMemoryRepo) Due(context.Context, time.Time, int) ([]string, error) { return nil, nil }
func (r *fileBatchMemoryRepo) Expire(context.Context, time.Time) (int64, error)      { return 0, nil }
func (r *fileBatchMemoryRepo) WithLocked(ctx context.Context, id string, fn func(*UpstreamJob, UpstreamJobTx) error) (*UpstreamJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j := cloneFileBatchJob(r.jobs[id])
	before := r.charges
	if err := fn(j, r); err != nil {
		r.charges = before
		return nil, err
	}
	r.jobs[id] = j
	return cloneFileBatchJob(j), nil
}
func (r *fileBatchMemoryRepo) Charge(_ context.Context, l *UsageLog, c *UsageBillingCommand) error {
	if r.failCharge {
		return errors.New("injected transaction failure")
	}
	r.charges++
	l.ID = int64(r.charges)
	return nil
}
func TestQianwenFileBatchPersistentClaimAndSettlement(t *testing.T) {
	ctx := context.Background()
	_, snapshot, err := ValidateQianwenBatchFile([]byte(testBatchLine))
	require.NoError(t, err)
	for _, family := range []string{"files", "batch"} {
		t.Run(family, func(t *testing.T) {
			repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
			template := &UpstreamJob{Platform: PlatformQianwen, Family: family, UserID: 1, APIKeyID: 2, AccountID: 19, Billing: UpstreamJobBilling{Model: QianwenBatchTestModel, RequestedModel: QianwenBatchTestModel, FileBatch: snapshot}}
			var submissions, polls atomic.Int32
			evidence := UpstreamJobUpdate{TaskID: "upstream-1", Status: "succeeded", ResultRef: "upstream-1"}
			if family == "batch" {
				evidence.Usage, err = validateQianwenBatchResult(template, []byte(testBatchResult))
				require.NoError(t, err)
			}
			poll := func(context.Context, *UpstreamJob) (UpstreamJobUpdate, error) { polls.Add(1); return evidence, nil }
			svc := NewUpstreamJobService(repo, poll, QianwenJobCharge)
			submit := func(context.Context) (UpstreamJobUpdate, error) {
				submissions.Add(1)
				if family == "files" {
					return evidence, nil
				}
				return UpstreamJobUpdate{TaskID: "upstream-1", Status: "submitted"}, nil
			}
			var wg sync.WaitGroup
			ids := make(chan string, 20)
			for range 20 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					j, e := svc.Submit(ctx, template, []byte(`{"sha":"same"}`), submit)
					if e != nil {
						t.Error(e)
						return
					}
					ids <- j.PublicRequestID
				}()
			}
			wg.Wait()
			close(ids)
			id := ""
			for got := range ids {
				if id == "" {
					id = got
				}
				require.Equal(t, id, got)
			}
			require.Equal(t, int32(1), submissions.Load())
			_, err = svc.Get(ctx, id, 1, 3)
			require.ErrorIs(t, err, ErrUpstreamJobNotFound)
			// A fresh service simulates process restart; no in-memory ownership is needed.
			svc = NewUpstreamJobService(repo, poll, QianwenJobCharge)
			svc.now = func() time.Time { return time.Now().Add(time.Minute) }
			for range 20 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, e := svc.Poll(ctx, id)
					if e != nil {
						t.Error(e)
					}
				}()
			}
			wg.Wait()
			j, err := svc.Get(ctx, id, 1, 2)
			require.NoError(t, err)
			require.Equal(t, "succeeded", j.Status)
			require.NotNil(t, j.ChargedUsageLogID)
			require.Equal(t, 1, repo.charges)
			_, err = svc.Settle(ctx, id, evidence)
			require.NoError(t, err)
			require.Equal(t, 1, repo.charges)
			if family == "batch" {
				require.Equal(t, int32(1), polls.Load())
			} else {
				require.Zero(t, polls.Load())
			}
		})
	}
}
func TestQianwenBatchSettlementRollbackAndUnknownSubmission(t *testing.T) {
	ctx := context.Background()
	_, snapshot, err := ValidateQianwenBatchFile([]byte(testBatchLine))
	require.NoError(t, err)
	repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
	template := &UpstreamJob{Platform: PlatformQianwen, Family: "batch", UserID: 1, APIKeyID: 2, AccountID: 19, Billing: UpstreamJobBilling{Model: QianwenBatchTestModel, FileBatch: snapshot}}
	usage, err := validateQianwenBatchResult(template, []byte(testBatchResult))
	require.NoError(t, err)
	evidence := UpstreamJobUpdate{TaskID: "b", Status: "succeeded", ResultRef: "f", Usage: usage}
	svc := NewUpstreamJobService(repo, func(context.Context, *UpstreamJob) (UpstreamJobUpdate, error) { return evidence, nil }, QianwenJobCharge)
	j, err := svc.Submit(ctx, template, []byte(`{}`), func(context.Context) (UpstreamJobUpdate, error) {
		return UpstreamJobUpdate{TaskID: "b", Status: "submitted"}, nil
	})
	require.NoError(t, err)
	repo.failCharge = true
	_, err = svc.Settle(ctx, j.PublicRequestID, evidence)
	require.Error(t, err)
	j, err = svc.Get(ctx, j.PublicRequestID, 1, 2)
	require.NoError(t, err)
	require.Equal(t, "submitted", j.Status)
	require.Nil(t, j.ChargedUsageLogID)
	repo.failCharge = false
	_, err = svc.Settle(ctx, j.PublicRequestID, evidence)
	require.NoError(t, err)
	require.Equal(t, 1, repo.charges)
	calls := 0
	submit := func(context.Context) (UpstreamJobUpdate, error) {
		calls++
		return UpstreamJobUpdate{}, errors.New("timeout")
	}
	j, err = svc.Submit(ctx, template, []byte(`{"other":true}`), submit)
	require.NoError(t, err)
	require.Equal(t, "submission_unknown", j.ErrorCode)
	_, err = svc.Submit(ctx, template, []byte(`{"other":true}`), submit)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}

type fileBatchAccountRepo struct {
	AccountRepository
	account *Account
}

func (r *fileBatchAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func TestQianwenBatchPollValidatesResultsAndContent(t *testing.T) {
	a := qianwenTestAccount()
	a.Status = StatusActive
	_, snapshot, err := ValidateQianwenBatchFile([]byte(testBatchLine))
	require.NoError(t, err)
	j := &UpstreamJob{Platform: PlatformQianwen, Family: "batch", AccountID: a.ID, UpstreamTaskID: "batch-1", Billing: UpstreamJobBilling{Model: QianwenBatchTestModel, FileBatch: snapshot}}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		qianwenModelPage(`{"id":"batch-1","status":"completed","output_file_id":"result-1","request_counts":{"total":1,"completed":1,"failed":0}}`, 200),
		qianwenModelPage(testBatchResult, 200),
		qianwenModelPage(testBatchResult, 200),
		qianwenModelPage(testBatchResult+"\n", 200),
	}}
	s := &OpenAIGatewayService{httpUpstream: upstream, accountRepo: &fileBatchAccountRepo{account: a}}
	u, err := s.PollQianwenJob(context.Background(), j)
	require.NoError(t, err)
	require.Equal(t, "succeeded", u.Status)
	require.Equal(t, "/compatible-mode/v1/files/result-1/content", upstream.lastReq.URL.Path)
	j.ResultRef = u.ResultRef
	j.UsageJSON = u.Usage
	j.Status = u.Status
	raw, err := s.QianwenResourceContent(context.Background(), j)
	require.NoError(t, err)
	require.Equal(t, testBatchResult, string(raw))
	_, err = s.QianwenResourceContent(context.Background(), j)
	require.ErrorContains(t, err, "integrity")
}

func validateQianwenBatchResult(j *UpstreamJob, raw []byte) (json.RawMessage, error) {
	if j.Billing.FileBatch == nil {
		return nil, ErrUpstreamJobUnsupportedUsage
	}
	return validateQianwenBatchItems(j, raw, len(j.Billing.FileBatch.CustomIDHashes))
}
