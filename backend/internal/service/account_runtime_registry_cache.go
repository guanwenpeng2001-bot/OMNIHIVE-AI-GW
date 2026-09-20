package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// ModelRegistryDownloadKey must be used unchanged by download cache, flight and
// stale fallback. TrustDomain identifies the transport/verification policy;
// proxyURL includes proxy credentials and is hashed, never exposed in the key.
func ModelRegistryDownloadKey(sourceURL, proxyURL, trustDomain string) string {
	encoded, _ := json.Marshal([]string{sourceURL, proxyURL, trustDomain})
	digest := sha256.Sum256(encoded)
	return "registry:" + hex.EncodeToString(digest[:])
}

// ModelRegistryEnrichmentKey isolates account-specific facts from public bytes.
// Draft identity is a server-issued reference, not the shared numeric ID zero.
func ModelRegistryEnrichmentKey(downloadKey string, account *Account, draftIdentity string) (string, error) {
	if account == nil {
		return "", fmt.Errorf("registry enrichment requires account identity")
	}
	if account.ID == 0 && draftIdentity == "" {
		return "", fmt.Errorf("registry enrichment requires draft identity")
	}
	encoded, _ := json.Marshal([]string{downloadKey, AccountRuntimeIdentityKey(account), draftIdentity})
	digest := sha256.Sum256(encoded)
	return "registry-enrichment:" + hex.EncodeToString(digest[:]), nil
}

// Resolve transport once: random TLS profiles must match both the download key
// and the profile actually used by the request, including asynchronous flights.
type modelsDevDownloadSnapshot struct {
	sourceURL   string
	proxyURL    string
	profile     *tlsfingerprint.Profile
	accountID   int64
	concurrency int
	downloadKey string
}

func (service *AccountTestService) modelsDevDownloadSnapshot(account *Account) modelsDevDownloadSnapshot {
	snapshot := modelsDevDownloadSnapshot{
		sourceURL:   modelsDevRegistryURL,
		proxyURL:    upstreamModelsProxyURL(account),
		accountID:   account.ID,
		concurrency: account.Concurrency,
	}
	trustDomain := "verified-tls"
	if service.tlsFPProfileService != nil {
		snapshot.profile = service.tlsFPProfileService.ResolveTLSProfile(account)
		profile, _ := json.Marshal(snapshot.profile)
		trustDomain += string(profile)
	}
	snapshot.downloadKey = ModelRegistryDownloadKey(snapshot.sourceURL, snapshot.proxyURL, trustDomain)
	return snapshot
}

func (service *AccountTestService) cachedModelsDevRegistry(downloadKey string) (map[string]modelsDevProvider, time.Time) {
	service.modelMetadataRegistryMu.Lock()
	defer service.modelMetadataRegistryMu.Unlock()
	if service.modelMetadataRegistryKey != downloadKey {
		return nil, time.Time{}
	}
	return service.modelMetadataRegistry, service.modelMetadataRegistryAt
}
