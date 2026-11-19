package byok

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
	classes "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials/byok"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/lib/pq"
)

type Patch struct {
	Projects        *[]string `json:"allowed_project_ids,omitempty"`
	Members         *[]string `json:"allowed_member_ids,omitempty"`
	ServiceAccounts *[]string `json:"allowed_service_account_ids,omitempty"`
	Keys            *[]string `json:"allowed_key_ids,omitempty"`
	Models          *[]string `json:"allowed_model_ids,omitempty"`
	Priority        *int      `json:"priority,omitempty"`
	Fallback        *bool     `json:"allow_platform_fallback,omitempty"`
	Budget          *bool     `json:"include_external_in_budget,omitempty"`
}

func (v Patch) apply(c Credential) (Policy, error) {
	p := Policy{Projects: c.Scope.Projects, Members: c.Scope.Members, ServiceAccounts: c.Scope.ServiceAccounts, Keys: c.Scope.Keys, Models: c.Scope.Models, Priority: c.Priority, Fallback: c.Fallback, Budget: c.Budget}
	if v == (Patch{}) {
		return p, fault.Invalid("/patch")
	}
	if v.Projects != nil {
		p.Projects = *v.Projects
	}
	if v.Members != nil {
		p.Members = *v.Members
	}
	if v.ServiceAccounts != nil {
		p.ServiceAccounts = *v.ServiceAccounts
	}
	if v.Keys != nil {
		p.Keys = *v.Keys
	}
	if v.Models != nil {
		p.Models = *v.Models
	}
	if v.Priority != nil {
		p.Priority = *v.Priority
	}
	if v.Fallback != nil {
		p.Fallback = *v.Fallback
	}
	if v.Budget != nil {
		p.Budget = *v.Budget
	}
	return p, nil
}

func (s *Store) Get(ctx context.Context, t Target) (Credential, error) {
	if _, err := s.access.Evaluate(ctx, t.Actor, t.Workspace, t.Project, Manage); err != nil {
		return Credential{}, err
	}
	return scanCredential(s.db.QueryRowContext(ctx, `SELECT `+metadata+` FROM byok_credentials c WHERE `+visible+` AND c.id=$3`, t.Workspace, t.Project, t.ID))
}

func saveEvent(ctx context.Context, tx *sql.Tx, cmd Command, result any) error {
	var c Credential
	event := cmd.Action
	switch v := result.(type) {
	case Credential:
		c = v
	case identity.Operation:
		var ok bool
		c, ok = v.Result.(Credential)
		if !ok {
			return errors.New("missing BYOK lifecycle result")
		}
		if v.State == "failed" {
			event += ".failed"
		}
	default:
		return errors.New("missing BYOK lifecycle result")
	}
	if event == "revoke" {
		event += "." + cmd.Mode
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO byok_lifecycle_events(workspace_id,credential_id,project_ids,actor_id,version,event) VALUES($1,$2,$3,$4,$5,$6)`, c.Workspace, c.ID, pq.Array(c.Scope.Projects), cmd.Actor, c.Version, "credential."+event)
	return err
}

type EventPage struct {
	Items []connections.Event `json:"items"`
	Next  *string             `json:"next_cursor"`
	AsOf  time.Time           `json:"as_of"`
}

func (s *Store) Events(ctx context.Context, t Target, cursor string, limit int) (EventPage, error) {
	if limit < 1 || limit > 100 {
		return EventPage{}, fault.Invalid("/limit")
	}
	if _, err := s.Get(ctx, t); err != nil {
		return EventPage{}, err
	}
	page, err := s.eventPage(t, cursor)
	if err != nil {
		return EventPage{}, err
	}
	out := EventPage{Items: []connections.Event{}, AsOf: page.AsOf}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.event,e.version,e.at,e.actor_id::text FROM byok_credentials c JOIN byok_lifecycle_events e ON (e.workspace_id,e.credential_id)=(c.workspace_id,c.id) WHERE `+visible+` AND c.id=$3 AND ($2=0 OR e.project_ids=ARRAY[$2]) AND e.id>$4 AND e.at<=$5 ORDER BY e.id LIMIT $6`, t.Workspace, t.Project, t.ID, page.After, out.AsOf, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e connections.Event
		if err = rows.Scan(&e.ID, &e.Type, &e.Version, &e.At, &e.Actor); err != nil {
			return out, err
		}
		out.Items = append(out.Items, e)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		page.After = out.Items[limit-1].ID
		v, err := s.eventCursor(t, page)
		if err != nil {
			return out, err
		}
		out.Next = &v
	}
	return out, rows.Err()
}

func (s *Store) Classes(ctx context.Context, t Target) ([]classes.Class, error) {
	if _, err := s.access.Evaluate(ctx, t.Actor, t.Workspace, t.Project, Manage); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT provider,'api_key',enabled FROM byok_pilot_providers p WHERE workspace_id=$1 AND NOT EXISTS(SELECT FROM byok_provider_classes c WHERE c.workspace_id=p.workspace_id AND c.provider=p.provider AND c.auth_type='api_key')
 UNION ALL SELECT provider,auth_type,enabled FROM byok_provider_classes WHERE workspace_id=$1`, t.Workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	enrolled := map[[2]string]bool{}
	for rows.Next() {
		var key [2]string
		var yes bool
		if err = rows.Scan(&key[0], &key[1], &yes); err != nil {
			return nil, err
		}
		enrolled[key] = yes
	}
	items := classes.Classes()
	for i, c := range items {
		items[i].Enabled = c.Supported && enrolled[[2]string{c.Provider, c.Auth}]
	}
	return items, rows.Err()
}
