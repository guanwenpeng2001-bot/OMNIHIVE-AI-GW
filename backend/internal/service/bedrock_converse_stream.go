package service

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/tidwall/gjson"
)

// DecodeConverse shares the Invoke decoder's buffered reader, CRC table and
// header parser, but retains Converse event types instead of filtering to chunk.
func (decoder *bedrockEventStreamDecoder) DecodeConverse() (string, []byte, error) {
	prelude := make([]byte, 12)
	if _, err := io.ReadFull(decoder.reader, prelude); err != nil {
		return "", nil, err
	}
	if crc32.Checksum(prelude[:8], crc32IEEETable) != bedrockReadUint32(prelude[8:]) {
		return "", nil, fmt.Errorf("eventstream prelude CRC mismatch")
	}
	totalLength := bedrockReadUint32(prelude[:4])
	headersLength := bedrockReadUint32(prelude[4:8])
	// Bound individual frames before allocating; this adapter handles text/tools.
	if totalLength < 16 || totalLength > 16*1024*1024 || headersLength > totalLength-16 {
		return "", nil, fmt.Errorf("invalid Converse eventstream frame lengths")
	}
	frame := make([]byte, int(totalLength)-12)
	if _, err := io.ReadFull(decoder.reader, frame); err != nil {
		return "", nil, err
	}
	checksum := crc32.New(crc32IEEETable)
	_, _ = checksum.Write(prelude)
	_, _ = checksum.Write(frame[:len(frame)-4])
	if checksum.Sum32() != bedrockReadUint32(frame[len(frame)-4:]) {
		return "", nil, fmt.Errorf("eventstream message CRC mismatch")
	}
	headers := frame[:headersLength]
	payload := frame[headersLength : len(frame)-4]
	eventType := extractEventStreamHeaderValue(headers, ":event-type")
	messageType := extractEventStreamHeaderValue(headers, ":message-type")
	if messageType == "exception" || messageType == "error" || extractEventStreamHeaderValue(headers, ":exception-type") != "" {
		exceptionType := extractEventStreamHeaderValue(headers, ":exception-type")
		if exceptionType == "" {
			exceptionType = eventType
		}
		if exceptionType == "" {
			exceptionType = extractEventStreamHeaderValue(headers, ":error-code")
		}
		// Do not echo upstream bodies, which can contain submitted content.
		return "", nil, fmt.Errorf("Bedrock Converse stream exception: %s", exceptionType)
	}
	if eventType == "" || messageType != "event" || !gjson.ValidBytes(payload) {
		return "", nil, fmt.Errorf("invalid Converse event headers or JSON")
	}
	return eventType, payload, nil
}

type bedrockConverseStreamBlock struct {
	kind      string
	arguments string
}

// ConvertBedrockConverseStream emits Messages SSE through the existing gateway
// writer. It owns and closes body, including when cancellation unblocks a read.
// A clean EOF requires messageStop plus metadata, so truncated usage is not success.
func ConvertBedrockConverseStream(ctx context.Context, body io.ReadCloser, writer io.Writer, requestID, model string) (map[string]any, error) {
	defer func() { _ = body.Close() }()
	stopCancellation := context.AfterFunc(ctx, func() { _ = body.Close() })
	defer stopCancellation()
	decoder := newBedrockEventStreamDecoder(body)
	started, stopped, metadataSeen := false, false, false
	stopReason := ""
	blocks := make(map[int]*bedrockConverseStreamBlock)
	closedBlocks := make(map[int]bool)
	var usage map[string]any
	emit := func(event map[string]any) error {
		encoded, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event["type"], encoded); err != nil {
			return err
		}
		if flusher, ok := writer.(interface{ Flush() }); ok {
			flusher.Flush()
		}
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return usage, err
		}
		eventType, payload, err := decoder.DecodeConverse()
		if ctx.Err() != nil {
			return usage, ctx.Err()
		}
		if err == io.EOF {
			if !stopped || !metadataSeen {
				return usage, io.ErrUnexpectedEOF
			}
			return usage, emit(map[string]any{"type": "message_stop"})
		}
		if err != nil {
			return usage, err
		}
		event := gjson.ParseBytes(payload)
		if metadataSeen {
			return usage, fmt.Errorf("Converse event after metadata")
		}
		if eventType != "messageStart" && !started {
			return usage, fmt.Errorf("Converse event before messageStart")
		}
		switch eventType {
		case "messageStart":
			if started || event.Get("role").String() != "assistant" {
				return usage, fmt.Errorf("invalid Converse messageStart")
			}
			started = true
			err = emit(map[string]any{"type": "message_start", "message": map[string]any{"id": requestID, "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{}}})
		case "contentBlockStart", "contentBlockDelta", "contentBlockStop":
			indexValue := event.Get("contentBlockIndex")
			if stopped || indexValue.Type != gjson.Number || indexValue.Int() < 0 || float64(indexValue.Int()) != indexValue.Float() {
				return usage, fmt.Errorf("invalid Converse block index or order")
			}
			index := int(indexValue.Int())
			if closedBlocks[index] {
				return usage, fmt.Errorf("Converse block already closed")
			}
			block := blocks[index]
			switch eventType {
			case "contentBlockStart":
				tool := event.Get("start.toolUse")
				if block != nil || tool.Get("toolUseId").String() == "" || tool.Get("name").String() == "" {
					return usage, fmt.Errorf("invalid Converse tool start")
				}
				blocks[index] = &bedrockConverseStreamBlock{kind: "tool_use"}
				err = emit(map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "tool_use", "id": tool.Get("toolUseId").String(), "name": tool.Get("name").String(), "input": map[string]any{}}})
			case "contentBlockDelta":
				delta := event.Get("delta")
				if text := delta.Get("text"); text.Type == gjson.String {
					if block == nil {
						block = &bedrockConverseStreamBlock{kind: "text"}
						blocks[index] = block
						if err := emit(map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "text", "text": ""}}); err != nil {
							return usage, err
						}
					}
					if block.kind != "text" {
						return usage, fmt.Errorf("Converse block kind changed")
					}
					err = emit(map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "text_delta", "text": text.String()}})
				} else if input := delta.Get("toolUse.input"); input.Type == gjson.String && block != nil && block.kind == "tool_use" {
					if len(block.arguments)+len(input.String()) > 16*1024*1024 {
						return usage, fmt.Errorf("Converse tool arguments too large")
					}
					block.arguments += input.String()
					err = emit(map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": input.String()}})
				} else {
					return usage, fmt.Errorf("unsupported Converse content delta")
				}
			case "contentBlockStop":
				if block == nil {
					return usage, fmt.Errorf("Converse stop without content")
				}
				if block.kind == "tool_use" && (!gjson.Valid(block.arguments) || !gjson.Parse(block.arguments).IsObject()) {
					return usage, fmt.Errorf("incomplete Converse tool arguments")
				}
				delete(blocks, index)
				closedBlocks[index] = true
				err = emit(map[string]any{"type": "content_block_stop", "index": index})
			}
		case "messageStop":
			if stopped || len(blocks) != 0 {
				return usage, fmt.Errorf("invalid Converse messageStop")
			}
			stopReason, err = bedrockConverseStopReason(event.Get("stopReason").String())
			stopped = true
		case "metadata":
			if !stopped || !event.Get("usage").IsObject() {
				return usage, fmt.Errorf("invalid Converse metadata")
			}
			usage = bedrockConverseUsage(event.Get("usage"))
			metadataSeen = true
			err = emit(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": usage})
		default:
			return usage, fmt.Errorf("unsupported Converse event: %s", eventType)
		}
		if err != nil {
			return usage, err
		}
	}
}
