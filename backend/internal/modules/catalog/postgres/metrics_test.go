package postgres

import (
	"database/sql"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/stretchr/testify/require"
)

func TestCatalogSparseMetricsKeepSampleCounts(t *testing.T) {
	zero := observedMetrics(catalog.Window{}, 0, 0, 0, 0, true, sql.NullInt64{}, sql.NullFloat64{})
	require.Equal(t, int64(0), *zero.Requests)
	require.Nil(t, zero.SuccessRate)
	value := sql.NullInt64{Int64: 0, Valid: true}
	sparse := observedMetrics(catalog.Window{}, 5, 0, 1, 0, true, value, sql.NullFloat64{})
	require.Equal(t, float64(0), *sparse.SuccessRate)
	require.Nil(t, sparse.TTFT)
	require.Equal(t, int64(1), *sparse.TTFTSamples)
	require.Nil(t, sparse.Speed)
	sparse = observedMetrics(catalog.Window{}, 5, 5, 5, 0, false, value, sql.NullFloat64{})
	require.Nil(t, sparse.SuccessRate)
	require.Equal(t, int64(0), *sparse.TTFT)
}
