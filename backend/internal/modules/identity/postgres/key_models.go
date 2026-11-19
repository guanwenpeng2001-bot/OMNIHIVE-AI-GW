package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

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
