package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/lib/pq"
)

func jsonIDs(chain []Layer) string { data, _ := json.Marshal(chainIDs(chain)); return string(data) }

func impact(ctx context.Context, tx *sql.Tx, w int64, s Scope) (Impact, error) {
	var i Impact
	t := targetFor(w, s)
	err := tx.QueryRowContext(ctx, `WITH affected AS (
 SELECT p.id,p.workspace_id FROM projects p WHERE ($1=0 OR p.workspace_id=$1) AND ($2=0 OR p.id=$2) AND p.archived_at IS NULL
 ), keys AS (
 SELECT k.id FROM api_keys k JOIN projects d ON d.workspace_id=k.workspace_id AND d.is_default
 LEFT JOIN project_key_policies x ON x.workspace_id=k.workspace_id AND x.api_key_id=k.id
 LEFT JOIN project_resources r ON r.kind='api_keys' AND r.workspace_id=k.workspace_id AND r.resource_id=k.id
 JOIN affected a ON a.id=coalesce(x.project_id,r.project_id,d.id) AND a.workspace_id=k.workspace_id
 WHERE k.deleted_at IS NULL AND ($3=0 OR k.id=$3) AND NOT EXISTS(SELECT FROM identity_scope_quarantine WHERE kind='api_keys' AND resource_id=k.id))
 SELECT (SELECT count(*) FROM affected),(SELECT count(*) FROM keys)`, w, t.Project, t.Key).Scan(&i.Projects, &i.Keys)
	return i, err
}
func (s *Store) publish(ctx context.Context, c Caller, id, key, action, note string, etag int64, input any, selectVersion func(*sql.Tx, Head, Impact) (string, error)) (Publication, error) {
	if !validText(note, 1000) {
		return Publication{}, fault.Invalid("/change_note")
	}
	return command(ctx, s, c, action+"/"+id, key, struct {
		ETag  int64
		Input any
	}{etag, input}, func(tx *sql.Tx) (Publication, error) {
		var out Publication
		// Serialize publication before reading parents or assigning an outbox epoch.
		// This also prevents a parent's update racing the simulation validity check.
		err := tx.QueryRowContext(ctx, "SELECT epoch FROM routing_policy_clock WHERE singleton FOR UPDATE").Scan(&out.Epoch)
		if err != nil {
			return out, err
		}
		h, err := head(ctx, tx, c.Workspace, id, true)
		if err != nil {
			return out, err
		}
		if h.ETag != etag {
			return out, Conflict
		}
		out.Impact, err = impact(ctx, tx, c.Workspace, h.Definition.Scope)
		if err != nil {
			return out, err
		}
		src, err := selectVersion(tx, h, out.Impact)
		if err != nil {
			return out, err
		}
		l, err := revision(ctx, tx, h, src, action == "rollback")
		if err != nil {
			return out, err
		}
		out.ID, out.Previous = id, h.Active
		out.Active, out.PublishedAt, err = insertRevision(ctx, tx, c, id, h.Active, "published", note, l.Definition)
		if err != nil {
			return out, err
		}
		_, err = tx.ExecContext(ctx, "UPDATE routing_policies SET active_version=$2,draft_version=NULL,etag=etag+1 WHERE id=$1", id, out.Active)
		var dbErr *pq.Error
		if errors.As(err, &dbErr) && dbErr.Code == "23505" {
			return out, Conflict
		}
		if err != nil {
			return out, err
		}
		err = tx.QueryRowContext(ctx, "UPDATE routing_policy_clock SET epoch=epoch+1 WHERE singleton RETURNING epoch").Scan(&out.Epoch)
		if err != nil {
			return out, err
		}
		raw, err := json.Marshal(out.Impact)
		if err != nil {
			return out, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO routing_policy_outbox(revocation_epoch,policy_id,version,published_at,previous_version,impact)
  VALUES ($1,$2,$3,$4,nullif($5,'')::uuid,$6)`, out.Epoch, id, out.Active, out.PublishedAt, out.Previous, string(raw))
		return out, err
	})
}

func (s *Store) Publish(ctx context.Context, c Caller, id, key string, etag int64, in Publish) (Publication, error) {
	return s.publish(ctx, c, id, key, "publish", in.Note, etag, in, func(tx *sql.Tx, h Head, approved Impact) (string, error) {
		if in.Draft == "" || in.Draft != h.Draft || in.Base != h.Active {
			return "", Conflict
		}
		return h.Draft, s.checkProof(ctx, tx, c, h, in.Simulation, approved)
	})
}
func (s *Store) Rollback(ctx context.Context, c Caller, id, key string, etag int64, in Rollback) (Publication, error) {
	return s.publish(ctx, c, id, key, "rollback", in.Note, etag, in, func(tx *sql.Tx, h Head, approved Impact) (string, error) {
		if h.Active == "" || in.From != h.Active || in.Restore == h.Active {
			return "", Conflict
		}
		return in.Restore, nil
	})
}

// BootstrapPlatform installs a versioned platform definition from trusted
// deployment configuration. Platform writes have no tenant HTTP route.
func (s *Store) BootstrapPlatform(ctx context.Context, actor int64, id, key string, etag int64, draft, base, note string) (Publication, error) {
	c := Caller{Actor: actor}
	return s.publish(ctx, c, id, key, "platform", note, etag, []string{draft, base, note}, func(tx *sql.Tx, h Head, approved Impact) (string, error) {
		if h.Definition.Scope.Type != "platform" || draft == "" || draft != h.Draft || base != h.Active {
			return "", Conflict
		}
		return draft, nil
	})
}

type Change struct {
	Epoch             int64
	PolicyID, Version string
	PublishedAt       time.Time
}

func (s *Store) Changes(ctx context.Context, after int64, limit int) ([]Change, error) {
	if after < 0 || limit < 1 || limit > 1000 {
		return nil, fault.Invalid("/cursor")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT revocation_epoch,policy_id,version,published_at FROM routing_policy_outbox WHERE revocation_epoch>$1 ORDER BY revocation_epoch LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Change{}
	for rows.Next() {
		var v Change
		if err := rows.Scan(&v.Epoch, &v.PolicyID, &v.Version, &v.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
