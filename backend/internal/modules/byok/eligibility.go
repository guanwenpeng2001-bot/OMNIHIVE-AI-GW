package byok

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	classes "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials/byok"
	"slices"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
)

// CandidateID binds the funding lane, revision and concrete native account in
// the BE-08/10 graph. Returning an opaque ID keeps connection IDs out of traces.
func CandidateID(c Credential, account int64, source string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("byok/v1/%d/%d/%d/%d/%s", c.Workspace, c.ID, c.Version, account, source)))
	return fmt.Sprintf("by_%x", sum[:16])
}

const principal = ` JOIN byok_credentials c ON c.workspace_id=k.workspace_id AND k.id=ANY(c.key_ids)
    JOIN accounts ca ON ca.workspace_id=c.workspace_id AND ca.id=c.id AND ca.deleted_at IS NULL
    JOIN byok_pilot_providers pilot ON pilot.workspace_id=c.workspace_id AND pilot.provider=c.provider
    JOIN workspace_members m ON m.workspace_id=c.workspace_id AND m.user_id=$3
    LEFT JOIN byok_provider_classes pc ON pc.workspace_id=c.workspace_id AND pc.provider=c.provider AND pc.auth_type=c.auth_type
    LEFT JOIN byok_service_accounts sa ON sa.api_key_id=k.id
    JOIN users u ON u.id=m.user_id AND u.deleted_at IS NULL AND u.status='active'
    JOIN workspaces w ON w.id=k.workspace_id
    WHERE k.workspace_id=$1 AND p.id=$2 AND k.id=$4 AND k.user_id=m.user_id
    AND p.id=ANY(c.project_ids) AND coalesce(pc.enabled,(pilot.enabled AND c.auth_type='api_key'))
    AND ((sa.id IS NULL AND m.id=ANY(c.member_bindings) AND m.user_id=ANY(c.member_ids)) OR
         (sa.workspace_id=c.workspace_id AND sa.project_id=p.id AND sa.membership_id=m.id AND sa.id=ANY(c.service_account_ids) AND sa.revoked_at IS NULL))
    AND $6=ANY(c.model_ids) AND ` + keyActive + `
    AND (x.api_key_id IS NULL OR x.model_mode='inherit' OR (x.model_mode='allow_list' AND $6=ANY(x.models)))
    AND EXISTS(SELECT FROM api_key_credentials ak WHERE ak.api_key_id=k.id AND ak.workspace_id=k.workspace_id
        AND ak.revoked_at IS NULL AND (ak.valid_until IS NULL OR ak.valid_until>clock_timestamp())
        AND ((x.api_key_id IS NULL AND ak.status IN ('current','grace')) OR (x.api_key_id IS NOT NULL AND ak.status='project')))`

const grants = principal + ` AND w.scope_version=$5 AND $6=ANY(c.verified_models) AND c.state='verified' AND c.verified_until>clock_timestamp()`

func eligibleArgs(a scope.Authorized, model string) ([]any, error) {
	if a.WorkspaceID < 1 || a.ProjectID < 1 || a.ActorID < 1 || a.KeyID < 1 || a.Version < 1 || a.KeyOwnerID != a.ActorID || a.FundingUserID != a.ActorID || model == "" {
		return nil, fault.Forbidden
	}
	return []any{a.WorkspaceID, a.ProjectID, a.ActorID, a.KeyID, a.Version, model}, nil
}

// Eligible returns only currently authorized tenant credentials in primary/backup order.
// The native producer intersects these with endpoint/model/capacity policy.
func (s *Store) Eligible(ctx context.Context, a scope.Authorized, model string) ([]Credential, error) {
	args, e := eligibleArgs(a, model)
	if e != nil {
		return nil, e
	}
	rows, e := s.db.QueryContext(ctx, "SELECT "+metadata+keyProject+grants+" ORDER BY c.priority,c.id", args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		c, e := scanCredential(rows)
		if e != nil {
			return nil, e
		}
		if classes.Supported(c.Provider, c.Auth) {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}
func eligibleOne(ctx context.Context, tx *sql.Tx, a scope.Authorized, model string, id int64) (Credential, error) {
	args, e := eligibleArgs(a, model)
	if e != nil {
		return Credential{}, e
	}
	c, e := scanCredential(tx.QueryRowContext(ctx, "SELECT "+metadata+keyProject+grants+" AND c.id=$7 FOR SHARE OF c,k,m,p,w,u,ca,pilot", append(args, id)...))
	if errors.Is(e, fault.NotFound) {
		e = unavailable
	}
	if e == nil {
		_, e = enabled(ctx, tx, c.Workspace, c.Provider, c.Auth)
	}
	return c, e
}

// Estimate is a metadata-only management read. The actor must manage the
// requested project and presents a specific granted key and its owning member.
func (s *Store) Estimate(ctx context.Context, t Target, member, key int64, model string, b Bounds) (Estimate, error) {
	access, e := s.access.Evaluate(ctx, t.Actor, t.Workspace, t.Project, Manage)
	if e != nil {
		return Estimate{}, e
	}
	a := scope.Authorized{WorkspaceID: t.Workspace, ProjectID: t.Project, ActorID: member, KeyOwnerID: member, FundingUserID: member, KeyID: key, Version: access.Version}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Estimate{}, e
	}
	defer tx.Rollback()
	c, e := eligibleOne(ctx, tx, a, model, t.ID)
	if e != nil {
		return Estimate{}, e
	}
	// The manager of one project can inspect only that project's portion of a grant.
	if !slices.Contains(c.Scope.Projects, decimalID(t.Project)) {
		return Estimate{}, fault.Forbidden
	}
	cost, _, e := price(ctx, tx, c, a.ProjectID, c.ID, "byok", model, b, time.Now().UTC())
	if e != nil {
		return cost, e
	}
	return cost, tx.Commit()
}

func (s *Store) WorkspaceEstimate(ctx context.Context, t Target, project, member, key int64, model string, b Bounds) (Estimate, error) {
	if _, err := s.access.Evaluate(ctx, t.Actor, t.Workspace, 0, Manage); err != nil {
		return Estimate{}, err
	}
	t.Project = project
	return s.Estimate(ctx, t, member, key, model, b)
}
