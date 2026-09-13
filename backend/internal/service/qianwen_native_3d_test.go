//go:build unit

package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQianwenNative3DRejectMappedModelsWithoutUpstreamOrUsage(t *testing.T) {
	for _, model := range []string{"Tripo/Tripo-H3.1", "Tripo/Tripo-P1.0", "tripo-v2", "TRIPO/future", " Tripo/Tripo-P1.0 "} {
		for _, operation := range []QianwenNativeOperation{QianwenNativeText, QianwenNativeMultimodal} {
			for _, stream := range []bool{false, true} {
				t.Run(model+string(operation), func(t *testing.T) {
					account := qianwenTestAccount()
					account.Credentials["model_mapping"] = map[string]any{"alias": model}
					upstream := &httpUpstreamRecorder{}
					s := &OpenAIGatewayService{httpUpstream: upstream}
					body := []byte(`{"model":"alias","input":{"prompt":"a cat"}}`)
					req, err := s.BuildQianwenNativeRequest(context.Background(), account, operation, body, stream)
					require.ErrorContains(t, err, "unsupported DashScope model family")
					require.Nil(t, req)
					w := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(w)
					c.Request = httptest.NewRequest("POST", QianwenNativeMultimodalPath, nil)
					result, err := s.ForwardQianwenNative(context.Background(), c, account, operation, body, stream)
					require.Error(t, err)
					require.Nil(t, result, "rejected 3D must not produce a billable result")
					require.Equal(t, 400, w.Code)
					require.Contains(t, w.Body.String(), "invalid_request_error")
					require.Empty(t, upstream.requests, "must not create an unowned job")
				})
			}
		}
	}
}

func TestQianwenNative3DClassifierDoesNotBlockExistingFamilies(t *testing.T) {
	for _, model := range []string{"qwen-plus", "qwen-image-plus", "wan2.6-t2v", "cosyvoice-v3"} {
		require.False(t, IsQianwenNative3DModel(model))
	}
	_, enabled := QianwenNativeOperationForPath(QianwenNative3DPath)
	require.False(t, enabled)
}
