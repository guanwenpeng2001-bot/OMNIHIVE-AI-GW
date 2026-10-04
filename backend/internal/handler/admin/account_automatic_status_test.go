//go:build unit

package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type automaticStatusAdmin struct {
	*stubAdminService
	fresh *service.Account
	err   error
	reads int
}

func (admin *automaticStatusAdmin) GetAccount(context.Context, int64) (*service.Account, error) {
	admin.reads++
	return admin.fresh, admin.err
}

func TestAutomaticAccountProbesRespectStatus(t *testing.T) {
	for _, kind := range []string{"catalog", "responses", "grok"} {
		for _, state := range []string{"inactive", "error", "queued-inactive", "queued-error", "read-failure", "missing"} {
			t.Run(kind+"/"+state, func(t *testing.T) {
				scheduler := newGrokImportProbeScheduler(1, time.Second)
				oldOpenAI, oldGrok := defaultOpenAIImportProbeScheduler, defaultGrokImportProbeScheduler
				defaultOpenAIImportProbeScheduler, defaultGrokImportProbeScheduler = scheduler, scheduler
				release, started := make(chan struct{}), make(chan struct{})
				scheduler.scheduleProbe(1, func(ctx context.Context) {
					close(started)
					select {
					case <-release:
					case <-ctx.Done():
					}
				})
				<-started
				t.Cleanup(func() {
					scheduler.close()
					require.Eventually(t, func() bool { return snapshotGrokImportProbeScheduler(scheduler).workers == 0 }, time.Second, time.Millisecond)
					defaultOpenAIImportProbeScheduler, defaultGrokImportProbeScheduler = oldOpenAI, oldGrok
				})
				repo := &combinedCatalogRepository{handlerSelectionRepository: newHandlerSelectionRepository(), committed: make(chan service.AccountModelCatalogBaseline, 1)}
				repo.account.Platform = service.PlatformOpenAI
				upstream := &syncUpstreamHTTPUpstream{err: errors.New("unexpected upstream")}
				prober := newGrokImportProbeStub(1)
				account := repo.account
				if kind == "grok" {
					account.Platform, account.Type = service.PlatformGrok, service.AccountTypeOAuth
				}
				admin := &automaticStatusAdmin{stubAdminService: newStubAdminService(), fresh: &account}
				handler := &AccountHandler{adminService: admin, grokImportProber: prober, accountTestService: service.NewAccountTestService(repo, nil, nil, nil, nil, upstream, nil, nil)}
				if state == "inactive" || state == "error" {
					account.Status = state
				}
				switch kind {
				case "catalog":
					handler.scheduleModelCatalogApply(&account, true)
				case "responses":
					handler.scheduleOpenAIResponsesProbe(&account)
				case "grok":
					handler.scheduleGrokImportProbe(&account)
				}
				queued := state != "inactive" && state != "error"
				if queued {
					require.Equal(t, 1, snapshotGrokImportProbeScheduler(scheduler).queued)
				} else {
					require.Zero(t, snapshotGrokImportProbeScheduler(scheduler).queued)
				}
				switch state {
				case "queued-inactive":
					account.Status = "inactive"
				case "queued-error":
					account.Status = service.StatusError
				case "read-failure":
					admin.err = errors.New("read unavailable")
				case "missing":
					admin.fresh = nil
				}
				close(release)
				require.Eventually(t, func() bool { return snapshotGrokImportProbeScheduler(scheduler).workers == 0 }, time.Second, time.Millisecond)
				if queued {
					require.Equal(t, 1, admin.reads)
				} else {
					require.Zero(t, admin.reads)
				}
				require.Empty(t, upstream.requests)
				require.False(t, repo.probed)
				require.Empty(t, repo.committed)
				calls, _, _ := prober.snapshot()
				require.Empty(t, calls)
			})
		}
	}
}

func TestManualAccountTestStillProbesInactiveAccount(t *testing.T) {
	repo := newHandlerSelectionRepository()
	repo.account.Status = "inactive"
	repo.account.Extra["synthetic_ui_test"] = true
	handler := &AccountHandler{accountTestService: service.NewAccountTestService(repo, nil, nil, nil, nil, nil, nil, nil)}
	router := gin.New()
	router.POST("/accounts/:id/test", handler.Test)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/accounts/42/test", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"type":"test_complete"`)
	require.Contains(t, recorder.Body.String(), `"success":true`)
}
