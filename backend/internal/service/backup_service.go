package service

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
)

const (
	settingKeyBackupS3Config = "backup_s3_config"
	settingKeyBackupSchedule = "backup_schedule"
	settingKeyBackupRecords  = "backup_records"

	backupObjectCleanupTimeout = 2 * time.Minute

	// backupScheduledLeaderLockKey gates the scheduled full-database backup so
	// that only one instance in a clustered deployment performs the
	// dump-and-upload each cycle. Without it every instance runs the cron
	// independently, producing N concurrent pg_dumps against the same database,
	// N× peak memory while the archive is uploaded, and N identical objects that
	// overwrite the same timestamped key. Every other periodic job in this
	// package is already gated the same way; the scheduled backup was the last
	// one that still fanned out across every instance.
	backupScheduledLeaderLockKey = "backup:scheduled:leader"
	// backupScheduledLeaderLockTTL bounds crash recovery only; the lock is
	// released as soon as the backup finishes. It must exceed the job's
	// worst-case runtime (the scheduled backup context is bounded at 30m) so the
	// lock cannot expire mid-dump and let a peer start a second backup.
	backupScheduledLeaderLockTTL = 35 * time.Minute
)

var (
	ErrBackupS3NotConfigured = infraerrors.BadRequest("BACKUP_S3_NOT_CONFIGURED", "backup S3 storage is not configured")
	ErrBackupNotFound        = infraerrors.NotFound("BACKUP_NOT_FOUND", "backup record not found")
	ErrBackupInProgress      = infraerrors.Conflict("BACKUP_IN_PROGRESS", "a backup is already in progress")
	ErrBackupRecordStale     = infraerrors.Conflict("BACKUP_RECORD_STALE", "backup record was changed or deleted by another worker")
	ErrRestoreInProgress     = infraerrors.Conflict("RESTORE_IN_PROGRESS", "a restore is already in progress")
	ErrBackupRecordsCorrupt  = infraerrors.InternalServer("BACKUP_RECORDS_CORRUPT", "backup records data is corrupted")
	ErrBackupS3ConfigCorrupt = infraerrors.InternalServer("BACKUP_S3_CONFIG_CORRUPT", "backup S3 config data is corrupted")
)

// ─── 接口定义 ───

// DBDumper abstracts database dump/restore operations
type DBDumper interface {
	Dump(ctx context.Context) (io.ReadCloser, error)
	Restore(ctx context.Context, data io.Reader) error
}

// BackupObjectStore abstracts object storage for backup files
type BackupObjectStore interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string) (sizeBytes int64, err error)
	UploadFile(ctx context.Context, key string, filePath string, contentType string) (sizeBytes int64, err error)
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	PresignURL(ctx context.Context, key string, expiry time.Duration) (string, error)
	HeadBucket(ctx context.Context) error
}

// BackupObjectStoreFactory creates an object store from S3 config
type BackupObjectStoreFactory func(ctx context.Context, cfg *BackupS3Config) (BackupObjectStore, error)

// ─── 数据模型 ───

// BackupS3Config S3 兼容存储配置（支持 Cloudflare R2）
type BackupS3Config struct {
	Endpoint        string `json:"endpoint"` // e.g. https://<account_id>.r2.cloudflarestorage.com
	Region          string `json:"region"`   // R2 用 "auto"
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key,omitempty"` //nolint:revive // field name follows AWS convention
	Prefix          string `json:"prefix"`                      // S3 key 前缀，如 "backups/"
	ForcePathStyle  bool   `json:"force_path_style"`
}

// IsConfigured 检查必要字段是否已配置
func (c *BackupS3Config) IsConfigured() bool {
	return c.Bucket != "" && c.AccessKeyID != "" && c.SecretAccessKey != ""
}

const defaultBackupRetainDays = 14

// BackupScheduleConfig 定时备份配置
type BackupScheduleConfig struct {
	Enabled     bool   `json:"enabled"`
	CronExpr    string `json:"cron_expr"`    // cron 表达式，如 "0 2 * * *" 每天凌晨2点
	RetainDays  int    `json:"retain_days"`  // 备份文件过期天数，默认14，0=不自动清理
	RetainCount int    `json:"retain_count"` // 最多保留份数，0=不限制
}

// UnmarshalJSON applies the same default to API input and saved schedules.
// An explicitly stored zero is the operator's permanent-retention policy.
func (c *BackupScheduleConfig) UnmarshalJSON(data []byte) error {
	type schedule BackupScheduleConfig
	value := schedule{RetainDays: defaultBackupRetainDays}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*c = BackupScheduleConfig(value)
	return nil
}

// BackupRecord 备份记录
type BackupRecord struct {
	ID                 string       `json:"id"`
	Status             string       `json:"status"`      // pending, running, completed, failed
	BackupType         string       `json:"backup_type"` // postgres
	FileName           string       `json:"file_name"`
	S3Key              string       `json:"s3_key"`
	Parts              []BackupPart `json:"parts,omitempty"`
	SizeBytes          int64        `json:"size_bytes"`
	TriggeredBy        string       `json:"triggered_by"` // manual, scheduled
	ErrorMsg           string       `json:"error_message,omitempty"`
	StartedAt          string       `json:"started_at"`
	FinishedAt         string       `json:"finished_at,omitempty"`
	ExpiresAt          string       `json:"expires_at,omitempty"` // 过期时间
	Progress           string       `json:"progress,omitempty"`   // "dumping", "uploading", ""
	Checksum           string       `json:"checksum,omitempty"`
	VerifiedAt         string       `json:"verified_at,omitempty"`
	VerificationError  string       `json:"verification_error,omitempty"`
	RestoreStatus      string       `json:"restore_status,omitempty"` // "", "running", "completed", "failed"
	RestoreOperationID string       `json:"restore_operation_id,omitempty"`
	RestoreError       string       `json:"restore_error,omitempty"`
	RestoredAt         string       `json:"restored_at,omitempty"`
	// AttemptID and Revision are database-only concurrency fences. They are not
	// exposed in API records or duplicated into the historical record JSON.
	AttemptID string `json:"-"`
	Revision  int64  `json:"-"`
}

// BackupSchedulerRuntime is the scheduler's persisted operational projection.
// All timestamps are RFC3339 to preserve the existing handler/API convention.
type BackupSchedulerRuntime struct {
	Loaded         bool   `json:"loaded"`
	Health         string `json:"health"`
	NextRunAt      string `json:"next_run_at,omitempty"`
	LastAttemptAt  string `json:"last_attempt_at,omitempty"`
	LastSuccessAt  string `json:"last_success_at,omitempty"`
	LastVerifiedAt string `json:"last_verified_at,omitempty"`
	LastError      string `json:"last_error,omitempty"`
	LeaderRequired bool   `json:"leader_required"`
	Timezone       string `json:"timezone"`
}

// BackupScheduleStatus is deliberately shared by GET and PUT so a schedule
// configuration is never returned as healthy when it failed to load/apply.
type BackupScheduleStatus struct {
	BackupScheduleConfig
	Runtime            BackupSchedulerRuntime `json:"runtime"`
	LastVerifiedBackup *BackupRecord          `json:"last_verified_backup,omitempty"`
	BackupSecretKey    BackupSecretKeyStatus  `json:"backup_secret_key"`
}

// BackupSchedulerAlertSink is a narrow adapter for Ops. The composition root
// can map it to its alert/event system without coupling backup execution to an
// Ops implementation; structured logs remain the safe fallback.
type BackupSchedulerAlertSink func(context.Context, BackupSchedulerRuntime)

// BackupDownloadPart 描述一个可下载的备份分卷。
type BackupDownloadPart struct {
	Index     int    `json:"index"`
	SizeBytes int64  `json:"size_bytes"`
	URL       string `json:"url"`
}

// BackupDownloadResponse 是单文件和分卷下载响应的兼容表示。
type BackupDownloadResponse struct {
	URL   string               `json:"url,omitempty"`
	Parts []BackupDownloadPart `json:"parts,omitempty"`
}

// BackupService 数据库备份恢复服务
type BackupService struct {
	restoreControl atomic.Value
	settingRepo    SettingRepository
	dbCfg          *config.DatabaseConfig
	encryptor      SecretEncryptor
	// encryptionKeyConfigured mirrors cfg.Totp.EncryptionKeyConfigured: false
	// means the secret encryption key was auto-generated and does not survive a
	// restart. Durable-secret writers must refuse to persist new secrets in that
	// mode (#4524).
	encryptionKeyConfigured bool
	backupSecretCryptor     *backupSecretCryptor
	secretPayloadRepo       BackupSecretPayloadRepository
	storeFactory            BackupObjectStoreFactory
	dumper                  DBDumper

	opMu      sync.Mutex // 保护 backingUp/restoring 标志
	backingUp bool
	restoring bool

	storeMu sync.Mutex // 保护 store/s3Cfg 缓存
	store   BackupObjectStore
	s3Cfg   *BackupS3Config

	recordsMu sync.Mutex // 保护 records 的 load/save 操作

	cronMu           sync.Mutex
	cronSched        *cron.Cron
	cronEntryID      cron.EntryID
	schedulerMu      sync.Mutex
	schedulerRuntime BackupSchedulerRuntime
	alertSink        BackupSchedulerAlertSink

	// lockCache/db elect a single leader for the scheduled backup across
	// instances; instanceID identifies this process as the lock owner. Injected
	// via SetLeaderLock — when both are nil the backup runs ungated
	// (single-instance / test behavior).
	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string

	wg            sync.WaitGroup     // 追踪活跃的备份/恢复 goroutine
	shuttingDown  atomic.Bool        // 阻止新备份启动
	bgCtx         context.Context    // 所有后台操作的 parent context
	bgCancel      context.CancelFunc // 取消所有活跃后台操作
	partSizeBytes int64              // 分卷阈值；生产使用 4 GiB，测试可注入更小值
}

func NewBackupService(
	settingRepo SettingRepository,
	cfg *config.Config,
	encryptor SecretEncryptor,
	storeFactory BackupObjectStoreFactory,
	dumper DBDumper,
) *BackupService {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	cryptor, _ := newBackupSecretCryptorFromEnv()
	return &BackupService{
		settingRepo:             settingRepo,
		dbCfg:                   &cfg.Database,
		encryptor:               encryptor,
		backupSecretCryptor:     cryptor,
		encryptionKeyConfigured: cfg.Totp.EncryptionKeyConfigured,
		storeFactory:            storeFactory,
		dumper:                  dumper,
		bgCtx:                   bgCtx,
		bgCancel:                bgCancel,
		partSizeBytes:           defaultBackupPartSizeBytes,
		instanceID:              uuid.NewString(),
		schedulerRuntime:        BackupSchedulerRuntime{Health: "unknown", LeaderRequired: true, Timezone: time.Local.String()},
	}
}

// SetSecretPayloadRepository is called by the composition root after migration
// 342 is available. Keeping it as a setter avoids changing shared Wire files in
// package branches while still making the dependency explicit and testable.
func (s *BackupService) SetSecretPayloadRepository(repo BackupSecretPayloadRepository) {
	if s == nil {
		return
	}
	s.secretPayloadRepo = repo
}

func (s *BackupService) SetSchedulerAlertSink(sink BackupSchedulerAlertSink) {
	if s != nil {
		s.alertSink = sink
	}
}

// SetLeaderLock injects the leader-lock cache and DB used to elect a single
// instance for the scheduled backup. When both are nil the scheduled backup runs
// ungated (single-instance / test behavior).
func (s *BackupService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

// Start 启动定时备份调度器并清理孤立记录
func (s *BackupService) Start() {
	if cryptor, err := newBackupSecretCryptorFromEnv(); err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] BACKUP_SECRET_ENCRYPTION_KEY is invalid; legacy backup secrets remain readable until it is corrected")
		s.backupSecretCryptor = &backupSecretCryptor{}
	} else {
		s.backupSecretCryptor = cryptor
		if !cryptor.configured() {
			logger.LegacyPrintf("service.backup", "[Backup] BACKUP_SECRET_ENCRYPTION_KEY is not configured; continuing with legacy TOTP-encrypted backup secrets; migration is required before changing credentials")
		} else if err := s.migrateLegacyBackupSecret(context.Background()); err != nil {
			logger.LegacyPrintf("service.backup", "[Backup] backup secret migration pending or failed; legacy fallback remains active")
		}
	}
	s.cronSched = cron.New()
	s.cronSched.Start()

	// A restored dump can contain its own running backup row. Bind the external journal before cleaning objects.
	if os.Getenv("OMNIHIVE_RESTORE_JOURNAL_DSN") == "" || s.boundRestoreControl() != nil {
		s.recoverStaleRecords()
	}

	// 加载已有的定时配置
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	schedule, err := s.GetSchedule(ctx)
	if err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 加载定时备份配置失败: %v", err)
		s.setSchedulerRuntime(ctx, func(runtime *BackupSchedulerRuntime) {
			runtime.Loaded = false
			runtime.Health = "error"
			runtime.LastError = "schedule configuration could not be loaded"
		})
		return
	}
	if schedule.Enabled && schedule.CronExpr != "" {
		if err := s.applyCronSchedule(schedule); err != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 应用定时备份配置失败: %v", err)
			s.setSchedulerRuntime(ctx, func(runtime *BackupSchedulerRuntime) {
				runtime.Loaded = false
				runtime.Health = "error"
				runtime.LastError = "schedule could not be applied"
			})
			return
		}
	}
	s.setSchedulerRuntime(ctx, func(runtime *BackupSchedulerRuntime) {
		runtime.Loaded = true
		runtime.Health = "healthy"
		runtime.LastError = ""
	})
}

// recoverStaleRecords 启动时将孤立的 running 记录标记为 failed，并清理已上传对象。
func (s *BackupService) recoverStaleRecords() {
	loadCtx, loadCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer loadCancel()

	records, err := s.loadRecords(loadCtx)
	if err != nil {
		return
	}
	for i := range records {
		if records[i].Status == "running" {
			staleRecord := records[i]
			if c := s.boundRestoreControl(); c != nil {
				completed, err := c.CompletedArchive(loadCtx, staleRecord.ID)
				if err != nil {
					logger.LegacyPrintf("service.backup", "[Backup] journal unavailable during backup recovery")
					continue
				}
				if completed != nil {
					completed.RestoreStatus, completed.RestoredAt, completed.RestoreError = staleRecord.RestoreStatus, staleRecord.RestoredAt, staleRecord.RestoreError
					if completed.RestoreStatus == "running" {
						completed.RestoreStatus = "review_required"
						completed.RestoreError = "restore outcome requires review after server restart"
					}
					s.saveRecoveredRecord(completed)
					continue
				}
			}
			records[i].Status = "failed"
			records[i].ErrorMsg = "interrupted by server restart"
			records[i].Progress = ""
			records[i].FinishedAt = time.Now().Format(time.RFC3339)
			s.saveRecoveredRecord(&records[i])

			if cleanupErr := s.cleanupStaleBackupObjects(&staleRecord); cleanupErr != nil {
				records[i].ErrorMsg = fmt.Sprintf("interrupted by server restart; cleanup failed, manual deletion may be required: %v", cleanupErr)
				s.saveRecoveredRecord(&records[i])
				logger.LegacyPrintf("service.backup", "[Backup] failed to clean stale backup objects for %s: %v", records[i].ID, cleanupErr)
			}
			logger.LegacyPrintf("service.backup", "[Backup] recovered stale running record: %s", records[i].ID)
		}
		if records[i].RestoreStatus == "running" {
			records[i].RestoreStatus = "review_required"
			records[i].RestoreError = "restore outcome requires review after server restart"
			s.saveRecoveredRecord(&records[i])
			logger.LegacyPrintf("service.backup", "[Backup] recovered stale restoring record: %s", records[i].ID)
		}
	}
}

func (s *BackupService) saveRecoveredRecord(record *BackupRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.saveRecord(ctx, record); err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 保存恢复后的备份记录失败 %s: %v", record.ID, err)
	}
}

func (s *BackupService) cleanupStaleBackupObjects(record *BackupRecord) error {
	if len(backupObjectKeys(record)) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), backupObjectCleanupTimeout)
	defer cancel()
	return s.deleteBackupObjects(ctx, record)
}

// Stop 停止定时备份并等待活跃操作完成
func (s *BackupService) Stop() {
	s.shuttingDown.Store(true)

	s.cronMu.Lock()
	if s.cronSched != nil {
		s.cronSched.Stop()
	}
	s.cronMu.Unlock()

	// 等待活跃备份/恢复完成（最多 5 分钟）
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		logger.LegacyPrintf("service.backup", "[Backup] all active operations finished")
	case <-time.After(5 * time.Minute):
		logger.LegacyPrintf("service.backup", "[Backup] shutdown timeout after 5min, cancelling active operations")
		if s.bgCancel != nil {
			s.bgCancel() // 取消所有后台操作
		}
		// 给 goroutine 时间响应取消并完成清理
		select {
		case <-done:
			logger.LegacyPrintf("service.backup", "[Backup] active operations cancelled and cleaned up")
		case <-time.After(10 * time.Second):
			logger.LegacyPrintf("service.backup", "[Backup] goroutine cleanup timed out")
		}
	}
}

// ─── S3 配置管理 ───

// EncryptionKeyConfigured reports whether a fixed (explicitly configured) secret
// encryption key is in use. When false the key is auto-generated on every start
// and secrets encrypted with it cannot be recovered after a restart, so callers
// that persist durable secrets must refuse to do so (#4524).
func (s *BackupService) EncryptionKeyConfigured() bool {
	// Compatibility for the image-storage service, whose separate migration is
	// outside this package. Backup S3 writes use backupSecretCryptor directly.
	return s != nil && s.encryptionKeyConfigured
}

func (s *BackupService) BackupSecretKeyStatus(ctx context.Context) BackupSecretKeyStatus {
	if s == nil || s.backupSecretCryptor == nil || !s.backupSecretCryptor.configured() {
		return BackupSecretKeyStatus{Source: "legacy_totp", MigrationRequired: true}
	}
	if s.secretPayloadRepo != nil {
		if envelope, err := s.secretPayloadRepo.Get(ctx, settingKeyBackupS3Config); err == nil && envelope != nil {
			return BackupSecretKeyStatus{Source: "backup_secret_encryption_key", Configured: true, KeyID: envelope.KeyID}
		}
	}
	return BackupSecretKeyStatus{Source: "backup_secret_encryption_key", Configured: true, MigrationRequired: true, KeyID: s.backupSecretCryptor.keyID()}
}

func (s *BackupService) GetS3Config(ctx context.Context) (*BackupS3Config, error) {
	cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &BackupS3Config{}, nil
	}
	// 脱敏返回
	cfg.SecretAccessKey = ""
	return cfg, nil
}

func (s *BackupService) UpdateS3Config(ctx context.Context, cfg BackupS3Config) (*BackupS3Config, error) {
	// 如果没提供 secret，保留原有值
	if cfg.SecretAccessKey == "" {
		old, err := s.loadS3Config(ctx)
		if err != nil {
			return nil, fmt.Errorf("load existing s3 config: %w", err)
		}
		if old != nil {
			cfg.SecretAccessKey = old.SecretAccessKey
		}
	}
	if cfg.SecretAccessKey != "" {
		if s.backupSecretCryptor == nil || !s.backupSecretCryptor.configured() {
			return nil, ErrBackupSecretKeyNotConfigured
		}
		if s.secretPayloadRepo == nil {
			return nil, infraerrors.ServiceUnavailable("BACKUP_SECRET_STORAGE_UNAVAILABLE", "backup secret envelope storage is not available")
		}
		// Keep the legacy ciphertext in the settings JSON through the rollback
		// window. The envelope is authoritative for v0.3.4 reads.
		legacyCiphertext, err := s.encryptor.Encrypt(cfg.SecretAccessKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt legacy rollback secret: %w", err)
		}
		envelope, err := s.backupSecretCryptor.seal(cfg.SecretAccessKey, backupS3SecretAAD)
		if err != nil {
			return nil, err
		}
		cfg.SecretAccessKey = legacyCiphertext
		data, err := json.Marshal(cfg)
		if err != nil {
			return nil, fmt.Errorf("marshal s3 config: %w", err)
		}
		if err := s.secretPayloadRepo.Save(ctx, settingKeyBackupS3Config, string(data), envelope); err != nil {
			return nil, fmt.Errorf("save backup secret envelopes: %w", err)
		}
	} else {
		data, err := json.Marshal(cfg)
		if err != nil {
			return nil, fmt.Errorf("marshal s3 config: %w", err)
		}
		if err := s.settingRepo.Set(ctx, settingKeyBackupS3Config, string(data)); err != nil {
			return nil, fmt.Errorf("save s3 config: %w", err)
		}
	}

	// 清除缓存的 S3 客户端
	s.storeMu.Lock()
	s.store = nil
	s.s3Cfg = nil
	s.storeMu.Unlock()

	cfg.SecretAccessKey = ""
	return &cfg, nil
}

func (s *BackupService) TestS3Connection(ctx context.Context, cfg BackupS3Config) error {
	// 如果没提供 secret，用已保存的
	if cfg.SecretAccessKey == "" {
		old, err := s.loadS3Config(ctx)
		if err != nil {
			return fmt.Errorf("load existing s3 config: %w", err)
		}
		if old != nil {
			cfg.SecretAccessKey = old.SecretAccessKey
		}
	}

	if cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return fmt.Errorf("incomplete S3 config: bucket, access_key_id, secret_access_key are required")
	}

	store, err := s.storeFactory(ctx, &cfg)
	if err != nil {
		return err
	}
	return store.HeadBucket(ctx)
}

// ─── 定时备份管理 ───

func (s *BackupService) GetSchedule(ctx context.Context) (*BackupScheduleConfig, error) {
	raw, err := s.settingRepo.GetValue(ctx, settingKeyBackupSchedule)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return nil, fmt.Errorf("load backup schedule: %w", err)
	}
	if raw == "" {
		return &BackupScheduleConfig{RetainDays: defaultBackupRetainDays}, nil
	}
	var cfg BackupScheduleConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, infraerrors.InternalServer("BACKUP_SCHEDULE_CORRUPT", "backup schedule data is corrupted")
	}
	return &cfg, nil
}

func (s *BackupService) GetScheduleStatus(ctx context.Context) (*BackupScheduleStatus, error) {
	cfg, err := s.GetSchedule(ctx)
	if err != nil {
		return nil, err
	}
	status := &BackupScheduleStatus{BackupScheduleConfig: *cfg, Runtime: s.getSchedulerRuntime(ctx), BackupSecretKey: s.BackupSecretKeyStatus(ctx)}
	s.applySchedulerAlertThreshold(cfg, &status.Runtime)
	if records, err := s.ListBackups(ctx); err == nil {
		for i := range records {
			if records[i].VerifiedAt == "" {
				continue
			}
			if status.LastVerifiedBackup == nil || records[i].VerifiedAt > status.LastVerifiedBackup.VerifiedAt {
				copy := records[i]
				status.LastVerifiedBackup = &copy
			}
		}
	}
	return status, nil
}

func (s *BackupService) applySchedulerAlertThreshold(cfg *BackupScheduleConfig, runtime *BackupSchedulerRuntime) {
	if cfg == nil || runtime == nil || !cfg.Enabled {
		return
	}
	if runtime.NextRunAt == "" {
		runtime.Health, runtime.LastError = "error", "schedule enabled but next run is unavailable"
		return
	}
	if runtime.LastSuccessAt == "" {
		return
	}
	lastSuccess, err := time.Parse(time.RFC3339, runtime.LastSuccessAt)
	if err != nil {
		return
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cfg.CronExpr)
	if err != nil {
		return
	}
	first := schedule.Next(lastSuccess)
	period := schedule.Next(first).Sub(first)
	threshold := 48 * time.Hour
	if period > 0 && 2*period > threshold {
		threshold = 2 * period
	}
	if time.Since(lastSuccess) > threshold {
		runtime.Health, runtime.LastError = "degraded", "no verified backup within the scheduler alert window"
	}
}

func (s *BackupService) UpdateSchedule(ctx context.Context, cfg BackupScheduleConfig) (*BackupScheduleConfig, error) {
	if cfg.Enabled && cfg.CronExpr == "" {
		return nil, infraerrors.BadRequest("INVALID_CRON", "cron expression is required when schedule is enabled")
	}
	// 验证 cron 表达式
	if cfg.CronExpr != "" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(cfg.CronExpr); err != nil {
			return nil, infraerrors.BadRequest("INVALID_CRON", fmt.Sprintf("invalid cron expression: %v", err))
		}
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal schedule config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, settingKeyBackupSchedule, string(data)); err != nil {
		return nil, fmt.Errorf("save schedule config: %w", err)
	}

	// 应用或停止定时任务
	if cfg.Enabled {
		if err := s.applyCronSchedule(&cfg); err != nil {
			s.setSchedulerRuntime(ctx, func(runtime *BackupSchedulerRuntime) {
				runtime.Loaded = false
				runtime.Health = "error"
				runtime.LastError = "schedule could not be applied"
			})
			return nil, err
		}
	} else {
		s.removeCronSchedule()
	}
	s.setSchedulerRuntime(ctx, func(runtime *BackupSchedulerRuntime) {
		runtime.Loaded = true
		runtime.Health = "healthy"
		runtime.LastError = ""
	})

	return &cfg, nil
}

func (s *BackupService) applyCronSchedule(cfg *BackupScheduleConfig) error {
	s.cronMu.Lock()
	defer s.cronMu.Unlock()

	if s.cronSched == nil {
		return fmt.Errorf("cron scheduler not initialized")
	}

	entryID, err := s.cronSched.AddFunc(cfg.CronExpr, func() {
		s.runScheduledBackup()
	})
	if err != nil {
		return infraerrors.BadRequest("INVALID_CRON", fmt.Sprintf("failed to schedule: %v", err))
	}
	if s.cronEntryID != 0 {
		s.cronSched.Remove(s.cronEntryID)
	}
	s.cronEntryID = entryID
	entry := s.cronSched.Entry(entryID)
	s.setSchedulerRuntime(context.Background(), func(runtime *BackupSchedulerRuntime) {
		runtime.Loaded = true
		runtime.Health = "healthy"
		runtime.NextRunAt = entry.Next.Format(time.RFC3339)
		runtime.LastError = ""
	})
	logger.LegacyPrintf("service.backup", "[Backup] 定时备份已启用: %s", cfg.CronExpr)
	return nil
}

func (s *BackupService) removeCronSchedule() {
	s.cronMu.Lock()
	defer s.cronMu.Unlock()
	if s.cronSched != nil && s.cronEntryID != 0 {
		s.cronSched.Remove(s.cronEntryID)
		s.cronEntryID = 0
		logger.LegacyPrintf("service.backup", "[Backup] 定时备份已停用")
	}
	s.setSchedulerRuntime(context.Background(), func(runtime *BackupSchedulerRuntime) {
		runtime.NextRunAt = ""
		runtime.Loaded = true
		runtime.Health = "healthy"
		runtime.LastError = ""
	})
}

func (s *BackupService) runScheduledBackup() {
	s.wg.Add(1)
	defer s.wg.Done()

	ctx, cancel := context.WithTimeout(s.bgCtx, 30*time.Minute)
	defer cancel()
	// 多实例保护: 集群部署时只让 leader 执行定时备份, 避免每个实例各自对同一个
	// 数据库跑一次全量 dump、上传时峰值内存翻倍、以及多份同名对象互相覆盖。
	// 手动触发的备份 (CreateBackup/StartBackup) 不受此限, 运维仍可随时在任一节点强制备份。
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, backupScheduledLeaderLockKey, s.instanceID, backupScheduledLeaderLockTTL)
	if !ok {
		logger.LegacyPrintf("service.backup", "[Backup] 定时备份跳过: 本实例非 leader")
		return
	}
	defer release()
	s.setSchedulerRuntimeGlobal(ctx, func(runtime *BackupSchedulerRuntime) {
		runtime.LastAttemptAt = time.Now().Format(time.RFC3339)
		runtime.LastError = ""
	})

	// 读取定时备份配置中的过期天数
	schedule, err := s.GetSchedule(ctx)
	if err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 加载备份配置失败: %v", err)
		s.setSchedulerRuntimeGlobal(ctx, func(runtime *BackupSchedulerRuntime) {
			runtime.Health = "error"
			runtime.LastError = "schedule configuration could not be loaded"
		})
		return
	}
	expireDays := schedule.RetainDays

	logger.LegacyPrintf("service.backup", "[Backup] 开始执行定时备份, 过期天数: %d", expireDays)
	record, err := s.CreateBackup(ctx, "scheduled", expireDays)
	if err != nil {
		if errors.Is(err, ErrBackupInProgress) {
			logger.LegacyPrintf("service.backup", "[Backup] 定时备份跳过: 已有备份正在进行中")
		} else {
			logger.LegacyPrintf("service.backup", "[Backup] 定时备份失败: %v", err)
		}
		s.setSchedulerRuntimeGlobal(ctx, func(runtime *BackupSchedulerRuntime) {
			runtime.Health = "error"
			runtime.LastError = "scheduled backup failed"
		})
		return
	}
	if record.VerifiedAt == "" {
		s.setSchedulerRuntimeGlobal(ctx, func(runtime *BackupSchedulerRuntime) {
			runtime.Health = "error"
			runtime.LastError = "scheduled backup completed without remote verification"
		})
		return
	}
	s.setSchedulerRuntimeGlobal(ctx, func(runtime *BackupSchedulerRuntime) {
		runtime.Health = "healthy"
		runtime.LastSuccessAt = record.FinishedAt
		runtime.LastVerifiedAt = record.VerifiedAt
		runtime.LastError = ""
	})
	logger.LegacyPrintf("service.backup", "[Backup] 定时备份完成: id=%s size=%d", record.ID, record.SizeBytes)

	// 清理过期备份（复用已加载的 schedule）
	if schedule == nil {
		return
	}
	if err := s.cleanupOldBackups(ctx, schedule); err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 清理过期备份失败: %v", err)
	}
}

// ─── 备份/恢复核心 ───

// CreateBackup 创建全量数据库备份并上传到 S3。
// expireDays: 备份过期天数，0=永不过期，默认14天
func (s *BackupService) CreateBackup(ctx context.Context, triggeredBy string, expireDays int) (*BackupRecord, error) {
	if s.shuttingDown.Load() {
		return nil, infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}

	s.opMu.Lock()
	if s.backingUp {
		s.opMu.Unlock()
		return nil, ErrBackupInProgress
	}
	s.backingUp = true
	s.opMu.Unlock()
	defer func() {
		s.opMu.Lock()
		s.backingUp = false
		s.opMu.Unlock()
	}()

	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	if s3Cfg == nil || !s3Cfg.IsConfigured() {
		return nil, ErrBackupS3NotConfigured
	}

	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return nil, fmt.Errorf("init object store: %w", err)
	}

	now := time.Now()
	backupID := uuid.New().String()[:8]
	fileName := fmt.Sprintf("%s_%s.sql.gz", s.dbCfg.DBName, now.Format("20060102_150405"))
	s3Key := s.buildS3Key(s3Cfg, fileName)

	var expiresAt string
	if expireDays > 0 {
		expiresAt = now.AddDate(0, 0, expireDays).Format(time.RFC3339)
	}

	record := &BackupRecord{
		ID:          backupID,
		Status:      "running",
		BackupType:  "postgres",
		FileName:    fileName,
		S3Key:       s3Key,
		TriggeredBy: triggeredBy,
		StartedAt:   now.Format(time.RFC3339),
		ExpiresAt:   expiresAt,
	}

	archivePath, sizeBytes, err := s.createCompressedBackupFile(ctx)
	if err != nil {
		record.Status = "failed"
		record.ErrorMsg = err.Error()
		record.FinishedAt = time.Now().Format(time.RFC3339)
		_ = s.saveRecord(ctx, record)
		return record, err
	}
	defer func() { _ = cleanupBackupFiles(archivePath) }()
	record.SizeBytes = sizeBytes
	if err := s.saveRecord(ctx, record); err != nil {
		return nil, fmt.Errorf("save initial record: %w", err)
	}
	if err := s.uploadBackupArchive(ctx, record, objectStore, s3Cfg, archivePath); err != nil {
		record.Status = "failed"
		record.ErrorMsg = err.Error()
		record.FinishedAt = time.Now().Format(time.RFC3339)
		_ = s.saveRecord(ctx, record)
		return record, err
	}
	if err := s.verifyRemoteBackup(ctx, record, objectStore); err != nil {
		record.Status = "failed"
		record.VerificationError = "remote backup verification failed"
		record.ErrorMsg = record.VerificationError
		record.FinishedAt = time.Now().Format(time.RFC3339)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), backupObjectCleanupTimeout)
		_ = deleteBackupObjectKeys(cleanupCtx, objectStore, record)
		cleanupCancel()
		_ = s.saveRecord(ctx, record)
		return record, err
	}

	record.Status = "completed"
	record.FinishedAt = time.Now().Format(time.RFC3339)
	if err := s.saveRecord(ctx, record); err != nil {
		return record, fmt.Errorf("save completed backup: %w", err)
	}

	return record, nil
}

// StartBackup 异步创建备份，立即返回 running 状态的记录
func (s *BackupService) StartBackup(ctx context.Context, triggeredBy string, expireDays int) (*BackupRecord, error) {
	if s.shuttingDown.Load() {
		return nil, infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}

	s.opMu.Lock()
	if s.backingUp {
		s.opMu.Unlock()
		return nil, ErrBackupInProgress
	}
	s.backingUp = true
	s.opMu.Unlock()

	// 初始化阶段出错时自动重置标志
	launched := false
	defer func() {
		if !launched {
			s.opMu.Lock()
			s.backingUp = false
			s.opMu.Unlock()
		}
	}()

	// 在返回前加载 S3 配置和创建 store，避免 goroutine 中配置被修改
	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	if s3Cfg == nil || !s3Cfg.IsConfigured() {
		return nil, ErrBackupS3NotConfigured
	}

	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return nil, fmt.Errorf("init object store: %w", err)
	}

	now := time.Now()
	backupID := uuid.New().String()[:8]
	fileName := fmt.Sprintf("%s_%s.sql.gz", s.dbCfg.DBName, now.Format("20060102_150405"))
	s3Key := s.buildS3Key(s3Cfg, fileName)

	var expiresAt string
	if expireDays > 0 {
		expiresAt = now.AddDate(0, 0, expireDays).Format(time.RFC3339)
	}

	record := &BackupRecord{
		ID:          backupID,
		Status:      "running",
		BackupType:  "postgres",
		FileName:    fileName,
		S3Key:       s3Key,
		TriggeredBy: triggeredBy,
		StartedAt:   now.Format(time.RFC3339),
		ExpiresAt:   expiresAt,
		Progress:    "pending",
	}

	if err := s.saveRecord(ctx, record); err != nil {
		return nil, fmt.Errorf("save initial record: %w", err)
	}

	launched = true
	// 在启动 goroutine 前完成拷贝，避免数据竞争
	result := *record

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.opMu.Lock()
			s.backingUp = false
			s.opMu.Unlock()
		}()
		defer func() {
			if r := recover(); r != nil {
				logger.LegacyPrintf("service.backup", "[Backup] panic recovered: %v", r)
				record.Status = "failed"
				record.ErrorMsg = fmt.Sprintf("internal panic: %v", r)
				record.Progress = ""
				record.FinishedAt = time.Now().Format(time.RFC3339)
				_ = s.saveRecord(context.Background(), record)
			}
		}()
		s.executeBackup(record, objectStore, s3Cfg)
	}()

	return &result, nil
}

// executeBackup 后台执行备份（独立于 HTTP context）
func (s *BackupService) executeBackup(record *BackupRecord, objectStore BackupObjectStore, s3Cfg *BackupS3Config) {
	ctx, cancel := context.WithTimeout(s.bgCtx, 30*time.Minute)
	defer cancel()

	// 阶段1: pg_dump -> gzip 临时文件
	record.Progress = "dumping"
	_ = s.saveRecord(ctx, record)
	archivePath, sizeBytes, err := s.createCompressedBackupFile(ctx)
	if err != nil {
		record.Status = "failed"
		record.ErrorMsg = err.Error()
		record.Progress = ""
		record.FinishedAt = time.Now().Format(time.RFC3339)
		_ = s.saveRecord(context.Background(), record)
		return
	}
	defer func() { _ = cleanupBackupFiles(archivePath) }()
	record.SizeBytes = sizeBytes

	// 阶段2: 单对象或分卷上传
	record.Progress = "uploading"
	_ = s.saveRecord(ctx, record)
	if err := s.uploadBackupArchive(ctx, record, objectStore, s3Cfg, archivePath); err != nil {
		record.Status = "failed"
		record.ErrorMsg = err.Error()
		record.Progress = ""
		record.FinishedAt = time.Now().Format(time.RFC3339)
		_ = s.saveRecord(context.Background(), record)
		return
	}
	if err := s.verifyRemoteBackup(ctx, record, objectStore); err != nil {
		record.Status = "failed"
		record.ErrorMsg = "remote backup verification failed"
		record.VerificationError = record.ErrorMsg
		record.Progress = ""
		record.FinishedAt = time.Now().Format(time.RFC3339)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), backupObjectCleanupTimeout)
		_ = deleteBackupObjectKeys(cleanupCtx, objectStore, record)
		cleanupCancel()
		_ = s.saveRecord(context.Background(), record)
		return
	}

	record.SizeBytes = sizeBytes
	record.Status = "completed"
	record.Progress = ""
	record.FinishedAt = time.Now().Format(time.RFC3339)
	if err := s.saveRecord(context.Background(), record); err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 保存备份记录失败: %v", err)
	}
}

func (s *BackupService) createCompressedBackupFile(ctx context.Context) (string, int64, error) {
	dumpReader, err := s.dumper.Dump(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("pg_dump: %w", err)
	}
	archive, err := os.CreateTemp("", "omnihive-backup-*.sql.gz")
	if err != nil {
		_ = dumpReader.Close()
		return "", 0, fmt.Errorf("create backup archive: %w", err)
	}
	archivePath := archive.Name()

	gzWriter := gzip.NewWriter(archive)
	_, copyErr := io.Copy(gzWriter, dumpReader)
	if closeErr := gzWriter.Close(); copyErr == nil && closeErr != nil {
		copyErr = closeErr
	}
	if closeErr := dumpReader.Close(); copyErr == nil && closeErr != nil {
		copyErr = closeErr
	}
	if closeErr := archive.Close(); copyErr == nil && closeErr != nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = cleanupBackupFiles(archivePath)
		return "", 0, fmt.Errorf("gzip/dump failed: %w", copyErr)
	}

	info, err := os.Stat(archivePath)
	if err != nil {
		_ = cleanupBackupFiles(archivePath)
		return "", 0, fmt.Errorf("stat backup archive: %w", err)
	}
	return archivePath, info.Size(), nil
}

func (s *BackupService) uploadBackupArchive(ctx context.Context, record *BackupRecord, objectStore BackupObjectStore, cfg *BackupS3Config, archivePath string) error {
	info, err := os.Stat(archivePath)
	if err != nil {
		return fmt.Errorf("stat backup archive: %w", err)
	}
	partSize := s.partSizeBytes
	if partSize <= 0 {
		partSize = defaultBackupPartSizeBytes
	}
	if info.Size() <= partSize {
		if _, err := objectStore.UploadFile(ctx, record.S3Key, archivePath, "application/gzip"); err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), backupObjectCleanupTimeout)
			cleanupErr := deleteBackupObjectKeys(cleanupCtx, objectStore, record)
			cleanupCancel()
			return errors.Join(fmt.Errorf("backup upload: %w", err), cleanupErr)
		}
		record.Parts = nil
		checksum, err := backupFileSHA256(archivePath)
		if err != nil {
			return fmt.Errorf("checksum backup archive: %w", err)
		}
		record.Checksum = checksum
		return nil
	}

	localParts, err := splitBackupFile(archivePath, partSize)
	if err != nil {
		return fmt.Errorf("split backup archive: %w", err)
	}
	defer func() {
		paths := make([]string, 0, len(localParts))
		for _, part := range localParts {
			paths = append(paths, part.Path)
		}
		_ = cleanupBackupFiles(paths...)
	}()
	if cfg == nil {
		return errors.New("backup S3 config is unavailable for split upload")
	}

	record.S3Key = ""
	record.Parts = make([]BackupPart, 0, len(localParts))
	partRoot := strings.TrimRight(s.buildS3Key(cfg, record.ID), "/")
	for _, part := range localParts {
		record.Parts = append(record.Parts, BackupPart{
			Index:     part.Index,
			S3Key:     s.buildBackupPartKey(partRoot, part.Index),
			SizeBytes: part.SizeBytes,
			SHA256:    part.SHA256,
		})
	}
	if err := s.saveRecord(ctx, record); err != nil {
		return fmt.Errorf("save split backup plan: %w", err)
	}
	for i, part := range localParts {
		if _, err := objectStore.UploadFile(ctx, record.Parts[i].S3Key, part.Path, "application/octet-stream"); err != nil {
			// PUT 可能已经在对象存储端成功、但客户端因超时收到错误；
			// 因此失败时清理整份分卷计划，而不只清理此前返回成功的卷。
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), backupObjectCleanupTimeout)
			cleanupErr := deleteBackupObjectKeys(cleanupCtx, objectStore, record)
			cleanupCancel()
			return errors.Join(fmt.Errorf("upload backup part %d: %w", part.Index, err), cleanupErr)
		}
	}
	return nil
}

// verifyRemoteBackup makes upload success meaningful: every object is read back
// from the remote store and checked against the locally calculated size/hash.
// A completed record is therefore safe to use as a release preflight backup.
func (s *BackupService) verifyRemoteBackup(ctx context.Context, record *BackupRecord, objectStore BackupObjectStore) error {
	if record == nil {
		return errors.New("backup record is required")
	}
	verify := func(key, wantChecksum string, wantSize int64) error {
		body, err := objectStore.Download(ctx, key)
		if err != nil {
			return fmt.Errorf("remote object %q is unavailable: %w", key, err)
		}
		defer func() { _ = body.Close() }()
		hash := sha256.New()
		n, err := io.Copy(hash, body)
		if err != nil {
			return fmt.Errorf("read remote object %q: %w", key, err)
		}
		if wantSize > 0 && n != wantSize {
			return fmt.Errorf("remote object %q size mismatch", key)
		}
		if wantChecksum == "" || hex.EncodeToString(hash.Sum(nil)) != wantChecksum {
			return fmt.Errorf("remote object %q checksum mismatch", key)
		}
		return nil
	}
	if len(record.Parts) > 0 {
		for _, part := range record.Parts {
			if err := verify(part.S3Key, part.SHA256, part.SizeBytes); err != nil {
				return err
			}
		}
	} else if err := verify(record.S3Key, record.Checksum, record.SizeBytes); err != nil {
		return err
	}
	record.VerifiedAt = time.Now().Format(time.RFC3339)
	record.VerificationError = ""
	return nil
}

func backupFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *BackupService) buildBackupPartKey(root string, index int) string {
	return fmt.Sprintf("%s/payload.part-%06d", strings.TrimRight(root, "/"), index)
}

// RestoreBackup 从 S3 下载备份并流式恢复到数据库
func (s *BackupService) RestoreBackup(ctx context.Context, backupID string) error {
	if s.db != nil || s.boundRestoreControl() != nil {
		return infraerrors.Forbidden("RESTORE_AUTHORIZATION_REQUIRED", "Use the authorized restore operation.")
	}
	s.opMu.Lock()
	if s.restoring {
		s.opMu.Unlock()
		return ErrRestoreInProgress
	}
	s.restoring = true
	s.opMu.Unlock()
	defer func() {
		s.opMu.Lock()
		s.restoring = false
		s.opMu.Unlock()
	}()

	record, err := s.GetBackupRecord(ctx, backupID)
	if err != nil {
		return err
	}
	if record.Status != "completed" {
		return infraerrors.BadRequest("BACKUP_NOT_COMPLETED", "can only restore from a completed backup")
	}

	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return err
	}
	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return fmt.Errorf("init object store: %w", err)
	}

	if len(record.Parts) > 0 {
		archivePath, err := s.downloadBackupParts(ctx, objectStore, record.Parts)
		if err != nil {
			return err
		}
		defer func() { _ = cleanupBackupFiles(archivePath) }()
		return s.restoreArchive(ctx, archivePath)
	}

	// 旧记录从 S3 流式下载
	body, err := objectStore.Download(ctx, record.S3Key)
	if err != nil {
		return fmt.Errorf("S3 download failed: %w", err)
	}
	defer func() { _ = body.Close() }()

	// 流式解压 gzip -> psql（不将全部数据加载到内存）
	gzReader, err := gzip.NewReader(body)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	// 流式恢复
	if err := s.dumper.Restore(ctx, gzReader); err != nil {
		return fmt.Errorf("pg restore: %w", err)
	}

	return nil
}

// StartRestore 异步恢复备份，立即返回
func (s *BackupService) StartRestore(ctx context.Context, backupID string) (*BackupRecord, error) {
	if s.db != nil || s.boundRestoreControl() != nil {
		return nil, infraerrors.Forbidden("RESTORE_AUTHORIZATION_REQUIRED", "Use the authorized restore operation.")
	}
	if s.shuttingDown.Load() {
		return nil, infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}

	s.opMu.Lock()
	if s.restoring {
		s.opMu.Unlock()
		return nil, ErrRestoreInProgress
	}
	s.restoring = true
	s.opMu.Unlock()

	// 初始化阶段出错时自动重置标志
	launched := false
	defer func() {
		if !launched {
			s.opMu.Lock()
			s.restoring = false
			s.opMu.Unlock()
		}
	}()

	record, err := s.GetBackupRecord(ctx, backupID)
	if err != nil {
		return nil, err
	}
	if record.Status != "completed" {
		return nil, infraerrors.BadRequest("BACKUP_NOT_COMPLETED", "can only restore from a completed backup")
	}

	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return nil, fmt.Errorf("init object store: %w", err)
	}

	record.RestoreStatus = "running"
	_ = s.saveRecord(ctx, record)

	launched = true
	result := *record

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.opMu.Lock()
			s.restoring = false
			s.opMu.Unlock()
		}()
		defer func() {
			if r := recover(); r != nil {
				logger.LegacyPrintf("service.backup", "[Backup] restore panic recovered: %v", r)
				record.RestoreStatus = "failed"
				record.RestoreError = fmt.Sprintf("internal panic: %v", r)
				_ = s.saveRecord(context.Background(), record)
			}
		}()
		s.executeRestore(record, objectStore)
	}()

	return &result, nil
}

// executeRestore 后台执行恢复
func (s *BackupService) executeRestore(record *BackupRecord, objectStore BackupObjectStore) {
	ctx, cancel := context.WithTimeout(s.bgCtx, 30*time.Minute)
	defer cancel()

	if len(record.Parts) > 0 {
		archivePath, err := s.downloadBackupParts(ctx, objectStore, record.Parts)
		if err != nil {
			record.RestoreStatus = "failed"
			record.RestoreError = err.Error()
			_ = s.saveRecord(context.Background(), record)
			return
		}
		defer func() { _ = cleanupBackupFiles(archivePath) }()
		if err := s.restoreArchive(ctx, archivePath); err != nil {
			record.RestoreStatus = "failed"
			record.RestoreError = fmt.Sprintf("pg restore: %v", err)
			_ = s.saveRecord(context.Background(), record)
			return
		}
		record.RestoreStatus = "completed"
		record.RestoredAt = time.Now().Format(time.RFC3339)
		if err := s.saveRecord(context.Background(), record); err != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 保存恢复记录失败: %v", err)
		}
		return
	}

	body, err := objectStore.Download(ctx, record.S3Key)
	if err != nil {
		record.RestoreStatus = "failed"
		record.RestoreError = fmt.Sprintf("S3 download failed: %v", err)
		_ = s.saveRecord(context.Background(), record)
		return
	}
	defer func() { _ = body.Close() }()

	gzReader, err := gzip.NewReader(body)
	if err != nil {
		record.RestoreStatus = "failed"
		record.RestoreError = fmt.Sprintf("gzip reader: %v", err)
		_ = s.saveRecord(context.Background(), record)
		return
	}
	defer func() { _ = gzReader.Close() }()

	if err := s.dumper.Restore(ctx, gzReader); err != nil {
		record.RestoreStatus = "failed"
		record.RestoreError = fmt.Sprintf("pg restore: %v", err)
		_ = s.saveRecord(context.Background(), record)
		return
	}

	record.RestoreStatus = "completed"
	record.RestoredAt = time.Now().Format(time.RFC3339)
	if err := s.saveRecord(context.Background(), record); err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 保存恢复记录失败: %v", err)
	}
}

func (s *BackupService) downloadBackupParts(ctx context.Context, objectStore BackupObjectStore, parts []BackupPart) (path string, err error) {
	if len(parts) == 0 {
		return "", errors.New("backup parts are empty")
	}
	ordered := append([]BackupPart(nil), parts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Index < ordered[j].Index })
	for i, part := range ordered {
		if part.Index != i+1 || part.S3Key == "" || part.SizeBytes <= 0 {
			return "", fmt.Errorf("invalid backup part metadata at index %d", i+1)
		}
	}

	archive, err := os.CreateTemp("", "omnihive-restore-*.sql.gz")
	if err != nil {
		return "", fmt.Errorf("create restore archive: %w", err)
	}
	path = archive.Name()
	cleanup := func() {
		_ = archive.Close()
		_ = cleanupBackupFiles(path)
	}

	for _, part := range ordered {
		body, downloadErr := objectStore.Download(ctx, part.S3Key)
		if downloadErr != nil {
			cleanup()
			return "", fmt.Errorf("download backup part %d: %w", part.Index, downloadErr)
		}
		hash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(archive, hash), body)
		closeErr := body.Close()
		if copyErr != nil {
			cleanup()
			return "", fmt.Errorf("read backup part %d: %w", part.Index, copyErr)
		}
		if closeErr != nil {
			cleanup()
			return "", fmt.Errorf("close backup part %d: %w", part.Index, closeErr)
		}
		if written != part.SizeBytes {
			cleanup()
			return "", fmt.Errorf("backup part %d size mismatch: got %d, want %d", part.Index, written, part.SizeBytes)
		}
		if part.SHA256 != "" && !strings.EqualFold(part.SHA256, hex.EncodeToString(hash.Sum(nil))) {
			cleanup()
			return "", fmt.Errorf("backup part %d checksum mismatch", part.Index)
		}
	}
	if err := archive.Close(); err != nil {
		_ = cleanupBackupFiles(path)
		return "", fmt.Errorf("close restore archive: %w", err)
	}
	return path, nil
}

func (s *BackupService) restoreArchive(ctx context.Context, archivePath string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open restore archive: %w", err)
	}
	defer func() { _ = archive.Close() }()

	gzReader, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gzReader.Close() }()
	if err := s.dumper.Restore(ctx, gzReader); err != nil {
		return fmt.Errorf("pg restore: %w", err)
	}
	return nil
}

// ─── 备份记录管理 ───

func (s *BackupService) ListBackups(ctx context.Context) ([]BackupRecord, error) {
	records, err := s.loadRecords(ctx)
	if err != nil {
		return nil, err
	}
	// 倒序返回（最新在前）
	sort.Slice(records, func(i, j int) bool {
		return backupStartedAfter(records[i], records[j])
	})
	if control := s.boundRestoreControl(); control != nil {
		return control.Project(ctx, records, "")
	}
	return records, nil
}

func (s *BackupService) GetBackupRecord(ctx context.Context, backupID string) (*BackupRecord, error) {
	records, err := s.loadRecords(ctx)
	if err != nil {
		return nil, err
	}
	if control := s.boundRestoreControl(); control != nil {
		records, err = control.Project(ctx, records, backupID)
		if err != nil {
			return nil, err
		}
	}
	for i := range records {
		if records[i].ID == backupID {
			return &records[i], nil
		}
	}
	return nil, ErrBackupNotFound
}

func (s *BackupService) DeleteBackup(ctx context.Context, backupID string) error {
	s.recordsMu.Lock()
	defer s.recordsMu.Unlock()

	records, err := s.loadRecordsLocked(ctx)
	if err != nil {
		return err
	}

	var found *BackupRecord
	var remaining []BackupRecord
	for i := range records {
		if records[i].ID == backupID {
			found = &records[i]
		} else {
			remaining = append(remaining, records[i])
		}
	}
	control := s.boundRestoreControl()
	if found == nil && control != nil {
		historical, err := control.Project(ctx, nil, backupID)
		if err != nil {
			return err
		}
		if len(historical) > 0 {
			found = &historical[0]
		}
	}
	if found == nil {
		return ErrBackupNotFound
	}
	if found.Status == "running" {
		// 后台上传仍可能依赖 Parts 计划；删除对象会让随后完成的记录引用失效卷。
		return ErrBackupInProgress
	}
	if s.db != nil {
		// Persist the terminal tombstone before deleting remote data. A worker
		// holding an older attempt/revision can no longer turn this row back into
		// a completed record if it finishes after the object is gone.
		if err := s.tombstoneBackupRecord(ctx, found); err != nil {
			return err
		}
	}

	// 从对象存储删除所有单文件或分卷对象。 Database-backed rows are already
	// tombstoned, deliberately favoring no false restorable evidence over a
	// stale worker resurrecting a record when remote deletion is interrupted.
	if err := s.deleteBackupObjects(ctx, found); err != nil {
		return err
	}

	if control != nil {
		if err := control.Forget(ctx, backupID); err != nil {
			return err
		}
	}
	if s.db != nil {
		return nil
	}
	return s.saveRecordsLocked(ctx, remaining)
}

// GetBackupDownloadURL 获取备份文件预签名下载 URL
func (s *BackupService) GetBackupDownloadURL(ctx context.Context, backupID string) (BackupDownloadResponse, error) {
	var download BackupDownloadResponse
	record, err := s.GetBackupRecord(ctx, backupID)
	if err != nil {
		return download, err
	}
	if record.Status != "completed" {
		return download, infraerrors.BadRequest("BACKUP_NOT_COMPLETED", "backup is not completed")
	}

	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return download, err
	}
	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return download, err
	}

	if len(record.Parts) > 0 {
		parts := append([]BackupPart(nil), record.Parts...)
		sort.Slice(parts, func(i, j int) bool { return parts[i].Index < parts[j].Index })
		for i, part := range parts {
			if part.Index != i+1 || part.S3Key == "" || part.SizeBytes <= 0 {
				return download, fmt.Errorf("invalid backup part metadata at index %d", i+1)
			}
			url, presignErr := objectStore.PresignURL(ctx, part.S3Key, 1*time.Hour)
			if presignErr != nil {
				return download, fmt.Errorf("presign backup part %d: %w", part.Index, presignErr)
			}
			download.Parts = append(download.Parts, BackupDownloadPart{
				Index:     part.Index,
				SizeBytes: part.SizeBytes,
				URL:       url,
			})
		}
		return download, nil
	}
	if record.S3Key == "" {
		return download, errors.New("backup object key is empty")
	}
	url, err := objectStore.PresignURL(ctx, record.S3Key, 1*time.Hour)
	if err != nil {
		return download, fmt.Errorf("presign url: %w", err)
	}
	download.URL = url
	return download, nil
}

// ─── 内部方法 ───

func (s *BackupService) loadS3Config(ctx context.Context) (*BackupS3Config, error) {
	raw, err := s.settingRepo.GetValue(ctx, settingKeyBackupS3Config)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("load backup s3 config: %w", err)
	}
	if raw == "" {
		// An envelope without its non-secret config envelope is inconsistent;
		// check storage before treating this as a confirmed no-secret state.
		if s.secretPayloadRepo != nil {
			envelope, envelopeErr := s.secretPayloadRepo.Get(ctx, settingKeyBackupS3Config)
			if envelopeErr != nil {
				return nil, fmt.Errorf("load backup secret envelope: %w", envelopeErr)
			}
			if envelope != nil {
				return nil, ErrBackupS3ConfigCorrupt
			}
		}
		return nil, nil //nolint:nilnil // confirmed no config is a valid state
	}
	var cfg BackupS3Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, ErrBackupS3ConfigCorrupt
	}
	if s.secretPayloadRepo != nil {
		envelope, envelopeErr := s.secretPayloadRepo.Get(ctx, settingKeyBackupS3Config)
		if envelopeErr != nil {
			return nil, fmt.Errorf("load backup secret envelope: %w", envelopeErr)
		}
		if envelope != nil {
			plaintext, err := s.backupSecretCryptor.open(*envelope, backupS3SecretAAD)
			if err != nil {
				return nil, ErrBackupSecretUnreadable
			}
			cfg.SecretAccessKey = plaintext
			return &cfg, nil
		}
	}
	// Legacy fallback is intentionally narrow: legacy TOTP ciphertext is still
	// accepted for an upgrade/rollback window, but an authentication failure is
	// never reinterpreted as plaintext.
	if cfg.SecretAccessKey != "" {
		decrypted, err := s.encryptor.Decrypt(cfg.SecretAccessKey)
		if err != nil {
			return nil, ErrBackupSecretUnreadable
		}
		cfg.SecretAccessKey = decrypted
	}
	return &cfg, nil
}

func (s *BackupService) migrateLegacyBackupSecret(ctx context.Context) error {
	if s == nil || s.secretPayloadRepo == nil || s.backupSecretCryptor == nil || !s.backupSecretCryptor.configured() {
		return nil
	}
	envelope, err := s.secretPayloadRepo.Get(ctx, settingKeyBackupS3Config)
	if err != nil {
		return err
	}
	if envelope != nil {
		return nil
	}
	raw, err := s.settingRepo.GetValue(ctx, settingKeyBackupS3Config)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	var legacy BackupS3Config
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		return ErrBackupS3ConfigCorrupt
	}
	if legacy.SecretAccessKey == "" {
		return nil
	}
	plaintext, err := s.encryptor.Decrypt(legacy.SecretAccessKey)
	if err != nil {
		_ = s.secretPayloadRepo.SetMigrationState(ctx, settingKeyBackupS3Config, "error", s.backupSecretCryptor.keyID(), backupSecretMigrationMessage(err))
		return ErrBackupSecretUnreadable
	}
	newEnvelope, err := s.backupSecretCryptor.seal(plaintext, backupS3SecretAAD)
	if err != nil {
		return err
	}
	if err := s.secretPayloadRepo.Save(ctx, settingKeyBackupS3Config, raw, newEnvelope); err != nil {
		return err
	}
	return nil
}

func (s *BackupService) getOrCreateStore(ctx context.Context, cfg *BackupS3Config) (BackupObjectStore, error) {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()

	if s.store != nil && s.s3Cfg != nil {
		return s.store, nil
	}

	if cfg == nil {
		return nil, ErrBackupS3NotConfigured
	}

	store, err := s.storeFactory(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s.store = store
	s.s3Cfg = cfg
	return store, nil
}

func (s *BackupService) buildS3Key(cfg *BackupS3Config, fileName string) string {
	prefix := strings.TrimRight(cfg.Prefix, "/")
	if prefix == "" {
		prefix = "backups"
	}
	// Scope new objects to a deterministic deployment identity. This prevents
	// two databases that accidentally share a bucket/prefix from overwriting one
	// another; existing record keys remain untouched for restore compatibility.
	identity := "unknown"
	if s != nil && s.dbCfg != nil {
		sum := sha256.Sum256([]byte(s.dbCfg.Host + "\x00" + s.dbCfg.DBName))
		identity = hex.EncodeToString(sum[:])[:12]
	}
	return fmt.Sprintf("%s/%s/%s/%s", prefix, identity, time.Now().Format("2006/01/02"), fileName)
}

func (s *BackupService) getSchedulerRuntime(ctx context.Context) BackupSchedulerRuntime {
	s.schedulerMu.Lock()
	runtime := s.schedulerRuntime
	s.schedulerMu.Unlock()
	if runtime.Timezone == "" {
		runtime.Timezone = time.Local.String()
	}
	if s == nil || s.db == nil {
		return runtime
	}
	var nextRun, lastAttempt, lastSuccess, lastVerified sql.NullTime
	var lastError sql.NullString
	var loaded bool
	var health string
	err := s.db.QueryRowContext(ctx, `SELECT loaded, health, next_run_at, last_attempt_at, last_success_at, last_verified_at, last_error FROM backup_scheduler_state WHERE state_key = 'backup_scheduler'`).Scan(&loaded, &health, &nextRun, &lastAttempt, &lastSuccess, &lastVerified, &lastError)
	if err != nil {
		return runtime
	}
	runtime.Loaded, runtime.Health = loaded, health
	if nextRun.Valid {
		runtime.NextRunAt = nextRun.Time.Format(time.RFC3339)
	}
	if lastAttempt.Valid {
		runtime.LastAttemptAt = lastAttempt.Time.Format(time.RFC3339)
	}
	if lastSuccess.Valid {
		runtime.LastSuccessAt = lastSuccess.Time.Format(time.RFC3339)
	}
	if lastVerified.Valid {
		runtime.LastVerifiedAt = lastVerified.Time.Format(time.RFC3339)
	}
	if lastError.Valid {
		runtime.LastError = lastError.String
	}
	return runtime
}

func (s *BackupService) setSchedulerRuntime(ctx context.Context, update func(*BackupSchedulerRuntime)) {
	s.setSchedulerRuntimeWithPersistence(ctx, false, update)
}

// setSchedulerRuntimeGlobal is reserved for the instance that won the
// scheduled-backup leader lock. Startup/configuration observations remain
// local, so a follower cannot overwrite healthy evidence from the leader.
func (s *BackupService) setSchedulerRuntimeGlobal(ctx context.Context, update func(*BackupSchedulerRuntime)) {
	s.setSchedulerRuntimeWithPersistence(ctx, true, update)
}

func (s *BackupService) setSchedulerRuntimeWithPersistence(ctx context.Context, persist bool, update func(*BackupSchedulerRuntime)) {
	if s == nil {
		return
	}
	s.schedulerMu.Lock()
	update(&s.schedulerRuntime)
	runtime := s.schedulerRuntime
	if runtime.Timezone == "" {
		runtime.Timezone = time.Local.String()
		s.schedulerRuntime.Timezone = runtime.Timezone
	}
	s.schedulerMu.Unlock()
	if runtime.Health == "error" || runtime.Health == "degraded" {
		logger.LegacyPrintf("service.backup", "[Backup] scheduler alert health=%s error=%s", runtime.Health, runtime.LastError)
		if s.alertSink != nil {
			s.alertSink(ctx, runtime)
		}
	}
	if !persist || s.db == nil {
		return
	}
	parse := func(value string) any {
		if value == "" {
			return nil
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil
		}
		return parsed
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO backup_scheduler_state (state_key, loaded, health, next_run_at, last_attempt_at, last_success_at, last_verified_at, last_error, updated_at)
VALUES ('backup_scheduler', $1, $2, $3, $4, $5, $6, NULLIF($7, ''), now())
ON CONFLICT (state_key) DO UPDATE SET
loaded = CASE WHEN EXCLUDED.last_attempt_at IS NULL OR backup_scheduler_state.last_attempt_at IS NULL OR EXCLUDED.last_attempt_at >= backup_scheduler_state.last_attempt_at THEN EXCLUDED.loaded ELSE backup_scheduler_state.loaded END,
health = CASE WHEN EXCLUDED.last_attempt_at IS NULL OR backup_scheduler_state.last_attempt_at IS NULL OR EXCLUDED.last_attempt_at >= backup_scheduler_state.last_attempt_at THEN EXCLUDED.health ELSE backup_scheduler_state.health END,
next_run_at = CASE WHEN EXCLUDED.last_attempt_at IS NULL OR backup_scheduler_state.last_attempt_at IS NULL OR EXCLUDED.last_attempt_at >= backup_scheduler_state.last_attempt_at THEN EXCLUDED.next_run_at ELSE backup_scheduler_state.next_run_at END,
last_attempt_at = CASE WHEN backup_scheduler_state.last_attempt_at IS NULL OR (EXCLUDED.last_attempt_at IS NOT NULL AND EXCLUDED.last_attempt_at > backup_scheduler_state.last_attempt_at) THEN EXCLUDED.last_attempt_at ELSE backup_scheduler_state.last_attempt_at END,
last_success_at = CASE WHEN backup_scheduler_state.last_success_at IS NULL OR (EXCLUDED.last_success_at IS NOT NULL AND EXCLUDED.last_success_at > backup_scheduler_state.last_success_at) THEN EXCLUDED.last_success_at ELSE backup_scheduler_state.last_success_at END,
last_verified_at = CASE WHEN backup_scheduler_state.last_verified_at IS NULL OR (EXCLUDED.last_verified_at IS NOT NULL AND EXCLUDED.last_verified_at > backup_scheduler_state.last_verified_at) THEN EXCLUDED.last_verified_at ELSE backup_scheduler_state.last_verified_at END,
last_error = CASE WHEN EXCLUDED.last_attempt_at IS NULL OR backup_scheduler_state.last_attempt_at IS NULL OR EXCLUDED.last_attempt_at >= backup_scheduler_state.last_attempt_at THEN EXCLUDED.last_error ELSE backup_scheduler_state.last_error END,
updated_at = now()`, runtime.Loaded, runtime.Health, parse(runtime.NextRunAt), parse(runtime.LastAttemptAt), parse(runtime.LastSuccessAt), parse(runtime.LastVerifiedAt), runtime.LastError)
	if err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] scheduler health persistence failed")
	}
}

// loadRecords 加载备份记录，区分"无数据"和"数据损坏"
func (s *BackupService) loadRecords(ctx context.Context) ([]BackupRecord, error) {
	s.recordsMu.Lock()
	defer s.recordsMu.Unlock()
	return s.loadRecordsLocked(ctx)
}

// loadRecordsLocked 在已持有 recordsMu 锁的情况下加载记录
func (s *BackupService) loadRecordsLocked(ctx context.Context) ([]BackupRecord, error) {
	if s.db != nil {
		rows, err := s.db.QueryContext(ctx, `SELECT record, attempt_id, revision FROM backup_jobs WHERE status <> 'deleted' ORDER BY started_at DESC`)
		if err != nil {
			return nil, fmt.Errorf("load backup jobs: %w", err)
		}
		defer func() { _ = rows.Close() }()
		var records []BackupRecord
		for rows.Next() {
			var raw []byte
			var record BackupRecord
			if err := rows.Scan(&raw, &record.AttemptID, &record.Revision); err != nil {
				return nil, fmt.Errorf("scan backup job: %w", err)
			}
			if err := json.Unmarshal(raw, &record); err != nil {
				return nil, ErrBackupRecordsCorrupt
			}
			records = append(records, record)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate backup jobs: %w", err)
		}
		return records, nil
	}
	raw, err := s.settingRepo.GetValue(ctx, settingKeyBackupRecords)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return nil, fmt.Errorf("load backup records: %w", err)
	}
	if raw == "" {
		return nil, nil //nolint:nilnil // no records is a valid state
	}
	var records []BackupRecord
	if err := json.Unmarshal([]byte(raw), &records); err != nil {
		return nil, ErrBackupRecordsCorrupt
	}
	return records, nil
}

// saveRecordsLocked 在已持有 recordsMu 锁的情况下保存记录
func (s *BackupService) saveRecordsLocked(ctx context.Context, records []BackupRecord) error {
	if s.db != nil {
		for i := range records {
			if err := s.saveRecordDB(ctx, &records[i]); err != nil {
				return err
			}
		}
		return nil
	}
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return s.settingRepo.Set(ctx, settingKeyBackupRecords, string(data))
}

// saveRecord 保存单条记录（带互斥锁保护）
func (s *BackupService) saveRecord(ctx context.Context, record *BackupRecord) error {
	s.recordsMu.Lock()
	defer s.recordsMu.Unlock()
	if s.db != nil {
		return s.saveRecordDB(ctx, record)
	}

	records, err := s.loadRecordsLocked(ctx)
	if err != nil {
		return err
	}

	// 更新已有记录或追加
	found := false
	for i := range records {
		if records[i].ID == record.ID {
			records[i] = *record
			found = true
			break
		}
	}
	if !found {
		records = append(records, *record)
	}

	// Retention removes objects and metadata together. Dropping metadata here
	// would leave retained objects inaccessible to restore and cleanup.
	return s.saveRecordsLocked(ctx, records)
}

func (s *BackupService) saveRecordDB(ctx context.Context, record *BackupRecord) error {
	if record == nil {
		return errors.New("backup record is required")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	parse := func(value string) any {
		if value == "" {
			return nil
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil
		}
		return parsed
	}
	manifest, err := json.Marshal(struct {
		S3Key    string       `json:"s3_key,omitempty"`
		Parts    []BackupPart `json:"parts,omitempty"`
		Checksum string       `json:"checksum,omitempty"`
	}{record.S3Key, record.Parts, record.Checksum})
	if err != nil {
		return err
	}
	if record.AttemptID == "" {
		record.AttemptID = uuid.NewString()
		record.Revision = 1
		result, execErr := s.db.ExecContext(ctx, `
INSERT INTO backup_jobs (id, triggered_by, status, attempt_id, revision, progress, started_at, finished_at, expires_at, error_message, object_manifest, verified_at, record, updated_at)
VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,NULLIF($10,''),$11::jsonb,$12,$13::jsonb,now())
ON CONFLICT (id) DO NOTHING`,
			record.ID, record.TriggeredBy, record.Status, record.AttemptID, record.Revision, record.Progress, parse(record.StartedAt), parse(record.FinishedAt), parse(record.ExpiresAt), record.ErrorMsg, manifest, parse(record.VerifiedAt), raw)
		if execErr != nil {
			return fmt.Errorf("create backup job: %w", execErr)
		}
		written, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("create backup job rows affected: %w", rowsErr)
		}
		if written != 1 {
			return ErrBackupRecordStale
		}
		return nil
	}
	if record.Revision < 1 {
		return ErrBackupRecordStale
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE backup_jobs SET triggered_by=$1,status=$2,progress=NULLIF($3,''),finished_at=$4,
expires_at=$5,error_message=NULLIF($6,''),object_manifest=$7::jsonb,verified_at=$8,record=$9::jsonb,updated_at=now(),revision=revision+1,
deleted_at=CASE WHEN $2 = 'deleted' THEN COALESCE(deleted_at, now()) ELSE deleted_at END
WHERE id=$10 AND attempt_id=$11 AND revision=$12 AND status <> 'deleted'`,
		record.TriggeredBy, record.Status, record.Progress, parse(record.FinishedAt), parse(record.ExpiresAt), record.ErrorMsg, manifest, parse(record.VerifiedAt), raw,
		record.ID, record.AttemptID, record.Revision)
	if err != nil {
		return fmt.Errorf("save backup job: %w", err)
	}
	written, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("save backup job rows affected: %w", err)
	}
	if written != 1 {
		return ErrBackupRecordStale
	}
	record.Revision++
	return nil
}

func (s *BackupService) tombstoneBackupRecord(ctx context.Context, record *BackupRecord) error {
	if record == nil {
		return errors.New("backup record is required")
	}
	if record.Status == "deleted" {
		return ErrBackupNotFound
	}
	record.Status = "deleted"
	record.Progress = ""
	record.ErrorMsg = "deleted"
	return s.saveRecordDB(ctx, record)
}

func (s *BackupService) cleanupOldBackups(ctx context.Context, schedule *BackupScheduleConfig) error {
	if schedule == nil {
		return nil
	}

	s.recordsMu.Lock()
	defer s.recordsMu.Unlock()

	records, err := s.loadRecordsLocked(ctx)
	if err != nil {
		return err
	}

	// 按时间倒序
	sort.Slice(records, func(i, j int) bool {
		return backupStartedAfter(records[i], records[j])
	})

	var toDelete []BackupRecord
	var toKeep []BackupRecord

	completedCount := 0
	for _, r := range records {
		if r.Status != "completed" {
			toKeep = append(toKeep, r)
			continue
		}
		completedCount++
		shouldDelete := false

		// 按保留份数清理
		if schedule.RetainCount > 0 && completedCount > schedule.RetainCount {
			shouldDelete = true
		}

		// 按保留天数清理
		if schedule.RetainDays > 0 && r.StartedAt != "" {
			startedAt, err := time.Parse(time.RFC3339, r.StartedAt)
			if err == nil && time.Since(startedAt) > time.Duration(schedule.RetainDays)*24*time.Hour {
				shouldDelete = true
			}
		}

		if shouldDelete && r.Status == "completed" {
			toDelete = append(toDelete, r)
		} else {
			toKeep = append(toKeep, r)
		}
	}

	var cleanupErrs []error
	deletedCount := 0
	for _, r := range toDelete {
		if s.db != nil {
			if err := s.tombstoneBackupRecord(ctx, &r); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("tombstone backup job %s: %w", r.ID, err))
				continue
			}
		}
		if err := s.deleteBackupObjects(ctx, &r); err != nil {
			// A database row remains tombstoned after object deletion starts: this
			// prevents a late worker from advertising deleted data as verified.
			if s.db == nil {
				toKeep = append(toKeep, r)
			}
			cleanupErrs = append(cleanupErrs, fmt.Errorf("cleanup backup %s: %w", r.ID, err))
			continue
		}
		deletedCount++
	}

	if len(toDelete) > 0 {
		if s.db == nil {
			if err := s.saveRecordsLocked(ctx, toKeep); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("save backup records after cleanup: %w", err))
			}
		}
		if deletedCount > 0 {
			logger.LegacyPrintf("service.backup", "[Backup] 自动清理了 %d 个过期备份", deletedCount)
		}
		return errors.Join(cleanupErrs...)
	}
	return nil
}

// backupObjectKeys 返回一条备份记录关联的全部对象 key。
// 新记录使用 Parts，旧记录使用 S3Key；两者同时存在时也全部返回，便于清理异常残留对象。
func backupObjectKeys(record *BackupRecord) []string {
	if record == nil {
		return nil
	}
	keys := make([]string, 0, len(record.Parts)+1)
	seen := make(map[string]struct{}, len(record.Parts)+1)
	appendKey := func(key string) {
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	appendKey(record.S3Key)
	parts := append([]BackupPart(nil), record.Parts...)
	sort.Slice(parts, func(i, j int) bool { return parts[i].Index < parts[j].Index })
	for _, part := range parts {
		appendKey(part.S3Key)
	}
	return keys
}

// deleteBackupObjects 尝试删除记录关联的所有对象，并聚合删除错误。
func (s *BackupService) deleteBackupObjects(ctx context.Context, record *BackupRecord) error {
	if len(backupObjectKeys(record)) == 0 {
		return nil
	}
	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return err
	}
	if s3Cfg == nil || !s3Cfg.IsConfigured() {
		// 兼容没有配置对象存储的旧记录：记录仍可被删除。
		return nil
	}
	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return err
	}
	return deleteBackupObjectKeys(ctx, objectStore, record)
}

func deleteBackupObjectKeys(ctx context.Context, objectStore BackupObjectStore, record *BackupRecord) error {
	keys := backupObjectKeys(record)
	if len(keys) == 0 {
		return nil
	}
	var errs []error
	for _, key := range keys {
		if deleteErr := objectStore.Delete(ctx, key); deleteErr != nil {
			errs = append(errs, fmt.Errorf("delete backup object %q: %w", key, deleteErr))
		}
	}
	return errors.Join(errs...)
}

type RestoreControl interface {
	StartAuthorized(context.Context, int64, string, string, string) (*BackupRecord, error)
	CompletedArchive(context.Context, string) (*BackupRecord, error)
	Forget(context.Context, string) error
	Project(context.Context, []BackupRecord, string) ([]BackupRecord, error)
}

func (s *BackupService) SetRestoreControl(control RestoreControl) {
	s.restoreControl.Store(control)
	if s.cronSched != nil && os.Getenv("OMNIHIVE_RESTORE_JOURNAL_DSN") != "" {
		s.recoverStaleRecords()
	}
}

func (s *BackupService) boundRestoreControl() RestoreControl {
	control, _ := s.restoreControl.Load().(RestoreControl)
	return control
}

func (s *BackupService) StartAuthorizedRestore(ctx context.Context, actor int64, backupID, key, session string) (*BackupRecord, error) {
	control := s.boundRestoreControl()
	if control == nil {
		return nil, infraerrors.ServiceUnavailable("RESTORE_UNAVAILABLE", "Restore control is unavailable.")
	}
	return control.StartAuthorized(ctx, actor, backupID, key, session)
}

func (s *BackupService) restoreArchiveStore(ctx context.Context) (BackupObjectStore, *BackupS3Config, error) {
	cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, nil, err
	}
	if cfg == nil || !cfg.IsConfigured() {
		return nil, nil, ErrBackupS3NotConfigured
	}
	store, err := s.storeFactory(ctx, cfg)
	return store, cfg, err
}

func (s *BackupService) OpenRestoreArchive(ctx context.Context, record *BackupRecord) (io.ReadCloser, error) {
	if record.Status != "completed" {
		return nil, infraerrors.BadRequest("BACKUP_NOT_COMPLETED", "Backup is incomplete.")
	}
	store, _, err := s.restoreArchiveStore(ctx)
	if err != nil {
		return nil, err
	}
	if len(record.Parts) == 0 {
		return store.Download(ctx, record.S3Key)
	}
	path, err := s.downloadBackupParts(ctx, store, record.Parts)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.Join(err, cleanupBackupFiles(path))
	}
	return temporaryArchive{file}, nil
}

type temporaryArchive struct{ *os.File }

func (a temporaryArchive) Close() error {
	return errors.Join(a.File.Close(), cleanupBackupFiles(a.Name()))
}

func (s *BackupService) SnapshotArchive(ctx context.Context, key string, dump io.ReadCloser) (string, string, error) {
	store, cfg, err := s.restoreArchiveStore(ctx)
	if err != nil {
		return "", "", errors.Join(err, dump.Close())
	}
	file, err := os.CreateTemp("", "omnihive-pre-restore-*.gz")
	if err != nil {
		return "", "", errors.Join(err, dump.Close())
	}
	defer os.Remove(file.Name())
	hash := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(file, hash))
	_, err = io.Copy(gz, dump)
	if err = errors.Join(err, dump.Close(), gz.Close(), file.Close()); err != nil {
		return "", "", err
	}
	parts, err := splitBackupFile(file.Name(), defaultBackupPartSizeBytes)
	if err != nil {
		return "", "", err
	}
	for _, part := range parts {
		defer os.Remove(part.Path)
	}
	root := strings.Trim(strings.TrimRight(cfg.Prefix, "/")+"/pre-restore/"+key, "/")
	manifest := make([]BackupPart, 0, len(parts))
	for _, part := range parts {
		k := s.buildBackupPartKey(root, part.Index)
		size, err := store.UploadFile(ctx, k, part.Path, "application/octet-stream")
		if err != nil {
			return "", "", err
		}
		if size != part.SizeBytes {
			return "", "", errors.New("incomplete snapshot upload")
		}
		manifest = append(manifest, BackupPart{Index: part.Index, SizeBytes: size, S3Key: k, SHA256: part.SHA256})
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return "", "", err
	}
	manifestKey := root + "/manifest.json"
	size, err := store.Upload(ctx, manifestKey, strings.NewReader(string(data)), "application/json")
	if err != nil {
		return "", "", err
	}
	if size != int64(len(data)) {
		return "", "", errors.New("incomplete snapshot manifest")
	}
	locator := url.URL{Scheme: "s3", Host: cfg.Bucket, Path: "/" + manifestKey}
	return locator.String(), "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}
