//go:build unit

package service

import (
	"context"
	"errors"
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

type batchIngressTPMRepository struct {
	unlimitedTPMRepository
	rejection error
	commands  []TPMReservationCommand
}

func (repository *batchIngressTPMRepository) ReserveTPMAttempt(_ context.Context, command TPMReservationCommand) (*TPMReservation, error) {
	repository.commands = append(repository.commands, command)
	return nil, repository.rejection
}

func TestBatchImageAdmissionRejectionReleasesHoldButUnknownSendRetainsIt(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		rejection error
		unknown   bool
	}{
		{name: "key_limit", rejection: &TPMLimitExceeded{Scope: TPMScopeAPIKey}},
		{name: "account_limit", rejection: &TPMLimitExceeded{Scope: TPMScopeAccount}},
		{name: "unknown_estimate", rejection: ErrTPMEstimateUnavailable},
		{name: "lost_create_response", unknown: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var creates atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if strings.HasPrefix(request.URL.Path, "/upload/") {
					_, _ = writer.Write([]byte(`{"file":{"name":"files/admission"}}`))
					return
				}
				creates.Add(1)
				connection, _, err := writer.(http.Hijacker).Hijack()
				require.NoError(t, err)
				_ = connection.Close()
			}))
			defer server.Close()
			svc, repo, _, _, _ := newTestBatchImagePublicService(true)
			svc.ProviderRegistry = NewBatchImageProviderRegistry(NewGeminiAPIBatchImageProvider(NewGeminiBatchHTTPClient(server.URL, server.Client())))
			tokens := &batchIngressTPMRepository{rejection: scenario.rejection}
			gateway := &OpenAIGatewayService{usageSettlement: NewUsageSettlementService(tokens, nil, nil)}
			ctx, parent := gateway.CaptureRequestPlan(context.Background(), GatewayRequestSpec{WorkspaceID: 31, UserID: 11, APIKeyID: 22, BillingGroupID: 41, Model: validBatchImageSubmitRequest().Model})
			defer parent.Finalize(false)
			_, err := svc.Submit(ctx, testBatchImageOwner(), validBatchImageSubmitRequest(), "")
			require.Error(t, err)
			require.Len(t, tokens.commands, 1)
			require.EqualValues(t, 31, tokens.commands[0].WorkspaceID)
			require.EqualValues(t, 41, tokens.commands[0].BillingGroupID)
			billing := svc.BillingRepo.(*fakeBatchImageBillingRepo)
			require.Len(t, billing.reserves, 1)
			if scenario.unknown {
				require.EqualValues(t, 1, creates.Load())
				require.Empty(t, billing.releases)
				for _, job := range repo.jobs {
					require.Equal(t, BatchImageJobStatusUploading, job.Status)
				}
			} else {
				require.True(t, IsTPMAdmissionError(err), err.Error())
				require.Zero(t, creates.Load())
				require.Len(t, billing.releases, 1)
				for _, job := range repo.jobs {
					require.Equal(t, BatchImageJobStatusFailed, job.Status)
				}
			}
		})
	}
}

func TestReceiptLostAcknowledgementComparesCompleteTPMIdentity(t *testing.T) {
	for _, scenario := range []string{"same", "workspace", "key", "execution", "attempt", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			original := durableReceiptFixture()
			original.TPMReservation = &TPMReservationIdentity{WorkspaceID: 1, APIKeyID: 2, ExecutionID: "original", AttemptIndex: 1}
			original, err := PrepareUsageSettlementReceipt(original)
			require.NoError(t, err)
			incoming := *original
			identity := *original.TPMReservation
			incoming.TPMReservation = &identity
			switch scenario {
			case "workspace":
				identity.WorkspaceID++
			case "key":
				identity.APIKeyID++
			case "execution":
				identity.ExecutionID = "other"
			case "attempt":
				identity.AttemptIndex++
			case "missing":
				incoming.TPMReservation = nil
			}
			repository := &durableReceiptStore{row: original, persistErr: errors.New("acknowledgement lost")}
			err = NewUsageSettlementService(repository, nil, nil).Submit(context.Background(), &incoming, nil)
			if scenario == "same" {
				require.NoError(t, err)
				require.Equal(t, 1, repository.applied)
			} else {
				require.ErrorIs(t, err, ErrTPMReservationConflict)
				require.Zero(t, repository.applied)
			}
		})
	}
}
