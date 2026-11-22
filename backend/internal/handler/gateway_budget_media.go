package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/tidwall/gjson"
)

func validateMediaRequestAliases(spec service.GatewayRequestSpec, body []byte, contentType ...string) error {
	route := strings.ToLower(spec.Route)
	if strings.Contains(route, "/tts") || strings.Contains(route, "/speech") {
		if _, err := service.GrokTTSCharacters(body); err != nil {
			return err
		}
	}
	if spec.Protocol == "images" || strings.Contains(route, "image") {
		aliases := body
		if len(contentType) > 0 {
			mediaType, params, _ := mime.ParseMediaType(contentType[0])
			if mediaType == "multipart/form-data" {
				fields := map[string]string{}
				reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
				for {
					part, err := reader.NextPart()
					if err != nil {
						break
					}
					if name := part.FormName(); name == "size" || name == "resolution" {
						if _, exists := fields[name]; exists {
							return fmt.Errorf("duplicate image %s", name)
						}
						value, err := io.ReadAll(io.LimitReader(part, 129))
						if err != nil || len(value) > 128 {
							return fmt.Errorf("invalid image %s", name)
						}
						fields[name] = string(value)
					}
				}
				aliases, _ = json.Marshal(fields)
			}
		}
		seen := map[string]bool{}
		duplicate := false
		gjson.ParseBytes(aliases).ForEach(func(key, value gjson.Result) bool {
			name := key.String()
			if name == "size" || name == "resolution" {
				duplicate = seen[name] || value.Type != gjson.String
				seen[name] = true
			}
			return !duplicate
		})
		if duplicate {
			return fmt.Errorf("invalid or duplicate image size or resolution")
		}
		size := strings.TrimSpace(gjson.GetBytes(aliases, "size").String())
		resolution := strings.TrimSpace(gjson.GetBytes(aliases, "resolution").String())
		if spec.Protocol == "images" && size == "" && resolution != "" {
			return fmt.Errorf("image resolution requires a matching size")
		}
		if !imageSizeAliasesAgree(size, resolution) {
			return fmt.Errorf("image size and resolution disagree")
		}
	}
	return nil
}

func requestMediaMeter(spec service.GatewayRequestSpec, body []byte, contentType ...string) *service.BudgetMeter {
	meter := &service.BudgetMeter{Count: 1, Numerator: 1, Denominator: 1}
	audio := body
	if len(contentType) > 0 {
		typ, params, _ := mime.ParseMediaType(contentType[0])
		if typ == "multipart/form-data" {
			fields := map[string]string{}
			audio = nil
			reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					return &service.BudgetMeter{}
				}
				name := part.FormName()
				if name == "file" || name == "audio" {
					if audio != nil {
						return &service.BudgetMeter{}
					}
					audio, err = io.ReadAll(part)
				} else if part.FileName() == "" {
					if _, duplicate := fields[name]; duplicate {
						return &service.BudgetMeter{}
					}
					var value []byte
					value, err = io.ReadAll(io.LimitReader(part, 65537))
					if len(value) > 65536 {
						return &service.BudgetMeter{}
					}
					fields[name] = string(value)
				}
				if err != nil {
					return &service.BudgetMeter{}
				}
			}
			body, _ = json.Marshal(fields)
		}
	}
	field := func(names ...string) string {
		for _, name := range names {
			if v := gjson.GetBytes(body, name); v.Exists() {
				return v.String()
			}
		}
		return ""
	}
	// Some adapters accept both a native and an OpenAI spelling. Take the
	// largest declared quantity if more than one spelling is present.
	upperInt := func(fallback int64, names ...string) int64 {
		var bound int64
		for _, name := range names {
			v := gjson.GetBytes(body, name)
			if !v.Exists() {
				continue
			}
			n, err := strconv.ParseInt(v.String(), 10, 64)
			if err != nil || n <= 0 {
				return 0
			}
			if n > bound {
				bound = n
			}
		}
		if bound == 0 {
			return fallback
		}
		return bound
	}
	meter.Count = upperInt(1, "n", "count", "num_images", "num_videos", "parameters.sample_count", "parameters.n")
	route := strings.ToLower(spec.Route)
	switch {
	case strings.Contains(route, "transcription") || strings.Contains(route, "translation") || strings.Contains(route, "/stt") || strings.Contains(route, "/asr"):
		meter.Unit = "second"
		if encoded := field("audio.data", "input_audio.data"); encoded != "" {
			audio, _ = base64.StdEncoding.DecodeString(encoded)
		}
		meter.Numerator, meter.Denominator = pcmWaveDuration(audio)
	case strings.Contains(route, "/tts") || strings.Contains(route, "/speech") || spec.Protocol == "audio" && field("text", "input.text", "input") != "":
		meter.Unit = "character"
		meter.Numerator = int64(utf8.RuneCountInString(field("text", "input.text", "input", "prompt")))
		if strings.Contains(route, "/tts") || strings.Contains(route, "/speech") {
			characters, err := service.GrokTTSCharacters(body)
			if err != nil {
				meter.Numerator = 0
			} else {
				meter.Numerator = characters
			}
		}
	case strings.Contains(route, "video") || strings.Contains(strings.ToLower(spec.Model), "video"):
		meter.Unit = "video_second"
		meter.Size = field("resolution", "size", "parameters.resolution")
		meter.Numerator = upperInt(int64(service.NormalizeVideoBillingDurationSecondsOrDefault(0)), "seconds", "duration", "duration_seconds", "parameters.duration")
	case geminiImageBudgetSize(spec, body) != "":
		meter.Unit = "image"
		meter.Size = geminiImageBudgetSize(spec, body)
		meter.Count = upperInt(1, "generationConfig.candidateCount", "generation_config.candidate_count")
	case spec.Protocol == "images" || strings.Contains(route, "image"):
		meter.Unit = "image"
		meter.Size = field("size", "resolution", "parameters.size")
		meter.Quality = field("quality", "parameters.quality")
		if !imageSizeAliasesAgree(field("size"), field("resolution")) {
			meter.Numerator = 0
		}
	}
	return meter
}

// imageSizeAliasesAgree rejects a request whose size and resolution spellings
// name different billing tiers; adapters forward one or the other, so the hold
// must not be priced from the spelling that is not sent.
func imageSizeAliasesAgree(size, resolution string) bool {
	sizeTier, sizeKnown := service.ClassifyImageBillingTier(size)
	resolutionTier, resolutionKnown := service.ClassifyImageBillingTier(resolution)
	if strings.TrimSpace(size) == "" || strings.TrimSpace(resolution) == "" {
		return true
	}
	return sizeKnown && resolutionKnown && sizeTier == resolutionTier ||
		strings.EqualFold(strings.TrimSpace(size), "auto") && strings.EqualFold(strings.TrimSpace(resolution), "auto")
}

func geminiImageBudgetSize(spec service.GatewayRequestSpec, body []byte) string {
	if spec.Protocol != "gemini" {
		return ""
	}
	for _, name := range []string{"generationConfig.imageConfig.imageSize", "generation_config.image_config.image_size"} {
		if value := gjson.GetBytes(body, name); value.Exists() {
			if tier, ok := service.ClassifyImageBillingTier(value.String()); ok {
				return tier
			}
			return service.ImageBillingSize4K
		}
	}
	for _, name := range []string{"generationConfig.responseModalities", "generation_config.response_modalities"} {
		for _, modality := range gjson.GetBytes(body, name).Array() {
			if strings.EqualFold(modality.String(), "IMAGE") {
				return service.ImageBillingSize4K
			}
		}
	}
	return ""
}

// A PCM WAV has an exact byte-to-frame conversion. A compressed upload or a
// remote URL needs a verified duration before it can acquire a hard-budget hold.
func pcmWaveDuration(b []byte) (int64, int64) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" || int64(binary.LittleEndian.Uint32(b[4:8])) != int64(len(b)-8) {
		return 0, 0
	}
	var data, rate int64
	for offset := 12; offset+8 <= len(b); {
		tag := string(b[offset : offset+4])
		size := int64(binary.LittleEndian.Uint32(b[offset+4 : offset+8]))
		offset += 8
		if size > int64(len(b)-offset) {
			return 0, 0
		}
		chunk := b[offset : offset+int(size)]
		switch tag {
		case "fmt ":
			if len(chunk) < 16 || rate != 0 || binary.LittleEndian.Uint16(chunk) != 1 {
				return 0, 0
			}
			channels := int64(binary.LittleEndian.Uint16(chunk[2:]))
			samples := int64(binary.LittleEndian.Uint32(chunk[4:]))
			bits := int64(binary.LittleEndian.Uint16(chunk[14:]))
			frame := int64(binary.LittleEndian.Uint16(chunk[12:]))
			if frame == 0 || frame != channels*((bits+7)/8) || samples == 0 {
				return 0, 0
			}
			rate = frame * samples
			if rate != int64(binary.LittleEndian.Uint32(chunk[8:])) {
				return 0, 0
			}
		case "data":
			data += size
		}
		offset += int(size + size%2)
	}
	return data, rate
}
