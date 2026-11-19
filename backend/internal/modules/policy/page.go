package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

type Page[T any] struct {
	Items []T       `json:"items"`
	Next  *string   `json:"next_cursor"`
	AsOf  time.Time `json:"as_of"`
}
type cursor struct {
	Caller     Caller
	Filter     string
	AsOf, Last time.Time
	ID         string
}

func page[T any](s *Store, c Caller, filter, token string, limit int, read func(cursor) (*sql.Rows, error), scan func(*sql.Rows) (T, string, time.Time, error)) (Page[T], error) {
	now := time.Now().UTC()
	pos := cursor{Caller: c, Filter: filter, AsOf: now, Last: now, ID: "ffffffff-ffff-ffff-ffff-ffffffffffff"}
	out := Page[T]{Items: []T{}}
	if limit < 1 || limit > 100 {
		return out, fault.Invalid("/limit")
	}
	if token != "" {
		if !s.verify("page", token, &pos) || pos.Caller != c || pos.Filter != filter {
			return out, fault.Invalid("/cursor")
		}
		if now.Sub(pos.AsOf) > 15*time.Minute {
			return out, Expired
		}
	}
	out.AsOf = pos.AsOf
	rows, err := read(pos)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Items) == limit {
			token, err := s.sign("page", pos)
			out.Next = &token
			return out, err
		}
		v, id, at, err := scan(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, v)
		pos.Last, pos.ID = at, id
	}
	return out, rows.Err()
}

func (s *Store) List(ctx context.Context, c Caller, scope Scope, token string, limit int) (Page[Head], error) {
	return page(s, c, "policies/"+scopeKey(c.Workspace, scope), token, limit, func(pos cursor) (*sql.Rows, error) {
		return s.db.QueryContext(ctx, `SELECT h.id,coalesce(h.active_version::text,''),coalesce(h.draft_version::text,''),coalesce(v.base_version::text,''),h.etag,v.definition,h.created_at
  FROM routing_policies h JOIN routing_policy_versions v ON v.id=coalesce(h.draft_version,h.active_version)
  WHERE h.workspace_id=$1 AND h.scope_type=$2 AND h.scope_id=$3 AND coalesce(h.project_id,0)=$4
  AND h.created_at<=$5 AND (h.created_at,h.id)<($6,$7::uuid) ORDER BY h.created_at DESC,h.id DESC LIMIT $8`, c.Workspace, scope.Type, scope.ID, scope.ProjectID(), pos.AsOf, pos.Last, pos.ID, limit+1)
	}, func(rows *sql.Rows) (Head, string, time.Time, error) {
		var h Head
		var at time.Time
		var raw []byte
		err := rows.Scan(&h.ID, &h.Active, &h.Draft, &h.Base, &h.ETag, &raw, &at)
		if err == nil {
			err = json.Unmarshal(raw, &h.Definition)
		}
		return h, h.ID, at, err
	})
}
func (s *Store) Versions(ctx context.Context, c Caller, id, token string, limit int) (Page[Version], error) {
	return page(s, c, "versions/"+id, token, limit, func(pos cursor) (*sql.Rows, error) {
		return s.db.QueryContext(ctx, `SELECT v.id,coalesce(v.base_version::text,''),v.actor_id,v.created_at,v.change_note FROM routing_policy_versions v
  JOIN routing_policies h ON h.id=v.policy_id AND h.workspace_id=$1 WHERE v.policy_id=$2 AND v.kind='published'
  AND v.created_at<=$3 AND (v.created_at,v.id)<($4,$5::uuid) ORDER BY v.created_at DESC,v.id DESC LIMIT $6`, c.Workspace, id, pos.AsOf, pos.Last, pos.ID, limit+1)
	}, func(rows *sql.Rows) (Version, string, time.Time, error) {
		var v Version
		err := rows.Scan(&v.ID, &v.Base, &v.Actor, &v.PublishedAt, &v.Note)
		return v, v.ID, v.PublishedAt, err
	})
}
