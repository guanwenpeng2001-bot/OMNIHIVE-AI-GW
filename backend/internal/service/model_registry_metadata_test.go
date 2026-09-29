package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	modelcapabilities "github.com/Wei-Shaw/sub2api/resources/model-capabilities"
	"github.com/stretchr/testify/require"
)

func TestRegistryNativeCreationAndVersionFacts(t *testing.T) {
	for _, scenario := range []struct {
		name, body, created, version string
	}{
		{"openai creation", `{"data":[{"id":"native","created":1704067200}]}`, "2024-01-01T00:00:00Z", ""},
		{"anthropic creation", `{"data":[{"id":"native","created_at":"2024-01-01T08:00:00+08:00"}]}`, "2024-01-01T00:00:00Z", ""},
		{"gemini version", `{"models":[{"name":"models/native","version":"001","supportedGenerationMethods":["generateContent","countTokens"]}]}`, "", "001"},
		{"not an observation time", `{"data":[{"id":"native","checked_at":"2024-01-01T00:00:00Z","observed_at":"2024-01-01T00:00:00Z"}]}`, "", ""},
		{"invalid date", `{"data":[{"id":"native","created_at":"not-a-date","created":-1}]}`, "", ""},
		{"no name guessing", `{"data":[{"id":"native-20240101"}]}`, "", ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			models, metadata, err := extractUpstreamModelCatalog([]byte(scenario.body), false)
			require.NoError(t, err)
			require.Len(t, models, 1)
			fact := metadata[models[0]]
			require.Equal(t, scenario.created, fact.CreatedAt)
			require.Equal(t, scenario.version, fact.ModelVersion)
			stampCapabilityEvidence(&fact, "upstream", "native-body-hash", "gemini", models[0], "native_id", "verified")
			account := &Account{Extra: map[string]any{}}
			account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Source: "upstream", SyncedAt: "2030-01-01T00:00:00Z", Models: map[string]UpstreamModelMetadata{models[0]: fact}})
			persisted := account.GetUpstreamModelMetadataSnapshot().Models[models[0]]
			projection := visibleModelMetadata(nil, "gemini", models[0], persisted)
			require.Equal(t, scenario.created, projection.CreatedAt)
			require.Equal(t, scenario.version, projection.ModelVersion)
			if scenario.created == "" {
				require.Equal(t, "unavailable", projection.CreatedAtStatus)
			} else {
				require.Equal(t, "available", projection.CreatedAtStatus)
				require.Equal(t, "upstream", projection.FieldEvidence["created_at"].Source)
			}
			if scenario.version == "" {
				require.Equal(t, "unavailable", projection.ModelVersionStatus)
			} else {
				require.Equal(t, "available", projection.ModelVersionStatus)
				require.Equal(t, "upstream", projection.FieldEvidence["model_version"].Source)
			}
		})
	}
}

func TestRegistryApprovedIdentityFactsAndFrozenReload(t *testing.T) {
	baseline := NewModelRegistry().Snapshot()
	directory, _ := registryTestPackage(t, baseline.Generation()+1)
	presets := baseline.Presets()
	presets[0].Models[0].CreatedAt = "2024-01-01T00:00:00Z"
	presets[0].Models[0].ModelVersion = "reviewed-revision"
	presetsBody, err := json.Marshal(presets)
	require.NoError(t, err)
	manifestBody, err := os.ReadFile(filepath.Join(directory, "release-manifest.json"))
	require.NoError(t, err)
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(manifestBody, &manifest))
	manifest["presets_sha256"] = capabilityHash(presetsBody)
	manifestBody, err = json.Marshal(manifest)
	require.NoError(t, err)
	pin, err := json.Marshal(modelcapabilities.ReleaseIdentity{ApprovedGeneration: baseline.Generation() + 1, ManifestSHA256: capabilityHash(manifestBody)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "platform_model_presets.json"), presetsBody, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "release-manifest.json"), manifestBody, 0600))
	registry := &ModelRegistry{trustedPin: pin}
	registry.current.Store(baseline)
	frozen := WithModelRegistrySnapshot(context.Background(), baseline)
	snapshot, err := registry.ReloadApprovedLocalPackage(directory)
	require.NoError(t, err)
	profile := presets[0].ProviderProfile
	if profile == "native" {
		profile = presets[0].Platform
	}
	model := presets[0].Models[0].ID
	projection := visibleModelMetadata(snapshot, profile, model, UpstreamModelMetadata{})
	require.Equal(t, "2024-01-01T00:00:00Z", projection.CreatedAt)
	require.Equal(t, "reviewed-revision", projection.ModelVersion)
	require.Equal(t, "platform-preset", projection.FieldEvidence["created_at"].Source)
	require.Equal(t, capabilityHash(presetsBody), projection.FieldEvidence["created_at"].ContentHash)
	require.Equal(t, snapshot.Version(), projection.FieldEvidence["model_version"].SourceVersion)
	require.Same(t, baseline, ModelRegistrySnapshotFromContext(frozen))
	require.Empty(t, visibleModelMetadata(baseline, profile, model, UpstreamModelMetadata{}).CreatedAt)
	projection.FieldEvidence["created_at"] = ModelCapabilityEvidence{Source: "mutated"}
	require.Equal(t, "platform-preset", visibleModelMetadata(snapshot, profile, model, UpstreamModelMetadata{}).FieldEvidence["created_at"].Source)
	native := UpstreamModelMetadata{CreatedAt: "2023-01-01T00:00:00Z", ModelVersion: "native-revision", FieldEvidence: map[string]ModelCapabilityEvidence{"created_at": {Source: "upstream"}}}
	merged, changed := mergeUpstreamModelMetadata(UpstreamModelMetadata{}, native)
	require.True(t, changed)
	require.Equal(t, native.CreatedAt, merged.CreatedAt)
	require.Equal(t, "upstream", merged.FieldEvidence["created_at"].Source)
	require.Equal(t, "native-revision", visibleModelMetadata(snapshot, profile, model, native).ModelVersion)
	// Invalid but repinned metadata fails atomically, retaining all prior facts.
	presets[0].Models[0].CreatedAt = "invalid"
	presetsBody, err = json.Marshal(presets)
	require.NoError(t, err)
	manifest["presets_sha256"] = capabilityHash(presetsBody)
	manifestBody, err = json.Marshal(manifest)
	require.NoError(t, err)
	registry.trustedPin, err = json.Marshal(modelcapabilities.ReleaseIdentity{ApprovedGeneration: baseline.Generation() + 1, ManifestSHA256: capabilityHash(manifestBody)})
	require.NoError(t, err)
	catalogBody, err := os.ReadFile(filepath.Join(directory, "catalog.json"))
	require.NoError(t, err)
	_, err = registry.build(catalogBody, manifestBody, presetsBody, nil)
	require.ErrorContains(t, err, "creation time")
	require.Same(t, snapshot, registry.Snapshot())
}

func TestVisibleModelsRetainEveryEndpointAndAccount(t *testing.T) {
	first := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	second := catalogEvidenceAccount(2, PlatformOpenAI, "native")
	first.Credentials["model_mapping"] = map[string]any{"public": "native"}
	second.Credentials["model_mapping"] = map[string]any{"public": "native"}
	secondSnapshot, _ := second.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
	secondSnapshot.EligibleModels[0].Endpoints = []string{"embeddings", "images"}
	first.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"native": {ProviderProfile: resolveCapabilityProfile(&first), CreatedAt: "2024-01-01T00:00:00Z", ModelVersion: "native-version"}}})
	repository := &visiblePolicyAccountRepo{accounts: []Account{first, second}, generation: 10}
	gateway := &GatewayService{accountRepo: repository}
	view, metadata, err := gateway.VisibleModelsWithMetadata(context.Background(), nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, []string{"public"}, view.Models)
	require.Equal(t, []string{"chat_completions", "count_tokens", "embeddings", "images", "messages", "responses"}, view.ExecutableEndpoints["public"])
	require.Len(t, view.Bindings["public"], 6)
	require.Len(t, metadata["public"], 6)
	for index, binding := range view.Bindings["public"] {
		fact := metadata["public"][index]
		if binding.AccountID == first.ID {
			require.Equal(t, "native-version", fact.ModelVersion)
			require.Equal(t, "available", fact.CreatedAtStatus)
		} else {
			require.Equal(t, "unavailable", fact.CreatedAtStatus)
			require.Equal(t, "unavailable", fact.ModelVersionStatus)
		}
	}
	plain, err := gateway.VisibleModels(context.Background(), nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, view.Bindings, plain.Bindings)
	require.Equal(t, view.ExecutableEndpoints, plain.ExecutableEndpoints)
	view.ExecutableEndpoints["public"][0] = "mutated"
	metadata["public"][0].ModelVersion = "mutated"
	unchanged, facts, err := gateway.VisibleModelsWithMetadata(context.Background(), nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, plain.ExecutableEndpoints, unchanged.ExecutableEndpoints)
	require.Equal(t, "native-version", facts["public"][0].ModelVersion)
	denied, facts, err := gateway.VisibleModelsWithMetadata(WithVisibleModelKeyPolicy(context.Background(), []string{}), nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Empty(t, denied.Models)
	require.Empty(t, denied.ExecutableEndpoints)
	require.Empty(t, facts)
	repository.changeDuringRead = true
	_, _, err = gateway.VisibleModelsWithMetadata(context.Background(), nil, PlatformOpenAI)
	require.ErrorIs(t, err, ErrVisibleModelRevisionChanged)
}

func TestVisibleModelsGeminiMethodEvidenceStaysWithExecutableBinding(t *testing.T) {
	chat := catalogEvidenceAccount(1, PlatformOpenAI, "private-chat")
	gemini := catalogEvidenceAccount(2, PlatformGemini, "private-gemini")
	chat.GroupIDs = []int64{9}
	gemini.GroupIDs = []int64{9}
	gemini.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot).EligibleModels[0].Endpoints = []string{"generate_content"}
	gemini.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"private-gemini": {ProviderProfile: resolveCapabilityProfile(&gemini), ModelVersion: "001", SupportedMethods: []string{"generateContent", "streamGenerateContent", "countTokens"}},
	}})
	group := Group{ID: 9, Platform: PlatformComposite}
	routes := &visiblePolicyRouteRepo{routes: []CompositeModelRoute{
		{PublicModel: "public", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformOpenAI, UpstreamModel: "private-chat", Endpoint: "responses", Enabled: true},
		{PublicModel: "public", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformGemini, UpstreamModel: "private-gemini", Endpoint: "gemini", Enabled: true},
	}}
	gateway := &GatewayService{accountRepo: &visiblePolicyAccountRepo{accounts: []Account{chat, gemini}}, groupRepo: &visiblePolicyGroupRepo{groups: []Group{group}}, compositeResolver: NewCompositeRouteResolver(routes)}
	view, metadata, err := gateway.VisibleModelsWithMetadata(context.Background(), &group.ID, PlatformComposite)
	require.NoError(t, err)
	require.Equal(t, []string{"gemini", "responses"}, view.ExecutableEndpoints["public"])
	require.Len(t, view.Bindings["public"], 2)
	require.Len(t, metadata["public"], 2)
	for index, binding := range view.Bindings["public"] {
		if binding.Endpoint == CompositeRouteEndpointGemini {
			require.Equal(t, "001", metadata["public"][index].ModelVersion)
			require.Equal(t, []string{"generateContent", "streamGenerateContent", "countTokens"}, metadata["public"][index].SupportedMethods)
		} else {
			require.Empty(t, metadata["public"][index].SupportedMethods)
		}
	}
	// Revoking the Gemini route removes its method evidence even though chat
	// still makes the same public model visible under the same account facts.
	routes.routes = routes.routes[:1]
	view, metadata, err = gateway.VisibleModelsWithMetadata(context.Background(), &group.ID, PlatformComposite)
	require.NoError(t, err)
	require.Equal(t, []string{"responses"}, view.ExecutableEndpoints["public"])
	require.Len(t, metadata["public"], 1)
	require.Empty(t, metadata["public"][0].SupportedMethods)
}
