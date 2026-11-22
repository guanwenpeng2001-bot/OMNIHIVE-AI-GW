package repository

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/stretchr/testify/require"
)

type pricingRedirectTransport func(*http.Request) (*http.Response, error)

func (f pricingRedirectTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPricingRemoteRejectsRedirectsWithoutMutatingClient(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308} {
		for _, target := range []string{"http://approved.test/pricing", "https://127.0.0.1/private", "https://disallowed.test/pricing"} {
			for _, hash := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/%s/hash=%v", status, target, hash), func(t *testing.T) {
					calls, redirects := 0, 0
					shared := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { redirects++; return nil }, Transport: pricingRedirectTransport(func(r *http.Request) (*http.Response, error) {
						calls++
						require.Equal(t, "https://approved.test/pricing", r.URL.String())
						return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{target}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
					})}
					client := &pricingRemoteClient{httpClient: pricingClientWithoutRedirects(shared)}
					var err error
					if hash {
						_, err = client.FetchHashText(t.Context(), "https://approved.test/pricing")
					} else {
						_, err = client.FetchPricingJSON(t.Context(), "https://approved.test/pricing")
					}
					require.ErrorContains(t, err, "redirects are not allowed")
					require.Equal(t, 1, calls)
					require.Zero(t, redirects)
					require.NoError(t, shared.CheckRedirect(nil, nil))
					require.Equal(t, 1, redirects)
				})
			}
		}
	}
}

func TestPricingRemoteConstructorClonesPoolClient(t *testing.T) {
	shared, err := httpclient.GetClient(httpclient.Options{Timeout: 30 * time.Second})
	require.NoError(t, err)
	before := shared.CheckRedirect
	remote, ok := NewPricingRemoteClient("", false).(*pricingRemoteClient)
	require.True(t, ok)
	require.NotSame(t, shared, remote.httpClient)
	require.Equal(t, fmt.Sprintf("%p", before), fmt.Sprintf("%p", shared.CheckRedirect))
	require.Error(t, remote.httpClient.CheckRedirect(nil, nil))
}
