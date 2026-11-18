package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayResponsesGeminiReachesRequestValidation(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		platform string
		target   string
		model    string
	}{
		{"gemini group", service.PlatformGemini, "", "gemini-2.5-flash"},
		{"gemini group alias", service.PlatformGemini, "", "PublicAlias"},
		{"composite detected target", service.PlatformComposite, "", "gemini-2.5-flash"},
		{"composite resolved alias", service.PlatformComposite, service.PlatformGemini, "PublicAlias"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, stream := range []string{`"invalid"`} {
				recorder := httptest.NewRecorder()
				ginContext, _ := gin.CreateTestContext(recorder)
				requestContext := context.Background()
				if scenario.target != "" {
					requestContext = service.WithResolvedTargetPlatform(requestContext, scenario.target)
				}
				ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses",
					strings.NewReader(`{"model":"`+scenario.model+`","input":"hello","stream":`+stream+`}`)).WithContext(requestContext)
				ginContext.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
					ID: 99, Group: &service.Group{Platform: scenario.platform}, User: &service.User{ID: 100},
				})
				ginContext.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100})
				// No scheduler, billing, or upstream dependencies: any attempt to
				// proceed past the ingress guard fails this test.
				gateway := &GatewayHandler{cfg: &config.Config{}}
				gateway.Responses(ginContext)
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Contains(t, recorder.Body.String(), invalidStreamFieldTypeMessage)
			}
		})
	}
}

func TestGatewayResponsesUsesResolvedPlatformRatherThanModelName(t *testing.T) {
	for _, platform := range []string{service.PlatformAnthropic, service.PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			requestContext := context.Background()
			if platform == service.PlatformComposite {
				requestContext = service.WithResolvedTargetPlatform(requestContext, service.PlatformAnthropic)
			}
			// Reaching stream validation proves that an allowed target passes the
			// platform guard even when its public alias has a Gemini prefix.
			ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses",
				strings.NewReader(`{"model":"gemini-public-alias","input":"hello","stream":"invalid"}`)).WithContext(requestContext)
			ginContext.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
				Group: &service.Group{Platform: platform},
			})
			ginContext.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100})
			gateway := &GatewayHandler{cfg: &config.Config{}}
			gateway.Responses(ginContext)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), invalidStreamFieldTypeMessage)
			require.NotContains(t, recorder.Body.String(), "Gemini does not support")
		})
	}
}
