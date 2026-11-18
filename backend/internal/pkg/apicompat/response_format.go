package apicompat

import (
	"encoding/json"
	"fmt"
)

// Anthropic's schema format has no name; OpenAI requires one. Preserve the
// caller's schema without imposing strict-mode constraints on its properties.
func anthropicOutputFormatToResponses(raw json.RawMessage) (json.RawMessage, error) {
	if len(normalizedRawJSON(raw)) == 0 {
		return nil, nil
	}
	format, ok := rawJSONObject(raw)
	if !ok || rawString(format["type"]) != "json_schema" {
		return nil, fmt.Errorf("output_config.format must use json_schema")
	}
	if _, ok := rawJSONObject(format["schema"]); !ok || string(format["schema"]) == "null" {
		return nil, fmt.Errorf("output_config.format.schema must be an object")
	}
	format["name"] = rawJSONString("response")
	return json.Marshal(format)
}

func chatResponseFormatToResponsesTextFormat(raw json.RawMessage) json.RawMessage {
	raw = normalizedRawJSON(raw)
	if len(raw) == 0 {
		return nil
	}

	obj, ok := rawJSONObject(raw)
	if !ok || rawString(obj["type"]) != "json_schema" {
		return raw
	}

	schemaRaw := normalizedRawJSON(obj["json_schema"])
	if len(schemaRaw) == 0 {
		return raw
	}

	var schema map[string]json.RawMessage
	if err := json.Unmarshal(schemaRaw, &schema); err != nil {
		return raw
	}
	schema["type"] = rawJSONString("json_schema")
	// Chat Completions leaves strict mode off by default. Preserve that mode
	// explicitly when translating to Responses, without changing explicit values.
	if _, exists := schema["strict"]; !exists {
		schema["strict"] = json.RawMessage("false")
	}

	out, err := json.Marshal(schema)
	if err != nil {
		return raw
	}
	return out
}

func responsesTextFormatToChatResponseFormat(raw json.RawMessage) json.RawMessage {
	raw = normalizedRawJSON(raw)
	if len(raw) == 0 {
		return nil
	}

	obj, ok := rawJSONObject(raw)
	if !ok || rawString(obj["type"]) != "json_schema" {
		return raw
	}
	if _, alreadyChatShape := obj["json_schema"]; alreadyChatShape {
		return raw
	}

	schema := make(map[string]json.RawMessage, len(obj))
	for key, value := range obj {
		if key == "type" {
			continue
		}
		schema[key] = value
	}
	if len(schema) == 0 {
		return raw
	}

	schemaRaw, err := json.Marshal(schema)
	if err != nil {
		return raw
	}
	out, err := json.Marshal(map[string]json.RawMessage{
		"type":        rawJSONString("json_schema"),
		"json_schema": schemaRaw,
	})
	if err != nil {
		return raw
	}
	return out
}

func normalizedRawJSON(raw json.RawMessage) json.RawMessage {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func rawJSONObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false
	}
	return obj, true
}

func rawJSONString(value string) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}
