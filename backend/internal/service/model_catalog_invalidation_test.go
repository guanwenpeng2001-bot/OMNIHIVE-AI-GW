package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"testing"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

type catalogGenerationFixture struct {
	AccountRepository
	generation int64
	versionErr error
	sourceErr  error
	accounts   []Account
	reads      int
	started    chan struct{}
	release    chan struct{}
}

func (fixture *catalogGenerationFixture) ModelCatalogGeneration(context.Context, string) (int64, error) {
	return fixture.generation, fixture.versionErr
}
func (fixture *catalogGenerationFixture) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	fixture.reads++
	snapshot := append([]Account(nil), fixture.accounts...)
	if fixture.started != nil {
		close(fixture.started)
		fixture.started = nil
		<-fixture.release
	}
	return snapshot, fixture.sourceErr
}
func (fixture *catalogGenerationFixture) ListSchedulable(ctx context.Context) ([]Account, error) {
	return fixture.ListSchedulableByGroupID(ctx, 0)
}
func catalogFixtureAccount(model string) Account {
	return Account{ID: 1, Platform: PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{model: model}}}
}
func catalogFixtureGateway(repository AccountRepository) *GatewayService {
	return &GatewayService{accountRepo: repository, modelsListCache: gocache.New(time.Hour, 0), modelsListCacheTTL: time.Hour}
}

func TestModelCatalogGenerationTwoInstancesWithoutNotifications(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	repository := &catalogGenerationFixture{generation: 1, accounts: []Account{catalogFixtureAccount("old")}}
	first, second := catalogFixtureGateway(repository), catalogFixtureGateway(repository)
	for _, gateway := range []*GatewayService{first, second} {
		models, err := gateway.GetAvailableModelsChecked(ctx, &groupID, PlatformAnthropic)
		require.NoError(t, err)
		require.Equal(t, []string{"old"}, models)
		ownership, err := gateway.resolveCompositeModelOwnership(ctx, groupID, "old")
		require.NoError(t, err)
		require.True(t, ownership.Matched)
	}
	repository.accounts = []Account{catalogFixtureAccount("new")}
	repository.generation++
	for _, gateway := range []*GatewayService{first, second} {
		models, err := gateway.GetAvailableModelsChecked(ctx, &groupID, PlatformAnthropic)
		require.NoError(t, err)
		require.Equal(t, []string{"new"}, models)
		ownership, err := gateway.resolveCompositeModelOwnership(ctx, groupID, "old")
		require.NoError(t, err)
		require.False(t, ownership.Matched)
	}
	repository.accounts = nil
	repository.generation++
	models, err := second.GetAvailableModelsChecked(ctx, &groupID, PlatformAnthropic)
	require.NoError(t, err)
	require.Empty(t, models)
}
func TestModelCatalogVersionFailureFallsBackAndPropagatesSourceFailure(t *testing.T) {
	repository := &catalogGenerationFixture{generation: 1, accounts: []Account{catalogFixtureAccount("old")}}
	gateway := catalogFixtureGateway(repository)
	ctx := context.Background()
	_, err := gateway.GetAvailableModelsChecked(ctx, nil, PlatformAnthropic)
	require.NoError(t, err)
	repository.versionErr = errors.New("version unavailable")
	repository.accounts = []Account{catalogFixtureAccount("fresh")}
	models, err := gateway.GetAvailableModelsChecked(ctx, nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, []string{"fresh"}, models)
	repository.sourceErr = errors.New("source unavailable")
	_, err = gateway.GetAvailableModelsChecked(ctx, nil, PlatformAnthropic)
	require.ErrorIs(t, err, repository.sourceErr)
}
func TestModelCatalogOldFillCannotBecomeCurrent(t *testing.T) {
	repository := &catalogGenerationFixture{generation: 1, accounts: []Account{catalogFixtureAccount("old")}, started: make(chan struct{}), release: make(chan struct{})}
	started := repository.started
	gateway := catalogFixtureGateway(repository)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = gateway.GetAvailableModelsChecked(context.Background(), nil, PlatformAnthropic)
	}()
	<-started
	repository.generation = 2
	repository.accounts = []Account{catalogFixtureAccount("new")}
	close(repository.release)
	<-done
	models, err := gateway.GetAvailableModelsChecked(context.Background(), nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, []string{"new"}, models)
}

type catalogRuleFixture struct {
	ErrorPassthroughRepository
	generation int64
	rules      []*model.ErrorPassthroughRule
	err        error
}

func (fixture *catalogRuleFixture) ModelCatalogGeneration(context.Context, string) (int64, error) {
	return fixture.generation, nil
}
func (fixture *catalogRuleFixture) List(context.Context) ([]*model.ErrorPassthroughRule, error) {
	return fixture.rules, fixture.err
}
func TestModelCatalogErrorRulesUseAuthoritativeGeneration(t *testing.T) {
	repository := &catalogRuleFixture{generation: 1, rules: []*model.ErrorPassthroughRule{{ID: 1, Enabled: true, PassthroughCode: true, ErrorCodes: []int{429}}}}
	first := &ErrorPassthroughService{repo: repository}
	second := &ErrorPassthroughService{repo: repository}
	require.NotNil(t, first.MatchRule("openai", 429, nil))
	require.NotNil(t, second.MatchRule("openai", 429, nil))
	repository.rules = nil
	repository.generation++
	require.Nil(t, first.MatchRule("openai", 429, nil))
	require.Nil(t, second.MatchRule("openai", 429, nil))
	repository.err = errors.New("source unavailable")
	repository.generation++
	require.Nil(t, second.MatchRule("openai", 429, nil))
}

type catalogChannelGroupFixture struct {
	GroupRepository
	generation int64
}

func (fixture *catalogChannelGroupFixture) ModelCatalogGeneration(context.Context, string) (int64, error) {
	return fixture.generation, nil
}

type catalogChannelFixture struct {
	ChannelRepository
	channels []Channel
	err      error
}

func (fixture *catalogChannelFixture) ListAll(context.Context) ([]Channel, error) {
	return fixture.channels, fixture.err
}
func (fixture *catalogChannelFixture) GetGroupPlatforms(context.Context, []int64) (map[int64]string, error) {
	return map[int64]string{1: PlatformOpenAI}, nil
}
func TestModelCatalogChannelsUseAuthoritativeGeneration(t *testing.T) {
	groups := &catalogChannelGroupFixture{generation: 1}
	repository := &catalogChannelFixture{channels: []Channel{{ID: 1, Name: "old", GroupIDs: []int64{1}}}}
	first := &ChannelService{repo: repository, groupRepo: groups}
	second := &ChannelService{repo: repository, groupRepo: groups}
	for _, instance := range []*ChannelService{first, second} {
		cached, err := instance.loadCache(context.Background())
		require.NoError(t, err)
		require.Equal(t, "old", cached.byID[1].Name)
	}
	repository.channels = []Channel{{ID: 1, Name: "new", GroupIDs: []int64{1}}}
	groups.generation++
	for _, instance := range []*ChannelService{first, second} {
		cached, err := instance.loadCache(context.Background())
		require.NoError(t, err)
		require.Equal(t, "new", cached.byID[1].Name)
	}
	repository.err = errors.New("source unavailable")
	groups.generation++
	_, err := second.loadCache(context.Background())
	require.Error(t, err)
}

type catalogRuntimeOwnerFixture struct {
	AccountRepository
	accounts map[int64]*Account
}

func (fixture *catalogRuntimeOwnerFixture) GetByID(_ context.Context, id int64) (*Account, error) {
	return fixture.accounts[id], nil
}
func (fixture *catalogRuntimeOwnerFixture) ListShadowsByParent(_ context.Context, id int64) ([]*Account, error) {
	var shadows []*Account
	for _, account := range fixture.accounts {
		if account.ParentAccountID != nil && *account.ParentAccountID == id {
			snapshot := *account
			snapshot.Proxy = nil
			shadows = append(shadows, &snapshot)
		}
	}
	return shadows, nil
}
func TestModelCatalogAdminNotesPreserveParentAndShadowRuntime(t *testing.T) {
	parentID, proxyID := int64(1), int64(9)
	parent := &Account{ID: parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "parent"}}
	shadow := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID, QuotaDimension: "spark", ProxyID: &proxyID, Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "proxy.invalid", Port: 8080}}
	bound, err := AccountWithRuntimeCredentialOwner(shadow, parent)
	require.NoError(t, err)
	usage := &UsageCache{}
	for _, account := range []*Account{parent, bound} {
		usage.apiCache.Store(AccountRuntimeIdentityKey(account), "keep")
	}
	repository := &catalogRuntimeOwnerFixture{accounts: map[int64]*Account{1: parent, 2: shadow}}
	admin := &adminServiceImpl{accountRepo: repository, runtimeInvalidator: &AccountRuntimeInvalidator{Usage: usage}}
	admin.captureRuntimeInvalidation(context.Background(), parent)()
	admin.captureRuntimeInvalidation(context.Background(), shadow)()
	for _, account := range []*Account{parent, bound} {
		_, found := usage.apiCache.Load(AccountRuntimeIdentityKey(account))
		require.True(t, found, "unchanged owner/proxy snapshots must not invalidate usage")
	}
}
