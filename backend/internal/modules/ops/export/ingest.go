package export

import (
	"context"
	"encoding/binary"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/audit"
)

// Offer gives the BE-21 observer a nonblocking, memory-bounded handoff.
func (s *Store) Offer(v audit.Observation) bool {
	select {
	case s.ingress <- v:
		return true
	default:
		s.dropped.Add(1)
		return false
	}
}

// Dropped counts full ingress and sampled observations skipped at spool admission.
func (s *Store) Dropped() uint64 { return s.dropped.Load() }

func (s *Store) RunIngest(ctx context.Context, onError func()) {
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case v := <-s.ingress:
			persist, cancel := context.WithTimeout(ctx, time.Second)
			err := s.ingest(persist, v)
			cancel()
			if err != nil && ctx.Err() == nil && onError != nil {
				onError()
			}
		}
	}
}

type sampleTarget struct {
	id                 uuid.UUID
	approver, version  int64
	ppm, retentionDays int
}

func denied(err error) bool {
	return errors.Is(err, fault.NotFound) || errors.Is(err, fault.Forbidden) || errors.Is(err, fault.Unauthenticated)
}

func (s *Store) ingest(ctx context.Context, v audit.Observation) error {
	now := time.Now()
	if v.Scope.Actor <= 0 || v.Scope.Workspace <= 0 || v.Scope.Project <= 0 || v.Duration < 0 ||
		v.At.Before(now.Add(-30*24*time.Hour)) || v.At.After(now.Add(time.Minute)) {
		return nil
	}
	w := v.Scope.Workspace
	var enabled bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM telemetry_exports WHERE
 workspace_id=$1 AND active_slot IS NOT NULL AND state='active' AND sample_ppm>0 AND approved_at<=$2)`, w, v.At).Scan(&enabled)
	if err != nil || !enabled {
		return err
	}

	if err := s.authorize(ctx, Caller{v.Scope.Actor, w}, v.Scope.Project, ""); err != nil {
		if denied(err) {
			return nil
		}
		return err
	}
	targets, err := s.sampleTargets(ctx, w, v.At)
	if err != nil {
		return err
	}
	measurement := timing{Operation: "scope.read", Success: v.Success, LatencyMS: min(v.Duration.Milliseconds(), time.Hour.Milliseconds())}
	if v.Write {
		measurement.Operation = "scope.write"
	}
	var result error
	for _, target := range targets {
		id := deliveryID(target, v)
		if !sampled(id, target.ppm) {
			continue
		}
		if err := s.authorize(ctx, Caller{target.approver, w}, 0, ConfigureGrant); err != nil {
			if !denied(err) {
				result = errors.Join(result, err)
			}
			continue
		}
		if err := s.enqueue(ctx, w, target, id, v.At, measurement); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (s *Store) sampleTargets(ctx context.Context, w int64, at time.Time) ([]sampleTarget, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,approved_by,version,sample_ppm,retention_days FROM telemetry_exports
 WHERE workspace_id=$1 AND active_slot IS NOT NULL AND state='active' AND sample_ppm>0 AND approved_at<=$2`, w, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := make([]sampleTarget, 0, maxActiveExports)
	for rows.Next() {
		var target sampleTarget
		if err := rows.Scan(&target.id, &target.approver, &target.version, &target.ppm, &target.retentionDays); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func deliveryID(t sampleTarget, v audit.Observation) uuid.UUID {
	var fields [49]byte
	binary.BigEndian.PutUint64(fields[0:8], uint64(t.version))
	binary.BigEndian.PutUint64(fields[8:16], uint64(v.Scope.Workspace))
	binary.BigEndian.PutUint64(fields[16:24], uint64(v.Scope.Project))
	binary.BigEndian.PutUint64(fields[24:32], uint64(v.Scope.Actor))
	binary.BigEndian.PutUint64(fields[32:40], uint64(v.At.UnixNano()))
	binary.BigEndian.PutUint64(fields[40:48], uint64(v.Duration))
	if v.Write {
		fields[48] = 1
	}
	if v.Success {
		fields[48] |= 2
	}
	return uuid.NewSHA1(t.id, fields[:])
}

func sampled(id uuid.UUID, ppm int) bool {
	return uint64(binary.BigEndian.Uint32(id[:4]))*sampleScale < uint64(ppm)*(1<<32)
}

func (s *Store) enqueue(ctx context.Context, w int64, target sampleTarget, id uuid.UUID, observed time.Time, data timing) error {
	expiry := observed.Add(time.Duration(target.retentionDays) * 24 * time.Hour)
	// The SQL uniqueness constraints bound the spool even when serving
	// processes race on the same free slot. Skip writes during reconfiguration.
	var accepted bool
	err := s.db.QueryRowContext(ctx, `
 WITH config AS MATERIALIZED (
  SELECT workspace_id,id,version FROM telemetry_exports
  WHERE workspace_id=$1 AND id=$2 AND version=$3 AND state='active' AND approved_at<=$4 AND $5>clock_timestamp()
  FOR KEY SHARE SKIP LOCKED
 ), inserted AS (
  INSERT INTO telemetry_export_jobs(id,workspace_id,export_id,export_version,slot,operation,success,latency_ms,expires_at)
  SELECT $6,e.workspace_id,e.id,e.version,candidate.queue_slot,$7,$8,$9,$5
  FROM config e CROSS JOIN LATERAL (
   SELECT slots.queue_slot FROM generate_series(1,$10) AS slots(queue_slot)
   WHERE NOT EXISTS (SELECT 1 FROM telemetry_export_jobs j WHERE (j.workspace_id,j.export_id,j.slot)=(e.workspace_id,e.id,slots.queue_slot))
   ORDER BY slots.queue_slot LIMIT 1
  ) candidate
  ON CONFLICT DO NOTHING RETURNING id
 )
 SELECT EXISTS(SELECT 1 FROM inserted) OR EXISTS(SELECT 1 FROM telemetry_export_jobs WHERE id=$6)`,
		w, target.id, target.version, observed, expiry, id, data.Operation, data.Success, data.LatencyMS, maxPending).Scan(&accepted)
	if err == nil && !accepted {
		s.dropped.Add(1)
	}
	return err
}
