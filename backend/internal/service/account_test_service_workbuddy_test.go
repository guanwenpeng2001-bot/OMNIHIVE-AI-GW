//go:build unit

package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type workBuddyRedirectTransport struct {
	client   *http.Client
	disabled bool
}

func (u *workBuddyRedirectTransport) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.disabled = HTTPUpstreamRedirectsDisabled(req.Context())
	client := *u.client
	if u.disabled {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("redirect forbidden") }
	}
	return client.Do(req)
}
func (u *workBuddyRedirectTransport) DoWithTLS(req *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, id, n)
}

func TestWorkBuddyAccountChatTestRejectsRedirects(t *testing.T) {
	for _, status := range []int{302, 307, 308} {
		var reached atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
		defer target.Close()
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NotEmpty(t, r.Header.Get("X-API-Key"))
			http.Redirect(w, r, target.URL, status)
		}))
		defer origin.Close()
		a := wbAccount("cn", origin.URL, AccountTypeAPIKey)
		transport := &workBuddyRedirectTransport{client: origin.Client()}
		svc := &AccountTestService{httpUpstream: transport}
		c, _ := newTestContext()
		err := svc.testOpenAIChatCompletionsConnection(newAccountProbeContext(c), a, "auto", "hello", origin.URL, "key")
		require.Error(t, err)
		require.True(t, transport.disabled)
		require.Zero(t, reached.Load())
	}
}
