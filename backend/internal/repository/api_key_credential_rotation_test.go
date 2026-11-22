package repository

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProjectCredentialRotatePreservesScopeAndCredentialStatus(t *testing.T) {
	for _, immediate := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		const keyID, workspaceID = int64(42), int64(8)
		deadline := time.Now().UTC().Add(time.Hour)

		mock.ExpectBegin()
		mock.ExpectQuery("project_key_policies.*FROM api_keys.*FOR UPDATE").
			WithArgs(keyID).WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "project"}).AddRow(workspaceID, true))
		mock.ExpectExec("UPDATE api_key_credentials").
			WithArgs(keyID, workspaceID, domain.CredentialStatusRevoked, sqlmock.AnyArg(), service.ProjectCredentialStatus, immediate, service.ProjectCredentialStatus).
			WillReturnResult(sqlmock.NewResult(0, 1))
		if !immediate {
			mock.ExpectExec("UPDATE api_key_credentials").
				WithArgs(keyID, workspaceID, service.ProjectCredentialStatus, deadline, sqlmock.AnyArg(), service.ProjectCredentialStatus).
				WillReturnResult(sqlmock.NewResult(0, 1))
		}
		mock.ExpectExec("INSERT INTO api_key_credentials").
			WithArgs(workspaceID, keyID, "fixture-digest", "tail", service.ProjectCredentialStatus, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		repo := NewAPIKeyCredentialRepository(db)
		err = repo.Rotate(t.Context(), service.APIKeyCredentialRotation{
			APIKeyID: keyID, WorkspaceID: 1, GraceUntil: deadline, RevokePrevious: immediate,
			NewCredential: service.APIKeyCredential{
				Digest: "fixture-digest", DisplaySuffix: "tail", Status: service.ProjectCredentialStatus,
			},
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestProjectCredentialRotateRejectsStalePolicy(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "project"}).AddRow(int64(8), true))
	mock.ExpectRollback()
	err = NewAPIKeyCredentialRepository(db).Rotate(t.Context(), service.APIKeyCredentialRotation{
		APIKeyID: 42, NewCredential: service.APIKeyCredential{Status: domain.CredentialStatusCurrent},
	})
	require.ErrorContains(t, err, "policy changed")
	require.NotErrorIs(t, err, sql.ErrNoRows)
	require.NoError(t, mock.ExpectationsWereMet())
}
