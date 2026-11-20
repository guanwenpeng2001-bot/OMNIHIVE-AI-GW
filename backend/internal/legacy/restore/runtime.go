package restore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/restore"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type Runtime struct {
	*restore.Service
	backup  *service.BackupService
	closeDB func()
}

func New(db *sql.DB, cfg *config.Config, backup *service.BackupService, access identity.AccessEvaluator, totp *service.TotpService, users *service.UserService) (*Runtime, error) {
	r := &Runtime{Service: &restore.Service{Access: access, StepUp: sessionGrant{users, totp}}, backup: backup}
	journal, isolated := os.Getenv("OMNIHIVE_RESTORE_JOURNAL_DSN"), os.Getenv("OMNIHIVE_RESTORE_ISOLATED_DSN")
	if journal == "" && isolated == "" {
		backup.SetRestoreControl(r)
		return r, nil
	}
	for _, key := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE", "PGOPTIONS"} {
		if os.Getenv(key) != "" {
			return nil, errors.New("restore cannot verify PostgreSQL targets with connection overrides")
		}
	}
	isoCfg, err := isolatedConfig(isolated)
	if err != nil {
		return nil, err
	}
	if journal == "" {
		return nil, errors.New("missing dedicated restore journal DSN")
	}
	j, err := sql.Open("postgres", journal)
	if err != nil {
		return nil, errors.New("invalid restore journal DSN")
	}
	i, err := sql.Open("postgres", isolated)
	if err != nil {
		j.Close()
		return nil, errors.New("invalid isolated restore DSN")
	}
	closeDB := func() { i.Close(); j.Close() }
	r.Journal = j
	r.Archives = r
	r.Targets = map[string]restore.Target{restore.Production: {DB: db, Database: repository.NewPgDumper(cfg, db)}, restore.Isolated: {DB: i, Database: repository.NewPgDumper(&config.Config{Database: isoCfg}, i)}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = r.Initialize(ctx); err != nil {
		closeDB()
		return nil, err
	}
	records, err := backup.ListBackups(ctx)
	if err != nil {
		closeDB()
		return nil, err
	}
	for _, record := range records {
		data, err := json.Marshal(record)
		if err == nil {
			err = r.Import(ctx, record.ID, data, record.RestoreStatus == "running" || record.RestoreStatus == "review_required")
		}
		if err != nil {
			closeDB()
			return nil, err
		}
	}
	backup.SetRestoreControl(r)
	r.closeDB = closeDB
	return r, nil
}
func (r *Runtime) Close() {
	if r.closeDB != nil {
		r.closeDB()
	}
}

func isolatedConfig(dsn string) (config.DatabaseConfig, error) {
	var cfg config.DatabaseConfig
	u, err := url.Parse(dsn)
	invalid := errors.New("isolated restore requires an explicit PostgreSQL URL with host, port, database, user and sslmode")
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.User == nil || u.User.Username() == "" || u.Hostname() == "" || len(u.Path) < 2 || strings.ContainsAny(u.Path[1:], "/=") || u.Fragment != "" {
		return cfg, invalid
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 1 || len(q["sslmode"]) != 1 || q.Get("sslmode") == "" {
		return cfg, invalid
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return cfg, invalid
	}
	password, _ := u.User.Password()
	return config.DatabaseConfig{Host: u.Hostname(), Port: port, DBName: u.Path[1:], User: u.User.Username(), Password: password, SSLMode: q.Get("sslmode")}, nil
}
func (r *Runtime) StartWorker(ctx context.Context) func() {
	if r.Journal == nil {
		return func() {}
	}
	ctx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			legacyErr := r.advanceLegacy(ctx)
			worked, err := r.Work(ctx)
			err = errors.Join(err, legacyErr)
			if err != nil && ctx.Err() == nil {
				slog.Error("restore worker could not advance journal")
			}
			if worked && err == nil {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
	return func() { stop(); <-done }
}
func (r *Runtime) Record(ctx context.Context, id string) (json.RawMessage, error) {
	b, err := r.backup.GetBackupRecord(ctx, id)
	if errors.Is(err, service.ErrBackupNotFound) {
		return nil, fault.NotFound
	}
	if err != nil {
		return nil, err
	}
	if b.Status != "completed" {
		return nil, fault.Invalid("/backup_id")
	}
	return json.Marshal(b)
}
func (r *Runtime) Open(ctx context.Context, data json.RawMessage) (io.ReadCloser, error) {
	var rec service.BackupRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	return r.backup.OpenRestoreArchive(ctx, &rec)
}
func (r *Runtime) Snapshot(ctx context.Context, key string, dump io.ReadCloser) (restore.Snapshot, error) {
	locator, digest, err := r.backup.SnapshotArchive(ctx, key, dump)
	return restore.Snapshot{Key: locator, Digest: digest}, err
}

func (r *Runtime) CompletedArchive(ctx context.Context, id string) (*service.BackupRecord, error) {
	if r.Journal == nil {
		return nil, nil
	}
	var data []byte
	err := r.Journal.QueryRowContext(ctx, `SELECT record FROM restore_journal.backups WHERE id=$1 AND record->>'status'='completed'`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var b service.BackupRecord
	if err = json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *Runtime) Forget(ctx context.Context, id string) error {
	if r.Journal == nil {
		return nil
	}
	_, err := r.Journal.ExecContext(ctx, "UPDATE restore_journal.backups SET removed_at=clock_timestamp() WHERE id=$1", id)
	return err
}

func (r *Runtime) Project(ctx context.Context, records []service.BackupRecord, filter string) ([]service.BackupRecord, error) {
	if r.Journal == nil {
		return records, nil
	}
	rows, err := r.Journal.QueryContext(ctx, `SELECT DISTINCT ON(id) id,record,operation,state,stamp,reason FROM (
 SELECT b.id,p.record,coalesce(l.id,o.id) AS operation,CASE WHEN o.stage='manual_recovery' THEN 'unknown' ELSE o.state END AS state,o.updated_at AS stamp,o.report->>'reason' AS reason,o.created_at
 FROM restore_journal.operations o JOIN restore_journal.previews p ON p.id=o.preview_id JOIN restore_journal.backups b ON b.id=p.backup_id
 LEFT JOIN restore_journal.legacy_requests l ON l.production_id=o.id
 WHERE o.target='production' AND b.removed_at IS NULL AND ($1='' OR b.id=$1) UNION ALL SELECT b.id,b.record,r.id,CASE WHEN r.state='pending' THEN 'queued' ELSE 'failed' END,r.created_at,r.reason,r.created_at
 FROM restore_journal.legacy_requests r JOIN restore_journal.backups b ON b.id=r.backup_id
 WHERE r.state<>'submitted' AND b.removed_at IS NULL AND ($1='' OR b.id=$1)) history ORDER BY id,created_at DESC`, filter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	index := make(map[string]int, len(records))
	for n, b := range records {
		index[b.ID] = n
	}
	for rows.Next() {
		var id, op, state string
		var data []byte
		var stamp time.Time
		var reason sql.NullString
		if err := rows.Scan(&id, &data, &op, &state, &stamp, &reason); err != nil {
			return nil, err
		}
		n, exists := index[id]
		if !exists || records[n].Status != "completed" {
			var b service.BackupRecord
			if err = json.Unmarshal(data, &b); err != nil {
				return nil, err
			}
			if exists {
				records[n] = b
			} else {
				n = len(records)
				records = append(records, b)
			}
		}
		projectStatus(&records[n], op, state, reason.String, stamp)
	}
	return records, rows.Err()
}

func (r *Runtime) StartAuthorized(ctx context.Context, actor int64, backupID, key, session string) (*service.BackupRecord, error) {
	rec, err := r.startLegacy(ctx, actor, backupID, key, session)
	if err == nil {
		return rec, nil
	}
	var f *fault.Fault
	if errors.As(err, &f) {
		return nil, infraerrors.New(f.Status, f.Reason, f.Message)
	}
	return nil, infraerrors.ServiceUnavailable("RESTORE_UNAVAILABLE", "Restore control is unavailable.")
}

type sessionGrant struct {
	users *service.UserService
	totp  *service.TotpService
}

func (g sessionGrant) Require(ctx context.Context, a restore.Actor) error {
	user, err := g.users.GetByID(ctx, a.ID)
	if err != nil {
		return restore.Unavailable
	}
	if !user.TotpEnabled {
		return &fault.Fault{Status: 403, Reason: "STEP_UP_TOTP_NOT_ENABLED", Message: "Enable two-factor authentication before restoring production."}
	}
	ok, err := g.totp.HasStepUpGrant(ctx, a.ID, a.Session)
	if err != nil {
		return &fault.Fault{Status: 503, Reason: "STEP_UP_UNAVAILABLE", Message: "Session verification is unavailable."}
	}
	if !ok {
		return &fault.Fault{Status: 403, Reason: "STEP_UP_REQUIRED", Message: "Verify this session with a current TOTP code."}
	}
	return nil
}
