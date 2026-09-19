//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBedrockPlatformRuntime(test *testing.T) {
	for _, stream := range []bool{false, true} {
		test.Run(fmt.Sprintf("Converse stream=%t", stream), func(test *testing.T) {
			target := "arn:aws:bedrock:eu-west-1:123456789012:inference-profile/private-profile"
			account := catalogEvidenceAccount(101, PlatformBedrock, target)
			account.Type = AccountTypeBedrock
			account.Credentials = map[string]any{"api_key": "fixture-key", "auth_mode": "apikey", "aws_region": "eu-west-1", "model_mapping": map[string]any{"alias": target}}
			require.True(test, account.IsBedrock())
			resolved, ok := ResolveBedrockModelID(&account, "alias")
			require.True(test, ok)
			require.Equal(test, target, resolved)
			body := []byte(fmt.Sprintf(`{"model":"alias","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream))
			upstream := []byte(`{"output":{"message":{"role":"assistant","content":[{"text":"hello"}]}},"stopReason":"end_turn","usage":{"inputTokens":11,"outputTokens":4,"totalTokens":15}}`)
			if stream {
				upstream = bedrockContractConversation()
			}
			transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(upstream))}}
			gateway := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), "anthropic")
			require.NoError(test, err)
			result, err := gateway.Forward(context.Background(), requestContext, &account, parsed)
			require.NoError(test, err)
			require.NotNil(test, result)
			require.Equal(test, 11, result.Usage.InputTokens)
			require.Equal(test, 4, result.Usage.OutputTokens)
			require.Equal(test, target, result.UpstreamModel)
			require.Contains(test, transport.lastReq.URL.String(), "bedrock-runtime.eu-west-1.amazonaws.com/model/arn%3Aaws")
			require.Equal(test, "Bearer fixture-key", transport.lastReq.Header.Get("Authorization"))
		})
	}
	test.Run("regional catalog availability and profile pagination", func(test *testing.T) {
		account := &Account{ID: 101, Platform: PlatformBedrock, Type: AccountTypeBedrock, Credentials: map[string]any{"aws_access_key_id": "fixture-access", "aws_secret_access_key": "fixture-secret", "aws_region": "eu-west-1"}}
		transport := &httpUpstreamRecorder{responses: []*http.Response{
			catalogFixtureResponse(`{"modelSummaries":[{"modelId":"amazon.nova-fixture","inputModalities":["TEXT"],"outputModalities":["TEXT"],"inferenceTypesSupported":["ON_DEMAND"]}]}`),
			catalogFixtureResponse(`{"modelId":"amazon.nova-fixture","agreementAvailability":{"status":"AVAILABLE"},"authorizationStatus":"AUTHORIZED","entitlementAvailability":"AVAILABLE","regionAvailability":"AVAILABLE"}`),
			catalogFixtureResponse(`{"inferenceProfileSummaries":[{"inferenceProfileId":"eu.profile","inferenceProfileArn":"arn:profile","status":"ACTIVE"}],"nextToken":"next"}`),
			catalogFixtureResponse(`{"inferenceProfileSummaries":[]}`),
		}}
		service := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		models, body, err := service.fetchUpstreamModelList(context.Background(), account)
		require.NoError(test, err)
		require.ElementsMatch(test, []string{"amazon.nova-fixture", "eu.profile", "arn:profile"}, models)
		_, metadata, err := extractUpstreamModelCatalog(body, false, PlatformBedrock)
		require.NoError(test, err)
		require.Equal(test, []string{"messages"}, metadata["amazon.nova-fixture"].SupportedMethods)
		require.Equal(test, []string{}, metadata["eu.profile"].SupportedMethods, "listing alone must not authorize profiles")
		require.Contains(test, transport.lastReq.Header.Get("Authorization"), "eu-west-1/bedrock/aws4_request")
		account.Credentials = map[string]any{"api_key": "runtime-only", "auth_mode": "apikey"}
		_, _, err = service.fetchUpstreamModelList(context.Background(), account)
		require.ErrorContains(test, err, "do not establish catalog permission")
	})
}

func TestBedrockLegacyCompatibility(test *testing.T) {
	model := "anthropic.claude-sonnet-4-20250514-v1:0"
	account := catalogEvidenceAccount(101, PlatformAnthropic, model)
	account.Type = AccountTypeBedrock
	account.Credentials = map[string]any{"api_key": "fixture-key", "auth_mode": "apikey", "aws_region": "us-east-1"}
	require.True(test, account.IsBedrock())
	require.Equal(test, PlatformAnthropic, account.Platform)
	body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":100,"messages":[{"role":"user","content":"hello"}]}`, model))
	response := `{"id":"fixture","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}`
	transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}}
	gateway := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
	requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), "anthropic")
	require.NoError(test, err)
	result, err := gateway.Forward(context.Background(), requestContext, &account, parsed)
	require.NoError(test, err)
	require.Equal(test, 5, result.Usage.InputTokens)
	require.True(test, strings.HasSuffix(transport.lastReq.URL.Path, "/invoke"))
	require.Equal(test, PlatformAnthropic, account.Platform)
}
