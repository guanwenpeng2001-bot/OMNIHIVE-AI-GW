package admin_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/routes"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

const selectionAccountsPath = "/api/v1/admin/accounts"

type selectionSchedulerCache struct {
	service.SchedulerCache
	mu       sync.Mutex
	accounts map[int64]*service.Account
}

func (c *selectionSchedulerCache) SetAccount(_ context.Context, account *service.Account) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accounts[account.ID] = account
	return nil
}

func (c *selectionSchedulerCache) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accounts[id], nil
}

type selectionTestUpstream struct {
	target *url.URL
	client *http.Client
}

func (u selectionTestUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.URL.Scheme, request.URL.Host = u.target.Scheme, u.target.Host
	request.Host = u.target.Host
	return u.client.Do(request)
}

func (u selectionTestUpstream) DoWithTLS(request *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxy, id, concurrency)
}

type selectionUnlimitedTPM struct {
	service.UsageSettlementReceiptRepository
	service.TPMReservationRepository
}

func (selectionUnlimitedTPM) ReserveTPMAttempt(context.Context, service.TPMReservationCommand) (*service.TPMReservation, error) {
	return nil, nil
}

// Explicit catalog commits in this test control the order of discovery and
// selection writes. Queue-driven discovery cannot reach an upstream.
type selectionPausedDiscovery struct {
	done <-chan struct{}
}

func (u selectionPausedDiscovery) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	select {
	case <-u.done:
		return nil, context.Canceled
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
}

func (u selectionPausedDiscovery) DoWithTLS(r *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, proxy, id, concurrency)
}

func TestAccountModelSelectionHTTPPostgres(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to an isolated PostgreSQL job_test database")
	}
	require.NoError(t, testresources.ValidatePostgres(dsn, "job_test"))
	db, client, _ := testutil.NewCapabilityPostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	workspaceID, err := domain.ResolveDefaultWorkspaceID(ctx, db)
	require.NoError(t, err)
	ctx = domain.WithWorkspaceScope(domain.WithDefaultWorkspaceID(ctx, workspaceID), workspaceID)

	independentDB, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, independentDB.Close()) })
	independentClient := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, independentDB)))
	cache := &selectionSchedulerCache{accounts: make(map[int64]*service.Account)}
	encryptor := testutil.CredentialEncryptor(t)
	repo := repository.ProvideAdminAccountRepositoryWithSecrets(client, db, cache, encryptor, nil)
	refreshRepo := repository.ProvideAccountRepositoryWithSecrets(independentClient, independentDB, nil, encryptor, nil)
	catalogWriter, ok := refreshRepo.(service.AccountModelCatalogCommitter)
	require.True(t, ok)
	cfg := &config.Config{}
	adminSvc := service.NewAdminService(
		cfg, nil, nil, repository.NewAdminGroupRepository(client, db), repo, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, client, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	discoveryDone := make(chan struct{})
	defer close(discoveryDone)
	syncer := service.NewAccountTestService(repo, nil, nil, nil, nil, selectionPausedDiscovery{done: discoveryDone}, cfg, nil)
	accountHandler := admin.NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, syncer, nil, nil, nil, nil, nil)
	credentialDrafts := service.NewAuthPendingIdentityService(client)
	accountHandler.SetCredentialDraftService(credentialDrafts)
	router := gin.New()
	testAuth := func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
		requestContext := ctx
		if c.GetHeader("X-Test-Other-Workspace") != "" {
			requestContext = domain.WithWorkspaceScope(ctx, workspaceID+1000)
		}
		c.Request = c.Request.WithContext(requestContext)
		c.Next()
	}
	pass := func(c *gin.Context) { c.Next() }
	routes.RegisterAdminRoutes(router.Group("/api/v1"), &handler.Handlers{Admin: &handler.AdminHandlers{Account: accountHandler}}, testAuth, pass, pass, nil, nil)

	type wireRequest struct {
		path, model, authorization string
	}
	wire := make(chan wireRequest, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		wire <- wireRequest{path: request.URL.Path, model: body.Model, authorization: request.Header.Get("Authorization")}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-fixture", "object": "chat.completion", "model": body.Model,
			"choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	t.Cleanup(upstream.Close)
	target, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	settlement := service.NewUsageSettlementService(selectionUnlimitedTPM{}, nil, nil)
	gateway := service.ProvideOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil,
		selectionTestUpstream{target: target, client: upstream.Client()}, nil, nil, nil, nil, nil, nil, nil, nil,
		settlement,
	)

	fixturePrefix := "model-selection-http-" + uuid.NewString()
	responsesName := fixturePrefix + "-responses"
	directName, oauthName := fixturePrefix+"-selection-direct", fixturePrefix+"-selection-oauth"
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		tx, err := db.BeginTx(cleanupCtx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		_, err = tx.ExecContext(cleanupCtx, `DELETE FROM credential_secrets WHERE account_id IN (
			SELECT id FROM accounts WHERE name IN ($1, $2, $3, $4, $5))`, fixturePrefix+"-router", fixturePrefix+"-code", responsesName, directName, oauthName)
		require.NoError(t, err)
		_, err = tx.ExecContext(cleanupCtx, `WITH removed AS (
			DELETE FROM accounts WHERE name IN ($1, $2, $3, $4, $5) RETURNING id
		) DELETE FROM scheduler_outbox WHERE account_id IN (SELECT id FROM removed)
			OR payload->>'account_id' IN (SELECT id::text FROM removed)`, fixturePrefix+"-router", fixturePrefix+"-code", responsesName, directName, oauthName)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	})
	type fixture struct {
		name, platform, baseURL, native string
		id                              int64
		revision, outbox                int64
	}
	fixtures := []fixture{
		{name: fixturePrefix + "-router", platform: service.PlatformOpenRouter, baseURL: "https://openrouter.ai/api/v1", native: "vendor/fixture-chat"},
		{name: fixturePrefix + "-code", platform: service.PlatformOpenCodeGo, baseURL: "https://opencode.ai/zen/go/v1", native: "fixture-chat"},
	}
	token := "model-selection-fixture-auth"
	request := func(t *testing.T, method, path string, body any, status int) json.RawMessage {
		t.Helper()
		return requestAccountSelectionHTTP(t, router, method, path, body, status, false)
	}
	getCatalog := func(t *testing.T, id int64) accountSelectionCatalogReply {
		t.Helper()
		return decodeSelectionHTTP[accountSelectionCatalogReply](t, request(t, http.MethodGet, selectionAccountPath(id)+"/models/catalog", nil, 200))
	}

	for i := range fixtures {
		f := &fixtures[i]
		if !t.Run(f.platform, func(t *testing.T) {
			created := request(t, http.MethodPost, selectionAccountsPath, map[string]any{
				"name": f.name, "platform": f.platform, "type": "apikey", "concurrency": 1,
				"credentials": map[string]any{"api_key": token, "base_url": f.baseURL, "api_protocol": "chat_completions"},
			}, 200)
			f.id = decodeSelectionHTTP[struct {
				ID int64 `json:"id"`
			}](t, created).ID
			require.Positive(t, f.id)
			require.NotContains(t, string(created), token)
			initial := getCatalog(t, f.id)
			require.Equal(t, service.ModelSelectionInherit, initial.Mode)
			require.Equal(t, map[string]string{}, initial.ModelMapping)
			f.revision = initial.RulesRevision
			f.outbox = selectionOutboxCount(t, ctx, db, f.id)
			path := selectionAccountPath(f.id)
			saved := decodeSelectionHTTP[service.ModelSelectionResult](t, request(t, http.MethodPut, path+"/models/allowlist", map[string]any{
				"enabled": true, "models": []string{f.native}, "expected_rules_revision": strconv.FormatInt(f.revision, 10),
			}, 200))
			f.revision++
			require.Equal(t, service.ModelSelectionRestricted, saved.Mode)
			require.Equal(t, []string{f.native}, saved.Models)
			require.Equal(t, map[string]string{}, saved.ModelMapping)
			require.Equal(t, []string{}, saved.AutoAdded)
			require.Equal(t, []string{f.native}, saved.NotDiscovered)
			require.Equal(t, f.revision, saved.RulesRevision)
			require.EqualValues(t, 7, saved.Scope.ConfirmedBy)
			assertSelectionDatabase(t, ctx, db, f.id, f.revision, true, []string{f.native}, nil)
			require.Greater(t, selectionOutboxCount(t, ctx, db, f.id), f.outbox)
			require.Eventually(t, func() bool {
				account, _ := cache.GetAccount(ctx, f.id)
				return account != nil && service.AccountModelSelectionView(account).RulesRevision == f.revision
			}, 2*time.Second, 10*time.Millisecond)

			// DB evidence records the independently observed upstream endpoint.
			fresh, err := refreshRepo.GetByID(ctx, f.id)
			require.NoError(t, err)
			baseline := fresh.ModelCatalogBaseline()
			state := fresh.ModelCatalogState()
			state.Result = "success"
			snapshot := selectionEvidence(baseline, f.native)
			require.NoError(t, catalogWriter.CommitAccountModelCatalog(ctx, f.id, baseline, snapshot, state, nil, false))

			catalog := getCatalog(t, f.id)
			require.Equal(t, []string{f.native}, catalog.Models)
			require.Equal(t, catalog.RulesRevision, catalog.State.RulesRevision)
			require.Equal(t, f.revision, catalog.RulesRevision)
			require.Len(t, catalog.EligibleModels, 1)
			require.Equal(t, f.native, catalog.EligibleModels[0].ID)
			detail := request(t, http.MethodGet, path, nil, 200)
			require.Contains(t, string(detail), service.UpstreamModelAllowlistExtraKey)
			require.NotContains(t, string(detail), token)
			require.NotContains(t, string(detail), "model_mapping")

			fresh, err = repo.GetByID(ctx, f.id)
			require.NoError(t, err)
			binding := service.ResolveAccountModelBinding(ctx, fresh, f.native, service.AccountModelBindingOptions{Endpoint: service.CompositeRouteEndpointChatCompletions})
			require.True(t, binding.Supported, "%+v", binding)
			require.Equal(t, f.native, binding.UpstreamModel)
			recorder, err := forwardSelectionChat(ctx, gateway, fresh, f.native)
			require.NoError(t, err, recorder.Body.String())
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			select {
			case received := <-wire:
				require.Equal(t, f.native, received.model)
				require.Equal(t, "Bearer "+token, received.authorization)
				require.Contains(t, received.path, "/chat/completions")
			default:
				t.Fatal("mock provider did not receive the gateway request")
			}
		}) {
			return
		}
	}
	f := &fixtures[0]
	path := selectionAccountPath(f.id)
	beforeAlias, err := refreshRepo.GetByID(ctx, f.id)
	require.NoError(t, err)
	aliasTarget, replacementTarget, bulkTarget := "vendor/alias-target", "vendor/replacement", "vendor/bulk-target"
	models := []string{f.native}
	for _, edit := range []struct{ alias, target string }{{"first-alias", aliasTarget}, {"next-alias", replacementTarget}} {
		priorOutbox := selectionOutboxCount(t, ctx, db, f.id)
		updated := decodeSelectionHTTP[accountSelectionEditReply](t, request(t, http.MethodPut, path, map[string]any{
			"credentials": map[string]any{"model_mapping": map[string]string{edit.alias: edit.target}},
		}, 200))
		f.revision++
		models = append(models, edit.target)
		slices.Sort(models)
		require.Equal(t, f.revision, updated.RulesRevision)
		require.Equal(t, []string{edit.target}, updated.AutoAdded)
		wantMap := map[string]string{edit.alias: edit.target}
		assertSelectionDatabase(t, ctx, db, f.id, f.revision, true, models, wantMap)
		require.Greater(t, selectionOutboxCount(t, ctx, db, f.id), priorOutbox)
		fresh, err := repo.GetByID(ctx, f.id)
		require.NoError(t, err)
		require.Equal(t, token, fresh.GetCredential("api_key"))
		require.Equal(t, f.baseURL, fresh.GetCredential("base_url"))
		require.Equal(t, "chat_completions", fresh.GetCredential("api_protocol"))
	}
	// This refresh started on another pool before the alias changes.
	staleBaseline := beforeAlias.ModelCatalogBaseline()
	err = catalogWriter.CommitAccountModelCatalog(ctx, f.id, staleBaseline, selectionEvidence(staleBaseline, "vendor/stale-refresh"), beforeAlias.ModelCatalogState(), nil, false)
	require.ErrorIs(t, err, service.ErrAccountModelCatalogConflict)
	require.Equal(t, models, getCatalog(t, f.id).Models)

	for i := range fixtures {
		fixtures[i].outbox = selectionOutboxCount(t, ctx, db, fixtures[i].id)
	}
	bulkMap := map[string]string{"bulk-alias": bulkTarget}
	type bulkSelectionReply struct {
		Success int `json:"success"`
		Failed  int `json:"failed"`
		Results []struct {
			AccountID int64 `json:"account_id"`
			Success   bool  `json:"success"`
			accountSelectionEditReply
		} `json:"results"`
	}
	bulk := decodeSelectionHTTP[bulkSelectionReply](t, request(t, http.MethodPost, selectionAccountsPath+"/bulk-update", map[string]any{
		"account_ids": []int64{fixtures[1].id, fixtures[0].id},
		"credentials": map[string]any{"model_mapping": bulkMap},
	}, 200))
	require.Equal(t, 2, bulk.Success)
	require.Zero(t, bulk.Failed)
	require.Len(t, bulk.Results, 2)
	expectedVersions := map[int64]int64{fixtures[0].id: fixtures[0].revision + 1, fixtures[1].id: fixtures[1].revision + 1}
	for _, row := range bulk.Results {
		require.True(t, row.Success)
		require.Equal(t, expectedVersions[row.AccountID], row.RulesRevision)
		require.Equal(t, []string{bulkTarget}, row.AutoAdded)
	}
	models = []string{aliasTarget, bulkTarget, f.native, replacementTarget}
	for i := range fixtures {
		account := &fixtures[i]
		account.revision++
		expectedModels := []string{account.native, bulkTarget}
		if i == 0 {
			expectedModels = models
		}
		assertSelectionDatabase(t, ctx, db, account.id, account.revision, true, expectedModels, bulkMap)
		require.Greater(t, selectionOutboxCount(t, ctx, db, account.id), account.outbox)
		fresh, err := repo.GetByID(ctx, account.id)
		require.NoError(t, err)
		require.Equal(t, token, fresh.GetCredential("api_key"))
		require.Equal(t, account.baseURL, fresh.GetCredential("base_url"))
	}

	allowlist := path + "/models/allowlist"
	deny := map[string]any{"enabled": true, "models": []string{}, "reason": "maintenance", "expected_rules_revision": f.revision}
	outbox := selectionOutboxCount(t, ctx, db, f.id)
	assertSelectionHTTPReason(t, request(t, http.MethodPut, allowlist, deny, 400), "MODEL_SELECTION_CONFIRMATION_REQUIRED")
	deny["confirm_deny_all"], deny["reason"] = true, " "
	assertSelectionHTTPReason(t, request(t, http.MethodPut, allowlist, deny, 400), "MODEL_SELECTION_REASON_REQUIRED")
	assertSelectionDatabase(t, ctx, db, f.id, f.revision, true, models, bulkMap)
	require.Equal(t, outbox, selectionOutboxCount(t, ctx, db, f.id))

	deny["reason"] = "maintenance"
	requestAccountSelectionHTTP(t, router, http.MethodPut, allowlist, deny, 404, true)
	denied := decodeSelectionHTTP[service.ModelSelectionResult](t, request(t, http.MethodPut, allowlist, deny, 200))
	f.revision++
	require.Equal(t, service.ModelSelectionDenyAll, denied.Mode)
	require.Equal(t, f.revision, denied.RulesRevision)
	require.Equal(t, []string{}, denied.Models)
	require.Equal(t, bulkMap, denied.ModelMapping)
	require.Equal(t, "maintenance", denied.Scope.Reason)
	assertSelectionDatabase(t, ctx, db, f.id, f.revision, true, []string{}, bulkMap)
	catalog := getCatalog(t, f.id)
	require.Equal(t, service.ModelSelectionDenyAll, catalog.Mode)
	require.NotEmpty(t, catalog.Snapshot.DiscoveredModels)
	require.Empty(t, catalog.EligibleModels)
	require.JSONEq(t, `[]`, string(request(t, http.MethodGet, path+"/models", nil, 200)))
	require.JSONEq(t, `[]`, string(request(t, http.MethodGet, path+"/models?view=supported", nil, 200)))
	fresh, err := repo.GetByID(ctx, f.id)
	require.NoError(t, err)
	require.False(t, fresh.IsModelSupported("bulk-alias"))
	require.False(t, service.AccountSupportsResolvedModel(ctx, fresh, "bulk-alias", service.AccountModelBindingOptions{Endpoint: service.CompositeRouteEndpointChatCompletions}))
	_, err = forwardSelectionChat(ctx, gateway, fresh, "bulk-alias")
	var rejected *service.UpstreamFailoverError
	require.ErrorAs(t, err, &rejected)
	require.Equal(t, service.ModelBindingRejectedReason, rejected.Reason)
	select {
	case received := <-wire:
		t.Fatalf("denied model %q reached %s", received.model, received.path)
	default:
	}

	closedOutbox := selectionOutboxCount(t, ctx, db, f.id)
	assertSelectionHTTPReason(t, request(t, http.MethodPut, path, map[string]any{
		"credentials": map[string]any{"model_mapping": map[string]string{"forbidden-alias": "vendor/forbidden"}},
	}, 409), "MODEL_SELECTION_DENIED")
	reopen := map[string]any{"mode": "restricted", "models": []string{f.native}, "expected_rules_revision": f.revision - 1}
	assertSelectionHTTPReason(t, request(t, http.MethodPut, allowlist, reopen, 409), "MODEL_SELECTION_CONFLICT")
	assertSelectionDatabase(t, ctx, db, f.id, f.revision, true, []string{}, bulkMap)
	require.Equal(t, closedOutbox, selectionOutboxCount(t, ctx, db, f.id))

	reopen["expected_rules_revision"] = f.revision
	reopened := decodeSelectionHTTP[service.ModelSelectionResult](t, request(t, http.MethodPut, allowlist, reopen, 200))
	require.Equal(t, service.ModelSelectionRestricted, reopened.Mode)
	require.Equal(t, []string{bulkTarget, f.native}, reopened.Models)
	require.Equal(t, []string{bulkTarget}, reopened.AutoAdded)
	require.Equal(t, f.revision+1, reopened.RulesRevision)
	assertSelectionDatabase(t, ctx, db, f.id, reopened.RulesRevision, true, reopened.Models, bulkMap)

	t.Run("explicit bulk model selection", func(t *testing.T) {
		f.revision = reopened.RulesRevision
		bulkPath := selectionAccountsPath + "/bulk-update"
		ids := []int64{fixtures[0].id, fixtures[1].id}
		apply := func(selection, credentials map[string]any, enabled bool, models, autoAdded []string, mapping map[string]string) {
			t.Helper()
			payload := map[string]any{"account_ids": ids, "model_selection": selection}
			if credentials != nil {
				payload["credentials"] = credentials
			}
			result := decodeSelectionHTTP[bulkSelectionReply](t, request(t, http.MethodPost, bulkPath, payload, 200))
			require.Equal(t, 2, result.Success)
			require.Zero(t, result.Failed)
			require.Len(t, result.Results, 2)
			for i, row := range result.Results {
				account := &fixtures[i]
				account.revision++
				require.Equal(t, account.id, row.AccountID)
				require.True(t, row.Success)
				require.Equal(t, autoAdded, row.AutoAdded)
				require.Equal(t, account.revision, row.RulesRevision)
				assertSelectionDatabase(t, ctx, db, account.id, account.revision, enabled, models, mapping)
			}
		}
		inherit := map[string]any{"mode": "inherit", "models": []string{}}
		apply(inherit, nil, false, []string{}, []string{}, bulkMap)
		deny := map[string]any{"mode": "deny_all", "models": []string{}, "reason": "batch maintenance"}
		assertSelectionHTTPReason(t, request(t, http.MethodPost, bulkPath, map[string]any{"account_ids": ids, "model_selection": deny}, 400), "MODEL_SELECTION_CONFIRMATION_REQUIRED")
		for _, account := range fixtures {
			assertSelectionDatabase(t, ctx, db, account.id, account.revision, false, []string{}, bulkMap)
		}
		deny["confirm_deny_all"] = true
		apply(deny, nil, true, []string{}, []string{}, bulkMap)
		restricted := map[string]any{"mode": "restricted", "models": []string{"vendor/requested"}}
		apply(restricted, nil, true, []string{bulkTarget, "vendor/requested"}, []string{bulkTarget}, bulkMap)
		clearMapping := map[string]any{"model_mapping": map[string]string{}}
		apply(inherit, clearMapping, false, []string{}, []string{}, map[string]string{})
	})

	t.Run("create and reauthorize with an alias-free scope", func(t *testing.T) {
		type writtenAccount struct{ ID int64 }
		restrictedA := map[string]any{"mode": "restricted", "models": []string{"vendor/model-a"}}
		restrictedB := map[string]any{"mode": "restricted", "models": []string{"vendor/model-b"}}
		direct := decodeSelectionHTTP[writtenAccount](t, request(t, http.MethodPost, selectionAccountsPath, map[string]any{
			"name": directName, "platform": service.PlatformOpenRouter, "type": "apikey", "concurrency": 1,
			"credentials": map[string]any{"api_key": token, "base_url": fixtures[0].baseURL}, "model_selection": restrictedA,
		}, 200))
		assertSelectionDatabase(t, ctx, db, direct.ID, 0, true, []string{"vendor/model-a"}, nil)

		browser := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
		binding := fmt.Sprintf("%x", sha256.Sum256([]byte(domain.WorkspaceActorScope(ctx, "admin:7")+":"+browser)))
		cookie := &http.Cookie{Name: "account_authorization_browser", Value: browser}
		t.Cleanup(func() {
			_, err := db.ExecContext(context.Background(), "DELETE FROM pending_auth_sessions WHERE browser_session_key=$1", binding)
			require.NoError(t, err)
		})
		post := func(t *testing.T, path string, payload any, status int) json.RawMessage {
			t.Helper()
			return requestAccountSelectionHTTP(t, router, http.MethodPost, path, payload, status, false, cookie)
		}
		review := func(t *testing.T, name, token string, scope, options map[string]any) map[string]any {
			t.Helper()
			draft, err := credentialDrafts.IssueAccountCredentialDraft(ctx, binding, service.PlatformAnthropic, service.AccountTypeOAuth, map[string]any{
				"access_token": token, "model_mapping": map[string]string{"draft-copy": "stale"},
			})
			require.NoError(t, err)
			payload := map[string]any{"name": name, "platform": service.PlatformAnthropic, "type": service.AccountTypeOAuth, "credential_draft": draft.Reference,
				"credential_draft_revision": draft.Revision, "credentials": options, "model_selection": scope}
			reviewed := decodeSelectionHTTP[service.AccountCredentialDraft](t, post(t, selectionAccountsPath+"/credential-drafts/"+draft.Reference+"/review", payload, 200))
			payload["credential_draft_revision"] = reviewed.Revision
			return payload
		}
		creation := review(t, oauthName, "draft-token", restrictedA, map[string]any{})
		creation["model_selection"] = restrictedB
		assertSelectionHTTPReason(t, post(t, selectionAccountsPath, creation, 409), "ACCOUNT_CREDENTIAL_DRAFT_CONFLICT")
		creation["model_selection"] = restrictedA
		oauth := decodeSelectionHTTP[writtenAccount](t, post(t, selectionAccountsPath, creation, 200))
		assertSelectionDatabase(t, ctx, db, oauth.ID, 0, true, []string{"vendor/model-a"}, nil)

		updated := decodeSelectionHTTP[accountSelectionEditReply](t, request(t, http.MethodPut, selectionAccountPath(oauth.ID), map[string]any{
			"credentials": map[string]any{"model_mapping": map[string]string{"custom": "alias-target"}},
		}, 200))
		options := map[string]any{"model_mapping": map[string]string{}}
		reauth := review(t, "account:"+strconv.FormatInt(oauth.ID, 10), "new-draft-token", restrictedB, options)
		post(t, selectionAccountPath(oauth.ID)+"/apply-oauth-credentials", reauth, 200)
		assertSelectionDatabase(t, ctx, db, oauth.ID, updated.RulesRevision+1, true, []string{"vendor/model-b"}, map[string]string{})
		fresh, err := repo.GetByID(ctx, oauth.ID)
		require.NoError(t, err)
		require.Equal(t, "new-draft-token", fresh.GetCredential("access_token"))
	})

	t.Run("responses passthrough and compact eligibility", func(t *testing.T) {
		const ordinary, compact = "gpt-4.1-mini", "compact-private"
		mapping := map[string]string{ordinary: ordinary}
		created := request(t, http.MethodPost, selectionAccountsPath, map[string]any{
			"name": responsesName, "platform": service.PlatformOpenAI, "type": "apikey", "concurrency": 1,
			"credentials": map[string]any{
				"api_key": token, "model_mapping": mapping, "compact_model_mapping": map[string]string{ordinary: compact},
			},
			"extra": map[string]any{
				"openai_passthrough": true, "openai_responses_supported": true,
				"openai_apikey_responses_websockets_v2_mode": service.OpenAIWSIngressModePassthrough,
			},
		}, 200)
		id := decodeSelectionHTTP[struct {
			ID int64 `json:"id"`
		}](t, created).ID
		catalog := getCatalog(t, id)
		allowlist := selectionAccountPath(id) + "/models/allowlist"
		restricted := decodeSelectionHTTP[service.ModelSelectionResult](t, request(t, http.MethodPut, allowlist, map[string]any{
			"mode": "restricted", "models": []string{ordinary, compact}, "expected_rules_revision": catalog.RulesRevision,
		}, 200))
		account, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		require.True(t, account.IsOpenAIPassthroughEnabled())
		require.Equal(t, service.OpenAIWSIngressModePassthrough, account.ResolveOpenAIResponsesWebSocketV2Mode(service.OpenAIWSIngressModeCtxPool))
		wsCtx := service.WithOpenAIWebSocketSelection(ctx, service.OpenAIWSIngressModeCtxPool)
		responseEndpoint := service.CompositeRouteEndpointResponses
		bindings := []struct {
			name, model string
			ctx         context.Context
			options     service.AccountModelBindingOptions
		}{
			{"HTTP passthrough", ordinary, ctx, service.AccountModelBindingOptions{Endpoint: responseEndpoint, FinalModel: ordinary}},
			{"WS passthrough", ordinary, wsCtx, service.AccountModelBindingOptions{Endpoint: responseEndpoint}},
			{"compact", compact, ctx, service.AccountModelBindingOptions{Endpoint: responseEndpoint, Compact: true}},
		}
		for _, test := range bindings {
			binding := service.ResolveAccountModelBinding(test.ctx, account, ordinary, test.options)
			require.True(t, binding.Supported, test.name)
			require.Equal(t, test.model, binding.UpstreamModel, test.name)
		}

		denied := decodeSelectionHTTP[service.ModelSelectionResult](t, request(t, http.MethodPut, allowlist, map[string]any{
			"mode": "deny_all", "models": []string{}, "reason": "maintenance", "expected_rules_revision": restricted.RulesRevision,
		}, 200))
		require.Equal(t, restricted.RulesRevision+1, denied.RulesRevision)
		assertSelectionDatabase(t, ctx, db, id, denied.RulesRevision, true, []string{}, mapping)
		account, err = repo.GetByID(ctx, id)
		require.NoError(t, err)
		for _, test := range bindings {
			require.False(t, service.AccountSupportsResolvedModel(test.ctx, account, ordinary, test.options), test.name)
		}
	})
}

type accountSelectionCatalogReply struct {
	Mode           service.ModelSelectionMode    `json:"mode"`
	Models         []string                      `json:"models"`
	ModelMapping   map[string]string             `json:"model_mapping"`
	RulesRevision  int64                         `json:"rules_revision,string"`
	EligibleModels []service.AccountCatalogModel `json:"eligible_models"`
	Snapshot       struct {
		DiscoveredModels []string `json:"discovered_models"`
	} `json:"snapshot"`
	State struct {
		RulesRevision int64 `json:"rules_revision,string"`
	} `json:"state"`
}

type accountSelectionEditReply struct {
	AutoAdded     []string `json:"auto_added"`
	RulesRevision int64    `json:"rules_revision,string"`
}

func selectionAccountPath(id int64) string {
	return selectionAccountsPath + "/" + strconv.FormatInt(id, 10)
}

func forwardSelectionChat(ctx context.Context, gateway *service.OpenAIGatewayService, account *service.Account, model string) (*httptest.ResponseRecorder, error) {
	ctx, plan := gateway.CaptureRequestPlan(ctx, service.GatewayRequestSpec{Protocol: "chat_completions", Model: model, WorkspaceID: account.WorkspaceID})
	defer plan.Finalize(false)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	body := fmt.Appendf(nil, `{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, model)
	_, err := gateway.ForwardAsChatCompletions(ctx, ginCtx, account, body, "", "")
	return recorder, err
}

func requestAccountSelectionHTTP(t *testing.T, router http.Handler, method, path string, body any, status int, otherWorkspace bool, cookies ...*http.Cookie) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", uuid.NewString())
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	if otherWorkspace {
		request.Header.Set("X-Test-Other-Workspace", "1")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, status, recorder.Code, recorder.Body.String())
	if status != http.StatusOK {
		return json.RawMessage(recorder.Body.Bytes())
	}
	reply := decodeSelectionHTTP[struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}](t, recorder.Body.Bytes())
	require.Zero(t, reply.Code)
	return reply.Data
}

func decodeSelectionHTTP[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var result T
	require.NoError(t, json.Unmarshal(raw, &result), string(raw))
	return result
}

func assertSelectionHTTPReason(t *testing.T, raw json.RawMessage, reason string) {
	t.Helper()
	reply := decodeSelectionHTTP[struct {
		Reason string `json:"reason"`
	}](t, raw)
	require.Equal(t, reason, reply.Reason)
}

func assertSelectionDatabase(t *testing.T, ctx context.Context, db *sql.DB, id, revision int64, enabled bool, models []string, mapping map[string]string) {
	t.Helper()
	var rawScope, rawMapping []byte
	var storedRevision int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT extra->'upstream_model_allowlist', credentials->'model_mapping',
		COALESCE((extra->'upstream_model_catalog_state'->>'rules_revision')::bigint, 0)
		FROM accounts WHERE id = $1`, id).Scan(&rawScope, &rawMapping, &storedRevision))
	scope := decodeSelectionHTTP[service.AccountModelAllowlist](t, rawScope)
	require.Equal(t, enabled, scope.Enabled)
	require.Equal(t, models, scope.Models)
	require.Equal(t, revision, storedRevision)
	if mapping == nil {
		require.Empty(t, rawMapping, "scope-only writes must omit the mapping key")
		return
	}
	require.Equal(t, mapping, decodeSelectionHTTP[map[string]string](t, rawMapping))
}

func selectionOutboxCount(t *testing.T, ctx context.Context, db *sql.DB, id int64) int64 {
	t.Helper()
	var count int64
	// A pending account event can be deduplicated until a scheduler consumes
	// it. The per-account catalog generation events still track each mutation.
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox
		WHERE (account_id = $1 AND event_type = $2)
		   OR (event_type = $3 AND payload->>'account_id' = $1::text)`,
		id, service.SchedulerOutboxEventAccountChanged, service.SchedulerOutboxEventModelCatalogChanged).Scan(&count))
	return count
}

func selectionEvidence(baseline service.AccountModelCatalogBaseline, model string) *service.AccountModelCatalogSnapshot {
	observed := time.Now().UTC().Format(time.RFC3339Nano)
	return &service.AccountModelCatalogSnapshot{
		Revision: baseline.SnapshotRevision + 1, RulesRevision: baseline.RulesRevision, IdentityRevision: baseline.IdentityRevision,
		Complete: true, SourceKind: "native_account_catalog", DiscoveryScope: "account", ObservedAt: observed,
		DiscoveredModels: []string{model}, EligibleModels: []service.AccountCatalogModel{
			{ID: model, Endpoints: []string{service.CompositeRouteEndpointChatCompletions}, EvidenceKind: "native_account_catalog", EvidenceObservedAt: observed},
		},
	}
}
