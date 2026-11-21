package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	auditdomain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/audit"
)

const (
	auditLogQueueCapacity = 4096
	auditLogBatchSize     = 100
	auditLogFlushInterval = time.Second

	auditRetentionCheckInterval = 24 * time.Hour
	auditRetentionStartupDelay  = 5 * time.Minute
	auditRetentionBatchSize     = 5000
)

// AuditLogService 管理面操作审计日志服务。
// 写入端为非阻塞异步批量落库（不拖慢管理请求）；
// 读取端提供分页查询；清空端点由 handler 层做 TOTP 强校验后调用 ClearAll。
type AuditLogService struct {
	repo           AuditLogRepository
	settingService *SettingService

	queue chan *AuditLog

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	droppedCount uint64
	writeFailed  uint64
	writtenCount uint64
	scoped       *auditdomain.Store
}

func (s *AuditLogService) SetScopedStore(store *auditdomain.Store) { s.scoped = store }

func NewAuditLogService(repo AuditLogRepository, settingService *SettingService) *AuditLogService {
	ctx, cancel := context.WithCancel(context.Background())
	return &AuditLogService{
		repo:           repo,
		settingService: settingService,
		queue:          make(chan *AuditLog, auditLogQueueCapacity),
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start 启动异步写入与保留期清理协程。
func (s *AuditLogService) Start() {
	if s == nil || s.repo == nil {
		return
	}
	s.wg.Add(2)
	go s.runWriter()
	go s.runRetentionLoop()
}

// Stop 停止服务并尽量落盘队列中剩余记录。
func (s *AuditLogService) Stop() {
	if s == nil {
		return
	}
	s.cancel()
	s.wg.Wait()
}

// Record 非阻塞入队一条审计记录；队列打满时丢弃并计数（管理面流量下几乎不可能发生）。
func (s *AuditLogService) Record(entry *AuditLog) {
	if s == nil || entry == nil {
		return
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	select {
	case <-s.ctx.Done():
		return
	default:
	}
	select {
	case s.queue <- scopedAuditProjection(entry):
	default:
		atomic.AddUint64(&s.droppedCount, 1)
	}
}

func (s *AuditLogService) captureScoped(entry *AuditLog) {
	if s.scoped == nil || entry.ProjectID == nil || entry.ActorUserID == nil || entry.RequestID == "" || entry.StatusCode < 200 || entry.StatusCode >= 400 {
		return
	}
	workspace := auditParam(entry, "workspace_id")
	workspaceID, _ := strconv.ParseInt(workspace, 10, 64)
	changes, _ := json.Marshal(auditChanges(entry.Extra))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.scoped.Capture(ctx, auditdomain.Operation{Workspace: workspaceID, Project: *entry.ProjectID, Actor: *entry.ActorUserID,
		Action: entry.Action, Route: entry.Path, RequestID: entry.RequestID, ResourceID: entry.ResourceID, Method: entry.Method,
		ActorEmail: entry.ActorEmail, ActorRole: entry.ActorRole, Status: entry.StatusCode, At: entry.CreatedAt, Changes: changes,
		Reason: auditReason(entry.Extra)}); err != nil {
		atomic.AddUint64(&s.writeFailed, 1)
	}
}

func auditParam(entry *AuditLog, key string) string {
	switch params := entry.Extra["params"].(type) {
	case map[string]string:
		return params[key]
	case map[string]any:
		value, _ := params[key].(string)
		return value
	}
	return ""
}

func auditReason(extra map[string]any) string {
	reason, _ := extra["reason"].(string)
	return truncateStringForAudit(reason, 512)
}

func truncateStringForAudit(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return string(runes)
}

func auditChanges(extra map[string]any) []ConfigurationFieldDiff {
	for _, key := range []string{"diff", "submitted_diff"} {
		raw, err := json.Marshal(extra[key])
		if err != nil {
			continue
		}
		var changes []ConfigurationFieldDiff
		if json.Unmarshal(raw, &changes) == nil && len(changes) > 0 {
			return changes
		}
	}
	return []ConfigurationFieldDiff{}
}

// List 分页查询审计日志。
func (s *AuditLogService) List(ctx context.Context, filter *AuditLogFilter) (*AuditLogList, error) {
	page, err := s.repo.List(ctx, filter)
	if err != nil || page == nil {
		return page, err
	}
	for i, v := range page.Logs {
		page.Logs[i] = auditManagementView(v)
	}
	return page, nil
}

// GetByID 查询单条详情。
func (s *AuditLogService) GetByID(ctx context.Context, id int64) (*AuditLog, error) {
	row, err := s.repo.GetByID(ctx, id)
	return auditManagementView(row), err
}

// ClearAll 全量清空审计日志并写入留痕记录。
// 调用方（handler）必须先完成 TOTP 验证；本方法负责：
// 在同一事务中统计并清空全表，同步写入一条 "audit_log.clear" 留痕记录。
// 绕过异步队列，留痕失败时回滚清理。
func (s *AuditLogService) ClearAll(ctx context.Context, trace *AuditLog) (int64, error) {
	if trace == nil {
		return 0, fmt.Errorf("audit clear trace is required")
	}
	trace.Action = AuditActionAuditLogClear
	if trace.CreatedAt.IsZero() {
		trace.CreatedAt = time.Now().UTC()
	}
	// Keep deletion and its trace in one transaction. If trace insertion fails,
	// the original audit trail must remain intact.
	return s.repo.ClearWithTrace(ctx, trace)
}

func (s *AuditLogService) runWriter() {
	defer s.wg.Done()

	ticker := time.NewTicker(auditLogFlushInterval)
	defer ticker.Stop()

	batch := make([]*AuditLog, 0, auditLogBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		inserted, err := s.repo.BatchInsert(ctx, batch)
		cancel()
		if err != nil {
			atomic.AddUint64(&s.writeFailed, uint64(len(batch)))
			_, _ = fmt.Fprintf(os.Stderr, "time=%s level=WARN msg=\"audit log flush failed\" batch=%d\n",
				time.Now().Format(time.RFC3339Nano), len(batch))
		} else {
			for _, entry := range batch {
				s.captureScoped(entry)
			}
			atomic.AddUint64(&s.writtenCount, uint64(inserted))
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-s.ctx.Done():
			// 停机前排空队列。
			for {
				select {
				case item := <-s.queue:
					if item == nil {
						continue
					}
					batch = append(batch, item)
					if len(batch) >= auditLogBatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case item := <-s.queue:
			if item == nil {
				continue
			}
			batch = append(batch, item)
			if len(batch) >= auditLogBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// runRetentionLoop 按保留期定期删除过期审计日志。
// 删除操作幂等，多实例并发执行无害，因此无需选主。
func (s *AuditLogService) runRetentionLoop() {
	defer s.wg.Done()

	startupTimer := time.NewTimer(auditRetentionStartupDelay)
	defer startupTimer.Stop()
	select {
	case <-s.ctx.Done():
		return
	case <-startupTimer.C:
	}

	ticker := time.NewTicker(auditRetentionCheckInterval)
	defer ticker.Stop()

	s.runRetentionOnce()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.runRetentionOnce()
		}
	}
}

func (s *AuditLogService) runRetentionOnce() {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
	defer cancel()

	days := 0
	if s.settingService != nil {
		days = s.settingService.GetAuditLogRetentionDays(ctx)
	}
	if days <= 0 {
		return // 0 或负值表示永久保留，仅支持手动清空
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	for {
		deleted, err := s.repo.DeleteBefore(ctx, cutoff, auditRetentionBatchSize)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "time=%s level=WARN msg=\"audit log retention cleanup failed\"\n",
				time.Now().Format(time.RFC3339Nano))
			return
		}
		if deleted == 0 {
			return
		}
	}
}
