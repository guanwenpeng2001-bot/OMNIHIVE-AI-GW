package service

import (
	"context"
	"net/http"
	"net/http/httptest"

	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchImageRealHTTPWithIngressParent(t *testing.T) {
	var uploads, creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/upload/") {
			uploads.Add(1)
			_, _ = w.Write([]byte(`{"file":{"name":"files/audit"}}`))
			return
		}
		creates.Add(1)
		_, _ = w.Write([]byte(`{"name":"batches/audit","state":"JOB_STATE_PENDING"}`))
	}))
	defer server.Close()
	svc, repo, _, _, _ := newTestBatchImagePublicService(true)
	svc.ProviderRegistry = NewBatchImageProviderRegistry(NewGeminiAPIBatchImageProvider(NewGeminiBatchHTTPClient(server.URL, server.Client())))
	gateway := &OpenAIGatewayService{usageSettlement: NewUsageSettlementService(unlimitedTPMRepository{}, nil, nil)}
	ctx, parent := gateway.CaptureRequestPlan(context.Background(), GatewayRequestSpec{WorkspaceID: 1, UserID: 11, APIKeyID: 22, Model: validBatchImageSubmitRequest().Model})
	defer parent.Finalize(false)
	got, err := svc.Submit(ctx, testBatchImageOwner(), validBatchImageSubmitRequest(), "")
	billing := svc.BillingRepo.(*fakeBatchImageBillingRepo)
	for _, job := range repo.jobs {
		t.Logf("status=%s hold=%v", job.Status, *job.HoldAmount)
	}
	t.Logf("result=%v error=%v uploads=%d creates=%d holds=%d releases=%d", got, err, uploads.Load(), creates.Load(), len(billing.reserves), len(billing.releases))
	require.NoError(t, err, "unlimited batch image request must remain usable")
	require.EqualValues(t, 1, creates.Load())
}

func TestAuditReceiptTPMConflictMustPropagate(t *testing.T) {
	original := durableReceiptFixture()
	original.TPMReservation = &TPMReservationIdentity{WorkspaceID: 1, APIKeyID: 2, ExecutionID: "first", AttemptIndex: 1}
	original, err := PrepareUsageSettlementReceipt(original)
	require.NoError(t, err)
	repo := &durableReceiptStore{row: original, persistErr: ErrTPMReservationConflict}
	incoming := *original
	incoming.TPMReservation = &TPMReservationIdentity{WorkspaceID: 1, APIKeyID: 2, ExecutionID: "second", AttemptIndex: 1}
	err = NewUsageSettlementService(repo, nil, nil).Submit(context.Background(), &incoming, nil)
	t.Logf("Submit err=%v stored execution=%s incoming execution=%s applied=%d", err, repo.row.TPMReservation.ExecutionID, incoming.TPMReservation.ExecutionID, repo.applied)
	require.ErrorIs(t, err, ErrTPMReservationConflict)
}
