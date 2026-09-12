package cursor

import "fmt"

const (
	interactionText      = 1
	interactionThinking  = 4
	interactionTokens    = 8
	interactionTurnEnded = 14
	deltaText            = 1
	deltaNotice          = 2
	deltaTokens          = 1
	usageInput           = 1
	usageOutput          = 2
	usageCacheRead       = 3
	usageCacheWrite      = 4
	usageReasoning       = 5
)

type TokenUsage struct {
	InputTokens, OutputTokens, CacheReadTokens, CacheWriteTokens, ReasoningTokens int
	Estimated                                                                     bool
}
type StreamEvent struct {
	Type, Text string
	Usage      TokenUsage
}

func parseEvents(payload []byte) ([]StreamEvent, error) {
	decoded, err := fields(payload)
	if err != nil {
		return nil, err
	}
	interaction := findField(decoded, serverInteraction)
	if interaction.number == 0 {
		return nil, nil
	}
	updates, err := fields(interaction.data)
	if err != nil {
		return nil, err
	}
	var events []StreamEvent
	for _, update := range updates {
		// Extensions need not contain protobuf messages. Only decode known payloads.
		switch update.number {
		case interactionText, interactionThinking, interactionTokens, interactionTurnEnded:
		default:
			continue
		}
		if update.wire != wireBytes {
			return nil, fmt.Errorf("cursor: malformed interaction")
		}
		details, err := fields(update.data)
		if err != nil {
			return nil, err
		}
		switch update.number {
		case interactionText, interactionThinking:
			if update.number == interactionText && findField(details, deltaNotice).integer != 0 {
				continue
			}
			event := StreamEvent{Type: "text", Text: string(findField(details, deltaText).data)}
			if update.number == interactionThinking {
				event.Type = "thinking"
			}
			events = append(events, event)
		case interactionTokens:
			events = append(events, StreamEvent{Type: "token_delta", Usage: TokenUsage{OutputTokens: int(findField(details, deltaTokens).integer)}})
		case interactionTurnEnded:
			usage := TokenUsage{InputTokens: int(findField(details, usageInput).integer), OutputTokens: int(findField(details, usageOutput).integer), CacheReadTokens: int(findField(details, usageCacheRead).integer), CacheWriteTokens: int(findField(details, usageCacheWrite).integer), ReasoningTokens: int(findField(details, usageReasoning).integer)}
			usage.Estimated = len(details) == 0
			events = append(events, StreamEvent{Type: "turn_ended", Usage: usage})
		}
	}
	return events, nil
}
