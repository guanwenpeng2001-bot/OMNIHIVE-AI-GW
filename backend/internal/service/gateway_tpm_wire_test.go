package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type tpmBoundaryRepository struct {
	unlimitedTPMRepository
	rejection error
	commands  []TPMReservationCommand
}

func (r *tpmBoundaryRepository) ReserveTPMAttempt(_ context.Context, command TPMReservationCommand) (*TPMReservation, error) {
	r.commands = append(r.commands, command)
	if r.rejection != nil {
		return nil, r.rejection
	}
	return &TPMReservation{TPMReservationIdentity: command.TPMReservationIdentity, Estimate: 25, Fingerprint: command.Fingerprint}, nil
}
func boundaryTPMPlan(repository *tpmBoundaryRepository, execution string) *GatewayRequestPlan {
	plan := NewGatewayRequestPlan(GatewayRequestSpec{WorkspaceID: 1, APIKeyID: 2, BillingGroupID: 3, ExecutionID: execution})
	plan.settlement = NewUsageSettlementService(repository, nil, nil)
	return plan
}

func TestGatewayTPM_AllTokenEntrypointsReserveBeforeUpstream(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses", "/v1beta/models/model:generateContent", "/v1/embeddings", "/v1/rerank", "/v1/audio/transcriptions", "/v1/images/generations", "/v1/batches"} {
		t.Run(path, func(t *testing.T) {
			repository := &tpmBoundaryRepository{rejection: &TPMLimitExceeded{Scope: TPMScopeGroup, ResetAt: time.Now().Add(time.Minute)}}
			plan := boundaryTPMPlan(repository, "request")
			request, _ := http.NewRequestWithContext(WithGatewayRequestPlan(context.Background(), plan), http.MethodPost, "https://fixture.invalid"+path, strings.NewReader(`{"model":"model","max_tokens":10}`))
			sends := 0
			send := func(*http.Request) (*http.Response, error) {
				sends++
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}
			_, err := generationHTTP(request, 4, send)
			require.True(t, IsTPMAdmissionError(err))
			require.Zero(t, sends)
			require.Len(t, repository.commands, 1)
			require.Equal(t, int64(3), repository.commands[0].BillingGroupID)
			repository.rejection = nil
			response, err := generationHTTP(request, 4, send)
			require.NoError(t, err)
			require.Equal(t, 1, sends)
			_ = response.Body.Close()
		})
	}
}

func TestGatewayTPM_WebSocketTurnsHaveDistinctReservations(t *testing.T) {
	repository := &tpmBoundaryRepository{}
	for _, execution := range []string{"socket:turn:1", "socket:turn:2"} {
		plan := boundaryTPMPlan(repository, execution)
		sends := 0
		err := generationWS(WithGatewayRequestPlan(context.Background(), plan), 4, map[string]any{"type": "response.create", "model": "model", "max_output_tokens": 10}, func(context.Context) error { sends++; return nil })
		require.NoError(t, err)
		require.Equal(t, 1, sends)
	}
	require.Len(t, repository.commands, 2)
	require.NotEqual(t, repository.commands[0].ExecutionID, repository.commands[1].ExecutionID)
}

func TestTPMEstimate_FinalWireBoundsAndCandidates(t *testing.T) {
	input := int64(100)
	no := false
	record := CapabilityRecord{Source: "approved-fixture", LiteLLMCapabilityFields: LiteLLMCapabilityFields{MaxInputTokens: &input, SupportsReasoning: &no}}
	estimate := estimateTPMFromRecord([]byte(`{"max_completion_tokens":12,"n":3,"tools":[{"type":"function"}]}`), "/chat/completions", record)
	total, err := estimate.Tokens()
	require.NoError(t, err)
	require.Equal(t, int64(136), total)
	for _, body := range []string{`{}`, `{"max_tokens":"12"}`, `{"max_tokens":1.2}`, `{"max_tokens":12,"previous_response_id":"remote"}`, `{"max_tokens":12,"input_image":"url"}`} {
		_, err := estimateTPMFromRecord([]byte(body), "/responses", record).Tokens()
		require.ErrorIs(t, err, ErrTPMEstimateUnavailable)
	}
	estimate = estimateTPMFromRecord([]byte(`{"input":["first","second"]}`), "/embeddings", record)
	total, err = estimate.Tokens()
	require.NoError(t, err)
	require.Equal(t, int64(200), total)
}

func TestTPMUsage_FailedAttemptNormalization(t *testing.T) {
	usage, known := normalizedTPMFailureUsage([]byte(`{"usage":{"prompt_tokens":30,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":20}}}`))
	require.True(t, known)
	total, err := TPMUsageTokens(usage)
	require.NoError(t, err)
	require.Equal(t, int64(37), total)
	for _, body := range []string{`{"usage":{"input_tokens":12}}`, `{"usage":{"input_tokens":1.2,"output_tokens":0}}`, `{"usage":{"input_tokens":9223372036854775808,"output_tokens":0}}`} {
		_, known := normalizedTPMFailureUsage([]byte(body))
		require.False(t, known)
	}
}

func TestGatewayTPM_MissingAuthoritativeStorageFailsClosed(t *testing.T) {
	plan := NewGatewayRequestPlan(GatewayRequestSpec{WorkspaceID: 1, APIKeyID: 2, ExecutionID: "unavailable"})
	_, err := BeginGenerationAttempt(WithGatewayRequestPlan(context.Background(), plan), 4)
	require.True(t, errors.Is(err, ErrTPMStateUnavailable))
}
