package tasks

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/lib/pq"
)

type Page struct {
	AsOf, AfterTime    time.Time
	AfterID            string
	Limit              int
	Kind, State, Model []string
	From, To           *time.Time
}
type scanner interface{ Scan(...any) error }

const projection = `SELECT h.id,h.project_id,h.kind,h.provider_job_id,h.source,h.source_id,h.model,h.execution::text,h.parent_task,
 h.display_state,
 h.display_stage,h.display_acceptance,h.display_cancel,h.progress,h.display_version+(CASE WHEN h.cancel_state='requested' THEN 1 ELSE 0 END),h.created_at,h.display_updated,
 h.owner_id,h.api_key_id,h.replay_input IS NOT NULL AND h.input_until>now(),h.item_count,h.success_count,h.fail_count,h.cancelled_count,
 h.estimate,h.currency,coalesce(r.id::text,CASE WHEN ml.id IS NOT NULL THEN 'upstream_job_usage:'||ml.id WHEN h.source='batch_image' AND h.settled_at IS NOT NULL THEN 'batch_image_capture:'||h.source_id END),
 coalesce(r.state,CASE WHEN ml.id IS NOT NULL OR h.settled_at IS NOT NULL THEN 'settled' WHEN EXISTS (SELECT 1 FROM usage_billing_dedup d
 WHERE d.api_key_id=h.api_key_id AND d.request_id='batch_image_hold:'||h.source_id) AND NOT EXISTS (SELECT 1 FROM usage_billing_dedup d
 WHERE d.api_key_id=h.api_key_id AND d.request_id='batch_image_release:'||h.source_id) THEN 'reserved' ELSE 'unknown' END),
 CASE WHEN r.state='settled' THEN CASE WHEN r.pricing_state='free' THEN '0.00000000' WHEN r.pricing_state='priced' THEN
 (CASE WHEN (r.billing_command->>'SubscriptionID') IS NOT NULL THEN r.billing_command->>'SubscriptionCost' ELSE r.billing_command->>'BalanceCost' END)::numeric(28,8)::text END
 WHEN ml.id IS NOT NULL THEN ml.actual_cost::numeric(28,8)::text WHEN h.settled_at IS NOT NULL THEN h.batch_charge END
 FROM task_center_history h LEFT JOIN usage_logs ml ON ml.id=h.media_usage AND ml.request_id=h.provider_job_id AND ml.account_id=h.media_account AND ml.api_key_id=h.api_key_id AND ml.user_id=h.funding_user_id LEFT JOIN LATERAL (SELECT s.* FROM usage_settlement_receipts s WHERE s.user_id=h.funding_user_id AND s.api_key_id=h.api_key_id
 AND ((s.request_id=h.execution::text AND EXISTS(SELECT 1 FROM gateway_execution_receipts x JOIN gateway_execution_attempts a USING(attempt_id) WHERE x.receipt_id=s.id AND a.request_id=h.execution))
 OR (h.source='upstream_job' AND EXISTS(SELECT 1 FROM upstream_jobs u WHERE u.public_request_id=h.source_id AND u.charged_usage_log_id=s.usage_log_id AND u.account_id=s.account_id)))
 ORDER BY s.id LIMIT 1) r ON true WHERE h.workspace_id=$1 AND h.project_id=$2`

func readDetail(row scanner, s Scope) (Detail, error) {
	var d Detail
	var total, ok, failed, canceled *int64
	var estimate, charge *string
	var currency string
	err := row.Scan(&d.ID, &d.Project, &d.Kind, &d.providerJob, &d.Source, &d.sourceID, &d.Model, &d.Request, &d.Parent, &d.State, &d.Stage, &d.Provider, &d.Cancel, &d.Progress, &d.Version, &d.Created, &d.Updated, &d.owner, &d.key, &d.replayable, &total, &ok, &failed, &canceled, &estimate, &currency, &d.SettlementID, &d.Settlement, &charge)
	if err != nil {
		return d, found(err)
	}
	d.Artifacts = []Artifact{}
	d.Actions = []string{}
	d.Reasons = map[string]string{}
	d.Created = d.Created.UTC()
	d.Updated = d.Updated.UTC()
	if d.State == "unknown" {
		d.Stage = "submission_unknown"
	}
	d.ProgressSource = "unreported"
	if d.Progress != nil {
		d.ProgressSource = "provider"
	}
	if total != nil {
		d.Counts = Counts{Total: *total, Succeeded: *ok, Failed: *failed, Canceled: *canceled, Pending: max(0, *total-*ok-*failed-*canceled)}
	} else {
		d.Counts.Total = 1
		switch d.State {
		case "succeeded":
			d.Counts.Succeeded = 1
		case "failed":
			d.Counts.Failed = 1
		case "canceled":
			d.Counts.Canceled = 1
		default:
			d.Counts.Pending = 1
		}
	}
	if estimate != nil {
		v, e := money.NewMoney(money.Currency(currency), *estimate)
		if e != nil {
			return d, e
		}
		d.Estimate = &v
	}
	if charge != nil {
		v, e := money.NewMoney(money.Currency(currency), *charge)
		if e != nil {
			return d, e
		}
		d.Charge = &v
	}
	if d.Request != nil && s.Require("requests.read") == nil {
		u := Base(s) + "/requests/" + url.PathEscape(*d.Request)
		d.RequestURL = &u
	}
	if d.SettlementID != nil && d.Request != nil && s.Require("billing.read") == nil {
		u := Base(s) + "/billing/charges?request_id=" + url.QueryEscape(*d.Request)
		d.SettlementURL = &u
	}
	for _, a := range []string{"tasks.read", "tasks.artifacts.read", "tasks.cancel", "tasks.retry"} {
		if s.Require(a) != nil {
			continue
		}
		switch {
		case a == "tasks.cancel" && d.Cancel != "available":
			d.Reasons[a] = "CANCEL_UNSUPPORTED"
		case a == "tasks.retry" && (d.Provider == "unknown" || d.State == "unknown"):
			d.Reasons[a] = "SUBMISSION_UNKNOWN"
		case a == "tasks.retry" && !d.canRetry(s):
			d.Reasons[a] = "RETRY_UNAVAILABLE"
		default:
			d.Actions = append(d.Actions, a)
		}
	}
	return d, nil
}
func (d Detail) canRetry(s Scope) bool {
	return d.State == "failed" && (d.Provider == "rejected" || d.Provider == "not_sent") && d.replayable && d.owner == s.ActorID
}
func (s *Store) List(ctx context.Context, a Scope, p Page) ([]Detail, error) {
	if err := a.Require("tasks.read"); err != nil {
		return nil, err
	}
	if p.Limit < 1 || p.Limit > 100 {
		return nil, fault.Invalid("/limit")
	}
	rows, err := s.DB.QueryContext(ctx, projection+` AND h.created_at<=$3 AND ($4='' OR (h.created_at,h.id)<($5,$4))
 AND (cardinality($6::text[])=0 OR h.kind=ANY($6)) AND (cardinality($7::text[])=0 OR
 (h.display_state)=ANY($7))
 AND (cardinality($8::text[])=0 OR h.model=ANY($8)) AND ($9::timestamptz IS NULL OR h.created_at>=$9) AND ($10::timestamptz IS NULL OR h.created_at<$10)
 ORDER BY h.created_at DESC,h.id DESC LIMIT $11`, a.WorkspaceID, a.ProjectID, p.AsOf, p.AfterID, p.AfterTime, pq.Array(append([]string{}, p.Kind...)), pq.Array(append([]string{}, p.State...)), pq.Array(append([]string{}, p.Model...)), p.From, p.To, p.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	data := []Detail{}
	for rows.Next() {
		d, e := readDetail(rows, a)
		if e != nil {
			return nil, e
		}
		data = append(data, d)
	}
	return data, rows.Err()
}
func (s *Store) lookup(ctx context.Context, a Scope, id string) (Detail, error) {
	return readDetail(s.DB.QueryRowContext(ctx, projection+" AND h.id=$3", a.WorkspaceID, a.ProjectID, id), a)
}
func (s *Store) Detail(ctx context.Context, a Scope, id string) (Detail, error) {
	if e := a.Require("tasks.read"); e != nil {
		return Detail{}, e
	}
	d, e := s.lookup(ctx, a, id)
	if e != nil {
		return d, e
	}
	d.ArtifactState = "pending"
	if a.Require("tasks.artifacts.read") != nil {
		d.ArtifactState = "withheld"
		return d, nil
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT id,content_type,size_bytes,expires_at,state FROM task_center_artifacts WHERE task_id=$1 ORDER BY id`, id)
	if e != nil {
		return d, e
	}
	defer rows.Close()
	for rows.Next() {
		var ar Artifact
		if e = rows.Scan(&ar.ID, &ar.ContentType, &ar.Size, &ar.Expires, &ar.State); e != nil {
			return d, e
		}
		d.Artifacts = append(d.Artifacts, ar)
		if d.ArtifactState != "available" {
			d.ArtifactState = ar.State
		}
	}
	return d, rows.Err()
}
func (s *Store) Items(ctx context.Context, a Scope, id string, p Page) ([]Item, error) {
	if e := a.Require("tasks.read"); e != nil {
		return nil, e
	}
	d, e := s.lookup(ctx, a, id)
	if e != nil {
		return nil, e
	}
	if p.Limit < 1 || p.Limit > 100 {
		return nil, fault.Invalid("/limit")
	}
	rows, e := s.DB.QueryContext(ctx, `WITH items AS (
 SELECT custom_id AS id,CASE status WHEN 'success' THEN 'succeeded' WHEN 'cancelled' THEN 'canceled' WHEN 'pending' THEN 'queued' ELSE 'failed' END AS state,
 CASE WHEN status IN ('success','failed') THEN 'accepted' ELSE $3 END AS acceptance,CASE WHEN status='failed' THEN 'ITEM_FAILED' END reason
 FROM batch_image_items WHERE $1='batch_image' AND job_id=$2 AND created_at<=$9
 UNION ALL SELECT '1',$4,$3,NULL WHERE $1<>'batch_image')
 SELECT id,state,acceptance,reason,ARRAY(SELECT a.id FROM task_center_artifacts a WHERE $5 AND a.task_id=$6 AND a.item_id=items.id ORDER BY a.id)
 FROM items WHERE id>$7 ORDER BY id LIMIT $8`, d.Source, d.sourceID, d.Provider, d.State, a.Require("tasks.artifacts.read") == nil, id, p.AfterID, p.Limit+1, p.AsOf)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var i Item
		if e = rows.Scan(&i.ID, &i.State, &i.Acceptance, &i.Reason, pq.Array(&i.Artifacts)); e != nil {
			return nil, e
		}
		i.Retryable = a.Require("tasks.retry") == nil && d.canRetry(a) && i.State == "failed"
		out = append(out, i)
	}
	return out, rows.Err()
}
func (s *Store) Artifact(ctx context.Context, a Scope, task, id string) (ArtifactContent, error) {
	var v ArtifactContent
	if e := a.Require("tasks.artifacts.read"); e != nil {
		return v, e
	}
	e := s.DB.QueryRowContext(ctx, `SELECT a.id,a.content_type,a.size_bytes,a.expires_at,a.state,CASE WHEN h.kind='video' THEN 'media_video' ELSE h.source END,a.locator,h.source_id,a.item_id,h.owner_id,h.api_key_id
 FROM task_center_history h JOIN task_center_artifacts a ON a.task_id=h.id WHERE h.workspace_id=$1 AND h.project_id=$2 AND h.id=$3 AND a.id=$4`, a.WorkspaceID, a.ProjectID, task, id).
		Scan(&v.ID, &v.ContentType, &v.Size, &v.Expires, &v.State, &v.Source, &v.Locator, &v.SourceID, &v.ItemID, &v.Owner, &v.Key)
	if errors.Is(e, sql.ErrNoRows) {
		return v, fault.NotFound
	}
	if e == nil && v.State != "available" {
		e = Gone
	}
	return v, e
}
func (s *Store) Revoke(ctx context.Context, a Scope, task, id string) error {
	if e := a.Require("tasks.artifacts.revoke"); e != nil {
		return e
	}
	res, e := s.DB.ExecContext(ctx, `INSERT INTO image_artifact_revocations(task_id,artifact_id,actor_id) SELECT h.id,i.id,$5 FROM task_center_history h
 JOIN task_center_artifacts i ON i.task_id=h.id WHERE h.workspace_id=$1 AND h.project_id=$2 AND h.id=$3 AND i.id=$4
 ON CONFLICT(task_id,artifact_id) DO UPDATE SET revoked_at=image_artifact_revocations.revoked_at`, a.WorkspaceID, a.ProjectID, task, id, a.ActorID)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if n == 0 {
		return fault.NotFound
	}
	return e
}
