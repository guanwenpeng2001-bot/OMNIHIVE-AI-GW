//go:build unit

package middleware

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUpstreamJobReadSkipsOnlyBillingEnforcement(t *testing.T) {
	require.True(t, isUpstreamJobRead("GET", "/api/v1/tasks/job-id"))
	for _, path := range []string{"/compatible-mode/v1/files/file-id", "/compatible-mode/v1/files/output-job/content", "/compatible-mode/v1/batches/job-id"} {
		require.True(t, isUpstreamJobRead("GET", path))
		require.False(t, isUpstreamJobRead("POST", path))
	}
	require.False(t, isUpstreamJobRead("GET", "/compatible-mode/v1/files"))
	require.False(t, isUpstreamJobRead("GET", "/compatible-mode/v1/files-unrelated/file"))
	require.False(t, isUpstreamJobRead("POST", "/api/v1/tasks/job-id"))
	require.False(t, isUpstreamJobRead("GET", "/api/v1/services/aigc/video-generation/video-synthesis"))
	require.False(t, isUpstreamJobRead("POST", "/api/v1/services/aigc/video-generation/video-synthesis"))
}
