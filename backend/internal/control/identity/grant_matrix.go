package identity

import (
	"bytes"
	"encoding/json"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

func (h *Handler) replaceGrants(r request, member, version int64) (domain.GrantVersion, error) {
	var empty domain.GrantVersion
	var b struct {
		Exact    json.RawMessage `json:"project_grants"`
		Projects json.RawMessage `json:"project_ids"`
		Actions  json.RawMessage `json:"actions"`
	}
	if err := r.body(&b); err != nil {
		return empty, fault.Invalid("/project_grants")
	}
	ctx := r.Request.Context()
	if b.Exact != nil {
		if b.Projects != nil || b.Actions != nil {
			return empty, fault.Invalid("/project_grants")
		}
		var input []struct {
			Project string   `json:"project_id"`
			Actions []string `json:"actions"`
		}
		dec := json.NewDecoder(bytes.NewReader(b.Exact))
		dec.DisallowUnknownFields()
		if dec.Decode(&input) != nil || input == nil {
			return empty, fault.Invalid("/project_grants")
		}
		rows := make([]domain.ProjectGrant, 0, len(input))
		for _, row := range input {
			id, err := identifier(row.Project)
			if err != nil || row.Actions == nil {
				return empty, fault.Invalid("/project_grants")
			}
			rows = append(rows, domain.ProjectGrant{ProjectID: id, Actions: row.Actions})
		}
		return h.store.SetExactGrants(ctx, r.actor, r.w, member, version, rows)
	}
	var ids, actions []string
	if json.Unmarshal(b.Projects, &ids) != nil || ids == nil {
		return empty, fault.Invalid("/project_ids")
	}
	if json.Unmarshal(b.Actions, &actions) != nil || actions == nil {
		return empty, fault.Invalid("/actions")
	}
	projects := make([]int64, len(ids))
	for i, s := range ids {
		id, err := identifier(s)
		if err != nil {
			return empty, fault.Invalid("/project_ids")
		}
		projects[i] = id
	}
	return h.store.SetGrants(ctx, r.actor, r.w, member, version, projects, actions)
}
