//go:build unit

package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBatchImageRecovery_SQLUsesWorkerPageSize(t *testing.T) {
	for _, configured := range []int{0, -1, 100, 1000, 1001, 2000} {
		t.Run(fmt.Sprintf("limit_%d", configured), func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = database.Close() }()
			repository := &batchImageRepository{sql: database}
			expectedLimit := 100
			if configured == 1000 {
				expectedLimit = 1000
			}
			workerLimit := service.NormalizeBatchImageRecoveryLimit(configured)
			require.Equal(t, expectedLimit, workerLimit)
			for _, inputLimit := range []int{configured, workerLimit} {
				mock.ExpectQuery(`SELECT batch_id FROM batch_image_jobs`).
					WithArgs("imgbatch_cursor", expectedLimit).
					WillReturnRows(sqlmock.NewRows([]string{"batch_id"}).AddRow("imgbatch_tail"))
				ids, err := repository.ListRecoverableBatchImageJobs(context.Background(), "imgbatch_cursor", inputLimit)
				require.NoError(t, err)
				require.Equal(t, []string{"imgbatch_tail"}, ids)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
