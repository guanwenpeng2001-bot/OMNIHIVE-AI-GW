//go:build wireinject
// +build wireinject

package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	audithttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/audit"
	billinghttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/billing"
	byokhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/byok"
	modelshttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/models"
	verificationhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/verification"
	connectionshttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/connections"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	observabilityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/observability"
	paymentshttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/payments"
	playgroundhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/playground"
	policieshttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/policies"
	requestshttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/requests"
	restorehttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/restore"
	statushttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/status"
	taskhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	legacyrestore "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/restore"
	auditstore "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/audit"
	byokdomain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/byok"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	verificationdomain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog/verification"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	identitymod "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	identitypostgres "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	exportstore "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/export"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/payments"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/payment"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/securityaudit"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/routes"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"

	"github.com/google/wire"
	process "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/runtime"
	"github.com/redis/go-redis/v9"
)

type Application struct {
	Supervisor *process.Supervisor
	Admission  *process.Admission
	Audit      *audithttp.Handler

	Status          *statushttp.Handler
	ImageTaskCenter *taskhttp.Handler
	Restores        *legacyrestore.Runtime
	OpsMetrics      *service.OpsMetricsCollector
	UpstreamJobs    *service.UpstreamJobService
	Server          *http.Server
	PromptAudit     *securityaudit.PromptService
	PluginManager   *service.PluginManager
	Cleanup         func()
}

func initializeApplication(buildInfo handler.BuildInfo, cfg *config.Config, supervisor *process.Supervisor) (*Application, error) {
	wire.Build(
		// Infrastructure and business providers
		repository.ProviderSet,
		runtimeServiceProviders,
		securityaudit.ProviderSet,
		payment.ProviderSet,
		middleware.ProviderSet,
		handler.ProviderSet,
		identityhttp.ProviderSet,
		connectionshttp.ProviderSet,
		modelshttp.ProviderSet,
		catalogVerificationStoreProvider,
		catalogVerificationHandlerProvider,
		restorehttp.ProviderSet,
		requestsHandlerProvider,
		handler.ProvideImageTaskCenter,
		policieshttp.ProviderSet,
		playgroundHandlerProvider,
		billingHandlerProvider,
		paymentStoreProvider,
		paymentEvidenceHandlerProvider,
		statusHandlerProvider,
		byokStoreProvider,
		byokHandlerProvider,
		auditStoreProvider,
		auditObserverProvider,
		auditHandlerProvider,
		observabilityStoreProvider,
		observabilityHandlerProvider,
		wire.Bind(new(connections.Prober), new(*service.AccountTestService)),
		provideIdentityOperations,

		// Server layer ProviderSet
		server.ProviderSet,
		provideDingTalkTokenCache,

		// Privacy client factory for OpenAI training opt-out
		providePrivacyClientFactory,

		// BuildInfo provider
		provideServiceBuildInfo,
		providePluginHostInfo,

		// Cleanup function provider
		provideCleanup,
		process.NewAdmission,

		// Application struct
		wire.Struct(new(Application), "Server", "PromptAudit", "PluginManager", "Cleanup", "UpstreamJobs", "OpsMetrics", "Restores", "ImageTaskCenter", "Status", "Audit", "Supervisor", "Admission"),
	)
	return nil, nil
}

func requestsHandlerProvider(db *sql.DB, store *identitypostgres.Store, cfg *config.Config) *requestshttp.Handler {
	return handler.ProvideRequestsHandler(db, store, cfg)
}

func playgroundHandlerProvider(
	db *sql.DB,
	access identity.AccessEvaluator,
	scopes identity.GatewayScopes,
	keys *service.APIKeyService,
	subscriptions *service.SubscriptionService,
	catalogReader catalog.CatalogReader,
	policies *policy.Store,
	gateway *service.GatewayService,
	handlers *handler.Handlers,
	composite *service.CompositeRouteResolver,
	cfg *config.Config,
) *playgroundhttp.Handler {
	dispatch := service.NewGatewayActionDispatch(routes.GatewayActionHandlers(handlers))
	common := []gin.HandlerFunc{middleware.GatewayProtocol(), handler.InboundEndpointMiddleware()}
	if handlers.MCP != nil {
		common = append(common, handlers.MCP.GatewayExtensionMiddleware())
	}
	native := playgroundhttp.NativeRoutes(dispatch, composite, handlers.Gateway.ActionRequirement, common...)
	return playgroundhttp.New(&playgroundhttp.Gateway{
		DB: db, Access: access, Scopes: scopes, Keys: keys, Subscriptions: subscriptions,
		Catalog: catalogReader, Policies: policies, Plans: gateway, Routes: native,
	}, cfg.TrustForwardedIPForAPIKeyACL())
}

func observabilityStoreProvider(supervisor *process.Supervisor, cfg *config.Config) (*exportstore.Store, error) {
	store, cleanup, err := observabilityhttp.ProvideStore(cfg)
	if err != nil {
		return nil, err
	}
	supervisor.Add(func(ctx context.Context) {
		<-ctx.Done()
		cleanup()
	})
	if supervisor.Mode.Serving() {
		supervisor.Add(func(ctx context.Context) { store.RunIngest(ctx, func() {}) })
	}
	if supervisor.Mode.Workers() {
		supervisor.Add(func(ctx context.Context) { store.RunDelivery(ctx, func() {}) })
	}
	return store, nil
}

func observabilityHandlerProvider(store *exportstore.Store) *observabilityhttp.Handler {
	return observabilityhttp.New(store)
}

func auditStoreProvider(db *sql.DB, access *identitypostgres.Store) *auditstore.Store {
	return auditstore.New(db, access)
}

func auditObserverProvider() *auditstore.Observer { return auditstore.NewObserver() }

func auditHandlerProvider(store *auditstore.Store, observer *auditstore.Observer, cfg *config.Config, audit *service.AuditLogService) *audithttp.Handler {
	audit.SetScopedStore(store)
	return audithttp.New(store, observer, cfg)
}

func byokStoreProvider(db *sql.DB, enc service.SecretEncryptor, probe *service.AccountTestService, access identitymod.AccessEvaluator, cfg *config.Config) *byokdomain.Store {
	return byokdomain.New(db, enc, probe, access, []byte(cfg.JWT.Secret))
}

func byokHandlerProvider(store *byokdomain.Store) *byokhttp.Handler {
	return byokhttp.New(store)
}

func catalogVerificationStoreProvider(supervisor *process.Supervisor, db *sql.DB, access identitymod.AccessEvaluator, enc service.SecretEncryptor, prober *service.AccountTestService) *verificationdomain.Store {
	store := verificationdomain.NewStore(db, access)
	if supervisor != nil && supervisor.Mode.Workers() {
		worker := verificationdomain.NewWorker(store, verificationdomain.NewConnectionProber(db, enc, prober))
		supervisor.Add(func(ctx context.Context) { worker.Run(ctx) })
	}
	return store
}

func catalogVerificationHandlerProvider(store *verificationdomain.Store) *verificationhttp.Handler {
	return verificationhttp.New(store)
}

func statusHandlerProvider(db *sql.DB, store *identitypostgres.Store, cfg *config.Config, ops *service.OpsService) *statushttp.Handler {
	return handler.ProvideStatusHandler(db, store, cfg, ops)
}

func billingHandlerProvider(db *sql.DB, store *identitypostgres.Store, totp *service.TotpService, cfg *config.Config) *billinghttp.Handler {
	return handler.ProvideCostWorkbench(db, store, totp, cfg)
}

func paymentStoreProvider(db *sql.DB, fulfillment *service.PaymentService, billing *service.BillingCacheService, keys *service.APIKeyService, subscriptions *service.SubscriptionService) *payments.Store {
	return &payments.Store{DB: db, Fulfillment: fulfillment, Refresh: func(ctx context.Context, payerID, groupID int64) error {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		balanceErr := billing.InvalidateUserBalance(cacheCtx, payerID)
		keys.InvalidateAuthCacheByUserID(cacheCtx, payerID)
		if groupID > 0 {
			return errors.Join(balanceErr, subscriptions.RefreshPaymentSubscription(payerID, groupID))
		}
		return balanceErr
	}}
}

func paymentEvidenceHandlerProvider(store *payments.Store, access identity.AccessEvaluator, totp *service.TotpService) paymentshttp.Handler {
	return paymentshttp.Handler{Store: *store, Access: access, Step: totp}
}

func provideIdentityOperations(store *connections.Store, restores *legacyrestore.Runtime, requests *requestshttp.Handler, billing *billinghttp.Handler, byok *byokdomain.Store, audit *auditstore.Store, paymentEvidence paymentshttp.Handler, catalogVerifications *verificationdomain.Store) identity.Operations {
	return identity.Operations{Readers: []identity.OperationReader{store, restores, requests, billing, byok, audit, paymentEvidence, catalogVerifications}}
}

func providePrivacyClientFactory() service.PrivacyClientFactory {
	return repository.CreatePrivacyReqClient
}

func provideServiceBuildInfo(buildInfo handler.BuildInfo) service.BuildInfo {
	return service.BuildInfo{
		Version:   buildInfo.Version,
		BuildType: buildInfo.BuildType,
	}
}

func providePluginHostInfo(buildInfo handler.BuildInfo) service.PluginHostInfo {
	return service.PluginHostInfo{
		Version:   buildInfo.Version,
		BuildType: buildInfo.BuildType,
	}
}

func provideCleanup(
	entClient *ent.Client,
	rdb *redis.Client,
	wheel *service.TimingWheelService,
	deferred *service.DeferredService,
	restores *legacyrestore.Runtime,
	opsMetricsCollector *service.OpsMetricsCollector,
	opsAggregation *service.OpsAggregationService,
	opsAlertEvaluator *service.OpsAlertEvaluatorService,
	opsCleanup *service.OpsCleanupService,
	opsScheduledReport *service.OpsScheduledReportService,
	opsSystemLogSink *service.OpsSystemLogSink,
	opsService *service.OpsService,
	opsIngressReject *service.OpsIngressRejectAggregator,
	apiKeyService *service.APIKeyService,
	authCacheInvalidationWorker *service.AuthCacheInvalidationWorker,
	schedulerSnapshot *service.SchedulerSnapshotService,
	tokenRefresh *service.TokenRefreshService,
	accountExpiry *service.AccountExpiryService,
	cnProviderBalanceCheck *service.CNProviderBalanceCheckService,
	codexVersionSync *service.OpenAICodexVersionSyncService,
	proxyExpiry *service.ProxyExpiryService,
	subscriptionExpiry *service.SubscriptionExpiryService,
	usageCleanup *service.UsageCleanupService,
	idempotencyCleanup *service.IdempotencyCleanupService,
	batchImageCleanup *service.BatchImageCleanupService,
	batchImageWorker *service.BatchImageWorkerRuntime,
	pricing *service.PricingService,
	emailQueue *service.EmailQueueService,
	billingCache *service.BillingCacheService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	usageSettlement *service.UsageSettlementService,
	subscriptionService *service.SubscriptionService,
	oauth *service.OAuthService,
	openaiOAuth *service.OpenAIOAuthService,
	geminiOAuth *service.GeminiOAuthService,
	antigravityOAuth *service.AntigravityOAuthService,
	grokOAuth *service.GrokOAuthService,
	openAIGateway *service.OpenAIGatewayService,
	scheduledTestRunner *service.ScheduledTestRunnerService,
	backupSvc *service.BackupService,
	paymentOrderExpiry *service.PaymentOrderExpiryService,
	channelMonitorRunner *service.ChannelMonitorRunner,
	channelTelemetryAggregator *service.ChannelTelemetryAggregator,
	quotaFlusher *service.UserPlatformQuotaUsageFlusher,
	upstreamBillingProbe *service.UpstreamBillingProbeService,
	ollamaCloudUsage *service.OllamaCloudUsageService,
	auditLog *service.AuditLogService,
	openAIAutoReset *service.OpenAIQuotaAutoResetService,
	promptAudit *securityaudit.PromptService,
	pluginManager *service.PluginManager,
) func() {
	return func() {
		admin.StopImportProbes()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		type cleanupStep struct {
			name string
			fn   func() error
		}

		// 应用层清理步骤可并行执行，基础设施资源（Redis/Ent）最后按顺序关闭。
		parallelSteps := []cleanupStep{
			{"PluginManager", func() error {
				if pluginManager != nil {
					pluginManager.Stop()
				}
				return nil
			}},
			{"OpenAIQuotaAutoResetService", func() error {
				if openAIAutoReset != nil {
					openAIAutoReset.Stop()
				}
				return nil
			}},
			{"OpsIngressRejectAggregator", func() error {
				if opsIngressReject != nil {
					opsIngressReject.Stop()
				}
				return nil
			}},
			{"AuthCacheInvalidationWorker", func() error {
				if authCacheInvalidationWorker != nil {
					authCacheInvalidationWorker.Stop()
				}
				return nil
			}},
			{"AuthCacheInvalidationSubscriber", func() error {
				if apiKeyService != nil {
					apiKeyService.StopAuthCacheInvalidationSubscriber()
				}
				return nil
			}},
			{"OpsRuntimeSettingsRefresh", func() error {
				if opsService != nil {
					opsService.StopRuntimeSettingsRefresh()
				}
				return nil
			}},
			{"PromptAuditService", func() error {
				if promptAudit != nil {
					return promptAudit.Shutdown(ctx)
				}
				return nil
			}},
			{"OpsScheduledReportService", func() error {
				if opsScheduledReport != nil {
					opsScheduledReport.Stop()
				}
				return nil
			}},
			{"OpsCleanupService", func() error {
				if opsCleanup != nil {
					opsCleanup.Stop()
				}
				return nil
			}},
			{"OpsSystemLogSink", func() error {
				if opsSystemLogSink != nil {
					opsSystemLogSink.Stop()
				}
				return nil
			}},
			{"AuditLogService", func() error {
				if auditLog != nil {
					auditLog.Stop()
				}
				return nil
			}},
			{"OpsAlertEvaluatorService", func() error {
				if opsAlertEvaluator != nil {
					opsAlertEvaluator.Stop()
				}
				return nil
			}},
			{"OpsAggregationService", func() error {
				if opsAggregation != nil {
					opsAggregation.Stop()
				}
				return nil
			}},
			{"OpsMetricsCollector", func() error {
				if opsMetricsCollector != nil {
					opsMetricsCollector.Stop()
				}
				return nil
			}},
			{"SchedulerSnapshotService", func() error {
				if schedulerSnapshot != nil {
					schedulerSnapshot.Stop()
				}
				return nil
			}},
			{"UsageCleanupService", func() error {
				if usageCleanup != nil {
					usageCleanup.Stop()
				}
				return nil
			}},
			{"IdempotencyCleanupService", func() error {
				if idempotencyCleanup != nil {
					idempotencyCleanup.Stop()
				}
				return nil
			}},
			{"BatchImageCleanupService", func() error {
				if batchImageCleanup != nil {
					batchImageCleanup.Stop()
				}
				return nil
			}},
			{"BatchImageWorkerRuntime", func() error {
				if batchImageWorker != nil {
					batchImageWorker.Stop()
				}
				return nil
			}},
			{"TokenRefreshService", func() error {
				tokenRefresh.Stop()
				return nil
			}},
			{"AccountExpiryService", func() error {
				if accountExpiry != nil {
					accountExpiry.Stop()
				}
				return nil
			}},
			{"CNProviderBalanceCheckService", func() error {
				if cnProviderBalanceCheck != nil {
					cnProviderBalanceCheck.Stop()
				}
				return nil
			}},
			{"OpenAICodexVersionSyncService", func() error {
				if codexVersionSync != nil {
					codexVersionSync.Stop()
				}
				return nil
			}},
			{"ProxyExpiryService", func() error {
				if proxyExpiry != nil {
					proxyExpiry.Stop()
				}
				return nil
			}},
			{"SubscriptionExpiryService", func() error {
				if subscriptionExpiry != nil {
					subscriptionExpiry.Stop()
				}
				return nil
			}},
			{"SubscriptionService", func() error {
				if subscriptionService != nil {
					subscriptionService.Stop()
				}
				return nil
			}},
			{"PricingService", func() error {
				pricing.Stop()
				return nil
			}},
			{"EmailQueueService", func() error {
				emailQueue.Stop()
				return nil
			}},
			{"OAuthService", func() error {
				oauth.Stop()
				return nil
			}},
			{"OpenAIOAuthService", func() error {
				openaiOAuth.Stop()
				return nil
			}},
			{"GeminiOAuthService", func() error {
				geminiOAuth.Stop()
				return nil
			}},
			{"AntigravityOAuthService", func() error {
				antigravityOAuth.Stop()
				return nil
			}},
			{"GrokOAuthService", func() error {
				if grokOAuth != nil {
					grokOAuth.Stop()
				}
				return nil
			}},
			{"OpenAIWSPool", func() error {
				if openAIGateway != nil {
					openAIGateway.CloseOpenAIWSPool()
				}
				return nil
			}},
			{"ScheduledTestRunnerService", func() error {
				if scheduledTestRunner != nil {
					scheduledTestRunner.Stop()
				}
				return nil
			}},
			{"BackupService", func() error {
				if backupSvc != nil {
					backupSvc.Stop()
				}
				return nil
			}},
			{"PaymentOrderExpiryService", func() error {
				if paymentOrderExpiry != nil {
					paymentOrderExpiry.Stop()
				}
				return nil
			}},
			{"ChannelTelemetryAggregator", func() error {
				if channelTelemetryAggregator != nil {
					channelTelemetryAggregator.Stop()
				}
				return nil
			}},
			{"ChannelMonitorRunner", func() error {
				if channelMonitorRunner != nil {
					channelMonitorRunner.Stop()
				}
				return nil
			}},
			{"UserPlatformQuotaUsageFlusher", func() error {
				if quotaFlusher != nil {
					quotaFlusher.Stop()
				}
				return nil
			}},
			{"UpstreamBillingProbeService", func() error {
				if upstreamBillingProbe != nil {
					upstreamBillingProbe.Stop()
				}
				return nil
			}},
			{"OllamaCloudUsageService", func() error {
				if ollamaCloudUsage != nil {
					ollamaCloudUsage.Stop()
				}
				return nil
			}},
		}

		infraSteps := []cleanupStep{
			{"UsageRecordWorkerPool", func() error {
				if usageSettlement != nil {
					usageSettlement.Stop()
				}
				if usageRecordWorkerPool != nil {
					usageRecordWorkerPool.Stop()
				}
				return nil
			}},
			{"BillingCacheService", func() error {
				billingCache.Stop()
				return nil
			}},
			{"DeferredAccountUpdates", func() error {
				if deferred != nil {
					deferred.Stop()
				}
				return nil
			}},
			{"RestoreConnections", func() error {
				if restores != nil {
					restores.Close()
				}
				return nil
			}},
			{"TimingWheel", func() error {
				if wheel != nil {
					wheel.Stop()
				}
				return nil
			}},
			{"Redis", func() error {
				if rdb == nil {
					return nil
				}
				return rdb.Close()
			}},
			{"Ent", func() error {
				if entClient == nil {
					return nil
				}
				return entClient.Close()
			}},
		}

		runParallel := func(steps []cleanupStep) {
			var wg sync.WaitGroup
			for i := range steps {
				step := steps[i]
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := step.fn(); err != nil {
						log.Printf("[Cleanup] %s failed: %v", step.name, err)
						return
					}
					log.Printf("[Cleanup] %s succeeded", step.name)
				}()
			}
			wg.Wait()
		}

		runSequential := func(steps []cleanupStep) {
			for i := range steps {
				step := steps[i]
				if err := step.fn(); err != nil {
					log.Printf("[Cleanup] %s failed: %v", step.name, err)
					continue
				}
				log.Printf("[Cleanup] %s succeeded", step.name)
			}
		}

		runParallel(parallelSteps)
		runSequential(infraSteps)

		// Check if context timed out
		select {
		case <-ctx.Done():
			log.Printf("[Cleanup] Warning: cleanup timed out after 10 seconds")
		default:
			log.Printf("[Cleanup] All cleanup steps completed")
		}
	}
}

// The binary selects passive constructors and worker-owned schedules explicitly.
var runtimeServiceProviders = wire.NewSet(
	service.NewStagedAccountImportService,
	service.NewProxyGroupAdminService,
	service.NewProxyGroupRuntime,
	service.NewModelAccessService,
	service.NewWorkspaceService,
	provideUpstreamJobService,
	service.ProvideCursorSessionProvider,
	service.ProvideCursorGatewayService,
	// Core services
	service.ProvideAuthService,
	service.NewPasskeyService,
	service.NewUserService,
	service.ProvideAPIKeyService,
	service.ProvideAPIKeyAuthCacheInvalidator,
	provideAuthCacheInvalidationWorker,
	service.NewGroupService,
	service.NewCompositeRouteResolver,
	service.NewAccountService,
	service.NewCredentialSecretService,
	service.NewProxyService,
	service.NewRedeemService,
	service.NewPromoService,
	service.NewUsageService,
	service.NewDashboardService,
	service.NewModelRegistry,
	service.ProvidePricingService,
	service.NewBillingService,
	service.ProvideBillingCacheService,
	service.NewAnnouncementService,
	service.ProvideAdminServiceWithEgress,
	service.ProvideAccountRuntimeInvalidator,
	service.ProvideGatewayService,
	service.ProvideOpenAIGatewayService,
	service.ProvideImageStorageSettingService,
	service.ProvideImageTaskService,
	service.ProvideBatchImageModelPricingResolver,
	service.NewBatchImagePublicService,
	service.NewBatchImageDownloadService,
	provideBatchImageCleanupService,
	provideBatchImageWorkerRuntime,
	wire.Bind(new(service.AccountRuntimeBlocker), new(*service.OpenAIGatewayService)),
	service.ProvideOAuthServiceWithEgress,
	service.ProvideOpenAIOAuthService,
	service.ProvideGrokOAuthService,
	wire.Bind(new(service.GrokOAuthTokenService), new(*service.GrokOAuthService)),
	service.ProvideGeminiOAuthServiceWithEgress,
	service.NewGeminiQuotaService,
	service.NewCompositeTokenCacheInvalidator,
	wire.Bind(new(service.TokenCacheInvalidator), new(*service.CompositeTokenCacheInvalidator)),
	service.ProvideAntigravityOAuthServiceWithEgress,
	service.ProvideOAuthRefreshAPI,
	service.ProvideGeminiTokenProvider,
	service.NewGeminiMessagesCompatService,
	service.ProvideAntigravityTokenProvider,
	service.ProvideGrokTokenProvider,
	service.ProvideOpenAITokenProvider,
	service.ProvideOpenAIQuotaService,
	provideOpenAIQuotaAutoResetService,
	service.ProvideGrokQuotaService,
	service.ProvideCNProviderQuotaService,
	service.ProvideCNProviderBalanceService,
	provideCNProviderBalanceCheckService,
	service.ProvideClaudeTokenProvider,
	service.NewAntigravityGatewayService,
	service.ProvideRateLimitService,
	service.ProvideAccountUsageService,
	service.ProvideAccountTestService,
	provideUpstreamBillingProbeService,
	provideOllamaCloudUsageService,
	service.ProvideSettingService,
	provideBackupService,
	service.ProvideOpsSystemLogSink,
	service.ProvideOpsService,
	service.ProvideOpsIngressRejectAggregator,
	service.ProvideAuditLogService,
	provideOpsMetricsCollector,
	provideOpsAggregationService,
	provideOpsAlertEvaluatorService,
	provideOpsCleanupService,
	provideOpsScheduledReportService,
	service.NewEmailService,
	service.NewNotificationEmailService,
	service.ProvideEmailQueueService,
	service.NewTurnstileService,
	service.NewTencentCaptchaService,
	service.NewAliyunCaptchaService,
	service.NewSubscriptionService,
	wire.Bind(new(service.DefaultSubscriptionAssigner), new(*service.SubscriptionService)),
	provideConcurrencyService,
	provideUserMessageQueueService,
	service.NewUsageRecordWorkerPool,
	provideUsageSettlementService,
	provideSchedulerSnapshotService,
	service.NewIdentityService,
	service.NewCRSSyncService,
	service.ProvideUpdateService,
	provideTokenRefreshService,
	wire.Bind(new(service.GrokOAuthReconciler), new(*service.TokenRefreshService)),
	provideAccountExpiryService,
	provideOpenAICodexVersionSyncService,
	provideProxyExpiryService,
	provideSubscriptionExpiryService,
	service.ProvideTimingWheelService,
	provideDashboardAggregationService,
	provideUsageCleanupService,
	service.ProvideDeferredService,
	service.NewAntigravityQuotaFetcher,
	service.NewGrokQuotaFetcher,
	service.NewUserAttributeService,
	service.NewUsageCache,
	service.NewTotpService,
	service.NewErrorPassthroughService,
	service.NewTLSFingerprintProfileService,
	service.NewPluginManager,
	service.NewDigestSessionStore,
	service.ProvideIdempotencyCoordinator,
	service.ProvideSystemOperationLockService,
	provideIdempotencyCleanupService,
	service.ProvideScheduledTestService,
	provideScheduledTestRunnerService,
	service.NewGroupCapacityService,
	service.NewChannelService,
	wire.Bind(new(service.ChannelCacheInvalidator), new(*service.ChannelService)),
	service.NewModelPricingResolver,
	service.NewModelPlazaService,
	service.ProvideContentModerationService,
	service.NewAffiliateService,
	service.ProvidePaymentConfigService,
	service.ProvidePaymentService,
	providePaymentOrderExpiryService,
	service.ProvideBalanceNotifyService,
	service.ProvideChannelMonitorService,
	provideChannelMonitorRunner,
	service.NewChannelMonitorQuotaFetcher,
	service.ProvideChannelTelemetryService,
	provideChannelTelemetryAggregator,
	service.NewChannelMonitorRequestTemplateService,
	provideUserPlatformQuotaUsageFlusher,
)
