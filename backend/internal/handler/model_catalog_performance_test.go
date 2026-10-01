package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// This fixture benchmarks real handler/projection/pricing/serialization code.
// Authentication identity and repository IO are controlled test doubles; it
// does not claim production network or database latency.
type catalogPerformanceAccounts struct {
	service.AccountRepository
	encoded    [][]byte
	generation atomic.Int64
	reads      atomic.Int64
}

func (repository *catalogPerformanceAccounts) ModelCatalogGeneration(context.Context, string) (int64, error) {
	return repository.generation.Load(), nil
}
func (repository *catalogPerformanceAccounts) ReadModelAccessSnapshot(context.Context, int64) (service.ModelAccessSnapshot, error) {
	return service.ModelAccessSnapshot{Generation: repository.generation.Load(), RevisionKnown: true}, nil
}
func (repository *catalogPerformanceAccounts) ListModelCatalogAccounts(ctx context.Context, groupID *int64, platforms []string, includeGrouped bool) ([]service.Account, error) {
	repository.reads.Add(1)
	accounts := make([]service.Account, 0, len(repository.encoded))
	for index, encoded := range repository.encoded {
		if groupID != nil && int64(index/5+1) != *groupID {
			continue
		}
		var account service.Account
		if err := json.Unmarshal(encoded, &account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}
func (repository *catalogPerformanceAccounts) ListModelCatalogAccountsForGroups(ctx context.Context, groupIDs []int64) ([]service.Account, error) {
	accounts, err := repository.ListModelCatalogAccounts(ctx, nil, nil, true)
	if err != nil {
		return nil, err
	}
	result := make([]service.Account, 0, len(accounts))
	for _, account := range accounts {
		if slices.Contains(groupIDs, account.GroupIDs[0]) {
			result = append(result, account)
		}
	}
	return result, nil
}
func (repository *catalogPerformanceAccounts) GetModelCatalogAccount(context.Context, int64) (*service.Account, error) {
	return nil, service.ErrAccountNotFound
}

type catalogPerformanceGroups struct {
	service.GroupRepository
	groups []service.Group
}

func (repository *catalogPerformanceGroups) ListActive(context.Context) ([]service.Group, error) {
	return append([]service.Group(nil), repository.groups...), nil
}
func (repository *catalogPerformanceGroups) GetByIDLite(_ context.Context, groupID int64) (*service.Group, error) {
	group := repository.groups[groupID-1]
	return &group, nil
}

type catalogPerformanceChannels struct {
	service.ChannelRepository
	encoded  []byte
	groupIDs []int64
}

func (repository *catalogPerformanceChannels) ListAll(context.Context) ([]service.Channel, error) {
	var channels []service.Channel
	err := json.Unmarshal(repository.encoded, &channels)
	return channels, err
}
func (repository *catalogPerformanceChannels) GetGroupPlatforms(context.Context, []int64) (map[int64]string, error) {
	result := make(map[int64]string)
	for _, groupID := range repository.groupIDs {
		result[groupID] = service.PlatformOpenAI
	}
	return result, nil
}

type catalogPerformanceSettings struct{ service.SettingRepository }

func (repository *catalogPerformanceSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{service.SettingKeyModelPlazaEnabled: "true"}, nil
}

type catalogPerformanceUsers struct{ service.UserRepository }

func (repository *catalogPerformanceUsers) GetByID(_ context.Context, userID int64) (*service.User, error) {
	role := service.RoleUser
	if userID == 2 {
		role = service.RoleAdmin
	}
	return &service.User{ID: userID, Role: role}, nil
}

type catalogPerformanceSubscriptions struct {
	service.UserSubscriptionRepository
}

func (repository *catalogPerformanceSubscriptions) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	return nil, nil
}

type catalogPerformanceRates struct {
	service.UserGroupRateRepository
}

func (repository *catalogPerformanceRates) GetByUserID(context.Context, int64) (map[int64]float64, error) {
	return map[int64]float64{1: 0.8}, nil
}

func newCatalogPerformanceRouter(tester testing.TB) (*gin.Engine, *catalogPerformanceAccounts) {
	tester.Helper()
	gin.SetMode(gin.TestMode)
	accounts := &catalogPerformanceAccounts{}
	groups := &catalogPerformanceGroups{}
	names := make([]string, 100)
	metadata := make(map[string]service.UpstreamModelMetadata, 100)
	members := make([]service.AccountCatalogModel, 100)
	for index := range names {
		name := fmt.Sprintf("model-%04d", index)
		names[index] = name
		members[index] = service.AccountCatalogModel{ID: name, Endpoints: []string{"responses", "chat_completions"}, EvidenceKind: "native_account_catalog", EvidenceObservedAt: "2026-09-15T00:00:00Z"}
		metadata[name] = service.UpstreamModelMetadata{ProviderProfile: "openai", CreatedAt: "2026-01-01T00:00:00Z", ContextWindow: 128000, MaxOutputTokens: 8192, SupportedMethods: []string{"responses", "chat_completions"}}
	}
	channels := make([]service.Channel, 20)
	channelRepo := &catalogPerformanceChannels{}
	input, output := 0.000003, 0.000015
	for index := 0; index < 20; index++ {
		groupID := int64(index + 1)
		groups.groups = append(groups.groups, service.Group{ID: groupID, Name: fmt.Sprintf("group-%02d", index), Platform: service.PlatformOpenAI, RateMultiplier: 1})
		channelRepo.groupIDs = append(channelRepo.groupIDs, groupID)
		channels[index] = service.Channel{ID: groupID, Name: fmt.Sprintf("channel-%02d", index), Status: service.StatusActive, GroupIDs: []int64{groupID}, ModelPricing: []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: names, BillingMode: service.BillingModeToken, InputPrice: &input, OutputPrice: &output}}}
	}
	for index := 0; index < 100; index++ {
		account := service.Account{ID: int64(index + 1), Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{int64(index/5 + 1)}, Credentials: map[string]any{}, Extra: map[string]any{
			service.UpstreamModelCatalogExtraKey:  service.AccountModelCatalogSnapshot{Revision: 1, Complete: true, DiscoveredModels: names, EligibleModels: members},
			service.UpstreamModelMetadataExtraKey: service.UpstreamModelMetadataSnapshot{Models: metadata},
		}}
		encoded, err := json.Marshal(account)
		require.NoError(tester, err)
		accounts.encoded = append(accounts.encoded, encoded)
	}
	encoded, err := json.Marshal(channels)
	require.NoError(tester, err)
	channelRepo.encoded = encoded
	configuration := &config.Config{}
	channelService := service.NewChannelService(channelRepo, groups, nil, nil, nil)
	pricing := service.NewModelPricingResolver(channelService, nil)
	billing := service.NewBillingService(configuration, nil)
	gateway := service.NewGatewayService(accounts, groups, nil, nil, nil, nil, nil, nil, configuration, nil, nil, billing, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, channelService, pricing, nil, nil, nil)
	gatewayHandler := &GatewayHandler{gatewayService: gateway}
	plaza := service.NewModelPlazaService(channelRepo, groups, nil, billing, pricing)
	keys := service.NewAPIKeyService(nil, &catalogPerformanceUsers{}, groups, &catalogPerformanceSubscriptions{}, &catalogPerformanceRates{}, nil, configuration)
	plazaHandler := NewModelPlazaHandler(plaza, keys, service.NewSettingService(&catalogPerformanceSettings{}, configuration))
	router := gin.New()
	router.Use(func(ctx *gin.Context) {
		userID := int64(1)
		if ctx.GetHeader("X-Test-Role") == "admin" {
			userID = 2
		}
		ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, UserID: userID, GroupID: &groups.groups[0].ID, Group: &groups.groups[0]})
		ctx.Next()
	})
	router.GET("/v1/models", gatewayHandler.Models)
	router.GET("/api/v1/model-plaza", plazaHandler.Get)
	return router, accounts
}

func catalogPerformanceRequest(router *gin.Engine, path, role string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("X-Test-Role", role)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestCatalogPerformanceHandlersPreservePayload(tester *testing.T) {
	router, accounts := newCatalogPerformanceRouter(tester)
	// Assert the public contract directly: additive metadata can evolve without
	// replacing opaque snapshots, while cold/warm responses must remain identical.
	for _, path := range []string{"/v1/models", "/api/v1/model-plaza"} {
		cold := catalogPerformanceRequest(router, path, "user")
		require.Equal(tester, http.StatusOK, cold.Code, cold.Body.String())
		warm := catalogPerformanceRequest(router, path, "user")
		require.Equal(tester, http.StatusOK, warm.Code)
		require.Equal(tester, cold.Body.String(), warm.Body.String())
		require.JSONEq(tester, cold.Body.String(), warm.Body.String())
		var payload struct {
			Data json.RawMessage `json:"data"`
		}
		require.NoError(tester, json.Unmarshal(warm.Body.Bytes(), &payload))
		if path == "/v1/models" {
			var models []publicOpenAIModel
			require.NoError(tester, json.Unmarshal(payload.Data, &models))
			require.Len(tester, models, 100)
			for index, model := range models {
				require.Equal(tester, fmt.Sprintf("model-%04d", index), model.ID)
				require.Equal(tester, model.ID, model.Name)
				require.Equal(tester, "model", model.Object)
				require.Equal(tester, service.PlatformOpenAI, model.OwnedBy)
				// Metadata observations lack binding field evidence: they must not
				// turn into guaranteed limits or reasoning capabilities.
				require.Nil(tester, model.Created)
				require.Empty(tester, model.CreatedAtSource)
				require.Equal(tester, publicClientModelMetadata{}, model.publicClientModelMetadata)
			}
		} else {
			var plaza modelPlazaResponse
			require.NoError(tester, json.Unmarshal(payload.Data, &plaza))
			require.Len(tester, plaza.Groups, 20)
			for _, group := range plaza.Groups {
				require.Len(tester, group.Models, 100)
				require.Equal(tester, service.PlatformOpenAI, group.Platform)
				require.Equal(tester, float64(1), group.RateMultiplier)
				for index, model := range group.Models {
					require.Equal(tester, fmt.Sprintf("model-%04d", index), model.Name)
					require.Equal(tester, service.PlatformOpenAI, model.Platform)
					require.NotNil(tester, model.Pricing)
					require.NotNil(tester, model.Pricing.InputPrice)
					require.NotNil(tester, model.Pricing.OutputPrice)
					require.InDelta(tester, 0.000003, *model.Pricing.InputPrice, 1e-15)
					require.InDelta(tester, 0.000015, *model.Pricing.OutputPrice, 1e-15)
					require.Nil(tester, model.Pricing.CacheWritePrice)
					require.Nil(tester, model.Pricing.CacheReadPrice)
				}
				require.NotNil(tester, group.Models[0].Pricing)
				capabilities := group.Models[0].Capabilities
				require.NotNil(tester, capabilities)
				require.Nil(tester, capabilities.UnspecifiedContext.Guaranteed)
				require.True(tester, capabilities.UnspecifiedContext.Unknown)
				require.Equal(tester, []int64{128000}, capabilities.UnspecifiedContext.Available)
				require.Equal(tester, int64(128000), *capabilities.UnspecifiedContext.ObservedMinimum)
				require.Nil(tester, capabilities.MaxOutputTokens.Guaranteed)
				require.True(tester, capabilities.MaxOutputTokens.Unknown)
				require.Equal(tester, []int64{8192}, capabilities.MaxOutputTokens.Available)
			}
			require.NotNil(tester, plaza.Groups[0].UserRateMultiplier)
			require.Equal(tester, 0.8, *plaza.Groups[0].UserRateMultiplier)
		}
	}
	require.Equal(tester, int64(2), accounts.reads.Load())
}

func TestCatalogPerformanceLatencyMatrix(tester *testing.T) {
	samples, err := strconv.Atoi(os.Getenv("CATALOG_PERFORMANCE_SAMPLES"))
	if err != nil || samples <= 0 {
		tester.Skip("set CATALOG_PERFORMANCE_SAMPLES for a controlled handler latency run")
	}
	for _, surface := range []struct {
		path, role string
		budget     time.Duration
	}{{"/v1/models", "user", 500 * time.Millisecond}, {"/api/v1/model-plaza", "user", 2 * time.Second}, {"/api/v1/model-plaza", "admin", 2 * time.Second}} {
		for _, concurrency := range []int{1, 10} {
			tester.Run(fmt.Sprintf("%s/%s/concurrency-%d", surface.path, surface.role, concurrency), func(tester *testing.T) {
				router, accounts := newCatalogPerformanceRouter(tester)
				coldStarted := time.Now()
				cold := catalogPerformanceRequest(router, surface.path, surface.role)
				coldElapsed := time.Since(coldStarted)
				require.Equal(tester, http.StatusOK, cold.Code)
				for warmup := 0; warmup < 20; warmup++ {
					require.Equal(tester, http.StatusOK, catalogPerformanceRequest(router, surface.path, surface.role).Code)
				}
				beforeReads := accounts.reads.Load()
				var beforeMemory, afterMemory runtime.MemStats
				runtime.ReadMemStats(&beforeMemory)
				durations := make([]time.Duration, samples)
				var errors atomic.Int64
				var workers sync.WaitGroup
				for worker := 0; worker < concurrency; worker++ {
					workers.Add(1)
					go func(worker int) {
						defer workers.Done()
						for index := worker; index < samples; index += concurrency {
							started := time.Now()
							response := catalogPerformanceRequest(router, surface.path, surface.role)
							durations[index] = time.Since(started)
							if response.Code != http.StatusOK || response.Body.Len() != cold.Body.Len() {
								errors.Add(1)
							}
						}
					}(worker)
				}
				workers.Wait()
				runtime.ReadMemStats(&afterMemory)
				sort.Slice(durations, func(first, second int) bool { return durations[first] < durations[second] })
				percentile := func(percent int) float64 {
					return float64(durations[(samples*percent+99)/100-1]) / float64(time.Millisecond)
				}
				tester.Logf("samples=%d concurrency=%d errors=%d cold_ms=%.3f p50_ms=%.3f p95_ms=%.3f p99_ms=%.3f account_queries=%d response_bytes=%d alloc_bytes_per_request=%d allocations_per_request=%d", samples, concurrency, errors.Load(), float64(coldElapsed)/float64(time.Millisecond), percentile(50), percentile(95), percentile(99), accounts.reads.Load()-beforeReads, cold.Body.Len(), (afterMemory.TotalAlloc-beforeMemory.TotalAlloc)/uint64(samples), (afterMemory.Mallocs-beforeMemory.Mallocs)/uint64(samples))
				require.Zero(tester, errors.Load())
				require.Less(tester, percentile(95), float64(surface.budget)/float64(time.Millisecond))
				require.Less(tester, coldElapsed, surface.budget)
			})
		}
	}
}
