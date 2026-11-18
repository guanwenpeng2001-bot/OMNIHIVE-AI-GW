package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

// A 400/404 that only says the probe model does not exist says nothing about
// the /v1/responses endpoint: the verdict must stay inconclusive and the
// account capability must not be overwritten with "unsupported".
func TestResponsesProbeModelUnavailableIsInconclusive(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound} {
		for _, body := range []string{
			`{"error":{"type":"model_not_found","message":"Model codex-auto-review is not supported by any configured account in this group"}}`,
			`{"error":{"code":"model_not_available"}}`,
			`{"error":{"message":"The model missing does not exist"}}`,
			`{"response":{"error":{"code":"invalid_model"}}}`,
		} {
			require.False(t, responsesProbeVerdictIsConclusive(status, []byte(body)))
			require.True(t, decideResponsesProbeSupport(status, []byte(body)))
			updates := make(chan map[string]any, 1)
			account := Account{ID: 96, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://upstream.example"}}
			repo := &snapshotUpdateAccountRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}, updateExtraCalls: updates}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)),
			}}
			svc := &AccountTestService{accountRepo: repo, cfg: &config.Config{}, httpUpstream: upstream}
			svc.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)
			require.NotNil(t, upstream.lastReq)
			select {
			case <-updates:
				t.Fatal("a model failure must not overwrite the account endpoint capability")
			default:
			}
		}
	}
	// A plain 404/405 still means the endpoint does not exist.
	require.True(t, responsesProbeVerdictIsConclusive(404, []byte("404 page not found")))
	require.False(t, decideResponsesProbeSupport(404, []byte("404 page not found")))
	require.False(t, decideResponsesProbeSupport(405, nil))
	require.True(t, responsesProbeVerdictIsConclusive(404, []byte(`{"error":{"message":"model output is not supported"}}`)))
}

func TestSelectResponsesProbeModelPrefersGeneralTextModel(t *testing.T) {
	account := &Account{Credentials: map[string]any{"model_mapping": map[string]any{
		"codex-auto-review": "codex-auto-review", "gpt-image-2": "gpt-image-2", "gpt-5.5": "gpt-5.5",
	}}}
	require.Equal(t, "gpt-5.5", selectResponsesProbeModel(account))
}
