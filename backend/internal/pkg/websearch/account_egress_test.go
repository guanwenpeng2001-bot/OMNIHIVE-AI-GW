package websearch

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestAccountEgressOverridesProviderProxyIncludingHostDefault(t *testing.T) {
	var direct, providerProxy, accountProxy atomic.Int32
	serve := func(count *atomic.Int32) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			count.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"web":{"results":[{"url":"https://fixture.invalid","title":"fixture"}]}}`))
		}
	}
	origin := httptest.NewServer(serve(&direct))
	defer origin.Close()
	configured := httptest.NewServer(serve(&providerProxy))
	defer configured.Close()
	selected := httptest.NewServer(serve(&accountProxy))
	defer selected.Close()
	original := *braveSearchURL
	endpoint, _ := url.Parse(origin.URL)
	*braveSearchURL = *endpoint
	defer func() { *braveSearchURL = original }()
	mgr := NewManager([]ProviderConfig{{Type: ProviderTypeBrave, APIKey: "fixture", ProxyID: 7, ProxyURL: configured.URL}}, nil)
	_, _, err := mgr.SearchWithBestProvider(context.Background(), SearchRequest{Query: "fixture", OverrideProxy: true})
	require.NoError(t, err)
	require.EqualValues(t, 1, direct.Load())
	require.Zero(t, providerProxy.Load())
	_, _, err = mgr.SearchWithBestProvider(context.Background(), SearchRequest{Query: "fixture", ProxyURL: selected.URL, OverrideProxy: true})
	require.NoError(t, err)
	require.EqualValues(t, 1, accountProxy.Load())
	require.EqualValues(t, 1, direct.Load())
	require.Zero(t, providerProxy.Load())
}
