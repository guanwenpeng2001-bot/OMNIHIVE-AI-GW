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
	"github.com/tidwall/gjson"
)

func TestForwardGrokResponsesPreservesReturnedModelObservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name, mode, modelJSON, earlyModel, terminal, wantModel string
		wantConflict                                           bool
	}{
		{name: "json_declared", mode: "json", modelJSON: `"grok-4.6-build"`, wantModel: "grok-4.6-build"},
		{name: "json_missing", mode: "json"},
		{name: "json_invalid_model_type", mode: "json", modelJSON: `123`},
		{name: "stream_completed", mode: "stream", modelJSON: `"grok-4.6-build"`, wantModel: "grok-4.6-build"},
		{name: "stream_terminal_wins", mode: "stream", earlyModel: "grok-4.6", modelJSON: `"grok-4.6-build"`, wantModel: "grok-4.6-build", wantConflict: true},
		{name: "stream_retains_actual_early_declaration", mode: "stream", earlyModel: "grok-4.6-build", wantModel: "grok-4.6-build"},
		{name: "stream_missing", mode: "stream"},
		{name: "stream_done_event_header", mode: "stream", terminal: "response.done", modelJSON: `"grok-4.6-build"`, wantModel: "grok-4.6-build"},
		{name: "stream_incomplete", mode: "stream", terminal: "response.incomplete", modelJSON: `"grok-4.6-build", "status":"incomplete"`, wantModel: "grok-4.6-build"},
		{name: "sse_to_json_terminal_wins", mode: "sse_to_json", earlyModel: "grok-4.6", modelJSON: `"grok-4.6-build"`, wantModel: "grok-4.6-build", wantConflict: true},
		{name: "sse_to_json_missing", mode: "sse_to_json"},
		{name: "sse_to_json_done_event_header", mode: "sse_to_json", terminal: "response.done", modelJSON: `"grok-4.6-build"`, wantModel: "grok-4.6-build"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			stream := scenario.mode == "stream"
			requestBody := []byte(fmt.Sprintf(`{"model":"public-grok","input":"hi","stream":%t}`, stream))
			writer := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(writer)
			ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(requestBody))
			// A new forwarding attempt must not inherit a prior attempt's model.
			beginUpstreamResponseModelObservation(ginContext).Observe("previous-attempt-model", true)
			modelField := ""
			if scenario.modelJSON != "" {
				modelField = `,"model":` + scenario.modelJSON
			}
			response := `{"id":"resp_grok_observation","object":"response","output":[],"usage":{"input_tokens":5,"output_tokens":2}` + modelField + `}`
			contentType := "application/json"
			if scenario.mode != "json" {
				contentType = "text/event-stream"
				terminal := scenario.terminal
				if terminal == "" {
					terminal = "response.completed"
				}
				prefix := ""
				if scenario.earlyModel != "" {
					prefix = fmt.Sprintf("data: {\"type\":\"response.created\",\"response\":{\"model\":%q}}\n\n", scenario.earlyModel)
				}
				// Exercise both JSON type and an SSE event header with no JSON type.
				if terminal == "response.done" {
					response = prefix + "event: response.done\ndata: {\"response\":" + response + "}\n\n"
				} else {
					response = prefix + fmt.Sprintf("data: {\"type\":%q,\"response\":%s}\n\n", terminal, response)
				}
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(response))}}
			account := &Account{ID: 5301, Platform: PlatformGrok, Type: AccountTypeAPIKey, Concurrency: 1,
				Credentials: map[string]any{"api_key": "synthetic-key", "base_url": "https://fixture.invalid/v1", "model_mapping": map[string]any{"public-grok": "grok-4.6"}}}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			result, err := gateway.Forward(context.Background(), ginContext, account, requestBody)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, http.StatusOK, writer.Code)
			require.Equal(t, "grok-4.6", gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, "public-grok", result.Model, "requested identity remains separate")
			require.Equal(t, "grok-4.6", result.UpstreamModel, "sent identity remains separate")
			require.Equal(t, scenario.wantModel, result.UpstreamResponseModel, "usage receives only an actual upstream declaration")
			require.Equal(t, scenario.wantConflict, result.UpstreamResponseModelConflict)
			require.Equal(t, "resp_grok_observation", result.ResponseID)
			require.Equal(t, 5, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
		})
	}
}
