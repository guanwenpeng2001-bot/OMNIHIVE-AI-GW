package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *upstreamJobRepository) ListResources(ctx context.Context, user, key, group int64, family, after string, limit int) ([]*service.UpstreamJob, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT row_to_json(j) FROM upstream_jobs j
 WHERE user_id=$1 AND api_key_id=$2 AND group_id=$3 AND platform='qianwen'
 AND COALESCE((resource_state->>'deleted')::boolean,false)=false
 AND (($4='batch' AND family='batch') OR ($4 IN ('files','batch_input') AND family='files')
 OR ($4 IN ('files','batch_output') AND family='batch' AND status='succeeded' AND result_ref<>'' AND COALESCE((resource_state->>'output_deleted')::boolean,false)=false))
 AND ($5='' OR id<(SELECT id FROM upstream_jobs WHERE public_request_id=$5 AND user_id=$1 AND api_key_id=$2 AND group_id=$3))
 ORDER BY id DESC LIMIT $6`, user, key, group, family, after, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []*service.UpstreamJob{}
	for rows.Next() {
		j, err := scanUpstreamJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (r *upstreamJobRepository) FileInUse(ctx context.Context, id string) (bool, error) {
	var used bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM upstream_jobs WHERE family='batch' AND billing_snapshot->'file_batch'->>'input_file_id'=$1 AND status IN ('submitted','running'))`, id).Scan(&used)
	return used, err
}
