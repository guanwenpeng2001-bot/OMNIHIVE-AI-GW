package securityaudit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestPreviewDeleteWithProcessLocalEncryptionKey(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	cfg := &config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}}
	encryptor, err := repository.NewAESEncryptor(cfg)
	require.NoError(t, err)
	require.False(t, encryptor.EncryptionKeyConfigured())
	manager := NewConfigManager(nil, nil, nil, encryptor, cfg)
	promptService := &PromptService{config: manager, repo: NewPostgreSQLRepository(database), clock: realClock{}}
	end := time.Now().UTC()
	start := end.Add(-time.Hour)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"count", "max"}).AddRow(2, 7))
	mock.ExpectCommit()
	preview, err := promptService.PreviewDelete(context.Background(), EventFilter{StartAt: &start, EndAt: &end}, 42)
	require.NoError(t, err)
	plain, err := manager.Decrypt(preview.ConfirmationToken)
	require.NoError(t, err)
	var claims deleteClaims
	require.NoError(t, json.Unmarshal([]byte(plain), &claims))
	require.Equal(t, int64(42), claims.AdminID)
	require.Equal(t, int64(7), claims.SnapshotMaxID)
	require.Equal(t, preview.FilterHash, claims.FilterHash)
	require.NoError(t, mock.ExpectationsWereMet())
	mock.ExpectClose()
}
