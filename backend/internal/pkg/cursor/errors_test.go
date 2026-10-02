package cursor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunPreservesSendHookCauseWithoutPrintingPrivateContext(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.New("private-token private-cookie private-body https://private.invalid/run")} {
		ctx := WithGenerationSendHook(context.Background(), func(*http.Request, func(*http.Request) (*http.Response, error)) (*http.Response, error) {
			return nil, cause
		})
		client := &Client{BaseURL: "https://private.invalid", HTTPClient: &http.Client{}}
		_, err := client.Run(ctx, Credentials{AccessToken: "private-token"}, []ChatMessage{{Role: "user", Content: "private-body"}}, "default", func(StreamEvent) error { t.Fatal("unexpected output"); return nil })
		require.ErrorIs(t, err, cause)
		require.NotContains(t, err.Error(), "private-")
		require.NotContains(t, err.Error(), "private.invalid")
	}
}

func TestRunTransportFailurePreservesTypedCauseAndSafeClassification(t *testing.T) {
	dialError := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("private address/token")}
	client := &Client{BaseURL: "https://private.invalid", HTTPClient: &http.Client{Transport: errorRoundTripper{err: dialError}}}
	_, err := client.Run(context.Background(), Credentials{}, []ChatMessage{{Role: "user", Content: "hello"}}, "default", func(StreamEvent) error { return nil })
	var network *net.OpError
	var wrappedURL *url.Error
	require.ErrorAs(t, err, &network)
	require.ErrorAs(t, err, &wrappedURL)
	require.Equal(t, "dial_failed", DiagnosticCode(err))
	require.NotContains(t, err.Error(), "private")
	require.Equal(t, "cursor Connect error: unknown", (&ConnectError{Code: "private-token", Message: "private-body"}).Error())
}

type errorRoundTripper struct{ err error }

func (transport errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, transport.err
}

func TestMCPMetadataDiagnosticSentinels(test *testing.T) {
	for _, scenario := range []struct {
		sentinel error
		code     string
	}{
		{errMalformedMCPMetadataIdentity, "malformed_mcp_metadata_identity"},
		{errMalformedMCPServerFilter, "malformed_mcp_server_filter"},
		{errMalformedMCPPrecheck, "malformed_mcp_precheck"},
		{errMCPMetadataReply, "mcp_metadata_reply_failed"},
	} {
		test.Run(scenario.code, func(test *testing.T) {
			wrapped := fmt.Errorf("private-context: %w", scenario.sentinel)
			require.ErrorIs(test, wrapped, scenario.sentinel)
			require.Equal(test, scenario.code, DiagnosticCode(scenario.sentinel))
			require.Equal(test, scenario.code, DiagnosticCode(wrapped))
			require.Equal(test, "unclassified", DiagnosticCode(errors.New(scenario.sentinel.Error())), "text alone cannot select a diagnostic")
		})
	}
}

func TestMCPMetadataReplyDiagnosticPreservesTransportPrecedence(test *testing.T) {
	for _, scenario := range []struct {
		cause error
		code  string
	}{
		{context.Canceled, "canceled"},
		{context.DeadlineExceeded, "deadline_exceeded"},
		{&net.OpError{Op: "dial", Err: errors.New("private address")}, "dial_failed"},
		{&net.OpError{Op: "read", Err: errors.New("private address")}, "read_failed"},
		{&net.OpError{Op: "write", Err: errors.New("private address")}, "write_failed"},
		{io.ErrUnexpectedEOF, "stream_truncated"},
	} {
		test.Run(scenario.code, func(test *testing.T) {
			failure := fmt.Errorf("%w: %w", errMCPMetadataReply, &url.Error{Op: "Post", URL: "https://private.invalid", Err: scenario.cause})
			require.ErrorIs(test, failure, errMCPMetadataReply)
			require.ErrorIs(test, failure, scenario.cause)
			require.Equal(test, scenario.code, DiagnosticCode(fmt.Errorf("wrapped: %w", failure)))
		})
	}
}
