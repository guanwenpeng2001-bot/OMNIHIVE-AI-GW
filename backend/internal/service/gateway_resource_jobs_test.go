//go:build unit

package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// gatewayResourceHTTPUpstream performs real HTTP against a loopback fixture so
// the resource actions are exercised end to end through the provider adapters.
type gatewayResourceHTTPUpstream struct{ client *http.Client }

func (u gatewayResourceHTTPUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(request)
}

func (u gatewayResourceHTTPUpstream) DoWithTLS(request *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxyURL, accountID, concurrency)
}

func gatewayResourceMultipartFixture(t *testing.T, filename string, content []byte) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("purpose", "batch"))
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body.Bytes(), writer.FormDataContentType()
}

func gatewayResourceFixtureJob(t *testing.T, platform, family, model string, hashes []string, requestCount int) *UpstreamJob {
	t.Helper()
	groupID := int64(30)
	account := openAIActionAccount()
	if platform == PlatformAnthropic {
		account = newAnthropicAPIKeyAccountForTest()
	}
	prices := GatewayResourceBatchTokenPricing(*gatewayResourceTestPricing())
	return &UpstreamJob{
		PublicRequestID: "public-batch", RequestFingerprint: "fingerprint", Platform: platform, Family: family,
		UserID: 10, APIKeyID: 20, GroupID: &groupID, AccountID: account.ID, Status: "running", UpstreamTaskID: "upstream-batch",
		Billing: UpstreamJobBilling{
			Model: model, RequestedModel: model, RateMultiplier: 1, AccountRateMultiplier: 1,
			ResourceBatch: &GatewayResourceBatchSnapshot{
				Model: model, UpstreamModel: model, Endpoint: "/v1/chat/completions", PricingMode: GatewayResourcePricingModeTokens,
				TokenPrices: &prices, PriceSource: "group", RequestCount: requestCount, CustomIDHashes: hashes,
			},
		},
	}
}

func TestGatewayResourceOpenAIBatchLifecycleSettlesOnlyValidatedOutput(t *testing.T) {
	const outputLine = `{"custom_id":"row-1","response":{"status_code":200,"body":{"model":"gpt-4o","usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500}}},"error":null}`
	completed := false
	outputBody := outputLine
	requests := make([]string, 0, 8)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/files":
			_, _ = io.WriteString(writer, `{"id":"file_in","object":"file","purpose":"batch","status":"processed"}`)
		case request.Method == http.MethodPost && request.URL.Path == "/v1/batches":
			// The provider reports immediate completion; settlement still waits for
			// output validation.
			_, _ = io.WriteString(writer, `{"id":"batch_up","object":"batch","status":"completed","endpoint":"/v1/chat/completions","input_file_id":"file_in","output_file_id":"file_out","request_counts":{"total":1,"completed":1,"failed":0}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/batches/batch_up":
			if !completed {
				_, _ = io.WriteString(writer, `{"id":"batch_up","object":"batch","status":"in_progress","endpoint":"/v1/chat/completions","input_file_id":"file_in","request_counts":{"total":1,"completed":0,"failed":0}}`)
				return
			}
			_, _ = io.WriteString(writer, `{"id":"batch_up","object":"batch","status":"completed","endpoint":"/v1/chat/completions","input_file_id":"file_in","output_file_id":"file_out","request_counts":{"total":1,"completed":1,"failed":0}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/files/file_out/content":
			_, _ = io.WriteString(writer, outputBody)
		case request.Method == http.MethodPost && request.URL.Path == "/v1/batches/batch_up/cancel":
			_, _ = io.WriteString(writer, `{"id":"batch_up","object":"batch","status":"cancelling"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	account := openAIActionAccount()
	account.Status = StatusActive
	account.Credentials["base_url"] = server.URL
	service := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig(), httpUpstream: gatewayResourceHTTPUpstream{client: server.Client()}, accountRepo: openAIVideoAccountRepository{account: account}}
	ctx := context.Background()

	fileBody, contentType := gatewayResourceMultipartFixture(t, "input.jsonl", []byte(gatewayResourceValidJSONLLine+"\n"))
	fileUpdate, err := service.SubmitOpenAIFile(ctx, account, contentType, fileBody)
	require.NoError(t, err)
	require.Equal(t, "succeeded", fileUpdate.Status)
	require.Equal(t, "file_in", fileUpdate.TaskID)

	batchBody := []byte(`{"input_file_id":"file_in","endpoint":"/v1/chat/completions","completion_window":"24h"}`)
	batchUpdate, err := service.SubmitOpenAIBatch(ctx, account, batchBody)
	require.NoError(t, err)
	require.Equal(t, "running", batchUpdate.Status, "an immediately completed batch is still validated before settlement")
	require.Equal(t, "batch_up", batchUpdate.TaskID)

	job := gatewayResourceFixtureJob(t, PlatformOpenAI, "batch", "gpt-4o", []string{gatewayResourceHash([]byte("row-1"))}, 1)
	job.UpstreamTaskID = "batch_up"
	poll := GatewayResourcePollJob(service)
	running, err := poll(ctx, job)
	require.NoError(t, err)
	require.Equal(t, "running", running.Status)
	require.NotEmpty(t, running.Usage)

	completed = true
	job.UsageJSON = running.Usage
	settled, err := poll(ctx, job)
	require.NoError(t, err)
	require.Equal(t, "succeeded", settled.Status)
	require.Equal(t, "completed", settled.ProviderStatus)

	job.UsageJSON = settled.Usage
	job.Status = "succeeded"
	log, command, err := GatewayResourceJobCharge(job)
	require.NoError(t, err)
	require.Equal(t, 1000, log.InputTokens)
	require.Equal(t, 500, log.OutputTokens)
	require.InDelta(t, QuantizeUsageBillingAmount(1000*0.0000015+500*0.0000075), command.BalanceCost, 1e-12)

	// An output line that cannot be matched to an admitted request is never billed.
	outputBody = strings.Replace(outputLine, "row-1", "row-unknown", 1)
	rejected, err := poll(ctx, job)
	require.NoError(t, err)
	require.Equal(t, "failed", rejected.Status)
	require.Equal(t, "unsupported_usage", rejected.ErrorCode)

	outputBody = outputLine
	require.NoError(t, service.ManageOpenAIResource(ctx, job, "cancel"))
	require.Contains(t, requests, "POST /v1/batches/batch_up/cancel")
	require.Contains(t, requests, "GET /v1/files/file_out/content")
}

func TestGatewayResourceAnthropicMessageBatchLifecycleSettlesEndedResults(t *testing.T) {
	const resultsLine = `{"custom_id":"vision-request","result":{"type":"succeeded","message":{"id":"msg_1","type":"message","model":"claude-sonnet-4-5","usage":{"input_tokens":120,"output_tokens":40,"cache_creation_input_tokens":10,"cache_read_input_tokens":5},"content":[{"type":"text","text":"ok"}]}}}`
	ended := false
	resultsBody := resultsLine
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/messages/batches":
			_, _ = io.WriteString(writer, `{"id":"msgbatch_fixture","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/messages/batches/msgbatch_fixture":
			if !ended {
				_, _ = io.WriteString(writer, `{"id":"msgbatch_fixture","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0}}`)
				return
			}
			_, _ = io.WriteString(writer, `{"id":"msgbatch_fixture","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/messages/batches/msgbatch_fixture/results":
			writer.Header().Set("Content-Type", "application/x-jsonl")
			_, _ = io.WriteString(writer, resultsBody)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	account := newAnthropicAPIKeyAccountForTest()
	account.Credentials["base_url"] = server.URL + "/v1"
	service := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig(), httpUpstream: gatewayResourceHTTPUpstream{client: server.Client()}, accountRepo: openAIVideoAccountRepository{account: account}}
	ctx := context.Background()

	created, err := service.SubmitAnthropicMessageBatch(ctx, account, anthropicBatchJobFixture(), []byte(anthropicBatchFixture))
	require.NoError(t, err)
	require.Equal(t, "running", created.Status)
	require.Equal(t, "msgbatch_fixture", created.TaskID)

	job := gatewayResourceFixtureJob(t, PlatformAnthropic, "batch", "claude-sonnet-4-5", []string{gatewayResourceHash([]byte("vision-request"))}, 1)
	job.UpstreamTaskID = "msgbatch_fixture"
	poll := GatewayResourcePollJob(service)
	running, err := poll(ctx, job)
	require.NoError(t, err)
	require.Equal(t, "running", running.Status)

	ended = true
	job.UsageJSON = running.Usage
	settled, err := poll(ctx, job)
	require.NoError(t, err)
	require.Equal(t, "succeeded", settled.Status)

	job.UsageJSON = settled.Usage
	job.Status = "succeeded"
	log, _, err := GatewayResourceJobCharge(job)
	require.NoError(t, err)
	require.Equal(t, 120, log.InputTokens)
	require.Equal(t, 40, log.OutputTokens)
	require.Equal(t, 10, log.CacheCreationTokens)
	require.Equal(t, 5, log.CacheReadTokens)

	// A server tool response carries fees outside the token tariff.
	resultsBody = strings.Replace(resultsLine, `{"type":"text","text":"ok"}`, `{"type":"server_tool_use","name":"web_search"}`, 1)
	rejected, err := poll(ctx, job)
	require.NoError(t, err)
	require.Equal(t, "failed", rejected.Status)
	require.Equal(t, "unsupported_usage", rejected.ErrorCode)

	resultsBody = resultsLine
	err = service.ManageAnthropicResource(ctx, job, "cancel")
	require.Error(t, err, "a loopback fixture without a cancel route is reported, not silently accepted")
}

func TestGatewayResourceAnthropicWireMatchesPlatformAdapter(t *testing.T) {
	account := newAnthropicAPIKeyAccountForTest()
	adapter := &GatewayService{cfg: &config.Config{}}
	mine := &OpenAIGatewayService{cfg: &config.Config{}}
	for _, operation := range []string{"create", "retrieve", "cancel", "results", "delete"} {
		t.Run(operation, func(t *testing.T) {
			job := anthropicBatchJobFixture()
			if operation != "create" {
				job.UpstreamTaskID = "msgbatch_fixture"
			}
			expected, err := adapter.BuildAnthropicMessageBatchRequest(context.Background(), account, job, 10, 20, 30, operation, []byte(anthropicBatchFixture))
			require.NoError(t, err)
			actual, err := mine.buildAnthropicMessageBatchRequest(context.Background(), account, job, operation, []byte(anthropicBatchFixture))
			require.NoError(t, err)
			require.Equal(t, expected.Method, actual.Method)
			require.Equal(t, expected.URL.String(), actual.URL.String())
			require.Equal(t, expected.Header.Get("x-api-key"), actual.Header.Get("x-api-key"))
			require.Equal(t, expected.Header.Get("anthropic-version"), actual.Header.Get("anthropic-version"))
			require.Equal(t, expected.Header.Get("Content-Type"), actual.Header.Get("Content-Type"))
			expectedBody, err := io.ReadAll(expected.Body)
			require.NoError(t, err)
			actualBody, err := io.ReadAll(actual.Body)
			require.NoError(t, err)
			require.Equal(t, string(expectedBody), string(actualBody))
		})
	}
	foreign := anthropicBatchJobFixture()
	foreign.AccountID = 999
	_, err := mine.buildAnthropicMessageBatchRequest(context.Background(), account, foreign, "retrieve", nil)
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
	wrongPlatform := anthropicBatchJobFixture()
	wrongPlatform.Platform = PlatformOpenAI
	_, err = mine.buildAnthropicMessageBatchRequest(context.Background(), account, wrongPlatform, "retrieve", nil)
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
	_, err = mine.buildAnthropicMessageBatchRequest(context.Background(), account, anthropicBatchJobFixture(), "unknown", nil)
	require.Error(t, err)
}

func TestGatewayResourceModerationForwardsNativeShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/moderations", request.URL.Path)
		require.Equal(t, "Bearer sk-fixture", request.Header.Get("Authorization"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"input":"check this","model":"omni-moderation-latest"}`, string(body))
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"id":"modr-1","model":"omni-moderation-latest","results":[{"flagged":false}]}`)
	}))
	defer server.Close()
	account := openAIActionAccount()
	account.Status = StatusActive
	account.Credentials["base_url"] = server.URL
	service := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig(), httpUpstream: gatewayResourceHTTPUpstream{client: server.Client()}, accountRepo: openAIVideoAccountRepository{account: account}}

	response, err := service.ForwardOpenAIModeration(context.Background(), account, []byte(`{"input":"check this","model":"omni-moderation-latest"}`))
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusOK, response.StatusCode)
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "modr-1", strings.TrimSpace(strings.Split(strings.Split(string(raw), `"id":"`)[1], `"`)[0]))

	_, err = service.ForwardOpenAIModeration(context.Background(), account, []byte(`{"model":"omni-moderation-latest"}`))
	require.Error(t, err, "a moderation request without input is refused before the upstream call")
	_, err = service.ForwardOpenAIModeration(context.Background(), account, []byte(`not json`))
	require.Error(t, err)
}

func TestGatewayResourceBatchUsageJSONRequiresConsistentCounts(t *testing.T) {
	_, err := GatewayResourceBatchUsageJSON(GatewayResourceBatchUsage{Lines: 2, Matched: 3})
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)
	encoded, err := GatewayResourceBatchUsageJSON(GatewayResourceBatchUsage{
		RequestCounts: map[string]int64{"total": 2, "completed": 1, "failed": 1}, ProviderStatus: "completed",
		Lines: 2, Matched: 1, InputTokens: 10,
	})
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Contains(t, decoded, "request_counts")
	require.Contains(t, decoded, "result")
}

func TestGatewayResourceBoundedReaderStopsOversizedStreams(t *testing.T) {
	reader := &gatewayResourceBoundedReader{reader: strings.NewReader(strings.Repeat("x", 4096)), limit: 1024}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
	}
	require.ErrorIs(t, scanner.Err(), errGatewayResourceStreamTooLarge)
}

func TestGatewayResourceFileSettlementBuildsZeroCostReceipt(t *testing.T) {
	repo := newGatewayResourceMemoryRepo()
	groupID := int64(30)
	account := openAIActionAccount()
	key := &APIKey{ID: 20, UserID: 10, GroupID: &groupID}
	snapshot := &GatewayResourceFileSnapshot{Purpose: "batch", Filename: "input.jsonl", Bytes: 64, Lines: 1, CustomIDHashes: []string{"h"}}
	billing := GatewayResourceFileBillingSnapshot(snapshot, account, key, nil)
	jobs := NewUpstreamJobService(repo, nil, GatewayResourceJobCharge)
	template := &UpstreamJob{Platform: PlatformOpenAI, Family: "files", AccountID: account.ID, UserID: 10, APIKeyID: 20, GroupID: &groupID, Billing: billing}
	job, err := jobs.Submit(context.Background(), template, []byte(`{"payload_sha256":"h"}`), func(context.Context) (UpstreamJobUpdate, error) {
		return UpstreamJobUpdate{TaskID: "file_upstream", Status: "succeeded", ResultRef: "file_upstream"}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "succeeded", job.Status)
	require.NotNil(t, job.ChargedUsageLogID)
	require.Equal(t, 1, repo.charges)
	require.Equal(t, PricingStateFree, job.Billing.PricingState)
}

func TestGatewayResourceOpenAIUsageSplitsMutuallyExclusiveInputBuckets(t *testing.T) {
	tokens, ok := gatewayResourceOpenAIUsageTokens("/v1/chat/completions", gjson.Parse(`{"usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500,"prompt_tokens_details":{"cached_tokens":400,"reasoning_tokens":0}}}`))
	require.True(t, ok)
	require.Equal(t, 600, tokens.InputTokens, "cached tokens are billed once, as cache reads")
	require.Equal(t, 400, tokens.CacheReadTokens)
	require.Equal(t, 500, tokens.OutputTokens)

	responses, ok := gatewayResourceOpenAIUsageTokens("/v1/responses", gjson.Parse(`{"usage":{"input_tokens":800,"output_tokens":200,"total_tokens":1000,"input_tokens_details":{"cached_tokens":100}}}`))
	require.True(t, ok)
	require.Equal(t, 700, responses.InputTokens)
	require.Equal(t, 100, responses.CacheReadTokens)

	_, ok = gatewayResourceOpenAIUsageTokens("/v1/chat/completions", gjson.Parse(`{"usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110,"prompt_tokens_details":{"cached_tokens":200}}}`))
	require.False(t, ok, "a cache read larger than the prompt is refused")

	_, ok = gatewayResourceOpenAIUsageTokens("/v1/chat/completions", gjson.Parse(`{"usage":{}}`))
	require.False(t, ok)

	free, ok := gatewayResourceOpenAIUsageTokens("/v1/moderations", gjson.Parse(`{}`))
	require.True(t, ok)
	require.Zero(t, free.OutputTokens)
}

type gatewayResourceCancelledBatchFixture struct {
	final bool
}

// serve answers a cancelled OpenAI batch: first with unfinished accounting, then
// with the final counters and the output file.
func (f *gatewayResourceCancelledBatchFixture) serve() (*OpenAIGatewayService, func()) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v1/batches/"):
			if !f.final {
				_, _ = io.WriteString(writer, `{"id":"batch_up","object":"batch","status":"cancelled","endpoint":"/v1/chat/completions","input_file_id":"file_in","request_counts":{"total":2,"completed":1,"failed":0}}`)
				return
			}
			_, _ = io.WriteString(writer, `{"id":"batch_up","object":"batch","status":"cancelled","endpoint":"/v1/chat/completions","input_file_id":"file_in","output_file_id":"file_out","request_counts":{"total":2,"completed":1,"failed":1}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/files/file_out/content":
			_, _ = io.WriteString(writer, `{"custom_id":"row-1","response":{"status_code":200,"body":{"model":"gpt-4o","usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}},"error":null}`)
			_, _ = io.WriteString(writer, "\n")
			_, _ = io.WriteString(writer, `{"custom_id":"row-2","error":{"code":"cancelled","message":"cancelled"}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	account := openAIActionAccount()
	account.Status = StatusActive
	account.Credentials["base_url"] = server.URL
	service := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig(), httpUpstream: gatewayResourceHTTPUpstream{client: server.Client()}, accountRepo: openAIVideoAccountRepository{account: account}}
	return service, server.Close
}

func TestGatewayResourceCancelledBatchSettlesOnlyFullyAccountedWork(t *testing.T) {
	fixture := &gatewayResourceCancelledBatchFixture{}
	service, cleanup := fixture.serve()
	defer cleanup()
	job := gatewayResourceFixtureJob(t, PlatformOpenAI, "batch", "gpt-4o", []string{gatewayResourceHash([]byte("row-1")), gatewayResourceHash([]byte("row-2"))}, 2)
	job.UpstreamTaskID = "batch_up"
	poll := GatewayResourcePollJob(service)

	// The provider still runs the admitted requests, so the batch is not settled.
	running, err := poll(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, "running", running.Status)
	require.Equal(t, "cancelled", running.ProviderStatus, "the durable state keeps reporting the cancellation")

	job.UsageJSON = running.Usage
	fixture.final = true
	final, err := poll(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, "succeeded", final.Status, "the completed portion settles once the accounting is final")

	job.UsageJSON, job.Status = final.Usage, "succeeded"
	log, _, err := GatewayResourceJobCharge(job)
	require.NoError(t, err)
	require.Equal(t, 100, log.InputTokens)
	require.Equal(t, 50, log.OutputTokens)
}
