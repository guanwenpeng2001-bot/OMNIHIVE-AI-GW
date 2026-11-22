//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
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

func freeAccount(platform, mode string) *Account {
	return &Account{ID: 73, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": mode}, Status: StatusActive, Schedulable: true, Concurrency: 1}
}
func TestFreeModelsCredentialsAndRouting(t *testing.T) {
	for _, tt := range []struct {
		platform, mode, key, want string
		valid                     bool
	}{
		{PlatformOpenCodeGo, AccountModeZenFree, "", "public", true}, {PlatformOpenCodeGo, AccountModeZenFree, "public", "public", true}, {PlatformOpenCodeGo, AccountModeZenFree, "paid", "", false},
		{PlatformKilo, "", "", "", true}, {PlatformKilo, "free", "public", "", true}, {PlatformKilo, "apikey", "key", "key", true}, {PlatformKilo, "apikey", "public", "", false}, {PlatformKilo, "apikey", "", "", false}, {PlatformKilo, "unknown", "", "", false},
	} {
		a := freeAccount(tt.platform, tt.mode)
		a.Credentials["api_key"] = tt.key
		original := a.Credentials
		err := NormalizeFreeModelCredentials(a)
		if !tt.valid {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, tt.want, a.Credentials["api_key"])
		require.Equal(t, tt.key, original["api_key"], "normalization must not mutate shared maps")
	}
	a := freeAccount(PlatformOpenCodeGo, AccountModeZenFree)
	require.Equal(t, DefaultOpenCodeZenBaseURL, a.GetOpenAIBaseURL())
	require.Equal(t, DefaultOpenCodeZenAnthropicBaseURL, a.GetAnthropicProtocolBaseURL())
	require.Equal(t, APIProtocolResponses, a.ResolveOpenCodeGoUpstreamProtocol("muse-spark-1.3-contributor-free"))
	require.Equal(t, APIProtocolAnthropic, a.ResolveOpenCodeGoUpstreamProtocol("union-alpha"))
	require.False(t, a.IsOpenCodeGoPlan())
	require.False(t, a.IsOpenCodeZen())
	require.Error(t, validateCodingPlanAccount(a))
	for _, mode := range []string{"", AccountModeGo, AccountModeZen} {
		paid := freeAccount(PlatformOpenCodeGo, mode)
		paid.Credentials["api_key"] = "paid-key"
		require.NoError(t, NormalizeFreeModelCredentials(paid))
		require.Equal(t, "paid-key", paid.GetCredential("api_key"))
		require.False(t, paid.IsOpenCodeZenFree())
	}
	k := freeAccount(PlatformKilo, "")
	require.True(t, k.IsKiloFree())
	require.Equal(t, DefaultKiloBaseURL, k.GetOpenAIBaseURL())
	require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletions(k))
	require.True(t, k.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	for _, suffix := range []string{"/", "/models", "/chat/completions/"} {
		k.Credentials["base_url"] = DefaultKiloBaseURL + suffix
		require.Equal(t, DefaultKiloBaseURL, kiloBaseURL(k))
	}
}
func TestZenFreeStrictCatalog(t *testing.T) {
	rows := []string{
		`{"id":"big-pickle"}`, `{"id":"union-alpha"}`, `{"id":"zero-only","cost":{"input":0,"cache":{"write":"0e5"}}}`, `{"id":"paid-free","cost":{"input":0,"new":{"price":"0.000000000000000000000000000001"}}}`,
		`{"id":"not-free","cost":{"input":"1e-999"}}`, `{"id":"unknown-paid"}`, `{"id":"jev-1-free","cost":{"input":0}}`, `{"id":"clef","cost":{"input":0}}`, `{"id":"system-free","recommended_api":"systemone","cost":{"input":0}}`,
		`{"id":"retired-free","status":"retired"}`, `{"id":"deprecated-free","deprecated":true}`, `{"id":"date-free","deprecated_at":"today"}`, `{"id":"retire-date-free","retirement_date":"tomorrow"}`, `{"id":"disabled-free","status":"active","lifecycle":"disabled"}`,
		`{"id":"rich-free","name":"Rich","cost":{"input":0,"output":0},"limit":{"context":8192,"input":7000,"output":1000},"modalities":{"input":["text/plain","image/jpeg","audio/wav","video/mp4","application/pdf"],"output":["text"]},"capabilities":{"reasoning":{"supported":true},"tools":{"supported":false}},"recommended_api":"responses"}`,
		`{"id":"duplicate-free","cost":{"input":0}}`, `{"id":"duplicate-free","cost":{"output":1}}`,
	}
	body := []byte(`{"data":[` + strings.Join(rows, ",") + `]}`)
	ids, metadata, err := extractAccountModelCatalog(body, freeAccount(PlatformOpenCodeGo, AccountModeZenFree))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"big-pickle", "union-alpha", "zero-only", "rich-free"}, ids)
	m := metadata["rich-free"]
	require.Equal(t, "Rich", m.DisplayName)
	require.EqualValues(t, 8192, m.ContextWindow)
	require.EqualValues(t, 7000, m.MaxInputTokens)
	require.EqualValues(t, 1000, m.MaxOutputTokens)
	require.ElementsMatch(t, []string{"text", "image", "audio", "video", "pdf"}, m.InputModalities)
	require.True(t, *m.Reasoning)
	require.False(t, *m.SupportsFunctionCalling)
	require.Equal(t, []string{"responses"}, m.SupportedMethods)
	paid, _, err := extractAccountModelCatalog(body, freeAccount(PlatformOpenCodeGo, AccountModeZen))
	require.NoError(t, err)
	require.Contains(t, paid, "paid-free")
}
func TestKiloCatalogModesAndDispatch(t *testing.T) {
	body := []byte(`{"data":[{"id":"stealth/glyph-cluster","name":"Glyph","isFree":true,"context_length":256000,"top_provider":{"max_completion_tokens":32000},"architecture":{"input_modalities":["text","image"]},"supported_parameters":["reasoning","tools","temperature"],"pricing":{"prompt":"0","completion":"0"}},{"id":"paid/model","isFree":false},{"id":"fake/free","isFree":"true"}]}`)
	a := freeAccount(PlatformKilo, "")
	ids, m, err := extractAccountModelCatalog(body, a)
	require.NoError(t, err)
	require.Equal(t, []string{"stealth/glyph-cluster"}, ids)
	require.True(t, *m[ids[0]].SupportsFunctionCalling)
	require.True(t, *m[ids[0]].Reasoning)
	require.EqualValues(t, 32000, m[ids[0]].MaxOutputTokens)
	require.Equal(t, `"0"`, string(m[ids[0]].Pricing["prompt"]))
	paid := freeAccount(PlatformKilo, "apikey")
	paid.Credentials["api_key"] = "key"
	all, _, err := extractAccountModelCatalog(body, paid)
	require.NoError(t, err)
	require.Len(t, all, 3)
	_, _, err = prepareKiloChat(a, []byte(`{"model":"paid/model"}`))
	require.ErrorIs(t, err, ErrAccountModelUnsupported)
	a.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: m})
	require.True(t, kiloFreeModelAllowed(a, ids[0]), "the reviewed bootstrap entry remains available before discovery")
	a.Extra[UpstreamModelCatalogExtraKey] = &AccountModelCatalogSnapshot{Complete: true, SourceKind: "live", DiscoveredModels: ids}
	require.True(t, kiloFreeModelAllowed(a, ids[0]))
	no := false
	m[ids[0]] = UpstreamModelMetadata{ID: ids[0], Free: &no}
	require.False(t, kiloFreeModelAllowed(a, ids[0]))
	for _, model := range []string{"kilo-auto/free", "openrouter/free", "stepfun/foo:free", "liquid/lfm-3-1.7b:free", "thinkingmachines/foo:free"} {
		// Reasoning normalization is independent of free admission: install explicit
		// current native discovery instead of relying on the model-name suffix.
		a.Extra[UpstreamModelCatalogExtraKey] = &AccountModelCatalogSnapshot{Complete: true, SourceKind: "live", DiscoveredModels: []string{model}}
		yes := true
		a.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{model: {ID: model, Free: &yes}}})
		out, _, err := prepareKiloChat(a, []byte(fmt.Sprintf(`{"model":%q,"stream":true,"stream_options":{"extra":true},"reasoning_effort":"none","reasoning":{"effort":"disabled","enabled":false}}`, model)))
		require.NoError(t, err)
		require.True(t, gjson.GetBytes(out, "stream_options.include_usage").Bool())
		require.True(t, gjson.GetBytes(out, "stream_options.extra").Bool())
		require.False(t, gjson.GetBytes(out, "reasoning_effort").Exists())
		require.False(t, gjson.GetBytes(out, "reasoning").Exists())
	}
}
func TestZenFreeSessionHeaders(t *testing.T) {
	a := freeAccount(PlatformOpenCodeGo, AccountModeZenFree)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Header.Set("X-Session-Affinity", "conversation-fixture")
	for _, protocol := range []string{"chat/completions", "responses", "messages"} {
		h := http.Header{"authorization": []string{"Bearer private"}}
		applyOpenCodeSessionHeader(c, a, "https://custom.example/"+protocol, h, []byte(`{"input":"hi"}`))
		require.Regexp(t, `^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`, h.Get("X-OpenCode-Session"))
		require.Equal(t, zenCanonicalID("ses", "conversation-fixture"), h.Get("X-OpenCode-Session"))
		require.Equal(t, h.Get("X-OpenCode-Session"), h.Get("X-Session-Id"))
		require.Equal(t, h.Get("X-Session-Id"), h.Get("X-Session-Affinity"))
		require.Regexp(t, `^msg_[0-9a-f]{12}[0-9A-Za-z]{14}$`, h.Get("X-OpenCode-Request"))
		require.Equal(t, zenFreeUserAgent, h.Get("User-Agent"))
		if protocol == "messages" {
			require.Equal(t, "public", h.Get("X-API-Key"))
			require.Empty(t, h.Get("Authorization"))
			require.Equal(t, "2023-06-01", h.Get("Anthropic-Version"))
		} else {
			require.Equal(t, "Bearer public", h.Get("Authorization"))
		}
	}
	first := zenSession(nil, http.Header{}, []byte(`{"messages":[{"role":"user","content":"hi"}]}`))
	next := zenSession(nil, http.Header{}, []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"},{"role":"user","content":"next"}]}`))
	require.Equal(t, first, next)
}
func TestZenFreeGuardAllProtocols(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic} {
		t.Run(protocol, func(t *testing.T) {
			tool := `{"name":"pwsh"}`
			if protocol == APIProtocolChatCompletions {
				tool = `{"type":"function","function":{"name":"pwsh"}}`
			}
			body, p, err := prepareZenFreeRequest(nil, []byte(`{"model":"big-pickle","tools":[`+tool+`]}`), protocol)
			require.NoError(t, err)
			require.Len(t, gjson.GetBytes(body, "tools").Array(), 5)
			require.Equal(t, "pwsh", p.renamed["bash"])
			require.True(t, p.injected["edit"])
			reply := `{"choices":[{"index":0,"message":{"tool_calls":[{"function":{"name":"bash","arguments":"{}"}},{"function":{"name":"read","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":9007199254740993,"cost":0.000000000000000000123}}`
			if protocol == APIProtocolResponses {
				reply = `{"output":[{"type":"function_call","name":"bash"},{"type":"function_call","name":"read"}],"usage":{"input_tokens":9007199254740993,"cost":0.000000000000000000123}}`
			}
			if protocol == APIProtocolAnthropic {
				reply = `{"content":[{"type":"tool_use","name":"bash"},{"type":"tool_use","name":"read"}],"stop_reason":"tool_use","usage":{"input_tokens":9007199254740993,"cost":0.000000000000000000123}}`
			}
			out, err := p.guard([]byte(reply))
			require.NoError(t, err)
			require.Contains(t, string(out), `"pwsh"`)
			require.NotContains(t, string(out), `"read"`)
			require.Contains(t, string(out), `9007199254740993`)
			require.Contains(t, string(out), `0.000000000000000000123`)
		})
	}
	for _, tt := range []struct{ protocol, reply, path, want string }{
		{APIProtocolChatCompletions, `{"choices":[{"index":0,"message":{"tool_calls":[{"function":{"name":"bash"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`, "choices.0.finish_reason", "stop"},
		{APIProtocolAnthropic, `{"content":[{"type":"tool_use","name":"read"}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":2}}`, "stop_reason", "end_turn"},
	} {
		_, p, err := prepareZenFreeRequest(nil, []byte(`{}`), tt.protocol)
		require.NoError(t, err)
		out, err := p.guard([]byte(tt.reply))
		require.NoError(t, err)
		require.Equal(t, tt.want, gjson.GetBytes(out, tt.path).String())
		require.True(t, gjson.GetBytes(out, "usage").Exists())
	}
}
func guardedSSE(t *testing.T, p *zenFreeToolPolicy, sse string) string {
	t.Helper()
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse))}
	resp, err := rewriteFreeModelResponse(resp, p.guard)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}
func TestZenFreeFragmentedChatToolsAndSSEUsage(t *testing.T) {
	_, p, err := prepareZenFreeRequest(nil, []byte(`{"tools":[{"function":{"name":"Bash"}}]}`), APIProtocolChatCompletions)
	require.NoError(t, err)
	sse := `: keepalive

data: {"choices":[{"index":0,"delta":{"content":"hello","tool_calls":[{"index":0,"id":"real","function":{"name":"ba","arguments":"{"}},{"index":1,"function":{"name":"re"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"sh","arguments":"}"}},{"index":1,"function":{"name":"ad","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}

data: {"choices":[],"usage":{"prompt_tokens":17,"completion_tokens":5,"cost":0.000000000123}}

data: [DONE]

`
	out := guardedSSE(t, p, sse)
	require.Contains(t, out, `"content":"hello"`)
	require.Contains(t, out, `"name":"Bash"`)
	require.NotContains(t, out, `"name":"read"`)
	require.Contains(t, out, `"finish_reason":"tool_calls"`)
	require.Contains(t, out, `"prompt_tokens":17`)
	require.Contains(t, out, "[DONE]")
	require.Contains(t, out, ": keepalive")
}
func TestZenFreeStreamingDecoyEvents(t *testing.T) {
	_, p, _ := prepareZenFreeRequest(nil, []byte(`{}`), APIProtocolAnthropic)
	out := guardedSSE(t, p, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"name\":\"read\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"partial_json\":\"{}\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":3}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	require.NotContains(t, out, "content_block_")
	require.Contains(t, out, `"stop_reason":"end_turn"`)
	require.Contains(t, out, `"output_tokens":3`)
	_, p, _ = prepareZenFreeRequest(nil, []byte(`{}`), APIProtocolResponses)
	out = guardedSSE(t, p, "data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"decoy\",\"type\":\"function_call\",\"name\":\"bash\"}}\n\ndata: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"decoy\",\"delta\":\"{}\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"function_call\",\"name\":\"bash\"}],\"usage\":{\"input_tokens\":12,\"output_tokens\":2}}}\n\n")
	require.NotContains(t, out, "function_call")
	require.Contains(t, out, "response.completed")
	require.Contains(t, out, `"input_tokens":12`)
}
func TestKiloUsageClampExactCostAndError(t *testing.T) {
	raw := []byte(`{"usage":{"prompt_tokens":9000000,"completion_tokens":99,"total_tokens":9000099,"completion_tokens_details":{"reasoning_tokens":70},"cost":0.00000000000000123}}`)
	out := normalizeKiloPayload(raw, "kilo-auto/free")
	require.EqualValues(t, 256000, gjson.GetBytes(out, "usage.prompt_tokens").Int())
	require.EqualValues(t, 99, gjson.GetBytes(out, "usage.completion_tokens").Int())
	require.EqualValues(t, 70, gjson.GetBytes(out, "usage.completion_tokens_details.reasoning_tokens").Int())
	require.Equal(t, "0.00000000000000123", gjson.GetBytes(out, "usage.cost").Raw)
	require.Equal(t, raw, normalizeKiloPayload(raw, "other/free"))
	errBody, _ := json.Marshal(map[string]any{"error": map[string]any{"message": "provider refused", "metadata": map[string]any{"raw": `{"error":{"message":"` + strings.Repeat("x", 300) + `"}}`}}})
	detail := kiloErrorDetail(errBody)
	require.Len(t, []rune(detail), 200)
	require.Contains(t, detail, "provider refused")
}
func TestFreeModelDiscoveryAndPublicProbe(t *testing.T) {
	for _, tt := range []struct{ platform, mode string }{{PlatformKilo, "free"}, {PlatformKilo, "apikey"}, {PlatformOpenCodeGo, AccountModeZenFree}} {
		platform := tt.platform
		t.Run(platform+tt.mode, func(t *testing.T) {
			a := freeAccount(platform, tt.mode)
			if tt.mode == "apikey" {
				a.Credentials["api_key"] = "private-fixture"
			}
			if platform == PlatformOpenCodeGo {
				a.Credentials["account_mode"] = AccountModeZenFree
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if platform == PlatformKilo {
					require.Equal(t, "/models", r.URL.Path)
					if tt.mode == "apikey" {
						if r.Header.Get("Authorization") == "" {
							w.WriteHeader(http.StatusUnauthorized)
							return
						}
						require.Equal(t, "Bearer private-fixture", r.Header.Get("Authorization"))
					} else {
						require.Empty(t, r.Header.Get("Authorization"))
					}
					fmt.Fprint(w, `{"data":[{"id":"kilo-auto/free","isFree":true},{"id":"paid","isFree":false}]}`)
				} else {
					require.Equal(t, "/v1/models", r.URL.Path)
					require.Equal(t, "Bearer public", r.Header.Get("Authorization"))
					require.Equal(t, zenFreeUserAgent, r.Header.Get("User-Agent"))
					require.Equal(t, "cli", r.Header.Get("X-OpenCode-Client"))
					fmt.Fprint(w, `{"data":[{"id":"big-pickle"},{"id":"paid","cost":{"input":1}}]}`)
				}
			}))
			defer server.Close()
			a.Credentials["base_url"] = server.URL
			svc := &AccountTestService{cfg: wbConfig(server.URL), httpUpstream: wbHTTP{server.Client()}}
			result := svc.ProbeConnection(t.Context(), a, "")
			require.Equal(t, "passed", result.Network)
			require.Equal(t, "passed", result.Authorization)
			require.Equal(t, "passed", result.Discovery)
			if tt.mode == "apikey" {
				require.Len(t, result.Models, 2)
			} else {
				require.Len(t, result.Models, 1)
			}
			ids, _, err := svc.fetchUpstreamModelList(t.Context(), a)
			require.NoError(t, err)
			require.Equal(t, result.Models, ids)
		})
	}
}
func TestFreeModelWireTransportAnd401(t *testing.T) {
	for _, protocol := range []string{"chat/completions", "responses", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			a := freeAccount(PlatformOpenCodeGo, AccountModeZenFree)
			base := &openCodeSessionHTTPUpstream{}
			req := httptest.NewRequest("POST", "https://opencode.ai/zen/v1/"+protocol, bytes.NewReader([]byte(`{"model":"big-pickle","messages":[{"role":"user","content":"hi"}]}`)))
			resp, err := accountHTTPUpstream(base, a).Do(req, "", a.ID, 1)
			require.NoError(t, err)
			resp.Body.Close()
			body, err := io.ReadAll(base.request.Body)
			require.NoError(t, err)
			require.Len(t, gjson.GetBytes(body, "tools").Array(), 5)
			require.Equal(t, zenFreeUserAgent, base.request.Header.Get("User-Agent"))
		})
	}
	for _, a := range []*Account{freeAccount(PlatformKilo, ""), freeAccount(PlatformOpenCodeGo, AccountModeZenFree)} {
		require.False(t, (&RateLimitService{}).HandleUpstreamError(context.Background(), a, 401, nil, []byte(`{"error":{"message":"public busy"}}`)))
		require.False(t, (&OpenAIGatewayService{}).handleOpenAIAccountUpstreamError(t.Context(), a, 401, nil, nil))
	}
}
func TestFreeModelPlazaCandidates(t *testing.T) {
	svc := newPlazaService(nil, []Group{{ID: 10, Platform: PlatformKilo, AccountType: AccountTypeAPIKey}, {ID: 11, Platform: PlatformOpenCodeGo, AccountType: AccountTypeAPIKey}}, nil)
	groups, err := svc.ListPublicCatalogGroups(t.Context())
	require.NoError(t, err)
	models := catalogGroupModels(groups)
	for _, id := range kiloFreeModelIDs {
		m, ok := models[10][PlazaModelIdentity{PlatformKilo, id}]
		require.True(t, ok, id)
		require.True(t, m.CatalogOnly)
		require.Nil(t, m.Pricing)
	}
	for _, m := range openCodeZenFreeModelPresets()[0].Models {
		_, ok := models[11][PlazaModelIdentity{PlatformOpenCodeGo, m.ID}]
		require.True(t, ok, m.ID)
	}
}

func TestFreeModelActiveTestsDefaultAndNativeProtocols(t *testing.T) {
	for _, tt := range []struct {
		platform, mode, model, path string
		response                    func() *http.Response
	}{
		{PlatformKilo, "", "", "https://api.kilo.ai/api/gateway/chat/completions", adaptiveCNChatTestResponse},
		{PlatformKilo, "apikey", "", "https://api.kilo.ai/api/gateway/chat/completions", adaptiveCNChatTestResponse},
		{PlatformOpenCodeGo, AccountModeZenFree, "", "https://opencode.ai/zen/v1/chat/completions", adaptiveCNChatTestResponse},
		{PlatformOpenCodeGo, AccountModeZenFree, "muse-spark-1.3-contributor-free", "https://opencode.ai/zen/v1/responses", adaptiveCNResponsesTestResponse},
		{PlatformOpenCodeGo, AccountModeZenFree, "union-alpha", "https://opencode.ai/zen/v1/messages", adaptiveCNAnthropicTestResponse},
	} {
		t.Run(tt.platform+tt.mode+tt.model, func(t *testing.T) {
			a := freeAccount(tt.platform, tt.mode)
			if tt.mode == "apikey" {
				a.Credentials["api_key"] = "fixture"
			}
			svc, upstream := adaptiveCNAccountTestService(a, tt.response())
			c, recorder := newTestContext()
			require.NoError(t, svc.TestAccountConnection(c, a.ID, tt.model, "hello", AccountTestModeDefault))
			require.Len(t, upstream.requests, 1)
			req := upstream.requests[0]
			require.Equal(t, tt.path, req.URL.String())
			require.Contains(t, recorder.Body.String(), `"type":"test_complete"`)
			if a.IsKiloFree() {
				require.Empty(t, req.Header.Get("Authorization"))
			}
		})
	}
}
func TestFreeModelAdminCreateAndModeChanges(t *testing.T) {
	for _, a := range []*Account{freeAccount(PlatformKilo, ""), freeAccount(PlatformOpenCodeGo, AccountModeZenFree)} {
		repo := &upstreamBillingProbeAccountRepo{}
		svc := &adminServiceImpl{accountRepo: repo}
		created, err := svc.CreateAccount(t.Context(), &CreateAccountInput{Name: "free", Platform: a.Platform, Type: a.Type, Credentials: a.Credentials, SkipDefaultGroupBind: true})
		require.NoError(t, err)
		if a.Platform == PlatformKilo {
			require.Equal(t, "free", created.Credentials["account_mode"])
			require.Equal(t, "", created.Credentials["api_key"])
		} else {
			require.Equal(t, "public", created.Credentials["api_key"])
		}
	}
}

func TestFreeModelAdminPublicTransitions(t *testing.T) {
	for _, platform := range []string{PlatformKilo, PlatformOpenCodeGo} {
		paid, public := KiloAccountModeAPIKey, KiloAccountModeFree
		if platform == PlatformOpenCodeGo {
			paid, public = AccountModeZen, AccountModeZenFree
		}
		a := freeAccount(platform, paid)
		a.Credentials["api_key"] = "previous-private-key"
		repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{a.ID: a}}
		svc := &adminServiceImpl{accountRepo: repo}
		updated, err := svc.UpdateAccount(t.Context(), a.ID, &UpdateAccountInput{Credentials: map[string]any{"account_mode": public}})
		require.NoError(t, err)
		require.True(t, publicModelLane(updated))
		require.NotEqual(t, "previous-private-key", updated.Credentials["api_key"])
		patch := &BulkUpdateAccountsInput{Credentials: map[string]any{"account_mode": public}}
		require.NoError(t, normalizeFreeModelBulkCredentials([]*Account{a}, patch))
		require.Equal(t, updated.Credentials["api_key"], patch.Credentials["api_key"])
		a.Credentials[CredentialSecretReferenceKey] = "ref-fixture"
		require.Error(t, normalizeFreeModelBulkCredentials([]*Account{a}, patch))
		if platform == PlatformKilo {
			_, err = svc.UpdateAccount(t.Context(), a.ID, &UpdateAccountInput{Credentials: map[string]any{"account_mode": paid}})
			require.Error(t, err)
			updated, err = svc.UpdateAccount(t.Context(), a.ID, &UpdateAccountInput{Credentials: map[string]any{"account_mode": paid, "api_key": "replacement"}})
			require.NoError(t, err)
			require.Equal(t, "replacement", updated.GetCredential("api_key"))
		}
	}
}

func TestZenFreeLegacyAndRetainedIndexes(t *testing.T) {
	body, p, err := prepareZenFreeRequest(nil, []byte(`{"functions":[{"name":"Bash","parameters":{"type":"object"}}]}`), APIProtocolChatCompletions)
	require.NoError(t, err)
	require.Len(t, gjson.GetBytes(body, "tools").Array(), 5)
	require.False(t, p.injected["bash"])
	out, err := p.guard([]byte(`{"choices":[{"index":0,"message":{"tool_calls":[{"function":{"name":"bash","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))
	require.NoError(t, err)
	require.Equal(t, "Bash", gjson.GetBytes(out, "choices.0.message.function_call.name").String())
	require.Equal(t, "function_call", gjson.GetBytes(out, "choices.0.finish_reason").String())
	for _, protocol := range []string{APIProtocolAnthropic, APIProtocolResponses} {
		_, p, err = prepareZenFreeRequest(nil, []byte(`{}`), protocol)
		require.NoError(t, err)
		if protocol == APIProtocolAnthropic {
			out, err = p.guard([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","name":"bash"}}`))
			require.NoError(t, err)
			require.Nil(t, out)
			out, err = p.guard([]byte(`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":"ok"}}`))
			require.NoError(t, err)
			require.EqualValues(t, 0, gjson.GetBytes(out, "index").Int())
		} else {
			out, err = p.guard([]byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","name":"bash","id":"d"}}`))
			require.NoError(t, err)
			require.Nil(t, out)
			out, err = p.guard([]byte(`{"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"m"}}`))
			require.NoError(t, err)
			require.EqualValues(t, 0, gjson.GetBytes(out, "output_index").Int())
		}
	}
}

func TestKiloProtocolBridgesAndUsage(t *testing.T) {
	const usage = `"usage":{"prompt_tokens":9000000,"completion_tokens":99,"total_tokens":9000099,"completion_tokens_details":{"reasoning_tokens":70},"cost":0.00000000000000123}`
	for _, mode := range []string{KiloAccountModeFree, KiloAccountModeAPIKey} {
		for _, stream := range []bool{false, true} {
			for _, protocol := range []string{"chat/completions", "messages", "responses"} {
				t.Run(fmt.Sprintf("%s/%t/%s", mode, stream, protocol), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						require.Equal(t, "/chat/completions", r.URL.Path)
						if mode == KiloAccountModeFree {
							require.Empty(t, r.Header.Get("Authorization"))
						} else {
							require.Equal(t, "Bearer fixture", r.Header.Get("Authorization"))
						}
						b, err := io.ReadAll(r.Body)
						require.NoError(t, err)
						if gjson.GetBytes(b, "stream").Bool() {
							require.True(t, gjson.GetBytes(b, "stream_options.include_usage").Bool())
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprint(w, ": KILO PROCESSING\n\ndata: {\"id\":\"chat-1\",\"model\":\"kilo-auto/free\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chat-1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],"+usage+"}\n\ndata: [DONE]\n\n")
						} else {
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprint(w, `{"id":"chat-1","object":"chat.completion","model":"kilo-auto/free","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],`+usage+`}`)
						}
					}))
					defer server.Close()
					a := freeAccount(PlatformKilo, mode)
					a.Credentials["base_url"] = server.URL
					if mode == KiloAccountModeAPIKey {
						a.Credentials["api_key"] = "fixture"
					}
					svc := &OpenAIGatewayService{cfg: wbConfig(server.URL), httpUpstream: wbHTTP{server.Client()}}
					body := []byte(fmt.Sprintf(`{"model":"kilo-auto/free","max_tokens":128,"stream":%t,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`, stream))
					if protocol == "responses" {
						body = []byte(fmt.Sprintf(`{"model":"kilo-auto/free","stream":%t,"input":"hi"}`, stream))
					}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest("POST", "/v1/"+protocol, bytes.NewReader(body))
					var result *OpenAIForwardResult
					var err error
					switch protocol {
					case "messages":
						result, err = svc.ForwardAsAnthropic(t.Context(), c, a, body, "", "")
					case "responses":
						result, err = svc.forwardResponsesViaRawChatCompletions(t.Context(), c, a, body)
					default:
						result, err = svc.forwardAsRawChatCompletions(t.Context(), c, a, body, "")
					}
					require.NoError(t, err)
					require.False(t, result.UsageIncomplete)
					require.Equal(t, 256000, result.Usage.InputTokens)
					require.Equal(t, 99, result.Usage.OutputTokens)
					require.Contains(t, rec.Body.String(), "hello")
					if protocol == "chat/completions" {
						require.Contains(t, rec.Body.String(), `"reasoning_tokens":70`)
						require.Contains(t, rec.Body.String(), `0.00000000000000123`)
					}
				})
			}
		}
	}
}

func TestFreeModelPaginationRetainsCounterEvidence(t *testing.T) {
	for _, provider := range []string{PlatformKilo, PlatformOpenCodeGo} {
		for _, evidence := range []string{`"cost":{"input":1}`, `"status":"retired"`, `"isFree":false`} {
			for _, crossPage := range []bool{false, true} {
				for _, reversed := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/pages=%v/reversed=%v", provider, evidence, crossPage, reversed), func(t *testing.T) {
						good := `{"id":"candidate-free","isFree":true,"cost":{"input":0}}`
						bad := `{"id":"candidate-free",` + evidence + `}`
						if evidence != `"isFree":false` {
							bad = `{"id":"candidate-free","isFree":true,` + evidence + `}`
						}
						if reversed {
							good, bad = bad, good
						}
						pages := []*http.Response{catalogFixtureResponse(`{"data":[` + good + `,` + bad + `,{"id":"safe-free","isFree":true}]}`)}
						if crossPage {
							pages = []*http.Response{catalogFixtureResponse(`{"data":[` + good + `],"next_cursor":"two"}`), catalogFixtureResponse(`{"data":[` + bad + `,{"id":"safe-free","isFree":true}]}`)}
						}
						upstream := &httpUpstreamRecorder{responses: pages}
						svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
						mode := "free"
						if provider == PlatformOpenCodeGo {
							mode = AccountModeZenFree
						}
						account := freeAccount(provider, mode)
						req, err := http.NewRequest(http.MethodGet, "https://api.example.test/models", nil)
						require.NoError(t, err)
						ids, raw, err := svc.fetchCompleteModelPages(req, account)
						require.NoError(t, err)
						require.Equal(t, []string{"safe-free"}, ids)
						entries, err := extractUpstreamModelRawEntries(raw)
						require.NoError(t, err)
						require.Len(t, entries, 3)
						parsed, _, err := extractAccountModelCatalog(raw, account)
						require.NoError(t, err)
						require.Equal(t, ids, parsed)
					})
				}
			}
		}
	}
}

func TestFreeModelPaginationFailurePreservesPartialStatus(t *testing.T) {
	for _, a := range []*Account{freeAccount(PlatformKilo, "free"), freeAccount(PlatformOpenCodeGo, AccountModeZenFree)} {
		t.Run(a.Platform, func(t *testing.T) {
			second := catalogFixtureResponse("{}")
			second.StatusCode = http.StatusServiceUnavailable
			svc := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: &httpUpstreamRecorder{responses: []*http.Response{
				catalogFixtureResponse(`{"data":[{"id":"candidate-free","isFree":true},{"id":"candidate-free","isFree":false},{"id":"safe-free","isFree":true}],"next_cursor":"two"}`), second,
			}}}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://catalog.test/models", nil)
			require.NoError(t, err)
			ids, _, err := svc.fetchCompleteModelPages(req, a)
			var partial *UpstreamModelSyncError
			require.ErrorAs(t, err, &partial)
			require.NotNil(t, partial.PartialCatalog)
			require.Equal(t, "partial", partial.PartialCatalog.CatalogSource)
			require.Empty(t, partial.PartialCatalog.Models)
			require.Equal(t, []string{"safe-free"}, ids)
		})
	}
}
