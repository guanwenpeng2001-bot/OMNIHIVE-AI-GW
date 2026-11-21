//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkBuddyModelPlazaConfiguredPlatform(t *testing.T) {
	svc := newPlazaService(nil, []Group{{ID: 10, Platform: PlatformWorkBuddy, AccountType: AccountTypeOAuth}}, nil)
	groups, err := svc.ListPublicCatalogGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	models := catalogGroupModels(groups)[10]
	for _, id := range []string{"hy4-preview-f", "gpt-5.5", "deepseek-v4.1-flash", "default-model"} {
		m, ok := models[PlazaModelIdentity{PlatformWorkBuddy, id}]
		require.True(t, ok, id)
		require.True(t, m.CatalogOnly)
		require.Nil(t, m.Pricing)
	}
}
