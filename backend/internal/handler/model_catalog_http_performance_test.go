package handler_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	_ "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/runtime"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository" //nolint:depguard // Opt-in external-package HTTP integration test assembles the real repository graph.
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/routes"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Count at the SQL driver boundary, including Ent and handwritten repository SQL.
type catalogSQLConnector struct {
	driver.Connector
	queries *atomic.Int64
}

func (connector catalogSQLConnector) Connect(ctx context.Context) (driver.Conn, error) {
	connection, err := connector.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return catalogSQLConnection{connection, connector.queries}, nil
}

type catalogSQLConnection struct {
	driver.Conn
	queries *atomic.Int64
}

var catalogQueryTally sync.Map

func catalogTallyQuery(query string) {
	key := strings.Join(strings.Fields(query), " ")
	// CATALOG_TALLY_ATTRIBUTION=1 annotates high-frequency read queries with
	// their service/repository call stacks; default runs keep plain SQL keys.
	if os.Getenv("CATALOG_TALLY_ATTRIBUTION") != "" {
		attribute := strings.Contains(key, "model_catalog_generations") && strings.HasPrefix(key, "SELECT")
		attribute = attribute || strings.HasPrefix(key, `SELECT "groups"."id"`) || strings.HasPrefix(key, `SELECT "accounts"."id"`) || strings.HasPrefix(key, `SELECT "account_groups".`) || strings.HasPrefix(key, `SELECT "settings".`)
		if attribute {
			var callers []string
			for depth := 4; depth < 16; depth++ {
				programCounter, file, line, ok := runtime.Caller(depth)
				if !ok {
					break
				}
				name := ""
				if fn := runtime.FuncForPC(programCounter); fn != nil {
					name = fn.Name()
				}
				if strings.Contains(name, "/service.") || strings.Contains(name, "/repository.") {
					callers = append(callers, fmt.Sprintf("%s(%s:%d)", name, filepath.Base(file), line))
				}
				if len(callers) >= 5 {
					break
				}
			}
			key = key + " <<<" + strings.Join(callers, " <- ") + ">>>"
		}
	}
	counter, _ := catalogQueryTally.LoadOrStore(key, &atomic.Int64{})
	if tally, ok := counter.(*atomic.Int64); ok {
		tally.Add(1)
	}
}

func (connection catalogSQLConnection) QueryContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	connection.queries.Add(1)
	catalogTallyQuery(query)
	queryConnection, supported := connection.Conn.(driver.QueryerContext)
	if !supported {
		return nil, fmt.Errorf("PostgreSQL driver must support QueryContext")
	}
	return queryConnection.QueryContext(ctx, query, arguments)
}
func (connection catalogSQLConnection) ExecContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	connection.queries.Add(1)
	catalogTallyQuery(query)
	executionConnection, supported := connection.Conn.(driver.ExecerContext)
	if !supported {
		return nil, fmt.Errorf("PostgreSQL driver must support ExecContext")
	}
	return executionConnection.ExecContext(ctx, query, arguments)
}
func (connection catalogSQLConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	transactionConnection, supported := connection.Conn.(driver.ConnBeginTx)
	if !supported {
		return nil, fmt.Errorf("PostgreSQL driver must support BeginTx")
	}
	return transactionConnection.BeginTx(ctx, options)
}

type catalogHTTPFixture struct {
	database        *sql.DB
	client          *dbent.Client
	queries         atomic.Int64
	responseBytes   atomic.Int64
	groups          []service.Group
	users           []service.User
	keys            []service.APIKey
	unboundKey      service.APIKey
	accounts        service.AccountRepository
	groupRepository service.GroupRepository
	userRepository  service.UserRepository
	keyRepository   service.APIKeyRepository
	channels        service.ChannelRepository
	modelCount      int
}

func newCatalogHTTPFixture(tester *testing.T, groupCount, modelCount int) *catalogHTTPFixture {
	tester.Helper()
	address, err := url.Parse(os.Getenv("CATALOG_PERFORMANCE_POSTGRES_DSN"))
	require.NoError(tester, err)
	require.Contains(tester, []string{"localhost", "127.0.0.1"}, address.Hostname())
	require.Equal(tester, "5433", address.Port(), "only the dedicated local test instance is permitted")
	require.Contains(tester, []string{"/postgres", "/stats_test", "/job_test"}, address.Path)
	administrator, err := sql.Open("postgres", address.String())
	require.NoError(tester, err)
	tester.Cleanup(func() { require.NoError(tester, administrator.Close()) })
	databaseName := fmt.Sprintf("catalog_performance_%d", time.Now().UnixNano())
	_, err = administrator.Exec("CREATE DATABASE " + pq.QuoteIdentifier(databaseName))
	require.NoError(tester, err)
	tester.Cleanup(func() {
		_, cleanupErr := administrator.Exec("DROP DATABASE " + pq.QuoteIdentifier(databaseName))
		require.NoError(tester, cleanupErr)
	})
	address.Path = "/" + databaseName
	connector, err := pq.NewConnector(address.String())
	require.NoError(tester, err)
	fixture := &catalogHTTPFixture{modelCount: modelCount}
	fixture.database = sql.OpenDB(catalogSQLConnector{connector, &fixture.queries})
	// Match the application's configured defaults rather than database/sql's
	// two-idle-connection default, which churns sockets during concurrency 10.
	fixture.database.SetMaxOpenConns(256)
	fixture.database.SetMaxIdleConns(128)
	tester.Cleanup(func() { require.NoError(tester, fixture.database.Close()) })
	require.NoError(tester, repository.ApplyMigrations(context.Background(), fixture.database))
	fixture.client = dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, fixture.database)))
	fixture.accounts = repository.NewAccountRepository(fixture.client, fixture.database, nil)
	fixture.groupRepository = repository.NewGroupRepository(fixture.client, fixture.database)
	fixture.userRepository = repository.NewUserRepository(fixture.client, fixture.database)
	fixture.keyRepository = repository.NewAPIKeyRepository(fixture.client, fixture.database)
	fixture.channels = repository.NewChannelRepository(fixture.database)
	ctx := context.Background()
	// A new database contains no customer rows; disable migration seed groups only here.
	_, err = fixture.database.Exec("UPDATE groups SET status='inactive'")
	require.NoError(tester, err)
	settings := repository.NewSettingRepository(fixture.client)
	require.NoError(tester, settings.Set(ctx, service.SettingKeyModelPlazaEnabled, "true"))
	require.NoError(tester, settings.Set(ctx, "session_binding_enabled", "false"))
	for _, role := range []string{service.RoleUser, service.RoleAdmin} {
		user := service.User{Email: role + "@performance.invalid", PasswordHash: "fixture-password-hash", Role: role, Balance: 100000, Concurrency: 100, Status: service.StatusActive}
		require.NoError(tester, fixture.userRepository.Create(ctx, &user))
		fixture.users = append(fixture.users, user)
	}
	names := make([]string, modelCount)
	members := make([]service.AccountCatalogModel, modelCount)
	metadata := make(map[string]service.UpstreamModelMetadata, modelCount)
	for index := range names {
		name := fmt.Sprintf("model-%04d", index)
		names[index] = name
		members[index] = service.AccountCatalogModel{ID: name, Endpoints: []string{"responses", "chat_completions"}, EvidenceKind: "native_account_catalog", EvidenceObservedAt: "2026-09-15T00:00:00Z"}
		metadata[name] = service.UpstreamModelMetadata{ProviderProfile: "openai", CreatedAt: "2026-01-01T00:00:00Z", ContextWindow: 128000, MaxOutputTokens: 8192, SupportedMethods: []string{"responses", "chat_completions"}}
	}
	input, output := 0.000003, 0.000015
	for groupIndex := 0; groupIndex < groupCount; groupIndex++ {
		group := service.Group{Name: fmt.Sprintf("performance-group-%02d", groupIndex), Platform: service.PlatformOpenAI, RateMultiplier: 1, Status: service.StatusActive, SubscriptionType: "standard"}
		if groupIndex == groupCount-1 {
			group.Platform = service.PlatformComposite
		}
		require.NoError(tester, fixture.groupRepository.Create(ctx, &group))
		fixture.groups = append(fixture.groups, group)
		if group.Platform == service.PlatformComposite {
			routes := repository.NewCompositeModelRouteRepository(fixture.client)
			for _, name := range names {
				route := service.CompositeModelRoute{GroupID: group.ID, PublicModel: name, MatchType: service.CompositeRouteMatchExact, TargetPlatform: service.PlatformOpenAI, AccountType: service.AccountTypeAPIKey, UpstreamModel: name, Endpoint: service.CompositeRouteEndpointAny, Enabled: true}
				require.NoError(tester, routes.Create(ctx, &route))
			}
		}
		channel := service.Channel{Name: group.Name, Status: service.StatusActive, GroupIDs: []int64{group.ID}, ModelPricing: []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: names, BillingMode: service.BillingModeToken, InputPrice: &input, OutputPrice: &output}}}
		require.NoError(tester, fixture.channels.Create(ctx, &channel))
		for accountIndex := 0; accountIndex < 5; accountIndex++ {
			account := service.Account{Name: fmt.Sprintf("%s-account-%d", group.Name, accountIndex), Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 100, GroupIDs: []int64{group.ID}, Credentials: map[string]any{"api_key": "local-fixture"}}
			require.NoError(tester, fixture.accounts.Create(ctx, &account))
			_, err = fixture.database.Exec("INSERT INTO account_groups(account_id,group_id,priority) VALUES($1,$2,0)", account.ID, group.ID)
			require.NoError(tester, err)
			extra, marshalErr := json.Marshal(map[string]any{service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{Revision: 1, Complete: true, DiscoveredModels: names, EligibleModels: members}, service.UpstreamModelMetadataExtraKey: service.UpstreamModelMetadataSnapshot{Models: metadata}})
			require.NoError(tester, marshalErr)
			_, err = fixture.database.Exec("UPDATE accounts SET extra=$1::jsonb WHERE id=$2", string(extra), account.ID)
			require.NoError(tester, err)
		}
	}
	for _, user := range fixture.users {
		_, err = fixture.database.Exec("INSERT INTO user_group_rate_multipliers(user_id,group_id,rate_multiplier) VALUES($1,$2,0.8)", user.ID, fixture.groups[0].ID)
		require.NoError(tester, err)
	}
	key := service.APIKey{Name: "bound", Key: "sk-catalog-performance-bound", UserID: fixture.users[0].ID, GroupID: &fixture.groups[0].ID, Status: service.StatusActive}
	require.NoError(tester, fixture.keyRepository.Create(ctx, &key))
	fixture.keys = append(fixture.keys, key)
	compositeKey := service.APIKey{Name: "composite", Key: "sk-catalog-performance-composite", UserID: fixture.users[0].ID, GroupID: &fixture.groups[len(fixture.groups)-1].ID, Status: service.StatusActive}
	require.NoError(tester, fixture.keyRepository.Create(ctx, &compositeKey))
	fixture.keys = append(fixture.keys, compositeKey)
	fixture.unboundKey = service.APIKey{Name: "unbound", Key: "sk-catalog-performance-unbound", UserID: fixture.users[0].ID, Status: service.StatusActive}
	require.NoError(tester, fixture.keyRepository.Create(ctx, &fixture.unboundKey))
	return fixture
}

func (fixture *catalogHTTPFixture) server(tester *testing.T) (*httptest.Server, []string, *service.ChannelService) {
	tester.Helper()
	configuration := &config.Config{}
	configuration.JWT.Secret = "catalog-performance-local-test-signing-secret"
	configuration.JWT.ExpireHour = 24
	settings := service.NewSettingService(repository.NewSettingRepository(fixture.client), configuration)
	subscriptions := repository.NewUserSubscriptionRepository(fixture.client)
	rates := repository.NewUserGroupRateRepository(fixture.database)
	keys := service.NewAPIKeyService(fixture.keyRepository, fixture.userRepository, fixture.groupRepository, subscriptions, rates, nil, configuration)
	channels := service.NewChannelService(fixture.channels, fixture.groupRepository, nil, nil, nil)
	pricing := service.NewModelPricingResolver(channels, nil)
	billing := service.NewBillingService(configuration, nil)
	gateway := service.NewGatewayService(fixture.accounts, fixture.groupRepository, nil, nil, fixture.userRepository, subscriptions, rates, nil, configuration, nil, nil, billing, nil, nil, nil, nil, nil, nil, nil, nil, nil, settings, nil, channels, pricing, service.NewCompositeRouteResolver(repository.NewCompositeModelRouteRepository(fixture.client)), nil, nil)
	gatewayHandler := handler.NewGatewayHandler(gateway, nil, nil, nil, nil, nil, nil, nil, keys, nil, nil, nil, nil, configuration, settings)
	plaza := handler.NewModelPlazaHandler(service.NewModelPlazaService(fixture.channels, fixture.groupRepository, nil, billing, pricing), keys, settings)
	authentication := service.NewAuthService(fixture.client, fixture.userRepository, nil, nil, configuration, settings, nil, nil, nil, nil, nil, nil, nil)
	tokens := make([]string, len(fixture.users))
	for index, user := range fixture.users {
		var err error
		tokens[index], err = authentication.GenerateToken(context.Background(), &user)
		require.NoError(tester, err)
	}
	router := gin.New()
	handlers := &handler.Handlers{Gateway: gatewayHandler, OpenAIGateway: &handler.OpenAIGatewayHandler{}, ModelPlaza: plaza}
	routes.RegisterGatewayRoutes(router, handlers, middleware.NewAPIKeyAuthMiddleware(keys, nil, configuration), keys, nil, nil, settings, service.NewCompositeRouteResolver(repository.NewCompositeModelRouteRepository(fixture.client)), configuration)
	routes.RegisterModelPlazaRoutes(router.Group("/api/v1"), handlers, middleware.NewOptionalJWTAuthMiddleware(authentication, service.NewUserService(fixture.userRepository, repository.NewSettingRepository(fixture.client), nil, nil), settings, nil), settings, nil)
	server := httptest.NewServer(router)
	tester.Cleanup(server.Close)
	return server, tokens, channels
}

type catalogHTTPScenario struct {
	name, path, token, protocol string
	budget                      time.Duration
}

func catalogHTTPClient(tester *testing.T, server *httptest.Server) *http.Client {
	tester.Helper()
	client := server.Client()
	transport, supported := client.Transport.(*http.Transport)
	require.True(tester, supported)
	transport.MaxIdleConns = 256
	transport.MaxIdleConnsPerHost = 128
	client.Timeout = 30 * time.Second
	return client
}

func catalogHTTPRead(client *http.Client, address string, scenario catalogHTTPScenario) ([]byte, time.Duration, error) {
	return catalogHTTPReadExpected(client, address, scenario, http.StatusOK)
}

func catalogHTTPReadExpected(client *http.Client, address string, scenario catalogHTTPScenario, expectedStatus int) ([]byte, time.Duration, error) {
	request, err := http.NewRequest(http.MethodGet, address+scenario.path, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+scenario.token)
	if scenario.protocol == "anthropic" {
		request.Header.Set("anthropic-version", "2023-06-01")
	}
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return nil, time.Since(started), err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	elapsed := time.Since(started)
	if response.StatusCode != expectedStatus {
		return body, elapsed, fmt.Errorf("HTTP %d: %s", response.StatusCode, body)
	}
	return body, elapsed, err
}

type catalogHTTPMeasurement struct {
	Scenario      string          `json:"scenario"`
	State         string          `json:"state"`
	Groups        int             `json:"groups"`
	Accounts      int             `json:"accounts"`
	Projections   int             `json:"projections"`
	Concurrency   int             `json:"concurrency"`
	Samples       int             `json:"samples"`
	Errors        int64           `json:"errors"`
	Queries       int64           `json:"queries"`
	AllocBytes    uint64          `json:"alloc_bytes_per_request"`
	Allocations   uint64          `json:"allocations_per_request"`
	ResponseBytes int             `json:"response_bytes"`
	P50           float64         `json:"p50_ms"`
	P95           float64         `json:"p95_ms"`
	P99           float64         `json:"p99_ms"`
	Budget        float64         `json:"budget_ms"`
	Durations     []time.Duration `json:"duration_ns"`
}

func catalogHTTPMeasure(tester *testing.T, fixture *catalogHTTPFixture, scenario catalogHTTPScenario, state string, concurrency, samples int, baseline []byte, request func() ([]byte, time.Duration, error)) {
	tester.Helper()
	beforeQueries := fixture.queries.Load()
	beforeBytes := fixture.responseBytes.Load()
	var beforeMemory, afterMemory runtime.MemStats
	runtime.ReadMemStats(&beforeMemory)
	durations := make([]time.Duration, samples)
	var errors atomic.Int64
	var workers sync.WaitGroup
	var firstError string
	var errorOnce sync.Once
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for index := worker; index < samples; index += concurrency {
				body, elapsed, requestErr := request()
				durations[index] = elapsed
				if requestErr != nil || !bytes.Equal(body, baseline) {
					errors.Add(1)
					errorOnce.Do(func() {
						firstError = fmt.Sprintf("error=%v bytes=%d expected=%d", requestErr, len(body), len(baseline))
					})
				}
			}
		}(worker)
	}
	workers.Wait()
	runtime.ReadMemStats(&afterMemory)
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(first, second int) bool { return sorted[first] < sorted[second] })
	percentile := func(percent int) float64 {
		return float64(sorted[(samples*percent+99)/100-1]) / float64(time.Millisecond)
	}
	measurement := catalogHTTPMeasurement{Scenario: tester.Name(), State: state, Groups: len(fixture.groups), Accounts: len(fixture.groups) * 5, Projections: len(fixture.groups) * fixture.modelCount, Concurrency: concurrency, Samples: samples, Errors: errors.Load(), Queries: fixture.queries.Load() - beforeQueries, AllocBytes: (afterMemory.TotalAlloc - beforeMemory.TotalAlloc) / uint64(samples), Allocations: (afterMemory.Mallocs - beforeMemory.Mallocs) / uint64(samples), ResponseBytes: len(baseline), P50: percentile(50), P95: percentile(95), P99: percentile(99), Budget: float64(scenario.budget) / float64(time.Millisecond), Durations: durations}
	if received := fixture.responseBytes.Load() - beforeBytes; received > 0 {
		measurement.ResponseBytes = int(received / int64(samples))
	}
	if directory := os.Getenv("CATALOG_PERFORMANCE_OUTPUT"); directory != "" {
		require.NoError(tester, os.MkdirAll(directory, 0750))
		encoded, marshalErr := json.MarshalIndent(measurement, "", "  ")
		require.NoError(tester, marshalErr)
		name := strings.NewReplacer("/", "-", "\\", "-").Replace(tester.Name()) + ".json"
		require.NoError(tester, os.WriteFile(filepath.Join(directory, name), encoded, 0600))
	}
	tester.Logf("state=%s groups=%d accounts=%d projections=%d concurrency=%d samples=%d errors=%d p50_ms=%.3f p95_ms=%.3f p99_ms=%.3f queries=%d alloc_bytes=%d allocations=%d response_bytes=%d first_error=%s", state, measurement.Groups, measurement.Accounts, measurement.Projections, concurrency, samples, measurement.Errors, measurement.P50, measurement.P95, measurement.P99, measurement.Queries, measurement.AllocBytes, measurement.Allocations, measurement.ResponseBytes, firstError)
	require.Zero(tester, measurement.Errors)
	require.Less(tester, measurement.P95, measurement.Budget)
}

func catalogHTTPAssertPayload(tester *testing.T, fixture *catalogHTTPFixture, scenario catalogHTTPScenario, body []byte) {
	tester.Helper()
	if strings.HasPrefix(scenario.name, "plaza") {
		var envelope struct {
			Data struct {
				Groups []struct {
					ID                 int64    `json:"id"`
					UserRateMultiplier *float64 `json:"user_rate_multiplier"`
					Models             []struct {
						Pricing      *json.RawMessage `json:"pricing"`
						Capabilities *json.RawMessage `json:"capabilities"`
					} `json:"models"`
				} `json:"groups"`
			} `json:"data"`
		}
		require.NoError(tester, json.Unmarshal(body, &envelope))
		tester.Logf("plaza payload: groups=%d expected=%d bytes=%d", len(envelope.Data.Groups), len(fixture.groups), len(body))
		require.Len(tester, envelope.Data.Groups, len(fixture.groups))
		for _, group := range envelope.Data.Groups {
			require.Len(tester, group.Models, fixture.modelCount)
			for _, model := range group.Models {
				require.NotNil(tester, model.Pricing)
				require.NotNil(tester, model.Capabilities)
			}
			if group.ID == fixture.groups[0].ID {
				require.NotNil(tester, group.UserRateMultiplier)
				require.Equal(tester, 0.8, *group.UserRateMultiplier)
			}
		}
	} else {
		var envelope struct {
			Data   []json.RawMessage `json:"data"`
			Models []json.RawMessage `json:"models"`
		}
		require.NoError(tester, json.Unmarshal(body, &envelope))
		if scenario.protocol == "gemini" {
			require.Len(tester, envelope.Models, fixture.modelCount)
		} else {
			require.Len(tester, envelope.Data, fixture.modelCount)
		}
	}
}

func TestCatalogHTTPPerformance(tester *testing.T) {
	samples, err := strconv.Atoi(os.Getenv("CATALOG_HTTP_PERFORMANCE_SAMPLES"))
	if err != nil || samples <= 0 {
		tester.Skip("set CATALOG_HTTP_PERFORMANCE_SAMPLES and CATALOG_PERFORMANCE_POSTGRES_DSN")
	}
	require.GreaterOrEqual(tester, samples, 1000, "release measurements require at least 1000 requests per cell")
	gin.SetMode(gin.TestMode)
	for _, scale := range []struct {
		name           string
		groups, models int
	}{{"budget", 20, 100}, {"production-shaped", 13, 150}} {
		tester.Run(scale.name, func(tester *testing.T) {
			fixture := newCatalogHTTPFixture(tester, scale.groups, scale.models)
			server, tokens, _ := fixture.server(tester)
			client := catalogHTTPClient(tester, server)
			scenarios := []catalogHTTPScenario{{"plaza-user", "/api/v1/model-plaza", tokens[0], "", 2 * time.Second}, {"plaza-admin", "/api/v1/model-plaza", tokens[1], "", 2 * time.Second}}
			for _, key := range fixture.keys {
				for _, protocol := range []string{"openai", "anthropic", "gemini"} {
					path := "/v1/models"
					if protocol == "anthropic" {
						path += "?limit=1000"
					}
					if protocol == "gemini" {
						path = "/v1beta/models?pageSize=1000"
					}
					scenarios = append(scenarios, catalogHTTPScenario{"models-" + protocol + "-" + key.Name, path, key.Key, protocol, 500 * time.Millisecond})
				}
			}
			for _, scenario := range scenarios {
				tester.Run(scenario.name, func(tester *testing.T) {
					// A fresh service graph has no populated in-memory evidence or pricing cache.
					// Its complete response is the uncached reference, including fields and prices.
					baselineServer, _, _ := fixture.server(tester)
					baseline, _, readErr := catalogHTTPRead(client, baselineServer.URL, scenario)
					baselineServer.Close()
					require.NoError(tester, readErr)
					catalogHTTPAssertPayload(tester, fixture, scenario, baseline)
					for _, concurrency := range []int{1, 10} {
						tester.Run(fmt.Sprintf("concurrency-%d", concurrency), func(tester *testing.T) {
							for warmup := 0; warmup < 20; warmup++ {
								body, _, warmErr := catalogHTTPRead(client, server.URL, scenario)
								require.NoError(tester, warmErr)
								require.True(tester, bytes.Equal(baseline, body))
							}
							tester.Run("hot", func(tester *testing.T) {
								catalogHTTPMeasure(tester, fixture, scenario, "hot", concurrency, samples, baseline, func() ([]byte, time.Duration, error) { return catalogHTTPRead(client, server.URL, scenario) })
							})
						})
					}
					tester.Run("cold", func(tester *testing.T) {
						catalogHTTPMeasure(tester, fixture, scenario, "cold", 1, 20, baseline, func() ([]byte, time.Duration, error) {
							coldServer, _, _ := fixture.server(tester)
							defer coldServer.Close()
							return catalogHTTPRead(client, coldServer.URL, scenario)
						})
					})
					tester.Run("invalidated", func(tester *testing.T) {
						catalogHTTPMeasure(tester, fixture, scenario, "invalidated", 1, 20, baseline, func() ([]byte, time.Duration, error) {
							_, invalidateErr := fixture.database.Exec("UPDATE model_catalog_generations SET generation=generation+1 WHERE scope='catalog'")
							if invalidateErr != nil {
								return nil, 0, invalidateErr
							}
							return catalogHTTPRead(client, server.URL, scenario)
						})
					})
					tester.Run("simultaneous-refresh", func(tester *testing.T) {
						_, invalidateErr := fixture.database.Exec("UPDATE model_catalog_generations SET generation=generation+1 WHERE scope='catalog'")
						require.NoError(tester, invalidateErr)
						start := make(chan struct{})
						var ready atomic.Int64
						catalogHTTPMeasure(tester, fixture, scenario, "simultaneous-refresh", 10, 10, baseline, func() ([]byte, time.Duration, error) {
							if ready.Add(1) == 10 {
								close(start)
							}
							<-start
							return catalogHTTPRead(client, server.URL, scenario)
						})
					})
				})
			}
			for _, protocol := range []string{"openai", "anthropic", "gemini"} {
				tester.Run("models-"+protocol+"-unbound-rejection", func(tester *testing.T) {
					path := "/v1/models"
					if protocol == "gemini" {
						path = "/v1beta/models"
					}
					scenario := catalogHTTPScenario{"unbound-rejection", path, fixture.unboundKey.Key, protocol, 500 * time.Millisecond}
					request := func() ([]byte, time.Duration, error) {
						return catalogHTTPReadExpected(client, server.URL, scenario, http.StatusForbidden)
					}
					baseline, _, requestErr := request()
					require.NoError(tester, requestErr)
					require.Contains(tester, string(baseline), "error")
					for _, parallelism := range []int{1, 10} {
						tester.Run(fmt.Sprintf("concurrency-%d", parallelism), func(tester *testing.T) {
							for warmup := 0; warmup < 20; warmup++ {
								_, _, requestErr := request()
								require.NoError(tester, requestErr)
							}
							catalogHTTPMeasure(tester, fixture, scenario, "unbound-rejection", parallelism, samples, baseline, request)
						})
					}
				})
			}
			tester.Run("authorization-revocation", func(tester *testing.T) {
				scenario := catalogHTTPScenario{name: "models-openai-bound", path: "/v1/models", token: fixture.keys[0].Key}
				body, _, requestErr := catalogHTTPRead(client, server.URL, scenario)
				require.NoError(tester, requestErr)
				catalogHTTPAssertPayload(tester, fixture, scenario, body)
				_, updateErr := fixture.database.Exec("UPDATE groups SET model_allowlist=$1::jsonb WHERE id=$2", `{"enabled":true,"models":[]}`, fixture.groups[0].ID)
				require.NoError(tester, updateErr)
				body, _, requestErr = catalogHTTPRead(client, server.URL, scenario)
				require.NoError(tester, requestErr)
				var envelope struct {
					Data []json.RawMessage `json:"data"`
				}
				require.NoError(tester, json.Unmarshal(body, &envelope))
				require.Empty(tester, envelope.Data, "warm evidence must not postpone an authorization revocation")
				_, updateErr = fixture.database.Exec("UPDATE api_keys SET status='inactive' WHERE id=$1", fixture.keys[0].ID)
				require.NoError(tester, updateErr)
				_, _, requestErr = catalogHTTPRead(client, server.URL, scenario)
				require.ErrorContains(tester, requestErr, "HTTP 401")
			})
		})
	}
}
