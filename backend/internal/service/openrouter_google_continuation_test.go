//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const googleReplayDetailsFixture = `[{"type":"reasoning.text","id":"first","index":0,"text":"fixture","signature":"FAKE_SIGNATURE"},{"type":"reasoning.encrypted","id":"second","index":1,"data":"FAKE_DATA"}]`

func googleReplayTokenForTest(t *testing.T, source openRouterContinuationSource, details string) string {
	t.Helper()
	encoded, err := json.Marshal(openRouterContinuation{Namespace: openRouterContinuationNamespace, Source: source, Details: json.RawMessage(details)})
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(encoded)
}
func googleReplayRequestForTest(t *testing.T, tokens ...string) []byte {
	t.Helper()
	parts := []any{}
	for index, token := range tokens {
		parts = append(parts, map[string]any{"functionCall": map[string]any{"id": string(rune('a' + index)), "name": "echo", "args": map[string]any{}}, "thoughtSignature": token})
	}
	body, err := json.Marshal(map[string]any{"contents": []any{map[string]any{"role": "model", "parts": parts}}})
	require.NoError(t, err)
	return body
}
func googleReplayContextForTest(t *testing.T, ctx context.Context) (context.Context, *gin.Context, *GeminiProtocolWriter) {
	t.Helper()
	ctx = WithGatewayRequestPlan(ctx, NewGatewayRequestPlan(GatewayRequestSpec{Protocol: "gemini", Model: "fixture"}))
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("POST", "/v1beta/models/fixture:generateContent", nil).WithContext(ctx)
	writer := NewGeminiProtocolWriter(request.Writer, "fixture", "generateContent", false, 0)
	request.Writer = writer
	ctx = WithGeminiProtocolWriter(ctx, writer)
	request.Request = request.Request.WithContext(ctx)
	return ctx, request, writer
}

func TestOpenRouterGoogleCarrierSourceValidationAndParallelReplay(t *testing.T) {
	source, err := openRouterContinuationSourceForTarget("https://openrouter.ai/api/v1/chat/completions")
	require.NoError(t, err)
	token := googleReplayTokenForTest(t, source, googleReplayDetailsFixture)
	// A stock client may echo a shared signature on more than one parallel call.
	decodedToken, decodeErr := base64.StdEncoding.DecodeString(token)
	require.NoError(t, decodeErr)
	alternate := base64.RawURLEncoding.EncodeToString(decodedToken)
	ctx, cleaned, err := PrepareGoogleContinuation(context.Background(), googleReplayRequestForTest(t, token, alternate), PlatformOpenRouter)
	require.NoError(t, err)
	require.NotContains(t, string(cleaned), token)
	history := ctx.Value(openRouterGoogleHistoryKey{}).([]openRouterGoogleHistory)
	require.Len(t, history, 1)
	ctx, request, writer := googleReplayContextForTest(t, ctx)
	gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	chat := []byte(`{"model":"fixture","messages":[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"echo","arguments":"{}"}},{"id":"b","type":"function","function":{"name":"echo","arguments":"{}"}}]}]}`)
	account := openRouterFixtureAccount()
	result, err := gateway.prepareOpenRouterGoogleReplay(ctx, request, account, chat)
	require.NoError(t, err)
	require.JSONEq(t, googleReplayDetailsFixture, gjson.GetBytes(result, "messages.0.reasoning_details").Raw)
	require.Len(t, gjson.GetBytes(result, "messages.0.reasoning_details").Array(), 2)
	account.ID = 999
	account.Credentials["api_key"] = "rotated-fixture-key"
	_, err = gateway.prepareOpenRouterGoogleReplay(ctx, request, account, chat)
	require.NoError(t, err, "same endpoint account/key rotation is not an ownership violation")
	account.Credentials["base_url"] = "https://different.example/v1"
	_, err = gateway.prepareOpenRouterGoogleReplay(ctx, request, account, chat)
	require.ErrorContains(t, err, "endpoint source mismatch")
	require.NotNil(t, writer.openRouterSource)
	wrong := source
	wrong.Platform = PlatformGrok
	_, _, err = PrepareGoogleContinuation(context.Background(), googleReplayRequestForTest(t, googleReplayTokenForTest(t, wrong, googleReplayDetailsFixture)), PlatformOpenRouter)
	require.Error(t, err)
	_, _, err = PrepareGoogleContinuation(context.Background(), googleReplayRequestForTest(t, token), PlatformGemini)
	require.Error(t, err)
	_, _, err = PrepareGoogleContinuation(context.Background(), googleReplayRequestForTest(t, token, googleReplayTokenForTest(t, source, `[{"type":"reasoning.encrypted","data":"DIFFERENT"}]`)), PlatformOpenRouter)
	require.ErrorContains(t, err, "conflicting")
	for _, broken := range []string{token + "!", googleReplayTokenForTest(t, source, `{"not":"an array"}`)} {
		_, _, err = PrepareGoogleContinuation(context.Background(), googleReplayRequestForTest(t, broken), PlatformOpenRouter)
		require.Error(t, err)
	}
}

func TestOpenRouterGoogleCarrierLegacyAndSourcePrivacy(t *testing.T) {
	for _, signature := range []string{"skip_thought_signature_validator", "gAAAA_legacy_vendor_bytes", base64.StdEncoding.EncodeToString([]byte("legacy-vendor-signature")), base64.StdEncoding.EncodeToString([]byte(`{"data":"sub2api.reasoning-continuation"}`))} {
		original := googleReplayRequestForTest(t, signature)
		_, cleaned, err := PrepareGoogleContinuation(context.Background(), original, PlatformGemini)
		require.NoError(t, err)
		require.Equal(t, string(original), string(cleaned))
	}
	clean, err := openRouterContinuationSourceForTarget("https://relay.internal.example/api/v1/chat/completions")
	require.NoError(t, err)
	credentials, err := openRouterContinuationSourceForTarget("HTTPS://user:SECRET@relay.internal.example:443/api/v1/chat/completions?token=SECRET#private")
	require.NoError(t, err)
	require.Equal(t, clean, credentials)
	token := googleReplayTokenForTest(t, credentials, googleReplayDetailsFixture)
	decoded, err := base64.StdEncoding.DecodeString(token)
	require.NoError(t, err)
	require.NotContains(t, string(decoded), "SECRET")
	require.NotContains(t, string(decoded), "relay.internal.example")
}

func TestOpenRouterGoogleCarrierResetsBeforeEligibleAttempt(t *testing.T) {
	ctx, request, writer := googleReplayContextForTest(t, context.Background())
	gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	account := openRouterFixtureAccount()
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	_, err := gateway.prepareOpenRouterGoogleReplay(ctx, request, account, body)
	require.NoError(t, err)
	require.NoError(t, captureOpenRouterGoogleDetails(request, []byte(`{"choices":[{"delta":{"reasoning_details":[{"id":"failed-attempt","type":"reasoning.encrypted","data":"OLD"}]}}]}`), true))
	writer.openRouterToolParts = []any{map[string]any{"functionCall": map[string]any{"name": "old"}}}
	account.ID++
	_, err = gateway.prepareOpenRouterGoogleReplay(ctx, request, account, body)
	require.NoError(t, err)
	require.Empty(t, writer.openRouterDetails)
	require.Empty(t, writer.openRouterToolParts)
	require.NoError(t, captureOpenRouterGoogleDetails(request, []byte(`{"choices":[{"delta":{"reasoning_details":[{"id":"successful-attempt","type":"reasoning.encrypted","data":"NEW"}]}}]}`), true))
	parts, err := writer.attachOpenRouterContinuation([]any{map[string]any{"functionCall": map[string]any{"name": "echo"}}})
	require.NoError(t, err)
	encoded := parts[0].(map[string]any)["thoughtSignature"].(string)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	require.Contains(t, string(raw), "successful-attempt")
	require.False(t, strings.Contains(string(raw), "failed-attempt"))
}

func TestOpenRouterGoogleCarrierProtoJSONBase64Encodings(t *testing.T) {
	source, err := openRouterContinuationSourceForTarget("https://openrouter.ai/api/v1/chat/completions")
	require.NoError(t, err)
	token := googleReplayTokenForTest(t, source, `[{"type":"reasoning.text","text":"synthetic_测试_~?/","signature":"FAKE"}]`)
	raw, err := base64.StdEncoding.DecodeString(token)
	require.NoError(t, err)
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, recognized, err := decodeOpenRouterContinuation(encoding.EncodeToString(raw))
		require.NoError(t, err)
		require.True(t, recognized)
		require.Contains(t, string(decoded.Details), "测试")
	}
}
