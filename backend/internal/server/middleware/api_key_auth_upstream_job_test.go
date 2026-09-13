//go:build unit

package middleware

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUpstreamJobReadSkipsOnlyBillingEnforcement(t *testing.T) {
	require.True(t, isUpstreamJobRead("GET", "/api/v1/tasks/job-id"))
	require.False(t, isUpstreamJobRead("POST", "/api/v1/tasks/job-id"))
	require.False(t, isUpstreamJobRead("GET", "/api/v1/services/aigc/video-generation/video-synthesis"))
	require.False(t, isUpstreamJobRead("POST", "/api/v1/services/aigc/video-generation/video-synthesis"))
}
