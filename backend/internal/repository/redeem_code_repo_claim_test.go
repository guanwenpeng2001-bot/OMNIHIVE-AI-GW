//go:build unit

package repository

import (
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRedeemCodeClaimChecksExpiryInAtomicUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repository := NewRedeemCodeRepository(dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db))))
	mock.ExpectExec(`UPDATE "redeem_codes".*"expires_at" IS NULL OR "expires_at" > clock_timestamp\(\)`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	require.ErrorIs(t, repository.Use(t.Context(), 1, 2), service.ErrRedeemCodeUsed)
	require.NoError(t, mock.ExpectationsWereMet())
}
