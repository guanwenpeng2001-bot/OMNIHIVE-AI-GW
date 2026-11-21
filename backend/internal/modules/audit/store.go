package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

const (
	Read   = "audit.read"
	Export = "audit.export"
	Manage = "audit.retention.write"
)

type Scope struct{ Actor, Workspace, Project int64 }
type Store struct {
	db     *sql.DB
	access identity.AccessEvaluator
}

func New(db *sql.DB, access identity.AccessEvaluator) *Store { return &Store{db, access} }

func (s *Store) Authorize(ctx context.Context, k Scope, action string) (identity.Access, error) {
	a, err := s.access.Evaluate(ctx, k.Actor, k.Workspace, k.Project, action)
	if err != nil {
		return a, err
	}
	if k.Actor <= 0 || k.Project <= 0 || a.ActorID != k.Actor || a.WorkspaceID != k.Workspace || a.ProjectID != k.Project {
		return a, fault.NotFound
	}
	return a, a.Require(action)
}

type Event struct {
	ID         int64     `json:"audit_event_id,string"`
	Workspace  int64     `json:"workspace_id,string"`
	Project    int64     `json:"project_id,string"`
	Actor      *int64    `json:"actor_id,string"`
	Resource   string    `json:"resource"`
	ResourceID *string   `json:"resource_id"`
	Action     string    `json:"action"`
	Effect     string    `json:"effect"`
	At         time.Time `json:"at"`
	Affected   *int64    `json:"affected,string,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
	Route      string    `json:"route,omitempty"`
	Method     string    `json:"method,omitempty"`
	ActorEmail string    `json:"actor_email,omitempty"`
	ActorRole  string    `json:"actor_role,omitempty"`
	StatusCode *int      `json:"status_code,omitempty"`
	Changes    []Change  `json:"changes"`
	Reason     string    `json:"reason,omitempty"`
}
type Change struct {
	Field     string `json:"field"`
	Operation string `json:"operation"`
	Before    any    `json:"before"`
	After     any    `json:"after"`
}
type Filter struct {
	Resource, ResourceID, Action string
	From, To                     *time.Time
}
type Page struct {
	Filter
	Limit     int
	AsOf      time.Time
	AfterTime *time.Time
	AfterID   int64
}

const columns = "id,workspace_id,project_id,actor_id,resource,resource_id::text,action,effect,at,affected,coalesce(request_id,''),coalesce(route,''),coalesce(method,''),coalesce(actor_email,''),coalesce(actor_role,''),status_code,changes,coalesce(reason,'')"

func scan(row interface{ Scan(...any) error }) (Event, error) {
	var e Event
	var changes []byte
	err := row.Scan(&e.ID, &e.Workspace, &e.Project, &e.Actor, &e.Resource, &e.ResourceID, &e.Action, &e.Effect, &e.At, &e.Affected, &e.RequestID, &e.Route, &e.Method, &e.ActorEmail, &e.ActorRole, &e.StatusCode, &changes, &e.Reason)
	if err == nil {
		e.Changes = []Change{}
		err = json.Unmarshal(changes, &e.Changes)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return e, fault.NotFound
	}
	return e, err
}
func (s *Store) List(ctx context.Context, k Scope, p Page) ([]Event, error) {
	if _, err := s.Authorize(ctx, k, Read); err != nil {
		return nil, err
	}
	if p.Limit < 1 || p.Limit > 100 {
		return nil, fault.Invalid("/limit")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM scoped_audit_events
 WHERE workspace_id=$1 AND project_id=$2 AND at<=$3 AND ($4='' OR resource=$4) AND ($5='' OR action=$5)
 AND ($6::timestamptz IS NULL OR at>=$6) AND ($7::timestamptz IS NULL OR at<$7)
 AND ($11='' OR resource_id=$11) AND ($8::timestamptz IS NULL OR (at,id)<($8,$9)) ORDER BY at DESC,id DESC LIMIT $10`,
		k.Workspace, k.Project, p.AsOf, p.Resource, p.Action, p.From, p.To, p.AfterTime, p.AfterID, p.Limit+1, p.ResourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Native event IDs have an explicit composite owner, including non-default projects.
func (s *Store) RequireResource(ctx context.Context, actor, w, p int64, action, kind string, id int64) error {
	if kind != "audit_events" {
		return fault.NotFound
	}
	if _, err := s.Authorize(ctx, Scope{actor, w, p}, action); err != nil {
		return err
	}
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM scoped_audit_events WHERE workspace_id=$1 AND project_id=$2 AND id=$3)`, w, p, id).Scan(&found)
	if err == nil && !found {
		return fault.NotFound
	}
	return err
}

var _ identity.ScopeResolver = (*Store)(nil)

func (s *Store) Detail(ctx context.Context, k Scope, id int64) (Event, error) {
	if err := s.RequireResource(ctx, k.Actor, k.Workspace, k.Project, Read, "audit_events", id); err != nil {
		return Event{}, err
	}
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM scoped_audit_events WHERE workspace_id=$1 AND project_id=$2 AND id=$3", k.Workspace, k.Project, id))
}
func (s *Store) exportAllowed(ctx context.Context, k Scope, id int64) error {
	a, err := s.Authorize(ctx, k, Export)
	if err != nil {
		return err
	}
	if err = a.Require(Read); err != nil {
		return err
	}
	return s.RequireResource(ctx, k.Actor, k.Workspace, k.Project, Read, "audit_events", id)
}

type handle struct {
	ID      string
	Expires time.Time
}

func operation(k Scope, event int64, h handle) identity.Operation {
	var download *string
	if time.Now().Before(h.Expires) {
		url := fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/audit/events/%d/exports/%s/download", k.Workspace, k.Project, event, h.ID)
		download = &url
	}
	res := strconv.FormatInt(event, 10)
	return identity.Operation{ID: h.ID, State: "succeeded", StatusURL: fmt.Sprintf("/api/v2/workspaces/%d/operations/%s", k.Workspace, h.ID), ProjectID: k.Project, OwnerID: k.Actor, ResourceID: &res, Result: map[string]any{"download_url": download, "expires_at": h.Expires}}
}
func (s *Store) StartExport(ctx context.Context, k Scope, id int64, key string) (identity.Operation, error) {
	if key == "" || len(key) > 256 {
		return identity.Operation{}, fault.Invalid("/Idempotency-Key")
	}
	if err := s.exportAllowed(ctx, k, id); err != nil {
		return identity.Operation{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return identity.Operation{}, err
	}
	defer tx.Rollback()
	hash := sha256.Sum256([]byte(key))
	newID := uuid.NewString()
	var h handle
	err = tx.QueryRowContext(ctx, `INSERT INTO scoped_audit_exports(id,workspace_id,project_id,actor_id,event_id,key_hash)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(workspace_id,project_id,actor_id,event_id,key_hash)
 DO UPDATE SET key_hash=EXCLUDED.key_hash RETURNING id,expires_at`, newID, k.Workspace, k.Project, k.Actor, id, hash[:]).Scan(&h.ID, &h.Expires)
	if err != nil {
		return identity.Operation{}, err
	}
	if h.ID == newID {
		_, err = tx.ExecContext(ctx, `INSERT INTO scoped_audit_events(workspace_id,project_id,actor_id,resource,resource_id,action,effect)
 VALUES($1,$2,$3,'audit_event',$4,'audit.export','committed')`, k.Workspace, k.Project, k.Actor, strconv.FormatInt(id, 10))
		if err != nil {
			return identity.Operation{}, err
		}
	}
	return operation(k, id, h), tx.Commit()
}
func (s *Store) ReadOperation(ctx context.Context, w, actor int64, id string) (identity.Operation, error) {
	if _, err := uuid.Parse(id); err != nil || len(id) != 36 {
		return identity.Operation{}, fault.NotFound
	}
	k := Scope{Workspace: w, Actor: actor}
	h := handle{ID: id}
	var event int64
	err := s.db.QueryRowContext(ctx, `SELECT project_id,event_id,expires_at FROM scoped_audit_exports WHERE workspace_id=$1 AND actor_id=$2 AND id=$3`, w, actor, id).Scan(&k.Project, &event, &h.Expires)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.Operation{}, fault.NotFound
	}
	if err != nil {
		return identity.Operation{}, err
	}
	if err = s.exportAllowed(ctx, k, event); err != nil {
		return identity.Operation{}, err
	}
	return operation(k, event, h), nil
}
func (s *Store) Download(ctx context.Context, k Scope, event int64, id string) (Event, error) {
	if _, err := uuid.Parse(id); err != nil || len(id) != 36 {
		return Event{}, fault.NotFound
	}
	if err := s.exportAllowed(ctx, k, event); err != nil {
		return Event{}, err
	}
	var expires time.Time
	err := s.db.QueryRowContext(ctx, `SELECT expires_at FROM scoped_audit_exports WHERE workspace_id=$1 AND project_id=$2 AND actor_id=$3 AND event_id=$4 AND id=$5`, k.Workspace, k.Project, k.Actor, event, id).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, fault.NotFound
	}
	if err != nil {
		return Event{}, err
	}
	e, err := s.Detail(ctx, k, event)
	if err != nil {
		return Event{}, err
	}
	if err = s.exportAllowed(ctx, k, event); err != nil {
		return Event{}, err
	}
	if !time.Now().Before(expires) {
		return Event{}, &fault.Fault{Status: 410, Reason: "ARTIFACT_EXPIRED", Message: "Export expired."}
	}
	return e, nil
}
