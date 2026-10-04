//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorPersistentKeyWritesRejectedWithoutMutation(t *testing.T) {
	encryptor := &persistentTestEncryptor{}
	source := &ChannelMonitor{ID: 42, Name: "original", Provider: MonitorProviderOpenAI, APIKey: "ENC:synthetic", Endpoint: "https://8.8.8.8", PrimaryModel: "test-model", IntervalSeconds: 60}
	repo := &duplicateChannelMonitorRepoStub{source: source}
	monitorService := NewChannelMonitorService(repo, encryptor)
	_, err := monitorService.Create(context.Background(), ChannelMonitorCreateParams{Name: "new", Provider: source.Provider, APIKey: "synthetic", Endpoint: source.Endpoint, PrimaryModel: source.PrimaryModel, IntervalSeconds: 60})
	require.ErrorIs(t, err, ErrEncryptionKeyRequired)
	newKey, newName := "replacement", "changed"
	_, err = monitorService.Update(context.Background(), 42, ChannelMonitorUpdateParams{APIKey: &newKey, Name: &newName})
	require.ErrorIs(t, err, ErrEncryptionKeyRequired)
	require.Equal(t, "original", source.Name)
	require.Equal(t, "ENC:synthetic", source.APIKey)
	_, err = monitorService.Duplicate(context.Background(), 42, 7, "admin:7", "copy")
	require.ErrorIs(t, err, ErrEncryptionKeyRequired)
	require.Empty(t, repo.created)
	require.Zero(t, encryptor.encryptCalls)
	for _, key := range []*string{nil, new(string)} {
		_, updated, err := monitorService.applyAPIKeyUpdate(source, key)
		require.NoError(t, err)
		require.False(t, updated)
	}
}

func TestChannelMonitorQuotaEmptyKeyStillWorksWithoutPersistentKey(t *testing.T) {
	accountID := int64(7)
	repo := &duplicateChannelMonitorRepoStub{}
	monitorService := NewChannelMonitorService(repo, &persistentTestEncryptor{})
	monitorService.SetQuotaFetcher(newQuotaModeFetcher(map[int64]*Account{
		accountID: {ID: accountID, Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": "coding"}},
	}, nil))
	created, err := monitorService.Create(context.Background(), ChannelMonitorCreateParams{Name: "quota", Provider: MonitorProviderKimi, CheckMode: MonitorCheckModeQuota, AccountID: &accountID, IntervalSeconds: 60})
	require.NoError(t, err)
	require.Empty(t, created.APIKey)
	repo.source = repo.created[0]
	duplicate, err := monitorService.Duplicate(context.Background(), created.ID, 7, "admin:7", "empty-copy")
	require.NoError(t, err)
	require.Empty(t, duplicate.APIKey)
	require.Len(t, repo.created, 2)
}
