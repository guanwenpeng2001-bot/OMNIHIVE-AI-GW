// Package media describes the verified single-result media task families.
package media

import (
	"strings"
	"unicode/utf8"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

var Unsupported = &fault.Fault{Status: 422, Reason: "UNSUPPORTED_ACTION", Message: "This media action has no verified, funded endpoint."}

type Profile struct {
	Kind, Model, Action, Protocol, Route, Format, Unit string
	MaxDurationSeconds                                 int
	CancelQueued                                       bool
}

func Lookup(kind, model string) (Profile, bool) {
	switch {
	case kind == "audio" && (model == "qwen3-tts-flash" || model == "qwen3-tts-flash-2025-11-27" || model == "qwen3-tts-flash-2025-09-18"):
		return Profile{kind, model, "audio.tts", "audio", "/v1/audio/speech", "wav", "character", MaxAudioSeconds, false}, true
	case kind == "video" && model == "wan2.6-t2v":
		return Profile{kind, model, "video.generate", "media", "/v1/videos", "mp4", "second", 5, true}, true
	}
	return Profile{}, false
}

func Validate(kind, prompt, format, size string, seconds, count int) error {
	if strings.TrimSpace(prompt) == "" {
		return fault.Invalid("/input/prompt")
	}
	switch kind {
	case "audio":
		if utf8.RuneCountInString(prompt) > 600 {
			return fault.Invalid("/input/prompt")
		}
		if format != "wav" {
			return fault.Invalid("/input/format")
		}
		if seconds != 0 {
			return fault.Invalid("/input/duration_seconds")
		}
		if size != "" {
			return fault.Invalid("/input/size")
		}
	case "video":
		if utf8.RuneCountInString(prompt) > 8000 {
			return fault.Invalid("/input/prompt")
		}
		if format != "mp4" {
			return fault.Invalid("/input/format")
		}
		if seconds != 5 {
			return fault.Invalid("/input/duration_seconds")
		}
		if size != "1280x720" {
			return fault.Invalid("/input/size")
		}
	default:
		return fault.Invalid("/kind")
	}
	if count != 0 {
		return fault.Invalid("/input/count")
	}
	return nil
}

// Proof pins the installation's scoped endpoint and immutable tariff approval.
type Proof struct {
	ID, PriceVersion string
	Account          int64
}
