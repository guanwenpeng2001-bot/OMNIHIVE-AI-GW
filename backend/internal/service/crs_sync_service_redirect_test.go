//go:build unit

package service

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type crsRedirectTransport func(*http.Request) (*http.Response, error)

func (f crsRedirectTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCRSRejectsRedirectsWithoutMutatingClient(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308} {
		for _, target := range []string{"http://approved.test/stolen", "https://127.0.0.1/private", "https://disallowed.test/stolen"} {
			for _, login := range []bool{true, false} {
				t.Run(fmt.Sprintf("%d/%s/login=%v", status, target, login), func(t *testing.T) {
					calls, redirects := 0, 0
					client := &http.Client{Timeout: time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { redirects++; return nil }, Transport: crsRedirectTransport(func(r *http.Request) (*http.Response, error) {
						calls++
						require.Equal(t, "https://approved.test", r.URL.Scheme+"://"+r.URL.Host)
						if login {
							body, err := io.ReadAll(r.Body)
							require.NoError(t, err)
							require.Contains(t, string(body), "fixture-secret")
						} else {
							require.Equal(t, "Bearer fixture-token", r.Header.Get("Authorization"))
						}
						return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{target}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
					})}
					var err error
					if login {
						_, err = crsLogin(t.Context(), client, "https://approved.test", "fixture-user", "fixture-secret")
					} else {
						_, err = crsExportAccounts(t.Context(), client, "https://approved.test", "fixture-token")
					}
					require.ErrorContains(t, err, "redirects are not allowed")
					require.Equal(t, 1, calls)
					require.Zero(t, redirects)
					require.NoError(t, client.CheckRedirect(nil, nil))
					require.Equal(t, 1, redirects)
					require.Equal(t, time.Second, client.Timeout)
				})
			}
		}
	}
}
