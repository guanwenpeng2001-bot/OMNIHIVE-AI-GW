package byok

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/connections"
	classes "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials/byok"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

func (s *Store) change(ctx context.Context, tx *sql.Tx, cmd Command) (any, error) {
	if cmd.Action == "create" {
		return s.create(ctx, tx, cmd.Target, cmd.Create)
	}
	verify := cmd.Action == "verify" || cmd.Action == "rotate"
	c, e := current(ctx, tx, cmd.Target, !verify)
	if e != nil {
		return nil, e
	}
	if c.State == "revoked" && (cmd.Action != "revoke" || cmd.Mode != "force") {
		return nil, unavailable
	}
	if cmd.Action != "revoke" {
		if _, e = enabled(ctx, tx, c.Workspace, c.Provider, c.Auth); e != nil {
			return nil, e
		}
	}
	switch cmd.Action {
	case "policy", "patch":
		p := cmd.Policy
		keepBindings := false
		if cmd.Action == "patch" {
			if cmd.Patch == nil {
				return nil, fault.Invalid("/patch")
			}
			p, e = cmd.Patch.apply(c)
			if e != nil {
				return nil, e
			}
			keepBindings = cmd.Patch.Members == nil
		}
		bindings, e := checkGrants(ctx, tx, cmd.Target, p)
		if e != nil {
			return nil, e
		}
		return scanCredential(tx.QueryRowContext(ctx, `UPDATE byok_credentials AS c SET project_ids=$3,member_ids=$4,member_bindings=CASE WHEN $12 THEN member_bindings ELSE $5 END,key_ids=$6,model_ids=$7,
        priority=$8,allow_platform_fallback=$9,include_external_in_budget=$10,service_account_ids=$11,self_service=true,version=version+1 WHERE workspace_id=$1 AND id=$2 RETURNING `+metadata, c.Workspace, c.ID, pq.Array(p.Projects), pq.Array(nonNil(p.Members)), pq.Array(bindings), pq.Array(p.Keys), pq.Array(p.Models), p.Priority, p.Fallback, p.Budget, pq.Array(nonNil(p.ServiceAccounts)), keepBindings))
	case "revoke":
		if cmd.Mode != "force" && cmd.Mode != "drain" {
			return nil, fault.Invalid("/mode")
		}
		if e = retire(ctx, tx, c); e != nil {
			return nil, e
		}
		c, e = scanCredential(tx.QueryRowContext(ctx, `UPDATE byok_credentials AS c SET state='revoked',force_revoked_at=CASE WHEN $3='force' THEN coalesce(force_revoked_at,clock_timestamp()) ELSE force_revoked_at END,verified_until=NULL,verified_models='{}',version=version+1 WHERE workspace_id=$1 AND id=$2 RETURNING `+metadata, c.Workspace, c.ID, cmd.Mode))
	case "verify", "rotate":
		var secret map[string]any
		if cmd.Action == "rotate" {
			secret, e = classes.Payload(c.Provider, c.Auth, cmd.Secret)
		} else {
			secret, e = s.resolve(ctx, tx, c)
		}
		if e != nil {
			return nil, e
		}
		account := &service.Account{ID: c.ID, WorkspaceID: c.Workspace, Platform: c.Provider, Type: legacy.AccountType(c.Auth), Credentials: secret}
		var result service.ConnectionProbe
		if c.Auth == "service_account" || c.Auth == "bedrock" || (c.Auth == "oauth" && (c.Provider == "cursor" || c.Provider == "antigravity")) {
			if probe, ok := s.probe.(interface {
				ProbeBYOKModels(context.Context, *service.Account, []string) service.ConnectionProbe
			}); ok {
				result = probe.ProbeBYOKModels(ctx, account, c.Scope.Models)
			}
		} else {
			result = s.probe.ProbeConnection(ctx, account, "")
		}
		if _, e = current(ctx, tx, cmd.Target, true); e != nil {
			return nil, e
		}
		expiry := classes.BearerExpiry(c.Auth, secret)
		if (expiry != nil && !expiry.After(time.Now())) || result.Network != "passed" || result.Authorization != "passed" || result.Discovery != "passed" || len(result.Models) == 0 {
			if cmd.Action == "rotate" {
				return nil, unverified
			}
			c, e = scanCredential(tx.QueryRowContext(ctx, `UPDATE byok_credentials AS c SET state='unverified',verified_until=NULL,verified_models='{}',version=version+1 WHERE workspace_id=$1 AND id=$2 RETURNING `+metadata, c.Workspace, c.ID))
			if e != nil {
				return nil, e
			}
			return record(ctx, tx, cmd, c, false)
		}
		if cmd.Action == "rotate" {
			if e = retire(ctx, tx, c); e != nil {
				return nil, e
			}
			c.SecretVersion++
			ref, cipher, fp, e := s.protect(ctx, c.Workspace, c.ID, c.SecretVersion, secret)
			if e != nil {
				return nil, e
			}
			c.Fingerprint = fp
			if e = install(ctx, tx, c, ref, cipher); e != nil {
				return nil, e
			}
		}
		c, e = scanCredential(tx.QueryRowContext(ctx, `UPDATE byok_credentials AS c SET state='verified',verified_until=least(clock_timestamp()+interval '24 hours',$6::timestamptz),verified_models=$3,version=version+1,secret_version=$4,fingerprint=$5 WHERE workspace_id=$1 AND id=$2 RETURNING `+metadata, c.Workspace, c.ID, pq.Array(result.Models), c.SecretVersion, c.Fingerprint, expiry))
	default:
		return nil, fault.Invalid("/operation")
	}
	if e != nil {
		return nil, e
	}
	return record(ctx, tx, cmd, c, true)
}

func record(ctx context.Context, tx *sql.Tx, cmd Command, c Credential, success bool) (identity.Operation, error) {
	id := decimalID(c.ID)
	op := identity.Operation{ID: uuid.NewString(), State: "succeeded", ResourceID: &id, OwnerID: cmd.Actor, ProjectID: cmd.Project, Result: c, DelegatedAction: Manage}
	if !success {
		op.State = "failed"
		op.Error = map[string]string{"reason": unverified.Reason}
	}
	op.StatusURL = fmt.Sprintf("/api/v2/workspaces/%d/operations/%s", cmd.Workspace, op.ID)
	doc, e := json.Marshal(op)
	if e != nil {
		return op, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO byok_operations(id,workspace_id,project_scope,actor_id,credential_id,event,result) VALUES($1,$2,$3,$4,$5,$6,$7)`, op.ID, cmd.Workspace, cmd.Project, cmd.Actor, c.ID, cmd.Action, string(doc))
	return op, e
}
func (s *Store) ReadOperation(ctx context.Context, w, actor int64, id string) (identity.Operation, error) {
	var op identity.Operation
	if _, e := uuid.Parse(id); e != nil {
		return op, fault.NotFound
	}
	var raw []byte
	e := s.db.QueryRowContext(ctx, `SELECT project_scope,actor_id,result FROM byok_operations WHERE id=$1 AND workspace_id=$2`, id, w).Scan(&op.ProjectID, &op.OwnerID, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return op, fault.NotFound
	}
	if e != nil {
		return op, e
	}
	if _, e = s.access.Evaluate(ctx, actor, w, op.ProjectID, Manage); e != nil {
		return op, e
	}
	op.DelegatedAction = Manage
	raw, e = completePilotResponse(raw)
	if e != nil {
		return op, e
	}
	e = json.Unmarshal(raw, &op)
	return op, e
}
