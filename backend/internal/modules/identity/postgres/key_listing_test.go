package postgres

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/stretchr/testify/require"
)

func TestProjectKeyReceiptReplaysCurrentProjection(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	ctx := t.Context()
	a := identity.Access{ActorID: 11, WorkspaceID: 12, ProjectID: 13}
	const keyID int64 = 14
	now := time.Now().UTC()
	expiry := now.Add(-time.Hour).In(time.FixedZone("fixture", 3*60*60))
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO identity_create_receipts").
		WithArgs(a.WorkspaceID, a.ProjectID, a.ActorID, "POST keys", "token", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow([]byte(`{"id":"14","name":"issued","credential":null}`)))
	mock.ExpectQuery("SELECT k.id,k.name").
		WithArgs(a.WorkspaceID, a.ProjectID, keyID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "prefix", "owner_id", "version", "status", "expires_at", "group_id", "kind", "now", "exhausted", "credential"}).
			AddRow(keyID, "renamed", "oh_p_", a.ActorID, 1, "active", expiry, nil, "project_policy", now, false, true))
	mock.ExpectRollback()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	row, err := createProjectKeyOnce(ctx, tx, a, "token", identity.CreateKey{}, func() (identity.Key, error) {
		t.Fatal("an existing receipt must not reissue a secret")
		return identity.Key{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, keyID, row.ID)
	require.Equal(t, "renamed", row.Name)
	require.Equal(t, "active", row.Status)
	require.Equal(t, identity.KeyExpired, row.State)
	require.Equal(t, expiry.UTC(), *row.ExpiresAt)
	require.Nil(t, row.GroupID)
	require.Nil(t, row.Credential)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
