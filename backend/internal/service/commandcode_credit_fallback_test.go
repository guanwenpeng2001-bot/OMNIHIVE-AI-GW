package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCommandCodeCreditFailoverRecognition(t *testing.T) {
	observed := []byte(`{"error":{"type":"invalid_request_error","message":"You have insufficient credits to make this request. Please purchase more credits to continue using the service."}}`)
	for _, scenario := range []struct {
		name, platform  string
		status          int
		body            []byte
		committed, want bool
	}{
		{"observed credit rejection", PlatformCommandCode, 400, observed, false, true},
		{"ordinary bad request", PlatformCommandCode, 400, []byte(`{"error":{"message":"invalid messages"}}`), false, false},
		{"echoed credit text", PlatformCommandCode, 400, []byte(`{"error":{"message":"Invalid request: You have insufficient credits to make this request. Please purchase more credits to continue using the service."}}`), false, false},
		{"unstructured credit text", PlatformCommandCode, 400, []byte(commandCodeCreditExhaustedMessage), false, false},
		{"authentication status", PlatformCommandCode, 401, observed, false, false},
		{"plan status", PlatformCommandCode, 403, observed, false, false},
		{"other provider", PlatformOpenCodeGo, 400, observed, false, false},
		{"response already committed", PlatformCommandCode, 400, observed, true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := &Account{ID: 47, Platform: scenario.platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
			requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			if scenario.committed {
				MarkResponseCommitted(requestContext)
			}
			failure := commandCodeCreditFailover(requestContext, account, scenario.status, http.Header{"X-Request-Id": []string{"synthetic-request"}}, scenario.body)
			if !scenario.want {
				require.Nil(t, failure)
				return
			}
			require.NotNil(t, failure)
			require.Equal(t, 400, failure.StatusCode)
			require.Equal(t, 400, failure.ClientStatusCode)
			require.Equal(t, observed, failure.ResponseBody)
			require.Equal(t, CommandCodeCreditExhaustedReason, failure.Reason)
			require.True(t, failure.ShouldRetryNextAccount())
			require.False(t, failure.RetryableOnSameAccount)
			require.False(t, failure.SafeToFailoverAfterWrite)
			require.False(t, failure.IsCredentialFailure())
			require.False(t, requestContext.Writer.Written())
			require.True(t, account.Schedulable)
			require.Equal(t, StatusActive, account.Status)
			events := requestContext.MustGet(OpsUpstreamErrorsKey).([]*OpsUpstreamErrorEvent)
			require.Len(t, events, 1)
			require.Equal(t, 400, events[0].UpstreamStatusCode)
			require.Equal(t, int64(47), events[0].AccountID)
		})
	}
}
