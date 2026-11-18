//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	modelcapabilities "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-capabilities"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestModelMethods_PresetsDoNotGrantAccountEligibility(t *testing.T) {
	for _, platform := range []string{PlatformOpenCodeGo, PlatformCommandCode} {
		t.Run(platform, func(t *testing.T) {
			candidates := NewModelRegistry().Snapshot().CandidateIDs(platform, "")
			require.NotEmpty(t, candidates)
			account := catalogEvidenceAccount(1, platform, candidates[0])
			gateway := &GatewayService{accountRepo: &visiblePolicyAccountRepo{accounts: []Account{account}}}
			view, err := gateway.VisibleModels(context.Background(), nil, platform)
			require.NoError(t, err)
			require.Equal(t, []string{candidates[0]}, view.Models)
			require.False(t, ResolveAccountModelBinding(context.Background(), &account, candidates[1], AccountModelBindingOptions{Endpoint: "chat_completions"}).Supported)
			account.Extra = map[string]any{}
			gateway.accountRepo = &visiblePolicyAccountRepo{accounts: []Account{account}}
			view, err = gateway.VisibleModels(context.Background(), nil, platform)
			require.NoError(t, err)
			require.Empty(t, view.Models, "reviewed candidates cannot establish credential permission")
		})
	}
}

func TestModelMethods_CommandCodeResponsesHonorsExplicitNativeProtocol(t *testing.T) {
	account := commandCodeTestAccount()
	presetMethods := map[string][]string{}
	for _, profile := range NewModelRegistry().Snapshot().Presets() {
		if profile.Platform == PlatformCommandCode {
			for _, model := range profile.Models {
				presetMethods[model.ID] = model.Methods
			}
		}
	}
	require.Contains(t, presetMethods["gpt-6-astra"], "responses")
	// Presets alone do not select a model's native endpoint or grant permission.
	require.Equal(t, APIProtocolChatCompletions, account.ResolveCommandCodeUpstreamProtocol("gpt-6-astra"))
	require.Equal(t, APIProtocolAnthropic, account.ResolveCommandCodeUpstreamProtocol("claude-sonnet-5"))
	descriptor, found := domain.LookupPlatform(PlatformCommandCode)
	require.True(t, found)
	require.Equal(t, domain.EndpointNative, descriptor.ResponsesPOST)

	for _, target := range []string{"gpt-6-astra", "private/exact-model"} {
		t.Run(target, func(t *testing.T) {
			account := commandCodeTestAccount()
			account.Credentials["protocol_rules"] = []any{map[string]any{"pattern": "*", "protocol": APIProtocolResponses}}
			account.Credentials["model_mapping"] = map[string]any{"public-model": target}
			require.Nil(t, account.GetUpstreamModelMetadataSnapshot())
			require.Equal(t, APIProtocolResponses, account.ResolveCommandCodeUpstreamProtocol(target))
			require.Equal(t, APIProtocolResponses, account.ResolveCommandCodeUpstreamProtocol("claude-sonnet-5"), "explicit rules precede the default Claude route")
			require.False(t, ResolveAccountModelBinding(context.Background(), account, "unselected-model", AccountModelBindingOptions{Endpoint: "responses"}).Supported, "a protocol rule cannot authorize an unselected model")

			body := []byte(`{"model":"public-model","input":"hi","stream":false}`)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"id":"fixture","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"native response"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
			}}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			request := adaptiveProtocolTestContext("/v1/responses", body)
			SetOpenAIClientTransport(request, OpenAIClientTransportHTTP)
			result, err := gateway.Forward(context.Background(), request, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, DefaultCommandCodeBaseURL+"/responses", upstream.lastReq.URL.String())
			require.Equal(t, target, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, "hi", gjson.GetBytes(upstream.lastBody, "input").String())
			require.False(t, gjson.GetBytes(upstream.lastBody, "messages").Exists(), "native Responses must not be silently converted to Chat")
		})
	}
}

func TestModelMethods_UnknownEndpointDoesNotBecomeNativeSupport(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformCommandCode, "gpt-6-astra")
	snapshot := account.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
	snapshot.EligibleModels[0].Endpoints = []string{"future_endpoint"}
	require.False(t, ResolveAccountModelBinding(context.Background(), &account, "gpt-6-astra", AccountModelBindingOptions{Endpoint: "future_endpoint"}).Supported)
	require.False(t, ResolveAccountModelBinding(context.Background(), &account, "gpt-6-astra", AccountModelBindingOptions{Endpoint: "responses"}).Supported)
	descriptor := domain.PlatformAction(domain.ActionIdentity{Platform: PlatformCommandCode}, "future_endpoint")
	require.NotEqual(t, domain.ActionImplemented, descriptor.Status)
	for _, profile := range NewModelRegistry().Snapshot().Presets() {
		if profile.Platform == PlatformCommandCode {
			for _, model := range profile.Models {
				if model.ID == "claude-haiku-4-5" {
					require.Empty(t, model.Methods, "dated provider identity is not a proven alias")
				}
			}
		}
	}
}

func TestModelMethods_ExistingSnapshotSurvivesRejectedRelease(t *testing.T) {
	baseline := NewModelRegistry().Snapshot()
	directory, pin := registryTestPackage(t, baseline.Generation()+1)
	registry := &ModelRegistry{trustedPin: pin}
	registry.current.Store(baseline)
	requestContext := WithModelRegistrySnapshot(context.Background(), baseline)
	for _, name := range []string{"catalog.json", "release-manifest.json", "platform_model_presets.json"} {
		path := filepath.Join(directory, name)
		original, err := os.ReadFile(path)
		require.NoError(t, err)
		// Even a valid JSON whitespace byte changes the approved content hash.
		require.NoError(t, os.WriteFile(path, append(append([]byte(nil), original...), ' '), 0600))
		_, err = registry.ReloadApprovedLocalPackage(directory)
		require.Error(t, err, name)
		require.Same(t, baseline, registry.Snapshot())
		require.NoError(t, os.Remove(path))
		_, err = registry.ReloadApprovedLocalPackage(directory)
		require.Error(t, err, name)
		require.Same(t, baseline, registry.Snapshot())
		require.NoError(t, os.WriteFile(path, original, 0600))
	}
	// A pin in the package cannot replace the independently compiled pin.
	require.NoError(t, os.WriteFile(filepath.Join(directory, "release-pin.json"), pin, 0600))
	oldBinary := &ModelRegistry{trustedPin: modelcapabilities.TrustedPinJSON()}
	oldBinary.current.Store(baseline)
	_, err := oldBinary.ReloadApprovedLocalPackage(directory)
	require.Error(t, err)
	require.Same(t, baseline, oldBinary.Snapshot())
	// Mixing the candidate presets/catalog with the previous manifest fails.
	manifestPath := filepath.Join(directory, "release-manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, modelcapabilities.ManifestJSON(), 0600))
	_, err = registry.ReloadApprovedLocalPackage(directory)
	require.Error(t, err)
	require.Same(t, baseline, registry.Snapshot())
	require.NoError(t, os.WriteFile(manifestPath, manifest, 0600))
	approved, err := registry.ReloadApprovedLocalPackage(directory)
	require.NoError(t, err)
	require.Equal(t, baseline.Generation()+1, approved.Generation())
	require.Same(t, approved, registry.Snapshot())
	require.Same(t, baseline, ModelRegistrySnapshotFromContext(requestContext))
}
