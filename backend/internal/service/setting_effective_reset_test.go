//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResetConfigOverridesReloadsOpsAfterCommit(t *testing.T) {
	repo := &registrySettingRepoStub{values: map[string]string{
		SettingKeyOpsAdvancedSettings:  `{"advanced":true}`,
		SettingKeyOpsRuntimeLogConfig: `{"level":"warn"}`,
	}}
	svc := registryService(repo)
	keys := []string{SettingKeyOpsAdvancedSettings}
	_, err := svc.ResetConfigOverrides(t.Context(), keys, SettingResetHooks{})
	require.ErrorIs(t, err, ErrSettingResetUnavailable)
	require.Empty(t, repo.deleteKeys)
	reloaded := false
	_, err = svc.ResetConfigOverrides(t.Context(), keys, SettingResetHooks{Refresh: func(context.Context) error {
		require.NotContains(t, repo.values, SettingKeyOpsAdvancedSettings)
		reloaded = true
		return nil
	}})
	require.NoError(t, err)
	require.True(t, reloaded)
	require.Equal(t, keys, repo.deleteKeys)
	require.Contains(t, repo.values, SettingKeyOpsRuntimeLogConfig)
}
