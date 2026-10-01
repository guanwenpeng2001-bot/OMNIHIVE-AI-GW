//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/batchimagejob"
	"github.com/Wei-Shaw/sub2api/ent/migrate"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBatchImageEgressSnapshotDedicatedPostgres(t *testing.T) {
	_, client := proxyGroupTestPostgres(t)
	ctx := context.Background()
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback() // Own fixtures and migration DDL are fully transactional.
	c := tx.Client()
	user, err := c.User.Create().SetEmail("egress-" + uuid.NewString() + "@fixture.invalid").SetPasswordHash("fixture-only").Save(ctx)
	require.NoError(t, err)
	batchID := uuid.NewString()
	requestHash, manifestHash := uuid.NewString(), uuid.NewString()
	// Legacy insertion deliberately omits the new field and works before migration.
	_, err = c.ExecContext(ctx, `INSERT INTO batch_image_jobs
		(batch_id,user_id,provider,model,item_count,request_hash,manifest_hash,estimated_cost,hold_amount,actual_cost)
		VALUES ($1,$2,'fixture','fixture',1,$3,$4,1.25,2.5,0.75)`, batchID, user.ID, requestHash, manifestHash)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("281_batch_image_egress_snapshot.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = c.ExecContext(ctx, string(migration))
		require.NoError(t, err, "new migration must be idempotent")
	}
	columns, err := c.QueryContext(ctx, `SELECT data_type,is_nullable,column_default FROM information_schema.columns
		WHERE table_schema='public' AND table_name='batch_image_jobs' AND column_name='egress_snapshot'`)
	require.NoError(t, err)
	require.True(t, columns.Next())
	var dataType, nullable string
	var defaultValue sql.NullString
	require.NoError(t, columns.Scan(&dataType, &nullable, &defaultValue))
	require.NoError(t, columns.Close())
	require.Equal(t, "jsonb", dataType)
	require.Equal(t, "YES", nullable)
	require.False(t, defaultValue.Valid)
	found := false
	for _, column := range migrate.BatchImageJobsTable.Columns {
		if column.Name == batchimagejob.FieldEgressSnapshot {
			found = true
			require.True(t, column.Nullable)
			require.Nil(t, column.Default)
			require.Equal(t, "jsonb", column.SchemaType["postgres"])
		}
	}
	require.True(t, found, "generated schema must include the nullable column")
	legacy, err := c.BatchImageJob.Query().Where(batchimagejob.BatchIDEQ(batchID)).Only(ctx)
	require.NoError(t, err)
	require.Nil(t, legacy.EgressSnapshot, "migration must not backfill legacy jobs")
	nullRows, err := c.QueryContext(ctx, "SELECT egress_snapshot IS NULL FROM batch_image_jobs WHERE batch_id=$1", batchID)
	require.NoError(t, err)
	require.True(t, nullRows.Next())
	var sqlNull bool
	require.NoError(t, nullRows.Scan(&sqlNull))
	require.NoError(t, nullRows.Close())
	require.True(t, sqlNull, "legacy absence must be SQL NULL, not JSON null")
	require.NoError(t, c.BatchImageJob.UpdateOneID(legacy.ID).SetEgressSnapshot(map[string]any{"mode": "proxy", "proxy_id": int64(211)}).Exec(ctx))
	stored, err := c.BatchImageJob.Get(ctx, legacy.ID)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"mode": "proxy", "proxy_id": float64(211)}, stored.EgressSnapshot)
	require.Equal(t, &requestHash, stored.RequestHash)
	require.Equal(t, &manifestHash, stored.ManifestHash)
	require.Equal(t, legacy.EstimatedCost, stored.EstimatedCost)
	require.Equal(t, legacy.HoldAmount, stored.HoldAmount)
	require.Equal(t, legacy.ActualCost, stored.ActualCost)
	encoded, err := json.Marshal(stored)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "egress_snapshot", "server storage metadata must not appear in entity JSON")
	var supplied dbent.BatchImageJob
	require.NoError(t, json.Unmarshal([]byte(`{"egress_snapshot":{"mode":"proxy","proxy_id":999}}`), &supplied))
	require.Nil(t, supplied.EgressSnapshot, "entity JSON must not accept client snapshot input")
	require.NoError(t, c.BatchImageJob.UpdateOneID(legacy.ID).ClearEgressSnapshot().Exec(ctx))
	cleared, err := c.BatchImageJob.Get(ctx, legacy.ID)
	require.NoError(t, err)
	require.Nil(t, cleared.EgressSnapshot)
	created, err := c.BatchImageJob.Create().SetBatchID(uuid.NewString()).SetUserID(user.ID).
		SetProvider("fixture").SetModel("fixture").SetItemCount(1).Save(ctx)
	require.NoError(t, err)
	require.Nil(t, created.EgressSnapshot, "new jobs also have no implicit egress snapshot")
}
