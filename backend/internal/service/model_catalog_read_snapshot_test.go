package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

type catalogBatchTestRepository struct {
	AccountRepository
	accounts    []Account
	generation  atomic.Int64
	reads       atomic.Int64
	parentReads atomic.Int64
	requested   []int64
	mutex       sync.Mutex
	entered     chan struct{}
	release     chan struct{}
}

func (repository *catalogBatchTestRepository) ModelCatalogGeneration(context.Context, string) (int64, error) {
	return repository.generation.Load(), nil
}
func (repository *catalogBatchTestRepository) ListModelCatalogAccountsForGroups(ctx context.Context, groups []int64) ([]Account, error) {
	repository.mutex.Lock()
	repository.requested = append([]int64(nil), groups...)
	repository.mutex.Unlock()
	return repository.ListModelCatalogAccounts(ctx, nil, nil, true)
}
func (repository *catalogBatchTestRepository) ListModelCatalogAccounts(ctx context.Context, groupID *int64, platforms []string, includeGrouped bool) ([]Account, error) {
	repository.reads.Add(1)
	if repository.entered != nil {
		select {
		case repository.entered <- struct{}{}:
		default:
		}
		select {
		case <-repository.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	result := make([]Account, 0, len(repository.accounts))
	for _, account := range repository.accounts {
		if groupID != nil && !slices.Contains(account.GroupIDs, *groupID) {
			continue
		}
		if len(platforms) > 0 && !slices.Contains(platforms, account.Platform) {
			continue
		}
		// Mirror fresh JSON ownership at the repository boundary.
		encoded, err := json.Marshal(account)
		if err != nil {
			return nil, err
		}
		var decoded Account
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			return nil, err
		}
		result = append(result, decoded)
	}
	return result, nil
}
func (repository *catalogBatchTestRepository) GetModelCatalogAccount(context.Context, int64) (*Account, error) {
	repository.parentReads.Add(1)
	return nil, ErrAccountNotFound
}

func catalogSnapshotTestGateway(repository *catalogBatchTestRepository, groups []Group) *GatewayService {
	return &GatewayService{accountRepo: repository, groupRepo: &visiblePolicyGroupRepo{groups: groups}, modelsListCache: gocache.New(time.Minute, 0)}
}

func TestCatalogSnapshotReprojectsKeyPolicyAndGeneration(tester *testing.T) {
	account := catalogEvidenceAccount(11, PlatformOpenAI, "public", "restricted")
	repository := &catalogBatchTestRepository{accounts: []Account{account}}
	gateway := catalogSnapshotTestGateway(repository, nil)
	ctx := context.Background()
	view, err := gateway.VisibleModels(ctx, nil, PlatformOpenAI)
	require.NoError(tester, err)
	require.Equal(tester, []string{"public", "restricted"}, view.Models)
	view.Models[0] = "caller-change"
	view.Bindings["public"][0].UpstreamModel = "caller-change"
	restricted, err := gateway.VisibleModels(WithVisibleModelKeyPolicy(ctx, []string{"public"}), nil, PlatformOpenAI)
	require.NoError(tester, err)
	require.Equal(tester, []string{"public"}, restricted.Models)
	require.Equal(tester, "public", restricted.Bindings["public"][0].UpstreamModel)
	denied, err := gateway.VisibleModels(WithVisibleModelKeyPolicy(ctx, []string{}), nil, PlatformOpenAI)
	require.NoError(tester, err)
	require.Empty(tester, denied.Models)
	require.Equal(tester, int64(1), repository.reads.Load())
	repository.accounts[0].Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{Result: "credentials_invalid"}
	repository.generation.Add(1)
	revoked, err := gateway.VisibleModels(ctx, nil, PlatformOpenAI)
	require.NoError(tester, err)
	require.Empty(tester, revoked.Models)
	require.Equal(tester, int64(2), repository.reads.Load())
}

func TestCatalogSnapshotScopesWorkspaceAndProtocol(tester *testing.T) {
	repository := &catalogBatchTestRepository{accounts: []Account{catalogEvidenceAccount(11, PlatformOpenAI, "public")}}
	gateway := catalogSnapshotTestGateway(repository, nil)
	for _, ctx := range []context.Context{context.Background(), domain.WithWorkspaceScope(context.Background(), 1), domain.WithWorkspaceScope(context.Background(), 2)} {
		for _, platform := range []string{PlatformOpenAI, PlatformComposite} {
			_, err := gateway.VisibleModels(ctx, nil, platform)
			require.NoError(tester, err)
		}
	}
	require.Equal(tester, int64(6), repository.reads.Load())
}

func TestCatalogSnapshotExpiryRecheckedWithoutGenerationChange(tester *testing.T) {
	expires := time.Now().Add(100 * time.Millisecond)
	account := catalogEvidenceAccount(11, PlatformOpenAI, "public")
	account.ExpiresAt = &expires
	repository := &catalogBatchTestRepository{accounts: []Account{account}}
	gateway := catalogSnapshotTestGateway(repository, nil)
	view, err := gateway.VisibleModels(context.Background(), nil, PlatformOpenAI)
	require.NoError(tester, err)
	require.Equal(tester, []string{"public"}, view.Models)
	time.Sleep(time.Until(expires) + time.Millisecond)
	view, err = gateway.VisibleModels(context.Background(), nil, PlatformOpenAI)
	require.NoError(tester, err)
	require.Empty(tester, view.Models)
	require.Equal(tester, int64(1), repository.reads.Load())
}

func TestCatalogSnapshotSingleflightAndCanceledWaiter(tester *testing.T) {
	repository := &catalogBatchTestRepository{accounts: []Account{catalogEvidenceAccount(11, PlatformOpenAI, "public")}, entered: make(chan struct{}, 1), release: make(chan struct{})}
	gateway := catalogSnapshotTestGateway(repository, nil)
	var workers sync.WaitGroup
	errors := make(chan error, 10)
	for request := 0; request < 10; request++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := gateway.VisibleModels(context.Background(), nil, PlatformOpenAI)
			errors <- err
		}()
	}
	<-repository.entered
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := gateway.VisibleModels(canceled, nil, PlatformOpenAI)
	require.ErrorIs(tester, err, context.Canceled)
	close(repository.release)
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(tester, err)
	}
	require.Equal(tester, int64(1), repository.reads.Load())
}

func TestCatalogBatchFiltersGroupsBeforeLoadingAndReusesAccounts(tester *testing.T) {
	account := catalogEvidenceAccount(11, PlatformOpenAI, "public")
	account.GroupIDs = []int64{1, 2, 3}
	repository := &catalogBatchTestRepository{accounts: []Account{account}}
	groups := []Group{{ID: 1, Platform: PlatformOpenAI}, {ID: 2, Platform: PlatformOpenAI, IsExclusive: true}, {ID: 3, Platform: PlatformAnthropic}}
	gateway := catalogSnapshotTestGateway(repository, groups)
	channels := &visiblePolicyChannelRepo{channels: []Channel{{ID: 7, Status: StatusActive, GroupIDs: []int64{1, 2, 3}, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"public"}}}}}}
	channelService := NewChannelService(channels, gateway.groupRepo, nil, nil, nil)
	channelService.visibleModels = &VisibleModelResolver{gateway: gateway}
	plaza := NewModelPlazaService(channels, gateway.groupRepo, nil, nil, NewModelPricingResolver(channelService, nil))
	visible, err := plaza.ListVisibleGroups(context.Background(), nil, false)
	require.NoError(tester, err)
	require.Equal(tester, []int64{1, 3}, repository.requested)
	require.Len(tester, visible, 1)
	require.Equal(tester, int64(1), visible[0].ID)
	require.Equal(tester, int64(1), repository.reads.Load())
	visible, err = plaza.ListVisibleGroups(context.Background(), map[int64]struct{}{2: {}}, true)
	require.NoError(tester, err)
	require.Equal(tester, []int64{2}, repository.requested)
	require.Len(tester, visible, 1)
	require.Equal(tester, int64(2), visible[0].ID)
	visible, err = plaza.ListVisibleGroups(context.Background(), nil, false)
	require.NoError(tester, err)
	require.Len(tester, visible, 1)
	require.Equal(tester, int64(1), visible[0].ID)
	require.Equal(tester, int64(2), repository.reads.Load())
}

func BenchmarkCatalogReadSnapshot(benchmark *testing.B) {
	modelNames := make([]string, 100)
	for index := range modelNames {
		modelNames[index] = fmt.Sprintf("model-%04d", index)
	}
	groups := make([]Group, 20)
	accounts := make([]Account, 100)
	channels := make([]Channel, 20)
	for index := range groups {
		groupID := int64(index + 1)
		groups[index] = Group{ID: groupID, Name: fmt.Sprintf("group-%02d", index), Platform: PlatformOpenAI}
		channels[index] = Channel{ID: groupID, Status: StatusActive, GroupIDs: []int64{groupID}, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: modelNames}}}
	}
	for index := range accounts {
		accounts[index] = catalogEvidenceAccount(int64(index+1), PlatformOpenAI, modelNames...)
		accounts[index].GroupIDs = []int64{int64(index/5 + 1)}
	}
	for _, surface := range []string{"models", "plaza"} {
		for _, temperature := range []string{"warm", "cold"} {
			benchmark.Run(surface+"/"+temperature, func(benchmark *testing.B) {
				repository := &catalogBatchTestRepository{accounts: accounts}
				gateway := catalogSnapshotTestGateway(repository, groups)
				channelRepo := &visiblePolicyChannelRepo{channels: channels}
				channelService := NewChannelService(channelRepo, gateway.groupRepo, nil, nil, nil)
				channelService.visibleModels = &VisibleModelResolver{gateway: gateway}
				plaza := NewModelPlazaService(channelRepo, gateway.groupRepo, nil, nil, NewModelPricingResolver(channelService, nil))
				invoke := func() error {
					if surface == "plaza" {
						_, err := plaza.ListGroups(context.Background())
						return err
					}
					_, _, err := gateway.VisibleModelsWithMetadata(context.Background(), &groups[0].ID, PlatformOpenAI)
					return err
				}
				if err := invoke(); err != nil {
					benchmark.Fatal(err)
				}
				benchmark.ReportAllocs()
				repository.reads.Store(0)
				benchmark.ResetTimer()
				for iteration := 0; iteration < benchmark.N; iteration++ {
					if temperature == "cold" {
						gateway.modelsListCache.Flush()
					}
					if err := invoke(); err != nil {
						benchmark.Fatal(err)
					}
				}
				benchmark.ReportMetric(float64(repository.reads.Load())/float64(benchmark.N), "account-reads/op")
			})
		}
	}
}

func TestCatalogSnapshotMetadataOwnershipAndAccountPolicy(tester *testing.T) {
	account := catalogEvidenceAccount(11, PlatformOpenAI, "public")
	account.Extra[UpstreamModelMetadataExtraKey] = UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"public": {ProviderProfile: "openai", SupportedMethods: []string{"responses"}, ContextWindow: 128000}}}
	repository := &catalogBatchTestRepository{accounts: []Account{account}}
	gateway := catalogSnapshotTestGateway(repository, nil)
	_, metadata, err := gateway.VisibleModelsWithMetadata(context.Background(), nil, PlatformOpenAI)
	require.NoError(tester, err)
	require.NotEmpty(tester, metadata["public"])
	metadata["public"][0].SupportedMethods[0] = "caller-change"
	_, metadata, err = gateway.VisibleModelsWithMetadata(context.Background(), nil, PlatformOpenAI)
	require.NoError(tester, err)
	require.Equal(tester, "responses", metadata["public"][0].SupportedMethods[0])
	policy := withModelAccessSnapshot(context.Background(), ModelAccessSnapshot{AccountExcludes: map[int64][]string{11: {"public"}}})
	view, err := gateway.VisibleModels(policy, nil, PlatformOpenAI)
	require.NoError(tester, err)
	require.Empty(tester, view.Models)
	require.Equal(tester, int64(1), repository.reads.Load())
}

func TestCatalogSnapshotKeyPolicyRevisionRaceFailsClosed(tester *testing.T) {
	repository := &visiblePolicyAccountRepo{accounts: []Account{catalogEvidenceAccount(11, PlatformOpenAI, "public")}, changeDuringRead: true}
	gateway := &GatewayService{accountRepo: repository}
	_, err := gateway.VisibleModels(WithVisibleModelKeyPolicy(context.Background(), nil), nil, PlatformOpenAI)
	require.ErrorIs(tester, err, ErrVisibleModelRevisionChanged)
}

func TestCatalogBatchIncludesUnboundCredentialOwner(tester *testing.T) {
	parent := catalogEvidenceAccount(11, PlatformOpenAI, "public")
	parent.Type = AccountTypeOAuth
	shadow := catalogEvidenceAccount(12, PlatformOpenAI, "public")
	shadow.ParentAccountID = &parent.ID
	shadow.GroupIDs = []int64{1}
	catalog := shadow.Extra[UpstreamModelCatalogExtraKey].(*AccountModelCatalogSnapshot)
	catalog.CredentialAccountID = parent.ID
	repository := &catalogBatchTestRepository{accounts: []Account{shadow, parent}}
	groups := []Group{{ID: 1, Platform: PlatformOpenAI}}
	gateway := catalogSnapshotTestGateway(repository, groups)
	resolver := &VisibleModelResolver{gateway: gateway}
	frozen, err := resolver.freeze(context.Background())
	require.NoError(tester, err)
	frozen, err = resolver.preloadCatalogGroups(frozen, groups)
	require.NoError(tester, err)
	batch := frozen.Value(catalogGroupSnapshotKey{}).(*catalogGroupSnapshot)
	require.Len(tester, batch.accounts[1], 1)
	require.NotNil(tester, batch.accounts[1][0].modelCatalogCredentialAccount)
	require.Equal(tester, parent.ID, batch.accounts[1][0].modelCatalogCredentialAccount.ID)
	require.Zero(tester, repository.parentReads.Load())
}
