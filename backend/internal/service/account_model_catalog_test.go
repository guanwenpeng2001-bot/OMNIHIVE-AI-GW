package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

type catalogFixtureRepository struct {
	AccountRepository
	mutex    sync.Mutex
	accounts map[int64]*Account
	commits  int
}

func (repository *catalogFixtureRepository) CommitAccountModelCatalog(_ context.Context, id int64, baseline AccountModelCatalogBaseline, snapshot *AccountModelCatalogSnapshot, state AccountModelCatalogState, metadata *UpstreamModelMetadataSnapshot, _ bool) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	account := repository.accounts[id]
	if baseline.RequireActive {
		if account == nil || account.Status != StatusActive {
			return ErrAccountModelCatalogConflict
		}
		baseline.RequireActive = false
	}
	if account == nil || account.ModelCatalogBaseline() != baseline {
		return ErrAccountModelCatalogConflict
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[UpstreamModelCatalogStateExtraKey] = state
	if snapshot != nil {
		account.Extra[UpstreamModelCatalogExtraKey] = snapshot
	}
	if metadata != nil {
		account.Extra[UpstreamModelMetadataExtraKey] = metadata
	}
	repository.commits++
	return nil
}

func catalogFixtureResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
}

func TestAccountModelCatalogPreviewApplyShrinkEmptyFailureIsolation(t *testing.T) {
	account := &Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://api.openai.com", "model_mapping": map[string]any{"alias": "kept"}}}
	other := &Account{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "other"}}
	repository := &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account, other.ID: other}}
	entry := func(ids ...string) string {
		models := []map[string]any{}
		for _, id := range ids {
			models = append(models, map[string]any{"id": id, "reasoning": false, "input_modalities": []string{"text"}, "modalities": map[string]any{"output": []string{"text"}}, "context_window": 1000, "max_output_tokens": 100, "supports_function_calling": false})
		}
		body, err := json.Marshal(map[string]any{"data": models})
		require.NoError(t, err)
		return string(body)
	}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{catalogFixtureResponse(entry("kept", "removed")), catalogFixtureResponse(entry("kept", "removed")), catalogFixtureResponse(entry("kept")), catalogFixtureResponse(entry()), {StatusCode: 502, Body: io.NopCloser(strings.NewReader("failed"))}}}
	service := &AccountTestService{accountRepo: repository, httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	original, err := json.Marshal(account)
	require.NoError(t, err)
	preview, err := service.SyncUpstreamModelCatalog(context.Background(), account)
	require.NoError(t, err)
	require.False(t, preview.Applied)
	require.Zero(t, repository.commits)
	after, err := json.Marshal(account)
	require.NoError(t, err)
	require.JSONEq(t, string(original), string(after))
	applied, err := service.ApplyAccountModelCatalog(context.Background(), account, nil)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	require.Equal(t, int64(1), applied.SnapshotRevision)
	require.ElementsMatch(t, []string{"kept", "removed"}, applied.Models)
	applied, err = service.ApplyAccountModelCatalog(context.Background(), account, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"removed"}, applied.Diff.Removed)
	require.Equal(t, []string{"kept"}, applied.Models)
	applied, err = service.ApplyAccountModelCatalog(context.Background(), account, nil)
	require.NoError(t, err)
	require.Empty(t, applied.Models)
	require.NotNil(t, applied.Models)
	require.Empty(t, account.GetUpstreamModelCatalog().EligibleModels)
	last := account.GetUpstreamModelCatalog()
	_, err = service.ApplyAccountModelCatalog(context.Background(), account, nil)
	require.Error(t, err)
	require.Equal(t, last, account.GetUpstreamModelCatalog())
	require.Equal(t, "failed", account.ModelCatalogState().Result)
	require.Equal(t, map[string]any{"alias": "kept"}, account.Credentials["model_mapping"])
	require.Nil(t, other.GetUpstreamModelCatalog())
}

func TestAccountModelCatalogTrustsDiscoveredModelsWithoutConfirmation(t *testing.T) {
	for _, platform := range []string{PlatformQianwen, PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			account := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://custom.example.com", "api_protocol": APIProtocolChatCompletions, "model_mapping": map[string]any{"alias": "missing"}}}
			catalog := &UpstreamModelCatalog{Models: []string{"native"}, Metadata: map[string]UpstreamModelMetadata{"native": {TaskType: "text_generation"}}}
			snapshot := projectAccountModelCatalog(account, catalog)
			require.Empty(t, snapshot.ExcludedModels)
			require.Equal(t, []AccountCatalogModel{{ID: "native", Category: "chat", Endpoints: []string{"chat_completions"}, EvidenceKind: "native_account_catalog", EvidenceObservedAt: catalog.ObservedAt}}, snapshot.EligibleModels)
			require.Equal(t, []string{"native"}, snapshot.DiscoveredModels)
			require.Empty(t, catalog.InvalidAliases)
			for _, scope := range []string{"provider", "product", "custom", "unknown"} {
				snapshot.DiscoveryScope = scope
				account.Extra = map[string]any{UpstreamModelCatalogExtraKey: snapshot}
				require.Len(t, account.EffectiveUpstreamModelCatalog().EligibleModels, 2, scope)
				require.Equal(t, snapshot.EligibleModels[0], account.EffectiveUpstreamModelCatalog().EligibleModels[0])
			}
		})
	}
}

func TestAccountModelCatalogProductDiscoveryUsesAccountCredentials(t *testing.T) {
	account := &Account{ID: 23, Platform: PlatformQianwen, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "account-product-key", "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1", "api_protocol": APIProtocolChatCompletions}}
	repository := &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account}}
	productPage := `{"success":true,"output":{"total":3,"page_no":1,"models":[{"model":"native-chat","modalities":{"output":["text"]},"reasoning":false,"input_modalities":["text"],"context_window":1000},{"model":"native-video","modalities":{"output":["video"]}},{"model":"qwen1.5-72b-chat","modalities":{"output":["text"]}}]}}`
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		catalogFixtureResponse(productPage),
		catalogFixtureResponse(`{"success":true,"output":{"total":0,"page_no":1,"models":[]}}`),
		catalogFixtureResponse(productPage),
	}}
	service := &AccountTestService{accountRepo: repository, httpUpstream: upstream, cfg: upstreamModelSyncTestConfig(),
		modelMetadataRegistryKey: ModelRegistryDownloadKey(modelsDevRegistryURL, "", "verified-tls"), modelMetadataRegistry: map[string]modelsDevProvider{"alibaba": {}}, modelMetadataRegistryAt: time.Now()}
	for _, wantModels := range [][]string{{"native-chat"}, {}, {"native-chat"}} {
		catalog, err := service.ApplyAccountModelCatalog(context.Background(), account, nil)
		require.NoError(t, err)
		require.True(t, catalog.Applied)
		require.Equal(t, "provider", catalog.DiscoveryScope)
		require.Equal(t, wantModels, catalog.Models)
		effective := account.EffectiveUpstreamModelCatalog()
		require.NotNil(t, effective)
		require.Len(t, effective.EligibleModels, len(wantModels))
		require.Equal(t, ModelSelectionInherit, AccountModelSelectionView(account).Mode)
		require.Empty(t, configuredModelMapping(account))
		require.Zero(t, account.ModelCatalogState().RulesRevision)
	}
	require.Len(t, upstream.requests, 3)
	for _, request := range upstream.requests {
		require.Equal(t, "Bearer account-product-key", request.Header.Get("Authorization"))
		require.Equal(t, "dashscope.aliyuncs.com", request.URL.Hostname())
	}
}

func TestAccountModelCatalogConflictAndRevisionOwnership(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "first"}}
	repository := &catalogFixtureRepository{accounts: map[int64]*Account{1: account}}
	baseline := account.ModelCatalogBaseline()
	snapshot := &AccountModelCatalogSnapshot{Revision: 1, Complete: true}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- repository.CommitAccountModelCatalog(context.Background(), 1, baseline, snapshot, AccountModelCatalogState{}, nil, false)
		}()
	}
	first, second := <-results, <-results
	require.True(t, (first == nil && errors.Is(second, ErrAccountModelCatalogConflict)) || (second == nil && errors.Is(first, ErrAccountModelCatalogConflict)))
	incoming := &Account{Platform: account.Platform, Type: account.Type, Credentials: map[string]any{"api_key": "second"}}
	AdvanceAccountModelCatalogRevisions(account, incoming)
	require.Equal(t, int64(1), incoming.ModelCatalogState().IdentityRevision)
	incoming.Credentials = map[string]any{"api_key": "first", "model_mapping": map[string]any{"alias": "target"}}
	AdvanceAccountModelCatalogRevisions(account, incoming)
	require.Zero(t, incoming.ModelCatalogState().IdentityRevision)
	require.Equal(t, int64(1), incoming.ModelCatalogState().RulesRevision)
}

func TestModelCatalogStrictEmptyAndCompletePages(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"data":null}`, `{"data":[{}]}`} {
		_, err := extractCatalogModelIDsWithSelector([]byte(body), upstreamModelEntryID)
		require.Error(t, err, body)
	}
	for _, body := range []string{`[]`, `{"data":[]}`, `{"models":[]}`} {
		ids, err := extractCatalogModelIDsWithSelector([]byte(body), upstreamModelEntryID)
		require.NoError(t, err)
		require.Empty(t, ids)
	}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{catalogFixtureResponse(`{"data":[{"id":"first"}],"has_more":true,"last_id":"first"}`), catalogFixtureResponse(`{"data":[],"has_more":false}`)}}
	service := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	request, err := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	require.NoError(t, err)
	ids, _, err := service.fetchCompleteModelPages(request, &Account{Platform: PlatformAnthropic})
	require.NoError(t, err)
	require.Equal(t, []string{"first"}, ids)
	require.Equal(t, "first", upstream.requests[1].URL.Query().Get("after_id"))
}

func TestPlatformPresetCoverage(t *testing.T) {
	presets, err := PlatformModelPresets()
	require.NoError(t, err)
	require.NotEmpty(t, presets)
}

func TestAccountModelCatalogResponseContract(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"base_url": "https://api.openai.com", "model_mapping": map[string]string{"z-alias": "missing", "a-alias": "unknown"}},
		Extra:       map[string]any{UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{IdentityRevision: 9007199254740993, RulesRevision: 9007199254740995}}}
	catalog := &UpstreamModelCatalog{Models: []string{"unknown"}}
	projectAccountModelCatalog(account, catalog)
	encoded, err := json.Marshal(catalog)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, json.Unmarshal(encoded, &response))
	require.Equal(t, "9007199254740993", response["identity_revision"])
	require.Equal(t, "9007199254740995", response["rules_revision"])
	require.Equal(t, "0", response["snapshot_revision"])
	require.Empty(t, response["invalid_aliases"])
	require.Empty(t, response["excluded_models"])
	require.Equal(t, []any{"unknown"}, response["models"])
	// Internal state remains numeric and can still be decoded from account extra.
	encoded, err = json.Marshal(account.ModelCatalogState())
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"identity_revision":9007199254740993`)
	account.Credentials["model_mapping"] = map[string]string{}
	empty := &UpstreamModelCatalog{Models: []string{}}
	projectAccountModelCatalog(account, empty)
	encoded, err = json.Marshal(empty)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, &response))
	require.Equal(t, []any{}, response["excluded_models"])
	require.Equal(t, []any{}, response["invalid_aliases"])
}

func TestAccountModelCatalogPreviewPreservesStoredEvidenceAndCache(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			account := &Account{ID: 51, Platform: PlatformDeepseek, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test", "base_url": "https://api.deepseek.com", "model_mapping": map[string]string{"alias": "old"}},
				Extra: map[string]any{
					UpstreamModelCatalogExtraKey:      AccountModelCatalogSnapshot{Complete: true, Revision: 8, EligibleModels: []AccountCatalogModel{{ID: "old", Endpoints: []string{"chat_completions"}}}},
					UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{RulesRevision: 4, Result: "succeeded"},
					UpstreamModelMetadataExtraKey:     UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"old": {TaskType: "text_generation"}}},
				}}
			repository := &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account}}
			// Every persistence method other than the catalog commit panics through the
			// embedded nil repository; catalog commits also represent outbox writes.
			gateway := &GatewayService{modelsListCache: gocache.New(time.Hour, 0)}
			key := modelsListCacheKey(nil, account.Platform)
			gateway.modelsListCache.SetDefault(key, []string{"old"})
			previous := availableModelsCacheInvalidator.Swap(gateway)
			t.Cleanup(func() { availableModelsCacheInvalidator.Store(previous) })
			before, err := json.Marshal(account)
			require.NoError(t, err)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}}
			service := &AccountTestService{accountRepo: repository, httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
			catalog, err := service.SyncUpstreamModelCatalog(context.Background(), account)
			if status == http.StatusOK {
				require.NoError(t, err)
				require.Equal(t, []string{"old"}, catalog.Diff.Removed)
				require.Empty(t, catalog.InvalidAliases)
				require.False(t, catalog.Applied)
			} else {
				require.Error(t, err)
			}
			after, err := json.Marshal(account)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
			require.Zero(t, repository.commits)
			cached, found := gateway.modelsListCache.Get(key)
			require.True(t, found, "preview must not invalidate available-model caches")
			require.Equal(t, []string{"old"}, cached)
		})
	}
}

type concurrentCatalogRepository struct {
	*catalogFixtureRepository
	ready   chan struct{}
	release chan struct{}
}

func (repository *concurrentCatalogRepository) CommitAccountModelCatalog(ctx context.Context, id int64, baseline AccountModelCatalogBaseline, snapshot *AccountModelCatalogSnapshot, state AccountModelCatalogState, metadata *UpstreamModelMetadataSnapshot, freezeSelectionBaseline bool) error {
	repository.ready <- struct{}{}
	select {
	case <-repository.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return repository.catalogFixtureRepository.CommitAccountModelCatalog(ctx, id, baseline, snapshot, state, metadata, freezeSelectionBaseline)
}

func TestAccountModelCatalogConcurrentApplySameBaselineHasOneWinner(t *testing.T) {
	stored := &Account{ID: 61, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://api.deepseek.com"}}
	repository := &concurrentCatalogRepository{catalogFixtureRepository: &catalogFixtureRepository{accounts: map[int64]*Account{stored.ID: stored}}, ready: make(chan struct{}, 2), release: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type result struct {
		catalog *UpstreamModelCatalog
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		// Production requests load separate account instances from the repository.
		account := *stored
		service := &AccountTestService{accountRepo: repository, httpUpstream: &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[]}`)}, cfg: upstreamModelSyncTestConfig()}
		go func() {
			catalog, err := service.ApplyAccountModelCatalog(ctx, &account, nil)
			results <- result{catalog, err}
		}()
	}
	for range 2 {
		select {
		case <-repository.ready:
		case <-ctx.Done():
			t.Fatal("both applications must reach commit with the same baseline")
		}
	}
	close(repository.release)
	winners, conflicts := 0, 0
	for range 2 {
		result := <-results
		if result.err == nil {
			winners++
			require.True(t, result.catalog.Applied)
			require.Equal(t, int64(1), result.catalog.SnapshotRevision)
		} else {
			require.ErrorIs(t, result.err, ErrAccountModelCatalogConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, winners)
	require.Equal(t, 1, conflicts)
	require.Equal(t, 1, repository.commits)
	require.Equal(t, int64(1), stored.GetUpstreamModelCatalog().Revision)
}

func TestAccountModelCatalogApplyAccountsOnSamePlatformRemainIndependent(t *testing.T) {
	makeAccount := func(id int64, model string) *Account {
		return &Account{ID: id, Platform: PlatformDeepseek, Type: AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": model, "base_url": "https://api.deepseek.com", "model_mapping": map[string]string{"alias": model}},
			Extra: map[string]any{
				UpstreamModelCatalogExtraKey:  AccountModelCatalogSnapshot{Complete: true, Revision: 3, EligibleModels: []AccountCatalogModel{{ID: model, Endpoints: []string{"chat_completions"}}}},
				UpstreamModelMetadataExtraKey: UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{model: {TaskType: "text_generation"}}},
			}}
	}
	left, right := makeAccount(71, "left"), makeAccount(72, "right")
	repository := &catalogFixtureRepository{accounts: map[int64]*Account{left.ID: left, right.ID: right}}
	rightBefore, err := json.Marshal(right)
	require.NoError(t, err)
	service := &AccountTestService{accountRepo: repository, cfg: upstreamModelSyncTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[]}`)}}
	applied, err := service.ApplyAccountModelCatalog(context.Background(), left, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"left"}, applied.Diff.Removed)
	require.Empty(t, left.GetUpstreamModelCatalog().EligibleModels)
	rightAfter, err := json.Marshal(right)
	require.NoError(t, err)
	require.JSONEq(t, string(rightBefore), string(rightAfter))

	lastSnapshot := right.GetUpstreamModelCatalog()
	lastMetadata := right.Extra[UpstreamModelMetadataExtraKey]
	service.httpUpstream = &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("failed"))}}
	_, err = service.ApplyAccountModelCatalog(context.Background(), right, nil)
	require.Error(t, err)
	require.Equal(t, lastSnapshot, right.GetUpstreamModelCatalog(), "failed discovery must preserve nonempty members and revision")
	require.Equal(t, lastMetadata, right.Extra[UpstreamModelMetadataExtraKey], "failed discovery must preserve capabilities")
	require.Equal(t, map[string]string{"alias": "right"}, right.Credentials["model_mapping"])
	require.Equal(t, "failed", right.ModelCatalogState().Result)
	require.Equal(t, int64(4), left.GetUpstreamModelCatalog().Revision)
	require.Equal(t, int64(3), right.GetUpstreamModelCatalog().Revision)

	service.httpUpstream = &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[]}`)}
	_, err = service.ApplyAccountModelCatalog(context.Background(), right, nil)
	require.NoError(t, err)
	require.Equal(t, int64(4), right.GetUpstreamModelCatalog().Revision)
	require.Equal(t, int64(4), left.GetUpstreamModelCatalog().Revision)
	require.Equal(t, map[string]string{"alias": "left"}, left.Credentials["model_mapping"])
}

func TestAccountModelCatalogExclusionReasons(t *testing.T) {
	for _, tc := range []struct {
		name      string
		platform  string
		baseURL   string
		allowlist AccountModelAllowlist
		metadata  UpstreamModelMetadata
		reason    CatalogExclusionReason
	}{
		{name: "provider missing endpoint evidence", platform: PlatformQianwen},
		{name: "provider account restriction", platform: PlatformQianwen, allowlist: AccountModelAllowlist{Enabled: true}, metadata: UpstreamModelMetadata{TaskType: "text_generation"}, reason: CatalogReasonAccountRestricted},
		{name: "account restriction", platform: PlatformDeepseek, baseURL: "https://api.deepseek.com", allowlist: AccountModelAllowlist{Enabled: true}, reason: CatalogReasonAccountRestricted},
		{name: "missing endpoint evidence", platform: PlatformOpenAI, baseURL: "https://api.openai.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{Platform: tc.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": tc.baseURL}, Extra: map[string]any{UpstreamModelAllowlistExtraKey: tc.allowlist}}
			catalog := &UpstreamModelCatalog{Models: []string{"native"}, Metadata: map[string]UpstreamModelMetadata{"native": tc.metadata}}
			snapshot := projectAccountModelCatalog(account, catalog)
			switch tc.reason {
			case CatalogReasonAccountRestricted:
				require.Len(t, snapshot.EligibleModels, 1)
				require.Empty(t, catalog.ExcludedModels)
				account.Extra[UpstreamModelCatalogExtraKey] = snapshot
				require.Empty(t, account.EffectiveUpstreamModelCatalog().EligibleModels)
			case "":
				require.Len(t, snapshot.EligibleModels, 1)
				require.Contains(t, snapshot.EligibleModels[0].Endpoints, CompositeRouteEndpointChatCompletions)
				require.Empty(t, catalog.ExcludedModels)
			default:
				require.Empty(t, snapshot.EligibleModels)
				require.Equal(t, []ExcludedCatalogModel{{ID: "native", Reason: tc.reason}}, catalog.ExcludedModels)
			}
		})
	}
}
