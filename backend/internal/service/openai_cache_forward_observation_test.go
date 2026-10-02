//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestCacheForwardHTTPObservation(t *testing.T) {
	for _, scenario := range []struct {
		name                                             string
		reject, exhausted, transport, ordinary, noFields bool
	}{
		{name: "unchanged"},
		{name: "stripped", reject: true},
		{name: "budget exhausted", reject: true, exhausted: true},
		{name: "transport failure", transport: true},
		{name: "ordinary OAuth strips options before first send", ordinary: true},
		{name: "no cache fields", noFields: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-6.1-sol","stream":true,"prompt_cache_key":"secret-session","prompt_cache_options":{"mode":"explicit"},"input":[{"role":"user","content":[{"type":"input_text","text":"secret-prompt","prompt_cache_breakpoint":{"mode":"explicit"}}]}]}`)
			if scenario.noFields {
				body = []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"secret-prompt prompt_cache_options prompt_cache_breakpoint"}`)
			}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{openAICompatSSECompletedResponse("fixture", "gpt-6.1-sol")}}
			if scenario.reject {
				upstream.responses = append([]*http.Response{
					newOpenAIRejectedFieldTestResponse(400, `{"error":{"code":"unsupported_parameter","param":"input[0].content[0].prompt_cache_breakpoint","message":"secret-upstream"}}`),
					newOpenAIRejectedFieldTestResponse(400, `{"error":{"code":"unsupported_parameter","param":"prompt_cache_options","message":"secret-upstream"}}`),
				}, upstream.responses...)
			}
			if scenario.transport {
				upstream.err = errors.New("secret-transport")
			}
			requestContext := newOpenAIRejectedFieldTestContext(body)
			if !scenario.ordinary {
				requestContext.Request.Header.Set("User-Agent", "codex_cli_rs/0.146.0")
				requestContext.Request.Header.Set("originator", "codex_cli_rs")
			}
			if scenario.exhausted {
				requestContext.Set(openAIResponsesRejectedFieldRetryBudgetContextKey, &openAIResponsesRejectedFieldRetryBudget{attempts: maxOpenAIResponsesRejectedFieldRetries})
			}
			core, logs := observer.New(zap.InfoLevel)
			requestContext.Request = requestContext.Request.WithContext(logger.IntoContext(requestContext.Request.Context(), zap.New(core).With(zap.String("request_id", "request-fixture"), zap.String("client_request_id", "client-fixture"))))
			account := newOpenAIOAuthNamespaceTestAccount()
			account.Name = "secret-account-name"
			_, err := newOpenAIRejectedFieldTestService(upstream).Forward(context.Background(), requestContext, account, body)
			if scenario.exhausted || scenario.transport {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			summaries := logs.FilterMessage("openai cache fields: upstream HTTP observation").All()
			if scenario.noFields {
				require.Empty(t, summaries)
				return
			}
			require.Len(t, summaries, 1)
			fields := summaries[0].ContextMap()
			require.Len(t, fields, 12) // Only IDs, bounded numeric facts, and presence flags.
			attempts, retries, status := 1, 0, 200
			if scenario.reject && !scenario.exhausted {
				attempts, retries = 3, 2
			}
			if scenario.exhausted {
				status = 400
			}
			if scenario.transport {
				status = 0
			}
			require.Len(t, upstream.bodies, attempts)
			require.EqualValues(t, attempts, fields["upstream_attempt_count"])
			require.EqualValues(t, attempts-1, fields["retry_count"])
			require.EqualValues(t, retries, fields["rejected_field_retry_count"])
			require.EqualValues(t, status, fields["final_upstream_status_code"])
			firstStatus := 200
			if scenario.reject {
				firstStatus = 400
			}
			if scenario.transport {
				firstStatus = 0
			}
			require.EqualValues(t, firstStatus, fields["first_upstream_status_code"])
			for _, position := range []struct {
				name  string
				index int
			}{{"first", 0}, {"final", attempts - 1}} {
				require.Equal(t, gjson.GetBytes(upstream.bodies[position.index], "prompt_cache_options").Exists(), fields[position.name+"_prompt_cache_options_present"])
				require.Equal(t, gjson.GetBytes(upstream.bodies[position.index], "input.0.content.0.prompt_cache_breakpoint").Exists(), fields[position.name+"_prompt_cache_breakpoint_present"])
			}
			retryLogs := logs.FilterMessage("openai: retrying rejected Responses field").All()
			require.Len(t, retryLogs, retries)
			for ordinal, entry := range retryLogs {
				require.Len(t, entry.ContextMap(), 6)
				require.EqualValues(t, ordinal+1, entry.ContextMap()["retry_ordinal"])
				require.EqualValues(t, 400, entry.ContextMap()["upstream_status_code"])
			}
			for _, entry := range append(retryLogs, summaries...) {
				values := entry.ContextMap()
				require.Equal(t, "request-fixture", values["request_id"])
				require.Equal(t, "client-fixture", values["client_request_id"])
				require.EqualValues(t, account.ID, values["account_id"])
				encoded, marshalErr := json.Marshal(values)
				require.NoError(t, marshalErr)
				require.NotContains(t, string(encoded), "secret-")
				require.NotContains(t, entry.Message+string(encoded), "cache_hit")
				require.NotContains(t, entry.Message+string(encoded), "generation completed")
			}
		})
	}
}

func TestCacheRejectedFieldLogSanitizesUnknownReason(t *testing.T) {
	requestContext := newOpenAIRejectedFieldTestContext(nil)
	core, logs := observer.New(zap.InfoLevel)
	requestContext.Request = requestContext.Request.WithContext(logger.IntoContext(requestContext.Request.Context(), zap.New(core)))
	logOpenAIRejectedFieldRetry(requestContext, 45, 400, 1, "secret-upstream-error")
	require.Equal(t, "rejected field", logs.All()[0].ContextMap()["reason"])
}

func TestMessagesCacheRejectedFieldContextLogging(t *testing.T) {
	body := []byte(`{"model":"gpt-6.1-sol","max_tokens":16,"stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"secret-prompt","cache_control":{"type":"ephemeral"}}]}]}`)
	account := newOpenAIRejectedFieldTestAccount()
	account.Credentials["base_url"] = "https://api.openai.com/v1"
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(400, `{"error":{"code":"unsupported_parameter","param":"input[0].content[0].prompt_cache_breakpoint","message":"secret-upstream"}}`),
		openAICompatSSECompletedResponse("fixture", "gpt-6.1-sol"),
	}}
	requestContext := newOpenAIRejectedFieldTestContext(body)
	core, logs := observer.New(zap.InfoLevel)
	requestContext.Request = requestContext.Request.WithContext(logger.IntoContext(requestContext.Request.Context(), zap.New(core).With(zap.String("request_id", "messages-request"), zap.String("client_request_id", "messages-client"))))
	_, err := newOpenAIRejectedFieldTestService(upstream).ForwardAsAnthropic(context.Background(), requestContext, account, body, "", "")
	require.NoError(t, err)
	require.Len(t, upstream.bodies, 2)
	entries := logs.FilterMessage("openai: retrying rejected Responses field").All()
	require.Len(t, entries, 1)
	require.Equal(t, map[string]any{
		"request_id": "messages-request", "client_request_id": "messages-client", "account_id": account.ID,
		"upstream_status_code": int64(400), "retry_ordinal": int64(1), "reason": "content prompt_cache_breakpoint parameter rejection",
	}, entries[0].ContextMap())
}
