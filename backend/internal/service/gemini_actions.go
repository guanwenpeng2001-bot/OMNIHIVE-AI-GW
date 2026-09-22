package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ForwardAction bridges standard JSON image and speech requests through the
// native Gemini service. Ingress retains responsibility for authorization,
// account selection and frozen pricing; this method never selects an account.
func (s *GeminiMessagesCompatService) ForwardAction(ctx context.Context, requestContext *gin.Context, account *Account, action string, body []byte) (*ForwardResult, error) {
	model, nativeBody, format, err := buildGeminiActionRequest(action, body)
	if err != nil {
		return nil, err
	}
	// Buffer only synchronous JSON. Streaming still uses ForwardNative directly.
	originalWriter := requestContext.Writer
	buffered := &geminiActionWriter{ResponseWriter: originalWriter, status: http.StatusOK}
	requestContext.Writer = buffered
	defer func() { requestContext.Writer = originalWriter }()
	result, forwardErr := s.ForwardNative(ctx, requestContext, account, model, "generateContent", false, nativeBody)
	requestContext.Writer = originalWriter
	if forwardErr != nil || buffered.status >= http.StatusBadRequest {
		// Replay only a response that was actually committed; a bare error
		// (e.g. client cancellation) must not surface as an empty 200.
		if buffered.written || buffered.status != http.StatusOK {
			originalWriter.WriteHeader(buffered.status)
			if buffered.body.Len() > 0 {
				_, _ = originalWriter.Write(buffered.body.Bytes())
			}
		}
		return result, forwardErr
	}
	payload, contentType, err := convertGeminiActionResponse(action, format, buffered.body.Bytes())
	if err != nil {
		// The upstream 200 carried no usable media; the caller treats a returned
		// error as "response already committed", so write the failure explicitly.
		requestContext.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "upstream_error", "message": "Gemini upstream returned no usable media"}})
		return result, err
	}
	originalWriter.Header().Del("Content-Length")
	// The native pass-through already committed a JSON content type to the
	// underlying writer; gin's Data does not override an existing header.
	originalWriter.Header().Del("Content-Type")
	requestContext.Data(http.StatusOK, contentType, payload)
	return result, nil
}

type geminiActionWriter struct {
	gin.ResponseWriter
	body    bytes.Buffer
	status  int
	written bool
}

func (w *geminiActionWriter) WriteHeader(status int) { w.status = status }
func (w *geminiActionWriter) WriteHeaderNow()        { w.written = true }
func (w *geminiActionWriter) Write(body []byte) (int, error) {
	w.written = true
	return w.body.Write(body)
}
func (w *geminiActionWriter) WriteString(body string) (int, error) {
	w.written = true
	return w.body.WriteString(body)
}
func (w *geminiActionWriter) Status() int { return w.status }
func (w *geminiActionWriter) Size() int {
	if !w.written {
		return -1
	}
	return w.body.Len()
}
func (w *geminiActionWriter) Written() bool { return w.written }
func (w *geminiActionWriter) Flush()        { w.written = true }

func buildGeminiActionRequest(action string, body []byte) (string, []byte, string, error) {
	var request struct {
		Model          string   `json:"model"`
		Prompt         string   `json:"prompt"`
		Input          string   `json:"input"`
		Voice          string   `json:"voice"`
		ResponseFormat string   `json:"response_format"`
		Count          int      `json:"n"`
		Image          string   `json:"image"`
		Size           string   `json:"size"`
		Speed          *float64 `json:"speed"`
	}
	if json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.Model) == "" {
		return "", nil, "", errors.New("Gemini action requires model and valid JSON")
	}
	parts := []any{}
	configuration := map[string]any{}
	switch action {
	case "image.generate", "image.edit":
		if strings.TrimSpace(request.Prompt) == "" || request.Count < 0 || request.Count > 1 || request.Size != "" {
			return "", nil, "", errors.New("Gemini images require prompt, n=1 and no pixel size override")
		}
		if request.ResponseFormat != "" && request.ResponseFormat != "b64_json" {
			return "", nil, "", errors.New("Gemini images support response_format=b64_json")
		}
		parts = append(parts, map[string]any{"text": request.Prompt})
		if action == "image.edit" {
			prefix, encoded, found := strings.Cut(request.Image, ",")
			if !found || !strings.HasPrefix(prefix, "data:image/") || !strings.HasSuffix(prefix, ";base64") {
				return "", nil, "", errors.New("Gemini image edit requires a base64 image data URL")
			}
			if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
				return "", nil, "", errors.New("invalid image base64")
			}
			parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": strings.TrimSuffix(strings.TrimPrefix(prefix, "data:"), ";base64"), "data": encoded}})
		}
		configuration["responseModalities"] = []string{"TEXT", "IMAGE"}
	case "audio.tts":
		if strings.TrimSpace(request.Input) == "" || strings.TrimSpace(request.Voice) == "" || (request.Speed != nil && *request.Speed != 1) {
			return "", nil, "", errors.New("Gemini speech requires input, native voice and speed=1")
		}
		if request.ResponseFormat == "" {
			request.ResponseFormat = "wav"
		}
		if request.ResponseFormat != "wav" && request.ResponseFormat != "pcm" {
			return "", nil, "", errors.New("Gemini speech supports wav or pcm output")
		}
		parts = append(parts, map[string]any{"text": request.Input})
		configuration["responseModalities"] = []string{"AUDIO"}
		configuration["speechConfig"] = map[string]any{"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": request.Voice}}}
	default:
		return "", nil, "", errors.New("Gemini action has no compatibility bridge")
	}
	native, err := json.Marshal(map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}, "generationConfig": configuration})
	return request.Model, native, request.ResponseFormat, err
}

// Evidence: ai.google.dev/gemini-api/docs/{image-generation,speech-generation},
// accessed 2026-09-21. Speech PCM is signed 16-bit, mono, 24 kHz. The observed
// wire MIME type carries codec parameters in varying order/case, e.g.
// "audio/L16;codec=pcm;rate=24000" and "audio/l16; rate=24000; channels=1".
func convertGeminiActionResponse(action, format string, body []byte) ([]byte, string, error) {
	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData struct {
						MIMEType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, "", err
	}
	images := []map[string]string{}
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			inline := part.InlineData
			if inline.Data == "" {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(inline.Data)
			if err != nil {
				return nil, "", errors.New("invalid Gemini media response")
			}
			mediaType := strings.ToLower(strings.ReplaceAll(inline.MIMEType, " ", ""))
			if action == "audio.tts" && strings.HasPrefix(mediaType, "audio/l16;") && strings.Contains(mediaType, "rate=24000") {
				if format == "pcm" {
					return decoded, "audio/pcm", nil
				}
				var wave bytes.Buffer
				_, _ = wave.WriteString("RIFF")
				_ = binary.Write(&wave, binary.LittleEndian, uint32(36+len(decoded)))
				_, _ = wave.WriteString("WAVEfmt ")
				for _, value := range []any{uint32(16), uint16(1), uint16(1), uint32(24000), uint32(48000), uint16(2), uint16(16)} {
					_ = binary.Write(&wave, binary.LittleEndian, value)
				}
				_, _ = wave.WriteString("data")
				_ = binary.Write(&wave, binary.LittleEndian, uint32(len(decoded)))
				_, _ = wave.Write(decoded)
				return wave.Bytes(), "audio/wav", nil
			}
			if action != "audio.tts" && strings.HasPrefix(mediaType, "image/") {
				images = append(images, map[string]string{"b64_json": inline.Data})
			}
		}
	}
	if len(images) == 0 {
		return nil, "", errors.New("Gemini returned no requested media")
	}
	payload, err := json.Marshal(map[string]any{"created": time.Now().Unix(), "data": images})
	return payload, "application/json", err
}

// prepareGeminiEmbeddingBody keeps every embedding option while binding all
// nested model references to the account's resolved model. A mixed-model batch
// cannot bypass the model authorization performed for the URL.
// Protocol evidence: https://ai.google.dev/api/embeddings (2026-09-21).
func prepareGeminiEmbeddingBody(body []byte, action, publicModel, upstreamModel string) ([]byte, error) {
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil || request == nil {
		return nil, errors.New("invalid Gemini embedding request")
	}
	bindModel := func(entry map[string]json.RawMessage) error {
		if entry == nil {
			return errors.New("invalid Gemini embedding request")
		}
		if raw, exists := entry["model"]; exists {
			var model string
			if json.Unmarshal(raw, &model) != nil || (model != publicModel && model != "models/"+publicModel) {
				return errors.New("embedding model must match URL model")
			}
		}
		entry["model"], _ = json.Marshal("models/" + upstreamModel)
		return nil
	}
	if action == "batchEmbedContents" {
		var entries []map[string]json.RawMessage
		if json.Unmarshal(request["requests"], &entries) != nil || len(entries) == 0 {
			return nil, errors.New("embedding requests must be a non-empty array")
		}
		for _, entry := range entries {
			if err := bindModel(entry); err != nil {
				return nil, err
			}
		}
		request["requests"], _ = json.Marshal(entries)
	} else if err := bindModel(request); err != nil {
		return nil, err
	}
	return json.Marshal(request)
}
