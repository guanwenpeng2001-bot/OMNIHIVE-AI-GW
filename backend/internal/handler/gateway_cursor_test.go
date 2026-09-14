//go:build unit

package handler

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCursorModelFallbackAndEndpointOwnership(t *testing.T) {
	models := defaultModelIDsForPlatform(service.PlatformCursor)
	require.Contains(t, models, "default")
	require.Contains(t, models, "claude-sonnet-4-6")
	for _, inbound := range []string{EndpointChatCompletions, EndpointMessages, EndpointResponses} {
		require.Equal(t, "/agent.v1.AgentService/Run", DeriveUpstreamEndpoint(inbound, inbound, service.PlatformCursor))
	}
	requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	setActualUpstreamEndpoint(requestContext, EndpointChatCompletions)
	require.Equal(t, EndpointChatCompletions, GetUpstreamEndpoint(requestContext, service.PlatformCursor))
}

func TestCursorPublicModelsFallsBackWithoutMapping(t *testing.T) {
	groupID := int64(81)
	handler := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{groupID: {{ID: 1, Platform: service.PlatformCursor}}}})
	writer := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(writer)
	requestContext.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	requestContext.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{ID: groupID, Platform: service.PlatformCursor}})
	handler.Models(requestContext)
	require.Equal(t, http.StatusOK, writer.Code)
	var response gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(writer.Body.Bytes(), &response))
	require.Equal(t, service.DefaultCursorModelIDs(), modelIDsForTest(response.Data))
	for _, model := range response.Data {
		require.Equal(t, "cursor", model.OwnedBy)
	}
}

type cursorRouteSessionCache struct{ service.CursorSessionCache }

func (cursorRouteSessionCache) Get(context.Context, string) (cursor.Session, error) {
	return cursor.Session{Token: "synthetic-session", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

type cursorRouteUsageRepo struct {
	service.UsageLogRepository
	logs  []*service.UsageLog
	mutex sync.Mutex
}

func (repo *cursorRouteUsageRepo) CreateBestEffort(_ context.Context, usage *service.UsageLog) error {
	repo.mutex.Lock()
	defer repo.mutex.Unlock()
	repo.logs = append(repo.logs, usage)
	return nil
}
func (repo *cursorRouteUsageRepo) Create(ctx context.Context, usage *service.UsageLog) (bool, error) {
	return true, repo.CreateBestEffort(ctx, usage)
}

// Count acquired and released leases at the real handler boundary.
type cursorRouteSlotCache struct {
	fakeConcurrencyCache
	accounts, users atomic.Int32
}

func (cache *cursorRouteSlotCache) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	cache.accounts.Add(1)
	return true, nil
}
func (cache *cursorRouteSlotCache) ReleaseAccountSlot(context.Context, int64, string) error {
	cache.accounts.Add(-1)
	return nil
}
func (cache *cursorRouteSlotCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	cache.users.Add(1)
	return true, nil
}
func (cache *cursorRouteSlotCache) ReleaseUserSlot(context.Context, int64, string) error {
	cache.users.Add(-1)
	return nil
}

func TestCursorRoutes(t *testing.T) {
	for _, scenario := range []struct {
		name, path                      string
		stream, preFailure, interrupted bool
	}{
		{name: "chat_non_stream", path: EndpointChatCompletions},
		{name: "chat_stream_once", path: EndpointChatCompletions, stream: true},
		{name: "before_output_failover", path: EndpointChatCompletions, stream: true, preFailure: true},
		{name: "after_output_partial_usage_once", path: EndpointChatCompletions, stream: true, interrupted: true},
		{name: "messages_non_stream", path: EndpointMessages},
		{name: "messages_stream", path: EndpointMessages, stream: true},
		{name: "responses_non_stream", path: EndpointResponses},
		{name: "responses_stream", path: EndpointResponses, stream: true},
		{name: "responses_partial_usage_once", path: EndpointResponses, stream: true, interrupted: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var attempts atomic.Int32
			var machinesMu sync.Mutex
			var machines []string
			// Synthetic fixtures reused from the wire driver's HTTP/2 tests.
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != cursor.RunEndpoint {
					_, _ = writer.Write([]byte{0x0a, 7, 'd', 'e', 'f', 'a', 'u', 'l', 't'})
					return
				}
				var frameHeader [5]byte
				if _, err := io.ReadFull(request.Body, frameHeader[:]); err != nil {
					return
				}
				if _, err := io.CopyN(io.Discard, request.Body, int64(binary.BigEndian.Uint32(frameHeader[1:]))); err != nil {
					return
				}
				machinesMu.Lock()
				machines = append(machines, request.Header.Get("X-Cursor-Checksum"))
				machinesMu.Unlock()
				attempt := attempts.Add(1)
				if scenario.preFailure && attempt == 1 {
					writer.WriteHeader(503)
					writer.(http.Flusher).Flush()
					return
				}
				fixtures := []string{"0a0722050a03776879", "0a060a040a026865", "0a070a050a036c6c6f", "0a0442020863", "0a0c720a080c1005180620022803"}
				if scenario.interrupted {
					fixtures = []string{"0a090a070a0568656c6c6f", "0a0442020803"}
				}
				for _, fixture := range fixtures {
					payload, _ := hex.DecodeString(fixture)
					header := make([]byte, 5)
					binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
					_, _ = writer.Write(append(header, payload...))
					writer.(http.Flusher).Flush()
				}
				if scenario.interrupted {
					_, _ = writer.Write([]byte{0, 0, 0, 0, 2, 1})
					writer.(http.Flusher).Flush()
				}
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()
			groupID := int64(81)
			group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformCursor, Status: service.StatusActive}
			accounts := []*service.Account{}
			for _, accountID := range []int64{1, 2} {
				accounts = append(accounts, &service.Account{ID: accountID, Platform: service.PlatformCursor, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: int(accountID), Credentials: map[string]any{"api_key": "synthetic-key", "machine_id": fmt.Sprint(accountID)}, AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}}})
			}
			handler, cleanup := newTestGatewayHandler(t, group, accounts)
			defer cleanup()
			cfg := &config.Config{RunMode: config.RunModeSimple}
			repo := &cursorRouteUsageRepo{}
			slots := &cursorRouteSlotCache{}
			concurrency := service.NewConcurrencyService(slots)
			handler.concurrencyHelper = NewConcurrencyHelper(concurrency, SSEPingFormatClaude, 0)
			snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
			handler.gatewayService = service.NewGatewayService(nil, &fakeGroupRepo{group: group}, repo, nil, nil, nil, nil, nil, cfg, snapshot, concurrency, service.NewBillingService(cfg, nil), nil, nil, nil, nil, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			handler.cursorGatewayService = service.NewCursorGatewayWithClientForTest(cursorRouteSessionCache{}, &cursor.Client{HTTPClient: server.Client(), BaseURL: server.URL})
			pool := newUsageRecordTestPool(t)
			defer pool.Stop()
			handler.usageRecordWorkerPool = pool
			apiKey := &service.APIKey{ID: 82, UserID: 83, GroupID: &groupID, Group: group, Status: service.StatusActive, User: &service.User{ID: 83, Concurrency: 10, Balance: 100}}
			router := gin.New()
			router.Use(func(requestContext *gin.Context) {
				requestContext.Set(string(middleware2.ContextKeyAPIKey), apiKey)
				requestContext.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 83, Concurrency: 10})
				requestContext.Request = requestContext.Request.WithContext(context.WithValue(requestContext.Request.Context(), ctxkey.Group, group))
				requestContext.Next()
			})
			router.POST(EndpointChatCompletions, handler.ChatCompletions)
			router.POST(EndpointMessages, handler.Messages)
			router.POST(EndpointResponses, handler.Responses)
			body := fmt.Sprintf(`{"model":"default","stream":%t,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}],"input":"hi","max_tokens":10}`, scenario.stream)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, scenario.path, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			pool.Stop()

			require.Equal(t, 200, recorder.Code, recorder.Body.String())
			if !scenario.stream || scenario.interrupted {
				require.Contains(t, recorder.Body.String(), "hello")
			}
			require.Zero(t, slots.accounts.Load(), "account slots must be released")
			require.Zero(t, slots.users.Load(), "user slots must be released")
			require.Equal(t, uint64(1), pool.Stats().SubmittedTasks)
			require.Len(t, repo.logs, 1)
			expectedAttempts := int32(1)
			expectedAccount := int64(1)
			if scenario.preFailure {
				expectedAttempts = 2
				expectedAccount = 2
			}
			require.Equal(t, expectedAttempts, attempts.Load())
			require.Equal(t, expectedAccount, repo.logs[0].AccountID)
			machinesMu.Lock()
			defer machinesMu.Unlock()
			require.Contains(t, machines[0], "1/")
			if scenario.preFailure {
				require.Contains(t, machines[1], "2/")
			}
			if scenario.interrupted {
				require.NotContains(t, recorder.Body.String(), "[DONE]")
				require.Contains(t, recorder.Body.String(), "Cursor stream interrupted")
				require.Equal(t, 3, repo.logs[0].OutputTokens)
				require.Equal(t, 1, repo.logs[0].InputTokens)
			} else {
				require.Equal(t, 12, repo.logs[0].InputTokens)
				require.Equal(t, 5, repo.logs[0].OutputTokens)
				require.Equal(t, 6, repo.logs[0].CacheReadTokens)
				require.Equal(t, 2, repo.logs[0].CacheCreationTokens)
				if scenario.stream {
					var eventTypes []string
					var textDeltas, thinkingDeltas strings.Builder
					var finalUsage map[string]any
					for _, line := range strings.Split(recorder.Body.String(), "\n") {
						if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
							continue
						}
						var event map[string]any
						require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
						if kind, ok := event["type"].(string); ok {
							eventTypes = append(eventTypes, kind)
						}
						if usage, ok := event["usage"].(map[string]any); ok {
							finalUsage = usage
						}
						switch scenario.path {
						case EndpointMessages:
							if delta, ok := event["delta"].(map[string]any); ok {
								if value, ok := delta["text"].(string); ok {
									textDeltas.WriteString(value)
								}
								if value, ok := delta["thinking"].(string); ok {
									thinkingDeltas.WriteString(value)
								}
							}
						case EndpointResponses:
							if event["type"] == "response.output_text.delta" {
								textDeltas.WriteString(event["delta"].(string))
							}
							if event["type"] == "response.reasoning_summary_text.delta" {
								thinkingDeltas.WriteString(event["delta"].(string))
							}
							if event["type"] == "response.completed" {
								finalUsage = event["response"].(map[string]any)["usage"].(map[string]any)
							}
						default:
							for _, choice := range event["choices"].([]any) {
								delta := choice.(map[string]any)["delta"].(map[string]any)
								if value, ok := delta["content"].(string); ok {
									textDeltas.WriteString(value)
								}
								if value, ok := delta["reasoning_content"].(string); ok {
									thinkingDeltas.WriteString(value)
								}
							}
						}
					}
					require.Equal(t, "hello", textDeltas.String())
					require.Equal(t, "why", thinkingDeltas.String())
					require.Equal(t, false, finalUsage["estimated"])
					switch scenario.path {
					case EndpointMessages:
						require.Equal(t, []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "content_block_start", "content_block_delta", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}, eventTypes)
						require.Equal(t, float64(12), finalUsage["input_tokens"])
						require.Equal(t, float64(5), finalUsage["output_tokens"])
						require.Equal(t, float64(6), finalUsage["cache_read_input_tokens"])
						require.Equal(t, float64(2), finalUsage["cache_creation_input_tokens"])
					case EndpointResponses:
						require.Equal(t, "response.created", eventTypes[0])
						require.Equal(t, "response.completed", eventTypes[len(eventTypes)-1])
						require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.completed"))
						require.Equal(t, float64(20), finalUsage["input_tokens"])
						require.Equal(t, float64(5), finalUsage["output_tokens"])
						require.Equal(t, float64(3), finalUsage["output_tokens_details"].(map[string]any)["reasoning_tokens"])
					default:
						require.Equal(t, 1, strings.Count(recorder.Body.String(), "[DONE]"))
						require.Equal(t, float64(20), finalUsage["prompt_tokens"])
					}
				} else {
					var response map[string]any
					require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
					usage := response["usage"].(map[string]any)
					require.Equal(t, false, usage["estimated"])
					if scenario.path == EndpointMessages {
						require.Equal(t, "message", response["type"])
						require.Equal(t, "end_turn", response["stop_reason"])
						require.Equal(t, float64(5), usage["output_tokens"])
					}
					if scenario.path == EndpointResponses {
						require.Equal(t, "response", response["object"])
						require.Equal(t, "completed", response["status"])
						require.Equal(t, float64(3), usage["output_tokens_details"].(map[string]any)["reasoning_tokens"])
					}
				}
			}
		})
	}
}

func TestCursorCompositeModelsIncludeMappedAndDefaultCatalog(t *testing.T) {
	require.Subset(t, defaultModelIDsForPlatform(service.PlatformComposite), service.DefaultCursorModelIDs())
	for _, mapped := range []bool{false, true} {
		t.Run(fmt.Sprint(mapped), func(t *testing.T) {
			groupID := int64(82)
			account := service.Account{ID: 1, Platform: service.PlatformCursor}
			expected := service.DefaultCursorModelIDs()
			if mapped {
				account.Credentials = map[string]any{"model_mapping": map[string]any{"cursor-custom": "default"}}
				expected = []string{"cursor-custom"}
			}
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{groupID: {account}}})
			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{ID: groupID, Platform: service.PlatformComposite}})
			h.Models(c)
			require.Equal(t, http.StatusOK, writer.Code)
			var response gatewayModelsResponseForTest
			require.NoError(t, json.Unmarshal(writer.Body.Bytes(), &response))
			require.ElementsMatch(t, expected, modelIDsForTest(response.Data))
		})
	}
}
