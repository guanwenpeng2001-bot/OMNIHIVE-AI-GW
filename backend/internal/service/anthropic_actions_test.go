package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Synthetic, credential-free fixtures based on the official API documentation
// retrieved 2026-09-21; these are not claimed to be captured production traffic.
const anthropicBatchFixture = `{"requests":[{"custom_id":"vision-request","params":{"model":"claude-sonnet-4-5","max_tokens":32,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/photo.png"}},{"type":"text","text":"Describe this","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}}]}`

func anthropicBatchJobFixture() *UpstreamJob {
	groupID := int64(30)
	return &UpstreamJob{Platform: PlatformAnthropic, Family: "batch", UserID: 10, APIKeyID: 20, GroupID: &groupID, AccountID: 201}
}

func TestAnthropicMessageBatchWireLifecycle(t *testing.T) {
	account := newAnthropicAPIKeyAccountForTest()
	service := &GatewayService{cfg: &config.Config{}}
	job := anthropicBatchJobFixture()
	for _, operation := range []string{"create", "retrieve", "cancel", "results", "delete"} {
		t.Run(operation, func(t *testing.T) {
			if operation != "create" {
				job.UpstreamTaskID = "msgbatch_fixture"
			}
			request, err := service.BuildAnthropicMessageBatchRequest(context.Background(), account, job, 10, 20, 30, operation, []byte(anthropicBatchFixture))
			require.NoError(t, err)
			require.Equal(t, "upstream-anthropic-key", request.Header.Get("x-api-key"))
			require.Empty(t, request.Header.Get("Authorization"))
			require.Equal(t, "2023-06-01", request.Header.Get("anthropic-version"))
			expectedMethod, expectedPath := http.MethodGet, "/v1/messages/batches/msgbatch_fixture"
			switch operation {
			case "create":
				expectedMethod, expectedPath = http.MethodPost, "/v1/messages/batches"
			case "cancel":
				expectedMethod, expectedPath = http.MethodPost, expectedPath+"/cancel"
			case "results":
				expectedPath += "/results"
			case "delete":
				expectedMethod = http.MethodDelete
			}
			require.Equal(t, expectedMethod, request.Method)
			require.Equal(t, "https://api.anthropic.com"+expectedPath, request.URL.String())
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			if operation == "create" {
				require.JSONEq(t, anthropicBatchFixture, string(body))
			} else {
				require.Empty(t, body)
			}
		})
	}
}

func TestAnthropicMessageBatchHTTPRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/messages/batches", request.URL.Path)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "upstream-anthropic-key", request.Header.Get("x-api-key"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, anthropicBatchFixture, string(body))
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"id":"msgbatch_fixture","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1}}`)
	}))
	defer server.Close()
	configuration := &config.Config{}
	configuration.Security.URLAllowlist.AllowInsecureHTTP = true
	service := &GatewayService{cfg: configuration}
	account := newAnthropicAPIKeyAccountForTest()
	account.Credentials["base_url"] = server.URL + "/v1/"
	request, err := service.BuildAnthropicMessageBatchRequest(context.Background(), account, anthropicBatchJobFixture(), 10, 20, 30, "create", []byte(anthropicBatchFixture))
	require.NoError(t, err)
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	state, err := ParseAnthropicMessageBatchState(body, "")
	require.NoError(t, err)
	require.Equal(t, "msgbatch_fixture", state.ID)
	require.Equal(t, 1, state.RequestCounts.Processing)
}

func TestAnthropicMessageBatchOwnerAndCredentialBoundary(t *testing.T) {
	service := &GatewayService{cfg: &config.Config{}}
	for _, operation := range []string{"create", "retrieve", "cancel", "results", "delete"} {
		for _, mutate := range []func(*UpstreamJob){
			func(job *UpstreamJob) { job.UserID++ }, func(job *UpstreamJob) { job.APIKeyID++ },
			func(job *UpstreamJob) { job.GroupID = nil }, func(job *UpstreamJob) { *job.GroupID++ },
			func(job *UpstreamJob) { job.AccountID++ }, func(job *UpstreamJob) { job.Platform = PlatformOpenAI },
			func(job *UpstreamJob) { job.Family = "files" },
		} {
			job := anthropicBatchJobFixture()
			mutate(job)
			_, err := service.BuildAnthropicMessageBatchRequest(context.Background(), newAnthropicAPIKeyAccountForTest(), job, 10, 20, 30, operation, []byte(anthropicBatchFixture))
			require.ErrorIs(t, err, ErrUpstreamJobNotFound)
		}
	}
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeBedrock, AccountTypeServiceAccount} {
		account := newAnthropicAPIKeyAccountForTest()
		account.Type = accountType
		_, err := service.BuildAnthropicMessageBatchRequest(context.Background(), account, anthropicBatchJobFixture(), 10, 20, 30, "create", []byte(anthropicBatchFixture))
		require.ErrorContains(t, err, "not verified")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.BuildAnthropicMessageBatchRequest(ctx, newAnthropicAPIKeyAccountForTest(), anthropicBatchJobFixture(), 10, 20, 30, "create", []byte(anthropicBatchFixture))
	require.ErrorIs(t, err, context.Canceled)
}

func TestAnthropicMessageBatchValidation(t *testing.T) {
	require.NoError(t, ValidateAnthropicMessageBatch([]byte(anthropicBatchFixture)))
	for _, body := range []string{`{}`, `{"requests":[]}`, `null`, `{`,
		strings.Replace(anthropicBatchFixture, `"max_tokens":32`, `"max_tokens":0`, 1),
		strings.Replace(anthropicBatchFixture, `"max_tokens":32`, `"max_tokens":32,"stream":true`, 1),
		strings.Replace(anthropicBatchFixture, `"max_tokens":32`, `"max_tokens":32,"speed":"fast"`, 1),
		strings.Replace(anthropicBatchFixture, "vision-request", "../foreign", 1),
		`{"requests":[{"custom_id":"same","params":{"model":"claude","max_tokens":1,"messages":[{}]}},{"custom_id":"same","params":{"model":"claude","max_tokens":1,"messages":[{}]}}]}`,
	} {
		require.Error(t, ValidateAnthropicMessageBatch([]byte(body)), body)
	}
}

func TestAnthropicMessageBatchStateAndPartialCancellation(t *testing.T) {
	for _, status := range []string{"in_progress", "canceling", "ended"} {
		state, err := ParseAnthropicMessageBatchState([]byte(`{"id":"msgbatch_fixture","type":"message_batch","processing_status":"`+status+`","request_counts":{"succeeded":1,"canceled":2}}`), "msgbatch_fixture")
		require.NoError(t, err)
		require.Equal(t, 1, state.RequestCounts.Succeeded)
		require.Equal(t, 2, state.RequestCounts.Canceled)
	}
	for _, body := range []string{`{}`, `{"id":"msgbatch_other","type":"message_batch","processing_status":"ended"}`, `{"id":"msgbatch_fixture","type":"message_batch","processing_status":"unknown"}`, `{"id":"msgbatch_fixture","type":"message_batch","processing_status":"ended","request_counts":{"processing":1}}`} {
		_, err := ParseAnthropicMessageBatchState([]byte(body), "msgbatch_fixture")
		require.Error(t, err)
	}
}

func TestAnthropicMessageBatchUsageAndPricing(t *testing.T) {
	line := `{"custom_id":"vision-request","result":{"type":"succeeded","message":{"type":"message","model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":40,"ephemeral_1h_input_tokens":50}}}}}`
	result, err := ParseAnthropicMessageBatchResult([]byte(line))
	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4-5", result.Model)
	require.Equal(t, &ClaudeUsage{InputTokens: 10, OutputTokens: 20, CacheReadInputTokens: 30, CacheCreationInputTokens: 90, CacheCreation5mTokens: 40, CacheCreation1hTokens: 50}, result.Usage)
	for _, status := range []string{"canceled", "expired", "errored"} {
		result, err := ParseAnthropicMessageBatchResult([]byte(`{"custom_id":"failed-request","result":{"type":"` + status + `","error":{"type":"invalid_request_error"}}}`))
		require.NoError(t, err)
		require.Nil(t, result.Usage)
	}
	for _, invalid := range []string{`{}`, strings.Replace(line, `"input_tokens":10`, `"input_tokens":-1`, 1), strings.Replace(line, `"input_tokens":10,`, "", 1), strings.Replace(line, `"input_tokens":10`, `"input_tokens":1.5`, 1)} {
		_, err := ParseAnthropicMessageBatchResult([]byte(invalid))
		require.Error(t, err)
	}
	standard := ModelPricing{InputPricePerToken: 3, OutputPricePerToken: 15, CacheCreationPricePerToken: 3.75, CacheReadPricePerToken: .3, CacheCreation5mPrice: 3.75, CacheCreation1hPrice: 6}
	batch := AnthropicMessageBatchTokenPricing(standard)
	require.Equal(t, 1.5, batch.InputPricePerToken)
	require.Equal(t, 7.5, batch.OutputPricePerToken)
	require.Equal(t, .15, batch.CacheReadPricePerToken)
	require.Equal(t, 1.875, batch.CacheCreation5mPrice)
	require.Equal(t, 3.0, batch.CacheCreation1hPrice)
	require.Equal(t, 3.0, standard.InputPricePerToken)
}

func TestAnthropicMessageBatchFingerprintUsesSharedIdempotency(t *testing.T) {
	job := anthropicBatchJobFixture()
	first, err := UpstreamJobFingerprint(job, []byte(anthropicBatchFixture))
	require.NoError(t, err)
	second, err := UpstreamJobFingerprint(job, []byte("\n "+anthropicBatchFixture))
	require.NoError(t, err)
	require.Equal(t, first, second)
	job.APIKeyID++
	other, err := UpstreamJobFingerprint(job, []byte(anthropicBatchFixture))
	require.NoError(t, err)
	require.NotEqual(t, first, other)
}
