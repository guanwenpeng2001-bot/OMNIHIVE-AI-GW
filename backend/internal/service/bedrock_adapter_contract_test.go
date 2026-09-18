package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// These are synthetic fixtures built from the AWS API reference, not recordings
// from an authorized account. They intentionally use no network or database.
func TestBedrockAdapterContract(t *testing.T) {
	contextRoot := context.Background()
	iamAccount := &Account{Credentials: map[string]any{"aws_access_key_id": "fixture-access", "aws_secret_access_key": "fixture-secret", "aws_session_token": "fixture-session", "aws_region": "eu-west-1"}}
	keyAccount := &Account{Credentials: map[string]any{"api_key": "fixture-runtime-key", "aws_region": "us-east-1"}}
	textRequest := []byte(`{"model":"client-alias","max_tokens":42,"messages":[{"role":"user","content":"hello"}]}`)
	t.Run("regional catalogue is not authorization", func(t *testing.T) {
		catalog, err := ParseBedrockFoundationCatalog("eu-west-1", []byte(`{"modelSummaries":[{"modelId":"amazon.nova-pro-v1:0","modelArn":"arn:aws:bedrock:eu-west-1::foundation-model/amazon.nova-pro-v1:0","inputModalities":[],"responseStreamingSupported":false,"modelLifecycle":{"status":"LEGACY"}}]}`))
		require.NoError(t, err)
		require.Equal(t, "eu-west-1", catalog.Region)
		require.Len(t, catalog.Models, 1)
		require.Equal(t, "LEGACY", catalog.Models[0].ModelLifecycle.Status)
		require.NotNil(t, catalog.Models[0].InputModalities)
		require.Nil(t, catalog.Models[0].OutputModalities)
		require.NotNil(t, catalog.Models[0].ResponseStreamingSupported)
		require.False(t, *catalog.Models[0].ResponseStreamingSupported)
		availability, err := ParseBedrockModelAvailability([]byte(`{"modelId":"amazon.nova-pro-v1:0","agreementAvailability":{"status":"AVAILABLE"},"authorizationStatus":"NOT_AUTHORIZED","entitlementAvailability":"AVAILABLE","regionAvailability":"NOT_AVAILABLE"}`))
		require.NoError(t, err)
		require.Equal(t, "NOT_AUTHORIZED", availability.AuthorizationStatus)
		require.Equal(t, "NOT_AVAILABLE", availability.RegionAvailability)
		empty, err := ParseBedrockFoundationCatalog("us-east-1", []byte(`{"modelSummaries":[]}`))
		require.NoError(t, err)
		require.Empty(t, empty.Models)
		for _, body := range []string{`{}`, `{"message":"denied"}`, `{"modelSummaries":null}`, `{"modelSummaries":[{}]}`} {
			_, err := ParseBedrockFoundationCatalog("us-east-1", []byte(body))
			require.Error(t, err)
		}
	})
	t.Run("control plane IAM only and encoded cursor", func(t *testing.T) {
		for _, action := range []string{"foundation-models", "inference-profiles", "availability"} {
			target := ""
			if action == "availability" {
				target = "amazon.nova-pro-v1:0"
			}
			request, err := BuildBedrockCatalogRequest(contextRoot, iamAccount, action, target, "")
			require.NoError(t, err)
			require.Equal(t, "bedrock.eu-west-1.amazonaws.com", request.URL.Host)
			require.Equal(t, http.MethodGet, request.Method)
			require.Contains(t, request.Header.Get("Authorization"), "/eu-west-1/bedrock/aws4_request")
			require.Equal(t, "fixture-session", request.Header.Get("X-Amz-Security-Token"))
			_, err = BuildBedrockCatalogRequest(contextRoot, keyAccount, action, target, "")
			require.Error(t, err)
		}
		request, err := BuildBedrockCatalogRequest(contextRoot, iamAccount, "inference-profiles", "", "a+/= b")
		require.NoError(t, err)
		require.Equal(t, "a+/= b", request.URL.Query().Get("nextToken"))
		_, err = BuildBedrockCatalogRequest(contextRoot, iamAccount, "foundation-models", "", "cursor")
		require.Error(t, err)
		_, err = bedrockEndpoint("us-east-1.example.org/path", true)
		require.Error(t, err)
	})
	t.Run("profiles preserve ARN and fail whole pagination", func(t *testing.T) {
		page := `{"inferenceProfileSummaries":[{"inferenceProfileId":"eu.profile","inferenceProfileArn":"arn:aws:bedrock:eu-west-1:123456789012:inference-profile/eu.profile","status":"ACTIVE","models":[{"modelArn":"arn:aws:bedrock:eu-west-1::foundation-model/amazon.nova-pro-v1:0"}]}],"nextToken":"next"}`
		calls := 0
		profiles, err := CollectBedrockProfiles(contextRoot, func(_ context.Context, cursor string) ([]byte, error) {
			calls++
			if calls == 1 {
				require.Empty(t, cursor)
				return []byte(page), nil
			}
			require.Equal(t, "next", cursor)
			return []byte(`{"inferenceProfileSummaries":[]}`), nil
		})
		require.NoError(t, err)
		require.Len(t, profiles, 1)
		require.Contains(t, profiles[0].ARN, "inference-profile/eu.profile")
		require.Len(t, profiles[0].Models, 1)
		profiles, err = CollectBedrockProfiles(contextRoot, func(context.Context, string) ([]byte, error) { return []byte(page), nil })
		require.ErrorContains(t, err, "repeated")
		require.Nil(t, profiles)
		profiles, err = CollectBedrockProfiles(contextRoot, func(_ context.Context, cursor string) ([]byte, error) {
			if cursor == "" {
				return []byte(page), nil
			}
			return nil, errors.New("permission denied")
		})
		require.Error(t, err)
		require.Nil(t, profiles)
		cancelled, cancel := context.WithCancel(contextRoot)
		cancel()
		_, err = CollectBedrockProfiles(cancelled, func(context.Context, string) ([]byte, error) {
			t.Fatal("must not fetch cancelled request")
			return nil, nil
		})
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("runtime auth targets and legacy Claude preparation", func(t *testing.T) {
		for _, account := range []*Account{iamAccount, keyAccount} {
			for _, stream := range []bool{false, true} {
				for _, invoke := range []bool{false, true} {
					target := "arn:aws:bedrock:eu-west-1:123456789012:inference-profile/eu.profile"
					request, err := BuildBedrockAdapterRequest(contextRoot, account, target, textRequest, stream, invoke, "")
					require.NoError(t, err)
					require.Contains(t, request.URL.EscapedPath(), "arn%3Aaws%3Abedrock%3Aeu-west-1%3A123456789012%3Ainference-profile%2Feu.profile")
					payload, err := io.ReadAll(request.Body)
					require.NoError(t, err)
					require.NoError(t, request.Body.Close())
					if invoke {
						require.Equal(t, "bedrock-2023-05-31", gjson.GetBytes(payload, "anthropic_version").String())
						require.False(t, gjson.GetBytes(payload, "model").Exists())
						require.Contains(t, request.URL.Path, "/invoke")
					} else {
						require.Contains(t, request.URL.Path, "/converse")
						require.Equal(t, int64(42), gjson.GetBytes(payload, "inferenceConfig.maxTokens").Int())
					}
					if account == keyAccount {
						require.Equal(t, "Bearer fixture-runtime-key", request.Header.Get("Authorization"))
					} else {
						require.Contains(t, request.Header.Get("Authorization"), "AWS4-HMAC-SHA256")
					}
				}
			}
		}
	})
	t.Run("text tools choice and usage round trip", func(t *testing.T) {
		body := []byte(`{"system":[{"type":"text","text":"rules"}],"temperature":0,"top_p":0.5,"stop_sequences":["done"],"tools":[{"name":"weather","description":"Forecast","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"weather"},"messages":[{"role":"user","content":"weather?"},{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"weather","input":{"city":"Paris"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":[{"type":"text","text":"sunny"}],"is_error":true}]}]}`)
		converted, err := PrepareBedrockConverseRequest(body)
		require.NoError(t, err)
		require.Equal(t, "rules", gjson.GetBytes(converted, "system.0.text").String())
		require.True(t, gjson.GetBytes(converted, "inferenceConfig.temperature").Exists())
		require.Equal(t, "weather", gjson.GetBytes(converted, "toolConfig.toolChoice.tool.name").String())
		require.Equal(t, "object", gjson.GetBytes(converted, "toolConfig.tools.0.toolSpec.inputSchema.json.type").String())
		require.Equal(t, "Paris", gjson.GetBytes(converted, "messages.1.content.0.toolUse.input.city").String())
		require.Equal(t, "error", gjson.GetBytes(converted, "messages.2.content.0.toolResult.status").String())
		response, err := ConvertBedrockConverseResponse([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"checking"},{"toolUse":{"toolUseId":"call-1","name":"weather","input":{"city":"Paris"}}}]}},"stopReason":"tool_use","usage":{"inputTokens":7,"outputTokens":3,"totalTokens":10,"cacheReadInputTokens":0}}`), "request-id", "alias")
		require.NoError(t, err)
		require.Equal(t, "call-1", gjson.GetBytes(response, "content.1.id").String())
		require.Equal(t, "tool_use", gjson.GetBytes(response, "stop_reason").String())
		require.Equal(t, int64(10), gjson.GetBytes(response, "usage.total_tokens").Int())
		require.True(t, gjson.GetBytes(response, "usage.cache_read_input_tokens").Exists())
		require.False(t, gjson.GetBytes(response, "usage.cache_creation_input_tokens").Exists())
		for _, invalid := range []string{`{"messages":[]}`, `{"messages":[{"role":"user","content":[{"type":"image"}]}]}`, `{"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled"}}`} {
			_, err := PrepareBedrockConverseRequest([]byte(invalid))
			require.Error(t, err)
		}
		_, err = ConvertBedrockConverseResponse([]byte(`{"message":"denied"}`), "id", "model")
		require.Error(t, err)
	})
	t.Run("Converse stream text tools usage", func(t *testing.T) {
		frames := bedrockContractConversation()
		var output bytes.Buffer
		usage, err := ConvertBedrockConverseStream(contextRoot, io.NopCloser(iotest.OneByteReader(bytes.NewReader(frames))), &output, "request-id", "alias")
		require.NoError(t, err)
		require.Equal(t, float64(11), usage["input_tokens"])
		require.Equal(t, float64(4), usage["output_tokens"])
		require.Equal(t, float64(0), usage["cache_read_input_tokens"])
		require.NotContains(t, usage, "cache_creation_input_tokens")
		require.Contains(t, output.String(), `"type":"text_delta"`)
		require.Contains(t, output.String(), `"partial_json":"{\"city\":"`)
		require.Contains(t, output.String(), `"id":"call-1"`)
		require.Contains(t, output.String(), `"stop_reason":"tool_use"`)
		require.True(t, strings.HasSuffix(output.String(), "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	})
	t.Run("truncation errors bad CRC and cancellation", func(t *testing.T) {
		valid := bedrockContractConversation()
		for _, length := range []int{0, 1, 11, len(valid) - 1} {
			_, err := ConvertBedrockConverseStream(contextRoot, io.NopCloser(bytes.NewReader(valid[:length])), io.Discard, "id", "model")
			require.Error(t, err)
		}
		for _, exception := range []string{"accessDeniedException", "validationException", "resourceNotFoundException", "throttlingException", "modelStreamErrorException"} {
			frame := bedrockContractFrame(exception, "exception", `{"message":"private request body"}`)
			_, _, err := newBedrockEventStreamDecoder(bytes.NewReader(frame)).DecodeConverse()
			require.ErrorContains(t, err, exception)
			require.NotContains(t, err.Error(), "private request")
		}
		corrupt := append([]byte(nil), valid...)
		corrupt[8] ^= 1
		_, _, err := newBedrockEventStreamDecoder(bytes.NewReader(corrupt)).DecodeConverse()
		require.ErrorContains(t, err, "CRC")
		corrupt = bedrockContractFrame("messageStart", "event", `{"role":"assistant"}`)
		corrupt[len(corrupt)-1] ^= 1
		_, _, err = newBedrockEventStreamDecoder(bytes.NewReader(corrupt)).DecodeConverse()
		require.ErrorContains(t, err, "CRC")
		reader, writer := io.Pipe()
		defer func() { _ = writer.Close() }()
		cancelled, cancel := context.WithCancel(contextRoot)
		defer cancel()
		finished := make(chan error, 1)
		go func() {
			_, err := ConvertBedrockConverseStream(cancelled, reader, io.Discard, "id", "model")
			finished <- err
		}()
		cancel()
		select {
		case err := <-finished:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("cancel did not unblock stream")
		}
	})
	t.Run("stream ordering incomplete tools and writer failure", func(t *testing.T) {
		start := bedrockContractFrame("messageStart", "event", `{"role":"assistant"}`)
		for _, events := range [][]byte{
			bedrockContractFrame("metadata", "event", `{"usage":{"inputTokens":1}}`),
			bedrockContractFrame("contentBlockStop", "event", `{"contentBlockIndex":0}`),
			bedrockContractFrame("contentBlockDelta", "event", `{"contentBlockIndex":-1,"delta":{"text":"x"}}`),
			bedrockContractFrame("messageStop", "event", `{"stopReason":"end_turn"}`),
			append(bedrockContractFrame("contentBlockStart", "event", `{"contentBlockIndex":0,"start":{"toolUse":{"toolUseId":"call","name":"tool"}}}`), bedrockContractFrame("contentBlockStop", "event", `{"contentBlockIndex":0}`)...),
		} {
			_, err := ConvertBedrockConverseStream(contextRoot, io.NopCloser(bytes.NewReader(append(append([]byte(nil), start...), events...))), io.Discard, "id", "model")
			require.Error(t, err)
		}
		_, err := ConvertBedrockConverseStream(contextRoot, io.NopCloser(bytes.NewReader(bedrockContractConversation())), bedrockContractFailingWriter{}, "id", "model")
		require.ErrorIs(t, err, io.ErrClosedPipe)
		invalidLengths := make([]byte, 12)
		binary.BigEndian.PutUint32(invalidLengths[:4], 16)
		binary.BigEndian.PutUint32(invalidLengths[4:8], 100)
		binary.BigEndian.PutUint32(invalidLengths[8:], crc32.ChecksumIEEE(invalidLengths[:8]))
		_, _, err = newBedrockEventStreamDecoder(bytes.NewReader(invalidLengths)).DecodeConverse()
		require.ErrorContains(t, err, "lengths")
	})
}

type bedrockContractFailingWriter struct{}

func (bedrockContractFailingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func bedrockContractConversation() []byte {
	var frames []byte
	for _, event := range []struct{ kind, body string }{
		{"messageStart", `{"role":"assistant"}`},
		{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"Checking"}}`},
		{"contentBlockStop", `{"contentBlockIndex":0}`},
		{"contentBlockStart", `{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"call-1","name":"weather"}}}`},
		{"contentBlockDelta", `{"contentBlockIndex":1,"delta":{"toolUse":{"input":"{\"city\":"}}}`},
		{"contentBlockDelta", `{"contentBlockIndex":1,"delta":{"toolUse":{"input":"\"Paris\"}"}}}`},
		{"contentBlockStop", `{"contentBlockIndex":1}`},
		{"messageStop", `{"stopReason":"tool_use"}`},
		{"metadata", `{"usage":{"inputTokens":11,"outputTokens":4,"totalTokens":15,"cacheReadInputTokens":0}}`},
	} {
		frames = append(frames, bedrockContractFrame(event.kind, "event", event.body)...)
	}
	return frames
}

func bedrockContractFrame(eventType, messageType, payload string) []byte {
	var headers []byte
	for _, header := range []struct{ name, value string }{{":event-type", eventType}, {":message-type", messageType}} {
		headers = append(headers, byte(len(header.name)))
		headers = append(headers, header.name...)
		headers = append(headers, 7, byte(len(header.value)>>8), byte(len(header.value)))
		headers = append(headers, header.value...)
	}
	frame := make([]byte, 12)
	binary.BigEndian.PutUint32(frame[:4], uint32(16+len(headers)+len(payload)))
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(headers)))
	binary.BigEndian.PutUint32(frame[8:12], crc32.ChecksumIEEE(frame[:8]))
	frame = append(frame, headers...)
	frame = append(frame, payload...)
	checksum := crc32.ChecksumIEEE(frame)
	frame = append(frame, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(frame[len(frame)-4:], checksum)
	return frame
}
