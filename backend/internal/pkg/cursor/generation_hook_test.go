package cursor

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type generationHookDoer struct{ sends int }

func (d *generationHookDoer) Do(*http.Request) (*http.Response, error) {
	d.sends++
	return &http.Response{StatusCode: http.StatusUnauthorized}, nil
}

func TestGenerationHookRetryAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	denied := errors.New("budget exhausted")
	attempts := 0
	ctx = WithGenerationSendHook(ctx, func(r *http.Request, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempts == 1 {
			return nil, denied
		}
		attempts++
		return send(r)
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.invalid/run", nil)
	require.NoError(t, err)
	d := &generationHookDoer{}
	_, err = sendGeneration(d, req)
	require.NoError(t, err)
	_, err = sendGeneration(d, req)
	require.ErrorIs(t, err, denied)
	cancel()
	_, err = sendGeneration(d, req)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, d.sends)
}
