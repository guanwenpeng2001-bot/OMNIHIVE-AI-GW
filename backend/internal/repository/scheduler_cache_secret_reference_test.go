//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerCacheProjectionDoesNotExposeReferencedCredentials(t *testing.T) {
	secret := "synthetic" + "-scheduler-secret"
	account := service.Account{
		ID: 9, WorkspaceID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{
			service.CredentialSecretReferenceKey: "synthetic-reference",
			service.CredentialSecretVersionKey:   2,
			"api_key":                            secret,
			"model_mapping":                      map[string]any{"public": "upstream"},
		},
	}
	full, metadata, err := marshalSchedulerCacheAccount(account)
	require.NoError(t, err)
	require.NotContains(t, string(full), secret)
	require.NotContains(t, string(metadata), secret)
	var projected service.Account
	require.NoError(t, json.Unmarshal(metadata, &projected))
	reference, ok := service.CredentialReference(projected.Credentials)
	require.True(t, ok)
	require.Equal(t, "synthetic-reference", reference)
	require.Equal(t, map[string]any{"public": "upstream"}, projected.Credentials["model_mapping"])
}

func TestSchedulerCacheRewritesLeakedReferencedMetadata(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	secret := "synthetic" + "-old-cache-secret"
	account := service.Account{
		ID: 73, WorkspaceID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{
			service.CredentialSecretReferenceKey: "synthetic-reference",
			service.CredentialSecretVersionKey:   2,
			"api_key":                            secret,
		},
	}
	id := strconv.FormatInt(account.ID, 10)
	// Simulate a projection created by the old implementation.
	require.NoError(t, cache.rdb.Set(ctx, schedulerAccountMetaKey(id), `{"Credentials":{"api_key":"`+secret+`"}}`, 0).Err())
	require.NoError(t, cache.SetAccount(ctx, &account))
	for _, key := range []string{schedulerAccountKey(id), schedulerAccountMetaKey(id)} {
		payload, err := cache.rdb.Get(ctx, key).Result()
		require.NoError(t, err)
		require.NotContains(t, payload, secret)
	}
	loaded, err := cache.GetAccount(ctx, account.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	_, referenced := service.CredentialReference(loaded.Credentials)
	require.True(t, referenced)
}
