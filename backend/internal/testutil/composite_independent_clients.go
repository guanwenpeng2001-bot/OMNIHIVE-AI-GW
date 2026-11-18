//go:build unit

package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testprocess"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testresources"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type compositeProcessRequest struct {
	Operation  string
	GroupID    int64
	AccountID  int64
	ExcludedID int64
	Namespace  string
}

type compositeProcessResponse struct {
	PID        int
	BackendPID int
	Database   string
	Generation int64
	Identity   int64
	AccountID  int64
	Error      string
}

func RunCompositeGroupIndependentClients(test *testing.T) {
	ctx := context.Background()
	if os.Getenv("UPSTREAM_JOB_TEST_DSN") == "" || os.Getenv("AFFINITY_REDIS_TEST_ADDR") == "" {
		test.Skip("requires exclusive job_test and Redis 6380")
	}
	database, err := testresources.OpenPostgres(ctx, os.Getenv("UPSTREAM_JOB_TEST_DSN"), "job_test")
	require.NoError(test, err)
	database.SetMaxOpenConns(1)
	test.Cleanup(func() { require.NoError(test, database.Close()) })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	accounts := repository.NewAccountRepository(client, database, nil)
	committer, supportsCommit := accounts.(service.AccountModelCatalogCommitter)
	require.True(test, supportsCommit)
	redisAddress := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	require.NoError(test, testresources.ValidateRedis(redisAddress))
	if role := testprocess.Role(); role != "" {
		generation := int64(7)
		if role == "composite-new-release" {
			generation = 8
		}
		var gateway *service.GatewayService
		var cache service.GatewayCache
		var pinned *service.Account
		var groupID int64
		var observed context.Context
		const session = "v2:binding:composite-independent-clients"
		testprocess.Serve(test, func(payload json.RawMessage) any {
			var request compositeProcessRequest
			require.NoError(test, json.Unmarshal(payload, &request))
			response := compositeProcessResponse{PID: os.Getpid(), Generation: generation}
			require.NoError(test, database.QueryRowContext(ctx, `SELECT pg_backend_pid(), current_database()`).Scan(&response.BackendPID, &response.Database))
			switch request.Operation {
			case "init":
				groupID = request.GroupID
				redisClient := redis.NewClient(&redis.Options{Addr: redisAddress})
				redisClient.AddHook(testresources.RedisNamespaceHook(request.Namespace))
				test.Cleanup(func() { require.NoError(test, redisClient.Close()) })
				require.NoError(test, redisClient.Ping(ctx).Err())
				cache = repository.NewGatewayCache(redisClient)
				resolver := service.NewCompositeRouteResolver(repository.NewCompositeModelRouteRepository(client))
				gateway = service.NewGatewayService(accounts, repository.NewGroupRepository(client, database), nil, nil, nil, nil, nil, cache, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, resolver, nil, nil)
				pinned, err = accounts.GetByID(ctx, request.AccountID)
				require.NoError(test, err)
			case "select":
				observed = service.WithAffinityBindingState(ctx)
				observed = service.WithGatewayRequestPlan(observed, service.NewGatewayRequestPlan(service.GatewayRequestSpec{Route: "/v1/chat/completions", Protocol: "openai", Model: "composite-fixture", BillingGroupID: groupID}))
				excluded := map[int64]struct{}{}
				if request.ExcludedID != 0 {
					excluded[request.ExcludedID] = struct{}{}
				}
				selected, selectErr := gateway.SelectAccountForModelWithExclusions(observed, &groupID, session, "composite-fixture", excluded)
				if selectErr != nil {
					response.Error = selectErr.Error()
				} else {
					response.AccountID = selected.ID
				}
			case "bind":
				require.NotNil(test, observed)
				require.NoError(test, cache.SetSessionAccountID(service.WithAffinityAdmission(observed), groupID, session, request.AccountID, time.Minute))
			case "failed_binding":
				require.NotNil(test, observed)
				require.NoError(test, cache.DeleteSessionAccountID(observed, groupID, session))
			case "binding":
				response.AccountID, err = cache.GetSessionAccountID(ctx, groupID, session)
				require.NoError(test, err)
			case "binding_absent":
				response.AccountID, err = cache.GetSessionAccountID(ctx, groupID, session)
				require.ErrorIs(test, err, service.ErrStickySessionNotFound)
			case "publish":
				current, readErr := accounts.GetByID(ctx, pinned.ID)
				require.NoError(test, readErr)
				candidate := compositeCatalogSnapshot(current, generation)
				// Process identity is fixed at startup; a fresh CAS cannot authorize a rotated identity.
				candidate.IdentityRevision = pinned.ModelCatalogBaseline().IdentityRevision
				commitErr := committer.CommitAccountModelCatalog(ctx, current.ID, current.ModelCatalogBaseline(), candidate, current.ModelCatalogState(), &service.UpstreamModelMetadataSnapshot{}, false)
				if errors.Is(commitErr, service.ErrAccountModelCatalogConflict) {
					response.Error = "conflict"
				} else {
					require.NoError(test, commitErr)
				}
			default:
				test.Fatalf("unknown operation %q", request.Operation)
			}
			response.Identity = pinned.ModelCatalogBaseline().IdentityRevision
			return response
		})
		return
	}
	var otherConnections int
	require.NoError(test, database.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()`).Scan(&otherConnections))
	require.Zero(test, otherConnections, "exclusive job_test is required")
	require.NoError(test, repository.ApplyMigrations(ctx, database))
	namespace := "test:isolated-resource-adaptation:" + uuid.NewString() + ":"
	_ = testresources.RedisClientWithNamespace(test, &redis.Options{Addr: redisAddress}, namespace)
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddress})
	test.Cleanup(func() { require.NoError(test, redisClient.Close()) })
	info, err := redisClient.Info(ctx, "server").Result()
	require.NoError(test, err)
	test.Logf("real storage PostgreSQL database=job_test (validated UPSTREAM_JOB_TEST_DSN); Redis=%s namespace=%s server=%s", redisAddress, namespace, info)
	group, err := client.Group.Create().SetName("composite-independent-" + uuid.NewString()).SetPlatform(service.PlatformComposite).SetStatus(service.StatusActive).Save(ctx)
	require.NoError(test, err)
	test.Cleanup(func() { require.NoError(test, client.Group.DeleteOneID(group.ID).Exec(ctx)) })
	routes := repository.NewCompositeModelRouteRepository(client)
	route := &service.CompositeModelRoute{GroupID: group.ID, PublicModel: "composite-fixture", MatchType: service.CompositeRouteMatchExact, TargetPlatform: service.PlatformOpenAI, UpstreamModel: "fixture-target", Endpoint: service.CompositeRouteEndpointAny, Enabled: true}
	require.NoError(test, routes.Create(ctx, route))
	test.Cleanup(func() { require.NoError(test, routes.Delete(ctx, route.ID)) })
	var accountIDs []int64
	for _, priority := range []int{0, 10} {
		row, err := client.Account.Create().SetName(fmt.Sprintf("composite-independent-%d", priority)).SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeAPIKey).SetStatus(service.StatusActive).SetSchedulable(true).SetPriority(priority).SetCredentials(map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"fixture-target": "fixture-target"}}).Save(ctx)
		require.NoError(test, err)
		accountIDs = append(accountIDs, row.ID)
		test.Cleanup(func() {
			_, err := database.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE account_id=$1`, row.ID)
			require.NoError(test, err)
			require.NoError(test, client.Account.DeleteOneID(row.ID).Exec(ctx))
		})
		_, err = client.AccountGroup.Create().SetAccountID(row.ID).SetGroupID(group.ID).Save(ctx)
		require.NoError(test, err)
		test.Cleanup(func() {
			_, err := database.ExecContext(ctx, `DELETE FROM account_groups WHERE account_id=$1 AND group_id=$2`, row.ID, group.ID)
			require.NoError(test, err)
		})
		account, err := accounts.GetByID(ctx, row.ID)
		require.NoError(test, err)
		require.NoError(test, committer.CommitAccountModelCatalog(ctx, row.ID, account.ModelCatalogBaseline(), compositeCatalogSnapshot(account, 7), account.ModelCatalogState(), &service.UpstreamModelMetadataSnapshot{}, false))
	}
	first, second := testprocess.Start(test, "composite-old-release"), testprocess.Start(test, "composite-new-release")
	exchange := func(participant *testprocess.Client, request compositeProcessRequest) compositeProcessResponse {
		participant.Send(request)
		var response compositeProcessResponse
		participant.Receive(&response)
		test.Logf("operation=%s pid=%d pg_backend_pid=%d database=%s fixed_generation=%d fixed_identity=%d selected=%d error=%s", request.Operation, response.PID, response.BackendPID, response.Database, response.Generation, response.Identity, response.AccountID, response.Error)
		return response
	}
	initial := compositeProcessRequest{Operation: "init", GroupID: group.ID, AccountID: accountIDs[0], Namespace: namespace}
	older, newer := exchange(first, initial), exchange(second, initial)
	require.NotEqual(test, older.PID, newer.PID)
	require.NotEqual(test, older.BackendPID, newer.BackendPID)
	require.NotEqual(test, os.Getpid(), older.PID)
	require.NotEqual(test, os.Getpid(), newer.PID)
	require.Equal(test, int64(7), older.Generation)
	require.Equal(test, int64(8), newer.Generation)
	selectAccount := func(participant *testprocess.Client, excluded, expected int64) {
		result := exchange(participant, compositeProcessRequest{Operation: "select", ExcludedID: excluded})
		require.Empty(test, result.Error)
		require.Equal(test, expected, result.AccountID)
	}
	test.Run("selection_and_cross_process_affinity", func(test *testing.T) {
		selectAccount(first, 0, accountIDs[0])
		exchange(first, compositeProcessRequest{Operation: "bind", AccountID: accountIDs[0]})
		require.Equal(test, accountIDs[0], exchange(second, compositeProcessRequest{Operation: "binding"}).AccountID)
		selectAccount(second, 0, accountIDs[0])
	})
	test.Run("failed_account_fallback_and_stale_failure", func(test *testing.T) {
		selectAccount(second, accountIDs[0], accountIDs[1])
		exchange(second, compositeProcessRequest{Operation: "failed_binding"})
		exchange(second, compositeProcessRequest{Operation: "bind", AccountID: accountIDs[1]})
		exchange(first, compositeProcessRequest{Operation: "failed_binding"})
		require.Equal(test, accountIDs[1], exchange(first, compositeProcessRequest{Operation: "binding"}).AccountID)
		selectAccount(first, 0, accountIDs[1])
	})
	test.Run("fixed_release_cannot_overwrite_new_publication", func(test *testing.T) {
		require.Empty(test, exchange(second, compositeProcessRequest{Operation: "publish"}).Error)
		require.Equal(test, "conflict", exchange(first, compositeProcessRequest{Operation: "publish"}).Error)
		current, err := accounts.GetByID(ctx, accountIDs[0])
		require.NoError(test, err)
		require.Equal(test, int64(8), current.GetUpstreamModelCatalog().SourceGeneration)
	})
	test.Run("identity_revocation_invalidates_sticky_and_fixed_clients", func(test *testing.T) {
		// A subtest-only run has no earlier selection observation. Observe the
		// current binding before deleting it, just as a real request would.
		require.Empty(test, exchange(first, compositeProcessRequest{Operation: "select"}).Error)
		// Rebind the preferred account, then rotate its identity in the real repository.
		exchange(first, compositeProcessRequest{Operation: "failed_binding"})
		selectAccount(first, 0, accountIDs[0])
		exchange(first, compositeProcessRequest{Operation: "bind", AccountID: accountIDs[0]})
		writer, supportsCredentials := accounts.(interface {
			UpdateCredentials(context.Context, int64, map[string]any) error
		})
		require.True(test, supportsCredentials)
		beforeRotation, err := accounts.GetByID(ctx, accountIDs[0])
		require.NoError(test, err)
		priorCatalog := beforeRotation.GetUpstreamModelCatalog()
		require.NotNil(test, priorCatalog)
		require.NoError(test, writer.UpdateCredentials(ctx, accountIDs[0], map[string]any{"api_key": "rotated", "model_mapping": map[string]any{"fixture-target": "fixture-target"}}))
		rotated, err := accounts.GetByID(ctx, accountIDs[0])
		require.NoError(test, err)
		require.Greater(test, rotated.ModelCatalogState().IdentityRevision, beforeRotation.ModelCatalogState().IdentityRevision)
		require.NotNil(test, rotated.GetUpstreamModelCatalog())
		require.Equal(test, priorCatalog.IdentityRevision, rotated.GetUpstreamModelCatalog().IdentityRevision, "old publication must not be relabeled during credential rotation")
		test.Logf("identity rotation account=%d previous=%d current=%d stored_catalog=%d", rotated.ID, beforeRotation.ModelCatalogState().IdentityRevision, rotated.ModelCatalogState().IdentityRevision, rotated.GetUpstreamModelCatalog().IdentityRevision)
		selectAccount(second, 0, accountIDs[1])
		selectAccount(first, 0, accountIDs[1])
		require.Zero(test, exchange(second, compositeProcessRequest{Operation: "binding_absent"}).AccountID, "revoked identity must not remain sticky")
		require.Equal(test, "conflict", exchange(first, compositeProcessRequest{Operation: "publish"}).Error)
		require.Equal(test, "conflict", exchange(second, compositeProcessRequest{Operation: "publish"}).Error)

		// A fresh, authorized publication restores availability without reviving
		// the old clients' pinned identity or changing the Composite group.
		current, err := accounts.GetByID(ctx, accountIDs[0])
		require.NoError(test, err)
		require.NoError(test, committer.CommitAccountModelCatalog(ctx, current.ID, current.ModelCatalogBaseline(), compositeCatalogSnapshot(current, 9), current.ModelCatalogState(), &service.UpstreamModelMetadataSnapshot{}, false))
		exchange(first, compositeProcessRequest{Operation: "failed_binding"})
		selectAccount(first, 0, accountIDs[0])
		selectAccount(second, 0, accountIDs[0])
		require.Equal(test, "conflict", exchange(first, compositeProcessRequest{Operation: "publish"}).Error)
		require.Equal(test, "conflict", exchange(second, compositeProcessRequest{Operation: "publish"}).Error)
	})
}

func compositeCatalogSnapshot(account *service.Account, generation int64) *service.AccountModelCatalogSnapshot {
	baseline := account.ModelCatalogBaseline()
	return &service.AccountModelCatalogSnapshot{SchemaVersion: service.AccountModelCatalogSchemaVersion, EnrichmentVersion: "capability-v2", SourceGeneration: generation, SourceVersions: map[string]string{"catalog": fmt.Sprintf("fixture-%d", generation)}, Revision: baseline.SnapshotRevision + 1, IdentityRevision: baseline.IdentityRevision, RulesRevision: baseline.RulesRevision, Complete: true, DiscoveryScope: "account", DiscoveredModels: []string{"fixture-target"}, EligibleModels: []service.AccountCatalogModel{{ID: "fixture-target", Endpoints: []string{"chat_completions"}, EvidenceKind: "native_account_catalog"}}}
}
