package cursor

import (
	"context"
	"errors"
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
