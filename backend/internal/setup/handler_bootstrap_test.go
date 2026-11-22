package setup

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSetupBootstrapIsLocalSingleUseAndRenewsWithinDeadline(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("DATA_DIR", directory)
	require.NoError(t, initializeBootstrapToken())
	tokenBytes, err := os.ReadFile(filepath.Join(directory, "setup-bootstrap-token"))
	require.NoError(t, err)
	token := strings.TrimSpace(string(tokenBytes))
	expiresAt := bootstrapState.expiresAt
	attempt := func(token string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		requestContext, _ := gin.CreateTestContext(recorder)
		requestContext.Request = httptest.NewRequest(http.MethodPost, "/setup/test-db", nil)
		requestContext.Request.Header.Set("X-Setup-Bootstrap-Token", token)
		if consumeBootstrapToken(requestContext) {
			requestContext.Status(http.StatusNoContent)
			requestContext.Writer.WriteHeaderNow()
		}
		return recorder
	}
	require.Equal(t, http.StatusUnauthorized, attempt("").Code)
	first := attempt(token)
	require.Equal(t, http.StatusNoContent, first.Code)
	next := first.Header().Get("X-Setup-Bootstrap-Token")
	require.NotEmpty(t, next)
	require.NotEqual(t, token, next)
	require.Equal(t, http.StatusUnauthorized, attempt(token).Code)
	require.True(t, bootstrapState.expiresAt.After(expiresAt))
	bootstrapState.deadline = time.Now().Add(time.Minute)
	renewed := attempt(next)
	require.Equal(t, http.StatusNoContent, renewed.Code)
	require.Equal(t, bootstrapState.deadline, bootstrapState.expiresAt)
	next = renewed.Header().Get("X-Setup-Bootstrap-Token")
	bootstrapState.expiresAt = time.Now().Add(-time.Second)
	expired := attempt(next)
	require.Equal(t, http.StatusUnauthorized, expired.Code)
	require.Contains(t, expired.Body.String(), "expired")
	require.True(t, validateHostname("192.168.1.12"))
	require.True(t, validateHostname("postgres.internal"))
}
