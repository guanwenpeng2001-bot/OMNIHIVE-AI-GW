package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// LegacyImport reads one bounded Redis SCAN page. Run additional full passes
// during overlap. Source versions prevent an older scan overwriting newer data.
type LegacyImport struct {
	DB              *sql.DB
	Redis           *redis.Client
	BatchLockPrefix string
}

func (m LegacyImport) Page(ctx context.Context, cursor uint64) (uint64, error) {
	keys, next, e := m.Redis.Scan(ctx, cursor, "image_task:imgtask_*", 100).Result()
	if e != nil {
		return cursor, e
	}
	for _, key := range keys {
		raw, e := m.Redis.Get(ctx, key).Bytes()
		if errors.Is(e, redis.Nil) {
			continue
		}
		if e != nil {
			return cursor, e
		}
		var v struct {
			ID        string `json:"id"`
			Workspace int64  `json:"workspace_id"`
			Owner     int64  `json:"user_id"`
			Key       int64  `json:"api_key_id"`
			Version   int64  `json:"version"`
			Model     string `json:"public_model"`
			Status    string `json:"status"`
			Created   int64  `json:"created_at"`
			Finished  int64  `json:"completed_at"`
			Expires   int64  `json:"expires_at"`
			Deadline  int64  `json:"execution_deadline"`
			Result    struct {
				Data []struct {
					URL string `json:"url"`
				} `json:"data"`
			} `json:"result"`
		}
		if e = json.Unmarshal(raw, &v); e != nil {
			return cursor, fmt.Errorf("invalid image task record")
		}
		if key != "image_task:"+v.ID || v.Created <= 0 || v.Expires <= v.Created || v.Owner <= 0 || v.Key <= 0 {
			continue
		}
		state, acceptance := "unknown", "unknown"
		switch v.Status {
		case "processing":
			if time.Now().Unix() < v.Deadline {
				state = "running"
			}
		case "completed":
			state, acceptance = "succeeded", "accepted"
		case "failed":
			state = "failed"
		}
		tx, e := m.DB.BeginTx(ctx, nil)
		if e != nil {
			return cursor, e
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO image_tasks(id,workspace_id,project_id,actor_id,owner_id,funding_user_id,api_key_id,source,source_id,model,state,acceptance,
 version,generation,lease_owner,lease_until,created_at,updated_at)
 SELECT 'redis_'||$1,k.workspace_id,coalesce(x.project_id,r.project_id,p.id),$2,$2,$2,k.id,'redis_image',$1,$5,$6,$7,$8,$8,
 CASE WHEN $6='running' THEN 'legacy' END,CASE WHEN $6='running' THEN to_timestamp($9) END,to_timestamp($10),to_timestamp($11)
 FROM api_keys k JOIN projects p ON p.workspace_id=k.workspace_id AND p.is_default
 LEFT JOIN project_resources r ON r.kind='api_keys' AND r.resource_id=k.id AND r.workspace_id=k.workspace_id
 LEFT JOIN project_key_policies x ON x.api_key_id=k.id AND x.workspace_id=k.workspace_id
 WHERE k.id=$3 AND k.user_id=$2 AND k.workspace_id=$4
 AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q WHERE q.kind='api_keys' AND q.resource_id=k.id)
 ON CONFLICT(id) DO UPDATE SET state=excluded.state,acceptance=excluded.acceptance,version=excluded.version,generation=excluded.generation,
 lease_owner=excluded.lease_owner,lease_until=excluded.lease_until,updated_at=excluded.updated_at
 WHERE image_tasks.workspace_id=excluded.workspace_id AND image_tasks.project_id=excluded.project_id AND image_tasks.owner_id=excluded.owner_id AND image_tasks.api_key_id=excluded.api_key_id AND (image_tasks.version<excluded.version OR (image_tasks.state='running' AND excluded.state='unknown' AND image_tasks.version=excluded.version))`,
			v.ID, v.Owner, v.Key, v.Workspace, v.Model, state, acceptance, max(1, v.Version), v.Deadline, v.Created, max(v.Created, v.Finished))
		if e != nil {
			tx.Rollback()
			return cursor, e
		}
		n, e := res.RowsAffected()
		if e != nil {
			tx.Rollback()
			return cursor, e
		}
		if n > 0 && state == "succeeded" {
			for i, a := range v.Result.Data {
				if i >= 16 || !strings.HasPrefix(a.URL, "https://") {
					continue
				}
				_, e = tx.ExecContext(ctx, `INSERT INTO image_task_artifacts(task_id,id,item_id,locator,content_type,expires_at) VALUES($1,$2,'1',$3,'application/octet-stream',to_timestamp($4)) ON CONFLICT DO NOTHING`, "redis_"+v.ID, fmt.Sprintf("image_%d", i+1), a.URL, v.Expires)
				if e != nil {
					tx.Rollback()
					return cursor, e
				}
			}
		}
		if e = tx.Commit(); e != nil {
			return cursor, e
		}
	}
	return next, nil
}

// BatchLeases copies positive TTLs only. Legacy ownership is observational:
// a disappearing Redis token never authorizes a second provider submission.
func (m LegacyImport) BatchLeases(ctx context.Context, cursor uint64) (uint64, error) {
	prefix := m.BatchLockPrefix
	if prefix == "" {
		prefix = "batch_image:queue:lock:"
	}
	keys, next, e := m.Redis.Scan(ctx, cursor, prefix+"*", 100).Result()
	if e != nil {
		return cursor, e
	}
	for _, k := range keys {
		ttl, e := m.Redis.PTTL(ctx, k).Result()
		if e != nil {
			return cursor, e
		}
		if ttl <= 0 {
			continue
		}
		_, e = m.DB.ExecContext(ctx, `UPDATE image_tasks SET lease_owner='legacy',lease_until=clock_timestamp()+($2::bigint*interval '1 millisecond'),
 generation=CASE WHEN lease_until IS NULL OR lease_until<=clock_timestamp() THEN generation+1 ELSE generation END
 WHERE source='batch_image' AND source_id=$1`, strings.TrimPrefix(k, prefix), ttl.Milliseconds())
		if e != nil {
			return cursor, e
		}
	}
	return next, nil
}
