//go:build unit

package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkBuddyMonitorRejectsRedirects(t *testing.T) {
	swapMonitorHTTPClient(t)
	for _, code := range []int{302, 307, 308} {
		var reached atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
		defer target.Close()
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NotEmpty(t, r.Header.Get("X-API-Key"))
			http.Redirect(w, r, target.URL, code)
		}))
		defer origin.Close()
		result := runCheckForModel(t.Context(), PlatformWorkBuddy, origin.URL, "monitor-key", "auto", nil)
		require.Equal(t, MonitorStatusError, result.Status)
		require.Zero(t, reached.Load())
	}
}

func TestNewPlatformsMonitorTemplateProtections(t *testing.T) {
	for _, platform := range []string{PlatformWorkBuddy, PlatformKilo} {
		t.Run(platform, func(t *testing.T) {
			capture := &openAICaptureHandler{}
			endpoint := setupFakeOpenAI(t, capture)
			// Return a rejection after receipt so both wire formats can share a recorder.
			capture.status = http.StatusBadRequest
			opts := &CheckOptions{BodyOverrideMode: MonitorBodyOverrideModeMerge, BodyOverride: map[string]any{"model": "replaced", "messages": []any{}, "stream": platform != PlatformWorkBuddy, "temperature": 0.3}}
			_, _, status, err := callProvider(t.Context(), platform, endpoint, "fixture-key", "original-model", "challenge", opts)
			require.NoError(t, err)
			require.Equal(t, http.StatusBadRequest, status)
			require.Equal(t, "original-model", capture.lastBody["model"])
			messages, err := json.Marshal(capture.lastBody["messages"])
			require.NoError(t, err)
			require.Contains(t, string(messages), "challenge")
			require.Equal(t, platform == PlatformWorkBuddy, capture.lastBody["stream"])
			require.Equal(t, 0.3, capture.lastBody["temperature"])
			for _, body := range []map[string]any{{"model": "x"}, {"messages": []any{}}, {"messages": ""}} {
				err = validateBodyModeForProtocol(platform, "", MonitorBodyOverrideModeReplace, body)
				require.Error(t, err)
			}
			err = validateBodyModeForProtocol(platform, "", MonitorBodyOverrideModeReplace, map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}})
			require.NoError(t, err)
		})
	}
}

func TestKiloKeylessMonitorLifecycle(t *testing.T) {
	ctx := t.Context()
	copies := &duplicateChannelMonitorRepoStub{}
	create := NewChannelMonitorService(copies, &duplicateChannelMonitorEncryptor{})
	params := ChannelMonitorCreateParams{Name: "free", Provider: PlatformKilo, Endpoint: "https://8.8.8.8", IntervalSeconds: 90, PrimaryModel: "kilo-auto/free"}
	created, err := create.Create(ctx, params)
	require.NoError(t, err)
	require.Empty(t, created.APIKey)
	copies.source = copies.created[0]
	clone, err := create.Duplicate(ctx, created.ID, 1, "admin:1", "copy")
	require.NoError(t, err)
	require.Empty(t, clone.APIKey)

	capture := &openAICaptureHandler{}
	stored := copies.created[1]
	stored.Endpoint = setupFakeOpenAI(t, capture)
	repo := &quotaModeRepoStub{monitor: stored}
	runner := newQuotaModeService(repo)
	_, err = runner.Update(ctx, clone.ID, ChannelMonitorUpdateParams{})
	require.NoError(t, err)
	paid := "paid/model"
	_, err = runner.Update(ctx, clone.ID, ChannelMonitorUpdateParams{PrimaryModel: &paid})
	require.ErrorIs(t, err, ErrChannelMonitorMissingAPIKey)
	results, err := runner.RunCheck(ctx, clone.ID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, MonitorStatusOperational, results[0].Status)
	require.Empty(t, capture.lastHeaders.Get("Authorization"))
	require.Equal(t, params.PrimaryModel, capture.lastBody["model"])
	for _, provider := range []string{PlatformWorkBuddy, PlatformOpenAI, PlatformKilo} {
		params.Provider = provider
		params.PrimaryModel = paid
		require.ErrorIs(t, validateCreateParams(params), ErrChannelMonitorMissingAPIKey)
		params.APIKey = "private-key"
		require.NoError(t, validateCreateParams(params))
		params.APIKey = ""
	}
	params.PrimaryModel = "kilo-auto/free"
	params.ExtraModels = []string{paid}
	require.ErrorIs(t, validateCreateParams(params), ErrChannelMonitorMissingAPIKey)
}
