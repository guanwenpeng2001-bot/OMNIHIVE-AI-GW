package handler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func (h *OpenAIGatewayHandler) QianwenAudioSpeech(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	// The Responses audit extractor reads the speech request's string input.
	adm, ok := h.admitQianwenStandardRequest(c, service.ContentModerationProtocolOpenAIResponses)
	if !ok {
		return
	}
	body, err := service.QianwenAudioSpeechBody(adm.body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	h.forwardQianwenStandardAudio(c, adm, body, "tts")
}

func (h *OpenAIGatewayHandler) QianwenAudioTranscriptions(c *gin.Context) {
	body, err := readQianwenAudioMultipart(c)
	if err != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.Is(err, service.ErrQianwenAudioTooLarge) || errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		h.errorResponse(c, status, "invalid_request_error", err.Error())
		return
	}
	// Reuse the standard admission/scheduling/recording path after bounded
	// multipart decoding. No multipart temp files or user-supplied paths are read.
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Type", "application/json")
	adm, ok := h.admitQianwenStandardRequest(c, service.ContentModerationProtocolOpenAIChat)
	if !ok {
		return
	}
	h.forwardQianwenStandardAudio(c, adm, body, "stt")
}

func (h *OpenAIGatewayHandler) forwardQianwenStandardAudio(c *gin.Context, adm *qianwenStandardAdmission, body []byte, mode string) {
	action := "audio.tts"
	if mode == "stt" {
		action = "audio.stt"
	}
	// Audio uses character/second pricing and its action plan, not text profit estimates.
	c.Request = c.Request.WithContext(service.WithOpenAIProfitControlSuppressed(c.Request.Context()))
	result, account, ok := h.forwardQianwenStandard(c, adm, "audio", "", func(account *service.Account) (*service.OpenAIForwardResult, error) {
		return h.gatewayService.ForwardQianwenAudioCompat(c.Request.Context(), c, account, body, mode)
	})
	if ok {
		h.recordQianwenStandardUsage(c, adm, account, result, action)
	}
}

func readQianwenAudioMultipart(c *gin.Context) ([]byte, error) {
	const envelopeAllowance = 64 << 10
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.QianwenAudioMaxUploadBytes+envelopeAllowance)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("multipart file and model are required")
	}
	fields := make(map[string]string)
	var audio []byte
	var mediaType string
	seen := make(map[string]bool)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := part.FormName()
		if seen[name] {
			_ = part.Close()
			return nil, fmt.Errorf("duplicate multipart field %s", name)
		}
		seen[name] = true
		if name == "file" {
			// Filename is metadata used only for format detection. Read the supplied
			// part bytes, never open a path named by the client.
			_, disposition, parseErr := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
			filename := disposition["filename"]
			if parseErr != nil || filename == "" {
				_ = part.Close()
				return nil, fmt.Errorf("file must be uploaded with a filename")
			}
			audio, err = io.ReadAll(io.LimitReader(part, service.QianwenAudioMaxUploadBytes+1))
			if err != nil {
				_ = part.Close()
				return nil, err
			}
			if len(audio) > service.QianwenAudioMaxUploadBytes {
				_ = part.Close()
				return nil, service.ErrQianwenAudioTooLarge
			}
			mediaType, _, err = mime.ParseMediaType(part.Header.Get("Content-Type"))
			if err != nil && part.Header.Get("Content-Type") != "" {
				_ = part.Close()
				return nil, fmt.Errorf("invalid audio content type")
			}
			if mediaType == "" || mediaType == "application/octet-stream" {
				mediaType = map[string]string{".wav": "audio/wav", ".mp3": "audio/mpeg", ".aiff": "audio/aiff", ".opus": "audio/opus", ".wma": "audio/x-ms-wma", ".aac": "audio/aac", ".ogg": "audio/ogg", ".flac": "audio/flac", ".amr": "audio/amr", ".webm": "audio/webm"}[strings.ToLower(filepath.Ext(filename))]
			}
		} else {
			if name != "model" && name != "response_format" {
				_ = part.Close()
				return nil, fmt.Errorf("unsupported transcription field %s", name)
			}
			value, readErr := io.ReadAll(io.LimitReader(part, 1025))
			if readErr != nil {
				_ = part.Close()
				return nil, readErr
			}
			if len(value) > 1024 {
				_ = part.Close()
				return nil, fmt.Errorf("multipart field too long")
			}
			fields[name] = string(value)
		}
		_ = part.Close()
	}
	if fields["response_format"] != "" && fields["response_format"] != "json" {
		return nil, fmt.Errorf("Qwen transcription supports response_format json only") //nolint:staticcheck // ST1005: preserve the existing user-visible provider error text.
	}
	return service.QianwenAudioTranscriptionBody(fields["model"], mediaType, audio)
}
