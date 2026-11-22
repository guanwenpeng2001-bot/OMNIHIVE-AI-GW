package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// SQL-shape tests can declare an empty target set without inventing connection
// ownership. Actual row-lock and conflict behavior is covered against PostgreSQL.
func expectAccountConfigurationPreflight(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("account_configuration_owner_lock").WillReturnRows(sqlmock.NewRows([]string{"id"}))
}

type accountConfigurationRecordingDriver struct {
	*entsql.Driver
	recorder *recordingSQLExecutor
}

type accountConfigurationRecordingTx struct {
	dialect.Tx
	recorder *recordingSQLExecutor
}

func (driver accountConfigurationRecordingDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := driver.Driver.Tx(ctx)
	if err != nil {
		return nil, err
	}
	return accountConfigurationRecordingTx{Tx: tx, recorder: driver.recorder}, nil
}

func (tx accountConfigurationRecordingTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.recorder.ExecContext(ctx, query, args...)
}

func (tx accountConfigurationRecordingTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return tx.Tx.(*entsql.Tx).QueryContext(ctx, query, args...)
}

func recordingAccountConfigurationRepository(t *testing.T, recorder *recordingSQLExecutor) *accountRepository {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(accountConfigurationRecordingDriver{
		Driver: entsql.OpenDB(dialect.Postgres, db), recorder: recorder,
	}))
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = client.Close()
	})
	mock.ExpectBegin()
	expectAccountConfigurationPreflight(mock)
	mock.ExpectCommit()
	return newAccountRepositoryWithSQL(client, db, nil)
}

func TestManagedConnectionRejectsSelectionAndBulkBeforeMutation(t *testing.T) {
	for _, mutation := range []string{"scope", "mapping", "bulk_name", "bulk_inline", "bulk_secret", "admin_plan_observation_shape"} {
		t.Run(mutation, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := newAccountRepositoryWithSQL(client, db, nil)
			repo.credentialEncryptor = persistentTestAES(t, true)
			ctx := domain.WithWorkspaceScope(context.Background(), 9)
			mock.ExpectBegin()
			mock.ExpectQuery("account_configuration_owner_lock.*workspace_id = 9").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
			mock.ExpectQuery("SELECT account_id FROM provider_connections.*workspace_id = 9").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"account_id"}).AddRow(7))
			mock.ExpectRollback()
			switch mutation {
			case "scope", "mapping":
				var mapping *map[string]string
				if mutation == "mapping" {
					mapping = &map[string]string{"alias": "changed"}
				}
				_, err = repo.ReplaceAccountModelSelectionScope(ctx, 7, service.AccountModelCatalogBaseline{}, service.AccountModelAllowlist{Enabled: true, Models: []string{"changed"}}, mapping)
			default:
				patch := service.AccountBulkUpdate{}
				switch mutation {
				case "bulk_name":
					name := "changed"
					patch.Name = &name
				case "bulk_inline":
					patch.Credentials = map[string]any{"model_mapping": map[string]string{"alias": "changed"}}
				case "bulk_secret":
					patch.Credentials = map[string]any{"api_key": "synthetic-new-secret"}
				case "admin_plan_observation_shape":
					patch.Credentials = map[string]any{"plan_type": "free"}
				}
				_, err = repo.BulkUpdate(ctx, []int64{7}, patch)
			}
			require.ErrorIs(t, err, service.ErrAccountConnectionManaged)
			var application *infraerrors.ApplicationError
			require.ErrorAs(t, err, &application)
			require.Equal(t, "7", application.Metadata["account_id"])
			require.Equal(t, "connections", application.Metadata["management_surface"])
			require.NoError(t, mock.ExpectationsWereMet(), "no account, secret, or outbox write may precede the ownership check")
		})
	}
}

func TestAccountConfigurationGuardDoesNotRevealForeignAccounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAccountRepositoryWithSQL(client, db, nil)
	mock.ExpectBegin()
	mock.ExpectQuery("account_configuration_owner_lock.*workspace_id = 9").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	_, err = repo.ReplaceAccountModelSelectionScope(domain.WithWorkspaceScope(context.Background(), 9), 77, service.AccountModelCatalogBaseline{}, service.AccountModelAllowlist{}, nil)
	require.ErrorIs(t, err, service.ErrAccountNotFound)
	require.NotErrorIs(t, err, service.ErrAccountConnectionManaged)
	require.NoError(t, mock.ExpectationsWereMet(), "foreign targets must not reach the metadata-bearing connection query")
}

func TestAccountConfigurationGuardDatabaseFailureDoesNotOpenAuthority(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAccountRepositoryWithSQL(client, db, nil)
	mock.ExpectBegin()
	mock.ExpectQuery("account_configuration_owner_lock").WillReturnError(errors.New("synthetic database unavailable"))
	mock.ExpectRollback()
	name := "changed"
	_, err = repo.BulkUpdate(context.Background(), []int64{7}, service.AccountBulkUpdate{Name: &name})
	require.ErrorContains(t, err, "synthetic database unavailable")
	require.NoError(t, mock.ExpectationsWereMet())
}
