//go:build unit

package admin

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSiteICPSettingsRoundTrip(t *testing.T) {
	handler, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySiteName: "Example Gateway",
	})
	for _, number := range []string{"蒙ICP备2026xxxx号", ""} {
		recorder := doUpdateSettings(t, handler, map[string]any{"site_icp": number}, nil)
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, number, repo.values[service.SettingKeySiteICP])
		require.Contains(t, recorder.Body.String(), `"site_icp":"`+number+`"`)

		settings, err := handler.settingService.GetAllSettings(context.Background())
		require.NoError(t, err)
		require.Equal(t, number, settings.SiteICP)
		public, err := handler.settingService.GetPublicSettings(context.Background())
		require.NoError(t, err)
		require.Equal(t, number, public.SiteICP)
		injected, err := handler.settingService.GetPublicSettingsForInjection(context.Background())
		require.NoError(t, err)
		require.Equal(t, number, injected.(*service.PublicSettingsInjectionPayload).SiteICP)

		recorder = doUpdateSettings(t, handler, map[string]any{"site_name": "Example Gateway"}, nil)
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, number, repo.values[service.SettingKeySiteICP])
	}
}
