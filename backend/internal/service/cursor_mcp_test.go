//go:build unit

package service

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

func cursorMCPBytes(n protowire.Number, b []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, n, protowire.BytesType), b)
}
func cursorMCPNumber(n protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, n, protowire.VarintType), v)
}
func cursorMCPFrame(b []byte) []byte {
	return append(binary.BigEndian.AppendUint32([]byte{0}, uint32(len(b))), b...)
}
func cursorMCPRead(r io.Reader) ([]byte, error) {
	var h [5]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return nil, e
	}
	b := make([]byte, binary.BigEndian.Uint32(h[1:]))
	_, e := io.ReadFull(r, b)
	return b, e
}

func cursorMCPRequest(protocol string, stream, second bool) []byte {
	var request map[string]any
	_ = json.Unmarshal(cursorProtocolRequest(protocol, stream), &request)
	tool := map[string]any{"name": "weather", "description": "Weather", "parameters": map[string]any{"type": "object"}}
	switch protocol {
	case APIProtocolAnthropic:
		request["tools"] = []any{map[string]any{"name": "weather", "input_schema": map[string]any{"type": "object"}}}
		if second {
			request["messages"] = []any{map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "call-1", "name": "weather", "input": map[string]any{"city": "Paris"}}}}, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "call-1", "content": "sunny"}}}}
		}
	case APIProtocolResponses:
		tool["type"] = "function"
		request["tools"] = []any{tool}
		if second {
			request["input"] = []any{map[string]any{"type": "function_call", "call_id": "call-1", "name": "weather", "arguments": `{"city":"Paris"}`}, map[string]any{"type": "function_call_output", "call_id": "call-1", "output": "sunny"}}
		}
	default:
		request["tools"] = []any{map[string]any{"type": "function", "function": tool}}
		if second {
			request["messages"] = []any{map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": "weather", "arguments": `{"city":"Paris"}`}}}}, map[string]any{"role": "tool", "tool_call_id": "call-1", "content": "sunny"}}
		}
	}
	b, _ := json.Marshal(request)
	return b
}

func TestCursorMCPGatewayThreeProtocolsHTTP2(t *testing.T) {
	testCursorMCPGatewayThreeProtocolsHTTP2(t, false)
}

func TestCursorMCPNormalizedIDsThreeProtocolsHTTP2(t *testing.T) {
	testCursorMCPGatewayThreeProtocolsHTTP2(t, true)
}

func TestCursorMCPRedisRestartThreeProtocolsHTTP2(t *testing.T) {
	testCursorMCPGatewayThreeProtocolsHTTP2(t, true, "restart")
}

func TestCursorMCPRedisUnavailableThreeProtocolsHTTP2(t *testing.T) {
	testCursorMCPGatewayThreeProtocolsHTTP2(t, true, "unavailable")
}

func testCursorMCPGatewayThreeProtocolsHTTP2(t *testing.T, opaqueIDs bool, persistence ...string) {
	callID := func(i int) string {
		id := fmt.Sprintf("call-%d", i)
		if opaqueIDs {
			id += "-12345678-1234-1234-1234-123456789012-0\nfc_" + strings.Repeat("a", 40)
		}
		return id
	}
	publicID := func(i int) string {
		id := callID(i)
		if opaqueIDs {
			return cursor.NormalizeToolCallID(id + "\x00exec-" + id)
		}
		return id
	}
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", protocol, stream), func(t *testing.T) {
				var calls atomic.Int32
				failures := make(chan error, 2)
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					first, err := cursorMCPRead(r.Body)
					if err != nil {
						failures <- err
						return
					}
					if r.ProtoMajor != 2 || !strings.Contains(string(first), "api-weather") {
						failures <- fmt.Errorf("missing MCP definition or h2")
						return
					}
					write := func(b []byte) { _, _ = w.Write(cursorMCPFrame(b)); w.(http.Flusher).Flush() }
					if calls.Add(1) == 1 {
						for i := 1; i <= 2; i++ {
							id := callID(i)
							args := cursorMCPBytes(1, []byte("weather"))
							entry := append(cursorMCPBytes(1, []byte("city")), cursorMCPBytes(2, cursorMCPBytes(3, []byte("Paris")))...)
							args = append(args, cursorMCPBytes(2, entry)...)
							for _, field := range []struct {
								n protowire.Number
								s string
							}{{3, id}, {4, "api"}, {5, "weather"}, {9, "api"}} {
								args = append(args, cursorMCPBytes(field.n, []byte(field.s))...)
							}
							exec := append(cursorMCPNumber(1, uint64(i)), cursorMCPBytes(15, []byte("exec-"+id))...)
							exec = append(exec, cursorMCPBytes(11, args)...)
							write(cursorMCPBytes(2, exec))
							reply, err := cursorMCPRead(r.Body)
							if err != nil {
								failures <- err
								return
							}
							if !strings.Contains(string(reply), "deferred_to_client") || !strings.Contains(string(reply), "exec-"+id) {
								failures <- fmt.Errorf("missing deferred identity")
								return
							}
						}
						write(cursorMCPBytes(1, cursorMCPBytes(1, cursorMCPBytes(1, []byte("HIDDEN_AFTER_TOOL")))))
					} else {
						rawID, _ := json.Marshal(callID(1))
						rawExecID, _ := json.Marshal("exec-" + callID(1))
						if opaqueIDs && (!strings.Contains(string(first), `"cursor_exec_id":`+string(rawExecID)) || !strings.Contains(string(first), `"id":`+string(rawID))) {
							failures <- fmt.Errorf("missing original execution identity: %q", first)
							return
						}
						if !strings.Contains(string(first), `"tool_call_id":`+string(rawID)) || !strings.Contains(string(first), "sunny") {
							failures <- fmt.Errorf("missing tool history: %q", first)
							return
						}
						write(cursorMCPBytes(1, cursorMCPBytes(1, cursorMCPBytes(1, []byte("Forecast ready")))))
					}
					write(cursorMCPBytes(1, cursorMCPBytes(8, cursorMCPNumber(1, 100))))
					usage := append(cursorMCPNumber(1, 12), cursorMCPNumber(2, 5)...)
					write(cursorMCPBytes(1, cursorMCPBytes(14, usage)))
					failures <- nil
				}))
				server.EnableHTTP2 = true
				server.StartTLS()
				defer server.Close()
				gateway, account, _ := cursorTestGateway()
				var store cursor.ToolIdentityStore
				if len(persistence) > 0 {
					redisServer := miniredis.RunT(t)
					store = cursor.NewRedisToolIdentityStore(cursorIdentityRedisClient(t, redisServer))
					if persistence[0] == "unavailable" {
						redisServer.SetError("ERR synthetic unavailable")
					}
					gateway.toolStore = store
				}
				gateway.native = &cursor.Client{HTTPClient: server.Client(), BaseURL: server.URL}
				gateway.catalog = map[string]cursorCatalogEntry{cursorSessionKey(account): {models: cursor.ProtocolVariantModels(), expires: time.Now().Add(time.Hour)}}
				for _, second := range []bool{false, true} {
					if second && len(persistence) > 0 && persistence[0] == "restart" {
						gateway, account, _ = cursorTestGateway()
						gateway.toolStore = store
						gateway.native = &cursor.Client{HTTPClient: server.Client(), BaseURL: server.URL}
						gateway.catalog = map[string]cursorCatalogEntry{cursorSessionKey(account): {models: cursor.ProtocolVariantModels(), expires: time.Now().Add(time.Hour)}}
					}
					writer := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(writer)
					request := cursorMCPRequest(protocol, stream, second)
					if second {
						request = []byte(strings.ReplaceAll(string(request), "call-1", publicID(1)))
					}
					result, err := callCursorProtocol(gateway, c, account, request, protocol)
					require.NoError(t, err)
					require.NoError(t, <-failures)
					require.Equal(t, 12, result.Usage.InputTokens)
					require.Equal(t, 5, result.Usage.OutputTokens)
					body := writer.Body.String()
					require.NotContains(t, body, "HIDDEN_AFTER_TOOL")
					if second {
						require.Contains(t, body, "Forecast ready")
						continue
					}
					require.Contains(t, body, publicID(1))
					require.Contains(t, body, publicID(2))
					if opaqueIDs {
						require.NotContains(t, body, `\nfc_`)
						require.NotContains(t, body, "cursor_exec_id")
						require.Len(t, publicID(1), 55)
					}
					require.Contains(t, body, "Paris")
					switch protocol {
					case APIProtocolAnthropic:
						require.Contains(t, body, "tool_use")
					case APIProtocolResponses:
						require.Contains(t, body, "function_call")
					default:
						require.Contains(t, body, `"finish_reason":"tool_calls"`)
					}
					if stream {
						terminal := map[string]string{APIProtocolChatCompletions: "data: [DONE]", APIProtocolAnthropic: "event: message_stop", APIProtocolResponses: "event: response.completed"}[protocol]
						require.Equal(t, 1, strings.Count(body, terminal))
					}
				}
				require.Equal(t, int32(2), calls.Load())
			})
		}
	}
}

func TestCursorMCPRelayPreservesTools(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.NotEmpty(t, body["tools"])
				_, _ = fmt.Fprint(w, `{"id":"chatcmpl-test","model":"default","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"weather","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":5}}`)
			}))
			defer server.Close()
			gateway := NewCursorGatewayService(nil, nil)
			account := &Account{Type: AccountTypeUpstream, Platform: PlatformCursor, Credentials: map[string]any{"base_url": server.URL, "api_key": "synthetic", "api_protocol": APIProtocolChatCompletions}}
			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			result, err := callCursorProtocol(gateway, c, account, cursorMCPRequest(protocol, false, true), protocol)
			require.NoError(t, err)
			require.Equal(t, 12, result.Usage.InputTokens)
			require.Contains(t, writer.Body.String(), "call-1")
		})
	}
}
