package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/lib/pq"
)

func (s *Store) write(ctx context.Context, actor, w, p int64, action string, command func(*sql.Tx, identity.Access) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, w).Scan(&locked); err != nil {
		return found(err)
	}
	a, err := evaluate(ctx, tx, actor, w, p, action)
	if err != nil {
		return err
	}
	if err = command(tx, a); err != nil {
		return err
	}
	return tx.Commit()
}

func createOnce[T any](ctx context.Context, tx *sql.Tx, a identity.Access, route, token string, input any, create func() (T, error)) (T, error) {
	var result T
	if len(token) == 0 || len(token) > 200 {
		return result, fault.Invalid("/Idempotency-Key")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256(payload)
	args := []any{a.WorkspaceID, a.ProjectID, a.ActorID, route, token, hash[:]}
	var data []byte
	err = tx.QueryRowContext(ctx, `INSERT INTO identity_create_receipts(workspace_id,project_scope,actor_id,route,request_key,fingerprint)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(workspace_id,project_scope,actor_id,route,request_key)
 DO UPDATE SET fingerprint=excluded.fingerprint WHERE identity_create_receipts.fingerprint=excluded.fingerprint RETURNING result`, args...).Scan(&data)
	if err == sql.ErrNoRows {
		return result, fault.Conflict
	}
	if err != nil {
		return result, err
	}
	if len(data) > 0 {
		err = json.Unmarshal(data, &result)
		return result, err
	}
	result, err = create()
	if err != nil {
		return result, err
	}
	data, err = json.Marshal(result)
	if err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE identity_create_receipts SET result=$6 WHERE workspace_id=$1 AND project_scope=$2 AND actor_id=$3 AND route=$4 AND request_key=$5`, append(args[:5], string(data))...)
	return result, err
}

func nameValue(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", fault.Invalid("/name")
	}
	return name, nil
}

func (s *Store) CreateProject(ctx context.Context, actor, w int64, name, token string) (identity.Project, error) {
	var out identity.Project
	name, err := nameValue(name)
	if err != nil {
		return out, err
	}
	err = s.write(ctx, actor, w, 0, "projects.create", func(tx *sql.Tx, a identity.Access) error {
		var err error
		out, err = createOnce(ctx, tx, a, "POST projects", token, name, func() (identity.Project, error) {
			var p identity.Project
			err := tx.QueryRowContext(ctx, `INSERT INTO projects(workspace_id,name) VALUES($1,$2) RETURNING id,workspace_id,name,version`, w, name).Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Version)
			return p, err
		})
		return err
	})
	return out, err
}

func (s *Store) ChangeProject(ctx context.Context, actor, w, p, v int64, name string, archive bool) (identity.Project, error) {
	var out identity.Project
	action := "projects.delete"
	if !archive {
		var err error
		name, err = nameValue(name)
		if err != nil {
			return out, err
		}
		action = "projects.edit"
	}
	err := s.write(ctx, actor, w, p, action, func(tx *sql.Tx, _ identity.Access) error {
		var current int64
		var def bool
		if err := tx.QueryRowContext(ctx, `SELECT version,is_default FROM projects WHERE workspace_id=$1 AND id=$2 AND archived_at IS NULL FOR UPDATE`, w, p).Scan(&current, &def); err != nil {
			return found(err)
		}
		if current != v {
			return fault.Stale(current)
		}
		if archive && def {
			return &fault.Fault{Status: 409, Reason: "DEFAULT_PROJECT", Message: "The default project is required."}
		}
		return tx.QueryRowContext(ctx, `UPDATE projects SET name=CASE WHEN $4 THEN name ELSE $3 END,
  archived_at=CASE WHEN $4 THEN now() END,version=version+1 WHERE workspace_id=$1 AND id=$2 RETURNING id,workspace_id,name,version`, w, p, name, archive).Scan(&out.ID, &out.WorkspaceID, &out.Name, &out.Version)
	})
	return out, err
}

func normalized(values []string, field string) ([]string, error) {
	if len(values) > 256 {
		return nil, fault.Invalid(field)
	}
	out := slices.Clone(values)
	if out == nil {
		out = []string{}
	}
	slices.Sort(out)
	for _, v := range out {
		if v == "" || v != strings.TrimSpace(v) || len(v) > 256 {
			return nil, fault.Invalid(field)
		}
	}
	return slices.Compact(out), nil
}

func (s *Store) CreateKey(ctx context.Context, actor, w, p int64, in identity.CreateKey, token string) (identity.Key, error) {
	var out identity.Key
	var err error
	in.Name, err = nameValue(in.Name)
	if err != nil {
		return out, err
	}
	in.Actions, err = normalized(in.Actions, "/actions")
	if err != nil {
		return out, err
	}
	if len(in.Actions) == 0 {
		return out, fault.Invalid("/actions")
	}
	in.ModelAccess.IDs, err = normalized(in.ModelAccess.IDs, "/model_access/ids")
	if err != nil {
		return out, err
	}
	switch in.ModelAccess.Mode {
	case "allow_list":
		if len(in.ModelAccess.IDs) == 0 {
			return out, fault.Invalid("/model_access/ids")
		}
	case "inherit", "deny_all":
		if len(in.ModelAccess.IDs) > 0 {
			return out, fault.Invalid("/model_access/ids")
		}
	default:
		return out, fault.Invalid("/model_access/mode")
	}
	if in.ExpiresAt != nil {
		v := in.ExpiresAt.UTC()
		in.ExpiresAt = &v
	}
	var issued *string
	err = s.write(ctx, actor, w, p, "keys.create", func(tx *sql.Tx, a identity.Access) error {
		for _, action := range in.Actions {
			if err := a.Require(action); err != nil {
				return err
			}
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_actions WHERE (category='key' OR action='models.read') AND action=ANY($1)`, pq.Array(in.Actions)).Scan(&count); err != nil {
			return err
		}
		if count != len(in.Actions) {
			return fault.Invalid("/actions")
		}
		var err error
		out, err = createOnce(ctx, tx, a, "POST keys", token, in, func() (identity.Key, error) {
			k := identity.Key{Name: in.Name, OwnerID: actor, Version: 1, Prefix: identity.ProjectKeyPrefix,
				Status: "active", ExpiresAt: in.ExpiresAt, EvaluatedAt: time.Now().UTC(), Actions: slices.Clone(in.Actions), ModelAccess: in.ModelAccess}
			if in.ExpiresAt != nil && !in.ExpiresAt.After(time.Now()) {
				return k, fault.Invalid("/expires_at")
			}
			var random [32]byte
			if _, err := rand.Read(random[:]); err != nil {
				return k, err
			}
			secret := k.Prefix + base64.RawURLEncoding.EncodeToString(random[:])
			digest := sha256.Sum256([]byte(secret))
			group, err := s.keyGroup(ctx, tx, a, in)
			if err != nil {
				return k, err
			}
			k.GroupID = group
			if group != nil {
				if err := tx.QueryRowContext(ctx, `SELECT platform FROM groups WHERE id=$1 AND workspace_id=$2`, *group, w).Scan(&k.Platform); err != nil {
					return k, err
				}
			}
			if err := tx.QueryRowContext(ctx, `INSERT INTO api_keys(workspace_id,user_id,name,status,expires_at,group_id) VALUES($1,$2,$3,'active',$4,$5) RETURNING id`, w, actor, k.Name, in.ExpiresAt, group).Scan(&k.ID); err != nil {
				return k, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO project_key_policies(api_key_id,workspace_id,project_id,actions,model_mode,models,member_bound,issued_membership)
 SELECT $1,$2,$3,$4,$5,$6,m.id IS NOT NULL,m.id FROM users u LEFT JOIN workspace_members m ON m.user_id=u.id AND m.workspace_id=$2 WHERE u.id=$7`, k.ID, w, p, pq.Array(in.Actions), in.ModelAccess.Mode, pq.Array(in.ModelAccess.IDs), actor); err != nil {
				return k, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO api_key_credentials(workspace_id,api_key_id,credential_digest,display_suffix,status) VALUES($1,$2,$3,$4,'project')`, w, k.ID, hex.EncodeToString(digest[:]), secret[len(secret)-4:]); err != nil {
				return k, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO project_resources(kind,resource_id,workspace_id,project_id) VALUES('api_keys',$1,$2,$3)`, k.ID, w, p); err != nil {
				return k, err
			}
			var models []string
			if in.ModelAccess.Mode != "inherit" {
				models = slices.Clone(in.ModelAccess.IDs)
				if models == nil {
					models = []string{}
				}
			}
			if err := WriteKeyModelRule(ctx, tx, w, k.ID, identity.ModelRule{Models: models, Exclude: []string{}}); err != nil {
				return k, err
			}
			k.ModelPolicySource = "model_access_rules"
			k.ExcludedModels = []string{}

			issued = &secret
			return k, nil
		})
		return err
	})
	if err != nil {
		return identity.Key{}, err
	}
	out.Credential = issued
	return out, nil
}

func (s *Store) SetGrants(ctx context.Context, actor, w, member, expected int64, projects []int64, actions []string) (identity.GrantVersion, error) {
	var out identity.GrantVersion
	acts, err := normalized(actions, "/actions")
	if err != nil {
		return out, err
	}
	if len(projects) > 256 {
		return out, fault.Invalid("/project_ids")
	}
	projects = slices.Clone(projects)
	slices.Sort(projects)
	projects = slices.Compact(projects)
	err = s.write(ctx, actor, w, 0, "members.grants.write", func(tx *sql.Tx, a identity.Access) error {
		var current int64
		if err := tx.QueryRowContext(ctx, `SELECT grant_version FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE`, w, member).Scan(&current); err != nil {
			return found(err)
		}
		if current != expected {
			return fault.Stale(current)
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE workspace_id=$1 AND id=ANY($2) AND archived_at IS NULL`, w, pq.Array(projects)).Scan(&count); err != nil {
			return err
		}
		if count != len(projects) {
			return fault.NotFound
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_actions WHERE action=ANY($1) AND category IN ('member','project','key')`, pq.Array(acts)).Scan(&count); err != nil {
			return err
		}
		if count != len(acts) {
			return fault.Invalid("/actions")
		}
		for _, act := range acts {
			if err := a.Require(act); err != nil {
				return err
			}
		}
		var rectangular bool
		if err := tx.QueryRowContext(ctx, `SELECT count(*) = count(DISTINCT project_id) * count(DISTINCT action) FROM project_grants WHERE workspace_id=$1 AND user_id=$2`, w, member).Scan(&rectangular); err != nil {
			return err
		}
		if !rectangular {
			return &fault.Fault{Status: 409, Reason: "GRANT_REPRESENTATION_LOSS", Message: "Use the project grant endpoint to preserve per-project permissions."}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM project_grants WHERE workspace_id=$1 AND user_id=$2`, w, member); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_grants(workspace_id,user_id,project_id,action) SELECT $1,$2,p,a FROM unnest($3::bigint[]) p CROSS JOIN unnest($4::text[]) a`, w, member, pq.Array(projects), pq.Array(acts)); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `UPDATE workspace_members SET grant_version=grant_version+1,updated_at=now() WHERE workspace_id=$1 AND user_id=$2 RETURNING user_id,grant_version,updated_at`, w, member).Scan(&out.MemberID, &out.Version, &out.EffectiveAt)
	})
	out.EffectiveAt = out.EffectiveAt.UTC()
	return out, err
}

// SetProjectGrants replaces exactly one project's direct grants, under the member CAS.
func (s *Store) SetProjectGrants(ctx context.Context, actor, w, member, p, expected int64, actions []string) (identity.GrantVersion, error) {
	var out identity.GrantVersion
	acts, err := normalized(actions, "/actions")
	if err != nil {
		return out, err
	}
	err = s.write(ctx, actor, w, 0, "members.grants.write", func(tx *sql.Tx, a identity.Access) error {
		var current int64
		if err := tx.QueryRowContext(ctx, `SELECT grant_version FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE`, w, member).Scan(&current); err != nil {
			return found(err)
		}
		if current != expected {
			return fault.Stale(current)
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE workspace_id=$1 AND id=$2 AND archived_at IS NULL)`, w, p).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fault.NotFound
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_actions WHERE action=ANY($1) AND category IN ('member','project','key')`, pq.Array(acts)).Scan(&count); err != nil {
			return err
		}
		if count != len(acts) {
			return fault.Invalid("/actions")
		}
		for _, action := range acts {
			if err := a.Require(action); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM project_grants WHERE workspace_id=$1 AND user_id=$2 AND project_id=$3`, w, member, p); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_grants(workspace_id,user_id,project_id,action) SELECT $1,$2,$3,unnest($4::text[])`, w, member, p, pq.Array(acts)); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `UPDATE workspace_members SET grant_version=grant_version+1,updated_at=now() WHERE workspace_id=$1 AND user_id=$2 RETURNING user_id,grant_version,updated_at`, w, member).Scan(&out.MemberID, &out.Version, &out.EffectiveAt)
	})
	out.EffectiveAt = out.EffectiveAt.UTC()
	return out, err
}
