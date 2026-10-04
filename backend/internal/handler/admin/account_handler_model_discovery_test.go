package admin

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testassert"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokImportProbeSchedulerTestSnapshot struct {
	queued     int
	workers    int
	maxWorkers int
}

func snapshotGrokImportProbeScheduler(s *grokImportProbeScheduler) grokImportProbeSchedulerTestSnapshot {
	if s == nil {
		return grokImportProbeSchedulerTestSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return grokImportProbeSchedulerTestSnapshot{
		queued:     len(s.queue),
		workers:    s.workers,
		maxWorkers: s.maxWorkers,
	}
}

type backgroundCatalogAdmin struct {
	*stubAdminService
	account         service.Account
	started         chan context.Context
	release         chan struct{}
	requestContext  context.Context
	foregroundReads int
}

func (admin *backgroundCatalogAdmin) CreateAccount(context.Context, *service.CreateAccountInput) (*service.Account, error) {
	account := admin.account
	return &account, nil
}
func (admin *backgroundCatalogAdmin) UpdateAccount(context.Context, int64, *service.UpdateAccountInput) (*service.Account, error) {
	account := admin.account
	return &account, nil
}
func (admin *backgroundCatalogAdmin) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	// PUT may read the stored identity for credential validation before saving.
	// Only the independently scheduled discovery read waits on the barrier.
	if ctx == admin.requestContext {
		admin.foregroundReads++
		account := admin.account
		return &account, nil
	}
	select {
	case admin.started <- ctx:
	default:
	}
	select {
	case <-admin.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Return a freshly read row, rather than the object used by the save handler.
	account := admin.account
	account.Platform = "no-discovery-adapter"
	return &account, nil
}

type backgroundCatalogCommit struct {
	workspace int64
	snapshot  *service.AccountModelCatalogSnapshot
	state     service.AccountModelCatalogState
}
type backgroundCatalogRepository struct {
	service.AccountRepository
	committed chan backgroundCatalogCommit
}

func (repo *backgroundCatalogRepository) CommitAccountModelCatalog(ctx context.Context, _ int64, _ service.AccountModelCatalogBaseline, snapshot *service.AccountModelCatalogSnapshot, state service.AccountModelCatalogState, _ *service.UpstreamModelMetadataSnapshot, _ bool) error {
	workspace, _ := domain.WorkspaceScope(ctx)
	repo.committed <- backgroundCatalogCommit{workspace: workspace, snapshot: snapshot, state: state}
	return nil
}
func TestModelCatalogBackgroundSaveDoesNotWaitForDiscovery(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			scheduler := newGrokImportProbeScheduler(1, 5*time.Second)
			previous := defaultOpenAIImportProbeScheduler
			defaultOpenAIImportProbeScheduler = scheduler
			defer func() { scheduler.close(); defaultOpenAIImportProbeScheduler = previous }()
			account := service.Account{ID: 42, WorkspaceID: 2, Name: "saved", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{"model_mapping": map[string]string{"alias": "Private-ID"}}, Extra: map[string]any{service.UpstreamModelAllowlistExtraKey: service.AccountModelAllowlist{Enabled: true, Models: []string{"Private-ID"}}}}
			admin := &backgroundCatalogAdmin{stubAdminService: newStubAdminService(), account: account, started: make(chan context.Context, 1), release: make(chan struct{})}
			repo := &backgroundCatalogRepository{committed: make(chan backgroundCatalogCommit, 1)}
			handler := &AccountHandler{adminService: admin, accountTestService: service.NewAccountTestService(repo, nil, nil, nil, nil, &syncUpstreamHTTPUpstream{}, nil, nil)}
			router := gin.New()
			router.POST("/accounts", handler.Create)
			router.PUT("/accounts/:id", handler.Update)
			path := "/accounts"
			if method == http.MethodPut {
				path += "/42"
			}
			request := httptest.NewRequest(method, path, strings.NewReader(`{"name":"saved","platform":"anthropic","type":"apikey","credentials":{"model_mapping":{"alias":"Private-ID"}}}`))
			request.Header.Set("Content-Type", "application/json")
			requestContext, cancel := context.WithCancel(domain.WithWorkspaceScope(request.Context(), 2))
			defer cancel()
			request = request.WithContext(requestContext)
			admin.requestContext = requestContext
			recorder := httptest.NewRecorder()
			returned := make(chan struct{})
			var releaseOnce sync.Once
			releaseDiscovery := func() { releaseOnce.Do(func() { close(admin.release) }) }
			defer func() {
				releaseDiscovery()
				select {
				case <-returned:
				case <-time.After(time.Second):
					t.Error("save did not finish after releasing discovery")
				}
				require.Eventually(t, func() bool { return snapshotGrokImportProbeScheduler(scheduler).workers == 0 }, time.Second, time.Millisecond)
			}()
			go func() { router.ServeHTTP(recorder, request); close(returned) }()
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("save waited for background discovery")
			}
			require.Equal(t, 200, recorder.Code, recorder.Body.String())
			expectedForegroundReads := 0
			if method == http.MethodPut && len(domain.CompatibleProviders()) > 0 {
				expectedForegroundReads = 1
			}
			require.Equal(t, expectedForegroundReads, admin.foregroundReads, "credential validation uses the request context without waiting for discovery")
			var backgroundContext context.Context
			select {
			case backgroundContext = <-admin.started:
			case <-time.After(time.Second):
				t.Fatal("background task was not scheduled")
			}
			select {
			case <-repo.committed:
				t.Fatal("discovery committed before its read barrier was released")
			default:
			}
			cancel()
			require.NoError(t, backgroundContext.Err(), "request cancellation must not cancel the scheduled task")
			workspace, scoped := domain.WorkspaceScope(backgroundContext)
			require.True(t, scoped)
			require.EqualValues(t, 2, workspace)
			releaseDiscovery()
			select {
			case commit := <-repo.committed:
				require.EqualValues(t, 2, commit.workspace)
				require.Nil(t, commit.snapshot, "failed discovery must only patch state")
				require.Equal(t, "failed", commit.state.Result)
				require.Equal(t, "upstream_model_sync_not_adapted", commit.state.Error, "Apply must use the freshly loaded platform")
			case <-time.After(time.Second):
				t.Fatal("discovery did not record failure")
			}
			require.Equal(t, "Private-ID", admin.account.GetModelMapping()["alias"])
			require.Equal(t, []string{"Private-ID"}, testassert.Type[service.AccountModelAllowlist](t, admin.account.Extra[service.UpstreamModelAllowlistExtraKey]).Models)
			require.Eventually(t, func() bool { scheduler.mu.Lock(); defer scheduler.mu.Unlock(); return scheduler.workers == 0 }, time.Second, time.Millisecond)
		})
	}
}
func TestModelCatalogBackgroundGuards(t *testing.T) {
	handler := &AccountHandler{}
	handler.scheduleModelCatalogApply(nil, true)
	handler.scheduleModelCatalogApply(&service.Account{ID: 42}, true)
}

// CN Responses detection is local, so this exercises the shared task without a
// real upstream request. Its revision change must be reread before Apply.
type combinedCatalogRepository struct {
	*handlerSelectionRepository
	probed    bool
	committed chan service.AccountModelCatalogBaseline
}

func (repo *combinedCatalogRepository) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if _, err := repo.GetByID(ctx, id); err != nil {
		return err
	}
	repo.probed = true
	for key, value := range updates {
		repo.account.Extra[key] = value
	}
	state := repo.account.ModelCatalogState()
	state.RulesRevision++
	repo.account.Extra[service.UpstreamModelCatalogStateExtraKey] = state
	return nil
}
func (repo *combinedCatalogRepository) CommitAccountModelCatalog(_ context.Context, _ int64, baseline service.AccountModelCatalogBaseline, _ *service.AccountModelCatalogSnapshot, _ service.AccountModelCatalogState, _ *service.UpstreamModelMetadataSnapshot, _ bool) error {
	repo.committed <- baseline
	return nil
}
func TestModelCatalogBackgroundCombinesProbeAndDiscovery(t *testing.T) {
	scheduler := newGrokImportProbeScheduler(1, time.Second)
	previous := defaultOpenAIImportProbeScheduler
	defaultOpenAIImportProbeScheduler = scheduler
	defer func() { scheduler.close(); defaultOpenAIImportProbeScheduler = previous }()
	repo := &combinedCatalogRepository{handlerSelectionRepository: newHandlerSelectionRepository(), committed: make(chan service.AccountModelCatalogBaseline, 1)}
	repo.account.Platform = service.PlatformZhipu
	admin := service.NewAdminService(&config.Config{}, nil, nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler := &AccountHandler{adminService: admin, accountTestService: service.NewAccountTestService(repo, nil, nil, nil, nil, nil, nil, nil)}
	handler.scheduleModelCatalogApply(&repo.account, true)
	select {
	case baseline := <-repo.committed:
		require.EqualValues(t, 4, baseline.RulesRevision)
	case <-time.After(time.Second):
		t.Fatal("catalog task was lost behind the Responses probe")
	}
	require.Eventually(t, func() bool { return snapshotGrokImportProbeScheduler(scheduler).workers == 0 }, time.Second, time.Millisecond)
	require.True(t, repo.probed)
}
func TestModelCatalogBackgroundUsesBoundedQueue(t *testing.T) {
	scheduler := newGrokImportProbeScheduler(1, time.Minute)
	previous := defaultOpenAIImportProbeScheduler
	defaultOpenAIImportProbeScheduler = scheduler
	defer func() { scheduler.close(); defaultOpenAIImportProbeScheduler = previous }()
	started := make(chan struct{})
	scheduler.scheduleProbe(1, func(ctx context.Context) { close(started); <-ctx.Done() })
	<-started
	handler := &AccountHandler{adminService: newStubAdminService(), accountTestService: &service.AccountTestService{}}
	for accountID := int64(2); accountID < 1000; accountID++ {
		handler.scheduleModelCatalogApply(&service.Account{ID: accountID, WorkspaceID: 2, Status: service.StatusActive}, false)
	}
	snapshot := snapshotGrokImportProbeScheduler(scheduler)
	require.Equal(t, grokImportProbeQueueLimit, snapshot.queued)
	require.Equal(t, 1, snapshot.workers)
	scheduler.close()
	require.Eventually(t, func() bool { return snapshotGrokImportProbeScheduler(scheduler).workers == 0 }, time.Second, time.Millisecond)
}
