//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

type cursorProbeDriver struct {
	cursorDriverStub
	pickerError error
}

func (driver cursorProbeDriver) AvailableModels(context.Context, cursor.Credentials) ([]cursor.AvailableModel, error) {
	return []cursor.AvailableModel{{ID: "live", RunSlug: "live-run"}}, driver.pickerError
}
func TestCursorAccountProbeRequiresMinimalRun(t *testing.T) {
	for _, scenario := range []string{"success", "exchange", "picker", "run", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			gateway, account, exchanges := cursorTestGateway()
			calls := 0
			var runModel string
			var runMessages []cursor.ChatMessage
			driver := cursorProbeDriver{}
			driver.run = func(_ context.Context, _ cursor.Credentials, messages []cursor.ChatMessage, model string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
				calls++
				runModel = model
				runMessages = append([]cursor.ChatMessage(nil), messages...)
				if scenario == "run" {
					return cursor.TokenUsage{}, errors.New("private-upstream-body")
				}
				if scenario != "empty" {
					require.NoError(t, emit(cursor.StreamEvent{Type: "text", Text: "OK"}))
				}
				return cursor.TokenUsage{}, nil
			}
			if scenario == "exchange" {
				gateway.sessions.exchange = func(context.Context, string) (cursor.Session, error) {
					return cursor.Session{}, errors.New("private-key")
				}
			}
			if scenario == "picker" {
				driver.pickerError = errors.New("private-session")
			}
			gateway.native = driver
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Request = httptest.NewRequest("POST", "/test", nil)
			err := (&AccountTestService{cursorGatewayService: gateway}).testCursorAccountConnection(requestContext, account)
			if scenario == "success" {
				require.NoError(t, err)
				require.Contains(t, recorder.Body.String(), `"success":true`)
				require.Equal(t, "live-run", runModel)
				require.Len(t, runMessages, 1)
				require.Equal(t, "user", runMessages[0].Role)
				require.NotEmpty(t, runMessages[0].Content)
				require.Equal(t, int32(1), exchanges.Load())
			} else {
				require.Error(t, err)
				require.NotContains(t, recorder.Body.String(), `"success":true`)
			}
			if scenario == "exchange" || scenario == "picker" {
				require.Zero(t, calls)
				require.Contains(t, recorder.Body.String(), scenario)
			} else {
				require.Equal(t, 1, calls)
			}
			require.NotContains(t, recorder.Body.String(), "private-")
			require.NotContains(t, recorder.Body.String(), "session-1")
		})
	}
}
