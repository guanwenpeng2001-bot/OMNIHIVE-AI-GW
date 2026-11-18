package service

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIModelsRefreshRechecksFreshEntry(t *testing.T) {
	s := &OpenAIGatewayService{}
	req := openAIModelsRequest{url: "https://models.example/v1/models", accountID: 7, credentialAccountID: 7, headers: http.Header{"Authorization": {"Bearer synthetic-a"}}}
	key := buildOpenAIModelsCacheKey(req)
	_, state := s.openAIModelsCache.get(key, time.Now())
	require.Equal(t, openAIModelsCacheMiss, state)
	// Another refresh wins after the caller observed a miss, before DoChan.
	cached := &OpenAIModelsResponse{Body: []byte(`{"models":[]}`), upstreamETag: "old-etag"}
	s.openAIModelsCache.set(key, cached, time.Now())
	var calls atomic.Int32
	fetch := func(context.Context, string) (*OpenAIModelsResponse, error) {
		calls.Add(1)
		return &OpenAIModelsResponse{Body: []byte(`{"models":[{"slug":"other"}]}`)}, nil
	}
	result := <-s.refreshCachedOpenAIModels(key, req, fetch)
	require.NoError(t, result.Err)
	require.Same(t, cached, result.Val)
	require.Zero(t, calls.Load())
	// Reuse must not renew TTL or make the response available to another identity.
	s.openAIModelsCache.mu.Lock()
	entry := s.openAIModelsCache.entries[key]
	s.openAIModelsCache.mu.Unlock()
	result = <-s.refreshCachedOpenAIModels(key, req, fetch)
	require.NoError(t, result.Err)
	s.openAIModelsCache.mu.Lock()
	after := s.openAIModelsCache.entries[key]
	s.openAIModelsCache.mu.Unlock()
	require.Equal(t, entry.expiresAt, after.expiresAt)
	req.headers = http.Header{"Authorization": {"Bearer synthetic-b"}}
	newKey := buildOpenAIModelsCacheKey(req)
	require.NotEqual(t, key, newKey)
	result = <-s.refreshCachedOpenAIModels(newKey, req, fetch)
	require.NoError(t, result.Err)
	require.EqualValues(t, 1, calls.Load())
}

func TestOpenAIModelsRefreshStillRevalidatesStaleEntry(t *testing.T) {
	s := &OpenAIGatewayService{}
	req := openAIModelsRequest{url: "https://models.example/v1/models", accountID: 7}
	key := buildOpenAIModelsCacheKey(req)
	cached := &OpenAIModelsResponse{Body: []byte(`{"models":[]}`), upstreamETag: "etag-1"}
	s.openAIModelsCache.set(key, cached, time.Now())
	s.openAIModelsCache.mu.Lock()
	entry := s.openAIModelsCache.entries[key]
	entry.expiresAt = time.Now().Add(-time.Second)
	s.openAIModelsCache.entries[key] = entry
	s.openAIModelsCache.mu.Unlock()
	var calls atomic.Int32
	result := <-s.refreshCachedOpenAIModels(key, req, func(_ context.Context, etag string) (*OpenAIModelsResponse, error) {
		calls.Add(1)
		if etag != "etag-1" {
			t.Errorf("conditional validator = %q", etag)
		}
		return &OpenAIModelsResponse{NotModified: true}, nil
	})
	require.NoError(t, result.Err)
	require.EqualValues(t, 1, calls.Load())
	require.Same(t, cached, result.Val)
	_, state := s.openAIModelsCache.get(key, time.Now())
	require.Equal(t, openAIModelsCacheFresh, state)
}
