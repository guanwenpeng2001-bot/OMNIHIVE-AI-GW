package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func completeCapabilityFixture() UpstreamModelMetadata {
	no := false
	m := UpstreamModelMetadata{ID: "sample", ContextLimitKind: "input", ProviderProfile: "openai", TaskType: "text_generation", SupportedMethods: []string{"responses"}, InputModalities: []string{"text"}, OutputModalities: []string{"text"}, ContextWindow: 128000, MaxOutputTokens: 8192, Reasoning: &no, SupportsFunctionCalling: &no}
	stampCapabilityEvidence(&m, "fixture", "v1", "openai", "sample", "exact", "verified")
	return m
}
func TestModelCapabilityV2RequiredFacts(t *testing.T) {
	m := completeCapabilityFixture()
	require.Equal(t, "complete", evaluateModelMetadataCompleteness(m).Status)
	m.MaxOutputTokens = 0
	require.Contains(t, evaluateModelMetadataCompleteness(m).MissingFields, "max_output_tokens")
	require.True(t, upstreamCatalogNeedsRegistry([]string{"sample"}, map[string]UpstreamModelMetadata{"sample": m}))
	m = completeCapabilityFixture()
	m.SupportsFunctionCalling = nil
	require.Contains(t, evaluateModelMetadataCompleteness(m).MissingFields, "supports_function_calling")
	m = completeCapabilityFixture()
	e := m.FieldEvidence["context_window"]
	e.Confidence = "estimated"
	m.FieldEvidence["context_window"] = e
	require.Equal(t, "estimated", evaluateModelMetadataCompleteness(m).Status)
	m = completeCapabilityFixture()
	m.InputModalitiesStatus = "partial"
	require.Contains(t, evaluateModelMetadataCompleteness(m).MissingFields, "input_modalities")
	m = completeCapabilityFixture()
	delete(m.FieldEvidence, "reasoning")
	require.Contains(t, evaluateModelMetadataCompleteness(m).MissingFields, "reasoning.evidence")
}
func TestModelCapabilityV2ReasoningKinds(t *testing.T) {
	for _, kind := range []string{"fixed", "toggle", "budget", "adaptive", "effort", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			m := completeCapabilityFixture()
			yes := true
			m.Reasoning = &yes
			c := &ReasoningControl{Kind: kind}
			m.ReasoningControl = c
			if kind == "effort" {
				c.EffortValues = []string{"low", "high"}
			}
			if kind == "budget" {
				min, max := int64(1), int64(8192)
				c.BudgetMin = &min
				c.BudgetMax = &max
			}
			stampCapabilityEvidence(&m, "fixture", "v1", "openai", "sample", "exact", "verified")
			result := evaluateModelMetadataCompleteness(m)
			if kind == "unknown" {
				require.Equal(t, "incomplete", result.Status)
			} else {
				require.Equal(t, "complete", result.Status)
			}
			require.Empty(t, m.SupportedReasoningLevels)
		})
	}
}
func TestModelCapabilityV2NonTextProfiles(t *testing.T) {
	m := completeCapabilityFixture()
	m.TaskType = "embedding"
	m.Reasoning = nil
	m.SupportsFunctionCalling = nil
	m.MaxOutputTokens = 0
	require.Contains(t, evaluateModelMetadataCompleteness(m).MissingFields, "embedding_dimensions")
	m.EmbeddingDimensions = 1536
	stampCapabilityEvidence(&m, "fixture", "v1", "openai", "sample", "exact", "verified")
	require.Equal(t, "complete", evaluateModelMetadataCompleteness(m).Status)
	m.TaskType = "video_generation"
	m.ContextWindow = 0
	m.SupportedMethods = nil
	m.EndpointStatus = "unsupported"
	stampCapabilityEvidence(&m, "fixture", "v1", "openai", "sample", "exact", "verified")
	// Endpoint knowledge does not replace media format, resolution or duration facts.
	e := m.FieldEvidence["endpoint_status"]
	m.FieldEvidence["supported_methods"] = e
	require.Equal(t, "incomplete", evaluateModelMetadataCompleteness(m).Status)
	require.Contains(t, evaluateModelMetadataCompleteness(m).MissingFields, "task_limits.max_duration_seconds")
}
func TestModelCapabilityHintsAreNotSpecifications(t *testing.T) {
	for _, id := range []string{"private-256k", "private-1m", "asr-flash", "private-unknown", "private-non-reasoning"} {
		m := inferModelCapabilityHints(id)
		require.Zero(t, m.ContextWindow)
		require.Zero(t, m.MaxOutputTokens)
		require.Nil(t, m.SupportsFunctionCalling)
		require.Nil(t, m.Reasoning)
		require.Empty(t, m.TaskType)
	}
	hint := inferModelCapabilityHints("private-reasoner")
	require.True(t, *hint.Reasoning)
	require.Equal(t, "estimated", hint.FieldEvidence["reasoning"].Confidence)
	no := false
	m, _ := mergeUpstreamModelMetadata(UpstreamModelMetadata{Reasoning: &no}, hint)
	require.False(t, *m.Reasoning)
}
func TestModelCapabilityScopedAliasesAndCollisions(t *testing.T) {
	r := CapabilityRecord{Profile: "cursor", ModelID: "GLM-5", Aliases: []string{"ZHIPU/GLM-5"}}
	records := map[string]CapabilityRecord{capabilityKey(r.Profile, r.ModelID): r}
	snapshot := &ModelCapabilitySnapshot{records: records, aliases: buildCapabilityAliasIndex(records)}
	match := lookupCapabilityMetadata(snapshot, "cursor", "ZHIPU/GLM-5")
	require.Equal(t, "matched", match.Status)
	require.Equal(t, "ZHIPU/GLM-5", match.Metadata.ID)
	require.Equal(t, "not_found", lookupCapabilityMetadata(snapshot, "openai", "GLM-5").Status)
	require.Equal(t, "not_found", lookupCapabilityMetadata(snapshot, "cursor", "glm-5").Status)
	r.ModelID = "other"
	records[capabilityKey(r.Profile, r.ModelID)] = r
	snapshot.aliases = buildCapabilityAliasIndex(records)
	for i := 0; i < 100; i++ {
		require.Equal(t, "ambiguous", lookupCapabilityMetadata(snapshot, "cursor", "ZHIPU/GLM-5").Status)
	}
}
func TestModelCapabilityRegistryProviderTieAndRegions(t *testing.T) {
	registry := map[string]modelsDevProvider{"a": {API: "https://provider.example/v1"}, "b": {API: "https://provider.example/v1"}}
	for i := 0; i < 100; i++ {
		_, ok := matchModelsDevProviderByAPIURL(registry, "https://provider.example/v1")
		require.False(t, ok)
	}
	for host, id := range map[string]string{"dashscope.aliyuncs.com": "alibaba-cn", "dashscope-intl.aliyuncs.com": "alibaba", "api.anthropic.com": "anthropic", "generativelanguage.googleapis.com": "google", "api.x.ai": "xai"} {
		registry[id] = modelsDevProvider{ID: id, Models: map[string]modelsDevModel{"sample": {}}}
		p, ok := matchModelsDevProvider(registry, "https://"+host)
		require.True(t, ok)
		require.Equal(t, id, p.ID)
	}
}
func TestModelCapabilityModelsDevControlsAndOriginalID(t *testing.T) {
	var m modelsDevModel
	require.NoError(t, json.Unmarshal([]byte(`{"id":"canonical","reasoning":true,"tool_call":false,"reasoning_options":[{"type":"budget_tokens","min":1024,"max":8192}],"modalities":{"input":["text","audio","pdf"],"output":["text"]}}`), &m))
	result := upstreamMetadataFromModelsDevModel("original", m)
	require.Equal(t, "original", result.ID)
	require.False(t, *result.SupportsFunctionCalling)
	require.Empty(t, result.SupportedReasoningLevels)
	require.Empty(t, result.DefaultReasoningLevel)
	require.Equal(t, "budget", result.ReasoningControl.Kind)
	require.Equal(t, int64(8192), *result.ReasoningControl.BudgetMax)
	require.Equal(t, []string{"text", "audio", "pdf"}, result.InputModalities)
	require.Nil(t, result.CodexToolCapabilities)
}
func TestModelCapabilityMergeEvidenceAndIsolation(t *testing.T) {
	primary := completeCapabilityFixture()
	primary.ContextLimitKind = "total"
	primary.MaxOutputTokens = 0
	fallback := UpstreamModelMetadata{MaxOutputTokens: 256000, ContextWindow: 1000000, SupportsFunctionCalling: new(bool)}
	m, _ := mergeUpstreamModelMetadata(primary, fallback)
	require.Zero(t, m.MaxOutputTokens)
	require.Equal(t, int64(128000), m.ContextWindow)
	yes := true
	fallback = UpstreamModelMetadata{Reasoning: &yes, ReasoningControl: &ReasoningControl{Kind: "fixed"}, SupportsFunctionCalling: &yes}
	m, _ = mergeUpstreamModelMetadata(primary, fallback)
	require.Nil(t, m.ReasoningControl)
	require.False(t, *m.SupportsFunctionCalling)
	m.FieldEvidence["reasoning"] = ModelCapabilityEvidence{}
	require.Equal(t, "fixture", primary.FieldEvidence["reasoning"].Source)
}

type capabilityRegistryHTTP struct {
	calls   atomic.Int32
	start   chan struct{}
	release chan struct{}
}

func (h *capabilityRegistryHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	h.calls.Add(1)
	select {
	case h.start <- struct{}{}:
	default:
	}
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-h.release:
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"openai":{"models":{"sample":{"reasoning":false}}}}`))}, nil
}
func (h *capabilityRegistryHTTP) DoWithTLS(req *http.Request, p string, a int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return h.Do(req, p, a, c)
}
func TestModelCapabilityRegistrySingleflightAndCancellation(t *testing.T) {
	h := &capabilityRegistryHTTP{start: make(chan struct{}, 1), release: make(chan struct{})}
	svc := &AccountTestService{httpUpstream: h}
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "false") // This service uses only the fake transport above.
	a := &Account{}
	var wg sync.WaitGroup
	var release sync.Once
	defer func() {
		release.Do(func() { close(h.release) })
		wg.Wait()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := svc.fetchModelsDevRegistry(context.Background(), a)
		if err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-h.start:
	case <-time.After(5 * time.Second):
		t.Fatal("fake registry transport was not reached; check offline test isolation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.fetchModelsDevRegistry(ctx, a)
	require.ErrorIs(t, err, context.Canceled)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			registry, err := svc.fetchModelsDevRegistry(context.Background(), a)
			if err != nil {
				t.Error(err)
			}
			if registry["openai"].Version == "" {
				t.Error("missing version")
			}
		}()
	}
	release.Do(func() { close(h.release) })
	wg.Wait()
	require.Equal(t, int32(1), h.calls.Load())
}

func TestModelCapabilityRegistryOfflineAndStale(t *testing.T) {
	h := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("rate limited"))}}
	svc := &AccountTestService{httpUpstream: h, modelMetadataRegistryKey: ModelRegistryDownloadKey(modelsDevRegistryURL, "", "verified-tls"), modelMetadataRegistry: map[string]modelsDevProvider{"openai": {Version: "old"}}, modelMetadataRegistryAt: time.Now().Add(-7 * time.Hour)}
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "false") // This service uses only the fake transport above.
	registry, err := svc.fetchModelsDevRegistry(context.Background(), &Account{})
	require.Error(t, err)
	require.True(t, registry["openai"].Stale)
	require.Equal(t, "old", registry["openai"].Version)
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	registry, err = svc.fetchModelsDevRegistry(context.Background(), &Account{})
	require.ErrorContains(t, err, "offline")
	require.Nil(t, registry)
	require.Len(t, h.requests, 1)
}

type countedCapabilityProvider struct {
	snapshot *ModelCapabilitySnapshot
	calls    int
}

func (p *countedCapabilityProvider) CapabilitySnapshot() *ModelCapabilitySnapshot {
	p.calls++
	return p.snapshot
}
func TestModelCapabilityCascadeCapturesOneSnapshotAndPreservesPriority(t *testing.T) {
	yes, no := true, false
	input, output := int64(256000), int64(16000)
	record := CapabilityRecord{Profile: "openai", ModelID: "private-reasoner", Source: "fixture", Mode: "chat", LiteLLMCapabilityFields: LiteLLMCapabilityFields{MaxInputTokens: &input, MaxOutputTokens: &output, SupportsReasoning: &yes, SupportsFunctionCalling: &no, InputModalities: []string{"text"}}}
	snapshot := &ModelCapabilitySnapshot{version: "v1", records: map[string]CapabilityRecord{capabilityKey("openai", "private-reasoner"): record}}
	provider := &countedCapabilityProvider{snapshot: snapshot}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"private-reasoner","reasoning":false,"context_window":64000,"supported_methods":["chat_completions"]}]}`))},
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"openai":{"models":{"private-reasoner":{"reasoning":true,"tool_call":true,"modalities":{"input":["text","image"],"output":["text"]},"limit":{"context":1000000,"output":8000}}}}}`))},
	}}
	svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "false") // This service uses only the fake transport above.
	a := &Account{ID: 301, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://api.openai.com/v1"}}
	catalog, err := svc.DiscoverModelCapabilities(context.Background(), a, provider)
	require.NoError(t, err)
	require.Equal(t, 1, provider.calls)
	require.Len(t, upstream.requests, 2)
	m := catalog.Metadata["private-reasoner"]
	require.False(t, *m.Reasoning)
	require.False(t, *m.SupportsFunctionCalling)
	require.Equal(t, int64(64000), m.ContextWindow)
	require.Equal(t, int64(16000), m.MaxOutputTokens)
	require.Equal(t, []string{"text"}, m.InputModalities)
	require.Equal(t, "complete", m.MetadataStatus, "missing=%v conflicts=%v evidence=%v", m.MissingFields, m.Conflicts, m.FieldEvidence)
	require.Empty(t, catalog.Warnings)
	require.Equal(t, "upstream", m.FieldEvidence["context_window"].Source)
	require.Equal(t, "fixture", m.FieldEvidence["max_output_tokens"].Source)
	require.Equal(t, "models.dev", m.FieldEvidence["output_modalities"].Source)
	require.Equal(t, []string{"private-reasoner"}, catalog.DiscoveredModels)
}

func TestModelCapabilityRegistryBudgetAndExpiredCache(t *testing.T) {
	h := &capabilityRegistryHTTP{start: make(chan struct{}, 1), release: make(chan struct{})}
	svc := &AccountTestService{httpUpstream: h, modelMetadataRegistryKey: ModelRegistryDownloadKey(modelsDevRegistryURL, "", "verified-tls"), modelMetadataRegistry: map[string]modelsDevProvider{"openai": {Version: "too-old"}}, modelMetadataRegistryAt: time.Now().Add(-80 * time.Hour)}
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "false") // This service uses only the fake transport above.
	started := time.Now()
	registry, err := svc.fetchModelsDevRegistry(context.Background(), &Account{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, registry)
	require.Less(t, time.Since(started), 5*time.Second)
}

func TestModelCapabilityStructuredSupplementCompatibility(t *testing.T) {
	var r CapabilityRecord
	require.NoError(t, json.Unmarshal([]byte(`{"model_id":"fixed-model","profile":"hosting","supports_reasoning":true,"reasoning_control":{"kind":"fixed"},"aliases":["fixed-latest"]}`), &r))
	require.NoError(t, validateCapabilityRecord(r))
	require.Equal(t, "fixed", r.Control.Kind)
	clone := cloneCapabilityRecord(r)
	clone.Aliases[0] = "mutated"
	clone.Control.Kind = "unknown"
	require.Equal(t, "fixed", r.Control.Kind)
	require.Equal(t, "fixed-latest", r.Aliases[0])
}

func TestModelCapabilityEmptyDeclarationsRoundTrip(t *testing.T) {
	m := UpstreamModelMetadata{ID: "empty", InputModalities: []string{}, OutputModalities: []string{}, SupportedMethods: []string{}, SupportedReasoningLevels: []string{}}
	body, err := json.Marshal(m)
	require.NoError(t, err)
	var restored UpstreamModelMetadata
	require.NoError(t, json.Unmarshal(body, &restored))
	require.Equal(t, m, restored)
	c := cloneUpstreamModelMetadata(UpstreamModelMetadata{ReasoningControl: &ReasoningControl{Kind: "effort", EffortValues: []string{}}})
	require.NotNil(t, c.ReasoningControl.EffortValues)
}

func TestModelCapabilityRegistryInvalidResponsesRetainCache(t *testing.T) {
	for name, body := range map[string]string{"empty": "{}", "malformed": "{", "too_large": strings.Repeat(" ", int(upstreamModelsBodyLimit)+1)} {
		t.Run(name, func(t *testing.T) {
			h := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}}
			svc := &AccountTestService{httpUpstream: h, modelMetadataRegistryKey: ModelRegistryDownloadKey(modelsDevRegistryURL, "", "verified-tls"), modelMetadataRegistry: map[string]modelsDevProvider{"openai": {Version: "valid"}}, modelMetadataRegistryAt: time.Now().Add(-7 * time.Hour)}
			t.Setenv("MODEL_CAPABILITY_OFFLINE", "false") // Only the fake transport above can run.
			registry, err := svc.fetchModelsDevRegistry(context.Background(), &Account{})
			require.Error(t, err)
			require.Equal(t, "valid", registry["openai"].Version)
			require.True(t, registry["openai"].Stale)
			require.Equal(t, "valid", svc.modelMetadataRegistry["openai"].Version)
			require.False(t, svc.modelMetadataRegistry["openai"].Stale)
		})
	}
}

func TestModelCapabilityExplicitEmptyEffortRejectsLowerControl(t *testing.T) {
	yes := true
	m, changed := mergeUpstreamModelMetadata(UpstreamModelMetadata{Reasoning: &yes, ReasoningControl: &ReasoningControl{Kind: "unknown"}, SupportedReasoningLevels: []string{}}, UpstreamModelMetadata{ReasoningControl: &ReasoningControl{Kind: "effort", EffortValues: []string{"high"}}})
	require.False(t, changed)
	require.Empty(t, m.SupportedReasoningLevels)
	require.Equal(t, "unknown", m.ReasoningControl.Kind)
}
func BenchmarkCapabilityLookup(b *testing.B) {
	snapshot, err := buildModelCapabilitySnapshot(nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lookupCapabilityMetadata(snapshot, "openai", "gpt-4o")
	}
}

func TestModelCapabilityNativeVisionFalseCannotBeOverriddenByCompleteModalities(t *testing.T) {
	no := false
	primary := UpstreamModelMetadata{SupportsVision: &no, InputModalities: []string{"text"}, InputModalitiesStatus: "partial"}
	m, _ := mergeUpstreamModelMetadata(primary, UpstreamModelMetadata{InputModalities: []string{"text", "image"}, InputModalitiesStatus: "complete"})
	require.Equal(t, []string{"text"}, m.InputModalities)
	require.Equal(t, "partial", m.InputModalitiesStatus)
	require.False(t, *m.SupportsVision)
	m, _ = mergeUpstreamModelMetadata(primary, UpstreamModelMetadata{InputModalities: []string{"text", "audio"}, InputModalitiesStatus: "complete"})
	require.Equal(t, []string{"text", "audio"}, m.InputModalities)
	require.Equal(t, "complete", m.InputModalitiesStatus)
}
