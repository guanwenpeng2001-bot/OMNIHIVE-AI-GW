package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesLeadingRolesProfiles(t *testing.T) {
	for _, tt := range []struct {
		name, instructions, input, want string
		strict                          bool
	}{
		{"default preserves leading roles", "root", `[{"role":"developer","content":"dev"},{"role":"system","content":"sys"},{"role":"user","content":"hi"}]`, `[{"role":"system","content":"root"},{"role":"developer","content":"dev"},{"role":"system","content":"sys"},{"role":"user","content":"hi"}]`, false},
		{"strict ordered merge", " root\n", `[{"role":"developer","content":[{"type":"input_text","text":" dev "},{"type":"input_text","text":""}]},{"role":"system","content":"sys"},{"role":"user","content":"hi"}]`, `[{"role":"system","content":" root\n\n\n dev \n\n\n\nsys"},{"role":"user","content":"hi"}]`, true},
		{"empty boundaries", "", `[{"role":"system","content":null},{"role":"developer","content":[]},{"role":"system","content":""},{"role":"user","content":"hi"}]`, `[{"role":"system","content":"\n\n\n\n"},{"role":"user","content":"hi"}]`, true},
		{"single empty", "", `[{"role":"developer"}]`, `[{"role":"system","content":""}]`, true},
		{"whitespace instructions", " \n", `"hello"`, `[{"role":"system","content":" \n"},{"role":"user","content":"hello"}]`, true},
		{"ordinary strict", "", `[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]`, `[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]`, true},
		{"ordinary default", "", `[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]`, `[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]`, false},
		{"single keeps escaped text", "", `[{"role":"developer","content":"  a\nb  "}]`, `[{"role":"system","content":"  a\nb  "}]`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, err := ResponsesToChatCompletionsRequestWithOptions(&ResponsesRequest{
				Model: "qwen-fixture", Instructions: tt.instructions, Input: json.RawMessage(tt.input),
			}, &ResponsesToChatOptions{SingleLeadingSystemMessage: tt.strict})
			require.NoError(t, err)
			got, err := json.Marshal(out.Messages)
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(got))
		})
	}
}

func TestResponsesStrictLeadingRolesRejectMidConversation(t *testing.T) {
	for _, input := range []string{
		`[{"role":"user","content":"hi"},{"role":"system","content":"late"}]`,
		`[{"role":"system","content":"root"},{"role":"assistant","content":""},{"role":"developer","content":"late"}]`,
		`[{"role":"user","content":""},{"role":"developer","content":""}]`,
		`[{"type":"function_call","call_id":"pending","name":"exec","arguments":"{}"},{"role":"system","content":"late"}]`,
		`[{"type":"function_call","call_id":"invalid","name":"exec","arguments":"{"},{"role":"system","content":"late"}]`,
		`[{"type":"reasoning","summary":[]},{"role":"developer","content":"late"}]`,
		`["hello",{"role":"system","content":"late"}]`,
	} {
		t.Run(input, func(t *testing.T) {
			out, err := ResponsesToChatCompletionsRequestWithOptions(&ResponsesRequest{Input: json.RawMessage(input)}, &ResponsesToChatOptions{SingleLeadingSystemMessage: true})
			require.ErrorContains(t, err, "mid-conversation")
			require.Nil(t, out)
		})
	}
}

func TestResponsesInstructionUnsupportedContentIsExplicit(t *testing.T) {
	for _, role := range []string{"system", "developer"} {
		for _, content := range []string{
			`[{"type":"input_text","text":"keep"},{"type":"input_image","image_url":"https://example.com/a.png"}]`,
			`{"type":"input_audio","data":"AA=="}`,
			`[{"type":"future_content","text":"opaque"}]`,
			`[null]`, `42`, `[{"type":"text","text":42}]`, `[{"type":"text","text":null}]`,
		} {
			for _, strict := range []bool{false, true} {
				out, err := ResponsesToChatCompletionsRequestWithOptions(&ResponsesRequest{
					Input: json.RawMessage(`[{"role":"` + role + `","content":` + content + `}]`),
				}, &ResponsesToChatOptions{SingleLeadingSystemMessage: strict})
				require.ErrorContains(t, err, "unsupported "+role+" instruction")
				require.Nil(t, out)
			}
		}
	}
}
