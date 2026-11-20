package export

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/audit"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

type Egress interface {
	Do(*http.Request) (*http.Response, error)
}

type Store struct {
	db      *sql.DB
	access  identity.AccessEvaluator
	egress  Egress
	ingress chan audit.Observation
	dropped atomic.Uint64
}

func New(db *sql.DB, access identity.AccessEvaluator, egress Egress) *Store {
	return &Store{db: db, access: access, egress: egress, ingress: make(chan audit.Observation, 256)}
}

func (s *Store) authorize(ctx context.Context, caller Caller, project int64, action string) error {
	if caller.ActorID <= 0 {
		return fault.Unauthenticated
	}
	if caller.WorkspaceID <= 0 {
		return fault.NotFound
	}
	a, err := s.access.Evaluate(ctx, caller.ActorID, caller.WorkspaceID, project, action)
	if err != nil {
		return err
	}
	if a.ActorID != caller.ActorID || a.WorkspaceID != caller.WorkspaceID || a.ProjectID != project {
		return fault.NotFound
	}
	return a.Require(action)
}

func (s *Store) beginChange(ctx context.Context, caller Caller) (*sql.Tx, error) {
	if err := s.authorize(ctx, caller, 0, ConfigureGrant); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Serialize configuration writers in a workspace, including idempotent
	// creates competing for the last active slot.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('omnihive.telemetry.configure'), hashint8($1))`, caller.WorkspaceID); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

type creation struct {
	resource    string
	keyHash     [32]byte
	fingerprint [32]byte
	id          uuid.UUID
	destination uuid.NullUUID
}

func newCreation(resource, key string, input any) (creation, error) {
	if key == "" || len(key) > 256 || strings.TrimSpace(key) != key {
		return creation{}, fault.Invalid("/Idempotency-Key")
	}
	doc, err := json.Marshal(input)
	if err != nil {
		return creation{}, err
	}
	return creation{
		resource: resource, keyHash: sha256.Sum256([]byte(key)), fingerprint: sha256.Sum256(doc),
	}, nil
}

func (c *creation) replay(ctx context.Context, tx *sql.Tx, caller Caller) (bool, error) {
	var existing []byte
	err := tx.QueryRowContext(ctx, `SELECT fingerprint, result_id, result_destination_id FROM telemetry_export_create_keys
 WHERE workspace_id=$1 AND actor_id=$2 AND resource=$3 AND key_hash=$4`,
		caller.WorkspaceID, caller.ActorID, c.resource, c.keyHash[:]).Scan(&existing, &c.id, &c.destination)
	if errors.Is(err, sql.ErrNoRows) {
		c.id = uuid.New()
		return false, nil
	}
	if err == nil && !bytes.Equal(existing, c.fingerprint[:]) {
		return false, fault.Conflict
	}
	return err == nil, err
}

func (c creation) record(ctx context.Context, tx *sql.Tx, caller Caller) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO telemetry_export_create_keys
 (workspace_id, actor_id, resource, key_hash, fingerprint, result_id, result_destination_id) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		caller.WorkspaceID, caller.ActorID, c.resource, c.keyHash[:], c.fingerprint[:], c.id, c.destination)
	return err
}

func found(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fault.NotFound
	}
	return err
}

func (s *Store) RegisterDestination(ctx context.Context, caller Caller, key, endpoint string) (Destination, error) {
	address, err := parseEndpoint(endpoint)
	if err != nil {
		return Destination{}, err
	}
	create, err := newCreation("destination", key, address)
	if err != nil {
		return Destination{}, err
	}
	tx, err := s.beginChange(ctx, caller)
	if err != nil {
		return Destination{}, err
	}
	defer tx.Rollback()
	replay, err := create.replay(ctx, tx, caller)
	if err != nil {
		return Destination{}, err
	}
	out := Destination{ID: create.id, State: "registered", Version: 1}
	if replay {
		return out, nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO telemetry_export_destinations(workspace_id,id,endpoint) VALUES($1,$2,$3)`,
		caller.WorkspaceID, out.ID, address); err != nil {
		return Destination{}, err
	}
	if err = create.record(ctx, tx, caller); err != nil {
		return Destination{}, err
	}
	return out, tx.Commit()
}

func (s *Store) RevokeDestination(ctx context.Context, caller Caller, id uuid.UUID, version int64) (Destination, error) {
	tx, err := s.beginChange(ctx, caller)
	if err != nil {
		return Destination{}, err
	}
	defer tx.Rollback()
	w := caller.WorkspaceID
	var out Destination
	out.ID = id
	var revoked bool
	if err = tx.QueryRowContext(ctx, `SELECT version, revoked_at IS NOT NULL FROM telemetry_export_destinations
 WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, w, id).Scan(&out.Version, &revoked); err != nil {
		return Destination{}, found(err)
	}
	if out.Version != version {
		return Destination{}, fault.Stale(out.Version)
	}
	out.State = "revoked"
	if revoked {
		return out, nil
	}

	// The destination fence serializes this mutation with network dispatch.
	// Lock affected exports before purging so an enqueue cannot race the purge.
	if _, err = tx.ExecContext(ctx, `SELECT id FROM telemetry_exports WHERE workspace_id=$1 AND destination_id=$2
 AND active_slot IS NOT NULL ORDER BY id FOR UPDATE`, w, id); err != nil {
		return Destination{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM telemetry_export_jobs j USING telemetry_exports e
 WHERE (e.workspace_id,e.id)=(j.workspace_id,j.export_id) AND e.workspace_id=$1 AND e.destination_id=$2
 AND e.active_slot IS NOT NULL`, w, id); err != nil {
		return Destination{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE telemetry_exports SET state='disabled', active_slot=NULL,
 version=version+1, last_error_reason=$3 WHERE workspace_id=$1 AND destination_id=$2 AND active_slot IS NOT NULL`, w, id, reasonDestinationRevoked); err != nil {
		return Destination{}, err
	}
	if err = tx.QueryRowContext(ctx, `UPDATE telemetry_export_destinations SET endpoint=NULL, revoked_at=clock_timestamp(),
 version=version+1 WHERE workspace_id=$1 AND id=$2 RETURNING version`, w, id).Scan(&out.Version); err != nil {
		return Destination{}, err
	}
	return out, tx.Commit()
}

func lockDestination(ctx context.Context, tx *sql.Tx, w int64, id uuid.UUID) (string, error) {
	var endpoint string
	err := tx.QueryRowContext(ctx, `SELECT endpoint FROM telemetry_export_destinations
 WHERE workspace_id=$1 AND id=$2 AND revoked_at IS NULL FOR SHARE`, w, id).Scan(&endpoint)
	return endpoint, found(err)
}

func availableSlot(ctx context.Context, tx *sql.Tx, w int64) (int, error) {
	var slot int
	err := tx.QueryRowContext(ctx, `SELECT n FROM generate_series(1,$2) n WHERE NOT EXISTS
 (SELECT 1 FROM telemetry_exports WHERE workspace_id=$1 AND active_slot=n) ORDER BY n LIMIT 1`, w, maxActiveExports).Scan(&slot)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errCapacity
	}
	return slot, err
}

func startValidation(ctx context.Context, tx *sql.Tx, w int64, c Config) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO telemetry_export_jobs(id,workspace_id,export_id,export_version,slot)
 VALUES($1,$2,$3,$4,0)`, uuid.New(), w, c.ID, c.Version)
	return err
}

func (s *Store) Create(ctx context.Context, caller Caller, key string, input CreateInput) (Config, error) {
	p, err := parsePolicy(input)
	if err != nil {
		return Config{}, err
	}
	create, err := newCreation("export", key, p)
	if err != nil {
		return Config{}, err
	}
	tx, err := s.beginChange(ctx, caller)
	if err != nil {
		return Config{}, err
	}
	defer tx.Rollback()
	replay, err := create.replay(ctx, tx, caller)
	if err != nil {
		return Config{}, err
	}
	out := Config{ID: create.id, State: stateValidating, Version: 1, DestinationID: p.DestinationID}
	if replay {
		out.DestinationID = create.destination.UUID
		return out, nil
	}
	w := caller.WorkspaceID
	if _, err = lockDestination(ctx, tx, w, p.DestinationID); err != nil {
		return Config{}, err
	}
	slot, err := availableSlot(ctx, tx, w)
	if err != nil {
		return Config{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telemetry_exports
 (workspace_id,id,destination_id,state,active_slot,approved_by,data_classes,sample_ppm,retention_days)
 VALUES($1,$2,$3,$4,$5,$6,ARRAY['timings'],$7,$8)`, w, out.ID, out.DestinationID, out.State, slot, caller.ActorID, p.SamplePPM, p.RetentionDays)
	if err != nil {
		return Config{}, err
	}
	if err = startValidation(ctx, tx, w, out); err != nil {
		return Config{}, err
	}
	create.destination = uuid.NullUUID{UUID: p.DestinationID, Valid: true}
	if err = create.record(ctx, tx, caller); err != nil {
		return Config{}, err
	}
	return out, tx.Commit()
}

func (s *Store) Get(ctx context.Context, caller Caller, id uuid.UUID) (Detail, error) {
	if err := s.authorize(ctx, caller, 0, ConfigureGrant); err != nil {
		return Detail{}, err
	}
	var out Detail
	out.ID = id
	out.DataClasses = []string{timings}
	var ppm int
	err := s.db.QueryRowContext(ctx, `SELECT e.state,e.version,e.destination_id,e.sample_ppm,e.retention_days,e.last_success_at,e.last_error_reason,
 (SELECT count(*) FROM telemetry_export_jobs j WHERE (j.workspace_id,j.export_id)=(e.workspace_id,e.id)
 AND j.slot>0 AND j.expires_at>clock_timestamp())
 FROM telemetry_exports e WHERE e.workspace_id=$1 AND e.id=$2`, caller.WorkspaceID, id).
		Scan(&out.State, &out.Version, &out.DestinationID, &ppm, &out.RetentionDays, &out.LastSuccessAt, &out.LastErrorReason, &out.QueueDepth)
	out.SampleRate = formatRate(ppm)
	if out.LastSuccessAt != nil {
		at := out.LastSuccessAt.UTC()
		out.LastSuccessAt = &at
	}
	return out, found(err)
}
