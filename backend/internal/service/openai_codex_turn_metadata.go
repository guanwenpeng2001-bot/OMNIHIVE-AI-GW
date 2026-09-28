package service

import (
	"encoding/json"
	"unicode/utf16"
)

// Turn metadata is also used as an HTTP header, including when carried in WS JSON.
func marshalCodexTurnMetadata(metadata map[string]any) ([]byte, error) {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	for index, currentByte := range raw {
		if currentByte < 0x7f {
			continue
		}
		out := make([]byte, 0, len(raw)+16)
		out = append(out, raw[:index]...)
		appendEscape := func(codePoint rune) {
			const hex = "0123456789abcdef"
			out = append(out, '\\', 'u', hex[codePoint>>12&15], hex[codePoint>>8&15], hex[codePoint>>4&15], hex[codePoint&15])
		}
		for _, codePoint := range string(raw[index:]) {
			switch {
			case codePoint < 0x7f:
				out = append(out, byte(codePoint))
			case codePoint <= 0xffff:
				appendEscape(codePoint)
			default:
				high, low := utf16.EncodeRune(codePoint)
				appendEscape(high)
				appendEscape(low)
			}
		}
		return out, nil
	}
	return raw, nil
}
