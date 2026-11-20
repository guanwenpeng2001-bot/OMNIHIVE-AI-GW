//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTransportModelBindingRejection(t *testing.T) {
	for _, protocol := range []string{"anthropic", "openai", "gemini"} {
		for _, diagnostic := range []string{"binding mismatch", "request body cannot be replayed: GetBody is nil", "connection refused in local binding diagnostic"} {
			t.Run(protocol+"/"+diagnostic, func(t *testing.T) {
				repo := &transportTempUnschedRepoStub{}
				openAI := &OpenAIGatewayService{accountRepo: repo}
				account := &Account{ID: 42, Name: "selected", Platform: protocol}
				requestContext := newTransportErrorTestGin(t)
				// Keep a real previous attempt in history, but do not attribute its
				// HTTP status to this local failure.
				setOpsUpstreamError(requestContext, http.StatusTooManyRequests, "rate limited", "")
				appendOpsUpstreamError(requestContext, OpsUpstreamErrorEvent{UpstreamStatusCode: http.StatusTooManyRequests, Kind: "http_error"})
				bindingErr := fmt.Errorf("%s: %w", diagnostic, ErrAccountModelUnsupported)
				require.False(t, IsAttemptAdmissionError(bindingErr), "binding failures must still permit failover")
				var result error
				switch protocol {
				case "anthropic":
					result = (&GatewayService{accountRepo: repo}).handleUpstreamTransportError(context.Background(), requestContext, account, bindingErr, OpsUpstreamErrorEvent{Passthrough: true, UpstreamURL: "https://fixture.invalid/v1/messages"})
				case "openai":
					result = openAI.handleOpenAIUpstreamTransportError(context.Background(), requestContext, account, bindingErr, true)
				case "gemini":
					result = (&GeminiMessagesCompatService{accountRepo: repo}).handleUpstreamTransportError(context.Background(), requestContext, account, bindingErr)
				}
				var failure *UpstreamFailoverError
				require.ErrorAs(t, result, &failure)
				require.Equal(t, http.StatusUnprocessableEntity, failure.StatusCode)
				require.Equal(t, http.StatusUnprocessableEntity, failure.ClientStatusCode)
				require.Equal(t, ModelBindingRejectedReason, failure.Reason)
				require.Equal(t, ModelBindingRejectedClientMessage, failure.ClientMessage)
				require.True(t, failure.ShouldRetryNextAccount())
				require.False(t, failure.ShouldReportAccountScheduleFailure(), "local authorization does not measure provider health")
				require.False(t, failure.RetryableOnSameAccount)
				require.True(t, json.Valid(failure.ResponseBody))
				require.Equal(t, "model_binding_rejected", gjson.GetBytes(failure.ResponseBody, "error.type").String())
				require.Equal(t, ModelBindingRejectedClientMessage, ExtractUpstreamErrorMessage(failure.ResponseBody))
				require.NotContains(t, string(failure.ResponseBody), diagnostic)
				require.Zero(t, repo.calls)
				require.False(t, openAI.isOpenAIAccountRuntimeBlocked(account))
				require.False(t, requestContext.Writer.Written())
				require.Equal(t, 0, requestContext.GetInt(OpsUpstreamStatusCodeKey))
				events := requestContext.MustGet(OpsUpstreamErrorsKey).([]*OpsUpstreamErrorEvent)
				require.Len(t, events, 2)
				require.Equal(t, http.StatusTooManyRequests, events[0].UpstreamStatusCode)
				event := events[1]
				require.Zero(t, event.UpstreamStatusCode)
				require.Equal(t, "model_binding_rejected", event.Kind)
				require.Contains(t, event.Detail, diagnostic)
				require.Equal(t, account.ID, event.AccountID)
				if protocol != "gemini" {
					require.True(t, event.Passthrough)
				}
				if protocol == "anthropic" {
					require.Equal(t, "https://fixture.invalid/v1/messages", event.UpstreamURL)
				}
			})
		}
	}
}

func TestTransportModelBindingRejectionFromFinalBinding(t *testing.T) {
	account := catalogEvidenceAccount(42, PlatformOpenAI, "selected-model")
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "unselected-model"}))
	bindingErr := validateAccountForwardModel(ctx, &forwardCatalogRepo{current: &account}, &account, "unselected-model", "responses")
	require.ErrorIs(t, bindingErr, ErrAccountModelUnsupported)
	requestContext := newTransportErrorTestGin(t)
	var service OpenAIGatewayService
	result := service.handleOpenAIUpstreamTransportError(ctx, requestContext, &account, bindingErr, false)
	var failure *UpstreamFailoverError
	require.True(t, errors.As(result, &failure))
	require.Equal(t, http.StatusUnprocessableEntity, failure.StatusCode)
	require.True(t, failure.ShouldRetryNextAccount())
	// The classifier must not independently decide whether a selected model is
	// supported; it only handles the validator's existing sentinel.
	require.False(t, requestContext.Writer.Written())
}

func TestTransportModelBindingMissingReplayBodyDiagnostic(t *testing.T) {
	account := catalogEvidenceAccount(42, PlatformOpenAI, "selected-model")
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "selected-model"}))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.invalid/v1/responses", strings.NewReader(`{"model":"selected-model"}`))
	require.NoError(t, err)
	request.GetBody = nil
	bindingErr := validateOpenAIForwardRequest(nil, &account, request)
	require.ErrorIs(t, bindingErr, ErrAccountModelUnsupported)
	require.ErrorContains(t, bindingErr, "GetBody is nil")
	requestContext := newTransportErrorTestGin(t)
	result := (&OpenAIGatewayService{}).handleOpenAIUpstreamTransportError(ctx, requestContext, &account, bindingErr, false)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, result, &failure)
	require.Equal(t, http.StatusUnprocessableEntity, failure.StatusCode)
	require.True(t, failure.ShouldRetryNextAccount())
	require.NotContains(t, string(failure.ResponseBody), "GetBody")
	events := requestContext.MustGet(OpsUpstreamErrorsKey).([]*OpsUpstreamErrorEvent)
	require.Contains(t, events[0].Detail, "GetBody is nil")
	require.Zero(t, events[0].UpstreamStatusCode)
}

func TestTransportModelBindingCompatibilityAdapters(t *testing.T) {
	for _, protocol := range []string{"chat_completions", "responses"} {
		t.Run(protocol, func(t *testing.T) {
			body := []byte(`{"model":"claude-sonnet-4-5","input":"hello"}`)
			forward := (*GatewayService).ForwardAsResponses
			if protocol == "chat_completions" {
				body = []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)
				forward = (*GatewayService).ForwardAsChatCompletions
			}
			plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: "claude-sonnet-4-5", Protocol: protocol})
			installUnlimitedTPMFixture(plan)
			ctx := WithGatewayRequestPlan(context.Background(), plan)
			account := catalogEvidenceAccount(42, PlatformAnthropic, "claude-sonnet-4-5")
			account.Credentials["api_key"] = "fixture-only"
			account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"another-model"}}
			upstream := &queuedHTTPUpstreamStub{}
			gateway := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, tlsFPProfileService: &TLSFingerprintProfileService{}}
			requestContext := newTransportErrorTestGin(t)
			requestContext.Request = requestContext.Request.WithContext(ctx)
			result, err := forward(gateway, ctx, requestContext, &account, body, nil)
			require.Nil(t, result)
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, http.StatusUnprocessableEntity, failure.StatusCode)
			require.Equal(t, ModelBindingRejectedReason, failure.Reason)
			require.True(t, failure.ShouldRetryNextAccount())
			require.Zero(t, upstream.callCount, "binding refusal happens before upstream submission")
			require.False(t, requestContext.Writer.Written())
			// The next account has a matching binding and may complete the same request.
			account = catalogEvidenceAccount(43, PlatformAnthropic, "claude-sonnet-4-5")
			account.Credentials["api_key"] = "fixture-only"
			stream := strings.Join([]string{
				`event: message_start`,
				`data: {"type":"message_start","message":{"id":"msg_fixture","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","usage":{"input_tokens":1}}}`, "",
				`event: content_block_start`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, "",
				`event: content_block_delta`,
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`, "",
				`event: content_block_stop`,
				`data: {"type":"content_block_stop","index":0}`, "",
				`event: message_delta`,
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`, "",
				`event: message_stop`, `data: {"type":"message_stop"}`, "", "",
			}, "\n")
			upstream.responses = []*http.Response{{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}}
			result, err = forward(gateway, ctx, requestContext, &account, body, nil)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 1, upstream.callCount)
			require.Equal(t, http.StatusOK, requestContext.Writer.Status())
			// A transport error after submission remains acceptance-unknown, with
			// no replay signal that could cause duplicate provider side effects.
			upstream = &queuedHTTPUpstreamStub{errors: []error{io.ErrUnexpectedEOF}}
			gateway.httpUpstream = upstream
			requestContext = newTransportErrorTestGin(t)
			_, err = forward(gateway, context.Background(), requestContext, &account, body, nil)
			require.ErrorContains(t, err, "acceptance unknown")
			require.False(t, errors.As(err, &failure))
			require.Equal(t, 1, upstream.callCount)
			events := requestContext.MustGet(OpsUpstreamErrorsKey).([]*OpsUpstreamErrorEvent)
			require.Equal(t, "request_error", events[0].Kind)
		})
	}
}
