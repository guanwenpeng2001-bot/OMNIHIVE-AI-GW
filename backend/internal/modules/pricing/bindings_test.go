package pricing

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestManagedPriceBookDoesNotFallBackAfterExpiry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	at := time.Now().UTC()
	mock.ExpectQuery("WITH selected AS").WithArgs("scope", at).WillReturnRows(sqlmock.NewRows([]string{"tariff"}).AddRow(nil))
	versions, err := (VersionStore{DB: db}).At(context.Background(), "scope", at)
	require.ErrorIs(t, err, ErrPriceInactive)
	require.Nil(t, versions)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPublishClosesBindingWithoutChangingTariff(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	v := Version{ID: "new", Source: "group", Currency: "USD", EffectiveFrom: at}
	body, err := json.Marshal(v)
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("scope").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT tariff FROM exact_price_versions").WithArgs("scope", "new").WillReturnRows(sqlmock.NewRows([]string{"tariff"}).AddRow(body))
	mock.ExpectQuery("SELECT id,effective_from,version_id FROM price_book_bindings").WithArgs("scope").WillReturnRows(sqlmock.NewRows([]string{"id", "effective_from", "version_id"}).AddRow(1, at.Add(-time.Hour), "old"))
	mock.ExpectExec("UPDATE price_book_bindings").WithArgs(int64(1), at, int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO price_book_bindings").WithArgs("scope", "new", at, nil, int64(7), int64(1)).WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()
	require.NoError(t, (VersionStore{DB: db}).Publish(context.Background(), "scope", "new", 7, at))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReferencePriceRejectedBeforePersistence(t *testing.T) {
	// A nil DB proves no database write can occur for a reference-only card.
	require.ErrorIs(t, (VersionStore{}).Save(context.Background(), "scope", Version{ID: "pi", Source: "pi_ai"}), ErrReferencePrice)
}
