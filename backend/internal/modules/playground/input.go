package playground

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"maps"
	"math"
	"strconv"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
)

const MaxInput = 256 << 10

type Options struct {
	Stream *bool `json:"stream,omitempty"`
	Raw    bool  `json:"include_raw_events,omitempty"`
	Retain bool  `json:"retain_text_preview,omitempty"`
}
type Submit struct {
	Registration         *journal.Registration `json:"-"`
	Key                  int64                 `json:"api_key_id,string"`
	Payer                int64                 `json:"payer_user_id,string,omitempty"`
	Model                string                `json:"model_id,omitempty"`
	Protocol             string                `json:"protocol,omitempty"`
	Action               string                `json:"action,omitempty"`
	Input                map[string]any        `json:"input"`
	Options              Options               `json:"options,omitempty"`
	Consent              bool                  `json:"funding_acknowledged"`
	ExpectedPriceVersion string                `json:"expected_price_version,omitempty"`
}

func (s Submit) Streaming() bool { return s.Options.Stream == nil || *s.Options.Stream }
func Parse(body []byte, key, parent string) (Submit, [32]byte, error) {
	var s Submit
	var fp [32]byte
	if strings.TrimSpace(key) == "" || len(key) > 200 {
		return s, fp, fault.Invalid("/Idempotency-Key")
	}
	if len(body) > MaxInput {
		return s, fp, fault.Invalid("/input")
	}
	registration, err := journal.Register("playground/"+key, body)
	if err != nil {
		return s, fp, fault.Invalid("/input")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if dec.Decode(&s) != nil || dec.Decode(new(any)) != io.EOF {
		return s, fp, fault.Invalid("/input")
	}
	if s.Key <= 0 {
		return s, fp, fault.Invalid("/api_key_id")
	}
	if len(s.ExpectedPriceVersion) > 200 || strings.TrimSpace(s.ExpectedPriceVersion) != s.ExpectedPriceVersion {
		return s, fp, fault.Invalid("/expected_price_version")
	}
	if !s.Consent {
		return s, fp, fault.Invalid("/funding_acknowledged")
	}
	if s.Options.Raw && !s.Streaming() {
		return s, fp, fault.Invalid("/options/include_raw_events")
	}
	if s.Input == nil {
		return s, fp, fault.Invalid("/input")
	}
	if parent != "" && (s.Model != "" || s.Protocol != "" || s.Action != "") {
		return s, fp, fault.Invalid("/replay")
	}
	s.Registration = registration
	canonical, err := json.Marshal(struct {
		Parent string
		Input  Submit
	}{parent, s})
	return s, sha256.Sum256(canonical), err
}

type Native struct {
	Path, Protocol, Adapter string
	Body                    []byte
	InputBound, OutputBound int
	Tools                   bool
}

func (s Submit) Native(model string) (Native, error) {
	var n Native
	if s.Action != "text.chat" || s.Model == "" || len(s.Model) > 255 || model == "" {
		return n, fault.Invalid("/model_id")
	}
	// The credential, selected model, output shape and multiplicity belong to the run.
	input := make(map[string]any, len(s.Input)+3)
	for k, v := range s.Input {
		input[k] = v
	}
	if v, ok := input["model"]; ok && v != model {
		return n, fault.Invalid("/input/model")
	}
	if v, ok := input["stream"]; ok && v != s.Streaming() {
		return n, fault.Invalid("/input/stream")
	}
	if tools, ok := input["tools"]; ok {
		a, ok := tools.([]any)
		if !ok || len(a) > 128 {
			return n, fault.Invalid("/input/tools")
		}
		n.Tools = len(a) > 0
	}
	if v, ok := input["store"]; ok && v != false {
		return n, fault.Invalid("/input/store")
	}
	params := input
	bound := "max_tokens"
	required := "messages"
	switch s.Protocol {
	case "openai_chat":
		n.Path, n.Protocol, n.Adapter = "/v1/chat/completions", "chat_completions", "chat"
		if s.Streaming() {
			options := map[string]any{}
			if v, ok := input["stream_options"]; ok {
				m, ok := v.(map[string]any)
				if !ok {
					return n, fault.Invalid("/input/stream_options")
				}
				options = maps.Clone(m)
			}
			options["include_usage"] = true
			input["stream_options"] = options
		}
		if input["max_completion_tokens"] != nil {
			if input["max_tokens"] != nil {
				return n, fault.Invalid("/input/max_tokens")
			}
			bound = "max_completion_tokens"
		}
	case "openai_responses":
		n.Path, n.Protocol, n.Adapter = "/v1/responses", "responses", "responses"
		bound, required = "max_output_tokens", "input"
		// Background or stored responses outlive the console's cancellation and retention contract.
		for _, key := range []string{"store", "background"} {
			if val, ok := input[key]; ok && val != false {
				return n, fault.Invalid("/input/" + key)
			}
			input[key] = false
		}
	case "anthropic_messages":
		n.Path, n.Protocol, n.Adapter = "/v1/messages", "anthropic", "messages"
	case "gemini_generate":
		if strings.ContainsAny(model, "?#:%\\") || strings.Contains(model, "..") {
			return n, fault.Invalid("/model_id")
		}
		suffix := ":generateContent"
		if s.Streaming() {
			suffix = ":streamGenerateContent?alt=sse"
		}
		n.Path, n.Protocol, n.Adapter = "/v1beta/models/"+model+suffix, "gemini", "generate_content"
		bound, required = "maxOutputTokens", "contents"
		params = map[string]any{}
		if v, ok := input["generationConfig"]; ok {
			var valid bool
			params, valid = v.(map[string]any)
			if !valid {
				return n, fault.Invalid("/input/generationConfig")
			}
		}
		params = maps.Clone(params)
		input["generationConfig"] = params
		delete(input, "model")
		delete(input, "stream")
	default:
		return n, fault.Invalid("/protocol")
	}
	if s.Protocol != "gemini_generate" {
		input["model"] = model
		input["stream"] = s.Streaming()
	}
	switch value := input[required].(type) {
	case []any:
		if len(value) == 0 || len(value) > 512 {
			return n, fault.Invalid("/input/" + required)
		}
	case string:
		if required != "input" || value == "" {
			return n, fault.Invalid("/input/" + required)
		}
	default:
		return n, fault.Invalid("/input/" + required)
	}
	for _, key := range []string{"n", "best_of", "candidateCount"} {
		if v, ok := params[key]; ok && v != json.Number("1") {
			return n, fault.Invalid("/input/" + key)
		}
	}
	for k, maximum := range map[string]float64{"temperature": 2, "top_p": 1, "topP": 1, "top_k": 500, "topK": 500, "max_tool_calls": 128, "top_logprobs": 20} {
		if val, ok := params[k]; ok {
			v, num := val.(json.Number)
			qty, err := v.Float64()
			if !num || err != nil || math.IsNaN(qty) || qty < 0 || qty > maximum {
				return n, fault.Invalid("/input/" + k)
			}
		}
	}
	n.OutputBound = 1024
	if v, ok := params[bound]; ok {
		j, ok := v.(json.Number)
		qty, err := strconv.Atoi(j.String())
		if !ok || err != nil || qty < 1 || qty > 32768 {
			return n, fault.Invalid("/input/" + bound)
		}
		n.OutputBound = qty
	}
	params[bound] = n.OutputBound
	b, err := json.Marshal(input)
	n.Body = b
	n.InputBound = len(b)
	return n, err
}
