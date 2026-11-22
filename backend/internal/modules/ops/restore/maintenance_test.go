package restore

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceDistinguishesJournalOutageFromRestore(t *testing.T) {
	tests := []struct {
		name       string
		queryError error
		blocked    bool
		wantReason string
	}{
		{name: "restore", blocked: true, wantReason: "RESTORE_MAINTENANCE"},
		{name: "journal unavailable", queryError: errors.New("journal unavailable"), wantReason: "RESTORE_STATUS_UNAVAILABLE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var journal *sql.DB
			journal, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = journal.Close() }()
			mock.MatchExpectationsInOrder(false)
			mock.ExpectExec("BEGIN").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery("pg_try_advisory_xact_lock_shared").WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
			query := mock.ExpectQuery("SELECT EXISTS")
			if tt.queryError != nil {
				query.WillReturnError(tt.queryError)
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"blocked"}).AddRow(tt.blocked))
			}
			mock.ExpectExec("ROLLBACK").WillReturnResult(sqlmock.NewResult(0, 0))
			handler := (&Service{Journal: journal}).Maintenance(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("request reached handler")
			}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/messages", nil))
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			require.Contains(t, response.Body.String(), tt.wantReason)
			require.Equal(t, "1", response.Header().Get("Retry-After"))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
