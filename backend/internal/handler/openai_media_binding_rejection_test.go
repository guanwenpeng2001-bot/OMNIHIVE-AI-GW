//go:build unit

package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIMediaBindingRejection(t *testing.T) {
	status, kind, message := openAIActionForwardFailure(fmt.Errorf("binding: %w", service.ErrAccountModelUnsupported))
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Equal(t, "model_binding_rejected", kind)
	require.Equal(t, service.ModelBindingRejectedClientMessage, message)
	status, kind, _ = openAIActionForwardFailure(io.ErrUnexpectedEOF)
	require.Equal(t, http.StatusBadGateway, status)
	require.Equal(t, "upstream_error", kind)
	status, kind, _ = openAIActionForwardFailure(service.ErrOpenAIActionCredential)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "api_error", kind)
}

func TestModelBindingRejectionOpsStatusIsNull(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestContext.Set(service.OpsUpstreamStatusCodeKey, http.StatusTooManyRequests)
	requestContext.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{
		{Kind: "http_error", UpstreamStatusCode: http.StatusTooManyRequests},
		{Kind: "model_binding_rejected", Message: service.ModelBindingRejectedClientMessage, Detail: "GetBody is nil"},
	})
	entry := &service.OpsInsertErrorLogInput{}
	applyOpsUpstreamFieldsFromContext(requestContext, entry)
	require.Nil(t, entry.UpstreamStatusCode, "local rejection must persist SQL NULL, never a synthetic 422 or stale HTTP status")
	require.Len(t, entry.UpstreamErrors, 2)
	require.Equal(t, http.StatusTooManyRequests, entry.UpstreamErrors[0].UpstreamStatusCode)
	require.Equal(t, "model_binding_rejected", entry.UpstreamErrors[1].Kind)
	require.Equal(t, "GetBody is nil", *entry.UpstreamErrorDetail)
}
