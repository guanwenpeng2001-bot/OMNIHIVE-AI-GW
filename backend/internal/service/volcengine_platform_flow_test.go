//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The responses below are synthetic transport fixtures, not vendor attestations.
func TestVolcenginePlatformFlow(test *testing.T) {
	account := volcengineTestAccount()
	require.Contains(test, ConcretePlatforms(), account.Platform)
	require.Equal(test, volcengineRuntimeBaseURL, account.GetOpenAIBaseURL())
	require.Equal(test, "inference-test-key", account.GetOpenAIProtocolAPIKey())
	require.True(test, IsChatOnlyPlatform(account.Platform))
	require.True(test, account.IsOpenAICompatible())
	require.Equal(test, account.Platform, NormalizeOpenAICompatiblePlatform(account.Platform))
	require.True(test, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	// The platform row publishes ResponsesPOST, so the capability gate admits the
	// native Ark Responses API at {root}/responses next to the embeddings cell
	// multimodal-ingress bound. The reviewed profile also publishes native Messages.
	require.True(test, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	require.True(test, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityEmbeddings))
	declared := catalogEvidenceAccount(42, PlatformVolcengine, "native")
	declared.Credentials["model_mapping"] = map[string]any{"alias": "ep-target"}
	delete(declared.Extra, UpstreamModelCatalogExtraKey)
	declared.Extra[ModelAvailabilityExtraKey] = []ModelAvailabilityDeclaration{{AccountID: 42, Model: "ep-target", Endpoint: "chat_completions", ConfirmedBy: 7, ConfirmedAt: time.Now().Add(-time.Minute), Reason: "fixture", EvidenceKind: ModelEvidenceAdministrator}}
	require.True(test, AccountSupportsResolvedModel(context.Background(), &declared, "alias", AccountModelBindingOptions{Endpoint: "chat_completions"}))
	require.True(test, AccountSupportsResolvedModel(context.Background(), &declared, "alias", AccountModelBindingOptions{Endpoint: "responses"}), "saved selection can use the installed Responses transport")
	require.False(test, AccountSupportsResolvedModel(context.Background(), &declared, "unselected", AccountModelBindingOptions{Endpoint: "responses"}))
	require.True(test, AccountSupportsResolvedModel(context.Background(), &declared, "alias", AccountModelBindingOptions{Endpoint: "messages"}), "saved selection can use the installed native Messages transport")
	discoveryTransport := &httpUpstreamRecorder{}
	discoveryService := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: discoveryTransport}
	discoveryRequest, discoveryErr := discoveryService.buildUpstreamModelsRequest(context.Background(), account)
	require.ErrorContains(test, discoveryErr, "no documented model discovery endpoint")
	require.Nil(test, discoveryRequest)
	// Official accounts expose candidates without treating them as discovered availability.
	preview, discoveryErr := discoveryService.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(test, discoveryErr)
	require.Equal(test, "platform-candidates", preview.CatalogSource)
	require.Empty(test, preview.DiscoveredModels)
	require.Empty(test, preview.EligibleModels)
	require.False(test, preview.DiscoveryComplete)
	require.Nil(test, preview.Review)
	require.Nil(test, discoveryTransport.lastReq)
	for _, stream := range []bool{false, true} {
		name := "buffered"
		if stream {
			name = "stream"
		}
		test.Run(name, func(test *testing.T) {
			body := []byte(`{"model":"alias","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"}}}]}`)
			responseBody := `{"id":"fixture","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`
			if stream {
				body = []byte(strings.TrimSuffix(string(body), "}") + `,"stream":true}`)
				responseBody = "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n"
			}
			transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responseBody))}}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body, "", "ep-exact-target")
			require.NoError(test, err)
			require.NotNil(test, result)
			require.Equal(test, 7, result.Usage.InputTokens)
			require.Equal(test, 3, result.Usage.OutputTokens)
			require.Equal(test, volcengineRuntimeBaseURL+"/chat/completions", transport.lastReq.URL.String())
			require.Equal(test, "Bearer inference-test-key", transport.lastReq.Header.Get("Authorization"))
			require.Equal(test, "ep-exact-target", gjson.GetBytes(transport.lastBody, "model").String())
			require.True(test, gjson.GetBytes(transport.lastBody, "tools").IsArray())
		})
	}
}

// The native Ark Responses API cell forwards on the shared trunk to the path the
// first-party documentation fixes: POST {root}/responses with the account's Ark
// API key as a Bearer credential (docs/82379/1298459).
func TestVolcengineResponsesForwardUsesTheDocumentedArkPath(test *testing.T) {
	body := []byte(`{"model":"ep-fixture-target","input":"hello"}`)
	transport := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_1","status":"completed","output":[{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":7,"output_tokens":3}}`)),
	}}
	gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	SetOpenAIClientTransport(requestContext, OpenAIClientTransportHTTP)

	result, err := gateway.Forward(context.Background(), requestContext, volcengineTestAccount(), body)
	require.NoError(test, err)
	require.NotNil(test, result)
	require.Equal(test, volcengineRuntimeBaseURL+"/responses", transport.lastReq.URL.String())
	require.Equal(test, "Bearer inference-test-key", transport.lastReq.Header.Get("Authorization"))
	require.Equal(test, "ep-fixture-target", gjson.GetBytes(transport.lastBody, "model").String())
}
