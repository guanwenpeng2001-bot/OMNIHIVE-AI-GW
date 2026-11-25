//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestLegacyModelCatalogSeedUpgrade(t *testing.T) {
	connectionString := os.Getenv("NEW_PLATFORMS_TEST_POSTGRES_DSN")
	if connectionString == "" {
		t.Skip("set NEW_PLATFORMS_TEST_POSTGRES_DSN for an isolated PostgreSQL upgrade fixture")
	}
	database := testresources.OpenSchema(t, connectionString)
	_, err := database.Exec(`CREATE TABLE accounts (
		id bigint PRIMARY KEY, platform text NOT NULL, type text NOT NULL,
		credentials jsonb NOT NULL DEFAULT '{}', extra jsonb,
		parent_account_id bigint, deleted_at timestamptz,
		workspace_id bigint NOT NULL DEFAULT 17,
		updated_at timestamptz NOT NULL DEFAULT '2026-09-20T00:00:00Z'
	);
	CREATE TABLE scheduler_outbox(event_type text, payload jsonb);
	CREATE TABLE account_groups(account_id bigint, group_id bigint);
	CREATE TABLE groups(id bigint);
	CREATE TABLE composite_model_routes(id bigint, group_id bigint);
	CREATE TABLE proxies(id bigint);
	CREATE TABLE channels(id bigint);
	CREATE TABLE channel_groups(id bigint);
	CREATE TABLE channel_model_pricing(id bigint);
	CREATE TABLE channel_pricing_intervals(id bigint);
	CREATE TABLE error_passthrough_rules(id bigint);
	INSERT INTO accounts(id,platform,type,credentials,extra) VALUES
	(51,'anthropic','apikey','{"model_mapping":{"public-model":"actual-model","duplicate":"actual-model","bad":"wild*","empty":"","number":42}}','{"note":"keep","upstream_model_metadata":{"source":"upstream","models":{"actual-model":{"id":"actual-model"}}}}'),
	(52,'openai','apikey','{"model_mapping":{"actual-model":"actual-model"}}','{"openai_passthrough":true,"openai_responses_supported":true}'),
	(53,'anthropic','apikey','{}','{"upstream_model_metadata":{"source":"models.dev","models":{"historical-model":{"id":"historical-model"},"*":{"id":"*"},"wrong-id":{"id":"different"}}}}'),
	(54,'anthropic','apikey','{"model_mapping":{"*":"*"}}','{}'),
	(55,'anthropic','apikey','{"model_mapping":{"public-model":"actual-model"}}','{"upstream_model_catalog":{"complete":true,"revision":9,"eligible_models":[],"discovered_models":[]}}'),
	(56,'anthropic','apikey','{"model_mapping":{"public-model":"actual-model"}}','{"upstream_model_catalog_state":{"identity_revision":2,"result":"credentials_invalid"}}'),
	(57,'anthropic','apikey','{"model_mapping":{"public-model":"actual-model"}}','{"model_availability":[]}'),
	(58,'anthropic','apikey','{"model_mapping":{"public-model":"actual-model"}}','{}'),
	(59,'openai','oauth','{"model_mapping":{"actual-model":"actual-model"}}','{}'),
	(60,'openai','oauth','{"model_mapping":{"actual-model":"actual-model"}}','{}'),
	(61,'anthropic','apikey','{"model_mapping":["invalid"]}','{"upstream_model_metadata":{"models":["invalid"]}}'),
	(62,'anthropic','apikey','{}',NULL),
	(63,'anthropic','apikey','{"model_mapping":{"public-model":"actual-model"}}','{"upstream_model_catalog":null}'),
	(64,'anthropic','apikey','{"model_mapping":{"*":"actual-model"}}','{}');
	UPDATE accounts SET deleted_at=now() WHERE id=58;
	UPDATE accounts SET parent_account_id=60 WHERE id=59;`)
	require.NoError(t, err)
	loadAccount := func(accountID int64) *service.Account {
		t.Helper()
		account := &service.Account{ID: accountID, Status: service.StatusActive, Schedulable: true}
		var credentials, extra []byte
		var parentID sql.NullInt64
		require.NoError(t, database.QueryRow(`SELECT platform,type,credentials,COALESCE(extra,'{}'),parent_account_id FROM accounts WHERE id=$1`, accountID).Scan(&account.Platform, &account.Type, &credentials, &extra, &parentID))
		require.NoError(t, json.Unmarshal(credentials, &account.Credentials))
		require.NoError(t, json.Unmarshal(extra, &account.Extra))
		if parentID.Valid {
			account.ParentAccountID = &parentID.Int64
		}
		return account
	}
	ctx := context.Background()
	options := service.AccountModelBindingOptions{Endpoint: "messages"}
	require.Nil(t, loadAccount(51).GetUpstreamModelCatalog(), "audit B1 before upgrade")
	var originalCredentials string
	require.NoError(t, database.QueryRow(`SELECT credentials::text FROM accounts WHERE id=51`).Scan(&originalCredentials))

	contents, err := migrations.FS.ReadFile("265_group_generation.sql")
	require.NoError(t, err)
	_, err = database.Exec(string(contents))
	require.NoError(t, err)
	account := loadAccount(51)
	require.True(t, service.AccountSupportsResolvedModel(ctx, account, "public-model", options))
	require.Len(t, account.EffectiveUpstreamModelCatalog().EligibleModels, 1)
	require.Empty(t, account.EffectiveUpstreamModelCatalog().DiscoveredModels)
	require.Equal(t, "legacy-mapping-upgrade", account.EffectiveUpstreamModelCatalog().SourceKind)
	require.Equal(t, "keep", account.Extra["note"])
	require.True(t, service.AccountSupportsResolvedModel(ctx, loadAccount(52), "actual-model", service.AccountModelBindingOptions{Endpoint: "chat_completions"}))
	require.True(t, service.AccountSupportsResolvedModel(ctx, loadAccount(52), "actual-model", service.AccountModelBindingOptions{Endpoint: "responses"}))
	unobserved := loadAccount(52)
	delete(unobserved.Extra, "openai_responses_supported")
	require.False(t, service.AccountSupportsResolvedModel(ctx, unobserved, "actual-model", service.AccountModelBindingOptions{Endpoint: "responses"}), "HTTP passthrough requires Responses protocol evidence")
	unobserved.Extra["openai_passthrough"] = false
	require.True(t, service.AccountSupportsResolvedModel(ctx, unobserved, "actual-model", service.AccountModelBindingOptions{Endpoint: "responses"}), "legacy forwarding retains its default Responses path")
	require.True(t, service.AccountSupportsResolvedModel(ctx, loadAccount(64), "any-public-alias", options))
	for _, accountID := range []int64{53, 54, 56, 57, 58, 61, 62, 63} {
		require.Nil(t, loadAccount(accountID).GetUpstreamModelCatalog(), "account %d must not receive a seed", accountID)
	}
	require.EqualValues(t, 9, loadAccount(55).GetUpstreamModelCatalog().Revision)
	require.Empty(t, loadAccount(55).GetUpstreamModelCatalog().EligibleModels)
	shadow, parent := loadAccount(59), loadAccount(60)
	shadowOptions := service.AccountModelBindingOptions{Endpoint: "responses", CredentialAccount: parent}
	require.True(t, service.AccountSupportsResolvedModel(ctx, shadow, "actual-model", shadowOptions))
	seedOnlyShadow := loadAccount(59)
	seedOnlyShadow.Credentials["model_mapping"] = map[string]any{}
	seedBinding := service.ResolveAccountModelBinding(ctx, seedOnlyShadow, "actual-model", shadowOptions)
	require.True(t, seedBinding.Supported)
	require.Equal(t, "legacy-mapping-upgrade", seedBinding.EvidenceKind)
	parent.Extra[service.UpstreamModelCatalogStateExtraKey] = service.AccountModelCatalogState{IdentityRevision: 1}
	currentBinding := service.ResolveAccountModelBinding(ctx, shadow, "actual-model", shadowOptions)
	require.True(t, currentBinding.Supported, "saved selection survives discarded credential-owner evidence")
	require.Equal(t, service.ModelEvidenceConfiguredSelection, currentBinding.EvidenceKind)
	require.False(t, service.AccountSupportsResolvedModel(ctx, seedOnlyShadow, "actual-model", shadowOptions), "stale credential-owner evidence alone cannot authorize a model")
	require.False(t, service.AccountSupportsResolvedModel(ctx, shadow, "unselected-model", shadowOptions))

	var firstSeed, secondSeed, savedCredentials string
	var firstGeneration, secondGeneration int64
	require.NoError(t, database.QueryRow(`SELECT extra::text FROM accounts WHERE id=51`).Scan(&firstSeed))
	require.NoError(t, database.QueryRow(`SELECT generation FROM model_catalog_generations WHERE scope='catalog'`).Scan(&firstGeneration))
	require.Positive(t, firstGeneration, "seeding must use the existing outbox/generation trigger")
	_, err = database.Exec(string(contents))
	require.NoError(t, err)
	require.NoError(t, database.QueryRow(`SELECT extra::text,credentials::text FROM accounts WHERE id=51`).Scan(&secondSeed, &savedCredentials))
	require.NoError(t, database.QueryRow(`SELECT generation FROM model_catalog_generations WHERE scope='catalog'`).Scan(&secondGeneration))
	require.Equal(t, firstGeneration, secondGeneration, "reapplication must not publish phantom changes")
	require.Equal(t, firstSeed, secondSeed, "reapplication must preserve the original evidence and revision")
	require.Equal(t, originalCredentials, savedCredentials)
	var unchangedAccounts int
	require.NoError(t, database.QueryRow(`SELECT count(*) FROM accounts WHERE workspace_id=17 AND updated_at='2026-09-20T00:00:00Z'`).Scan(&unchangedAccounts))
	require.Equal(t, 14, unchangedAccounts)

	// Once new catalog evidence replaces the seed, a repeat cannot resurrect it.
	_, err = database.Exec(`UPDATE accounts SET extra=jsonb_set(extra,'{upstream_model_catalog}','{"revision":2,"complete":true,"eligible_models":[],"discovered_models":[]}') WHERE id=51`)
	require.NoError(t, err)
	var replacedCatalog, reappliedCatalog string
	require.NoError(t, database.QueryRow(`SELECT extra->'upstream_model_catalog' FROM accounts WHERE id=51`).Scan(&replacedCatalog))
	_, err = database.Exec(string(contents))
	require.NoError(t, err)
	require.NoError(t, database.QueryRow(`SELECT extra->'upstream_model_catalog' FROM accounts WHERE id=51`).Scan(&reappliedCatalog))
	require.Equal(t, replacedCatalog, reappliedCatalog, "reapplication must not resurrect replaced seed evidence")
	replacement := loadAccount(51)
	require.EqualValues(t, 2, replacement.GetUpstreamModelCatalog().Revision)
	require.Empty(t, replacement.GetUpstreamModelCatalog().EligibleModels)
	require.Empty(t, replacement.GetUpstreamModelCatalog().DiscoveredModels)
	replacementBinding := service.ResolveAccountModelBinding(ctx, replacement, "public-model", options)
	require.True(t, replacementBinding.Supported, "empty discovery preserves the saved selection")
	require.Equal(t, service.ModelEvidenceConfiguredSelection, replacementBinding.EvidenceKind)
	replacement.Credentials["model_mapping"] = map[string]any{}
	replacement.Extra[service.UpstreamModelAllowlistExtraKey] = service.AccountModelAllowlist{Enabled: true, Reason: "explicit deny test"}
	require.False(t, service.AccountSupportsResolvedModel(ctx, replacement, "public-model", options), "removing the saved selection revokes access")

	_, err = database.Exec(`TRUNCATE accounts`)
	require.NoError(t, err)
	_, err = database.Exec(string(contents))
	require.NoError(t, err, "empty installations need no catalog seed")
}
