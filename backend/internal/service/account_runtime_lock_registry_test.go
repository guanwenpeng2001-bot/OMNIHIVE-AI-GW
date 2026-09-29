package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type openAILockWaitContendedCache struct{ GeminiTokenCache }

func (*openAILockWaitContendedCache) GetAccessToken(context.Context, string) (string, error) {
	return "", nil
}
func (*openAILockWaitContendedCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	return nil
}
func (*openAILockWaitContendedCache) AcquireRefreshLock(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}

func TestOpenAILockWaitRejectsRequestBindingReplacement(review *testing.T) {
	for _, dimension := range []string{"endpoint", "same_proxy_id", "headers", "secret_revision", "owner", "renewal"} {
		review.Run(dimension, func(review *testing.T) {
			proxyID := int64(7)
			selected := &Account{ID: 98701, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
				ProxyID: &proxyID, Proxy: &Proxy{Protocol: "http", Host: "old.proxy", Port: 8080}, Extra: map[string]any{},
				Credentials: map[string]any{"access_token": "old-token", "refresh_token": "old-refresh", "expires_at": time.Now().Add(-time.Minute).Format(time.RFC3339), "base_url": "https://old-relay.example/v1"}}
			latest := snapshotOAuthRefreshAccount(selected)
			latest.Credentials["access_token"] = "replacement-owner-token"
			latest.Credentials["refresh_token"] = "new-refresh"
			latest.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
			switch dimension {
			case "endpoint":
				latest.Credentials["base_url"] = "https://new-relay.example/v1"
			case "same_proxy_id":
				latest.Proxy.Host = "new.proxy"
			case "headers":
				latest.Credentials["header_overrides"] = map[string]any{"X-Owner": "new-owner"}
			case "secret_revision":
				latest.Extra["secret_revision"] = 2
			case "owner":
				latest.Credentials["account_id"] = "new-owner"
			}
			repo := &runtimeSnapshotTestRepo{current: latest}
			cache := &openAILockWaitContendedCache{}
			provider := NewOpenAITokenProvider(repo, cache, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &openAILockWaitExecutor{})
			token, err := provider.GetAccessToken(context.Background(), selected)
			review.Logf("dimension=%s selected_endpoint=%s returned_token=%q error=%v", dimension, selected.GetCredential("base_url"), token, err)
			if dimension == "renewal" {
				require.NoError(review, err)
				require.Equal(review, "replacement-owner-token", token)
			} else {
				require.ErrorIs(review, err, ErrAccountRuntimeIdentityChanged)
				require.Empty(review, token)
			}
		})
	}
}

type registryProfileRecordingHTTP struct {
	actualProfile *tlsfingerprint.Profile
}

func (upstream *registryProfileRecordingHTTP) Do(request *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"provider":{"models":{}}}`))}, nil
}
func (upstream *registryProfileRecordingHTTP) DoWithTLS(request *http.Request, proxy string, accountID int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	if profile != nil {
		upstream.actualProfile = profile
	}
	return upstream.Do(request, proxy, accountID, concurrency)
}

func TestRegistryDownloadKeyMatchesActualRandomTLSProfile(review *testing.T) {
	review.Setenv("MODEL_CAPABILITY_OFFLINE", "false")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		1: {ID: 1, Name: "first", CipherSuites: []uint16{0x1301}},
		2: {ID: 2, Name: "second", CipherSuites: []uint16{0x1302}},
	}}
	account := &Account{ID: 98801, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"enable_tls_fingerprint": true, "tls_fingerprint_profile_id": float64(-1)}}
	mismatches := 0
	for attempt := 0; attempt < 32; attempt++ {
		upstream := &registryProfileRecordingHTTP{}
		service := &AccountTestService{httpUpstream: upstream, tlsFPProfileService: profiles}
		_, err := service.fetchModelsDevRegistry(context.Background(), account)
		require.NoError(review, err)
		require.NotNil(review, upstream.actualProfile)
		actualJSON, err := json.Marshal(upstream.actualProfile)
		require.NoError(review, err)
		expectedKey := ModelRegistryDownloadKey(modelsDevRegistryURL, "", "verified-tls"+string(actualJSON))
		if service.modelMetadataRegistryKey != expectedKey {
			mismatches++
		}
	}
	review.Logf("actual TLS profile and stored download key mismatched in %d/32 real fetches", mismatches)
	require.Zero(review, mismatches, "cache must be stamped with the transport profile actually used for its bytes")
}

type openAILockWaitExecutor struct{ OAuthRefreshExecutor }

func (*openAILockWaitExecutor) CanRefresh(*Account) bool                  { return true }
func (*openAILockWaitExecutor) NeedsRefresh(*Account, time.Duration) bool { return true }
func (*openAILockWaitExecutor) CacheKey(account *Account) string          { return OpenAITokenCacheKey(account) }
