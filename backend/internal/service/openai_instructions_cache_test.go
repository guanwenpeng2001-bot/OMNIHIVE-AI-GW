//go:build unit

package service

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSupportsInstructionsCacheHoist(t *testing.T) {
	for model, want := range map[string]bool{"gpt-6.1-sol": false, "gpt-6.2-astra": true, "gpt-6.2-sol": true, "gpt-6.10": true, "gpt-6.3": true, "gpt-7-mini": true, "gpt-10": true, "gpt-5.6": false, "gpt-6.": false, "gpt-7x": false, "other-7": false, "GPT-6.2-Astra": true, " gpt-6.2 ": true} {
		t.Run(model, func(t *testing.T) { require.Equal(t, want, supportsInstructionsCacheHoist(model)) })
	}
}

func TestHoistOpenAIInstructions(t *testing.T) {
	const developer = `{"type":"message","role":"developer","content":[{"type":"input_text","text":"rules"}]}`
	const original = `{"store":false, "id":9007199254740993, "instructions":"rules","input":[  ]}`
	api := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for _, tc := range []struct {
		name, from, to string
		account        *Account
		compact        bool
	}{
		{"missing store", `"store":false,`, "", api, false}, {"store true", "false", "true", api, false}, {"store null", "false", "null", api, false}, {"store string", "false", `"false"`, api, false},
		{"previous response", `"id"`, `"previous_response_id":null,"id"`, api, false}, {"conversation", `"id"`, `"conversation":"abc","id"`, api, false},
		{"missing instructions", `"instructions":"rules",`, "", api, false}, {"empty instructions", `"rules"`, `"  "`, api, false}, {"object instructions", `"rules"`, "{}", api, false},
		{"missing input", `"input"`, `"other"`, api, false}, {"object input", "[  ]", "{}", api, false},
		{"compact", "", "", api, true}, {"OAuth", "", "", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, false}, {"third party", "", "", &Account{Platform: PlatformOpenRouter, Type: AccountTypeAPIKey}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(strings.Replace(original, tc.from, tc.to, 1))
			got, changed := hoistOpenAIInstructions(tc.account, "gpt-6.2-sol", tc.compact, body)
			require.False(t, changed)
			require.Equal(t, body, got)
		})
	}
	for _, tc := range []struct{ name, input, want string }{
		{"empty", "[  ]", "[" + developer + "  ]"},
		{"array", `[ {"n":1e+02} ]`, "[" + developer + `, {"n":1e+02} ]`},
		{"escaped string", `"a\nb"`, "[" + developer + `,{"type":"message","role":"user","content":[{"type":"input_text","text":"a\nb"}]}]`},
		{"duplicate", "[" + developer + "]", "[" + developer + "]"},
		{"short duplicate", `[{"role":"developer","content":"rules"}]`, `[{"role":"developer","content":"rules"}]`},
		{"different", `[{"role":"developer","content":"other"}]`, "[" + developer + `,{"role":"developer","content":"other"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(original, "[  ]", tc.input, 1)
			got, changed := hoistOpenAIInstructions(api, "gpt-6.2-sol", false, []byte(body))
			require.True(t, changed)
			require.Equal(t, `{"store":false, "id":9007199254740993,"input":`+tc.want+"}", string(got))
		})
	}
}

type instructionsCacheTestUpstream struct {
	HTTPUpstream
	server *httptest.Server
}

func (u instructionsCacheTestUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = u.server.Listener.Addr().String()
	return u.server.Client().Do(req)
}

func TestForwardInstructionsCacheHoist(t *testing.T) {
	for _, tc := range []struct {
		name, model                       string
		oauth, passthrough, enabled, want bool
	}{
		{"mapped", "gpt-6.2-sol", false, false, true, true}, {"passthrough", "gpt-6.2-astra", false, true, true, true}, {"legacy", "gpt-6.1-sol", false, false, true, false}, {"oauth", "gpt-6.2-sol", true, true, true, false}, {"disabled", "gpt-6.2-sol", false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				captured <- payload
				if tc.oauth {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: "+`{"type":"response.completed","response":{"id":"resp_test","output":[],"usage":{"input_tokens":9,"input_tokens_details":{"cached_tokens":7},"output_tokens":1}}}`+"\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"resp_test","object":"response","output":[],"usage":{"input_tokens":9,"input_tokens_details":{"cached_tokens":7},"output_tokens":1}}`)
			}))
			defer server.Close()
			svc := newOpenAIImageGenerationControlTestService(nil)
			svc.httpUpstream = instructionsCacheTestUpstream{server: server}
			svc.cfg.Gateway.OpenAIInstructionsCacheHoist = tc.enabled
			model := tc.model
			if tc.name == "mapped" {
				model = "public-model"
			}
			account := rawGPT56ResponsesAPIKeyAccount(model, tc.model)
			account.Extra["openai_passthrough"] = tc.passthrough
			if tc.oauth {
				account.Type = AccountTypeOAuth
				account.Credentials = map[string]any{"access_token": "fixture", "chatgpt_account_id": "fixture"}
			}
			c, _ := newOpenAIImageGenerationControlTestContext(true, "codex_cli_rs/0.144.1")
			body := fmt.Sprintf(`{"model":%q,"store":false,"instructions":"rules","input":[{"role":"user","content":"hello"}]}`, model)
			result, err := svc.Forward(t.Context(), c, account, []byte(body))
			require.NoError(t, err)
			got := <-captured
			require.Equal(t, !tc.want, gjson.GetBytes(got, "instructions").Exists())
			if tc.want {
				require.Equal(t, "developer", gjson.GetBytes(got, "input.0.role").String())
				require.Equal(t, "rules", gjson.GetBytes(got, "input.0.content.0.text").String())
			} else {
				require.Equal(t, "rules", gjson.GetBytes(got, "instructions").String())
				require.Equal(t, "user", gjson.GetBytes(got, "input.0.role").String())
			}
			require.Equal(t, 7, result.Usage.CacheReadInputTokens)
		})
	}
}
