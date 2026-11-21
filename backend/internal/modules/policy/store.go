package policy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/lib/pq"
)

type Caller struct{ Actor, Workspace, AccessVersion int64 }
type Store struct {
	db     *sql.DB
	secret []byte
}

func NewStore(db *sql.DB, key []byte) *Store {
	return &Store{db: db, secret: append([]byte(nil), key...)}
}

func digest(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	return sum[:], nil
}

func (s *Store) sign(purpose string, v any) (string, error) {
	if len(s.secret) == 0 {
		return "", errors.New("missing policy signing key")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw) + "." + s.mac(purpose, raw), nil
}
func (s *Store) mac(purpose string, raw []byte) string {
	h := hmac.New(sha256.New, s.secret)
	_, _ = h.Write([]byte("routing/v1/" + purpose + "\x00"))
	_, _ = h.Write(raw)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Store) verify(purpose, value string, v any) bool {
	if len(s.secret) == 0 || len(value) > 128<<10 {
		return false
	}
	encoded, sig, ok := strings.Cut(value, ".")
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	return ok && err == nil && hmac.Equal([]byte(sig), []byte(s.mac(purpose, raw))) && json.Unmarshal(raw, v) == nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fault.NotFound
	}
	return err
}
func isUUID(s string) bool { id, err := uuid.Parse(s); return err == nil && id.String() == s }

func (s *Store) Scope(ctx context.Context, w int64, id string) (Scope, error) {
	var scope Scope
	if !isUUID(id) {
		return scope, fault.NotFound
	}
	err := s.db.QueryRowContext(ctx, `SELECT scope_type,scope_id,CASE WHEN scope_type='api_key' THEN project_id::text ELSE '' END
 FROM routing_policies WHERE workspace_id=$1 AND id=$2`, w, id).Scan(&scope.Type, &scope.ID, &scope.Project)
	return scope, missing(err)
}

const headSelect = `SELECT h.id,coalesce(h.active_version::text,''),coalesce(h.draft_version::text,''),coalesce(v.base_version::text,''),h.etag,v.definition
 FROM routing_policies h JOIN routing_policy_versions v ON v.id=coalesce(h.draft_version,h.active_version) `

type scanner interface{ Scan(...any) error }

func scanHead(row scanner) (Head, error) {
	var h Head
	var raw []byte
	err := row.Scan(&h.ID, &h.Active, &h.Draft, &h.Base, &h.ETag, &raw)
	if err == nil {
		err = json.Unmarshal(raw, &h.Definition)
	}
	return h, missing(err)
}
func head(ctx context.Context, q queryer, w int64, id string, lock bool) (Head, error) {
	if !isUUID(id) {
		return Head{}, fault.NotFound
	}
	stmt := headSelect + " WHERE coalesce(h.workspace_id,0)=$1 AND h.id=$2"
	if lock {
		stmt += " FOR UPDATE OF h"
	}
	return scanHead(q.QueryRowContext(ctx, stmt, w, id))
}
func (s *Store) Get(ctx context.Context, w int64, id string) (Head, error) {
	return head(ctx, s.db, w, id, false)
}

func targetFor(w int64, sc Scope) Target {
	t := Target{Workspace: w, Project: sc.ProjectID()}
	if sc.Type == "api_key" {
		t.Key = ID(sc.ID)
	}
	return t
}

func layers(ctx context.Context, q queryer, t Target) ([]Layer, error) {
	rows, err := q.QueryContext(ctx, `SELECT v.id,v.definition FROM routing_policies h JOIN routing_policy_versions v ON v.id=h.active_version
 WHERE h.scope_type='platform' OR (h.workspace_id=$1 AND (h.scope_type='workspace' OR
 (h.project_id=$2 AND (h.scope_type='project' OR h.scope_type='api_key' AND h.scope_id=$3))))
 ORDER BY CASE h.scope_type WHEN 'platform' THEN 0 WHEN 'workspace' THEN 1 WHEN 'project' THEN 2 ELSE 3 END`, t.Workspace, t.Project, strconv.FormatInt(t.Key, 10))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Layer{}
	for rows.Next() {
		var l Layer
		var raw []byte
		if err := rows.Scan(&l.Version, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &l.Definition); err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, rows.Err()
}
func revision(ctx context.Context, q queryer, h Head, id string, published bool) (Layer, error) {
	var l Layer
	var raw []byte
	if !isUUID(id) {
		return l, fault.NotFound
	}
	err := q.QueryRowContext(ctx, `SELECT id,definition FROM routing_policy_versions WHERE policy_id=$1 AND id=$2 AND (NOT $3 OR kind='published')`, h.ID, id, published).Scan(&l.Version, &raw)
	if err != nil {
		return l, missing(err)
	}
	err = json.Unmarshal(raw, &l.Definition)
	return l, err
}

// The shared lock couples the BE-01 decision made before a command with its
// database effect. Grant changes bump scope_version in the same transaction.
func checkAccess(ctx context.Context, tx *sql.Tx, c Caller) error {
	// Platform bootstrap uses an internal caller with no workspace.
	if c.Workspace == 0 {
		return nil
	}
	var version int64
	err := tx.QueryRowContext(ctx, "SELECT scope_version FROM workspaces WHERE id=$1 FOR SHARE", c.Workspace).Scan(&version)
	if err != nil {
		return missing(err)
	}
	if c.AccessVersion != version {
		return fault.Forbidden
	}
	return nil
}
func insertRevision(ctx context.Context, tx *sql.Tx, c Caller, id, base, kind, note string, d Definition) (string, time.Time, error) {
	target := targetFor(c.Workspace, d.Scope)
	chain, err := layers(ctx, tx, target)
	if err != nil {
		return "", time.Time{}, err
	}
	for len(chain) > 0 && chain[len(chain)-1].Definition.Scope == d.Scope {
		chain = chain[:len(chain)-1]
	}
	parents, err := json.Marshal(chain)
	if err != nil {
		return "", time.Time{}, err
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return "", time.Time{}, err
	}
	version := uuid.NewString()
	var at time.Time
	err = tx.QueryRowContext(ctx, `INSERT INTO routing_policy_versions(id,policy_id,base_version,kind,actor_id,definition,inherited_layers,change_note)
 VALUES ($1,$2,nullif($3,'')::uuid,$4,$5,$6,$7,$8) RETURNING created_at`, version, id, base, kind, c.Actor, string(raw), string(parents), note).Scan(&at)
	return version, at.UTC(), err
}

func command[T any](ctx context.Context, s *Store, c Caller, route, key string, input any, fn func(*sql.Tx) (T, error)) (T, error) {
	var out T
	if !validText(key, 200) {
		return out, fault.Invalid("/Idempotency-Key")
	}
	fingerprint, err := digest(input)
	if err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = checkAccess(ctx, tx, c); err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO routing_policy_receipts(workspace_id,actor_id,route,request_key,fingerprint) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, c.Workspace, c.Actor, route, key, fingerprint)
	if err != nil {
		return out, err
	}
	var same bool
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT fingerprint=$5,response FROM routing_policy_receipts WHERE workspace_id=$1 AND actor_id=$2 AND route=$3 AND request_key=$4 FOR UPDATE`, c.Workspace, c.Actor, route, key, fingerprint).Scan(&same, &raw)
	if err != nil {
		return out, err
	}
	if !same {
		return out, fault.Conflict
	}
	if len(raw) > 0 {
		err = json.Unmarshal(raw, &out)
		return out, err
	}
	out, err = fn(tx)
	if err != nil {
		return out, err
	}
	raw, err = json.Marshal(out)
	if err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE routing_policy_receipts SET response=$5 WHERE workspace_id=$1 AND actor_id=$2 AND route=$3 AND request_key=$4`, c.Workspace, c.Actor, route, key, string(raw))
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) Create(ctx context.Context, c Caller, key string, d Definition) (Head, error) {
	if err := d.Validate(c.Workspace); err != nil {
		return Head{}, err
	}
	if d.Scope.Type == "request" || (d.Scope.Type == "platform") != (c.Workspace == 0) {
		return Head{}, fault.Invalid("/scope")
	}
	return command(ctx, s, c, "create/"+scopeKey(c.Workspace, d.Scope), key, d, func(tx *sql.Tx) (Head, error) {
		var h Head
		if d.Scope.Type == "api_key" {
			if _, err := keyOwner(ctx, tx, c.Workspace, d.Scope.ProjectID(), ID(d.Scope.ID)); err != nil {
				return h, err
			}
		}
		id := uuid.NewString()
		_, err := tx.ExecContext(ctx, `INSERT INTO routing_policies(id,workspace_id,scope_type,scope_id,project_id) VALUES($1,nullif($2::bigint,0),$3,$4,nullif($5::bigint,0))`, id, c.Workspace, d.Scope.Type, d.Scope.ID, d.Scope.ProjectID())
		var dbErr *pq.Error
		if errors.As(err, &dbErr) && dbErr.Constraint == "routing_policy_assignment" {
			return h, Conflict
		}
		if err != nil {
			return h, err
		}
		v, _, err := insertRevision(ctx, tx, c, id, "", "draft", "", d)
		if err != nil {
			return h, err
		}
		_, err = tx.ExecContext(ctx, "UPDATE routing_policies SET draft_version=$2 WHERE id=$1", id, v)
		return Head{ID: id, Draft: v, ETag: 1, Definition: d}, err
	})
}

func (s *Store) Draft(ctx context.Context, c Caller, id string, etag int64, d Definition) (Head, error) {
	var result Head
	if err := d.Validate(c.Workspace); err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = checkAccess(ctx, tx, c); err != nil {
		return result, err
	}
	h, err := head(ctx, tx, c.Workspace, id, true)
	if err != nil {
		return result, err
	}
	if etag != h.ETag {
		return result, fault.Stale(h.ETag)
	}
	if d.Scope != h.Definition.Scope {
		return result, fault.Invalid("/scope")
	}
	version, _, err := insertRevision(ctx, tx, c, id, h.Active, "draft", "", d)
	if err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE routing_policies SET draft_version=$2,etag=etag+1 WHERE id=$1", id, version)
	if err != nil {
		return result, err
	}
	h.Base, h.Draft, h.Definition, h.ETag = h.Active, version, d, h.ETag+1
	return h, tx.Commit()
}
