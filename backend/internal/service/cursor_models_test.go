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
			gateway, account, _ := cursorTestGateway()
			calls := 0
			driver := cursorProbeDriver{}
			driver.run = func(_ context.Context, _ cursor.Credentials, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
				calls++
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
