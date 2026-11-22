//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExplicitPassthroughBufferedResponsesUsage(t *testing.T) {
	const progress = "data: " + `{"type":"response.in_progress","response":{"usage":{"input_tokens":7,"output_tokens":2,"input_tokens_details":{"cached_tokens":3}}}}` + "\n\n"
	const delta = "data: " + `{"type":"response.output_text.delta","delta":"private partial output"}` + "\n\n"
	completed := func(usage string) string {
		return "data: " + `{"type":"response.completed","response":{"id":"resp_meter","status":"completed","output":[],"usage":` + usage + `}}` + "\n\n"
	}
	for _, tc := range []struct {
		name, frames          string
		input, output, cached int
		failed, review        bool
	}{
		{"empty terminal usage", progress + completed("{}"), 7, 2, 3, false, false},
		{"zero placeholder", progress + completed(`{"input_tokens":0,"output_tokens":0}`), 7, 2, 3, false, false},
		{"partial terminal", progress + completed(`{"output_tokens":4}`), 7, 4, 3, false, false},
		{"explicit zero", completed(`{"input_tokens":0,"output_tokens":0}`), 0, 0, 0, false, false},
		{"missing input", completed(`{"output_tokens":2}`), 0, 2, 0, false, true},
		{"empty SSE", "", 0, 0, 0, true, true},
		{"missing terminal", progress + delta + "data: [DONE]\n\n", 7, 2, 3, true, true},
		{"terminal without response", progress + "data: " + `{"type":"response.completed"}` + "\n\n", 7, 2, 3, true, true},
		{"transport interruption", progress + delta, 7, 2, 3, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, rec, resp := gatewayAuditContext(tc.frames)
			if tc.name == "transport interruption" {
				resp.Body = io.NopCloser(io.MultiReader(strings.NewReader(tc.frames), passthroughErrReadCloser{io.ErrUnexpectedEOF}))
			}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: resp}}
			account := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{"openai_passthrough": true}}
			body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
			result, err := svc.Forward(context.Background(), c, account, body)
			require.NotNil(t, result)
			require.Equal(t, "request-a", result.RequestID)
			require.Equal(t, tc.review, result.UsageIncomplete)
			require.Contains(t, rec.Result().Header.Get("Content-Type"), "application/json")
			if tc.failed {
				require.Error(t, err)
				require.Equal(t, http.StatusBadGateway, rec.Code)
				require.JSONEq(t, `{"error":{"type":"upstream_error","message":"Upstream response ended before completion"}}`, rec.Body.String())
			} else {
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, rec.Code)
				require.Equal(t, "resp_meter", gjson.GetBytes(rec.Body.Bytes(), "id").String())
			}

			repo := &settlementCapture{}
			billing := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			billing.usageSettlement = NewUsageSettlementService(repo, nil, nil)
			err = billing.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, Account: account, APIKey: &APIKey{ID: 2}, User: &User{ID: 1}})
			require.NotNil(t, repo.receipt)
			require.Equal(t, tc.input, result.Usage.InputTokens)
			require.Equal(t, tc.input-tc.cached, repo.receipt.Usage.InputTokens)
			require.Equal(t, tc.output, repo.receipt.Usage.OutputTokens)
			require.Equal(t, tc.cached, repo.receipt.Usage.CacheReadTokens)
			if tc.review {
				require.ErrorIs(t, err, ErrUsageSettlementNeedsReview)
				require.Equal(t, "unresolved", repo.receipt.ResolutionState)
				require.Zero(t, repo.settles)
			} else {
				require.NoError(t, err)
				require.Equal(t, "resolved", repo.receipt.ResolutionState)
				require.Equal(t, 1, repo.settles)
			}
		})
	}
}
