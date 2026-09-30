package handler

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const qianwenImageEditFileLimit = 10 << 20
const qianwenImageEditRequestLimit = 3*qianwenImageEditFileLimit + (64 << 10)

// QianwenImageEdit handles multipart POST /v1/images/edits. Parsing is bounded
// in memory and never reads paths supplied by the client or creates temp files.
func (h *OpenAIGatewayHandler) QianwenImageEdit(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, qianwenImageEditRequestLimit)
	body, status, code, err := parseQianwenImageEdit(c.Request)
	if err != nil {
		c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "code": code, "message": err.Error()}})
		return
	}
	// Reuse admission on the translated JSON so the existing audit sees both
	// the prompt and image content before scheduling and frozen-price capture.
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Type", "application/json")
	adm, ok := h.admitQianwenStandardRequest(c, service.ContentModerationProtocolOpenAIChat)
	if !ok {
		return
	}
	result, account, ok := h.forwardQianwenStandard(c, adm, "images", service.OpenAIEndpointCapabilityChatCompletions, func(account *service.Account) (*service.OpenAIForwardResult, error) {
		return h.gatewayService.ForwardQianwenImageEdit(c.Request.Context(), c, account, adm.body)
	})
	if !ok {
		return
	}
	h.recordQianwenStandardUsage(c, adm, account, result, "image.edit")
}

func parseQianwenImageEdit(request *http.Request) ([]byte, int, string, error) {
	fail := func(err error) ([]byte, int, string, error) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, http.StatusRequestEntityTooLarge, "image_edit_too_large", fmt.Errorf("image edit request exceeds size limit")
		}
		return nil, http.StatusBadRequest, "invalid_image_edit_request", err
	}
	reader, err := request.MultipartReader()
	if err != nil {
		return fail(fmt.Errorf("image editing requires multipart/form-data"))
	}
	fields := make(map[string]string)
	var images []string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(err)
		}
		name := part.FormName()
		if name == "image" || name == "image[]" {
			if part.FileName() == "" || len(images) == 3 {
				return fail(fmt.Errorf("image editing requires one to three uploaded image files"))
			}
			data, err := io.ReadAll(io.LimitReader(part, qianwenImageEditFileLimit+1))
			if err != nil {
				return fail(err)
			}
			if len(data) > qianwenImageEditFileLimit {
				return nil, http.StatusRequestEntityTooLarge, "image_edit_too_large", fmt.Errorf("each image must be at most 10 MiB")
			}
			mime := http.DetectContentType(data)
			switch mime {
			case "image/png", "image/jpeg", "image/webp", "image/gif", "image/bmp", "image/tiff":
			default:
				return fail(fmt.Errorf("unsupported or empty image file"))
			}
			images = append(images, "data:"+mime+";base64,"+base64.StdEncoding.EncodeToString(data))
			continue
		}
		switch name {
		case "model", "prompt", "size", "n", "response_format":
		default:
			return fail(fmt.Errorf("unsupported image edit field: %s", name))
		}
		if _, exists := fields[name]; exists || part.FileName() != "" {
			return fail(fmt.Errorf("invalid or duplicate field: %s", name))
		}
		data, err := io.ReadAll(io.LimitReader(part, (16<<10)+1))
		if err != nil {
			return fail(err)
		}
		if len(data) > 16<<10 {
			return fail(fmt.Errorf("image edit field is too long"))
		}
		fields[name] = string(data)
	}
	if len(images) == 0 {
		return fail(fmt.Errorf("image file is required"))
	}
	if fields["model"] == "" {
		return fail(fmt.Errorf("model is required"))
	}
	if format := fields["response_format"]; format != "" && format != "b64_json" {
		return fail(fmt.Errorf("image editing supports response_format=b64_json"))
	}
	count := 1
	if value, present := fields["n"]; present {
		count, err = strconv.Atoi(value)
		if err != nil {
			return fail(fmt.Errorf("n must be an integer"))
		}
	}
	body, err := service.BuildQianwenImageEditBody(fields["model"], fields["prompt"], fields["size"], count, images)
	if err != nil {
		return fail(err)
	}
	return body, http.StatusOK, "", nil
}
