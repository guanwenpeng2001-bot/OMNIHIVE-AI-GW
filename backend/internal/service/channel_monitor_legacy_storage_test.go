package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type legacyMonitorRepository struct {
	ChannelMonitorRepository
	monitors []*ChannelMonitor
}

func (repo legacyMonitorRepository) GetByID(context.Context, int64) (*ChannelMonitor, error) {
	return repo.monitors[0], nil
}

func (repo legacyMonitorRepository) ListEnabled(context.Context) ([]*ChannelMonitor, error) {
	return repo.monitors, nil
}

type legacyMonitorRuntime struct{}

func (legacyMonitorRuntime) GetChannelMonitorRuntime(context.Context) ChannelMonitorRuntime {
	return ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV1}
}

func TestLegacyCursorMonitorNeverExecutes(t *testing.T) {
	for _, mode := range []string{MonitorCheckModeProbe, MonitorCheckModeQuota, MonitorCheckModeQuotaProbe} {
		t.Run(mode, func(t *testing.T) {
			monitor := &ChannelMonitor{ID: 1, Provider: PlatformCursor, Enabled: true, CheckMode: mode}
			service := NewChannelMonitorService(legacyMonitorRepository{monitors: []*ChannelMonitor{monitor}}, nil)
			service.SetRuntimeReader(legacyMonitorRuntime{})
			// All outbound/checker/persistence dependencies are nil: reaching any
			// of them would panic instead of satisfying this error assertion.
			results, err := service.RunCheck(context.Background(), 1)
			require.ErrorIs(t, err, ErrChannelMonitorInvalidProvider)
			require.Nil(t, results)
			require.True(t, monitor.Enabled, "runtime refusal must not rewrite stored flags")
		})
	}
}

func TestLegacyCursorMonitorNotScheduledAndStillReadable(t *testing.T) {
	legacy := &ChannelMonitor{ID: 1, Provider: PlatformCursor, Enabled: true}
	supported := &ChannelMonitor{ID: 2, Provider: MonitorProviderOpenAI, Enabled: true}
	service := NewChannelMonitorService(legacyMonitorRepository{monitors: []*ChannelMonitor{legacy, supported}}, nil)
	runnable, err := service.ListEnabledMonitors(context.Background())
	require.NoError(t, err)
	require.Equal(t, []*ChannelMonitor{supported}, runnable)
	stored, err := service.Get(context.Background(), 1)
	require.NoError(t, err)
	require.Same(t, legacy, stored)
	require.True(t, stored.Enabled)
}

func TestLegacyCursorMonitorAndTemplateAreReadOnly(t *testing.T) {
	provider := MonitorProviderOpenAI
	monitor := &ChannelMonitor{Provider: PlatformCursor, Name: "historical", Enabled: true}
	require.ErrorIs(t, applyMonitorUpdate(monitor, ChannelMonitorUpdateParams{Provider: &provider}), ErrChannelMonitorInvalidProvider)
	require.Equal(t, PlatformCursor, monitor.Provider)
	template := &ChannelMonitorRequestTemplate{Provider: PlatformCursor, Name: "historical"}
	name := "replacement"
	require.ErrorIs(t, applyTemplateUpdate(template, ChannelMonitorRequestTemplateUpdateParams{Name: &name}), ErrChannelMonitorTemplateInvalidProvider)
	require.Equal(t, "historical", template.Name)
}
