package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
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

func (service *AccountTestService) modelsDevDownloadKey(account *Account) string {
	trustDomain := "verified-tls"
	if service.tlsFPProfileService != nil {
		profile, _ := json.Marshal(service.tlsFPProfileService.ResolveTLSProfile(account))
		trustDomain += string(profile)
	}
	return ModelRegistryDownloadKey(modelsDevRegistryURL, upstreamModelsProxyURL(account), trustDomain)
}

func (service *AccountTestService) cachedModelsDevRegistry(downloadKey string) (map[string]modelsDevProvider, time.Time) {
	service.modelMetadataRegistryMu.Lock()
	defer service.modelMetadataRegistryMu.Unlock()
	if service.modelMetadataRegistryKey != downloadKey {
		return nil, time.Time{}
	}
	return service.modelMetadataRegistry, service.modelMetadataRegistryAt
}
