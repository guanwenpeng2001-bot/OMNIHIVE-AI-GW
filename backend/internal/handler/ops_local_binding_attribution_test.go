package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestOpsLocalBindingAttribution(t *testing.T) {
	for _, earlierHTTP := range []bool{false, true} {
		requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
		events := []*service.OpsUpstreamErrorEvent{}
		if earlierHTTP {
			events = append(events, &service.OpsUpstreamErrorEvent{Kind: "http_error", UpstreamStatusCode: 503})
		}
		events = append(events, &service.OpsUpstreamErrorEvent{Kind: string(service.ModelBindingRejectedReason), Message: service.ModelBindingRejectedClientMessage})
		requestContext.Set(service.OpsUpstreamErrorsKey, events)
		phase, _, owner, source := classifyOpsErrorLog(requestContext, "invalid_request_error", service.ModelBindingRejectedClientMessage, "", 422)
		require.Equal(t, "routing", phase)
		require.Equal(t, "platform", owner)
		require.Equal(t, "gateway", source)
		entry := &service.OpsInsertErrorLogInput{}
		applyOpsUpstreamFieldsFromContext(requestContext, entry)
		require.Nil(t, entry.UpstreamStatusCode)
		require.Len(t, entry.UpstreamErrors, len(events))
		applyOpsStreamErrorSnapshot(entry, service.OpsStreamError{UpstreamStatus: 503, UpstreamErrors: events})
		require.Equal(t, "routing", entry.ErrorPhase)
		require.Equal(t, "platform", entry.ErrorOwner)
		require.Equal(t, "gateway", entry.ErrorSource)
		require.Nil(t, entry.UpstreamStatusCode)
		events = append(events, &service.OpsUpstreamErrorEvent{Kind: "http_error", UpstreamStatusCode: 503})
		requestContext.Set(service.OpsUpstreamErrorsKey, events)
		phase, _, owner, source = classifyOpsErrorLog(requestContext, "api_error", "provider unavailable", "", 503)
		require.Equal(t, "upstream", phase)
		require.Equal(t, "provider", owner)
		require.Equal(t, "upstream_http", source)
	}
}
