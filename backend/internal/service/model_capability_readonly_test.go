package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/capabilitysources"
	"github.com/stretchr/testify/require"
)

type capabilityDisplayTransport func(*http.Request) (*http.Response, error)

func (transport capabilityDisplayTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func capabilityDisplayAccount() *Account {
	return &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"base_url": "https://api.openai.com", "model_mapping": map[string]string{"alias": "private-display", "duplicate": "private-display", "pattern": "wild-*"}},
		Extra:       map[string]any{UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"native-display"}}}}
}

func waitForCapabilityDisplay(t *testing.T, enricher *CapabilityReadOnlyEnricher) {
	t.Helper()
	require.Eventually(t, func() bool {
		enricher.mu.Lock()
		defer enricher.mu.Unlock()
		for _, entry := range enricher.sources {
			if entry.refreshing {
				return false
			}
		}
		return len(enricher.sources) == len(capabilityDisplaySources)
	}, 5*time.Second, time.Millisecond)
}

func TestCapabilityReadOnlyEnrichment(t *testing.T) {
	t.Run("cold_read_singleflight_seven_days_failure_and_no_authorization", func(t *testing.T) {
		var requests atomic.Int64
		var failed atomic.Bool
		var notModified atomic.Bool
		release := make(chan struct{})
		transport := capabilityDisplayTransport(func(request *http.Request) (*http.Response, error) {
			requests.Add(1)
			select {
			case <-release:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
			if failed.Load() {
				return nil, errors.New("offline")
			}
			if notModified.Load() {
				if request.Header.Get("If-None-Match") == "" {
					return nil, errors.New("missing etag")
				}
				return &http.Response{StatusCode: http.StatusNotModified, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			var body string
			switch request.URL.Hostname() {
			case "models.dev":
				body = `{"openai":{"models":{"private-display":{"reasoning":false,"modalities":{"input":[],"output":["text"]},"limit":{"context":4096}}}}}`
			case "openrouter.ai":
				body = `{"data":[{"id":"private-display","context_length":99999}]}`
			case "raw.githubusercontent.com":
				body = `{"private-display":{"litellm_provider":"openai","max_input_tokens":9000,"max_output_tokens":512,"supports_reasoning":true,"mode":"chat","input_cost_per_token":123}}`
			default:
				return nil, errors.New("unapproved destination")
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Etag": []string{"fixture"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		enricher, err := NewCapabilityReadOnlyEnricher(nil, &http.Client{Transport: transport})
		require.NoError(t, err)
		clock := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
		enricher.now = func() time.Time { return clock }
		account := capabilityDisplayAccount()
		before, err := json.Marshal(account)
		require.NoError(t, err)
		baseline := account.ModelCatalogBaseline()
		eligible := AccountSupportsResolvedModel(context.Background(), account, "private-display", AccountModelBindingOptions{})
		require.False(t, eligible)
		cold := make(chan map[string]UpstreamModelMetadata, 1)
		go func() { cold <- enricher.Project(account, true) }()
		select {
		case projection := <-cold:
			require.Zero(t, projection["private-display"].ContextWindow)
		case <-time.After(time.Second):
			close(release)
			t.Fatal("cold projection blocked on network")
		}
		var readers sync.WaitGroup
		for reader := 0; reader < 32; reader++ {
			readers.Go(func() { enricher.Project(account, true) })
		}
		readers.Wait()
		require.Eventually(t, func() bool { return requests.Load() == 3 }, time.Second, time.Millisecond)
		close(release)
		waitForCapabilityDisplay(t, enricher)
		projection := enricher.Project(account, true)
		metadata := projection["private-display"]
		require.EqualValues(t, 4096, metadata.ContextWindow, "priority is per field, never largest context")
		require.Equal(t, "total", metadata.ContextLimitKind)
		require.EqualValues(t, 512, metadata.MaxOutputTokens)
		require.False(t, *metadata.Reasoning)
		require.NotNil(t, metadata.InputModalities)
		require.Empty(t, metadata.InputModalities)
		require.Nil(t, metadata.SupportedMethods, "registries cannot manufacture endpoint entitlement")
		require.Equal(t, "models.dev", metadata.FieldEvidence["context_window"].Source)
		require.Equal(t, "litellm", metadata.FieldEvidence["max_output_tokens"].Source)
		require.Equal(t, "incomplete", metadata.MetadataStatus)
		*metadata.Reasoning = true
		require.False(t, *enricher.Project(account, false)["private-display"].Reasoning, "callers cannot mutate cached facts")
		clock = clock.Add(capabilityDisplayTTL - time.Nanosecond)
		require.EqualValues(t, 4096, enricher.Project(account, true)["private-display"].ContextWindow)
		require.EqualValues(t, 3, requests.Load())
		clock = clock.Add(time.Nanosecond)
		failed.Store(true)
		require.Zero(t, enricher.Project(account, true)["private-display"].ContextWindow)
		waitForCapabilityDisplay(t, enricher)
		require.EqualValues(t, 6, requests.Load())
		enricher.mu.Lock()
		require.True(t, enricher.sources["models.dev"].expires.Equal(clock), "failure does not renew success TTL")
		require.NotEmpty(t, enricher.sources["models.dev"].index, "failure retains last successful capture")
		enricher.mu.Unlock()
		enricher.Project(account, true)
		require.EqualValues(t, 6, requests.Load(), "failure backoff bounds retry rate")
		clock = clock.Add(time.Minute)
		failed.Store(false)
		notModified.Store(true)
		enricher.Project(account, true)
		waitForCapabilityDisplay(t, enricher)
		require.EqualValues(t, 4096, enricher.Project(account, false)["private-display"].ContextWindow)
		after, err := json.Marshal(account)
		require.NoError(t, err)
		require.JSONEq(t, string(before), string(after), "extra, discovery, eligibility, mapping and all account fields unchanged")
		require.Equal(t, baseline, account.ModelCatalogBaseline())
		require.Equal(t, eligible, AccountSupportsResolvedModel(context.Background(), account, "private-display", AccountModelBindingOptions{}))
	})
	t.Run("offline_reviewed_resources_still_available", func(t *testing.T) {
		enricher, err := NewCapabilityReadOnlyEnricher(nil, &http.Client{Transport: capabilityDisplayTransport(func(*http.Request) (*http.Response, error) {
			t.Error("offline must not collect")
			return nil, errors.New("unexpected network")
		})})
		require.NoError(t, err)
		account := capabilityDisplayAccount()
		account.Credentials["model_mapping"] = map[string]string{"alias": "gpt-4o"}
		require.Positive(t, enricher.Project(account, false)["gpt-4o"].ContextWindow)
	})
}

func TestCapabilityScopeCoverage(t *testing.T) {
	t.Run("three_level_field_cascade", func(t *testing.T) {
		enricher, err := NewCapabilityReadOnlyEnricher(nil, nil)
		require.NoError(t, err)
		account := capabilityDisplayAccount()
		account.Credentials["model_mapping"] = map[string]string{"alias": "gpt-4o"}
		no := false
		account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"gpt-4o": {ID: "gpt-4o", ProviderProfile: "openai", Reasoning: &no, InputModalities: []string{}}}})
		account.Extra[ModelCapabilityDefaultsExtraKey] = map[string]any{"schema_version": 1, "reason": "fixture", "identity_revision": "0", "identity_fingerprint": ModelCapabilityIdentityFingerprint(account), "models": map[string]any{"gpt-4o": map[string]any{"reasoning": true, "max_output_tokens": 100, "input_modalities": []string{"text"}}}}
		metadata := enricher.Project(account, false)["gpt-4o"]
		require.False(t, *metadata.Reasoning, "native false wins")
		require.NotNil(t, metadata.InputModalities)
		require.Empty(t, metadata.InputModalities, "native empty wins")
		require.EqualValues(t, 100, metadata.MaxOutputTokens, "bound declaration wins over reviewed")
		require.Positive(t, metadata.ContextWindow, "reviewed fills remaining fields")
		require.Equal(t, "account-declaration", metadata.FieldEvidence["max_output_tokens"].Source)
		require.NotEmpty(t, metadata.RejectedCandidates)
	})
	t.Run("mapped_only_and_task_specific_gaps", func(t *testing.T) {
		enricher, err := NewCapabilityReadOnlyEnricher(nil, nil)
		require.NoError(t, err)
		account := capabilityDisplayAccount()
		account.Extra = nil
		projection := enricher.Project(account, false)
		require.Len(t, projection, 1, "empty discovery retains deduped concrete mapping targets")
		require.Contains(t, projection, "private-display")
		require.Equal(t, "incomplete", projection["private-display"].MetadataStatus)
		account.Extra = map[string]any{UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{Complete: false, DiscoveredModels: []string{"partial-discovery"}}}
		require.Contains(t, enricher.Project(account, false), "partial-discovery", "partial discovery rows stay in the denominator")
		account.Credentials["model_mapping"] = map[string]any{"unknown-target": "pattern?", "concrete": "private-display"}
		require.Len(t, enricher.Project(account, false), 2, "wildcard targets are not concrete models")
		for _, task := range []string{"embedding", "image_generation", "video_generation", "transcription", "unknown"} {
			metadata := completeCapabilityFixture()
			metadata.TaskType, metadata.EndpointStatus = task, "unsupported"
			require.Equal(t, "incomplete", evaluateModelMetadataCompleteness(metadata).Status, task)
		}
		metadata := completeCapabilityFixture()
		evidence := metadata.FieldEvidence["context_window"]
		evidence.Confidence = "derived"
		metadata.FieldEvidence["context_window"] = evidence
		require.Equal(t, "estimated", evaluateModelMetadataCompleteness(metadata).Status)
		evidence.Stale = true
		metadata.FieldEvidence["context_window"] = evidence
		require.Equal(t, "incomplete", evaluateModelMetadataCompleteness(metadata).Status)
	})
	t.Run("registry_namespaces_cannot_cross_deployments", func(t *testing.T) {
		observation, err := capabilitysources.Parse("openrouter", []byte(`{"data":[{"id":"openai/gpt-4o","context_length":99999}]}`), "2026-09-19")
		require.NoError(t, err)
		index := capabilityObservationIndex(observation)
		require.Contains(t, index, capabilityKey("openrouter", "openai/gpt-4o"))
		require.NotContains(t, index, capabilityKey("openai", "gpt-4o"))
		require.NotContains(t, index, capabilityKey("openai-codex", "gpt-4o"))
	})
	t.Run("identity_revision_change_does_not_reuse_old_native_facts", func(t *testing.T) {
		enricher, err := NewCapabilityReadOnlyEnricher(nil, nil)
		require.NoError(t, err)
		account := capabilityDisplayAccount()
		account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"private-display": {ID: "private-display", ProviderProfile: "openai", ContextWindow: 12345}}})
		account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 2}
		projection := enricher.Project(account, false)
		require.Contains(t, projection, "native-display", "retain unresolved historic rows")
		require.Zero(t, projection["private-display"].ContextWindow)
	})
}
