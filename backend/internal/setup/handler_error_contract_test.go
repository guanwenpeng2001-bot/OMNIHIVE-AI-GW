package setup

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSetupMalformedInputKeepsStableManagementError(t *testing.T) {
	for _, handler := range []gin.HandlerFunc{testDatabase, testRedis, install} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("POST", "/setup", strings.NewReader(`{"password":"credential-secret","host":`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		handler(ctx)
		require.Equal(t, 400, recorder.Code)
		require.Equal(t, "Invalid request", gjson.GetBytes(recorder.Body.Bytes(), "message").String())
		require.Equal(t, "BAD_REQUEST", gjson.GetBytes(recorder.Body.Bytes(), "reason").String())
		require.NotContains(t, recorder.Body.String(), "credential-secret")
	}
}
