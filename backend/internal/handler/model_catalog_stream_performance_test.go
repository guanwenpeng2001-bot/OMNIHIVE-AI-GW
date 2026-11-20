package handler_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository" //nolint:depguard // Opt-in external-package HTTP integration test assembles the real repository graph.
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/routes"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/redis/go-redis/v9" //nolint:depguard // Client connects only to the in-process random-port miniredis fixture.
	"github.com/stretchr/testify/require"
)

// Read only semantic content. HTTP headers, comments, heartbeats, role-only
// chunks and response.created deliberately cannot terminate this measurement.
type catalogStreamReader struct {
	io.Reader
	received int64
}

func (reader *catalogStreamReader) Read(buffer []byte) (int, error) {
	count, err := reader.Reader.Read(buffer)
	reader.received += int64(count)
	return count, err
}

func catalogFirstContent(client *http.Client, address, token, body string, received *int64) ([]byte, time.Duration, error) {
	request, err := http.NewRequest(http.MethodPost, address+"/v1/responses", strings.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return nil, time.Since(started), err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		return payload, time.Since(started), fmt.Errorf("HTTP %d: %s", response.StatusCode, payload)
	}
	reader := &catalogStreamReader{Reader: response.Body}
	defer func() { *received = reader.received }()
	scanner := bufio.NewScanner(reader)
	var first time.Duration
	var content []byte
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event) != nil {
			continue
		}
		if event.Type == "response.output_text.delta" && event.Delta != "" && first == 0 {
			first = time.Since(started)
			content = []byte(event.Delta)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, first, err
	}
	if first == 0 {
		return nil, 0, fmt.Errorf("stream ended without model content")
	}
	return content, first, nil
}

func TestCatalogGatewayFirstContentPerformance(tester *testing.T) {
	samples, err := strconv.Atoi(os.Getenv("CATALOG_HTTP_PERFORMANCE_SAMPLES"))
	if err != nil || samples <= 0 {
		tester.Skip("set CATALOG_HTTP_PERFORMANCE_SAMPLES and CATALOG_PERFORMANCE_POSTGRES_DSN")
	}
	require.GreaterOrEqual(tester, samples, 1000)
	gin.SetMode(gin.TestMode)
	fixture := newCatalogHTTPFixture(tester, 20, 100)
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			http.Error(writer, "unexpected upstream path", http.StatusNotFound)
			return
		}
		_, _ = io.Copy(io.Discard, request.Body)
		identity := upstreamCalls.Add(1)
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, supported := writer.(http.Flusher)
		if !supported {
			http.Error(writer, "stream flushing unavailable", http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprintf(writer, ": heartbeat\n\nevent: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"fixture-%d\",\"object\":\"response\",\"model\":\"model-0000\",\"status\":\"in_progress\",\"output\":[]}}\n\n", identity)
		flusher.Flush()
		time.Sleep(5 * time.Millisecond)
		_, _ = fmt.Fprint(writer, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"hello\"}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprintf(writer, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture-%d\",\"object\":\"response\",\"model\":\"model-0000\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}],\"usage\":{\"input_tokens\":2,\"output_tokens\":1,\"total_tokens\":3}}}\n\ndata: [DONE]\n\n", identity)
	}))
	tester.Cleanup(upstream.Close)
	_, err = fixture.database.Exec("UPDATE accounts SET credentials=credentials || jsonb_build_object('base_url',$1::text), extra=extra || '{\"openai_passthrough\":true}'::jsonb", upstream.URL)
	require.NoError(tester, err)
	configuration := &config.Config{}
	configuration.Security.URLAllowlist.AllowPrivateHosts = true
	configuration.Security.URLAllowlist.AllowInsecureHTTP = true
	configuration.Gateway.StreamDataIntervalTimeout = 30
	configuration.Gateway.MaxBodySize = 256 * 1024 * 1024
	configuration.Gateway.TextMaxBodySize = 32 * 1024 * 1024
	configuration.Gateway.MaxIdleConns = 2560
	configuration.Gateway.MaxIdleConnsPerHost = 120
	settings := service.NewSettingService(repository.NewSettingRepository(fixture.client), configuration)
	subscriptions := repository.NewUserSubscriptionRepository(fixture.client)
	rates := repository.NewUserGroupRateRepository(fixture.database)
	keys := service.NewAPIKeyService(fixture.keyRepository, fixture.userRepository, fixture.groupRepository, subscriptions, rates, nil, configuration)
	// This Redis protocol fixture lives inside this test process, on a random
	// loopback port. It never connects to either installed Redis instance.
	redisServer := miniredis.RunT(tester)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	tester.Cleanup(func() { require.NoError(tester, redisClient.Close()) })
	concurrency := service.NewConcurrencyService(repository.NewConcurrencyCache(redisClient, 1, 30))
	billingCache := service.NewBillingCacheService(nil, fixture.userRepository, subscriptions, fixture.keyRepository, nil, rates, configuration, nil)
	tester.Cleanup(billingCache.Stop)
	channels := service.NewChannelService(fixture.channels, fixture.groupRepository, nil, nil, nil)
	pricing := service.NewModelPricingResolver(channels, nil)
	billing := service.NewBillingService(configuration, nil)
	receipts := service.NewUsageSettlementService(repository.NewUsageSettlementReceiptWithQuotaRepository(fixture.database), nil, billingCache)
	gateway := service.ProvideOpenAIGatewayService(fixture.accounts, repository.NewUsageLogRepository(fixture.client, fixture.database), repository.NewUsageBillingRepository(fixture.client, fixture.database), fixture.userRepository, subscriptions, rates, nil, configuration, nil, concurrency, billing, nil, billingCache, repository.NewHTTPUpstream(configuration), nil, nil, nil, pricing, channels, nil, settings, nil, receipts)
	tester.Cleanup(gateway.CloseOpenAIWSPool)
	gatewayHandler := handler.NewOpenAIGatewayHandler(gateway, concurrency, billingCache, keys, nil, nil, nil, nil, configuration)
	composite := service.NewCompositeRouteResolver(repository.NewCompositeModelRouteRepository(fixture.client))
	catalogGateway := service.NewGatewayService(fixture.accounts, fixture.groupRepository, nil, nil, fixture.userRepository, subscriptions, rates, nil, configuration, nil, concurrency, billing, nil, billingCache, nil, repository.NewHTTPUpstream(configuration), nil, nil, nil, nil, nil, settings, nil, channels, pricing, composite, nil, nil)
	catalogHandler := handler.NewGatewayHandler(catalogGateway, gateway, nil, nil, nil, concurrency, billingCache, nil, keys, nil, nil, nil, nil, configuration, settings)
	router := gin.New()
	routes.RegisterGatewayRoutes(router, &handler.Handlers{Gateway: catalogHandler, OpenAIGateway: gatewayHandler}, middleware.NewAPIKeyAuthMiddleware(keys, nil, configuration), keys, nil, nil, settings, composite, configuration)
	server := httptest.NewServer(router)
	tester.Cleanup(server.Close)
	client := catalogHTTPClient(tester, server)
	requestBody := `{"model":"model-0000","input":"hello","stream":true,"max_output_tokens":16}`
	for _, phase := range []struct {
		name                       string
		parallelism, count, warmup int
	}{{"cold", 1, 1, 0}, {"hot", 1, samples, 20}, {"hot", 10, samples, 20}, {"invalidated", 1, 20, 0}, {"simultaneous-refresh", 10, 10, 0}} {
		parallelism := phase.parallelism
		tester.Run(fmt.Sprintf("%s/concurrency-%d", phase.name, parallelism), func(tester *testing.T) {
			var pairIndex atomic.Int64
			type observation struct {
				DirectNS, GatewayNS, DeltaNS time.Duration
				DirectBytes, GatewayBytes    int64
				GatewayFirst                 bool
			}
			observations := make([]observation, phase.count+phase.warmup)
			tester.Cleanup(func() {
				if directory := os.Getenv("CATALOG_PERFORMANCE_OUTPUT"); directory != "" {
					require.NoError(tester, os.MkdirAll(directory, 0750))
					encoded, marshalErr := json.MarshalIndent(observations, "", "  ")
					require.NoError(tester, marshalErr)
					require.NoError(tester, os.WriteFile(filepath.Join(directory, fmt.Sprintf("first-content-pairs-%s-concurrency-%d.json", phase.name, parallelism)), encoded, 0600))
				}
			})
			pair := func() ([]byte, time.Duration, error) {
				if phase.name == "invalidated" {
					if _, invalidateErr := fixture.database.Exec("UPDATE model_catalog_generations SET generation=generation+1 WHERE scope='catalog'"); invalidateErr != nil {
						return nil, 0, invalidateErr
					}
				}
				var directBody, gatewayBody []byte
				var directElapsed, gatewayElapsed time.Duration
				var directErr, gatewayErr error
				var directBytes, gatewayBytes int64
				index := pairIndex.Add(1) - 1
				if index%2 == 0 {
					gatewayBody, gatewayElapsed, gatewayErr = catalogFirstContent(client, server.URL, fixture.keys[0].Key, requestBody, &gatewayBytes)
					directBody, directElapsed, directErr = catalogFirstContent(client, upstream.URL, "local-fixture", requestBody, &directBytes)
				} else {
					directBody, directElapsed, directErr = catalogFirstContent(client, upstream.URL, "local-fixture", requestBody, &directBytes)
					gatewayBody, gatewayElapsed, gatewayErr = catalogFirstContent(client, server.URL, fixture.keys[0].Key, requestBody, &gatewayBytes)
				}
				observations[index] = observation{directElapsed, gatewayElapsed, gatewayElapsed - directElapsed, directBytes, gatewayBytes, index%2 == 0}
				fixture.responseBytes.Add(gatewayBytes)
				if directErr != nil {
					return nil, 0, directErr
				}
				if gatewayErr != nil {
					return nil, 0, gatewayErr
				}
				if string(directBody) != string(gatewayBody) {
					return nil, 0, fmt.Errorf("direct/gateway content mismatch")
				}
				return gatewayBody, gatewayElapsed - directElapsed, nil
			}
			for warmup := 0; warmup < phase.warmup; warmup++ {
				body, _, warmErr := pair()
				require.NoError(tester, warmErr)
				require.Equal(tester, "hello", string(body))
			}
			measurePair := pair
			if phase.name == "simultaneous-refresh" {
				_, invalidateErr := fixture.database.Exec("UPDATE model_catalog_generations SET generation=generation+1 WHERE scope='catalog'")
				require.NoError(tester, invalidateErr)
				start := make(chan struct{})
				var ready atomic.Int64
				measurePair = func() ([]byte, time.Duration, error) {
					if ready.Add(1) == int64(parallelism) {
						close(start)
					}
					<-start
					return pair()
				}
			}
			catalogHTTPMeasure(tester, fixture, catalogHTTPScenario{budget: 50 * time.Millisecond}, phase.name, parallelism, phase.count, []byte("hello"), measurePair)
		})
	}
}

// TestCatalogDumpQueryTally must run after the performance cells: it prints the
// process-wide normalized-SQL counts accumulated in catalogQueryTally.
func TestCatalogDumpQueryTally(tester *testing.T) {
	catalogQueryTally.Range(func(key, value any) bool {
		if tally, ok := value.(*atomic.Int64); ok {
			tester.Logf("TALLY %8d %s", tally.Load(), key)
		}
		return true
	})
}
