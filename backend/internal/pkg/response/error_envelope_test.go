package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestManagementErrorEnvelope(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 423, 429, 500, 503} {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		Error(context, status, "request failed")
		var envelope map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
		require.Equal(t, status, recorder.Code)
		require.Len(t, envelope, 4)
		require.Equal(t, float64(status), envelope["code"])
		require.NotEmpty(t, envelope["reason"])
		require.Equal(t, map[string]any{}, envelope["metadata"])
	}
}

func TestManagementErrorPreservesDomainReasonAndMetadata(t *testing.T) {
	for _, reason := range []string{"INSUFFICIENT_BALANCE", "GROUP_NOT_ALLOWED", "KEY_EXISTS", "OPS_DISABLED"} {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		ErrorWithDetails(context, http.StatusForbidden, "request failed", reason, map[string]string{"scope": "group"})
		var envelope map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
		require.Equal(t, reason, envelope["reason"])
		require.Equal(t, map[string]any{"scope": "group"}, envelope["metadata"])
	}
}
