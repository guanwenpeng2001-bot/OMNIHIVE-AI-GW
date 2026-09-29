//go:build unit

package handler

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// AntigravityImages is the provider-side image entry. Route registration and the
// "antigravity.images" dispatch binding belong to the multimodal-ingress branch,
// so these tests exercise the entry's admission gates directly; the upstream
// generateContent conversion is proven end-to-end at the service layer
// (service/antigravity_actions_test.go).
//
// The admission order is parse -> composite target -> image permission -> audit ->
// billing -> account selection, so a 403 permission response is itself the proof
// that a gemini-*-image model passed the provider parse step: the OpenAI images
// parser would have rejected it with a 400 model error first.

func newAntigravityImagesHandler() *GatewayHandler {
	openAIGatewayService := service.ProvideOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, // repos, cache
		&config.Config{},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
	return &GatewayHandler{openAIGatewayService: openAIGatewayService}
}

func newAntigravityImagesRequest(path, contentType string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	if contentType != "" {
		c.Request.Header.Set("Content-Type", contentType)
	}
	return c, recorder
}

func antigravityImagesGroup(platform string, allowImageGeneration bool) *service.Group {
	return &service.Group{
		ID:                   11,
		Platform:             platform,
		Hydrated:             true,
		Status:               service.StatusActive,
		AllowImageGeneration: allowImageGeneration,
	}
}

func authenticateAntigravityImagesRequest(c *gin.Context, group *service.Group) {
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      7,
		UserID:  3,
		GroupID: &group.ID,
		Group:   group,
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 3, Concurrency: 1})
}

func TestAntigravityImagesRequiresAPIKey(t *testing.T) {
	handler := &GatewayHandler{}
	c, recorder := newAntigravityImagesRequest("/v1/images/generations", "application/json", []byte(`{"model":"gemini-2.5-flash-image","prompt":"x"}`))

	handler.AntigravityImages(c)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestAntigravityImagesRequiresUserContext(t *testing.T) {
	handler := newAntigravityImagesHandler()
	c, recorder := newAntigravityImagesRequest("/v1/images/generations", "application/json", []byte(`{"model":"gemini-2.5-flash-image","prompt":"x"}`))
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 7})

	handler.AntigravityImages(c)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestAntigravityImagesRejectsEmptyBody(t *testing.T) {
	handler := newAntigravityImagesHandler()
	c, recorder := newAntigravityImagesRequest("/v1/images/generations", "application/json", nil)
	authenticateAntigravityImagesRequest(c, antigravityImagesGroup(service.PlatformAntigravity, true))

	handler.AntigravityImages(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "empty")
}

func TestAntigravityImagesRejectsInvalidJSON(t *testing.T) {
	handler := newAntigravityImagesHandler()
	c, recorder := newAntigravityImagesRequest("/v1/images/generations", "application/json", []byte(`{"model":`))
	authenticateAntigravityImagesRequest(c, antigravityImagesGroup(service.PlatformAntigravity, true))

	handler.AntigravityImages(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

// Positive admission: a gemini-*-image model must reach the provider permission
// gate. The shared OpenAI images parser rejects every non-gpt-image model, so a
// 403 here proves the Antigravity entry no longer borrows that model gate.
func TestAntigravityImagesAcceptsGeminiImageModelThroughAdmission(t *testing.T) {
	handler := newAntigravityImagesHandler()
	c, recorder := newAntigravityImagesRequest("/v1/images/generations", "application/json", []byte(`{"model":"gemini-2.5-flash-image","prompt":"a red fox"}`))
	authenticateAntigravityImagesRequest(c, antigravityImagesGroup(service.PlatformAntigravity, false))

	handler.AntigravityImages(c)

	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "permission")
	require.NotContains(t, recorder.Body.String(), "requires an image model")
}

// Positive admission (multimodal input): the multipart image upload an OpenAI SDK
// sends for /v1/images/edits also passes the provider parse step.
func TestAntigravityImagesAcceptsMultipartEditUploadThroughAdmission(t *testing.T) {
	handler := newAntigravityImagesHandler()
	body, contentType := antigravityHandlerMultipartEditBody(t)
	c, recorder := newAntigravityImagesRequest("/v1/images/edits", contentType, body)
	authenticateAntigravityImagesRequest(c, antigravityImagesGroup(service.PlatformAntigravity, false))

	handler.AntigravityImages(c)

	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "requires an image model")
}

// Negative: an edit without any source image is refused during parsing, before
// any admission work happens.
func TestAntigravityImagesRejectsEditWithoutSourceImage(t *testing.T) {
	handler := newAntigravityImagesHandler()
	c, recorder := newAntigravityImagesRequest("/v1/images/edits", "application/json", []byte(`{"model":"gemini-2.5-flash-image","prompt":"x"}`))
	authenticateAntigravityImagesRequest(c, antigravityImagesGroup(service.PlatformAntigravity, true))

	handler.AntigravityImages(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "requires a source image")
}

// Composite groups must resolve to Antigravity; an explicit route to another
// platform is refused instead of silently selecting a foreign account.
func TestAntigravityImagesCompositeTargets(t *testing.T) {
	handler := newAntigravityImagesHandler()
	body := []byte(`{"model":"gemini-2.5-flash-image","prompt":"a red fox"}`)

	c, recorder := newAntigravityImagesRequest("/v1/images/generations", "application/json", body)
	authenticateAntigravityImagesRequest(c, antigravityImagesGroup(service.PlatformComposite, false))
	c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{
		Matched:        true,
		Source:         service.CompositeRouteSourceExplicit,
		PublicModel:    "antigravity-image-alias",
		TargetPlatform: service.PlatformGrok,
		UpstreamModel:  "grok-imagine",
	}))
	handler.AntigravityImages(c)
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "not supported by this Antigravity endpoint")

	c, recorder = newAntigravityImagesRequest("/v1/images/generations", "application/json", body)
	authenticateAntigravityImagesRequest(c, antigravityImagesGroup(service.PlatformComposite, false))
	c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{
		Matched:        true,
		Source:         service.CompositeRouteSourceExplicit,
		PublicModel:    "antigravity-image-alias",
		TargetPlatform: service.PlatformAntigravity,
		UpstreamModel:  "gemini-2.5-flash-image",
	}))
	handler.AntigravityImages(c)
	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "permission")
}

func antigravityHandlerMultipartEditBody(t *testing.T) ([]byte, string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	require.NoError(t, writer.WriteField("model", "gemini-2.5-flash-image"))
	require.NoError(t, writer.WriteField("prompt", "make it blue"))
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="image"; filename="source.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	contentType := writer.FormDataContentType()
	require.Contains(t, fmt.Sprintf("%v", contentType), "boundary")
	return buffer.Bytes(), contentType
}
