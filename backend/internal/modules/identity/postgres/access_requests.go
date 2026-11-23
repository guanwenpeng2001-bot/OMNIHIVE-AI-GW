package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

func fingerprint(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	return sum[:], nil
}

func memberInstance(ctx context.Context, q queryer, w, user int64) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT m.id FROM workspace_members m JOIN users u ON u.id=m.user_id AND u.status='active' AND u.deleted_at IS NULL WHERE m.workspace_id=$1 AND m.user_id=$2`, w, user).Scan(&id)
	return id, found(err)
}

func teamProject(ctx context.Context, tx *sql.Tx, w, p int64) error {
	var shared bool
	err := tx.QueryRowContext(ctx, `SELECT w.is_default AND p.is_default FROM projects p JOIN workspaces w ON p.workspace_id=w.id
 WHERE w.id=$1 AND p.id=$2 AND p.archived_at IS NULL`, w, p).Scan(&shared)
	if err != nil {
		return found(err)
	}
	if shared {
		return fault.Invalid("/project_id")
	}
	return nil
}

func (s *Store) RequestAccess(ctx context.Context, actor, w int64, in identity.RequestAccess, key string) (identity.AccessRequest, error) {
	var out identity.AccessRequest
	in.Note = strings.TrimSpace(in.Note)
	if in.Note == "" || strings.ContainsRune(in.Note, 0) || utf8.RuneCountInString(in.Note) > 500 {
		return out, fault.Invalid("/note")
	}
	if len(key) == 0 || len(key) > 200 || strings.ContainsRune(key, 0) || strings.TrimSpace(key) != key {
		return out, fault.Invalid("/Idempotency-Key")
	}
	hash, err := fingerprint(in)
	if err != nil {
		return out, err
	}
	err = s.write(ctx, actor, w, 0, "", func(tx *sql.Tx, _ identity.Access) error {
		mid, err := memberInstance(ctx, tx, w, actor)
		if err != nil {
			return err
		}
		if in.RequestedProjectID != nil {
			if *in.RequestedProjectID <= 0 {
				return fault.Invalid("/requested_project_id")
			}
			if _, err = evaluate(ctx, tx, actor, w, *in.RequestedProjectID, ""); err != nil {
				return err
			}
			if err = teamProject(ctx, tx, w, *in.RequestedProjectID); err != nil {
				return err
			}
		}
		out = identity.AccessRequest{WorkspaceID: w, RequesterID: actor, RequestedProjectID: in.RequestedProjectID, Note: in.Note, State: "pending", Version: 1}
		err = tx.QueryRowContext(ctx, `INSERT INTO project_access_requests(workspace_id,membership_id,requested_project_id,note,request_key,fingerprint)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(membership_id,request_key) DO UPDATE SET request_key=excluded.request_key
 WHERE project_access_requests.fingerprint=excluded.fingerprint RETURNING id,created_at`, w, mid, in.RequestedProjectID, in.Note, key, hash).Scan(&out.ID, &out.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return fault.Conflict
		}
		return err
	})
	return out, err
}

// Project IDs are projected through the actor's current project visibility even
// when a completed request is still part of the membership's history.
func visibleRequestProject(column string) string {
	return "(SELECT p.id FROM projects p WHERE p.id=" + column + " AND p.workspace_id=w.id AND p.archived_at IS NULL AND " + visibleProject + ")"
}

const requestJoins = ` JOIN project_access_requests r ON r.workspace_id=w.id JOIN workspace_members target ON target.workspace_id=w.id AND target.id=r.membership_id `

func requestQuery() string {
	return "SELECT r.id,w.id,target.user_id," + visibleRequestProject("r.requested_project_id") + ",r.note,r.state,r.version,r.created_at," + visibleRequestProject("r.approved_project_id") + ",r.decided_at" + membership + requestJoins + " WHERE w.id=$2 AND " + memberFilter
}

func scanRequest(row interface{ Scan(...any) error }) (identity.AccessRequest, error) {
	var r identity.AccessRequest
	err := row.Scan(&r.ID, &r.WorkspaceID, &r.RequesterID, &r.RequestedProjectID, &r.Note, &r.State, &r.Version, &r.CreatedAt, &r.ApprovedProjectID, &r.DecidedAt)
	return r, found(err)
}

func (s *Store) AccessRequests(ctx context.Context, actor, w int64, view string, p identity.Page) ([]identity.AccessRequest, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := requestQuery()
	switch view {
	case "mine":
		if _, err = memberInstance(ctx, tx, w, actor); err != nil {
			return nil, err
		}
		q += " AND target.id=m.id"
	case "review":
		if _, err = evaluate(ctx, tx, actor, w, 0, "members.grants.write"); err != nil {
			return nil, err
		}
	default:
		return nil, fault.Invalid("/view")
	}
	rows, err := tx.QueryContext(ctx, q+" AND r.id>$3 AND r.created_at<=$4 ORDER BY r.id LIMIT $5", actor, w, p.After, p.AsOf, p.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []identity.AccessRequest{}
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

func (s *Store) DecideAccess(ctx context.Context, actor, w, id, expected int64, in identity.DecideAccess, requestID string) (identity.AccessRequest, error) {
	var out identity.AccessRequest
	switch in.Decision {
	case "approve":
		if in.ProjectID <= 0 || in.MemberVersion <= 0 || len(in.AddActions) == 0 {
			return out, fault.Invalid("/decision")
		}
		var err error
		in.AddActions, err = normalized(in.AddActions, "/add_actions")
		if err != nil {
			return out, err
		}
	case "decline":
		if in.ProjectID != 0 || in.MemberVersion != 0 || in.AddActions != nil {
			return out, fault.Invalid("/decision")
		}
	default:
		return out, fault.Invalid("/decision")
	}
	hash, err := fingerprint(in)
	if err != nil {
		return out, err
	}
	err = s.write(ctx, actor, w, 0, "members.grants.write", func(tx *sql.Tx, a identity.Access) error {
		var version, user, decider int64
		var prior []byte
		err := tx.QueryRowContext(ctx, `SELECT r.version,m.user_id,r.decision_fingerprint,coalesce(r.decision_actor_id,0)
 FROM project_access_requests r JOIN workspace_members m ON m.workspace_id=r.workspace_id AND m.id=r.membership_id
 WHERE r.workspace_id=$1 AND r.id=$2 FOR UPDATE OF r,m`, w, id).Scan(&version, &user, &prior, &decider)
		if err != nil {
			return found(err)
		}
		if prior != nil {
			if bytes.Equal(hash, prior) && actor == decider && (expected == version || expected == version-1) {
				out, err = scanRequest(tx.QueryRowContext(ctx, requestQuery()+" AND r.id=$3", actor, w, id))
				return err
			}
			if expected != version {
				return fault.Stale(version)
			}
			return &fault.Fault{Status: 409, Reason: "REQUEST_DECIDED", Message: "This request already has a decision."}
		}
		if expected != version {
			return fault.Stale(version)
		}
		v, err := memberVersion(ctx, tx, w, user, true)
		if err != nil {
			return err
		}
		state := "declined"
		var project *int64
		diff := []identity.ProjectGrant{}
		if in.Decision == "approve" {
			var active bool
			if err = tx.QueryRowContext(ctx, "SELECT status='active' FROM users WHERE id=$1 AND deleted_at IS NULL FOR SHARE", user).Scan(&active); err != nil {
				return found(err)
			}
			if !active {
				return fault.NotFound
			}
			if v.Version != in.MemberVersion {
				return fault.Stale(v.Version)
			}
			if err = teamProject(ctx, tx, w, in.ProjectID); err != nil {
				return err
			}
			rows := []identity.ProjectGrant{{ProjectID: in.ProjectID, Actions: in.AddActions}}
			if err = validateMatrix(ctx, tx, a, rows, nil); err != nil {
				return err
			}
			_, diff, err = applyMatrix(ctx, tx, a, v, rows, false)
			if err != nil {
				return err
			}
			state = "approved"
			project = &in.ProjectID
		}
		delta, err := json.Marshal(diff)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(actor_user_id,action,request_id,status_code,extra)
 VALUES($1,'project.access_request.decision',$2,200,jsonb_build_object('workspace_id',$3::bigint,'access_request_id',$4::bigint,
 'target_user_id',$5::bigint,'state',$6::text,'added_grants',$7::jsonb,'project_id',$8::bigint))`, actor, requestID, w, id, user, state, string(delta), project)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE project_access_requests SET state=$3,approved_project_id=$4,decision_actor_id=$5,decided_at=now(),version=version+1,decision_fingerprint=$6
 WHERE workspace_id=$1 AND id=$2`, w, id, state, project, actor, hash)
		if err != nil {
			return err
		}
		out, err = scanRequest(tx.QueryRowContext(ctx, requestQuery()+" AND r.id=$3", actor, w, id))
		return err
	})
	return out, err
}
