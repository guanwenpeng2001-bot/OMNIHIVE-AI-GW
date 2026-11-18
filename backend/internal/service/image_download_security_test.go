package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/stretchr/testify/require"
)

func TestSecurityBackportImageDownloadRejectsLoopback(t *testing.T) {
	var requests atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer origin.Close()
	storage := &fakeImageStorage{}
	uploader := NewImageResultUploader(storage, "images/", 0, nil)
	defer uploader.httpClient.CloseIdleConnections()
	result, err := json.Marshal(map[string]any{"data": []map[string]string{{"url": origin.URL}}})
	require.NoError(t, err)
	_, err = uploader.Rewrite(t.Context(), "test", result)
	require.Error(t, err)
	require.Zero(t, requests.Load())
	require.Empty(t, storage.saved)
}

func TestSecurityBackportImageDownloadRedirectPolicy(t *testing.T) {
	var requests atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "http://private.example/image.png", http.StatusFound)
	}))
	defer origin.Close()
	client := defaultImageDownloadHTTPClient()
	require.IsType(t, &httpclient.PublicTransport{}, client.Transport)
	base := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		require.Equal(t, "93.184.216.34:80", address)
		return (&net.Dialer{}).DialContext(ctx, "tcp", origin.Listener.Addr().String())
	}}
	client.Transport = httpclient.NewPublicTransport(base)
	defer client.CloseIdleConnections()
	ctx := httpclient.WithPublicIPLookup(t.Context(), func(_ context.Context, _, host string) ([]net.IP, error) {
		if host == "private.example" {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	uploader := NewImageResultUploader(&fakeImageStorage{}, "images/", 0, client)
	_, _, err := uploader.download(ctx, "http://public.example/image.png")
	require.ErrorContains(t, err, "not allowed")
	require.EqualValues(t, 1, requests.Load())
}

func TestSecurityBackportImageDownloadRejectsNonImage(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"internal":"synthetic"}`))
	}))
	defer origin.Close()
	// A trusted test client bypasses address validation to exercise content validation.
	uploader := NewImageResultUploader(&fakeImageStorage{}, "images/", 0, origin.Client())
	_, _, err := uploader.download(t.Context(), origin.URL)
	require.ErrorContains(t, err, "not an image")
}
