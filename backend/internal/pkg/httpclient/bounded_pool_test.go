package httpclient

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedPoolCapacityExpiryAndActiveStream(t *testing.T) {
	sharedClientsMu.Lock()
	for _, entry := range sharedClients {
		entry.client.CloseIdleConnections()
	}
	sharedClients = make(map[string]sharedClientEntry)
	sharedClientsMu.Unlock()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("start"))
		flusher, _ := writer.(http.Flusher)
		flusher.Flush()
		<-release
		_, _ = writer.Write([]byte("finish"))
	}))
	defer server.Close()
	defer close(release)
	client, err := GetClient(Options{})
	require.NoError(t, err)
	response, err := client.Get(server.URL)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	prefix := make([]byte, 5)
	_, err = io.ReadFull(response.Body, prefix)
	require.NoError(t, err)
	for index := 1; index <= sharedClientLimit; index++ {
		_, err = GetClient(Options{Timeout: time.Duration(index) * time.Second})
		require.NoError(t, err)
	}
	require.Len(t, sharedClients, sharedClientLimit)
	require.NotContains(t, sharedClients, buildClientKey(Options{}))
	// Expiring one cached client must remove it even when a different key is read.
	expiredKey := buildClientKey(Options{Timeout: time.Second})
	entry := sharedClients[expiredKey]
	entry.lastUsed = time.Now().Add(-sharedClientTTL)
	sharedClients[expiredKey] = entry
	_, err = GetClient(Options{Timeout: 2 * time.Second})
	require.NoError(t, err)
	require.NotContains(t, sharedClients, expiredKey)
	release <- struct{}{}
	suffix, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "finish", string(suffix))
}

func TestPublicTransportLRUAndExpiry(t *testing.T) {
	transport := NewPublicTransport(&http.Transport{})
	defer transport.CloseIdleConnections()
	oldest := transport.transportForHost("oldest")
	retained := transport.transportForHost("retained")
	transport.lastUsed["oldest"] = time.Now().Add(-time.Minute)
	for index := 0; index < 62; index++ {
		transport.transportForHost(fmt.Sprint(index))
	}
	require.Same(t, retained, transport.transportForHost("retained"))
	transport.transportForHost("overflow")
	require.Len(t, transport.transports, 64)
	require.NotContains(t, transport.transports, "oldest")
	require.Same(t, retained, transport.transports["retained"])
	transport.lastUsed["retained"] = time.Now().Add(-sharedClientTTL)
	require.NotSame(t, retained, transport.transportForHost("retained"))
	require.NotNil(t, oldest)
}

func TestSharedPoolConnectionLimitAndKeyPrivacy(t *testing.T) {
	transport, err := buildTransport(Options{})
	require.NoError(t, err)
	require.Equal(t, 100, transport.MaxConnsPerHost)
	require.Positive(t, transport.IdleConnTimeout)
	explicit, err := buildTransport(Options{MaxConnsPerHost: 7})
	require.NoError(t, err)
	require.Equal(t, 7, explicit.MaxConnsPerHost)
	key := buildClientKey(Options{ProxyURL: "http://user:password@proxy.example:8080"})
	require.Len(t, key, 64)
	require.NotContains(t, key, "password")
}

func TestSharedPoolConnectionLimitQueuesAndCancels(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusOK)
		flusher, _ := writer.(http.Flusher)
		flusher.Flush()
		<-release
	}))
	defer server.Close()
	defer close(release)
	client, err := buildClient(Options{MaxConnsPerHost: 1})
	require.NoError(t, err)
	defer client.CloseIdleConnections()
	response, err := client.Get(server.URL)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	_, err = client.Do(request)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.EqualValues(t, 1, calls.Load())
}
