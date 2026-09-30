package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// The ASR API limits the base64 representation to 10 MB (decimal). Reserve
// 64 bytes for the data URI prefix; the raw limit is an exact base64 block.
// https://www.alibabacloud.com/help/en/model-studio/qwen-asr-api-reference
const QianwenAudioMaxEncodedBytes = 10_000_000
const QianwenAudioMaxUploadBytes = (QianwenAudioMaxEncodedBytes - 64) / 4 * 3
const qianwenAudioMaxDownloadBytes = 32 << 20

var ErrQianwenAudioTooLarge = errors.New("audio exceeds the 10 MB base64 input limit")

func qianwenAudioMIMEAllowed(value string) bool {
	switch value {
	case "audio/wav", "audio/x-wav", "audio/mpeg", "audio/mp3", "audio/aiff", "audio/x-aiff", "audio/opus", "audio/x-ms-wma", "audio/aac", "audio/ogg", "audio/flac", "audio/x-flac", "audio/amr", "audio/webm":
		return true
	}
	return false
}

func validateQianwenAudioSource(value string) error {
	if qianwenAudioHTTPURL(value) {
		return nil
	}
	prefix, encoded, found := strings.Cut(value, ",")
	if !found || !strings.HasPrefix(prefix, "data:") || !strings.HasSuffix(prefix, ";base64") || !qianwenAudioMIMEAllowed(strings.TrimSuffix(strings.TrimPrefix(prefix, "data:"), ";base64")) {
		return fmt.Errorf("DashScope ASR requires an HTTP(S) URL or a supported base64 audio data URI")
	}
	if len(value) > QianwenAudioMaxEncodedBytes || len(encoded) > base64.StdEncoding.EncodedLen(QianwenAudioMaxUploadBytes) {
		return ErrQianwenAudioTooLarge
	}
	if encoded == "" || strings.ContainsAny(encoded, "\r\n") {
		return fmt.Errorf("invalid base64 audio data")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("invalid base64 audio data")
	}
	if len(decoded) > QianwenAudioMaxUploadBytes {
		return ErrQianwenAudioTooLarge
	}
	return nil
}

// QianwenAudioTranscriptionBody accepts uploaded bytes only, never a local path.
func QianwenAudioTranscriptionBody(model, mediaType string, audio []byte) ([]byte, error) {
	if qianwenNativeAudioMode(model) != "stt" {
		return nil, fmt.Errorf("unsupported synchronous Qwen ASR model")
	}
	if len(audio) > QianwenAudioMaxUploadBytes {
		return nil, ErrQianwenAudioTooLarge
	}
	if len(audio) == 0 || !qianwenAudioMIMEAllowed(mediaType) {
		return nil, fmt.Errorf("a non-empty supported audio file is required")
	}
	source := "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(audio)
	return json.Marshal(map[string]any{"model": model, "input": map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"audio": source}}}}}})
}

// QianwenAudioSpeechBody preserves the provider's WAV output. Unsupported
// encodings are rejected before generation instead of relabeling WAV bytes.
func QianwenAudioSpeechBody(body []byte) ([]byte, error) {
	var request struct {
		Model          string `json:"model"`
		Input          string `json:"input"`
		Voice          string `json:"voice"`
		ResponseFormat string `json:"response_format"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return nil, fmt.Errorf("invalid speech request: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("invalid speech JSON")
	}
	if qianwenNativeAudioMode(request.Model) != "tts" {
		return nil, fmt.Errorf("unsupported synchronous Qwen TTS model")
	}
	if request.ResponseFormat != "" && request.ResponseFormat != "wav" {
		return nil, fmt.Errorf("Qwen synchronous speech supports response_format wav only")
	}
	native, err := json.Marshal(map[string]any{"model": request.Model, "input": map[string]any{"text": request.Input, "voice": request.Voice}})
	if err == nil {
		err = validateQianwenNativeAudioInput(request.Model, native)
	}
	return native, err
}

// Capture only the synchronous native response. Keep the original gin context
// so scheduling/ops/usage facts from the existing adapter remain attached.
type qianwenAudioResponse struct {
	gin.ResponseWriter
	header  http.Header
	body    bytes.Buffer
	status  int
	written bool
}

func (w *qianwenAudioResponse) Header() http.Header { return w.header }
func (w *qianwenAudioResponse) WriteHeader(status int) {
	if !w.written {
		w.status = status
	}
}
func (w *qianwenAudioResponse) WriteHeaderNow() { w.written = true }
func (w *qianwenAudioResponse) Write(data []byte) (int, error) {
	w.WriteHeaderNow()
	return w.body.Write(data)
}
func (w *qianwenAudioResponse) WriteString(data string) (int, error) { return w.Write([]byte(data)) }
func (w *qianwenAudioResponse) Status() int                          { return w.status }
func (w *qianwenAudioResponse) Size() int {
	if !w.written {
		return -1
	}
	return w.body.Len()
}
func (w *qianwenAudioResponse) Written() bool { return w.written }
func (w *qianwenAudioResponse) Flush()        { w.WriteHeaderNow() }

// ForwardQianwenAudioCompat reuses native forwarding, usage observation and
// billing. After successful generation, a failed download still returns the
// billable result to the handler, while the client receives an explicit 502.
func (s *OpenAIGatewayService) ForwardQianwenAudioCompat(ctx context.Context, c *gin.Context, account *Account, body []byte, mode string) (*OpenAIForwardResult, error) {
	model := gjson.GetBytes(body, "model").String()
	if (mode != "tts" && mode != "stt") || qianwenNativeAudioMode(account.GetMappedModel(model)) != mode {
		err := fmt.Errorf("mapped model does not support this synchronous audio action")
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	original := c.Writer
	capture := &qianwenAudioResponse{ResponseWriter: original, header: make(http.Header), status: http.StatusOK}
	result, err := func() (*OpenAIForwardResult, error) {
		c.Writer = capture
		defer func() { c.Writer = original }()
		return s.ForwardQianwenNative(ctx, c, account, QianwenNativeMultimodal, body, false)
	}()
	if err != nil || result == nil {
		if capture.Written() {
			for name, values := range capture.header {
				original.Header()[name] = values
			}
			c.Data(capture.status, capture.header.Get("Content-Type"), capture.body.Bytes())
		} else {
			status := http.StatusBadGateway
			if errors.Is(err, ErrQianwenAudioTooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			writeChatCompletionsError(c, status, "upstream_error", "Qwen audio request failed")
		}
		return nil, err
	}
	if mode == "stt" {
		c.JSON(http.StatusOK, gin.H{"text": gjson.GetBytes(capture.body.Bytes(), "output.choices.0.message.content.0.text").String()})
		return result, nil
	}
	audio, err := downloadQianwenSpeech(ctx, gjson.GetBytes(capture.body.Bytes(), "output.audio.url").String())
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Qwen speech was generated but its audio download failed")
		return result, nil
	}
	c.Data(http.StatusOK, "audio/wav", audio)
	result.ClientDisconnect = ctx.Err() != nil
	return result, nil
}

func downloadQianwenSpeech(ctx context.Context, rawURL string) ([]byte, error) {
	// Official TTS responses can contain HTTP links. Upgrade them to HTTPS;
	// the shared video downloader enforces provider hosts and forbids redirects.
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid audio result URL")
	}
	if parsed.Scheme == "http" {
		parsed.Scheme = "https"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := OpenQianwenVideoContent(ctx, parsed.String())
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.ContentLength > qianwenAudioMaxDownloadBytes {
		return nil, fmt.Errorf("audio result exceeds size limit")
	}
	audio, err := io.ReadAll(io.LimitReader(response.Body, qianwenAudioMaxDownloadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(audio) > qianwenAudioMaxDownloadBytes {
		return nil, fmt.Errorf("audio result exceeds size limit")
	}
	if len(audio) < 12 || string(audio[:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
		return nil, fmt.Errorf("audio result is not WAV")
	}
	return audio, nil
}
