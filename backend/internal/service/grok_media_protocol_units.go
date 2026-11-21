package service

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

func GrokTTSCharacters(body []byte) (int64, error) {
	if !gjson.ValidBytes(body) {
		return 0, fmt.Errorf("invalid TTS request body")
	}
	var text string
	for _, name := range []string{"input", "text", "prompt"} {
		value := gjson.GetBytes(body, name)
		if !value.Exists() || value.Type != gjson.String {
			continue
		}
		candidate := strings.TrimSpace(value.String())
		if candidate == "" {
			continue
		}
		if text != "" && candidate != text {
			return 0, fmt.Errorf("TTS input aliases disagree")
		}
		text = candidate
	}
	return int64(utf8.RuneCountInString(text)), nil
}

func GrokImagineImageResolutionBound(body []byte) (string, error) {
	if !gjson.ValidBytes(body) {
		return "", fmt.Errorf("invalid image request body")
	}
	size := strings.TrimSpace(gjson.GetBytes(body, "size").String())
	resolution := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "resolution").String()))
	fromSize := grokImagineImageResolutionFromSize(size)
	if size == "" || strings.EqualFold(size, "auto") {
		fromSize = ""
	}
	if resolution == "auto" {
		resolution = ""
	} else if resolution != "" && grokImagineImageResolution(resolution) == "" {
		return "", fmt.Errorf("unsupported image resolution")
	}
	if fromSize != "" && resolution != "" && fromSize != resolution {
		return "", fmt.Errorf("image size and resolution disagree")
	}
	if resolution != "" {
		return resolution, nil
	}
	if fromSize != "" {
		return fromSize, nil
	}
	return "2k", nil
}
