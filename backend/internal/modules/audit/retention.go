package audit

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

type Retention struct {
	Days         int `json:"event_days"`
	TraceDays    int `json:"trace_days"`
	ContentHours int `json:"content_hours"`
}

const policySQL = `SELECT coalesce(r.days,90),coalesce(r.trace_days,7),coalesce(r.content_hours,24)
 FROM projects p LEFT JOIN scoped_audit_retention r ON (r.workspace_id,r.project_id)=(p.workspace_id,p.id)
 WHERE p.workspace_id=$1 AND p.id=$2`

func (s *Store) Policy(ctx context.Context, k Scope) (Retention, error) {
	var p Retention
	if _, err := s.Authorize(ctx, k, Read); err != nil {
		return p, err
	}
	err := s.db.QueryRowContext(ctx, policySQL, k.Workspace, k.Project).Scan(&p.Days, &p.TraceDays, &p.ContentHours)
	return p, err
}
func (s *Store) SetPolicy(ctx context.Context, k Scope, p Retention) error {
	if _, err := s.Authorize(ctx, k, Manage); err != nil {
		return err
	}
	if p.Days < 1 || p.Days > 3650 || p.TraceDays < 1 || p.TraceDays > 3650 || p.ContentHours < 0 || p.ContentHours > 24 {
		return fault.Invalid("/retention")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Use the same project row as the sweeper so a concurrent extension of TTL wins
	// before any subsequent deletion decision.
	var project int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, k.Workspace, k.Project).Scan(&project); err != nil {
		return err
	}
	var cur Retention
	if err = tx.QueryRowContext(ctx, policySQL, k.Workspace, k.Project).Scan(&cur.Days, &cur.TraceDays, &cur.ContentHours); err != nil {
		return err
	}
	if p == cur {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO scoped_audit_retention VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(workspace_id,project_id) DO UPDATE SET days=excluded.days,trace_days=excluded.trace_days,content_hours=excluded.content_hours`, k.Workspace, k.Project, p.Days, p.TraceDays, p.ContentHours); err != nil {
		return err
	}
	if p.ContentHours < cur.ContentHours {
		if _, err = tx.ExecContext(ctx, `UPDATE image_tasks SET input_until=least(input_until,created_at+make_interval(hours=>$3))
 WHERE workspace_id=$1 AND project_id=$2 AND replay_input IS NOT NULL AND input_until>created_at+make_interval(hours=>$3)`, k.Workspace, k.Project, p.ContentHours); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO scoped_audit_events(workspace_id,project_id,actor_id,resource,action,effect) VALUES($1,$2,$3,'retention','audit.retention.set','committed')`, k.Workspace, k.Project, k.Actor); err != nil {
		return err
	}
	return tx.Commit()
}

// Purge re-reads the scope policy under lock and atomically records the effects.
// A batch that has already committed leaves no eligible rows on retry.
func (s *Store) Purge(ctx context.Context, w, p int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var project int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND id=$2 FOR UPDATE SKIP LOCKED`, w, p).Scan(&project)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var policy Retention
	if err = tx.QueryRowContext(ctx, policySQL, w, p).Scan(&policy.Days, &policy.TraceDays, &policy.ContentHours); err != nil {
		return 0, err
	}
	var n int64
	err = tx.QueryRowContext(ctx, `WITH victims AS (
 SELECT id FROM scoped_audit_events e WHERE workspace_id=$1 AND project_id=$2
 AND at<now()-make_interval(days=>CASE WHEN effect='observed' THEN $4::integer ELSE $3::integer END)
 AND (evidence_until IS NULL OR evidence_until<=now())
 AND NOT EXISTS(SELECT FROM scoped_audit_exports x WHERE x.workspace_id=$1 AND x.project_id=$2 AND x.event_id=e.id AND x.expires_at>now())
 ORDER BY at,id LIMIT 500 FOR UPDATE),
 removed AS (DELETE FROM scoped_audit_events WHERE id IN(SELECT id FROM victims) RETURNING id),
 inputs AS (SELECT id FROM image_tasks WHERE workspace_id=$1 AND project_id=$2 AND replay_input IS NOT NULL
 AND least(input_until,created_at+make_interval(hours=>$5))<=now() ORDER BY id LIMIT 500 FOR UPDATE SKIP LOCKED),
 scrubbed AS (UPDATE image_tasks SET replay_input=NULL WHERE id IN(SELECT id FROM inputs) RETURNING id)
 SELECT (SELECT count(*) FROM removed)+(SELECT count(*) FROM scrubbed)`, w, p, policy.Days, policy.TraceDays, policy.ContentHours).Scan(&n)
	if err != nil {
		return 0, err
	}
	// Expired handles continue to return 410 for one day before physical removal.
	res, err := tx.ExecContext(ctx, `DELETE FROM scoped_audit_exports WHERE id IN
 (SELECT id FROM scoped_audit_exports WHERE workspace_id=$1 AND project_id=$2 AND expires_at<now()-interval '1 day' ORDER BY expires_at LIMIT 500)`, w, p)
	if err != nil {
		return 0, err
	}
	handles, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	n += handles
	if n > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO scoped_audit_events(workspace_id,project_id,resource,action,effect,affected) VALUES($1,$2,'retention','audit.retention.purge','committed',$3)`, w, p, n)
		if err != nil {
			return 0, err
		}
	}
	return n, tx.Commit()
}

// Sweep visits archived scopes too. A busy scope cannot starve later projects.
func (s *Store) Sweep(ctx context.Context) error {
	var last int64
	for {
		rows, err := s.db.QueryContext(ctx, `SELECT workspace_id,id FROM projects WHERE id>$1 ORDER BY id LIMIT 100`, last)
		if err != nil {
			return err
		}
		var scopes []Scope
		for rows.Next() {
			var k Scope
			if err = rows.Scan(&k.Workspace, &k.Project); err != nil {
				rows.Close()
				return err
			}
			scopes = append(scopes, k)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, k := range scopes {
			if _, err = s.Purge(ctx, k.Workspace, k.Project); err != nil {
				return err
			}
			last = k.Project
		}
		if len(scopes) < 100 {
			return nil
		}
	}
}
func (s *Store) RunRetention(ctx context.Context, onError func()) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := s.Sweep(ctx); err != nil && ctx.Err() == nil && onError != nil {
			onError()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
