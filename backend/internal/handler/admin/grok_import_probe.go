package admin

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

const (
	grokImportProbeConcurrency = 3
	grokImportProbeTimeout     = 25 * time.Second
	grokImportProbeQueueLimit  = 64
)

type grokImportProber interface {
	QueryQuota(ctx context.Context, accountID int64) (*service.GrokQuotaProbeResult, error)
}

type grokImportProbeTask struct {
	execute   func(context.Context)
	accountID int64
}

type grokImportProbeScheduler struct {
	mu          sync.Mutex
	queue       []grokImportProbeTask
	pending     map[int64]struct{}
	inFlight    map[int64]struct{}
	concurrency int
	workers     int
	maxWorkers  int
	timeout     time.Duration
	context     context.Context
	cancel      context.CancelFunc
	closed      bool
}

var defaultGrokImportProbeScheduler = newGrokImportProbeScheduler(
	grokImportProbeConcurrency,
	grokImportProbeTimeout,
)

func newGrokImportProbeScheduler(concurrency int, timeout time.Duration) *grokImportProbeScheduler {
	if concurrency <= 0 {
		concurrency = 1
	}
	if timeout <= 0 {
		timeout = grokImportProbeTimeout
	}
	schedulerContext, cancel := context.WithCancel(context.Background())
	return &grokImportProbeScheduler{
		context: schedulerContext, cancel: cancel,
		concurrency: concurrency,
		timeout:     timeout,
		pending:     make(map[int64]struct{}),
		inFlight:    make(map[int64]struct{}),
	}
}

func (s *grokImportProbeScheduler) schedule(prober grokImportProber, admin service.AdminService, account *service.Account) {
	if s == nil || prober == nil || admin == nil || account == nil || account.ID <= 0 || account.Status != service.StatusActive {
		return
	}
	if account.Platform != service.PlatformGrok || account.Type != service.AccountTypeOAuth {
		return
	}

	s.scheduleTask(prober, admin, account)
}

func (s *grokImportProbeScheduler) scheduleTask(prober grokImportProber, admin service.AdminService, account *service.Account) {
	accountID, workspaceID := account.ID, account.WorkspaceID

	s.scheduleProbe(accountID, func(ctx context.Context) {
		if workspaceID <= 0 {
			fresh, err := admin.GetAccount(ctx, accountID)
			if err != nil || fresh == nil || fresh.ID != accountID || fresh.WorkspaceID <= 0 {
				return
			}
			workspaceID = fresh.WorkspaceID
		}
		ctx = domain.WithWorkspaceScope(ctx, workspaceID)
		runGrokImportProbe(ctx, prober, admin, accountID)
	})
}

func (s *grokImportProbeScheduler) scheduleProbe(accountID int64, execute func(context.Context)) {
	if s == nil || accountID <= 0 || execute == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if _, exists := s.pending[accountID]; exists {
		s.mu.Unlock()
		return
	}
	if _, exists := s.inFlight[accountID]; exists {
		s.mu.Unlock()
		return
	}
	if len(s.queue) >= grokImportProbeQueueLimit {
		s.mu.Unlock()
		slog.Debug("account_import_active_probe_dropped", "account_id", accountID, "reason", "queue_full")
		return
	}
	s.queue = append(s.queue, grokImportProbeTask{execute: execute, accountID: accountID})
	s.pending[accountID] = struct{}{}
	if s.workers < s.concurrency {
		s.workers++
		if s.workers > s.maxWorkers {
			s.maxWorkers = s.workers
		}
		go s.worker()
	}
	s.mu.Unlock()
}

func (s *grokImportProbeScheduler) worker() {
	for {
		task, ok := s.nextTask()
		if !ok {
			return
		}
		s.run(task)
		s.finish(task.accountID)
	}
}

func (s *grokImportProbeScheduler) nextTask() (grokImportProbeTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		s.workers--
		return grokImportProbeTask{}, false
	}
	task := s.queue[0]
	s.queue[0] = grokImportProbeTask{}
	s.queue = s.queue[1:]
	if len(s.queue) == 0 {
		s.queue = nil
	}
	delete(s.pending, task.accountID)
	s.inFlight[task.accountID] = struct{}{}
	return task, true
}

func (s *grokImportProbeScheduler) finish(accountID int64) {
	s.mu.Lock()
	delete(s.inFlight, accountID)
	s.mu.Unlock()
}

func (s *grokImportProbeScheduler) run(task grokImportProbeTask) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error(
				"grok_import_active_probe_panic",
				"account_id", task.accountID,
				"recovery_type", panicType(recovered),
			)
		}
	}()

	ctx, cancel := context.WithTimeout(s.context, s.timeout)
	defer cancel()
	if ctx.Err() == nil {
		task.execute(ctx)
	}
}

func runGrokImportProbe(ctx context.Context, prober grokImportProber, admin service.AdminService, accountID int64) {
	fresh, err := admin.GetAccount(ctx, accountID)
	if err != nil || fresh == nil || fresh.Status != service.StatusActive || fresh.ID != accountID ||
		(fresh.WorkspaceID > 0 && fresh.WorkspaceID != domain.ResourceWorkspaceID(ctx)) {
		return
	}
	result, err := prober.QueryQuota(ctx, accountID)
	if err != nil {
		slog.Warn(
			"grok_import_active_probe_failed",
			"account_id", accountID,
			"status", infraerrors.Code(err),
			"reason", infraerrors.Reason(err),
		)
		return
	}
	if result == nil {
		slog.Warn(
			"grok_import_active_probe_failed",
			"account_id", accountID,
			"reason", "empty_result",
		)
		return
	}

	slog.Info(
		"grok_import_active_probe_completed",
		"account_id", accountID,
		"model", result.Model,
		"status", result.StatusCode,
		"headers_observed", result.HeadersObserved,
	)
}

func panicType(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case error:
		return "error"
	default:
		return "unknown"
	}
}

func (h *AccountHandler) scheduleGrokImportProbe(account *service.Account) {
	if h == nil {
		return
	}
	defaultGrokImportProbeScheduler.schedule(h.grokImportProber, h.adminService, account)
}

// ProvideAccountHandler injects the Grok active prober for production while
// keeping NewAccountHandler convenient for focused unit tests.
func ProvideAccountHandler(
	cfg *config.Config,
	adminService service.AdminService,
	oauthService *service.OAuthService,
	openaiOAuthService *service.OpenAIOAuthService,
	geminiOAuthService *service.GeminiOAuthService,
	antigravityOAuthService *service.AntigravityOAuthService,
	grokOAuthService service.GrokOAuthTokenService,
	rateLimitService *service.RateLimitService,
	accountUsageService *service.AccountUsageService,
	accountTestService *service.AccountTestService,
	concurrencyService *service.ConcurrencyService,
	crsSyncService *service.CRSSyncService,
	sessionLimitCache service.SessionLimitCache,
	rpmCache service.RPMCache,
	tokenCacheInvalidator service.TokenCacheInvalidator,
	grokQuotaService *service.GrokQuotaService,
	cursorSessions *service.CursorSessionProvider,
) *AccountHandler {
	handler := NewAccountHandler(
		adminService,
		oauthService,
		openaiOAuthService,
		geminiOAuthService,
		antigravityOAuthService,
		grokOAuthService,
		rateLimitService,
		accountUsageService,
		accountTestService,
		concurrencyService,
		crsSyncService,
		sessionLimitCache,
		rpmCache,
		tokenCacheInvalidator,
	)
	handler.grokImportProber = grokQuotaService
	handler.cursorSessions = cursorSessions
	handler.cfg = cfg
	return handler
}

// close drops queued work and cancels active probes. Workers exit when the queue empties.
func (s *grokImportProbeScheduler) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.queue = nil
	clear(s.pending)
	s.cancel()
}

var defaultOpenAIImportProbeScheduler = newGrokImportProbeScheduler(grokImportProbeConcurrency, grokImportProbeTimeout)

// StopImportProbes cancels active imports and releases queued work during shutdown.
func StopImportProbes() {
	defaultGrokImportProbeScheduler.close()
	defaultOpenAIImportProbeScheduler.close()
}
