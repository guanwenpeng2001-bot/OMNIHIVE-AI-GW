//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

// chatOnlyProbeStub is a local HTTP server used as the account base URL, so the
// assertion is "the request reached the configured base URL", not merely "a
// transport double was called".
type chatOnlyProbeStub struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
}

func (stub *chatOnlyProbeStub) serve(writer http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	stub.mu.Lock()
	stub.requests = append(stub.requests, request)
	stub.bodies = append(stub.bodies, string(body))
	stub.mu.Unlock()

	writer.Header().Set("Content-Type", "text/event-stream")
	_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"stub ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
}

func (stub *chatOnlyProbeStub) snapshot() ([]*http.Request, []string) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return append([]*http.Request(nil), stub.requests...), append([]string(nil), stub.bodies...)
}

// chatOnlyProbeStubTransport dials the stub server instead of serving a canned
// response, so a probe that never leaves the process cannot pass.
type chatOnlyProbeStubTransport struct {
	client *http.Client
}

func (transport chatOnlyProbeStubTransport) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return transport.client.Do(request)
}

func (transport chatOnlyProbeStubTransport) DoWithTLS(request *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return transport.Do(request, proxyURL, accountID, accountConcurrency)
}

func chatOnlyProbeTestAccount(id int64, platform, baseURL string) *Account {
	return &Account{
		ID:          id,
		Name:        platform + "-chat-only",
		Platform:    platform,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-chat-only-stub",
			"base_url": baseURL,
		},
	}
}

// chatOnlyPlatforms are the four chat-only platforms, in the order the create
// form offers them.
var chatOnlyPlatforms = []string{PlatformQianfan, PlatformHunyuan, PlatformMistral, PlatformVolcengine}

// runChatOnlyPlatformProbe probes one chat-only account against a local httptest
// stub and returns what the stub observed plus the SSE transcript the probe
// emitted, so callers can assert both sides of the same request.
func runChatOnlyPlatformProbe(t *testing.T, platform, model string) ([]*http.Request, []string, string) {
	t.Helper()

	stub := &chatOnlyProbeStub{}
	server := httptest.NewServer(http.HandlerFunc(stub.serve))
	defer server.Close()

	account := chatOnlyProbeTestAccount(900, platform, server.URL)
	svc := &AccountTestService{
		accountRepo: &openAIAccountTestRepo{
			mockAccountRepoForGemini: mockAccountRepoForGemini{
				accountsByID: map[int64]*Account{account.ID: account},
			},
		},
		httpUpstream: chatOnlyProbeStubTransport{client: server.Client()},
		cfg:          rawChatCompletionsTestConfig(),
	}
	c, recorder := newTestContext()

	require.NoError(t, svc.TestAccountConnection(c, account.ID, model, "hi", AccountTestModeDefault))
	requests, bodies := stub.snapshot()
	return requests, bodies, recorder.Body.String()
}

// The four chat-only platforms (qianfan / hunyuan / mistral / volcengine) are
// forwarded by the data plane on the OpenAI-compatible chat completions trunk,
// so "test connection" has to reach the account's configured base URL instead
// of dead-ending in "Account platform probe unsupported".
func TestAccountTestService_ChatOnlyPlatformsProbeConfiguredBaseURL(t *testing.T) {
	for _, platform := range chatOnlyPlatforms {
		t.Run(platform, func(t *testing.T) {
			requests, bodies, transcript := runChatOnlyPlatformProbe(t, platform, "")

			require.Len(t, requests, 1, "the probe must reach the configured base URL")
			require.Equal(t, "/v1/chat/completions", requests[0].URL.Path)
			require.Equal(t, "Bearer sk-chat-only-stub", requests[0].Header.Get("Authorization"))
			require.Contains(t, bodies[0], `"stream":true`)
			require.Contains(t, transcript, `"type":"test_complete"`)
			require.NotContains(t, transcript, "Account platform probe unsupported")
		})
	}
}

// The model catalog advertises probe_supported=true for every non-media model
// (AccountModelProbeSupported is the same predicate the probe dispatch uses), so
// a model the list calls probeable must reach the platform's OpenAI-compatible
// trunk instead of failing with "Account platform probe unsupported".
func TestAccountTestService_ChatOnlyPlatformProbeHonorsProbeSupportedMetadata(t *testing.T) {
	// The model the consistency audit read out of the qianfan catalog with
	// probe_supported=true while every probe failed at the platform dispatch.
	const advertisedModel = "bce-reranker-base"

	for _, platform := range chatOnlyPlatforms {
		t.Run(platform, func(t *testing.T) {
			require.True(t, AccountModelProbeSupported(chatOnlyProbeTestAccount(901, platform, ""), advertisedModel))

			requests, bodies, transcript := runChatOnlyPlatformProbe(t, platform, advertisedModel)

			require.Len(t, requests, 1)
			require.Contains(t, bodies[0], `"model":"`+advertisedModel+`"`)
			require.NotContains(t, transcript, "Account platform probe unsupported")
		})
	}
}

// The probe URL must equal the target the data plane builds for the official
// default base URL of each chat-only platform:
//
//	qianfan    buildQianfanRequest forces the /v2 path, then "/chat/completions"
//	hunyuan    buildHunyuanChatRequest uses base + "/chat/completions"
//	mistral    openAIChatCompletionsTargetURL uses buildOpenAIChatCompletionsURL
//	volcengine prepareVolcengineChatRequest uses buildOpenAIChatCompletionsURL
//
// so "can forward" and "can test connection" resolve to the same endpoint.
func TestAccountTestService_ChatOnlyPlatformDefaultProbeURLMatchesDataPlane(t *testing.T) {
	cases := []struct {
		platform string
		expected string
	}{
		{PlatformQianfan, "https://qianfan.baidubce.com/v2/chat/completions"},
		{PlatformHunyuan, "https://tokenhub.tencentmaas.com/v1/chat/completions"},
		{PlatformMistral, "https://api.mistral.ai/v1/chat/completions"},
		{PlatformVolcengine, "https://ark.cn-beijing.volces.com/api/v3/chat/completions"},
	}
	for index, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			account := chatOnlyProbeTestAccount(int64(800+index), tc.platform, "")
			require.Equal(t, tc.expected, buildOpenAIChatCompletionsURL(account.GetOpenAIBaseURL()))
		})
	}
}
