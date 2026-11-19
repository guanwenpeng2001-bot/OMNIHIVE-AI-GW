package export

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

func (s *Store) Change(ctx context.Context, caller Caller, id uuid.UUID, version int64, input PatchInput) (Config, error) {
	if input.DestinationID == nil && input.DataClasses == nil && input.SampleRate == nil && input.RetentionDays == nil && input.Enabled == nil {
		return Config{}, fault.Invalid("/body")
	}
	tx, err := s.beginChange(ctx, caller)
	if err != nil {
		return Config{}, err
	}
	defer tx.Rollback()
	w := caller.WorkspaceID

	// Dispatch takes the destination lock before the export lock. The
	// workspace lock prevents another configuration writer changing this ID.
	var target uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT destination_id FROM telemetry_exports WHERE workspace_id=$1 AND id=$2`, w, id).
		Scan(&target); err != nil {
		return Config{}, found(err)
	}
	if input.DestinationID != nil {
		target = *input.DestinationID
	}
	_, destinationErr := lockDestination(ctx, tx, w, target)
	if destinationErr != nil && !errors.Is(destinationErr, fault.NotFound) {
		return Config{}, destinationErr
	}

	out := Config{ID: id, DestinationID: target}
	var slot sql.NullInt64
	var ppm, retention int
	err = tx.QueryRowContext(ctx, `SELECT version,active_slot,sample_ppm,retention_days FROM telemetry_exports
 WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, w, id).
		Scan(&out.Version, &slot, &ppm, &retention)
	if err != nil {
		return Config{}, found(err)
	}
	if version != out.Version {
		return Config{}, fault.Stale(out.Version)
	}

	requested := CreateInput{DestinationID: target, DataClasses: []string{timings}, SampleRate: formatRate(ppm), RetentionDays: retention}
	if input.DataClasses != nil {
		requested.DataClasses = *input.DataClasses
	}
	if input.SampleRate != nil {
		requested.SampleRate = *input.SampleRate
	}
	if input.RetentionDays != nil {
		requested.RetentionDays = *input.RetentionDays
	}
	p, err := parsePolicy(requested)
	if err != nil {
		return Config{}, err
	}
	enabled := slot.Valid
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if (enabled || input.DestinationID != nil) && destinationErr != nil {
		return Config{}, destinationErr
	}
	out.State = stateDisabled
	if enabled {
		if !slot.Valid {
			free, err := availableSlot(ctx, tx, w)
			if err != nil {
				return Config{}, err
			}
			slot = sql.NullInt64{Int64: int64(free), Valid: true}
		}
		out.State = stateValidating
	} else {
		slot = sql.NullInt64{}
	}
	if err = deleteJobs(ctx, tx, w, id); err != nil {
		return Config{}, err
	}
	err = tx.QueryRowContext(ctx, `UPDATE telemetry_exports SET destination_id=$3,state=$4,active_slot=$5,approved_by=$6,
 approved_at=clock_timestamp(),sample_ppm=$7,retention_days=$8,version=version+1,last_success_at=NULL,last_error_reason=NULL
 WHERE workspace_id=$1 AND id=$2 RETURNING version`, w, id, p.DestinationID, out.State, slot, caller.ActorID, p.SamplePPM, p.RetentionDays).
		Scan(&out.Version)
	if err != nil {
		return Config{}, err
	}
	if enabled {
		if err = startValidation(ctx, tx, w, out); err != nil {
			return Config{}, err
		}
	}
	return out, tx.Commit()
}

func deleteJobs(ctx context.Context, tx *sql.Tx, w int64, id uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM telemetry_export_jobs WHERE workspace_id=$1 AND export_id=$2`, w, id)
	return err
}
