//go:build unit

package handler

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

var qianwenEditPNG = []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 0}

func qianwenEditMultipart(t *testing.T, fields map[string]string, images [][]byte) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		require.NoError(t, writer.WriteField(name, value))
	}
	for _, data := range images {
		part, err := writer.CreateFormFile("image[]", "input.png")
		require.NoError(t, err)
		_, err = part.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return body.Bytes(), writer.FormDataContentType()
}

func TestQianwenImageEditMultipartValidation(t *testing.T) {
	for _, test := range []struct {
		name, model, prompt, count, size string
		images                           [][]byte
		status                           int
		code                             string
	}{
		{"missing image", "qwen-image-edit-plus", "edit", "1", "", nil, 400, "invalid_image_edit_request"},
		{"missing prompt", "qwen-image-edit-plus", "", "1", "", [][]byte{qianwenEditPNG}, 400, "invalid_image_edit_request"},
		{"fractional count", "qwen-image-edit-plus", "edit", "1.5", "", [][]byte{qianwenEditPNG}, 400, "invalid_image_edit_request"},
		{"unsupported count", "qwen-image-edit", "edit", "2", "", [][]byte{qianwenEditPNG}, 400, "invalid_image_edit_request"},
		{"bad size", "qwen-image-edit-plus", "edit", "1", "0x1024", [][]byte{qianwenEditPNG}, 400, "invalid_image_edit_request"},
		{"fake image", "qwen-image-edit-plus", "edit", "1", "", [][]byte{[]byte("not an image")}, 400, "invalid_image_edit_request"},
		{"too many images", "qwen-image-edit-plus", "edit", "1", "", [][]byte{qianwenEditPNG, qianwenEditPNG, qianwenEditPNG, qianwenEditPNG}, 400, "invalid_image_edit_request"},
		{"oversize", "qwen-image-edit-plus", "edit", "1", "", [][]byte{make([]byte, qianwenImageEditFileLimit+1)}, 413, "image_edit_too_large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, contentType := qianwenEditMultipart(t, map[string]string{"model": test.model, "prompt": test.prompt, "n": test.count, "size": test.size}, test.images)
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", contentType)
			(&OpenAIGatewayHandler{}).QianwenImageEdit(c)
			require.Equal(t, test.status, response.Code, response.Body.String())
			require.Equal(t, test.code, gjson.GetBytes(response.Body.Bytes(), "error.code").String())
			_, upstreamStatus := c.Get(service.OpsUpstreamStatusCodeKey)
			require.False(t, upstreamStatus)
		})
	}
}

func TestQianwenImageEditMultipartNativeBody(t *testing.T) {
	body, contentType := qianwenEditMultipart(t, map[string]string{"model": "qwen-image-edit-plus", "prompt": "Make it blue", "size": "1024x1536", "n": "2"}, [][]byte{qianwenEditPNG, qianwenEditPNG})
	request := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	native, status, _, err := parseQianwenImageEdit(request)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, "1024*1536", gjson.GetBytes(native, "parameters.size").String())
	require.EqualValues(t, 2, gjson.GetBytes(native, "parameters.n").Int())
	require.Equal(t, "Make it blue", gjson.GetBytes(native, "input.messages.0.content.2.text").String())
	require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(qianwenEditPNG), gjson.GetBytes(native, "input.messages.0.content.0.image").String())
}

func TestQianwenStandardNativeGenerationHandler(t *testing.T) {
	upstream := &httpUpstreamRecorderStub{body: `{"output":{"choices":[{"message":{"content":[{"image":"data:image/png;base64,iVBORw0KGgoAAAAA"}]}}]},"usage":{"image_count":1,"width":1024,"height":1024}}`}
	h := newQianwenStandardTestHandler(t, upstream, []service.Account{qianwenCatalogAccount(1, "qwen-image-2.0", "chat_completions")})
	c, w := qianwenStandardContext(t, http.MethodPost, "/v1/images/generations", []byte(`{"model":"qwen-image-2.0","prompt":"a cat"}`), nil)
	h.QianwenImages(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "https://dashscope.aliyuncs.com"+service.QianwenNativeMultimodalPath, upstream.lastURL)
	require.Equal(t, "qwen-image-2.0", gjson.GetBytes(upstream.lastBody, "model").String())
	require.NotEmpty(t, gjson.GetBytes(w.Body.Bytes(), "data.0.b64_json").String())
}

func TestQianwenImageEditHandlerNativeForward(t *testing.T) {
	upstream := &httpUpstreamRecorderStub{body: `{"output":{"choices":[{"message":{"content":[{"image":"data:image/png;base64,iVBORw0KGgoAAAAA"}]}}]},"usage":{"image_count":1}}`}
	h := newQianwenStandardTestHandler(t, upstream, []service.Account{qianwenCatalogAccount(1, "qwen-image-edit-plus", "chat_completions")})
	body, contentType := qianwenEditMultipart(t, map[string]string{"model": "qwen-image-edit-plus", "prompt": "Make it blue"}, [][]byte{qianwenEditPNG})
	c, w := qianwenStandardContext(t, http.MethodPost, "/v1/images/edits", body, nil)
	c.Request.Header.Set("Content-Type", contentType)
	h.QianwenImageEdit(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "Make it blue", gjson.GetBytes(upstream.lastBody, "input.messages.0.content.1.text").String())
	require.NotEmpty(t, gjson.GetBytes(w.Body.Bytes(), "data.0.b64_json").String())
}

func TestQianwenImageEditHandlerMappedModel(t *testing.T) {
	for _, test := range []struct {
		model, mapped string
		status        int
	}{
		{"qwen-plus", "qwen-plus", 422},
		{"my-image-editor", "qwen-image-edit-plus", 200},
		{"qwen-image-edit-plus-private", "qwen-image-edit-plus-private", 200},
	} {
		t.Run(test.model, func(t *testing.T) {
			upstream := &httpUpstreamRecorderStub{body: `{"output":{"choices":[{"message":{"content":[{"image":"data:image/png;base64,iVBORw0KGgoAAAAA"}]}}]},"usage":{"image_count":1}}`}
			account := qianwenCatalogAccount(1, test.model, "chat_completions")
			account.Credentials["model_mapping"] = map[string]any{test.model: test.mapped}
			h := newQianwenStandardTestHandler(t, upstream, []service.Account{account})
			body, contentType := qianwenEditMultipart(t, map[string]string{"model": test.model, "prompt": "Make it blue"}, [][]byte{qianwenEditPNG})
			c, w := qianwenStandardContext(t, http.MethodPost, "/v1/images/edits", body, nil)
			c.Request.Header.Set("Content-Type", contentType)
			h.QianwenImageEdit(c)
			require.Equal(t, test.status, w.Code, w.Body.String())
			if test.status == 422 {
				require.Zero(t, upstream.calls)
				require.Equal(t, "unsupported_image_edit_model", gjson.GetBytes(w.Body.Bytes(), "error.code").String())
				_, hasStatus := c.Get(service.OpsUpstreamStatusCodeKey)
				require.False(t, hasStatus)
			} else {
				require.Equal(t, test.mapped, gjson.GetBytes(upstream.lastBody, "model").String())
			}
		})
	}
}
