package restore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"time"

	"github.com/google/uuid"
)

const ttl = 45 * time.Second

func (s *Service) claim(ctx context.Context) (lease, error) {
	var l lease
	// tx_apply and tx_verify still require a live lease before COMMIT can
	// be sent. A lost acknowledgement after that fence needs reconciliation.
	_, err := s.Journal.ExecContext(ctx, `UPDATE restore_journal.operations SET state='failed',
 stage=CASE WHEN stage IN ('tx_apply','tx_verify') THEN 'rolled_back' ELSE 'manual_recovery' END,
 owner=NULL, lease_until=NULL,
 report=coalesce(report,'{}') || jsonb_build_object('reason',CASE WHEN stage IN ('tx_apply','tx_verify') THEN 'TRANSACTION_ABORTED' ELSE coalesce(report->>'reason','RESTORE_REVIEW_REQUIRED') END,'source_digest',(SELECT digest FROM restore_journal.previews WHERE id=preview_id)),updated_at=clock_timestamp()
 WHERE (state='running' AND lease_until<=clock_timestamp() AND stage IN ('applying','verify_counts','tx_apply','tx_verify','committing'))
 OR state='unknown'`)
	if err != nil {
		return l, err
	}
	l.Owner = uuid.NewString()
	var rec, counts []byte
	err = s.Journal.QueryRowContext(ctx, `WITH candidate AS (
 SELECT id FROM restore_journal.operations WHERE state='queued' OR (state='running' AND lease_until<=clock_timestamp() AND stage IN ('check_source','snapshot'))
 ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED
 ), claimed AS (
 UPDATE restore_journal.operations o SET state='running',stage='check_source',owner=$1,lease_until=clock_timestamp()+$2*interval '1 second',
 generation=CASE WHEN o.state='queued' THEN generation ELSE generation+1 END,updated_at=clock_timestamp()
 FROM candidate c WHERE o.id=c.id RETURNING o.*
 ) SELECT c.id,c.workspace_id,c.actor_id,c.session_key,c.generation,c.target,p.digest,p.record,p.evidence FROM claimed c
 JOIN restore_journal.previews p ON p.id=c.preview_id`, l.Owner, ttl.Seconds()).Scan(
		&l.ID, &l.Actor.Workspace, &l.Actor.ID, &l.Actor.Session, &l.Generation, &l.Target, &l.Digest, &rec, &counts)
	if err != nil {
		return l, err
	}
	l.Record = rec
	l.Stage = "check_source"
	return l, json.Unmarshal(counts, &l.Evidence)
}

const fence = ` WHERE id=$1 AND generation=$2 AND owner=$3 AND state='running' AND lease_until>clock_timestamp()`

func changed(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}
func (s *Service) renew(ctx context.Context, l lease) error {
	return changed(s.Journal.ExecContext(ctx, `UPDATE restore_journal.operations SET lease_until=clock_timestamp()+$4*interval '1 second'`+fence, l.ID, l.Generation, l.Owner, ttl.Seconds()))
}
func (s *Service) stage(ctx context.Context, l *lease, to string, snapshot *Snapshot) error {
	var raw any
	if snapshot != nil {
		data, _ := json.Marshal(snapshot)
		raw = string(data)
	}
	err := changed(s.Journal.ExecContext(ctx, `UPDATE restore_journal.operations SET stage=$5,snapshot=coalesce($6::jsonb,snapshot),updated_at=clock_timestamp()`+fence+` AND stage=$4`, l.ID, l.Generation, l.Owner, l.Stage, to, raw))
	if err == nil {
		l.Stage = to
	}
	return err
}
func (s *Service) finish(ctx context.Context, l lease, counts map[string]string, reason string) error {
	state, stage := "failed", l.Stage
	if reason == "" {
		if l.Stage != "committing" || len(counts) == 0 || !maps.Equal(counts, l.Evidence) {
			return errors.New("restore verification evidence missing")
		}
		state, stage = "succeeded", "verified"
	} else if l.Stage == "committing" {
		stage = "manual_recovery"
	} else if l.Stage == "tx_apply" || l.Stage == "tx_verify" {
		stage = "rolled_back"
	}
	report, _ := json.Marshal(Report{Digest: l.Digest, Counts: reportCounts(counts), Reason: reason})
	return changed(s.Journal.ExecContext(ctx, `UPDATE restore_journal.operations SET state=$5,stage=$6,owner=NULL,lease_until=NULL,report=$7,updated_at=clock_timestamp()`+fence+` AND stage=$4`, l.ID, l.Generation, l.Owner, l.Stage, state, stage, string(report)))
}

// Work claims at most one operation. A process death leaves the persisted stage and lease for its peer.
func (s *Service) Work(ctx context.Context) (bool, error) {
	l, err := s.claim(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	done := make(chan struct{})
	go func(held lease) {
		defer close(done)
		tick := time.NewTicker(ttl / 3)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if err := s.renew(ctx, held); err != nil {
					cancel()
					return
				}
			}
		}
	}(l)
	defer func() { cancel(); <-done }()
	counts, reason := s.apply(ctx, &l)
	return true, s.finish(ctx, l, counts, reason)
}
func (s *Service) apply(ctx context.Context, l *lease) (map[string]string, string) {
	if err := s.authorize(ctx, l.Actor, ExecuteAction); err != nil {
		return nil, "ACTION_DENIED"
	}
	target := s.Targets[l.Target]
	cluster, name, err := databaseIdentity(ctx, target.DB)
	if err != nil {
		return nil, "TARGET_UNAVAILABLE"
	}
	var bound string
	if err = s.Journal.QueryRowContext(ctx, "SELECT database_identity FROM restore_journal.targets WHERE name=$1", l.Target).Scan(&bound); err != nil || bound != cluster+"/"+name {
		return nil, "TARGET_IDENTITY_CHANGED"
	}
	script, err := os.CreateTemp("", "omnihive-restore-*.sql")
	if err != nil {
		return nil, "STAGING_UNAVAILABLE"
	}
	defer os.Remove(script.Name())
	defer script.Close()
	raw, err := s.Archives.Open(ctx, l.Record)
	if err != nil {
		return nil, "SOURCE_UNAVAILABLE"
	}
	digest, expected, err := inspectArchive(ctx, raw, script)
	err = errors.Join(err, raw.Close())
	if err != nil || digest != l.Digest || !maps.Equal(expected, l.Evidence) {
		return nil, "RESTORE_SOURCE_CHANGED"
	}
	if _, err = script.Seek(0, io.SeekStart); err != nil {
		return nil, "STAGING_UNAVAILABLE"
	}
	checks, err := os.CreateTemp("", "omnihive-check-*.sql")
	if err != nil {
		return nil, "STAGING_UNAVAILABLE"
	}
	defer os.Remove(checks.Name())
	err = errors.Join(writeVerification(script, checks), checks.Close())
	if err != nil {
		return nil, "VERIFICATION_UNAVAILABLE"
	}
	if err = s.stage(ctx, l, "snapshot", nil); err != nil {
		return nil, "JOURNAL_UNAVAILABLE"
	}
	if l.Target == Production {
		if err = s.drainRequests(ctx); err != nil {
			return nil, "MAINTENANCE_UNAVAILABLE"
		}
	}
	reason := "APPLY_FAILED"
	locked := func() error {
		dump, err := target.Database.Dump(ctx)
		if err != nil {
			reason = "SNAPSHOT_FAILED"
			return err
		}
		snapshot, err := s.Archives.Snapshot(ctx, fmt.Sprintf("%s/%d", l.ID, l.Generation), dump)
		if err != nil || snapshot.Key == "" || !digestFormat.MatchString(snapshot.Digest) {
			reason = "SNAPSHOT_FAILED"
			return errors.New(reason)
		}
		if err = s.authorize(ctx, l.Actor, ExecuteAction); err != nil {
			reason = "ACTION_DENIED"
			return err
		}
		if l.Target == Production {
			if err = s.RequireStepUp(ctx, l.Actor); err != nil {
				reason = "STEP_UP_REQUIRED"
				return err
			}
		}
		return s.stage(ctx, l, "tx_apply", &snapshot)
	}
	applied := func() error {
		reason = "VERIFICATION_FAILED"
		return s.stage(ctx, l, "tx_verify", nil)
	}
	verified := func() error {
		reason = "COMMIT_UNCONFIRMED"
		return s.stage(ctx, l, "committing", nil)
	}
	if err = target.Database.RestoreChecked(ctx, script.Name(), checks.Name(), locked, applied, verified); err != nil {
		return nil, reason
	}
	return expected, ""
}
