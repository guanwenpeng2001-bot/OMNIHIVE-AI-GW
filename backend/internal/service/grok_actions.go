package service

import (
	"encoding/json"
	"fmt"
)

// prepareGrokSpeechBody bridges the public audio/speech request to xAI's
// native TTS contract. Native /tts callers do not pass through this conversion.
// Evidence: https://docs.x.ai/developers/model-capabilities/audio/text-to-speech
// retrieved 2026-09-21; examples are documentation fixtures, not live captures.
func prepareGrokSpeechBody(body []byte) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return nil, fmt.Errorf("speech request must be a JSON object")
	}
	var input string
	if err := json.Unmarshal(payload["input"], &input); err != nil || input == "" {
		return nil, fmt.Errorf("speech input must be a non-empty string")
	}
	if _, exists := payload["text"]; exists {
		return nil, fmt.Errorf("speech input and native text cannot be combined")
	}
	payload["text"] = payload["input"]
	delete(payload, "input")
	delete(payload, "model") // xAI TTS has no model selector in its wire contract.
	if voice, exists := payload["voice"]; exists {
		var name string
		if err := json.Unmarshal(voice, &name); err != nil || name == "" {
			return nil, fmt.Errorf("speech voice must be a non-empty xAI voice ID")
		}
		if _, exists := payload["voice_id"]; exists {
			return nil, fmt.Errorf("speech voice and native voice_id cannot be combined")
		}
		payload["voice_id"] = voice
		delete(payload, "voice")
	}
	if _, exists := payload["language"]; !exists {
		payload["language"] = json.RawMessage(`"auto"`)
	}
	if format, exists := payload["response_format"]; exists {
		var codec string
		if err := json.Unmarshal(format, &codec); err != nil {
			return nil, fmt.Errorf("speech response_format must be a string")
		}
		switch codec {
		case "mp3", "wav", "pcm":
		default:
			return nil, fmt.Errorf("speech response_format %q has no faithful xAI codec", codec)
		}
		if _, exists := payload["output_format"]; exists {
			return nil, fmt.Errorf("speech response_format and native output_format cannot be combined")
		}
		payload["output_format"], _ = json.Marshal(map[string]any{"codec": codec, "sample_rate": 24000})
		delete(payload, "response_format")
	}
	if raw, exists := payload["speed"]; exists {
		var speed float64
		if err := json.Unmarshal(raw, &speed); err != nil || speed < 0.7 || speed > 1.5 {
			return nil, fmt.Errorf("xAI speech speed must be between 0.7 and 1.5")
		}
	}
	for _, field := range []string{"instructions", "stream_format"} {
		if _, exists := payload[field]; exists {
			return nil, fmt.Errorf("speech %s has no faithful xAI mapping", field)
		}
	}
	return json.Marshal(payload)
}
