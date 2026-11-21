//go:build unit

package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCursorHeaderOverrideEligibility(t *testing.T) {
	for _, kind := range []string{AccountTypeUpstream, AccountTypeAPIKey, AccountTypeOAuth} {
		t.Run(kind, func(t *testing.T) {
			account := &Account{Platform: PlatformCursor, Type: kind, Credentials: map[string]any{
				credKeyHeaderOverrideEnabled: true, credKeyHeaderOverrides: map[string]any{"X-Custom": "configured"},
			}}
			require.Equal(t, kind == AccountTypeUpstream, account.IsHeaderOverrideEligible())
			headers := http.Header{"X-Custom": {"original"}, "x-CUSTOM": {"duplicate"}}
			account.ApplyHeaderOverrides(headers)
			if kind == AccountTypeUpstream {
				require.Equal(t, http.Header{"x-custom": {"configured"}}, headers)
			} else {
				require.Equal(t, http.Header{"X-Custom": {"original"}, "x-CUSTOM": {"duplicate"}}, headers)
			}
		})
	}
}

func TestCursorRelayAppliesHeaderOverrides(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		for _, stream := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%t/enabled=%t", protocol, stream, enabled), func(t *testing.T) {
					var received http.Header
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						received = r.Header.Clone()
						_, _ = w.Write([]byte(cursorRelayFixture(protocol, stream)))
					}))
					defer server.Close()
					account := &Account{Platform: PlatformCursor, Type: AccountTypeUpstream, Credentials: map[string]any{
						"base_url": server.URL, "api_key": "real-key", "api_protocol": protocol,
						credKeyHeaderOverrideEnabled: enabled,
						credKeyHeaderOverrides:       map[string]any{"x-CUSTOM": "configured", "USER-agent": "configured-agent", "AUTHORIZATION": "bad", "CONTENT-TYPE": "bad", "aCcEpT": "bad", "Anthropic-Version": "bad", "X-Api-Key": "bad"},
					}}
					requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
					_, err := callCursorProtocol(cursorRelayTestGateway(server), requestContext, account, cursorProtocolRequest(protocol, stream), protocol)
					require.NoError(t, err)
					require.Equal(t, "Bearer real-key", received.Get("Authorization"))
					require.Equal(t, "application/json", received.Get("Content-Type"))
					require.Empty(t, received.Get("X-Api-Key"))
					accept := "application/json"
					if stream {
						accept = "text/event-stream"
					}
					require.Equal(t, []string{accept}, received.Values("Accept"))
					if protocol == APIProtocolAnthropic {
						require.Equal(t, []string{"2023-06-01"}, received.Values("Anthropic-Version"))
					}
					if enabled {
						require.Equal(t, []string{"configured"}, received.Values("X-Custom"))
						require.Equal(t, []string{"configured-agent"}, received.Values("User-Agent"))
					} else {
						require.Empty(t, received.Get("X-Custom"))
					}
				})
			}
		}
	}
}
