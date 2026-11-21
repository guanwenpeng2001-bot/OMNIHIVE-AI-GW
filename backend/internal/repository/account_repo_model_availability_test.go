package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	_ "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/runtime"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestListModelAvailabilityCandidates_GroupQueryIgnoresTransientState(t *testing.T) {
	var capturedSQL string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(captureEntQueryMatcher{actual: &capturedSQL}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAccountRepositoryWithSQL(client, db, nil)

	mock.ExpectQuery("model availability candidates").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	groupID := int64(42)
	accounts, err := repo.ListModelAvailabilityCandidates(
		context.Background(),
		&groupID,
		[]string{service.PlatformAnthropic},
		false,
	)
	require.NoError(t, err)
	require.Empty(t, accounts)
	require.NoError(t, mock.ExpectationsWereMet())

	normalized := normalizeSQLWhitespace(capturedSQL)
	_, whereClause, found := strings.Cut(normalized, " WHERE ")
	require.True(t, found, "expected WHERE clause in query: %s", normalized)
	whereClause, _, _ = strings.Cut(whereClause, " ORDER BY ")
	for _, configuredPredicate := range []string{"group_id", "status", "schedulable", "platform"} {
		require.Contains(t, whereClause, configuredPredicate)
	}
	for _, transientPredicate := range []string{
		"rate_limit_reset_at",
		"overload_until",
		"temp_unschedulable_until",
		"expires_at",
		"auto_pause_on_expired",
	} {
		require.NotContains(t, whereClause, transientPredicate, "configured-state diagnosis must not filter transient predicate %q", transientPredicate)
	}
}

func TestModelCatalogProjectionReadsOnlyEvidenceAndRules(t *testing.T) {
	var capturedSQL string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(captureEntQueryMatcher{actual: &capturedSQL}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountRepository{sql: db}
	mock.ExpectQuery("catalog projection").WillReturnRows(sqlmock.NewRows([]string{
		"id", "platform", "type", "status", "schedulable", "parent_account_id", "expires_at", "auto_pause_on_expired", "temp_unschedulable_until", "workspace_id", "proxy_id", "proxy_group_id", "credentials", "extra",
	}).AddRow(int64(7), "openai", "apikey", "active", true, nil, nil, true, nil, 1, nil, nil,
		`{"model_mapping":{"public":"native"}}`, `{"upstream_model_catalog_state":{"identity_revision":3}}`))
	a, err := repo.GetModelCatalogAccount(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int64(3), a.ModelCatalogState().IdentityRevision)
	require.Equal(t, "native", a.GetMappedModel("public"))
	for _, secret := range []string{"api_key", "access_token", "refresh_token", "aws_secret_access_key"} {
		require.NotContains(t, capturedSQL, "'"+secret+"'")
		require.NotContains(t, a.Credentials, secret)
	}
	require.Contains(t, capturedSQL, "jsonb_object_agg")
	require.NoError(t, mock.ExpectationsWereMet())
}
