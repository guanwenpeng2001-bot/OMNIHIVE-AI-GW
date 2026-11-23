//go:build integration

package models

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	catalogpg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog/postgres"
	identitypg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
)

func TestCatalogStorageAndHTTP(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	ctx := t.Context()
	addr := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	require.NoError(t, testresources.ValidateRedis(addr))
	cache := testresources.RedisClient(t, &redis.Options{Addr: addr})
	require.NoError(t, cache.Ping(ctx).Err())
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	id := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&n))
		return n
	}
	actor := id("INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", uuid.NewString()+"@example.test")
	workspace := id("INSERT INTO workspaces(name) VALUES('catalog fixture') RETURNING id")
	project := id("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", workspace)
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')", workspace, actor)
	account := id(`INSERT INTO accounts(workspace_id,name,platform,type,extra) VALUES($1,'fixture','openai','apikey',
		'{"upstream_model_catalog":{"discovered_models":["private-target"]}}') RETURNING id`, workspace)
	exec(`UPDATE accounts SET extra=extra||'{"upstream_model_catalog_state":{"result":"failed","error":"private retry error"}}' WHERE id=$1`, account)
	require.Equal(t, int64(1), id(`SELECT count(*) FROM catalog_capability_evidence WHERE workspace_id=$1 AND source='legacy_discovery'
		AND payload #>> '{catalog,discovered_models,0}'='private-target'`, workspace))

	store := catalogpg.New(db)
	p := catalog.Perspective{Actor: actor, Workspace: workspace, Project: project}
	resources, err := store.ProjectResources(ctx, p)
	require.NoError(t, err)
	require.Contains(t, resources, catalogpg.Resource{ID: account, Kind: "accounts", Platform: "openai", AccountType: "apikey"})
	endpoints, err := store.EndpointEvidence(ctx, p, []int64{account})
	require.NoError(t, err)
	require.Equal(t, "failed", endpoints[account].LegacyResult)
	secretRef := uuid.NewString()
	exec("INSERT INTO credential_secrets(reference,workspace_id,account_id,ciphertext) VALUES($1,$2,$3,'fixture')", secretRef, workspace, account)
	verification := uuid.NewString()
	exec(`INSERT INTO provider_connections(account_id,workspace_id,provider,auth_type,name,concurrency,state,verified_id)
		VALUES($1,$2,'openai','apikey','fixture',1,'active',$3)`, account, workspace, verification)
	observed := time.Now().UTC().Add(-time.Hour)
	sample, err := json.Marshal(map[string]any{"models": []string{"private-target"}, "verified_at": observed})
	require.NoError(t, err)
	exec(`INSERT INTO connection_verifications(id,workspace_id,account_id,base_version,credential_version,proxy_revision,eligible,snapshot,expires_at)
		VALUES($1,$2,$3,1,1,'',true,$4,$5)`, verification, workspace, account, string(sample), observed.Add(24*time.Hour))
	endpoints, err = store.EndpointEvidence(ctx, p, []int64{account})
	require.NoError(t, err)
	require.Equal(t, []string{"private-target"}, endpoints[account].VerifiedModels)
	require.False(t, endpoints[account].VerificationFailed)
	exec(`INSERT INTO connection_verifications(id,workspace_id,account_id,base_version,credential_version,proxy_revision,eligible,snapshot,expires_at)
		VALUES($1,$2,$3,1,1,'',false,'{}',$4)`, uuid.NewString(), workspace, account, observed.Add(25*time.Hour))
	exec("UPDATE provider_connections SET verified_id=NULL WHERE account_id=$1", account)
	endpoints, err = store.EndpointEvidence(ctx, p, []int64{account})
	require.NoError(t, err)
	require.Equal(t, []string{"private-target"}, endpoints[account].VerifiedModels)
	require.True(t, endpoints[account].VerificationFailed)
	require.Equal(t, int64(1), id("SELECT count(*) FROM catalog_capability_evidence WHERE workspace_id=$1 AND source='connection_verification'", workspace))
	exec("UPDATE credential_secrets SET version=2,revoked_at=now() WHERE account_id=$1", account)
	endpoints, err = store.EndpointEvidence(ctx, p, []int64{account})
	require.NoError(t, err)
	require.Empty(t, endpoints[account].VerifiedModels)
	end := time.Now().UTC().Truncate(time.Hour)
	w := catalog.Window{Start: end.Add(-24 * time.Hour), End: end}
	modelID := catalog.StableID("openai", "private-target")
	m, err := store.Metrics(ctx, p, w, map[catalogpg.MetricKey]string{{Group: 1, Model: "private-target"}: modelID}, true)
	require.NoError(t, err)
	require.Equal(t, int64(0), *m[modelID].Requests)
	require.Nil(t, m[modelID].SuccessRate)

	release := uuid.NewString()
	registry := []catalog.RegistryModel{{Provider: "openai", Model: "visible", Facts: catalog.ModelFacts{
		Evidence: catalog.Observation("approved_registry", "stable", end, time.Time{}, end)}}}
	require.NoError(t, store.ImportRegistry(ctx, release, registry))
	input := catalog.Input{RegistryVersion: release, Registry: registry, Models: []catalog.Candidate{
		{Provider: "openai", Model: "visible", Offers: []catalog.Offer{{Endpoints: []catalog.Endpoint{{ID: "private-endpoint"}}}}},
	}}
	v := catalog.Project(catalog.Perspective{Workspace: 1}, input, w, end)
	require.NoError(t, store.Publish(ctx, v, release, end))
	require.NoError(t, store.Publish(ctx, v, release, end))
	require.Equal(t, int64(1), id("SELECT count(*) FROM catalog_publication_outbox WHERE version=$1", v.Version))
	require.Equal(t, int64(0), id("SELECT count(*) FROM catalog_publications WHERE version=$1 AND payload::text LIKE '%private-endpoint%'", v.Version))
	require.Error(t, store.Publish(ctx, catalog.Project(p, input, w, end), release, end))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(fixtureDefault)
	Register(r.Group("/api/v2"), New(&fixtureCatalog{}, identitypg.New(db), &config.Config{}), nil, []gin.HandlerFunc{
		func(c *gin.Context) {
			if c.Request.Header.Get("Authorization") == "fixture" {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor})
			}
			c.Next()
		},
	})
	check := func(path string, authorized bool, status int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if authorized {
			req.Header.Set("Authorization", "fixture")
		}
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		require.Equal(t, status, res.Code, res.Body.String())
	}
	path := fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/models", workspace, project)
	check(path, false, 401)
	check(path, true, 200)
	check(fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/models", workspace, project+1000000), true, 404)
}
