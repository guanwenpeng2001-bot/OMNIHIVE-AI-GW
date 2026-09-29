package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchCreateSharesJobBudgetWhileUploadsAndPollsDoNotGenerate(t *testing.T) {
	for _, provider := range []string{"gemini", "vertex"} {
		t.Run(provider, func(t *testing.T) {
			var sends atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				sends.Add(1)
				_, _ = writer.Write([]byte(`{}`))
			}))
			defer server.Close()
			ctx, finish := CaptureGatewayJobPlan(context.Background(), GatewayRequestSpec{ExecutionID: "batch", UpstreamAccountID: 7})
			defer finish()
			createPath := "/v1beta/models/image:batchGenerateContent"
			if provider == "vertex" {
				createPath = "/v1/projects/project/locations/region/batchPredictionJobs"
			}
			send := func(method, path string) error {
				request, err := http.NewRequestWithContext(ctx, method, server.URL+path, nil)
				require.NoError(t, err)
				var result map[string]any
				if provider == "gemini" {
					return NewGeminiBatchHTTPClient(server.URL, server.Client()).doJSON(request, &result)
				}
				return doVertexDecodeJSON(server.Client(), request, &result)
			}
			require.NoError(t, send(http.MethodPost, "/upload/files"))
			require.NoError(t, send(http.MethodGet, "/status/job"))
			require.Empty(t, GatewayRequestPlanFromContext(ctx).Attempts())
			require.NoError(t, send(http.MethodPost, createPath))
			require.ErrorIs(t, send(http.MethodPost, createPath), ErrAttemptBudgetExhausted)
			require.EqualValues(t, 3, sends.Load())
			require.EqualValues(t, 7, GatewayRequestPlanFromContext(ctx).Attempts()[0].AccountID)
		})
	}
}

func TestBatchCreateDoesNotFollowReplayRedirect(t *testing.T) {
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		sends.Add(1)
		writer.Header().Set("Location", "/replayed")
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	ctx, finish := CaptureGatewayJobPlan(context.Background(), GatewayRequestSpec{ExecutionID: "redirected-batch"})
	defer finish()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/batchPredictionJobs", nil)
	require.NoError(t, err)
	var result map[string]any
	require.Error(t, doVertexDecodeJSON(server.Client(), request, &result))
	require.EqualValues(t, 1, sends.Load())
}
