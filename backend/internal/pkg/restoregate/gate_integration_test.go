//go:build integration

package restoregate

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestBootstrapQueryDoesNotAbortFencingTransaction(t *testing.T) {
	dsn := os.Getenv("RESTOREGATE_TEST_DSN")
	if dsn == "" {
		t.Skip("requires RESTOREGATE_TEST_DSN")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	gate := NewBusinessPool(db)
	_, leave, err := gate.Admit(t.Context())
	require.NoError(t, err)
	defer leave()
	_, err = gate.held.ExecContext(t.Context(), "SELECT 1")
	require.NoError(t, err)
}
