package postgres

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

func (s *Store) keyGroup(ctx context.Context, tx *sql.Tx, a identity.Access, in identity.CreateKey) (*int64, error) {
	if in.GroupID == nil {
		return s.defaultKeyGroup(ctx, a, in.ModelAccess)
	}
	if *in.GroupID <= 0 {
		return nil, fault.Invalid("/group_id")
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT g.id FROM groups g
 JOIN users u ON u.id=$1 AND u.status='active' AND u.deleted_at IS NULL
 JOIN projects d ON d.workspace_id=g.workspace_id AND d.is_default
 LEFT JOIN project_resources r ON r.workspace_id=g.workspace_id AND r.kind='groups' AND r.resource_id=g.id
 WHERE g.id=$4 AND g.workspace_id=$2 AND coalesce(r.project_id,d.id)=$3
 AND g.status='active' AND g.deleted_at IS NULL
 AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q WHERE q.kind='groups' AND q.resource_id=g.id)
 AND CASE WHEN g.subscription_type='subscription' THEN EXISTS (
     SELECT 1 FROM user_subscriptions s WHERE s.user_id=u.id AND s.group_id=g.id AND s.status='active'
       AND s.deleted_at IS NULL AND s.expires_at>now())
 ELSE (NOT g.is_exclusive AND NOT u.restrict_public_groups) OR EXISTS (
     SELECT 1 FROM user_allowed_groups ug WHERE ug.user_id=u.id AND ug.group_id=g.id) END`,
		a.ActorID, a.WorkspaceID, a.ProjectID, *in.GroupID).Scan(&id)
	if err != nil {
		return nil, found(err)
	}
	return &id, nil
}

// defaultKeyGroup binds a project key only when all catalog selections
// identify a single common, authorized billing group. Older wire-name
// selections can narrow that set but never trigger automatic binding.
func (s *Store) defaultKeyGroup(ctx context.Context, a identity.Access, access identity.ModelAccess) (*int64, error) {
	if s.catalog == nil || access.Mode != "allow_list" || !slices.ContainsFunc(access.IDs, catalogModelID) {
		return nil, nil
	}
	snap, err := s.catalog.Snapshot(ctx, catalog.Perspective{Actor: a.ActorID, Workspace: a.WorkspaceID, Project: a.ProjectID, AccessVersion: a.Version}, time.Now().UTC())
	if err != nil {
		if errors.Is(err, fault.NotFound) {
			return nil, nil // The catalog can be disabled independently of key issuance.
		}
		return nil, err
	}
	groupsByModel := make(map[string][]int64)
	for _, d := range snap.Details {
		groupsByModel[d.Model.ID] = d.EligibleGroups
		if !catalogModelID(d.Model.Model) {
			groupsByModel[d.Model.Model] = append(groupsByModel[d.Model.Model], d.EligibleGroups...)
		}
	}
	var common []int64
	for index, id := range access.IDs {
		groups := groupsByModel[id]
		if len(groups) == 0 {
			return nil, nil
		}
		if index == 0 {
			common = slices.Clone(groups)
			slices.Sort(common)
			common = slices.Compact(common)
		} else {
			common = slices.DeleteFunc(common, func(group int64) bool { return !slices.Contains(groups, group) })
		}
	}
	if len(common) == 1 {
		return &common[0], nil
	}
	return nil, nil
}

func catalogModelID(id string) bool {
	if len(id) != 34 || !strings.HasPrefix(id, "m_") {
		return false
	}
	_, err := hex.DecodeString(id[2:])
	return err == nil
}
