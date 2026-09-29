package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ListGatewayResources returns one page of the caller's owned resource jobs for
// one platform. Resource ids are opaque public ids; the upstream binding never
// leaves the service layer.
func (r *upstreamJobRepository) ListGatewayResources(ctx context.Context, platform string, user, key, group int64, family, purpose, after string, limit int) ([]*service.UpstreamJob, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT row_to_json(j) FROM upstream_jobs j
 WHERE j.user_id=$1 AND j.api_key_id=$2 AND j.group_id=$3 AND j.platform=$4
 AND COALESCE((j.resource_state->>'deleted')::boolean,false)=false
 AND (($5='batch' AND j.family='batch')
  OR ($5='files' AND j.family='files' AND ($6='' OR COALESCE(j.billing_snapshot->'resource_file'->>'purpose','')=$6))
  OR ($5='batch_output' AND j.family='batch' AND j.status='succeeded' AND j.result_ref<>'' AND COALESCE((j.resource_state->>'output_deleted')::boolean,false)=false))
 AND ($7='' OR j.id<(SELECT id FROM upstream_jobs WHERE public_request_id=$7 AND user_id=$1 AND api_key_id=$2 AND group_id=$3))
 ORDER BY j.id DESC LIMIT $8`, user, key, group, platform, family, purpose, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	jobs := []*service.UpstreamJob{}
	for rows.Next() {
		job, err := scanUpstreamJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// GatewayResourceFileInUse reports whether a non-terminal batch still references
// this input file, so a file cannot be deleted out from under it.
func (r *upstreamJobRepository) GatewayResourceFileInUse(ctx context.Context, id string) (bool, error) {
	var used bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM upstream_jobs WHERE family='batch' AND billing_snapshot->'resource_batch'->>'input_file_id'=$1 AND status IN ('submitted','running'))`, id).Scan(&used)
	return used, err
}
