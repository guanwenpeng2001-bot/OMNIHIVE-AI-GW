package service

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountBindingComputationReusesGenerationAndBoundsCapacity(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	prepareCatalogReadAccount(&account, nil)
	ctx := context.WithValue(context.Background(), visibleModelRevisionKey{}, visibleModelRevision{generation: 1, authoritative: true})
	calls := 0
	compute := func() accountBindingMemoValue {
		calls++
		return accountBindingMemoValue{model: "native", ok: true}
	}
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			accountBindingComputation(ctx, &account, "public", AccountModelBindingOptions{}, 0, false, compute)
		})
	}
	workers.Wait()
	require.Equal(t, 1, calls)
	accountBindingComputation(ctx, &account, "public", AccountModelBindingOptions{Endpoint: "responses"}, 0, false, compute)
	accountBindingComputation(ctx, &account, "public", AccountModelBindingOptions{Endpoint: "messages"}, 0, false, compute)
	require.Equal(t, 1, calls, "model rewriting is shared across endpoint projections")
	for i := range accountBindingMemoCapacity + 10 {
		accountBindingComputation(ctx, &account, fmt.Sprint(i), AccountModelBindingOptions{}, 0, false, compute)
	}
	require.LessOrEqual(t, len(account.modelCatalogReadEvidence.bindings.entries), accountBindingMemoCapacity)
}

func TestAccountBindingComputationInvalidatesGenerationAndIdentity(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	prepareCatalogReadAccount(&account, nil)
	calls := 0
	compute := func() accountBindingMemoValue { calls++; return accountBindingMemoValue{ok: true} }
	ctx := context.WithValue(context.Background(), visibleModelRevisionKey{}, visibleModelRevision{generation: 1, authoritative: true})
	resolve := func() {
		accountBindingComputation(ctx, &account, "public", AccountModelBindingOptions{}, 0, false, compute)
	}
	resolve()
	resolve()
	require.Equal(t, 1, calls)
	ctx = context.WithValue(ctx, visibleModelRevisionKey{}, visibleModelRevision{generation: 2, authoritative: true})
	resolve()
	require.Equal(t, 2, calls)
	// A newly read identity owns new evidence even if the account ID is unchanged.
	account = catalogEvidenceAccount(1, PlatformOpenAI, "native")
	account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 2}
	prepareCatalogReadAccount(&account, nil)
	resolve()
	require.Equal(t, 3, calls)
	require.False(t, ResolveAccountModelBinding(ctx, &account, "native", AccountModelBindingOptions{Endpoint: "responses"}).Supported)
	ctx = context.Background()
	resolve()
	resolve()
	require.Equal(t, 5, calls, "unversioned callers must bypass shared computation")
}

func TestAccountBindingWarmComputationStillChecksAuthorizationAndExpiry(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"native": {ID: "native", ProviderProfile: resolveCapabilityProfile(&account), SupportsVision: new(false)},
	}})
	prepareCatalogReadAccount(&account, nil)
	ctx := context.WithValue(context.Background(), visibleModelRevisionKey{}, visibleModelRevision{generation: 1, authoritative: true})
	options := AccountModelBindingOptions{Endpoint: "responses"}
	require.True(t, ResolveAccountModelBinding(ctx, &account, "native", options).Supported)
	require.NotEmpty(t, account.modelCatalogReadEvidence.bindings.entries)
	denied := context.WithValue(ctx, visibleModelPolicyKey{}, []string{"other"})
	require.False(t, ResolveAccountModelBinding(denied, &account, "native", options).Supported)
	needsImage := context.WithValue(ctx, gatewayInputRequirementKey{}, GatewayInputRequirement{Image: true})
	require.True(t, ResolveAccountModelBinding(needsImage, &account, "native", options).Supported)
	account.ExpiresAt = new(time.Now().Add(-time.Second))
	require.False(t, ResolveAccountModelBinding(ctx, &account, "native", options).Supported)
}

func TestAccountBindingWarmComputationStillChecksAvailability(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI)
	account.Credentials["model_mapping"] = map[string]any{"public": "native"}
	declaration := ModelAvailabilityDeclaration{
		AccountID: account.ID, Model: "native", Endpoint: "responses",
		ConfirmedAt: time.Now().Add(-time.Minute), ConfirmedBy: 1,
		Reason: "confirmed", EvidenceKind: ModelEvidenceAdministrator,
	}
	account.Extra[ModelAvailabilityExtraKey] = []ModelAvailabilityDeclaration{declaration}
	prepareCatalogReadAccount(&account, nil)
	ctx := context.WithValue(context.Background(), visibleModelRevisionKey{}, visibleModelRevision{generation: 1, authoritative: true})
	options := AccountModelBindingOptions{Endpoint: "responses"}
	require.True(t, ResolveAccountModelBinding(ctx, &account, "public", options).Supported)
	declaration.ExpiresAt = new(time.Now().Add(-time.Second))
	account.Extra[ModelAvailabilityExtraKey] = []ModelAvailabilityDeclaration{declaration}
	require.False(t, ResolveAccountModelBinding(ctx, &account, "public", options).Supported)
}

func TestAccountBaseURLReusesIdentityAndReturnsIndependentCopies(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	calls := 0
	parse := func(raw string) (*url.URL, error) { calls++; return url.Parse(raw) }
	const base = "https://example.com/v1?key=value#fragment"
	first, err := account.parseBaseURL(base, parse)
	require.NoError(t, err)
	first.Path = "/changed"
	second, err := account.parseBaseURL(base, parse)
	require.NoError(t, err)
	require.Equal(t, "/v1", second.Path)
	require.Equal(t, 1, calls)
	require.Equal(t, buildOpenAIEndpointURL(base, "/v1/responses"), account.buildOpenAIEndpointURL(base, "/v1/responses"))
	account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 2}
	_, err = account.parseBaseURL(base, parse)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	_, err = account.parseBaseURL("https://changed.example/v2", parse)
	require.NoError(t, err)
	require.Equal(t, 3, calls)
	for i := range accountBaseURLCapacity + 2 {
		_, err = account.parseBaseURL(fmt.Sprintf("https://example.com/%d", i), parse)
		require.NoError(t, err)
	}
	require.LessOrEqual(t, len(account.baseURLMemo.entries), accountBaseURLCapacity)
}
