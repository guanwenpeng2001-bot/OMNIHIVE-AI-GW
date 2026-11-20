package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	modelcapabilities "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-capabilities"
	"github.com/stretchr/testify/require"
)

// Synthetic providers belong in tests, never in the pricing publication path.
type capabilityTestProvider struct {
	current atomic.Pointer[ModelCapabilitySnapshot]
}

func (p *capabilityTestProvider) CapabilitySnapshot() *ModelCapabilitySnapshot {
	return p.current.Load()
}
func (p *capabilityTestProvider) publishCapabilityCandidate(snapshot *ModelCapabilitySnapshot) {
	p.current.Store(snapshot)
}

func registryTestPackage(t *testing.T, generation int64) (string, []byte) {
	t.Helper()
	var catalog map[string]any
	require.NoError(t, json.Unmarshal(modelcapabilities.JSON(), &catalog))
	catalog["generated_at"] = time.Unix(generation, 0).UTC().Format(time.RFC3339)
	catalogBody, err := json.Marshal(catalog)
	require.NoError(t, err)
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(modelcapabilities.ManifestJSON(), &manifest))
	manifest["approved_generation"] = generation
	manifest["catalog_sha256"] = capabilityHash(catalogBody)
	manifest["presets_sha256"] = capabilityHash([]byte(strings.ReplaceAll(string(platformModelPresetsJSON), "\r\n", "\n")))
	manifestBody, err := json.Marshal(manifest)
	require.NoError(t, err)
	pin, err := json.Marshal(modelcapabilities.ReleaseIdentity{ApprovedGeneration: generation, ManifestSHA256: capabilityHash(manifestBody)})
	require.NoError(t, err)
	directory := t.TempDir()
	for name, body := range map[string][]byte{"catalog.json": catalogBody, "release-manifest.json": manifestBody, "platform_model_presets.json": platformModelPresetsJSON} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), body, 0600))
	}
	return directory, pin
}

func TestModelRegistryApprovedAtomicReload(t *testing.T) {
	baseline := NewModelRegistry().Snapshot()
	directory, pin := registryTestPackage(t, baseline.Generation()+1)
	registry := &ModelRegistry{trustedPin: pin}
	registry.current.Store(baseline)
	plan := newGatewayRequestPlanWithRegistry(GatewayRequestSpec{}, registry)
	ctx := WithGatewayRequestPlan(context.Background(), plan)
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for attempt := 0; attempt < 10; attempt++ {
				_, err := registry.ReloadApprovedLocalPackage(directory)
				if err != nil {
					t.Error(err)
				}
				snapshot := registry.Snapshot()
				if snapshot.Generation() != baseline.Generation()+1 || snapshot.capabilities.generation != snapshot.Generation() {
					t.Error("mixed generation")
				}
			}
		}()
	}
	workers.Wait()
	require.Same(t, baseline, ModelRegistrySnapshotFromContext(ctx))
	require.Same(t, baseline, plan.ModelRegistrySnapshot())
	lastGood := registry.Snapshot()
	newPlan := newGatewayRequestPlanWithRegistry(GatewayRequestSpec{}, registry)
	newCtx := WithGatewayRequestPlan(context.Background(), newPlan)
	require.NotSame(t, baseline, lastGood)
	require.Same(t, lastGood, newPlan.ModelRegistrySnapshot())
	require.Same(t, lastGood, ModelRegistrySnapshotFromContext(newCtx))
	require.Equal(t, baseline.Version(), plan.Snapshot().ModelVersion)
	require.Equal(t, lastGood.Version(), newPlan.Snapshot().ModelVersion)
	for _, name := range []string{"catalog.json", "release-manifest.json", "platform_model_presets.json"} {
		path := filepath.Join(directory, name)
		original, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
		_, err = registry.ReloadApprovedLocalPackage(directory)
		require.Error(t, err)
		require.Same(t, lastGood, registry.Snapshot())
		require.Same(t, baseline, ModelRegistrySnapshotFromContext(ctx))
		require.Same(t, lastGood, ModelRegistrySnapshotFromContext(newCtx))
		require.Same(t, lastGood, newGatewayRequestPlanWithRegistry(GatewayRequestSpec{}, registry).ModelRegistrySnapshot())
		require.NoError(t, os.WriteFile(path, original, 0600))
	}
	// A fresh instance with the same separately deployed pin accepts exactly the
	// same local artifact offline, and reports the same manifest identity.
	restarted := &ModelRegistry{trustedPin: pin}
	reloaded, err := restarted.ReloadApprovedLocalPackage(directory)
	require.NoError(t, err)
	require.Equal(t, lastGood.Version(), reloaded.Version())
	older, _ := registryTestPackage(t, baseline.Generation())
	_, err = registry.ReloadApprovedLocalPackage(older)
	require.Error(t, err)
	require.Same(t, lastGood, registry.Snapshot())
	_, err = registry.ReloadApprovedLocalPackage("https://example.com/package")
	require.Error(t, err)
}

func TestModelRegistryCandidateCopiesAndUnknownPlatform(t *testing.T) {
	snapshot := NewModelRegistry().Snapshot()
	require.Empty(t, snapshot.CandidateIDs("unknown", ""))
	require.Empty(t, defaultModelsListCandidateIDs("unknown"))
	presets := snapshot.Presets()
	presets[0].Models[0].ID = "mutated"
	require.NotEqual(t, "mutated", snapshot.Presets()[0].Models[0].ID)
	require.Equal(t, snapshot.CandidateIDs(PlatformAnthropic, ""), defaultModelsListCandidateIDs(PlatformAnthropic))
}

func TestRegistryPresetLimitsKeepTheirMeaningAndOwnership(t *testing.T) {
	var preset PlatformPresetModel
	require.NoError(t, json.Unmarshal([]byte(`{"id":"private-video","canonical_model_id":"canonical-video","context_window":4096,"context_limit_kind":"total","task_type":"video_generation","task_limits":{"max_duration_seconds":30},"input_modalities_status":"partial","reasoning_control":{"kind":"unknown"},"embedding_dimensions":128,"retirement_date":"2027-01-01"}`), &preset))
	require.Equal(t, "canonical-video", preset.CanonicalModelID)
	require.Equal(t, int64(128), preset.EmbeddingDimensions)
	require.Equal(t, "2027-01-01", preset.RetirementDate)
	record := fillCapabilityFields(CapabilityRecord{Profile: "private", ModelID: preset.ID, Source: "platform-preset"}, CapabilityRecord{ContextWindow: preset.ContextWindow, ContextLimitKind: preset.ContextLimitKind, Mode: preset.TaskType, TaskLimits: preset.TaskLimits})
	require.Nil(t, record.MaxInputTokens, "a total context limit is not an input limit")
	snapshot := &ModelCapabilitySnapshot{records: map[string]CapabilityRecord{capabilityKey("private", preset.ID): record}}
	match := lookupCapabilityMetadata(snapshot, "private", preset.ID)
	require.Equal(t, "matched", match.Status)
	require.Equal(t, "total", match.Metadata.ContextLimitKind)
	require.Equal(t, int64(4096), match.Metadata.ContextWindow)
	require.JSONEq(t, "30", string(match.Metadata.TaskLimits["max_duration_seconds"]))
	match.Metadata.TaskLimits["max_duration_seconds"][0] = '9'
	unchanged := lookupCapabilityMetadata(snapshot, "private", preset.ID)
	require.JSONEq(t, "30", string(unchanged.Metadata.TaskLimits["max_duration_seconds"]))
}

func TestRegistryRejectsRepinnedRollbackAndSameGenerationMutation(t *testing.T) {
	baseline := NewModelRegistry().Snapshot()
	for _, generation := range []int64{baseline.Generation() - 1, baseline.Generation()} {
		directory, pin := registryTestPackage(t, generation)
		// Even independently pinning a newly serialized package cannot reuse or
		// lower the generation of the last-good release.
		registry := &ModelRegistry{trustedPin: pin}
		registry.current.Store(baseline)
		_, err := registry.ReloadApprovedLocalPackage(directory)
		require.Error(t, err)
		require.Same(t, baseline, registry.Snapshot())
	}
}

func TestTokenHubCapabilityProfileUsesExactHosts(t *testing.T) {
	for _, host := range []string{"tokenhub.tencentmaas.com", "tokenhub.tencentmaas.cn", "tokenhub-intl.tencentmaas.com", "tokenhub-intl.tencentmaas.cn"} {
		account := &Account{Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://" + host + "/v1"}}
		if host == "tokenhub.tencentmaas.com" {
			require.Equal(t, "hunyuan", resolveCapabilityProfile(account))
		} else {
			// Alternate regions have routing evidence, but no pinned metadata identity.
			require.Equal(t, "custom:hunyuan:https://"+host+"/v1", resolveCapabilityProfile(account))
		}
		for _, spoofed := range []string{host + ".example.com", "not-" + host, "proxy.example.com/" + host} {
			account.Credentials["base_url"] = "https://" + spoofed + "/v1"
			require.NotEqual(t, "hunyuan", resolveCapabilityProfile(account))
		}
	}
}

func TestCapabilityOnlyDownloadStagesReviewWithoutPublishing(t *testing.T) {
	body := []byte(`{"spec-only":{"litellm_provider":"private","max_input_tokens":42}}`)
	pricing := newCapabilityPricing(t, &capabilityRemote{body: body, hash: capabilityHash(body)})
	baseline := pricing.CapabilitySnapshot()
	require.Error(t, pricing.downloadPricingData(), "capability observations cannot satisfy price admission")
	require.Same(t, baseline, pricing.CapabilitySnapshot())
	staged, err := os.ReadFile(pricing.getPricingFilePath() + ".capabilities.pending.json")
	require.NoError(t, err)
	var pending struct {
		Status            string          `json:"status"`
		BaseGeneration    int64           `json:"base_generation"`
		ChangedIdentities []string        `json:"changed_identities"`
		Input             json.RawMessage `json:"input"`
	}
	require.NoError(t, json.Unmarshal(staged, &pending))
	require.Equal(t, "pending_review", pending.Status)
	require.Equal(t, baseline.generation, pending.BaseGeneration)
	require.Equal(t, []string{capabilityKey("private", "spec-only")}, pending.ChangedIdentities)
	require.JSONEq(t, string(body), string(pending.Input))
	require.NoFileExists(t, pricing.getPricingFilePath(), "failed price admission does not overwrite the pricing cache")
}
