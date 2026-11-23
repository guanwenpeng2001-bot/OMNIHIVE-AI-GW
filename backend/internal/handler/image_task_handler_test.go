package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alitto/pond/v2"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type asyncImageMemoryStore struct {
	mu    sync.RWMutex
	tasks map[string]*service.ImageTaskRecord
}

func (s *asyncImageMemoryStore) Save(_ context.Context, task *service.ImageTaskRecord, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *task
	copy.Result = append(json.RawMessage(nil), task.Result...)
	copy.Error = append(json.RawMessage(nil), task.Error...)
	s.tasks[task.ID] = &copy
	return nil
}

func (s *asyncImageMemoryStore) Get(_ context.Context, id string) (*service.ImageTaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task := s.tasks[id]
	if task == nil {
		return nil, service.ErrImageTaskNotFound
	}
	copy := *task
	copy.Result = append(json.RawMessage(nil), task.Result...)
	copy.Error = append(json.RawMessage(nil), task.Error...)
	return &copy, nil
}

func TestAsyncImageHandlerSubmitAndPoll(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		for _, path := range []string{"/v1/images/generations/async", "/v1/images/edits/async", "/images/generations/async", "/images/edits/async"} {
			t.Run(platform+path, func(t *testing.T) { testAsyncImageSubmitAndPoll(t, platform, path) })
		}
	}
}

func testAsyncImageSubmitAndPoll(t *testing.T, platform, path string) {
	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	release := make(chan struct{})
	attemptErrors := make(chan error, 2)
	contexts := make(chan context.Context, 1)
	var submissionBudget *service.AttemptBudget
	cfg := &config.Config{}
	h := &AsyncImageHandler{tasks: tasks}
	h.openAI = &OpenAIGatewayHandler{gatewayService: service.ProvideOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))}
	h.execute = func(_ string, c *gin.Context) {
		<-release
		defer installAttemptBudget(c)()
		ctx := c.Request.Context()
		contexts <- ctx
		finish, err := service.BeginGenerationAttempt(ctx, 42)
		attemptErrors <- err
		if err != nil {
			c.Status(http.StatusBadGateway)
			return
		}
		finish(http.StatusOK, nil)
		// Neither adapter re-entry nor a detached retry may renew the task budget.
		retryCtx, cancel := service.WithAttemptBudget(context.WithoutCancel(ctx), 99, time.Hour)
		defer cancel()
		_, err = service.BeginGenerationAttempt(retryCtx, 42)
		attemptErrors <- err
		c.JSON(http.StatusOK, gin.H{"created": 123, "data": []gin.H{{"url": "https://example.test/image.png"}}})
	}

	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(nil))
	router.Use(func(c *gin.Context) {
		submissionBudget = service.AttemptBudgetFromContext(c.Request.Context())
		groupID := int64(3)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{WorkspaceID: 1,
			ID:      9,
			UserID:  7,
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: platform, AllowImageGeneration: true},
		})
		c.Next()
	})
	router.POST(path, h.Submit)
	router.GET("/v1/images/tasks/:task_id", h.Get)
	router.GET("/images/tasks/:task_id", h.Get)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-image-1","prompt":"cat","images":[{"image_url":"https://example.test/cat.png"}]}`)).WithContext(requestCtx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "3", w.Header().Get("Retry-After"))

	var accepted struct {
		TaskID  string `json:"task_id"`
		Status  string `json:"status"`
		PollURL string `json:"poll_url"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &accepted))
	require.Equal(t, service.ImageTaskStatusProcessing, accepted.Status)
	require.Equal(t, imageTaskPollURL(path, accepted.TaskID), accepted.PollURL)
	require.Equal(t, accepted.PollURL, w.Header().Get("Location"))

	// The detached background request must survive completion of/cancellation
	// from the short submission request.
	cancelRequest()
	close(release)
	require.Eventually(t, func() bool {
		got, err := tasks.Get(context.Background(), service.ImageTaskOwner{WorkspaceID: 1, UserID: 7, APIKeyID: 9}, accepted.TaskID)
		return err == nil && got.Status == service.ImageTaskStatusCompleted
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, <-attemptErrors)
	require.ErrorIs(t, <-attemptErrors, service.ErrAttemptBudgetExhausted)
	taskCtx := <-contexts
	taskBudget := service.AttemptBudgetFromContext(taskCtx)
	require.NotSame(t, submissionBudget, taskBudget)
	require.Len(t, taskBudget.Snapshot(), 1)
	require.Empty(t, submissionBudget.Snapshot())
	plan := service.GatewayRequestPlanFromContext(taskCtx)
	require.NotNil(t, plan)
	require.Equal(t, accepted.TaskID, plan.Snapshot().ExecutionID)
	require.Equal(t, "job", plan.Snapshot().ExecutionKind)
	require.Equal(t, "gpt-image-1", plan.Snapshot().Model)
	require.Equal(t, strings.TrimSuffix(path, "/async"), plan.Snapshot().Route)
	// run owns cancellation; detached transports cannot escape it after completion.
	require.Eventually(t, func() bool { return taskCtx.Err() != nil }, time.Second, time.Millisecond)
	_, err := service.BeginGenerationAttempt(context.WithoutCancel(taskCtx), 42)
	require.ErrorIs(t, err, context.Canceled)

	pollReq := httptest.NewRequest(http.MethodGet, accepted.PollURL, nil)
	pollWriter := httptest.NewRecorder()
	router.ServeHTTP(pollWriter, pollReq)
	require.Equal(t, http.StatusOK, pollWriter.Code)
	require.Equal(t, "no-store", pollWriter.Header().Get("Cache-Control"))
	require.Empty(t, pollWriter.Header().Get("Retry-After"))
	require.Contains(t, pollWriter.Body.String(), "https://example.test/image.png")
}

// When object storage is not configured the feature is fully disabled: the
// endpoints must return 404 without creating a task or writing to Redis.
func TestAsyncImageHandlerDisabledReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithOptions(store, time.Hour, time.Minute) // enabled == false
	h := &AsyncImageHandler{tasks: tasks}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(3)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{WorkspaceID: 1,
			ID:      9,
			UserID:  7,
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
		})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)
	router.GET("/v1/images/tasks/:task_id", h.Get)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-1","prompt":"cat"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "not enabled")

	pollReq := httptest.NewRequest(http.MethodGet, "/v1/images/tasks/imgtask_missing", nil)
	pollWriter := httptest.NewRecorder()
	router.ServeHTTP(pollWriter, pollReq)
	require.Equal(t, http.StatusNotFound, pollWriter.Code)

	// No task was created / persisted.
	require.Empty(t, store.tasks)
}

func TestAsyncImageHandlerTaskDeadlineBlocksDetachedSend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, 100*time.Millisecond)
	release := make(chan struct{})
	result := make(chan error, 1)
	h := &AsyncImageHandler{tasks: tasks}
	h.execute = func(_ string, c *gin.Context) {
		<-release
		defer installAttemptBudget(c)()
		ctx := c.Request.Context()
		<-ctx.Done()
		// Like streaming transports, this context has no direct cancellation;
		// the task root must still prevent an upstream send.
		detached := context.WithoutCancel(ctx)
		_, err := service.BeginGenerationAttempt(detached, 42)
		result <- err
	}
	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(nil), func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{WorkspaceID: 1,
			ID: 9, UserID: 7, Group: &service.Group{Platform: service.PlatformOpenAI, AllowImageGeneration: true},
		})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-1","prompt":"cat"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)
	close(release)
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(3 * time.Second):
		t.Fatal("task deadline did not stop execution")
	}
	var accepted service.ImageTask
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &accepted))
	require.Eventually(t, func() bool {
		task, err := tasks.Get(context.Background(), service.ImageTaskOwner{WorkspaceID: 1, UserID: 7, APIKeyID: 9}, accepted.ID)
		return err == nil && task.Status == service.ImageTaskStatusFailed
	}, time.Second, time.Millisecond)
}

// A full execution pool must reject the submission: no 202 is returned and the
// admitted task receipt is finished with an explicit queue_full error instead
// of being left processing forever (BE-unbounded-async-image-goroutines).
func TestAsyncImageHandlerQueueFullReturns503Not202(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	h := &AsyncImageHandler{tasks: tasks}
	h.execute = func(_ string, c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"created": 123, "data": []gin.H{{"url": "https://example.test/image.png"}}})
	}
	// One worker, no queue slots: occupying the worker makes the next
	// TrySubmit fail deterministically.
	pool := pond.NewPool(1, pond.WithQueueSize(0))
	workerStarted := make(chan struct{})
	releaseWorker := make(chan struct{})
	pool.Submit(func() {
		close(workerStarted)
		<-releaseWorker
	})
	<-workerStarted
	h.pool = pool
	t.Cleanup(func() {
		close(releaseWorker)
		pool.StopAndWait()
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(3)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{WorkspaceID: 1,
			ID:      9,
			UserID:  7,
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
		})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-1","prompt":"cat"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.NotEqual(t, http.StatusAccepted, w.Code, "enqueue failure must not be acknowledged as accepted")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	// The created receipt is settled as failed exactly once, never left pending.
	require.Len(t, store.tasks, 1)
	for _, record := range store.tasks {
		require.Equal(t, service.ImageTaskStatusFailed, record.Status)
		require.Equal(t, http.StatusServiceUnavailable, record.HTTPStatus)
		require.Contains(t, string(record.Error), "queue_full")
	}
}

func TestAsyncImageDrainRecordsUnknownForUnfinishedJobs(t *testing.T) {
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	handler := NewAsyncImageHandler(tasks, nil)
	owner := service.ImageTaskOwner{WorkspaceID: 1, UserID: 2, APIKeyID: 3}
	task, err := tasks.Create(t.Context(), owner)
	require.NoError(t, err)

	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	started := make(chan struct{})
	require.True(t, handler.submitTask(task.ID, cancel, func() {
		close(started)
		<-ctx.Done()
	}))
	<-started
	drained := handler.BeginDrain()
	require.False(t, handler.submitTask("new-job", cancel, func() {}))
	select {
	case <-drained:
		t.Fatal("drain returned before the active job exited")
	default:
	}

	handler.ExpireDrain(t.Context())
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("canceled job did not drain")
	}
	require.ErrorIs(t, context.Cause(ctx), errImageTaskShutdown)
	result, err := tasks.Get(t.Context(), owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusFailed, result.Status)
	require.Contains(t, string(result.Error), "execution_outcome_unknown")
}
