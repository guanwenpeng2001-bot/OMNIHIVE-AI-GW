package tasks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks/media"
)

type command struct {
	tx               *sql.Tx
	scope            Scope
	route            string
	key, fingerprint []byte
	replay           []byte
}

func (s *Store) command(ctx context.Context, a Scope, route, key string, body any) (*command, error) {
	if strings.TrimSpace(key) == "" || len(key) > 200 {
		return nil, fault.Invalid("/Idempotency-Key")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	k, h := sha256.Sum256([]byte(key)), sha256.Sum256(b)
	lock := fmt.Sprintf("image-task/%d/%d/%d/%s/%x", a.WorkspaceID, a.ProjectID, a.ActorID, route, k)
	// Release the connection between attempts. A burst of identical submits
	// must leave the winning request a connection for ordinary gateway admission.
	var tx *sql.Tx
	for {
		tx, err = s.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		var acquired bool
		err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, lock).Scan(&acquired)
		if err == nil && acquired {
			break
		}
		tx.Rollback()
		if err != nil {
			return nil, err
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c := &command{tx: tx, scope: a, route: route, key: k[:], fingerprint: h[:]}
	var fp []byte
	err = tx.QueryRowContext(ctx, `SELECT fingerprint,response FROM image_task_commands WHERE workspace_id=$1 AND project_id=$2 AND actor_id=$3 AND route=$4 AND key_hash=$5`, a.WorkspaceID, a.ProjectID, a.ActorID, route, k[:]).Scan(&fp, &c.replay)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	} else if err == nil && !bytes.Equal(fp, h[:]) {
		err = fault.Conflict
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return c, nil
}
func (c *command) save(ctx context.Context, id string, data any) error {
	b, e := json.Marshal(data)
	if e != nil {
		return e
	}
	_, e = c.tx.ExecContext(ctx, `INSERT INTO image_task_commands(workspace_id,project_id,actor_id,route,key_hash,fingerprint,task_id,response) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, c.scope.WorkspaceID, c.scope.ProjectID, c.scope.ActorID, c.route, c.key, c.fingerprint, id, string(b))
	if e != nil {
		return e
	}
	return c.tx.Commit()
}
func (s *Store) Create(ctx context.Context, a Scope, c Create, key, ip string) (Detail, error) {
	var d Detail
	if e := a.Require("tasks.create"); e != nil {
		return d, e
	}
	if c.Kind != "image" && c.Kind != "audio" && c.Kind != "video" {
		return d, fault.Invalid("/kind")
	}
	if !c.Acknowledged {
		return d, fault.Invalid("/funding_acknowledged")
	}
	if c.Key <= 0 {
		return d, fault.Invalid("/api_key_id")
	}
	if c.Model == "" || len(c.Model) > 255 {
		return d, fault.Invalid("/model_id")
	}
	if c.Kind == "image" {
		if c.Input.Format != "" || c.Input.Duration != 0 {
			return d, fault.Invalid("/input")
		}
		if strings.TrimSpace(c.Input.Prompt) == "" || utf8.RuneCountInString(c.Input.Prompt) > 8000 {
			return d, fault.Invalid("/input/prompt")
		}
		if c.Input.Count == 0 {
			c.Input.Count = 1
		}
		if c.Input.Count < 1 || c.Input.Count > 4 {
			return d, fault.Invalid("/input/count")
		}
		if len(c.Input.Size) > 32 {
			return d, fault.Invalid("/input/size")
		}
	} else if err := media.Validate(c.Kind, c.Input.Prompt, c.Input.Format, c.Input.Size, c.Input.Duration, c.Input.Count); err != nil {
		return d, err
	}
	cmd, e := s.command(ctx, a, "create", key, c)
	if e != nil {
		return d, e
	}
	defer cmd.tx.Rollback()
	if cmd.replay != nil {
		e = json.Unmarshal(cmd.replay, &d)
		return d, e
	}
	return s.submit(ctx, cmd, c, "", ip, "tasks.create")
}
func (s *Store) Retry(ctx context.Context, a Scope, id, key, ip string, r Retry) (RetryResult, error) {
	var out RetryResult
	if e := a.Require("tasks.retry"); e != nil {
		return out, e
	}
	if !r.Acknowledged {
		return out, fault.Invalid("/funding_acknowledged")
	}
	slices.Sort(r.Items)
	cmd, e := s.command(ctx, a, "retry/"+id, key, r)
	if e != nil {
		return out, e
	}
	defer cmd.tx.Rollback()
	if cmd.replay != nil {
		e = json.Unmarshal(cmd.replay, &out)
		return out, e
	}
	d, e := readDetail(cmd.tx.QueryRowContext(ctx, projection+" AND h.id=$3", a.WorkspaceID, a.ProjectID, id), a)
	if e != nil {
		return out, e
	}
	if d.Provider == "unknown" || d.State == "unknown" {
		return out, Unknown
	}
	if !d.canRetry(a) {
		return out, NotRetryable
	}
	if len(r.Items) > 0 && (len(r.Items) != 1 || r.Items[0] != "1") {
		return out, fault.Invalid("/item_ids")
	}
	var encrypted string
	var original []byte
	e = cmd.tx.QueryRowContext(ctx, `SELECT replay_input,submission_key FROM image_tasks WHERE id=$1 AND input_until>clock_timestamp() FOR UPDATE`, id).Scan(&encrypted, &original)
	if e != nil {
		return out, found(e)
	}
	if bytes.Equal(original, cmd.key) {
		return out, fault.Conflict
	}
	if s.Cipher == nil {
		return out, Unavailable
	}
	plain, e := s.Cipher.Decrypt(encrypted)
	if e != nil {
		return out, e
	}
	var c Create
	if e = json.Unmarshal([]byte(plain), &c); e != nil {
		return out, e
	}
	if c.Key != d.key || c.Model != d.Model || c.Kind != d.Kind || !c.Acknowledged {
		return out, NotRetryable
	}
	d, e = s.submit(ctx, cmd, c, id, ip, "tasks.retry")
	if e != nil {
		return out, e
	}
	return RetryResult{d.ID, id, *d.Request, d.State}, nil
}
func (s *Store) submit(ctx context.Context, c *command, in Create, parent, ip, action string) (Detail, error) {
	var d Detail
	if s.Runner == nil || s.Cipher == nil {
		return d, Unavailable
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return d, &fault.Fault{Status: 429, Reason: "TASK_CAPACITY", Message: "All task workers are busy."}
	}
	launched := false
	defer func() {
		if !launched {
			<-s.slots
		}
	}()
	work := Work{ID: "task_" + uuid.NewString(), Request: uuid.NewString(), Model: in.Model, Key: in.Key, Kind: in.Kind, Input: in.Input, IP: ip, Action: action}
	work.Lease = Lease{ID: work.ID, Owner: uuid.NewString(), Generation: 1}
	plan, e := s.Runner.Prepare(ctx, c.scope, work)
	if e != nil {
		return d, e
	}
	defer func() {
		if !launched {
			plan.Close()
		}
	}()
	plain, e := json.Marshal(in)
	if e != nil {
		return d, e
	}
	encrypted, e := s.Cipher.Encrypt(string(plain))
	if e != nil {
		return d, e
	}
	_, e = c.tx.ExecContext(ctx, `INSERT INTO image_tasks(id,workspace_id,project_id,actor_id,owner_id,funding_user_id,api_key_id,source,source_id,model,request_id,
 adapter_version,fingerprint,submission_key,parent_id,state,acceptance,generation,lease_owner,lease_until,replay_input,input_until)
 VALUES($1,$2,$3,$4,$4,$4,$5,'gateway',$1,$6,$7,$8,$9,$10,nullif($11,''),'running','unknown',1,$12,clock_timestamp()+interval '32 minutes',$13,clock_timestamp()+interval '24 hours')`,
		work.ID, c.scope.WorkspaceID, c.scope.ProjectID, c.scope.ActorID, work.Key, work.Model, work.Request, plan.Version, c.fingerprint, c.key, parent, work.Lease.Owner, encrypted)
	if e != nil {
		return d, e
	}
	if in.Kind != "image" {
		if plan.Media == nil {
			return d, media.Unsupported
		}
		var registered string
		e = c.tx.QueryRowContext(ctx, `INSERT INTO task_media_attributes(task_id,kind,verification_id,account_id,format,duration_seconds,size,billing_unit,price_version,provider_job_id)
 SELECT $1,$2,v.id,v.account_id,$3,nullif($4,0),nullif($5,''),v.billing_unit,v.price_version,CASE WHEN $2='video' THEN $6::text END
 FROM task_media_verifications v WHERE v.id=$7 AND v.workspace_id=$8 AND v.project_id=$9 AND v.account_id=$10 AND v.price_version=$11
 AND v.verified_at<=clock_timestamp() AND v.expires_at>clock_timestamp() AND v.revoked_at IS NULL RETURNING task_id`, work.ID, in.Kind, in.Input.Format, in.Input.Duration, in.Input.Size, work.Request, plan.Media.ID, c.scope.WorkspaceID, c.scope.ProjectID, plan.Media.Account, plan.Media.PriceVersion).Scan(&registered)
		if errors.Is(e, sql.ErrNoRows) {
			return d, media.Unsupported
		}
		if e != nil {
			return d, e
		}
	}
	d, e = readDetail(c.tx.QueryRowContext(ctx, projection+" AND h.id=$3", c.scope.WorkspaceID, c.scope.ProjectID, work.ID), c.scope)
	if e != nil {
		return d, e
	}
	d.ArtifactState = "pending"
	var response any = d
	if parent != "" {
		response = RetryResult{d.ID, parent, work.Request, d.State}
	}
	if e = c.save(ctx, work.ID, response); e != nil {
		return d, e
	}
	launched = true
	go func() {
		defer func() { <-s.slots }()
		defer plan.Close()
		result := func() (out Outcome) {
			defer func() {
				if recover() != nil {
					out = Outcome{State: "unknown", Acceptance: "unknown"}
				}
			}()
			runctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
			defer cancel()
			return plan.Run(runctx)
		}()
		persist, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// On a lost commit acknowledgment or expired fence, the public projection
		// reports unknown. This goroutine never dispatches a replacement generation.
		_ = s.Finish(persist, work.Lease, result)
	}()
	return d, nil
}

type Lease struct {
	ID, Owner  string
	Generation int64
}

// Finish commits output relations and the terminal observation under one live fence.
func (s *Store) Finish(ctx context.Context, l Lease, r Outcome) error {
	if !slices.Contains([]string{"succeeded", "failed", "unknown", "canceled"}, r.State) || !slices.Contains([]string{"accepted", "rejected", "not_sent", "unknown"}, r.Acceptance) {
		return fault.Invalid("/outcome")
	}
	if r.State == "succeeded" && (r.Acceptance != "accepted" || len(r.Artifacts) == 0) {
		return fault.Invalid("/artifacts")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	res, e := tx.ExecContext(ctx, `UPDATE image_tasks SET state=$4,acceptance=$5,progress=$6,version=version+1,updated_at=clock_timestamp(),
 cancel_state=CASE WHEN $4='canceled' THEN 'confirmed' ELSE cancel_state END,lease_owner=NULL,lease_until=NULL
 WHERE id=$1 AND lease_owner=$2 AND generation=$3 AND state='running' AND source='gateway' AND lease_until>clock_timestamp()`, l.ID, l.Owner, l.Generation, r.State, r.Acceptance, r.Progress)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrLostLease
	}
	if r.DurationMillis > 0 {
		_, e = tx.ExecContext(ctx, `UPDATE task_media_attributes SET result_duration_ms=$2 WHERE task_id=$1 AND kind='audio'`, l.ID, r.DurationMillis)
		if e != nil {
			return e
		}
	}
	for _, a := range r.Artifacts {
		_, e = tx.ExecContext(ctx, `INSERT INTO image_task_artifacts(task_id,id,item_id,locator,content_type,size_bytes,expires_at)
 VALUES($1,$2,'1',$3,$4,$5,clock_timestamp()+interval '24 hours')`, l.ID, a.ID, a.Locator, a.ContentType, a.Size)
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}

// Recover expires lost generations without sending the request again.
func (s *Store) Recover(ctx context.Context) error {
	_, e := s.DB.ExecContext(ctx, `UPDATE image_tasks SET state='unknown',acceptance='unknown',generation=generation+1,version=version+1,lease_owner=NULL,lease_until=NULL,
 updated_at=clock_timestamp() WHERE source='gateway' AND state='running' AND lease_until<=clock_timestamp()`)
	if e != nil {
		return e
	}
	_, e = s.DB.ExecContext(ctx, `UPDATE image_tasks SET replay_input=NULL WHERE replay_input IS NOT NULL AND input_until<=clock_timestamp()`)
	return e
}
func (s *Store) Cancel(ctx context.Context, a Scope, id, key string) (CancelResult, error) {
	var out CancelResult
	if e := a.Require("tasks.cancel"); e != nil {
		return out, e
	}
	c, e := s.command(ctx, a, "cancel/"+id, key, struct{}{})
	if e != nil {
		return out, e
	}
	defer c.tx.Rollback()
	if c.replay != nil {
		e = json.Unmarshal(c.replay, &out)
		return out, e
	}
	d, e := readDetail(c.tx.QueryRowContext(ctx, projection+" AND h.id=$3", a.WorkspaceID, a.ProjectID, id), a)
	if e != nil {
		return out, e
	}
	if d.Cancel == "confirmed" {
		out = CancelResult{id, "confirmed", d.State, d.Version}
		return out, c.save(ctx, id, out)
	}
	if d.Cancel != "available" || (d.Source != "batch_image" && d.Kind != "video") {
		return out, UnsupportedCancel
	}
	if (d.Kind == "video" && (s.Video == nil || d.providerJob == nil)) || (d.Kind != "video" && s.Batch == nil) {
		return out, Unavailable
	}
	res, e := c.tx.ExecContext(ctx, `UPDATE image_tasks SET cancel_state='requested' WHERE id=$1 AND cancel_state<>'requested' AND EXISTS(SELECT 1 FROM task_center_history WHERE id=$1 AND display_cancel='available')`, id)
	if e != nil {
		return out, e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return out, e
	}
	if n == 0 {
		return out, UnsupportedCancel
	}
	out = CancelResult{id, "requested", d.State, d.Version + 1}
	if e = c.save(ctx, id, out); e != nil {
		return out, e
	}
	// No second provider invocation after a crash or lost acknowledgement.
	if d.Kind == "video" {
		e = s.Video.CancelVideo(ctx, d.owner, d.key, *d.providerJob)
	} else {
		e = s.Batch.CancelBatch(ctx, d.owner, d.key, d.sourceID)
	}
	if e != nil {
		return out, Unknown
	}
	return out, nil
}
