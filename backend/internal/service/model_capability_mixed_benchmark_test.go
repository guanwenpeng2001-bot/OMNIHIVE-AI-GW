//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/capabilitysources"
	"github.com/stretchr/testify/require"
)

// The denominator is the synthetic account's D union concrete mapping values,
// not the size of any registry or the number of successful matches.
type capabilityMixedFixture struct {
	account      *Account
	records      map[string]CapabilityRecord
	observations map[string]capabilitysources.Observation
	bodies       map[string][]byte
	ids          []string
}

func newCapabilityMixedFixture(test testing.TB, count int) capabilityMixedFixture {
	test.Helper()
	fixture := capabilityMixedFixture{account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{}, Extra: map[string]any{}}, records: make(map[string]CapabilityRecord), observations: make(map[string]capabilitysources.Observation), bodies: make(map[string][]byte)}
	mapping := make(map[string]string)
	var discovered []string
	native := make(map[string]UpstreamModelMetadata)
	modelsDev := make(map[string]any)
	liteLLM := make(map[string]any)
	var openRouter []any
	for ordinal := 0; ordinal < count; ordinal++ {
		id := fmt.Sprintf("synthetic-mixed-%04d", ordinal)
		fixture.ids = append(fixture.ids, id)
		if ordinal%2 == 0 {
			discovered = append(discovered, id)
		} else {
			mapping["caller-"+id] = id
		}
		no, yes := false, true
		window, output := int64(8192), int64(512)
		canonical := id
		if ordinal%5 == 3 {
			canonical = "canonical-" + id
		}
		record := CapabilityRecord{Profile: "openai", Provider: "openai", ModelID: canonical, Mode: "chat", Source: "synthetic-reviewed", SupportedMethods: []string{"chat.completions"}, LiteLLMCapabilityFields: LiteLLMCapabilityFields{MaxInputTokens: &window, MaxOutputTokens: &output, SupportsReasoning: &no, SupportsFunctionCalling: &yes, InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}
		if ordinal%5 == 3 {
			record.Aliases = []string{id}
		}
		if ordinal%5 != 1 {
			fixture.records[capabilityKey("openai", canonical)] = record
		}
		if ordinal%5 == 2 {
			native[id] = UpstreamModelMetadata{ID: id, ProviderProfile: "openai", SupportsFunctionCalling: &no, Reasoning: &no}
		}
		if ordinal%5 == 4 {
			native[id] = UpstreamModelMetadata{ID: id, ProviderProfile: "openai", TaskType: "embedding"}
		}
		// Partial observations are intentionally split across sources. OpenRouter
		// remains a separate namespace, even when its IDs happen to be identical.
		modelsDev[id] = map[string]any{"reasoning": false, "limit": map[string]any{"context": 4096}, "modalities": map[string]any{"input": []string{"text"}, "output": []string{"text"}}}
		liteLLM[id] = map[string]any{"litellm_provider": "openai", "max_output_tokens": 1024, "supports_function_calling": true, "mode": "chat"}
		openRouter = append(openRouter, map[string]any{"id": id, "context_length": 999999})
	}
	fixture.account.Credentials["model_mapping"] = mapping
	fixture.account.Credentials["base_url"] = "https://api.openai.com"
	fixture.account.Extra[UpstreamModelCatalogExtraKey] = AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: discovered}
	fixture.account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: native})
	for source, value := range map[string]any{"models.dev": map[string]any{"openai": map[string]any{"models": modelsDev}}, "litellm": liteLLM, "openrouter": map[string]any{"data": openRouter}} {
		body, err := json.Marshal(value)
		require.NoError(test, err)
		fixture.bodies[source] = body
		observation, err := capabilitysources.Parse(source, body, "2026-09-19")
		require.NoError(test, err)
		require.Len(test, observation.Records, count)
		fixture.observations[source] = observation
	}
	return fixture
}

// Build and publish through the same immutable record/alias indexes and
// publication lock used by production. No hot Lookup-only benchmark.
func (fixture capabilityMixedFixture) candidate(generation int64) (*ModelCapabilitySnapshot, error) {
	records := make(map[string]CapabilityRecord, len(fixture.records))
	for key, original := range fixture.records {
		record := cloneCapabilityRecord(original)
		window := int64(8192) + generation
		record.MaxInputTokens = &window
		if err := validateCapabilityRecord(record); err != nil {
			return nil, err
		}
		records[key] = record
	}
	return &ModelCapabilitySnapshot{generation: generation, version: fmt.Sprint(generation), records: records, aliases: buildCapabilityAliasIndex(records)}, nil
}

func (fixture capabilityMixedFixture) pipeline(provider *capabilityTestProvider, enricher *CapabilityReadOnlyEnricher, observations map[string]capabilitysources.Observation) (map[string]UpstreamModelMetadata, error) {
	candidate, err := fixture.candidate(0)
	if err != nil {
		return nil, err
	}
	entries := make(map[string]*capabilityDisplaySource, len(observations))
	for source, observation := range observations {
		entries[source] = &capabilityDisplaySource{index: capabilityObservationIndex(observation), expires: time.Now().Add(capabilityDisplayTTL)}
	}
	enricher.mu.Lock()
	enricher.sources = entries
	enricher.mu.Unlock()
	provider.publishCapabilityCandidate(candidate)
	return enricher.Project(fixture.account, false), nil
}

func (fixture capabilityMixedFixture) verify(projection map[string]UpstreamModelMetadata) error {
	if len(projection) != len(fixture.ids) {
		return fmt.Errorf("denominator: got %d want %d", len(projection), len(fixture.ids))
	}
	for ordinal, id := range fixture.ids {
		metadata, exists := projection[id]
		if !exists || metadata.ID != id || metadata.ProviderProfile != "openai" {
			return fmt.Errorf("identity lost for %s", id)
		}
		if metadata.MetadataStatus == "" {
			return fmt.Errorf("completeness not evaluated for %s", id)
		}
		switch ordinal % 5 {
		case 0:
			if metadata.MetadataStatus != "complete" {
				return fmt.Errorf("complete fixture %s: %s %v", id, metadata.MetadataStatus, metadata.MissingFields)
			}
		case 1:
			if metadata.MetadataStatus != "incomplete" || len(metadata.MissingFields) == 0 || metadata.ContextWindow != 4096 || metadata.MaxOutputTokens != 1024 {
				return fmt.Errorf("missing/multi-source fixture changed: %s", id)
			}
			if metadata.FieldEvidence["context_window"].Source != "models.dev" || metadata.FieldEvidence["max_output_tokens"].Source != "litellm" {
				return fmt.Errorf("field source lost: %s", id)
			}
		case 2:
			if metadata.SupportsFunctionCalling == nil || *metadata.SupportsFunctionCalling || metadata.Reasoning == nil || *metadata.Reasoning {
				return fmt.Errorf("native false lost: %s", id)
			}
		case 3:
			if metadata.FieldEvidence["context_window"].MatchRule != "reviewed_alias" {
				return fmt.Errorf("alias bypassed: %s", id)
			}
		case 4:
			if metadata.TaskType != "embedding" || metadata.MetadataStatus != "incomplete" {
				return fmt.Errorf("task type conflict incorrectly completed: %s", id)
			}
		}
		if ordinal%5 != 1 && metadata.ContextWindow != 8192 {
			return fmt.Errorf("reviewed context overwritten for %s", id)
		}
	}
	return nil
}

func TestCapabilityEnrichmentMixedSnapshots(test *testing.T) {
	for _, count := range []int{252, 1000} {
		test.Run(fmt.Sprint(count), func(test *testing.T) {
			fixture := newCapabilityMixedFixture(test, count)
			provider := &capabilityTestProvider{}
			enricher, err := NewCapabilityReadOnlyEnricher(provider, nil)
			require.NoError(test, err)
			before, err := json.Marshal(fixture.account)
			require.NoError(test, err)
			projection, err := fixture.pipeline(provider, enricher, fixture.observations)
			require.NoError(test, err)
			require.NoError(test, fixture.verify(projection))
			oldSnapshot, err := fixture.candidate(0)
			require.NoError(test, err)
			newSnapshot, err := fixture.candidate(1)
			require.NoError(test, err)
			var failures, reads atomic.Int64
			var participants sync.WaitGroup
			start := make(chan struct{})
			finished := make(chan struct{})
			participants.Go(func() {
				<-start
				for {
					provider.publishCapabilityCandidate(oldSnapshot)
					provider.publishCapabilityCandidate(newSnapshot)
					select {
					case <-finished:
						return
					default:
						runtime.Gosched()
					}
				}
			})
			var readers sync.WaitGroup
			for reader := 0; reader < 16; reader++ {
				readers.Go(func() {
					<-start
					for iteration := 0; iteration < 8; iteration++ {
						result := enricher.Project(fixture.account, false)
						generationWindow := result[fixture.ids[0]].ContextWindow
						if len(result) != count || (generationWindow != 8192 && generationWindow != 8193) {
							failures.Add(1)
						}
						for ordinal, id := range fixture.ids {
							if ordinal%5 != 1 && result[id].ContextWindow != generationWindow {
								failures.Add(1)
							}
						}
						reads.Add(1)
					}
				})
			}
			close(start)
			readers.Wait()
			close(finished)
			participants.Wait()
			require.Zero(test, failures.Load(), "a Project call must capture one complete reviewed snapshot")
			require.EqualValues(test, 128, reads.Load())
			after, err := json.Marshal(fixture.account)
			require.NoError(test, err)
			require.Equal(test, string(before), string(after), "read-only enrichment must not mutate the account")
			// Wrong JSON types fail parsing instead of silently becoming false/zero.
			_, err = capabilitysources.Parse("litellm", []byte(`{"invalid":{"litellm_provider":"openai","supports_reasoning":"false"}}`), "2026-09-19")
			require.Error(test, err)
			test.Logf("models=%d concurrent_reads=%d partial_snapshots=%d synthetic=true", count, reads.Load(), failures.Load())
		})
	}
}

func BenchmarkCapabilityEnrichmentMixed(benchmark *testing.B) {
	for _, count := range []int{252, 1000} {
		benchmark.Run(fmt.Sprintf("full-merge-%d", count), func(benchmark *testing.B) {
			fixture := newCapabilityMixedFixture(benchmark, count)
			provider := &capabilityTestProvider{}
			enricher, err := NewCapabilityReadOnlyEnricher(provider, nil)
			require.NoError(benchmark, err)
			budget := 10 * time.Millisecond
			if count == 1000 {
				budget = 40 * time.Millisecond
			}
			runCapabilityMixedMeasurement(benchmark, fixture, budget, func() (map[string]UpstreamModelMetadata, error) {
				return fixture.pipeline(provider, enricher, fixture.observations)
			})
		})
		benchmark.Run(fmt.Sprintf("loopback-network-end-to-end-%d", count), func(benchmark *testing.B) {
			fixture := newCapabilityMixedFixture(benchmark, count)
			// A real loopback HTTP connection includes collection, body reading,
			// parser, indexes, publication and projection. This is not WAN evidence.
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				body, exists := fixture.bodies[request.URL.Query().Get("source")]
				if !exists {
					http.NotFound(response, request)
					return
				}
				if _, err := response.Write(body); err != nil {
					benchmark.Errorf("fixture HTTP write: %v", err)
				}
			}))
			defer server.Close()
			endpoint, err := url.Parse(server.URL)
			require.NoError(benchmark, err)
			transport := &http.Transport{Proxy: nil}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: capabilityDisplayTransport(func(request *http.Request) (*http.Response, error) {
				copyRequest := request.Clone(request.Context())
				copyURL := *endpoint
				for _, source := range capabilityDisplaySources {
					original, _ := capabilitysources.Endpoint(source)
					if request.URL.String() == original {
						values := url.Values{}
						values.Set("source", source)
						copyURL.RawQuery = values.Encode()
					}
				}
				copyRequest.URL = &copyURL
				copyRequest.Host = copyURL.Host
				return transport.RoundTrip(copyRequest)
			})}
			provider := &capabilityTestProvider{}
			enricher, err := NewCapabilityReadOnlyEnricher(provider, client)
			require.NoError(benchmark, err)
			runCapabilityMixedMeasurement(benchmark, fixture, 0, func() (map[string]UpstreamModelMetadata, error) {
				observations := make(map[string]capabilitysources.Observation)
				for _, source := range capabilityDisplaySources {
					capture, collectErr := capabilitysources.Collect(context.Background(), client, source, nil)
					if collectErr != nil {
						return nil, collectErr
					}
					observations[source] = capture.Observation
				}
				return fixture.pipeline(provider, enricher, observations)
			})
		})
	}
}

func runCapabilityMixedMeasurement(benchmark *testing.B, fixture capabilityMixedFixture, budget time.Duration, operation func() (map[string]UpstreamModelMetadata, error)) {
	benchmark.Helper()
	for warmup := 0; warmup < 5; warmup++ {
		projection, err := operation()
		require.NoError(benchmark, err)
		require.NoError(benchmark, fixture.verify(projection))
	}
	samples := make([]int64, benchmark.N)
	var peak atomic.Uint64
	stop := make(chan struct{})
	var observer sync.WaitGroup
	observer.Go(func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			if memory.HeapAlloc > peak.Load() {
				peak.Store(memory.HeapAlloc)
			}
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	})
	defer func() { close(stop); observer.Wait() }()
	benchmark.ReportAllocs()
	var memoryBefore, memoryAfter runtime.MemStats
	runtime.ReadMemStats(&memoryBefore)
	benchmark.ResetTimer()
	measurementStart := time.Now()
	for iteration := 0; iteration < benchmark.N; iteration++ {
		began := time.Now()
		projection, err := operation()
		samples[iteration] = time.Since(began).Nanoseconds()
		if err != nil {
			benchmark.Fatal(err)
		}
		if len(projection) != len(fixture.ids) {
			benchmark.Fatal("empty/partial benchmark output")
		}
	}
	benchmark.StopTimer()
	measurementElapsed := time.Since(measurementStart)
	runtime.ReadMemStats(&memoryAfter)
	ordered := append([]int64(nil), samples...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	quantile := func(percentile int) int64 { return ordered[(len(ordered)*percentile+99)/100-1] }
	for _, percentile := range []int{50, 95, 99} {
		benchmark.ReportMetric(float64(quantile(percentile)), fmt.Sprintf("p%d-ns", percentile))
	}
	benchmark.ReportMetric(float64(peak.Load()), "peak-heap-B")
	body, err := json.Marshal(map[string]any{"benchmark": benchmark.Name(), "models": len(fixture.ids), "warmup_rounds": 5, "samples_ns": samples, "p50_ns": quantile(50), "p95_ns": quantile(95), "p99_ns": quantile(99), "peak_heap_bytes": peak.Load(), "budget_ns": budget.Nanoseconds(), "ns_per_op": float64(measurementElapsed.Nanoseconds()) / float64(benchmark.N), "bytes_per_op": float64(memoryAfter.TotalAlloc-memoryBefore.TotalAlloc) / float64(benchmark.N), "allocs_per_op": float64(memoryAfter.Mallocs-memoryBefore.Mallocs) / float64(benchmark.N), "memory_method": "process Go heap sampled each millisecond; includes measurement observer", "network": "loopback only; external WAN not measured", "database": "not applicable; separate repository pressure suite"})
	require.NoError(benchmark, err)
	benchmark.Logf("capability_mixed_measurement=%s", body)
	// Go's first N=1 calibration is not a population percentile. The final
	// benchmark must collect at least 20 samples; the runner checks output too.
	if benchmark.N >= 20 && budget > 0 {
		require.LessOrEqual(benchmark, quantile(95), budget.Nanoseconds(), "fixed full pipeline p95 budget")
	}
}
