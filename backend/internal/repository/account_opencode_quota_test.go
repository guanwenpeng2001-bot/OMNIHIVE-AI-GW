package repository

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenCodeQuotaRepositoryRejectsReplacedOrDeletedKey(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "replaced", true: "deleted"}[deleted], func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
			t.Cleanup(func() { _ = client.Close() })
			repository := newAccountRepositoryWithSQL(client, database, nil)
			expected := &service.Account{ID: 55, Platform: service.PlatformOpenCodeGo, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "old-test-key"}, Extra: map[string]any{}}
			mock.ExpectBegin()
			lockedRows := sqlmock.NewRows([]string{"id"})
			if !deleted {
				lockedRows.AddRow(int64(55))
			}
			mock.ExpectQuery("SELECT id FROM accounts.*FOR UPDATE").WithArgs(int64(55)).WillReturnRows(lockedRows)
			if !deleted {
				mock.ExpectQuery(`SELECT .* FROM "accounts" WHERE "accounts"."id" = \$1`).WithArgs(int64(55)).WillReturnRows(
					sqlmock.NewRows([]string{"id", "platform", "type", "credentials", "extra"}).AddRow(
						int64(55), service.PlatformOpenCodeGo, service.AccountTypeAPIKey, []byte(`{"api_key":"replacement-test-key"}`), []byte(`{}`)))
			}
			mock.ExpectRollback()
			err = repository.ApplyOpenCodeQuotaProbe(context.Background(), expected, &service.CNProviderQuotaProbeResult{
				Success: true, CredentialValid: true, Tiers: []service.CNQuotaTier{{Window: "monthly", UsedPercent: 100}},
			})
			require.ErrorIs(t, err, service.ErrAccountRuntimeIdentityChanged)
			require.NoError(t, mock.ExpectationsWereMet(), "stale probe must roll back without quota or status writes")
		})
	}
}
