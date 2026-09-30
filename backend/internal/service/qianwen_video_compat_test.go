//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQianwenVideoCompatConversion(t *testing.T) {
	for _, seconds := range []string{`5`, `"5"`} {
		body, err := QianwenVideoCompatBody([]byte(`{"model":"wan2.6-t2v","prompt":"A cat","seconds":` + seconds + `,"size":"720x1280"}`))
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"wan2.6-t2v","input":{"prompt":"A cat"},"parameters":{"duration":5,"size":"720*1280"}}`, string(body))
	}
	for _, body := range []string{`null`, `[]`, `{}`, `{"model":"wan2.6-t2v","prompt":""}`, `{"model":"wan2.6-t2v","prompt":"cat","seconds":1.5}`, `{"model":"wan2.6-t2v","prompt":"cat","seconds":0}`, `{"model":"wan2.6-t2v","prompt":"cat","size":"invalid"}`, `{"model":"wan2.6-t2v","prompt":"cat","input_reference":"/tmp/image"}`, `{"model":"wan2.6-t2v","prompt":"cat","parameters":{}}`} {
		_, err := QianwenVideoCompatBody([]byte(body))
		require.Error(t, err, body)
	}
	for native, standard := range map[string]string{"submitted": "queued", "running": "in_progress", "succeeded": "completed", "failed": "failed", "expired": "failed", "unsupported": "failed"} {
		require.Equal(t, standard, QianwenVideoCompatStatus(native))
	}
}

type videoCompatAccountRepo struct {
	AccountRepository
	account     *Account
	requestedID int64
}

func (repo *videoCompatAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	repo.requestedID = id
	return repo.account, nil
}

func TestQianwenVideoCompatCancellationTransport(t *testing.T) {
	for _, test := range []struct {
		body         string
		status, want int
	}{
		{`"provider-request-id"`, 200, 200},
		{`{"code":"UnsupportedOperation"}`, 400, 409},
		{`{"code":"InsufficientBalance","message":"secret-upstream"}`, 402, 402},
		{`{}`, 200, 502}, {`null`, 200, 502}, {`""`, 200, 502}, {`{}`, 503, 503},
	} {
		account := qianwenTestAccount()
		account.Status = StatusActive
		repo := &videoCompatAccountRepo{account: account}
		transport := &httpUpstreamRecorder{responses: []*http.Response{qianwenModelPage(test.body, test.status)}}
		gateway := &OpenAIGatewayService{accountRepo: repo, httpUpstream: transport}
		job := &UpstreamJob{AccountID: 73, Platform: PlatformQianwen, Family: "video", Status: "submitted", UpstreamTaskID: "private-task"}
		status, err := gateway.CancelQianwenVideo(context.Background(), job)
		require.Equal(t, test.want, status)
		if test.want == 200 {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret-upstream")
		}
		require.Equal(t, int64(73), repo.requestedID)
		require.Equal(t, "/api/v1/tasks/private-task/cancel", transport.lastReq.URL.Path)
		require.Equal(t, http.MethodPost, transport.lastReq.Method)
		require.Empty(t, transport.lastReq.Header.Get("X-DashScope-Async"))
		require.Equal(t, "submitted", job.Status)
		require.Nil(t, job.ChargedUsageLogID)
	}
	for _, state := range []string{"running", "succeeded", "failed", "expired"} {
		status, err := (&OpenAIGatewayService{}).CancelQianwenVideo(context.Background(), &UpstreamJob{Platform: PlatformQianwen, Family: "video", Status: state, UpstreamTaskID: "task"})
		require.Equal(t, 409, status)
		require.ErrorIs(t, err, ErrQianwenVideoNotCancelable)
	}
}

func TestQianwenVideoCompatCancelLeavesDurableSettlementAuthoritative(t *testing.T) {
	for _, outcome := range []string{"SUCCEEDED", "CANCELED", "FAILED"} {
		t.Run(outcome, func(t *testing.T) {
			account := qianwenTestAccount()
			account.Status = StatusActive
			transport := &httpUpstreamRecorder{responses: []*http.Response{
				qianwenModelPage(`"cancel-acknowledged"`, 200),
				qianwenModelPage(`{"output":{"task_id":"private-task","task_status":"`+outcome+`","video_url":"https://example.test/video.mp4"},"usage":{"duration":5,"video_count":1,"SR":720}}`, 200),
			}}
			gateway := &OpenAIGatewayService{accountRepo: &videoCompatAccountRepo{account: account}, httpUpstream: transport}
			job := &UpstreamJob{PublicRequestID: "public", AccountID: account.ID, UserID: 1, APIKeyID: 2, Platform: PlatformQianwen, Family: "video", Status: "submitted", UpstreamTaskID: "private-task", ExpiresAt: time.Now().Add(time.Hour), Billing: UpstreamJobBilling{Model: "wan2.6-t2v", RequestedModel: "wan2.6-t2v", Resolution: "720p", UnitPrice: 0.1, RateMultiplier: 1, AccountRateMultiplier: 1}}
			repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{"public": cloneFileBatchJob(job)}}
			jobs := NewUpstreamJobService(repo, gateway.PollQianwenVideo, QianwenVideoJobCharge)
			_, err := gateway.CancelQianwenVideo(context.Background(), job)
			require.NoError(t, err)
			require.Equal(t, "submitted", repo.jobs["public"].Status)
			result, err := jobs.Poll(context.Background(), "public")
			require.NoError(t, err)
			if outcome == "SUCCEEDED" {
				require.Equal(t, "succeeded", result.Status)
				require.Equal(t, 1, repo.charges)
			} else {
				require.Equal(t, "failed", result.Status)
				require.Zero(t, repo.charges)
			}
			_, err = jobs.Poll(context.Background(), "public")
			require.NoError(t, err)
			if outcome == "SUCCEEDED" {
				require.Equal(t, 1, repo.charges)
			}
		})
	}
}
