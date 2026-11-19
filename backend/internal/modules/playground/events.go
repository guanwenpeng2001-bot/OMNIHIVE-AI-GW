package playground

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// Projection keeps provider error bodies, transport headers, opaque thinking
// signatures and encrypted payloads out of both the normalized and raw streams.
type Event struct {
	Name string
	Data map[string]any
}
type tool struct{ id, name string }
type Decoder struct {
	Protocol          string
	tools             map[int]tool
	Completed, Failed bool
}

func (d *Decoder) Frame(event, data string) ([]Event, error) {
	if d.Completed || d.Failed {
		return nil, nil
	}
	if data == "[DONE]" {
		if d.Protocol == "openai_chat" {
			d.Completed = true
		}
		return nil, nil
	}
	if !gjson.Valid(data) {
		return nil, errors.New("invalid upstream frame")
	}
	root := gjson.Parse(data)
	kind := root.Get("type").String()
	if kind == "" {
		kind = event
	}
	if root.Get("error").Exists() || kind == "error" || kind == "response.failed" || kind == "response.incomplete" || kind == "response.cancelled" || kind == "response.canceled" {
		d.Failed = true
		return nil, nil
	}
	events := []Event{}
	delta := func(idx int, k, s string) {
		if s != "" {
			events = append(events, Event{"delta", map[string]any{"kind": k, "index": idx, "text": s}})
		}
	}
	args := func(i int, id, name, fragment string) {
		if d.tools == nil {
			d.tools = map[int]tool{}
		}
		if i < 0 || i >= 256 {
			return
		}
		t := d.tools[i]
		if id != "" {
			t.id = id
		}
		if name != "" {
			t.name = name
		}
		d.tools[i] = t
		events = append(events, Event{"delta", map[string]any{"kind": "tool_arguments", "index": i, "tool_call_id": t.id, "name": t.name, "fragment": fragment}})
	}
	usage := func(r gjson.Result, input, output string) {
		q := []map[string]string{}
		for _, p := range [][2]string{{input, "input_token"}, {output, "output_token"}} {
			v := r.Get(p[0])
			if v.Type != gjson.Number {
				continue
			}
			if n, e := strconv.ParseUint(v.Raw, 10, 63); e == nil {
				q = append(q, map[string]string{"unit": p[1], "quantity": strconv.FormatUint(n, 10)})
			}
		}
		if len(q) > 0 {
			events = append(events, Event{"usage", map[string]any{"quantities": q, "evidence": "provider_reported"}})
		}
	}
	switch d.Protocol {
	case "openai_chat":
		for _, choice := range root.Get("choices").Array() {
			i := int(choice.Get("index").Int())
			m := choice.Get("delta")
			delta(i, "text", m.Get("content").String())
			delta(i, "reasoning", m.Get("reasoning_content").String())
			for _, t := range m.Get("tool_calls").Array() {
				args(int(t.Get("index").Int()), t.Get("id").String(), t.Get("function.name").String(), t.Get("function.arguments").String())
			}
		}
		usage(root.Get("usage"), "prompt_tokens", "completion_tokens")
	case "openai_responses":
		i := int(root.Get("output_index").Int())
		switch kind {
		case "response.output_text.delta":
			delta(i, "text", root.Get("delta").String())
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			delta(i, "reasoning", root.Get("delta").String())
		case "response.output_item.added":
			if t := root.Get("item"); t.Get("type").String() == "function_call" {
				args(i, t.Get("call_id").String(), t.Get("name").String(), t.Get("arguments").String())
			}
		case "response.function_call_arguments.delta":
			args(i, "", "", root.Get("delta").String())
		case "response.completed", "response.done":
			d.Completed = true
			usage(root.Get("response.usage"), "input_tokens", "output_tokens")
		}
	case "anthropic_messages":
		i := int(root.Get("index").Int())
		switch kind {
		case "message_start":
			usage(root.Get("message.usage"), "input_tokens", "output_tokens")
		case "message_delta":
			usage(root.Get("usage"), "input_tokens", "output_tokens")
		case "message_stop":
			d.Completed = true
		case "content_block_start":
			if t := root.Get("content_block"); t.Get("type").String() == "tool_use" {
				args(i, t.Get("id").String(), t.Get("name").String(), "")
			}
		case "content_block_delta":
			m := root.Get("delta")
			switch m.Get("type").String() {
			case "text_delta":
				delta(i, "text", m.Get("text").String())
			case "thinking_delta":
				delta(i, "reasoning", m.Get("thinking").String())
			case "input_json_delta":
				args(i, "", "", m.Get("partial_json").String())
			}
		}
	case "gemini_generate":
		for _, c := range root.Get("candidates").Array() {
			i := int(c.Get("index").Int())
			for j, p := range c.Get("content.parts").Array() {
				k := "text"
				if p.Get("thought").Bool() {
					k = "reasoning"
				}
				delta(i, k, p.Get("text").String())
				if t := p.Get("functionCall"); t.Exists() {
					id := t.Get("id").String()
					if id == "" {
						id = "call-" + strconv.Itoa(i) + "-" + strconv.Itoa(j)
					}
					args(j, id, t.Get("name").String(), t.Get("args").Raw)
				}
			}
			if why := c.Get("finishReason").String(); why != "" {
				d.Completed = why == "STOP" || why == "MAX_TOKENS"
				d.Failed = !d.Completed
			}
		}
		usage(root.Get("usageMetadata"), "promptTokenCount", "candidatesTokenCount")
	}
	return events, nil
}

// SyncOutput copies the native content fields recursively. Tool argument objects
// are model output and intentionally survive projection. Provider metadata does not.
func SyncOutput(protocol string, b []byte) (json.RawMessage, bool) {
	if !gjson.ValidBytes(b) {
		return nil, false
	}
	r := gjson.ParseBytes(b)
	var root string
	switch protocol {
	case "openai_chat":
		root = "choices"
	case "openai_responses":
		root = "output"
		if r.Get("status").String() != "completed" {
			return nil, false
		}
	case "anthropic_messages":
		root = "content"
	case "gemini_generate":
		root = "candidates"
	default:
		return nil, false
	}
	if !r.Get(root).IsArray() || len(r.Get(root).Array()) == 0 || r.Get("error").Exists() {
		return nil, false
	}
	out := map[string]any{root: project(r.Get(root))}
	for _, k := range []string{"usage", "usageMetadata"} {
		if v := r.Get(k); v.IsObject() {
			out[k] = project(v)
		}
	}
	for _, k := range []string{"id", "model", "object", "type", "role", "status", "stop_reason"} {
		if v := r.Get(k); v.Type == gjson.String {
			out[k] = v.String()
		}
	}
	raw, err := json.Marshal(out)
	return raw, err == nil
}

const fields = " call_id type role index text content message choices output status tool_calls function id name arguments reasoning_content thinking tool_use_id input parts candidates finishReason finish_reason functionCall args thought summary delta output_index item item_id content_block partial_json response usage usageMetadata prompt_tokens completion_tokens input_tokens output_tokens promptTokenCount candidatesTokenCount stop_reason model object "

func project(x gjson.Result) any {
	if x.IsArray() {
		items := []any{}
		for _, v := range x.Array() {
			items = append(items, project(v))
		}
		return items
	}
	if !x.IsObject() {
		return x.Value()
	}
	o := map[string]any{}
	x.ForEach(func(k, v gjson.Result) bool {
		if strings.Contains(k.String(), " ") || !strings.Contains(fields, " "+k.String()+" ") {
			return true
		}
		if k.String() == "args" || (k.String() == "input" && x.Get("type").String() == "tool_use") {
			o[k.String()] = json.RawMessage(v.Raw)
		} else {
			if k.String() != "input" {
				o[k.String()] = project(v)
			}
		}
		return true
	})
	return o
}

// RawFrame preserves native field names and projects only content and observed
// usage. The client receives transport framing separately from provider metadata.
func RawFrame(data string) any {
	if data == "[DONE]" {
		return data
	}
	r := gjson.Parse(data)
	if r.Get("error").Exists() || r.Get("type").String() == "error" {
		return map[string]any{"type": "error"}
	}
	return project(r)
}
