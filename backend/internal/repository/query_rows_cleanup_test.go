package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBulkSpecialAuthPreservesDecodeAndCloseErrors(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, database.Close())
	})
	errClose := errors.New("fixture rows close failure")
	rows := sqlmock.NewRows([]string{"platform", "credentials"}).AddRow("github_copilot", "{").CloseError(errClose)
	mock.ExpectQuery("SELECT platform, credentials FROM accounts").WillReturnRows(rows).RowsWillBeClosed()
	err = validateBulkCopilotNousCredentials(context.Background(), database, []int64{1}, map[string]any{"auth_mode": "fine_grained_pat"})
	var syntaxError *json.SyntaxError
	require.ErrorAs(t, err, &syntaxError)
	require.ErrorIs(t, err, errClose)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPendingSettlementPreservesRowsCloseErrors(t *testing.T) {
	for _, scenario := range []string{"pagination", "decode failure"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() {
				mock.ExpectClose()
				require.NoError(t, database.Close())
			})
			errClose := errors.New("fixture rows close failure")
			rows := sqlmock.NewRows([]string{"job"})
			if scenario == "pagination" {
				rows.AddRow(`{"id":2}`).AddRow(`{"id":1}`)
			} else {
				rows.AddRow("{")
			}
			rows.CloseError(errClose)
			mock.ExpectQuery("SELECT count").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
			mock.ExpectQuery("SELECT row_to_json").WillReturnRows(rows).RowsWillBeClosed()
			page, err := (&usageSettlementReceiptRepository{db: database}).ListUserPendingSettlement(context.Background(), 1, usagestats.UsageLogFilters{}, service.UserPendingSettlementCursor{Source: "upstream_job", Limit: 1})
			require.ErrorIs(t, err, errClose)
			if scenario == "decode failure" {
				var syntaxError *json.SyntaxError
				require.ErrorAs(t, err, &syntaxError)
				require.Nil(t, page)
			} else {
				require.Len(t, page.Items, 1)
				require.EqualValues(t, 2, page.NextCursor)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
