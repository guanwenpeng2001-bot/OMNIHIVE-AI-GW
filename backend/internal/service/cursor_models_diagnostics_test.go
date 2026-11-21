//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func TestCursorProbePublicDiagnostics(t *testing.T) {
	const secret = "private-token Authorization Bearer https://private.example/request"
	for _, phase := range []string{"exchange/authentication", "picker", "Run/protocol"} {
		for _, scenario := range []struct {
			name string
			err  error
			want string
		}{
			{"http", fmt.Errorf("%s: %w", secret, &cursor.HTTPError{Status: 429}), "upstream HTTP 429"},
			{"connect", fmt.Errorf("%s: %w", secret, &cursor.ConnectError{Code: "permission_denied", Message: secret}), "Connect code permission_denied"},
			{"unknown connect code", &cursor.ConnectError{Code: secret, Message: secret}, "unclassified upstream or protocol error"},
			{"unknown", errors.New(secret), "unclassified upstream or protocol error"},
			{"canceled", fmt.Errorf("%s: %w", secret, context.Canceled), "canceled"},
			{"deadline", fmt.Errorf("%s: %w", secret, context.DeadlineExceeded), "deadline exceeded"},
		} {
			t.Run(phase+"/"+scenario.name, func(t *testing.T) {
				gateway, account, _ := cursorTestGateway()
				runCalls := 0
				driver := cursorProbeDriver{}
				driver.run = func(context.Context, cursor.Credentials, []cursor.ChatMessage, string, func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
					runCalls++
					return cursor.TokenUsage{}, scenario.err
				}
				switch phase {
				case "exchange/authentication":
					gateway.sessions.exchange = func(context.Context, string) (cursor.Session, error) {
						return cursor.Session{}, scenario.err
					}
				case "picker":
					driver.pickerError = scenario.err
				}
				gateway.native = driver
				recorder := httptest.NewRecorder()
				requestContext, _ := gin.CreateTestContext(recorder)
				requestContext.Request = httptest.NewRequest("POST", "/test", nil)
				err := (&AccountTestService{cursorGatewayService: gateway}).testCursorAccountConnection(requestContext, account, "", "text")
				require.EqualError(t, err, "Cursor "+phase+" probe failed: "+scenario.want)
				require.Contains(t, recorder.Body.String(), err.Error())
				require.NotContains(t, recorder.Body.String(), "private")
				require.NotContains(t, recorder.Body.String(), "Authorization")
				require.NotContains(t, recorder.Body.String(), `"success":true`)
				if phase == "Run/protocol" {
					require.Equal(t, 1, runCalls)
				} else {
					require.Zero(t, runCalls)
				}
			})
		}
	}
}

func TestCursorProbeCompletedStreamRequiresText(t *testing.T) {
	for _, event := range []cursor.StreamEvent{{Type: "turn_ended"}, {Type: "thinking", Text: "private reasoning"}, {Type: "text"}} {
		t.Run(event.Type, func(t *testing.T) {
			gateway, account, _ := cursorTestGateway()
			gateway.native = cursorProbeDriver{cursorDriverStub: cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, _ []cursor.ChatMessage, _ string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
				return cursor.TokenUsage{}, emit(event)
			}}}
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Request = httptest.NewRequest("POST", "/test", nil)
			err := (&AccountTestService{cursorGatewayService: gateway}).testCursorAccountConnection(requestContext, account, "", "")
			require.EqualError(t, err, "Cursor Run/protocol probe failed: stream completed without text")
			require.NotContains(t, recorder.Body.String(), `"success":true`)
			require.NotContains(t, recorder.Body.String(), "private")
		})
	}
}

func TestCursorProbeFailureContextFallback(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	for _, scenario := range []struct {
		ctx  context.Context
		want string
	}{{canceled, "canceled"}, {expired, "deadline exceeded"}} {
		require.Equal(t, "Cursor Run/protocol probe failed: "+scenario.want, cursorProbeFailure(scenario.ctx, "Run/protocol", errors.New("private transport error")))
	}
}

func TestCursorRelayModelProbeHTTPDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/models", request.URL.Path)
		response.WriteHeader(http.StatusForbidden)
		_, _ = response.Write([]byte("private-token and upstream response body"))
	}))
	defer server.Close()
	gateway, account, _ := cursorTestGateway()
	gateway.relayClient = server.Client()
	account.Type = AccountTypeUpstream
	account.Credentials = map[string]any{"base_url": server.URL, "api_key": "private-key"}
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest("POST", "/test", nil)
	err := (&AccountTestService{cursorGatewayService: gateway}).testCursorAccountConnection(requestContext, account, "", "")
	require.EqualError(t, err, "Cursor relay model probe failed: upstream HTTP 403")
	require.Contains(t, recorder.Body.String(), "upstream HTTP 403")
	require.NotContains(t, recorder.Body.String(), "private")
	require.NotContains(t, recorder.Body.String(), server.URL)
}

func TestCursorProbeKnownConnectCodes(t *testing.T) {
	for _, code := range []string{"canceled", "unknown", "invalid_argument", "deadline_exceeded", "not_found",
		"already_exists", "permission_denied", "resource_exhausted", "failed_precondition", "aborted",
		"out_of_range", "unimplemented", "internal", "unavailable", "data_loss", "unauthenticated"} {
		t.Run(code, func(t *testing.T) {
			require.Equal(t, "Cursor Run/protocol probe failed: Connect code "+code,
				cursorProbeFailure(context.Background(), "Run/protocol", &cursor.ConnectError{Code: code, Message: "private-token"}))
		})
	}
}
