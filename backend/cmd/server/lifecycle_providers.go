package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	process "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/runtime"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/redis/go-redis/v9"
)

func provideOpsAggregationService(s *process.Supervisor, opsRepo service.OpsRepository,
	settingRepo service.SettingRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config) *service.OpsAggregationService {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideOpsAggregationService(opsRepo, settingRepo, db, redisClient, cfg)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideOpsAlertEvaluatorService(s *process.Supervisor, opsService *service.OpsService,
	opsRepo service.OpsRepository,
	emailService *service.EmailService,
	redisClient *redis.Client,
	cfg *config.Config,
	proxyRepo service.ProxyRepository) *service.OpsAlertEvaluatorService {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideOpsAlertEvaluatorService(opsService, opsRepo, emailService, redisClient, cfg, proxyRepo)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideOpsScheduledReportService(s *process.Supervisor, opsService *service.OpsService,
	userService *service.UserService,
	emailService *service.EmailService,
	redisClient *redis.Client,
	cfg *config.Config) *service.OpsScheduledReportService {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideOpsScheduledReportService(opsService, userService, emailService, redisClient, cfg)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideAccountExpiryService(s *process.Supervisor, accountRepo service.AccountRepository) *service.AccountExpiryService {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideAccountExpiryService(accountRepo)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideCNProviderBalanceCheckService(s *process.Supervisor, accountRepo service.AccountRepository,
	balanceService *service.CNProviderBalanceService,
	quotaService *service.CNProviderQuotaService,
	cfg *config.Config) *service.CNProviderBalanceCheckService {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideCNProviderBalanceCheckService(accountRepo, balanceService, quotaService, cfg)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideOpenAICodexVersionSyncService(s *process.Supervisor, settingRepo service.SettingRepository,
	settingService *service.SettingService,
	githubClient service.GitHubReleaseClient) *service.OpenAICodexVersionSyncService {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideOpenAICodexVersionSyncService(settingRepo, settingService, githubClient)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideProxyExpiryService(s *process.Supervisor, proxyRepo service.ProxyRepository) *service.ProxyExpiryService {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideProxyExpiryService(proxyRepo)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideSubscriptionExpiryService(s *process.Supervisor, userSubRepo service.UserSubscriptionRepository, settingRepo service.SettingRepository, notificationEmailService *service.NotificationEmailService, lockCache service.LeaderLockCache, db *sql.DB) *service.SubscriptionExpiryService {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideSubscriptionExpiryService(userSubRepo, settingRepo, notificationEmailService, lockCache, db)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideBatchImageWorkerRuntime(s *process.Supervisor, repo service.BatchImageRepository,
	accountRepo service.AccountRepository,
	queue service.BatchImageQueue,
	billingRepo service.UsageBillingRepository,
	usageLogRepo service.UsageLogRepository,
	pricing *service.BatchImageModelPricingResolver,
	authCache service.APIKeyAuthCacheInvalidator,
	cfg *config.Config) *service.BatchImageWorkerRuntime {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideBatchImageWorkerRuntime(repo, accountRepo, queue, billingRepo, usageLogRepo, pricing, authCache, cfg)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideScheduledTestRunnerService(s *process.Supervisor, planRepo service.ScheduledTestPlanRepository,
	scheduledSvc *service.ScheduledTestService,
	accountTestSvc *service.AccountTestService,
	rateLimitSvc *service.RateLimitService,
	cfg *config.Config) *service.ScheduledTestRunnerService {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideScheduledTestRunnerService(planRepo, scheduledSvc, accountTestSvc, rateLimitSvc, cfg)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func providePaymentOrderExpiryService(s *process.Supervisor, paymentSvc *service.PaymentService, lockCache service.LeaderLockCache, db *sql.DB) *service.PaymentOrderExpiryService {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvidePaymentOrderExpiryService(paymentSvc, lockCache, db)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideChannelMonitorRunner(s *process.Supervisor, svc *service.ChannelMonitorService,
	settingService *service.SettingService,
	quotaFetcher *service.ChannelMonitorQuotaFetcher) *service.ChannelMonitorRunner {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideChannelMonitorRunner(svc, settingService, quotaFetcher)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideChannelTelemetryAggregator(s *process.Supervisor, repo service.ChannelTelemetryRepository, db *sql.DB, settingService *service.SettingService) *service.ChannelTelemetryAggregator {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideChannelTelemetryAggregator(repo, db, settingService)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideUserPlatformQuotaUsageFlusher(s *process.Supervisor, cfg *config.Config, cache service.BillingCache, quotaRepo service.UserPlatformQuotaRepository, tw *service.TimingWheelService) *service.UserPlatformQuotaUsageFlusher {
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			worker := service.ProvideUserPlatformQuotaUsageFlusher(cfg, cache, quotaRepo, tw)
			<-ctx.Done()
			worker.Stop()
		})
	}
	return nil
}

func provideAuthCacheInvalidationWorker(s *process.Supervisor, repo service.AuthCacheInvalidationOutboxRepository, cache service.APIKeyCache, apiKeyService *service.APIKeyService) *service.AuthCacheInvalidationWorker {
	worker := service.NewAuthCacheInvalidationWorker(repo, cache, apiKeyService)
	s.WorkerStart(worker.Start)
	return worker
}

func provideDashboardAggregationService(s *process.Supervisor, repo service.DashboardAggregationRepository, timingWheel *service.TimingWheelService, lockCache service.LeaderLockCache, db *sql.DB, cfg *config.Config) *service.DashboardAggregationService {
	svc := service.NewDashboardAggregationService(repo, timingWheel, cfg)
	svc.SetLeaderLock(lockCache, db)
	s.WorkerStart(svc.Start)
	return svc
}

func provideBackupService(s *process.Supervisor, settingRepo service.SettingRepository,
	cfg *config.Config,
	encryptor service.SecretEncryptor,
	storeFactory service.BackupObjectStoreFactory,
	dumper service.DBDumper,
	lockCache service.LeaderLockCache,
	db *sql.DB) *service.BackupService {
	svc := service.NewBackupService(settingRepo, cfg, encryptor, storeFactory, dumper)
	svc.SetLeaderLock(lockCache, db)
	s.WorkerStart(svc.Start)
	return svc
}

func provideTokenRefreshService(workBuddyOAuth *service.WorkBuddyOAuthService, s *process.Supervisor, accountRepo service.AccountRepository,
	oauthService *service.OAuthService,
	openaiOAuthService *service.OpenAIOAuthService,
	geminiOAuthService *service.GeminiOAuthService,
	antigravityOAuthService *service.AntigravityOAuthService,
	grokOAuthService *service.GrokOAuthService,
	cacheInvalidator service.TokenCacheInvalidator,
	schedulerCache service.SchedulerCache,
	cfg *config.Config,
	tempUnschedCache service.TempUnschedCache,
	privacyClientFactory service.PrivacyClientFactory,
	proxyRepo service.ProxyRepository,
	refreshAPI *service.OAuthRefreshAPI,
	runtimeBlocker service.AccountRuntimeBlocker) *service.TokenRefreshService {
	svc := service.NewTokenRefreshService(accountRepo, oauthService, openaiOAuthService, geminiOAuthService, antigravityOAuthService, cacheInvalidator, schedulerCache, cfg, tempUnschedCache, grokOAuthService)

	svc.SetWorkBuddyOAuthService(workBuddyOAuth)
	svc.SetPrivacyDeps(privacyClientFactory, proxyRepo)

	svc.SetRefreshAPI(refreshAPI)

	svc.SetRefreshPolicy(service.DefaultBackgroundRefreshPolicy())
	svc.SetAccountRuntimeBlocker(runtimeBlocker)
	s.WorkerStart(svc.Start)
	return svc
}

func provideUsageCleanupService(s *process.Supervisor, repo service.UsageCleanupRepository, timingWheel *service.TimingWheelService, dashboardAgg *service.DashboardAggregationService, cfg *config.Config) *service.UsageCleanupService {
	svc := service.NewUsageCleanupService(repo, timingWheel, dashboardAgg, cfg)
	s.WorkerStart(svc.Start)
	return svc
}

func provideUpstreamBillingProbeService(s *process.Supervisor, accountRepo service.AccountRepository,
	accountTestService *service.AccountTestService,
	settingService *service.SettingService,
	lockCache service.LeaderLockCache,
	db *sql.DB) *service.UpstreamBillingProbeService {
	svc := service.NewUpstreamBillingProbeService(accountRepo, accountTestService, settingService)
	svc.SetLeaderLock(lockCache, db)
	s.WorkerStart(svc.Start)
	return svc
}

func provideBatchImageCleanupService(s *process.Supervisor, repo service.BatchImageRepository, accountRepo service.AccountRepository, cfg *config.Config) *service.BatchImageCleanupService {
	svc := service.NewBatchImageCleanupService(repo, accountRepo, cfg)
	s.WorkerStart(svc.Start)
	return svc
}

func provideIdempotencyCleanupService(s *process.Supervisor, repo service.IdempotencyRepository, cfg *config.Config) *service.IdempotencyCleanupService {
	svc := service.NewIdempotencyCleanupService(repo, cfg)
	s.WorkerStart(svc.Start)
	return svc
}

func provideOpenAIQuotaAutoResetService(s *process.Supervisor, accountRepo service.AccountRepository,
	quotaService *service.OpenAIQuotaService,
	rateLimitService *service.RateLimitService,
	idempotency *service.IdempotencyCoordinator,
	audit *service.AuditLogService,
	settingService *service.SettingService,
	leaderLock service.LeaderLockCache) *service.OpenAIQuotaAutoResetService {
	svc := service.NewOpenAIQuotaAutoResetService(
		accountRepo,
		quotaService,
		rateLimitService,
		idempotency,
		audit,
		settingService,
		leaderLock,
	)
	s.WorkerStart(svc.Start)
	return svc
}

func provideOpsMetricsCollector(s *process.Supervisor, opsRepo service.OpsRepository,
	settingRepo service.SettingRepository,
	accountRepo service.AccountRepository,
	concurrencyService *service.ConcurrencyService,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config) *service.OpsMetricsCollector {
	collector := service.NewOpsMetricsCollector(opsRepo, settingRepo, accountRepo, concurrencyService, db, redisClient, cfg)
	s.WorkerStart(collector.Start)
	return collector
}

func provideOpsCleanupService(s *process.Supervisor, opsRepo service.OpsRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	channelMonitorSvc *service.ChannelMonitorService,
	settingRepo service.SettingRepository,
	opsService *service.OpsService) *service.OpsCleanupService {
	svc := service.NewOpsCleanupService(opsRepo, db, redisClient, cfg, channelMonitorSvc, settingRepo)
	s.WorkerStart(svc.Start)
	if s.Mode.Workers() && opsService != nil {
		opsService.SetCleanupReloader(svc)
	}
	return svc
}

func provideSchedulerSnapshotService(s *process.Supervisor, cache service.SchedulerCache,
	outboxRepo service.SchedulerOutboxRepository,
	accountRepo service.AccountRepository,
	groupRepo service.GroupRepository,
	cfg *config.Config) *service.SchedulerSnapshotService {
	svc := service.NewSchedulerSnapshotService(cache, outboxRepo, accountRepo, groupRepo, cfg)
	s.WorkerStart(svc.Start)
	return svc
}

func provideConcurrencyService(s *process.Supervisor, cache service.ConcurrencyCache, cfg *config.Config) *service.ConcurrencyService {
	svc := service.NewConcurrencyService(cache)
	svc.SetAccountLoadBatchCacheTTL(time.Duration(cfg.Gateway.Scheduling.LoadBatchCacheTTLMS) * time.Millisecond)
	// Process prefixes identify ownership, not liveness. Other gateway and all
	// replicas can hold active slots when this process starts or restarts.
	if interval := cfg.Gateway.Scheduling.SlotCleanupInterval; s.Mode.Workers() && interval > 0 {
		s.Add(func(ctx context.Context) {
			process.Every(ctx, interval, func(ctx context.Context) {
				run, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				if err := cache.CleanupExpiredAccountSlotKeys(run); err != nil && ctx.Err() == nil {
					log.Print("expired account slot cleanup failed")
				}
			})
		})
	}
	return svc
}

func provideUserMessageQueueService(s *process.Supervisor, cache service.UserMsgQueueCache, rpmCache service.RPMCache, cfg *config.Config) *service.UserMessageQueueService {
	svc := service.NewUserMessageQueueService(cache, rpmCache, &cfg.Gateway.UserMessageQueue)
	if sec := cfg.Gateway.UserMessageQueue.CleanupIntervalSeconds; sec > 0 && s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			process.Every(ctx, time.Second*time.Duration(sec), func(ctx context.Context) {
				run, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				if _, err := cache.ReconcileExpiredLockCandidates(run, 1000); err != nil && ctx.Err() == nil {
					log.Print("orphaned message lock cleanup failed")
				}
			})
		})
	}
	return svc
}

func provideUsageSettlementService(s *process.Supervisor, repo service.UsageSettlementReceiptRepository, pool *service.UsageRecordWorkerPool, cache *service.BillingCacheService, keys *service.APIKeyService, db *sql.DB) *service.UsageSettlementService {
	// Live submissions keep their request-scoped quota invalidation callback.
	// Recovery carries its own API-key invalidator for replay without a caller.
	if s.Mode == process.Gateway || s.Mode.Workers() {
		recovery := &journal.Recovery{Store: journal.New(db), Interval: time.Minute}
		s.Add(recovery.Start)
	}
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			recovery := service.ProvideUsageSettlementService(repo, pool, cache, keys)
			<-ctx.Done()
			recovery.Stop()
		})
	}
	return service.NewUsageSettlementService(repo, pool, cache)
}

func provideUpstreamJobService(s *process.Supervisor, repo service.UpstreamJobRepository, gateway *service.OpenAIGatewayService, wheel *service.TimingWheelService, billing *service.BillingCacheService, keys *service.APIKeyService) *service.UpstreamJobService {
	// The existing provider installs its cache invalidation hook and a wheel timer.
	// Remove that timer before this application is published and run the same
	// durable poll operation on the supervised worker context.
	svc := service.ProvideUpstreamJobService(repo, gateway, wheel, billing, keys)
	wheel.Cancel("upstream-jobs")
	if s.Mode.Workers() {
		s.Add(func(ctx context.Context) {
			process.Every(ctx, 5*time.Second, func(ctx context.Context) {
				poll, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				if err := svc.PollDue(poll, 20); err != nil && ctx.Err() == nil {
					log.Print("upstream job poll failed")
				}
			})
		})
	}
	return svc
}

func provideOllamaCloudUsageService(s *process.Supervisor, accountRepo service.AccountRepository,
	httpUpstream service.HTTPUpstream,
	settingService *service.SettingService,
	encryptor service.SecretEncryptor,
	cfg *config.Config,
	lockCache service.LeaderLockCache,
	db *sql.DB) *service.OllamaCloudUsageService {
	if s.Mode.Workers() {
		return service.ProvideOllamaCloudUsageService(accountRepo, httpUpstream, settingService, encryptor, cfg, lockCache, db)
	}
	return service.NewOllamaCloudUsageService(accountRepo, httpUpstream, settingService, encryptor, cfg.Totp.EncryptionKeyConfigured)
}
