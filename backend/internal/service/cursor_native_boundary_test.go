//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Exercise the actual duplex client and installed generation hook, rather than
// a driver stub that never reaches admission or the native response parser.
func TestCursorNativeGatewayAdmissionAndDuplex(t *testing.T) {
	for _, scenario := range []string{"success", "tpm", "tpm_account", "budget", "committed", "uncertain", "connect", "resource_exhausted", "unknown_code", "http", "truncated"} {
		t.Run(scenario, func(t *testing.T) {
			var sends atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				sends.Add(1)
				if request.ProtoMajor != 2 {
					t.Error("native Run must use HTTP2")
					return
				}
				payload, err := cursorMCPRead(request.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if !strings.Contains(string(payload), "composer-2.5-fast") {
					t.Error("Run must use the picker slug")
				}
				if scenario == "http" {
					writer.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				writer.Header().Set("Content-Type", "application/connect+proto")
				if scenario == "truncated" {
					writer.WriteHeader(200)
					writer.(http.Flusher).Flush()
					return
				}
				if scenario == "connect" || scenario == "resource_exhausted" || scenario == "unknown_code" {
					code := "unavailable"
					if scenario == "resource_exhausted" {
						code = scenario
					}
					if scenario == "unknown_code" {
						code = "private-secret"
					}
					frame := cursorMCPFrame([]byte(fmt.Sprintf(`{"error":{"code":%q,"message":"private-secret"}}`, code)))
					frame[0] = 2
					_, _ = writer.Write(frame)
					writer.(http.Flusher).Flush()
					return
				}
				for _, encoded := range []string{"0a090a070a0568656c6c6f", "0a067204080c1005"} {
					payload, _ := hex.DecodeString(encoded)
					_, _ = writer.Write(cursorMCPFrame(payload))
					writer.(http.Flusher).Flush()
				}
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()
			gateway, account, _ := cursorTestGateway()
			evidence := catalogEvidenceAccount(account.ID, PlatformCursor, "composer-2.5")
			account.Extra, account.Status, account.Schedulable = evidence.Extra, StatusActive, true
			gateway.native = &cursor.Client{HTTPClient: server.Client(), BaseURL: server.URL}
			gateway.catalog[cursorSessionKey(account)] = cursorCatalogEntry{models: []cursor.AvailableModel{{ID: "composer-2.5", RunSlug: "composer-2.5-fast"}}, expires: time.Now().Add(time.Hour)}
			repository := &tpmBoundaryRepository{}
			plan := boundaryTPMPlan(repository, "native-boundary")
			plan.spec.Model = "composer-2.5"
			ctx, cancel := WithAttemptBudget(WithGatewayRequestPlan(context.Background(), plan), 1, 5*time.Second)
			defer cancel()
			var expected error
			switch scenario {
			case "tpm":
				expected = ErrTPMEstimateUnavailable
				repository.rejection = expected
			case "tpm_account":
				expected = &TPMLimitExceeded{Scope: TPMScopeAccount, ResetAt: time.Now().Add(time.Minute)}
				repository.rejection = expected
			case "budget", "uncertain":
				finish, err := BeginGenerationAttempt(ctx, account.ID)
				require.NoError(t, err)
				if scenario == "uncertain" {
					finish(0, fmt.Errorf("private-secret"))
					expected = ErrAttemptOutcomeUnknown
				} else {
					finish(401, nil)
					expected = ErrAttemptBudgetExhausted
				}
			case "committed":
				CommitAttemptResponse(ctx)
				expected = ErrResponseCommitted
			}
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
			result, err := gateway.ForwardAsChatCompletions(ctx, requestContext, account, []byte(`{"model":"composer-2.5","messages":[{"role":"user","content":"hello"}]}`))
			var event *OpsUpstreamErrorEvent
			if scenario != "success" {
				events, exists := requestContext.Get(OpsUpstreamErrorsKey)
				require.True(t, exists, "native failure must retain safe attempt diagnostics")
				stored := events.([]*OpsUpstreamErrorEvent)
				require.Len(t, stored, 1)
				event = stored[0]
				encoded, marshalErr := json.Marshal(events)
				require.NoError(t, marshalErr)
				require.NotContains(t, string(encoded), "private-secret")
				require.NotContains(t, string(encoded), server.URL)
				require.Contains(t, event.Detail, `"attempts"`)
			}
			if expected != nil {
				require.ErrorIs(t, err, expected)
				require.Zero(t, sends.Load())
				require.False(t, requestContext.Writer.Written())
				require.Equal(t, "pre_send", event.Stage)
				require.Zero(t, event.UpstreamStatusCode)
				return
			}
			require.EqualValues(t, 1, sends.Load())
			require.Len(t, AttemptBudgetFromContext(ctx).Snapshot(), 1)
			if scenario == "success" {
				require.NoError(t, err)
				require.Equal(t, 12, result.Usage.InputTokens)
				require.Contains(t, recorder.Body.String(), "hello")
				return
			}
			require.Error(t, err)
			require.False(t, requestContext.Writer.Written())
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.NotContains(t, string(failover.ResponseBody), "private-secret")
			switch scenario {
			case "http":
				var cause *cursor.HTTPError
				require.ErrorAs(t, err, &cause)
				require.Equal(t, 503, event.UpstreamStatusCode)
			case "truncated":
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
				require.Equal(t, "stream_truncated", event.Reason)
			default:
				var cause *cursor.ConnectError
				require.ErrorAs(t, err, &cause)
				require.Equal(t, "connect_"+cursor.SafeConnectCode(cause.Code), event.Reason)
				require.Equal(t, 200, event.UpstreamStatusCode)
			}
		})
	}
}

func TestCursorMessagesNativeFailureShape(t *testing.T) {
	var encodedBytes atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, cursor.RunEndpoint, request.URL.Path)
		payload, err := cursorMCPRead(request.Body)
		require.NoError(t, err)
		encodedBytes.Store(int64(len(payload)))
		frame := cursorMCPFrame([]byte(`{"error":{"code":"internal","message":"invalid tool schema sk-fakeIncidentCredential123456"}}`))
		frame[0] = 2
		_, _ = writer.Write(frame)
		flusher, ok := writer.(http.Flusher)
		require.True(t, ok)
		flusher.Flush()
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	gateway, account, _ := cursorTestGateway()
	gateway.native = &cursor.Client{HTTPClient: server.Client(), BaseURL: server.URL}
	gateway.catalog[cursorSessionKey(account)] = cursorCatalogEntry{models: []cursor.AvailableModel{{ID: "composer-2.5", RunSlug: "composer-2.5-fast"}}, expires: time.Now().Add(time.Hour)}
	requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestContext.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	_, err := gateway.ForwardAsAnthropic(context.Background(), requestContext, account, []byte(`{"model":"composer-2.5","max_tokens":128,"system":"PRIVATE_STATIC","messages":[{"role":"user","content":"PRIVATE_USER"},{"role":"system","content":[{"type":"text","text":"PRIVATE_DYNAMIC"}]}],"tools":[{"name":"lookup","description":"PRIVATE_TOOL","input_schema":{"type":"object"}}]}`))
	require.Error(t, err)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, http.StatusBadGateway, failover.StatusCode, "heuristic must not change internal status")
	raw, exists := requestContext.Get(OpsUpstreamErrorsKey)
	require.True(t, exists)
	storedEvents, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	entry := &OpsInsertErrorLogInput{UpstreamErrors: storedEvents}
	require.NoError(t, SanitizeOpsUpstreamErrorsForQueue(entry))
	for _, private := range []string{"PRIVATE_", "sk-fakeIncidentCredential123456", server.URL} {
		require.NotContains(t, *entry.UpstreamErrorsJSON, private)
	}
	events, err := ParseOpsUpstreamErrors(*entry.UpstreamErrorsJSON)
	require.NoError(t, err)
	require.Len(t, events, 1)
	event := events[0]
	require.Equal(t, "tool_or_schema", event.ConnectMessageClass)
	require.True(t, *event.CredentialLike)
	require.Equal(t, &cursor.RunDiagnostics{Model: "composer-2.5-fast", ToolCount: 1, InstructionPresent: true, MessageCount: 3, EncodedBytes: int(encodedBytes.Load())}, event.CursorRun)
	require.Positive(t, event.CursorRun.EncodedBytes)
}

type cursorBoundaryTransport struct {
	sends     int
	operation string
}

func (transport *cursorBoundaryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.sends++
	if transport.operation != "dial" || transport.sends > 1 {
		if _, err := cursorMCPRead(request.Body); err != nil {
			return nil, err
		}
	}
	if transport.sends == 1 {
		return nil, &net.OpError{Op: transport.operation, Net: "tcp", Err: fmt.Errorf("private transport context")}
	}
	var body []byte
	for _, encoded := range []string{"0a090a070a0568656c6c6f", "0a067204080c1005"} {
		payload, _ := hex.DecodeString(encoded)
		body = append(body, cursorMCPFrame(payload)...)
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func TestCursorNativeTransportRetainsAttemptSafety(t *testing.T) {
	for _, operation := range []string{"dial", "read"} {
		t.Run(operation, func(t *testing.T) {
			gateway, account, _ := cursorTestGateway()
			evidence := catalogEvidenceAccount(account.ID, PlatformCursor, "composer-2.5")
			account.Extra, account.Status, account.Schedulable = evidence.Extra, StatusActive, true
			transport := &cursorBoundaryTransport{operation: operation}
			gateway.native = &cursor.Client{BaseURL: "https://private.invalid", HTTPClient: &http.Client{Transport: transport}}
			plan := boundaryTPMPlan(&tpmBoundaryRepository{}, "transport-boundary")
			ctx, cancel := WithAttemptBudget(WithGatewayRequestPlan(context.Background(), plan), 2, 5*time.Second)
			defer cancel()
			forward := func() error {
				gateway.catalog[cursorSessionKey(account)] = cursorCatalogEntry{models: []cursor.AvailableModel{{ID: "composer-2.5", RunSlug: "composer-2.5-fast"}}, expires: time.Now().Add(time.Hour)}
				requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
				requestContext.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
				_, err := gateway.ForwardAsChatCompletions(ctx, requestContext, account, []byte(`{"model":"composer-2.5","messages":[{"role":"user","content":"hello"}]}`))
				if err != nil {
					events, exists := requestContext.Get(OpsUpstreamErrorsKey)
					require.True(t, exists)
					encoded, marshalErr := json.Marshal(events)
					require.NoError(t, marshalErr)
					require.NotContains(t, string(encoded), "private")
				}
				return err
			}
			first := forward()
			var cause *net.OpError
			require.ErrorAs(t, first, &cause)
			require.Equal(t, operation, cause.Op)
			account.ID++
			second := forward()
			records := AttemptBudgetFromContext(ctx).Snapshot()
			if operation == "read" {
				require.ErrorIs(t, second, ErrAttemptOutcomeUnknown)
				require.Equal(t, 1, transport.sends, "uncertain submission cannot regenerate on another account")
				require.Equal(t, "unknown", records[0].Outcome)
			} else {
				require.NoError(t, second)
				require.Equal(t, 2, transport.sends)
				require.Equal(t, "not_sent", records[0].Outcome)
				require.Equal(t, "account_switch", records[1].Reason)
			}
		})
	}
}
