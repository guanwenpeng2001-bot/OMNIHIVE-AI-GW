package integrationtests_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const corePrivateModel = "Private-ID-Case-2026-09"
const corePricedModel = "gpt-5.4"
const corePricedAlias = "Selected-Priced-Alias"

// The only HTTP transport double is the upstream boundary. It must never send
// to a real provider, even if an accidental production fallback changes the URL.
type coreFlowUpstream struct {
	server        *httptest.Server
	mu            sync.Mutex
	models        []string
	failDiscovery bool
	sent          []string
}

func newCoreFlowUpstream(t *testing.T) *coreFlowUpstream {
	t.Helper()
	upstream := &coreFlowUpstream{models: []string{corePricedModel, "Discovered-Unselected"}}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		upstream.mu.Lock()
		defer upstream.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet && request.URL.Path == "/v1/models" {
			if upstream.failDiscovery {
				w.WriteHeader(503)
				_, _ = io.WriteString(w, `{"error":{"message":"fixture discovery unavailable"}}`)
				return
			}
			entries := make([]map[string]any, 0, len(upstream.models))
			for _, model := range upstream.models {
				entries = append(entries, map[string]any{"id": model, "object": "model"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": entries})
			return
		}
		if request.Method != http.MethodPost || request.URL.Path != "/v1/responses" {
			http.Error(w, "unexpected fixture path", 404)
			return
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(w, "body", 400)
			return
		}
		model := gjson.GetBytes(body, "model").String()
		upstream.sent = append(upstream.sent, model)
		response := map[string]any{"id": "resp_" + uuid.NewString(), "object": "response", "status": "completed", "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "fixture answer"}}}}, "usage": map[string]any{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120}}
		if !strings.Contains(gjson.GetBytes(body, "input").String(), "omit declaration") {
			response["model"] = model + "-Declared"
		}
		if gjson.GetBytes(body, "stream").Bool() {
			w.Header().Set("Content-Type", "text/event-stream")
			created := map[string]any{"type": "response.created", "response": map[string]any{"id": response["id"], "object": "response", "status": "in_progress"}}
			terminal := map[string]any{"type": "response.completed", "response": response}
			for _, event := range []map[string]any{created, terminal} {
				encoded, _ := json.Marshal(event)
				_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], encoded)
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Errorf("flush fixture response: %v", err)
					return
				}
			}
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (upstream *coreFlowUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	allowed, _ := url.Parse(upstream.server.URL)
	if request.URL.Scheme != allowed.Scheme || request.URL.Host != allowed.Host {
		return nil, fmt.Errorf("core flow fixture rejected non-fixture upstream")
	}
	return upstream.server.Client().Do(request)
}
func (upstream *coreFlowUpstream) DoWithTLS(request *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return upstream.Do(request, proxy, accountID, concurrency)
}
func (upstream *coreFlowUpstream) calls() []string {
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	return append([]string(nil), upstream.sent...)
}

type coreFlowFixture struct {
	t                                              *testing.T
	db                                             *sql.DB
	ctx                                            context.Context
	router                                         *gin.Engine
	upstream                                       *coreFlowUpstream
	redisServer                                    *miniredis.Miniredis
	accounts                                       service.AccountRepository
	users                                          service.UserRepository
	discovery                                      *service.AccountTestService
	userID, otherUserID, accountID, groupID, keyID int64
	secret                                         string
}

func newCoreFlowFixture(t *testing.T) *coreFlowFixture {
	t.Helper()
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires explicitly assigned exclusive UPSTREAM_JOB_TEST_DSN (job_test)")
	}
	require.NoError(t, testresources.ValidatePostgres(dsn, "job_test"))
	// Discovery must reach our HTTP fixture; the transport above still rejects
	// every external destination. No application server or shared Redis is used.
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "false")
	db, client, accounts := testutil.NewCapabilityPostgres(t)
	ctx := domain.WithWorkspaceScope(context.Background(), domain.DefaultWorkspaceID)
	f := &coreFlowFixture{t: t, db: db, ctx: ctx, accounts: accounts, upstream: newCoreFlowUpstream(t)}
	redisServer := miniredis.RunT(t)
	f.redisServer = redisServer
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"127.0.0.1"}
	cfg.Database.MaxOpenConns = 8
	cfg.APIKeyAuth.L1Size = 32
	cfg.APIKeyAuth.L1TTLSeconds = 60
	cfg.APIKeyAuth.L2TTLSeconds = 60
	db.SetMaxOpenConns(8)
	f.users = repository.NewUserRepository(client, db)
	groups := repository.NewGroupRepository(client, db)
	keys := repository.NewAPIKeyRepository(client, db)
	rates := repository.NewUserGroupRateRepository(db)
	usage := repository.NewUsageLogRepository(client, db)
	billingRepo := repository.NewUsageBillingRepository(client, db)
	keyService := service.NewAPIKeyService(keys, f.users, groups, nil, rates, repository.NewAPIKeyCache(redisClient), cfg)
	keyService.SetCredentialRepository(repository.NewAPIKeyCredentialRepository(db))
	keyService.StartAuthCacheInvalidationSubscriber(ctx)
	t.Cleanup(keyService.StopAuthCacheInvalidationSubscriber)
	require.Eventually(t, func() bool { return keyService.AuthCacheInvalidationSubscriberHealth().Connected }, time.Second, time.Millisecond)
	concurrency := service.NewConcurrencyService(repository.NewConcurrencyCache(redisClient, 5, 30))
	keyService.SetConcurrencyService(concurrency)
	billingCache := service.NewBillingCacheService(repository.NewBillingCache(redisClient), f.users, nil, keys, nil, rates, cfg, nil)
	t.Cleanup(billingCache.Stop)
	billing := service.NewBillingService(cfg, nil)
	pricing := service.NewModelPricingResolver(nil, billing)
	settlement := service.NewUsageSettlementService(repository.NewUsageSettlementReceiptRepository(db), nil, billingCache)
	cache := repository.NewGatewayCache(redisClient)
	openAI := service.ProvideOpenAIGatewayService(accounts, usage, billingRepo, f.users, nil, rates, cache, cfg, nil, concurrency, billing, nil, billingCache, f.upstream, nil, nil, nil, pricing, nil, nil, nil, nil, settlement)
	gateway := service.NewGatewayService(accounts, groups, usage, billingRepo, f.users, nil, rates, cache, cfg, nil, concurrency, billing, nil, billingCache, nil, f.upstream, nil, nil, nil, nil, nil, nil, nil, nil, pricing, nil, nil, nil)
	adminService := service.NewAdminService(cfg, f.users, usage, repository.NewAdminGroupRepository(client, db), repository.NewAdminAccountRepository(client, db, nil), nil, keys, nil, rates, nil, billingCache, nil, nil, keyService, client, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	f.discovery = service.NewAccountTestService(accounts, nil, nil, nil, nil, f.upstream, cfg, nil)
	// Exercise the optional scheduled refresh deterministically below using the
	// same service entry point. Avoid the global import-probe scheduler outliving
	// database cleanup; its queue/probe behavior has separate handler tests.
	accountHandler := admin.NewAccountHandler(adminService, nil, nil, nil, nil, nil, nil, nil, nil, concurrency, nil, nil, nil, nil)
	catalogHandler := admin.NewAccountHandler(adminService, nil, nil, nil, nil, nil, nil, nil, f.discovery, concurrency, nil, nil, nil, nil)
	groupHandler := admin.NewGroupHandlerWithConfig(adminService, nil, nil, cfg)
	keyHandler := handler.NewAPIKeyHandler(keyService)
	usageService := service.NewUsageService(usage, f.users, client, keyService)
	usageHandler := handler.NewUsageHandler(usageService, keyService, nil, nil)
	adminUsageHandler := admin.NewUsageHandler(usageService, keyService, adminService, nil)
	modelsHandler := handler.NewGatewayHandler(gateway, openAI, nil, nil, nil, concurrency, billingCache, nil, keyService, nil, nil, nil, nil, cfg, nil)
	responsesHandler := handler.NewOpenAIGatewayHandler(openAI, concurrency, billingCache, keyService, nil, nil, nil, nil, cfg)
	for _, target := range []*int64{&f.userID, &f.otherUserID} {
		user := &service.User{Email: "core-flow-" + uuid.NewString() + "@example.invalid", Username: "core-flow", PasswordHash: "login-identity-fixture", Role: service.RoleUser, Status: service.StatusActive, Balance: 100, Concurrency: 4}
		require.NoError(t, f.users.Create(ctx, user))
		*target = user.ID
	}
	// Remove this scenario's derived receipt links before their source receipts.
	t.Cleanup(func() {
		_, err := db.ExecContext(context.Background(), `DELETE FROM usage_billing_dedup WHERE api_key_id=$1`, f.keyID)
		require.NoError(t, err)
		_, err = db.ExecContext(context.Background(), `DELETE FROM billing_movement_links WHERE receipt_id IN (SELECT id FROM usage_settlement_receipts WHERE user_id IN ($1,$2))`, f.userID, f.otherUserID)
		require.NoError(t, err)
		_, err = db.ExecContext(context.Background(), `DELETE FROM billing_settlement_outbox WHERE receipt_id IN (SELECT id FROM usage_settlement_receipts WHERE user_id IN ($1,$2))`, f.userID, f.otherUserID)
		require.NoError(t, err)
		for _, statement := range []string{
			`DELETE FROM billing_movement_links m USING usage_settlement_receipts r
			 WHERE m.receipt_id=r.id AND r.user_id IN ($1,$2)`,
			`DELETE FROM usage_settlement_receipts WHERE user_id IN ($1,$2)`,
			`DELETE FROM usage_logs WHERE user_id IN ($1,$2)`,
			`DELETE FROM api_keys WHERE user_id IN ($1,$2)`,
			`DELETE FROM users WHERE id IN ($1,$2)`,
		} {
			_, err := db.ExecContext(context.Background(), statement, f.userID, f.otherUserID)
			require.NoError(t, err)
		}
		_, err = db.ExecContext(context.Background(), `DELETE FROM accounts WHERE id=$1`, f.accountID)
		require.NoError(t, err)
		_, err = db.ExecContext(context.Background(), `DELETE FROM groups WHERE id=$1`, f.groupID)
		require.NoError(t, err)
		_, err = db.ExecContext(context.Background(), `DELETE FROM scheduler_outbox WHERE account_id=$1 OR group_id=$2 OR payload->>'account_id'=$1::text OR payload->'account_ids' @> jsonb_build_array($1::bigint) OR payload->'group_ids' @> jsonb_build_array($2::bigint)`, f.accountID, f.groupID)
		require.NoError(t, err)
		_, err = db.ExecContext(context.Background(), `DELETE FROM model_catalog_generations WHERE scope=$1`, fmt.Sprintf("group:%d", f.groupID))
		require.NoError(t, err)
	})
	gin.SetMode(gin.TestMode)
	f.router = gin.New()
	// This replaces user login only. Public routes below always authenticate the
	// issued secret with the production API-key middleware and database reader.
	panel := f.router.Group("/api/v1", func(c *gin.Context) {
		userID := f.userID
		if c.GetHeader("X-Fixture-Other-User") == "true" {
			userID = f.otherUserID
		}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID, Concurrency: 4})
		c.Request = c.Request.WithContext(domain.WithWorkspaceScope(c.Request.Context(), domain.DefaultWorkspaceID))
		c.Next()
	})
	panel.POST("/admin/accounts", accountHandler.Create)
	panel.GET("/admin/platform-model-presets", catalogHandler.GetPlatformModelPresets)
	panel.GET("/admin/usage", adminUsageHandler.List)
	panel.PUT("/admin/accounts/:id", accountHandler.Update)
	panel.GET("/admin/accounts/:id/models/catalog", catalogHandler.GetModelCatalog)
	panel.POST("/admin/accounts/:id/models/sync-upstream", catalogHandler.SyncUpstreamModels)
	panel.PUT("/admin/accounts/:id/models/allowlist", catalogHandler.UpdateModelAllowlist)
	panel.POST("/admin/groups", groupHandler.Create)
	panel.POST("/keys", keyHandler.Create)
	panel.DELETE("/keys/:id", keyHandler.Delete)
	panel.GET("/usage", usageHandler.List)
	panel.GET("/usage/:id", usageHandler.GetByID)
	public := f.router.Group("/v1", middleware.GatewayProtocol(), middleware.ClientRequestID(), handler.InboundEndpointMiddleware(),
		gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(keyService, nil, cfg)), middleware.GroupModelAllowlist(), modelsHandler.ActionRequirement,
		middleware.RequireGroupAssignment(nil, middleware.AnthropicErrorWriter))
	public.GET("/models", modelsHandler.Models)
	public.GET("/models/catalog", modelsHandler.ModelCatalog)
	public.POST("/responses", responsesHandler.Responses)
	return f
}

func (f *coreFlowFixture) request(method, path string, body any, authenticated bool) *httptest.ResponseRecorder {
	f.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(f.t, err)
		reader = strings.NewReader(string(encoded))
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	if authenticated {
		request.Header.Set("Authorization", "Bearer "+f.secret)
	}
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, request)
	return recorder
}

func (f *coreFlowFixture) ok(method, path string, body any, authenticated bool) gjson.Result {
	f.t.Helper()
	response := f.request(method, path, body, authenticated)
	// Never print account credentials or the issued Key on failure.
	require.Equal(f.t, http.StatusOK, response.Code, "%s %s error=%s", method, path, gjson.GetBytes(response.Body.Bytes(), "message").String())
	return gjson.ParseBytes(response.Body.Bytes())
}

func (f *coreFlowFixture) savedAccount() *service.Account {
	f.t.Helper()
	account, err := f.accounts.GetByID(f.ctx, f.accountID)
	require.NoError(f.t, err)
	return account
}

func (f *coreFlowFixture) saveSelection(mapping map[string]string, enabled bool, models []string) {
	f.t.Helper()
	request := map[string]any{"enabled": enabled, "models": models, "expected_rules_revision": fmt.Sprint(f.savedAccount().ModelCatalogState().RulesRevision)}
	if mapping != nil {
		request["model_mapping"] = mapping
	}
	f.ok("PUT", fmt.Sprintf("/api/v1/admin/accounts/%d/models/allowlist", f.accountID), request, false)
}

func (f *coreFlowFixture) refresh(models []string, fail bool) error {
	f.upstream.mu.Lock()
	f.upstream.models, f.upstream.failDiscovery = models, fail
	f.upstream.mu.Unlock()
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	_, err := f.discovery.ApplyAccountModelCatalog(ctx, f.savedAccount(), nil)
	return err
}
