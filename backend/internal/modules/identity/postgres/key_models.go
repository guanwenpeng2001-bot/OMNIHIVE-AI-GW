package postgres

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/lib/pq"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

var keyCatalogUnavailable = &fault.Fault{Status: 503, Reason: "CATALOG_UNAVAILABLE", Message: "The scoped model catalog is unavailable."}

const eligibleKeyGroups = ` FROM groups g
 JOIN users u ON u.id=$1 AND u.status='active' AND u.deleted_at IS NULL
 JOIN projects d ON d.workspace_id=g.workspace_id AND d.is_default
 LEFT JOIN project_resources r ON r.kind='groups' AND r.resource_id=g.id
 WHERE g.workspace_id=$2 AND coalesce(r.project_id,d.id)=$3
 AND (r.resource_id IS NULL OR r.workspace_id=g.workspace_id)
 AND g.status='active' AND g.deleted_at IS NULL
 AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q WHERE q.kind='groups' AND q.resource_id=g.id)
 AND ` + identity.KeyGroupEntitlement

func (s *Store) keyGroup(ctx context.Context, tx *sql.Tx, a identity.Access, in identity.CreateKey) (*int64, error) {
	if in.GroupID != nil && *in.GroupID <= 0 {
		return nil, fault.Invalid("/group_id")
	}
	var candidates []int64
	if in.GroupID == nil {
		var err error
		candidates, err = s.catalogKeyGroups(ctx, a, in.ModelAccess)
		if err != nil {
			return nil, err
		}
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT g.id`+eligibleKeyGroups+` AND ($4::bigint IS NULL OR g.id=$4) AND ($5::bigint[] IS NULL OR g.id=ANY($5))
 ORDER BY (SELECT count(*) FROM account_groups ag JOIN accounts acc ON acc.id=ag.account_id
   WHERE ag.group_id=g.id AND acc.workspace_id=g.workspace_id AND acc.deleted_at IS NULL
   AND acc.status='active' AND acc.schedulable) DESC,
   (NOT g.is_exclusive AND g.subscription_type <> 'subscription') DESC, g.id LIMIT 1 FOR SHARE OF g`,
		a.ActorID, a.WorkspaceID, a.ProjectID, in.GroupID, pq.Array(candidates)).Scan(&id)
	if in.GroupID == nil && errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, found(err)
	}
	if in.GroupID != nil {
		if err := s.checkExplicitGroupModels(ctx, a, id, in.ModelAccess); err != nil {
			return nil, err
		}
	}
	return &id, nil
}

func (s *Store) checkExplicitGroupModels(ctx context.Context, a identity.Access, group int64, access identity.ModelAccess) error {
	if access.Mode != "allow_list" || !slices.ContainsFunc(access.IDs, identity.IsStableModelID) {
		return nil
	}
	if s.catalog == nil {
		return keyCatalogUnavailable
	}
	snap, err := s.catalog.Snapshot(ctx, catalog.Perspective{Actor: a.ActorID, Workspace: a.WorkspaceID, Project: a.ProjectID, AccessVersion: a.Version}, time.Now().UTC())
	if err != nil {
		if errors.Is(err, fault.NotFound) {
			return keyCatalogUnavailable
		}
		return err
	}
	for _, id := range access.IDs {
		var matches int
		for _, d := range snap.Details {
			if !slices.Contains(d.EligibleGroups, group) {
				continue
			}
			if identity.IsStableModelID(id) {
				if d.Model.ID == id {
					matches++
				}
			} else if d.Model.Model == id || slices.Contains(d.Aliases, id) {
				matches++
			}
		}
		if matches != 1 {
			return fault.Invalid("/model_access/ids")
		}
	}
	return nil
}

// A nil candidate list leaves selection to the scoped permission query. An
// empty list means the selected catalog models have no shared eligible group.
func (s *Store) catalogKeyGroups(ctx context.Context, a identity.Access, access identity.ModelAccess) ([]int64, error) {
	if s.catalog == nil || access.Mode != "allow_list" || !slices.ContainsFunc(access.IDs, identity.IsStableModelID) {
		return nil, nil
	}
	snap, err := s.catalog.Snapshot(ctx, catalog.Perspective{Actor: a.ActorID, Workspace: a.WorkspaceID, Project: a.ProjectID, AccessVersion: a.Version}, time.Now().UTC())
	if err != nil {
		if errors.Is(err, fault.NotFound) {
			return nil, nil // The catalog can be disabled independently of key issuance.
		}
		return nil, err
	}
	return commonModelGroups(snap, access.IDs), nil
}

func commonModelGroups(snap catalog.Snapshot, ids []string) []int64 {
	groupsByModel := make(map[string][]int64)
	for _, d := range snap.Details {
		groupsByModel[d.Model.ID] = d.EligibleGroups
		if !identity.IsStableModelID(d.Model.Model) {
			groupsByModel[d.Model.Model] = append(groupsByModel[d.Model.Model], d.EligibleGroups...)
		}
	}
	var common []int64
	for index, id := range ids {
		groups := groupsByModel[id]
		if len(groups) == 0 {
			return []int64{}
		}
		if index == 0 {
			common = slices.Clone(groups)
			slices.Sort(common)
			common = slices.Compact(common)
		} else {
			common = slices.DeleteFunc(common, func(group int64) bool { return !slices.Contains(groups, group) })
		}
	}
	return common
}

func (s *Store) KeyGroups(ctx context.Context, actor, w, p int64, page identity.Page) ([]identity.KeyGroup, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := evaluate(ctx, tx, actor, w, p, "keys.create"); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT g.id,g.name,g.platform`+eligibleKeyGroups+` AND g.id>$4 AND g.created_at<=$5 ORDER BY g.id LIMIT $6`, actor, w, p, page.After, page.AsOf, page.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []identity.KeyGroup{}
	for rows.Next() {
		var g identity.KeyGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Platform); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, tx.Commit()
}
