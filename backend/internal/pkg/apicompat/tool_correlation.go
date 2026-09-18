package apicompat

import (
	"encoding/json"
	"fmt"
)

// PreserveResponsesToolIDs reverses only IDs introduced by this conversion.
// Raw blocks retain unknown provider fields and opaque reasoning/signatures.
func PreserveResponsesToolIDs(source *ResponsesRequest, target *AnthropicRequest) error {
	if source == nil || target == nil {
		return nil
	}
	var items []ResponsesInputItem
	if json.Unmarshal(source.Input, &items) != nil {
		return nil
	}
	aliases := map[string]string{}
	for _, item := range items {
		if item.Type != "function_call" && item.Type != "function_call_output" {
			continue
		}
		alias := fromResponsesCallIDToAnthropic(item.CallID)
		if original, ok := aliases[alias]; ok && original != item.CallID {
			return fmt.Errorf("ambiguous tool identity")
		}
		aliases[alias] = item.CallID
	}
	for i := range target.Messages {
		var blocks []json.RawMessage
		if json.Unmarshal(target.Messages[i].Content, &blocks) != nil {
			continue
		}
		changed := false
		for j, raw := range blocks {
			var block map[string]json.RawMessage
			if json.Unmarshal(raw, &block) != nil {
				continue
			}
			var kind string
			_ = json.Unmarshal(block["type"], &kind)
			key := ""
			switch kind {
			case "tool_use":
				key = "id"
			case "tool_result":
				key = "tool_use_id"
			default:
				continue
			}
			var id string
			_ = json.Unmarshal(block[key], &id)
			original, ok := aliases[id]
			if !ok || original == id {
				continue
			}
			block[key], _ = json.Marshal(original)
			updated, err := json.Marshal(block)
			if err != nil {
				return err
			}
			blocks[j] = updated
			changed = true
		}
		if changed {
			encoded, err := json.Marshal(blocks)
			if err != nil {
				return err
			}
			target.Messages[i].Content = encoded
		}
	}
	return nil
}
