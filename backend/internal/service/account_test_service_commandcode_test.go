//go:build unit

package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountTestServiceCommandCodeInference(t *testing.T) {
	for _, tc := range []struct {
		model, path string
		response    func() *http.Response
	}{
		{"", "/chat/completions", adaptiveCNChatTestResponse},
		{"claude-sonnet-5", "/messages", adaptiveCNAnthropicTestResponse},
	} {
		t.Run(tc.path, func(t *testing.T) {
			a := commandCodeTestAccount()
			svc, upstream := adaptiveCNAccountTestService(a, tc.response())
			c, rec := newTestContext()
			require.NoError(t, svc.TestAccountConnection(c, a.ID, tc.model, "hi", AccountTestModeDefault))
			require.Len(t, upstream.requests, 1)
			require.Equal(t, DefaultCommandCodeBaseURL+tc.path, upstream.requests[0].URL.String())
			require.Equal(t, "cc-test", upstream.requests[0].Header.Get("x-api-key"))
			require.Contains(t, rec.Body.String(), `"success":true`)
		})
	}
}

func TestAccountTestServiceCommandCodeNoFalseSuccess(t *testing.T) {
	for _, model := range []string{DefaultCommandCodeTestModel, "claude-sonnet-5"} {
		for _, body := range []string{`{"object":"list","data":[{"id":"public-model"}]}`, "", "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"partial\"}}\n\n"} {
			a := commandCodeTestAccount()
			svc, upstream := adaptiveCNAccountTestService(a, &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))})
			c, rec := newTestContext()
			require.Error(t, svc.TestAccountConnection(c, a.ID, model, "hi", AccountTestModeDefault))
			require.NotContains(t, rec.Body.String(), `"success":true`)
			require.NotContains(t, upstream.requests[0].URL.Path, "models")
		}
	}
}

func TestAccountTestServiceCommandCodePlanAndAuthentication(t *testing.T) {
	for _, tc := range []struct {
		model         string
		status        int
		body, message string
		authError     bool
	}{
		{"claude-sonnet-5", 403, `{"type":"error","error":{"type":"permission_error","message":"MODEL_NOT_IN_PLAN: upgrade plan"}}`, "MODEL_NOT_IN_PLAN", false},
		{DefaultCommandCodeTestModel, 403, `{"error":{"code":"upgrade_required","message":"upgrade plan"}}`, "UPGRADE_REQUIRED", false},
		{DefaultCommandCodeTestModel, 401, `{"error":{"code":"UNAUTHORIZED","message":"invalid key"}}`, "401", true},
		{"claude-sonnet-5", 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid key"}}`, "401", true},
	} {
		t.Run(tc.model+tc.message, func(t *testing.T) {
			a := commandCodeTestAccount()
			svc, _ := adaptiveCNAccountTestService(a, &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))})
			c, rec := newTestContext()
			err := svc.TestAccountConnection(c, a.ID, tc.model, "hi", AccountTestModeDefault)
			require.Error(t, err)
			require.Contains(t, rec.Body.String(), tc.message)
			require.NotContains(t, rec.Body.String(), `"success":true`)
			repo := svc.accountRepo.(*openAIAccountTestRepo)
			if !tc.authError {
				require.Empty(t, repo.setErrorMsg)
			}
		})
	}
}

func TestAccountTestServiceCommandCodeTransientRouteRetry(t *testing.T) {
	a := commandCodeTestAccount()
	svc, upstream := adaptiveCNAccountTestService(a, &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"cause":"POST is not a registered API route"}`))}, adaptiveCNChatTestResponse())
	c, _ := newTestContext()
	require.NoError(t, svc.TestAccountConnection(c, a.ID, "", "hi", AccountTestModeDefault))
	require.Len(t, upstream.requests, 2)
}
