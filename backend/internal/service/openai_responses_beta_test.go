//go:build unit

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesBetaNarrowForwarding(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, scenario := range []struct {
			name    string
			headers []string
			want    string
		}{
			{"supported", []string{"responses_multi_agent=v1"}, "responses_multi_agent=v1"},
			{"duplicates_and_unknown", []string{"responses=experimental, responses_multi_agent=v1", "future_feature=v2, responses_multi_agent=v1"}, "responses_multi_agent=v1"},
			{"near_match", []string{"responses_multi_agent=v10"}, ""},
			{"unknown", []string{"future_feature=v2"}, ""},
			{"absent", nil, ""},
		} {
			t.Run(accountType+"/"+scenario.name, func(t *testing.T) {
				client, _ := gin.CreateTestContext(httptest.NewRecorder())
				client.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				for _, value := range scenario.headers {
					client.Request.Header.Add("OpenAI-Beta", value)
				}
				account := &Account{Platform: PlatformOpenAI, Type: accountType, Credentials: map[string]any{"api_key": "fixture", "chatgpt_account_id": "fixture"}}
				request, err := (&OpenAIGatewayService{}).buildUpstreamRequest(client.Request.Context(), client, account, []byte(`{"model":"gpt-5.1","input":"hi"}`), "fixture", true, "", false)
				require.NoError(t, err)
				require.Equal(t, scenario.want, request.Header.Get("OpenAI-Beta"))
			})
		}
	}
}
