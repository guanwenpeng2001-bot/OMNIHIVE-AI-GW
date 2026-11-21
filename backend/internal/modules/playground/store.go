package playground

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

var Unavailable = &fault.Fault{Status: 503, Reason: "PLAYGROUND_UNAVAILABLE", Message: "Playground execution is unavailable."}

type Store struct{ DB *sql.DB }
type Summary struct {
	Run         string `json:"run_id"`
	Request     string `json:"request_id"`
	Protocol    string `json:"protocol"`
	State       string `json:"state"`
	Seq         int64  `json:"last_seq"`
	Retained    bool   `json:"content_retained"`
	StatusURL   string `json:"status_url"`
	DetailURL   string `json:"detail_url"`
	Settlement  string `json:"settlement_state"`
	Payer       int64  `json:"payer_user_id,string"`
	Preview     string `json:"text_preview,omitempty"`
	fingerprint []byte
}
type Handle struct{ Run, Request string }

func KeyHash(key string) []byte { h := sha256.Sum256([]byte(key)); return h[:] }
func (s Store) lookup(ctx context.Context, a identity.Access, condition string, v any) (Summary, error) {
	var r Summary
	err := s.DB.QueryRowContext(ctx, `SELECT r.run_id,r.request_id,r.protocol,
 CASE WHEN r.state IN ('running','cancel_requested') AND r.deadline<=clock_timestamp() THEN 'unknown' ELSE r.state END,r.last_seq,r.payer_user_id,r.fingerprint,
 coalesce((SELECT u.receipt_state FROM gateway_execution_usage u WHERE u.request_id=r.request_id AND u.receipt_state IS NOT NULL ORDER BY u.receipt_id DESC LIMIT 1),
 CASE WHEN r.state='unknown' OR (r.state IN ('running','cancel_requested') AND r.deadline<=clock_timestamp()) THEN 'unknown' ELSE 'pending' END)
 FROM playground_runs r WHERE r.workspace_id=$1 AND r.project_id=$2 AND r.actor_id=$3 AND `+condition, a.WorkspaceID, a.ProjectID, a.ActorID, v).Scan(&r.Run, &r.Request, &r.Protocol, &r.State, &r.Seq, &r.Payer, &r.fingerprint, &r.Settlement)
	if errors.Is(err, sql.ErrNoRows) {
		return r, fault.NotFound
	}
	base := fmt.Sprintf("/api/v2/workspaces/%d/projects/%d", a.WorkspaceID, a.ProjectID)
	r.StatusURL = base + "/playground/runs/" + r.Run
	r.DetailURL = base + "/requests/" + r.Request
	return r, err
}
func (s Store) Detail(ctx context.Context, a identity.Access, id string) (Summary, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Summary{}, fault.NotFound
	}
	return s.lookup(ctx, a, "r.run_id=$4", id)
}
func (s Store) ByKey(ctx context.Context, a identity.Access, key string, fp []byte) (Summary, error) {
	r, err := s.lookup(ctx, a, "r.key_hash=$4", KeyHash(key))
	if err == nil && fp != nil && !bytes.Equal(fp, r.fingerprint) {
		return Summary{}, fault.Conflict
	}
	return r, err
}
func (s Store) Claim(ctx context.Context, a identity.Access, in Submit, payer int64, key, parent string, fp [32]byte, deadline time.Time) (Summary, bool, error) {
	r := Handle{uuid.NewString(), uuid.NewString()}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO playground_runs(run_id,request_id,workspace_id,project_id,actor_id,api_key_id,payer_user_id,protocol,model_id,replay_of,key_hash,fingerprint,state,deadline,raw_events_requested,preview_consent)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,nullif($10,''),$11,$12,'running',$13,$14,$15) ON CONFLICT(workspace_id,project_id,actor_id,key_hash) DO NOTHING`,
		r.Run, r.Request, a.WorkspaceID, a.ProjectID, a.ActorID, in.Key, payer, in.Protocol, in.Model, parent, KeyHash(key), fp[:], deadline, in.Options.Raw, in.Options.Retain)
	if err != nil {
		return Summary{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Summary{}, false, err
	}
	run, err := s.ByKey(ctx, a, key, fp[:])
	return run, n == 1, err
}
func (s Store) Cancel(ctx context.Context, a identity.Access, id string) (Summary, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Summary{}, fault.NotFound
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE playground_runs SET state='cancel_requested' WHERE run_id=$1 AND workspace_id=$2 AND project_id=$3 AND actor_id=$4 AND state='running' AND deadline>clock_timestamp()`, id, a.WorkspaceID, a.ProjectID, a.ActorID)
	if err != nil {
		return Summary{}, err
	}
	return s.Detail(ctx, a, id)
}
func (s Store) Active(ctx context.Context, id string) (bool, error) {
	var live bool
	err := s.DB.QueryRowContext(ctx, `SELECT state='running' AND deadline>clock_timestamp() FROM playground_runs WHERE run_id=$1`, id).Scan(&live)
	return live, err
}
func (s Store) Progress(ctx context.Context, id string, seq int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE playground_runs SET last_seq=greatest(last_seq,$2) WHERE run_id=$1`, id, seq)
	return err
}

// A lost terminal or a canceled transport after a send cannot establish what the provider did.
func (s Store) Finish(ctx context.Context, r Summary, completed, providerFailed, canceled bool, seq int64) (string, error) {
	var state string
	err := s.DB.QueryRowContext(ctx, `UPDATE playground_runs SET last_seq=greatest(last_seq,$5),state=CASE
 WHEN $3 AND EXISTS(SELECT FROM gateway_execution_attempts WHERE request_id=$2 AND acceptance='accepted') THEN 'succeeded'
 WHEN $6 AND EXISTS(SELECT FROM gateway_execution_attempts WHERE request_id=$2 AND acceptance='accepted') THEN 'failed'
 WHEN EXISTS(SELECT FROM gateway_execution_attempts WHERE request_id=$2 AND (acceptance IN ('unknown','accepted') OR abandoned_uncertain)) THEN 'unknown'
 WHEN $4 OR state='cancel_requested' THEN 'canceled' ELSE 'failed' END WHERE run_id=$1 RETURNING state`, r.Run, r.Request, completed, canceled, seq, providerFailed).Scan(&state)
	return state, err
}
