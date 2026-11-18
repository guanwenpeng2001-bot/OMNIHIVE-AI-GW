package identity

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
)

type Project struct {
	ID          int64  `json:"id,string"`
	WorkspaceID int64  `json:"workspace_id,string"`
	Name        string `json:"name"`
	Version     int64  `json:"version,string"`
}

type Access struct {
	ActorID     int64     `json:"actor_id,string"`
	WorkspaceID int64     `json:"workspace_id,string"`
	ProjectID   int64     `json:"project_id,string"`
	Actions     []string  `json:"actions"`
	Version     int64     `json:"scope_version,string"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (a Access) Require(action string) error {
	if action != "" && !slices.Contains(a.Actions, action) {
		return fault.Forbidden
	}
	return nil
}

type Key struct {
	ID         int64   `json:"id,string"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	Credential *string `json:"credential"`
	OwnerID    int64   `json:"owner_id,string"`
	Version    int64   `json:"version,string"`
}

type ModelAccess struct {
	Mode string   `json:"mode"`
	IDs  []string `json:"ids"`
}

type CreateKey struct {
	Name        string      `json:"name"`
	ModelAccess ModelAccess `json:"model_access"`
	Actions     []string    `json:"actions"`
	ExpiresAt   *time.Time  `json:"expires_at"`
}

type KeyPolicy struct {
	AllowedGroups    []int64
	GroupID          int64
	GroupRPMOverride *int
	Scope            scope.Authorized
	Actions, Models  []string
	Mode             string
}

func (p *KeyPolicy) Allows(action string, models []string) bool {
	if !slices.Contains(p.Actions, action) {
		return false
	}
	if action == "usage.read" || action == "tasks.read" {
		return true
	}
	switch p.Mode {
	case "inherit":
		return true
	case "allow_list":
		if len(models) == 0 {
			return false
		}
		for _, m := range models {
			if !slices.Contains(p.Models, m) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

const ProjectKeyPrefix = "oh_p_"

type AccessEvaluator interface {
	Evaluate(ctx context.Context, actor, workspace, project int64, action string) (Access, error)
}

type ScopeResolver interface {
	RequireResource(ctx context.Context, actor, workspace, project int64, action, kind string, id int64) error
}

type GatewayScopes interface {
	ProjectCredential(ctx context.Context, digest string) (*KeyPolicy, error)
	LegacyScope(ctx context.Context, workspace, key, owner int64, digest string) (scope.Authorized, error)
}

type Operation struct {
	ID                 string  `json:"operation_id"`
	State              string  `json:"state"`
	StatusURL          string  `json:"status_url"`
	ResourceID         *string `json:"resource_id"`
	Result             any     `json:"result"`
	Error              any     `json:"error"`
	ProjectID, OwnerID int64   `json:"-"`
	DelegatedAction    string  `json:"-"`
}

// Readers constrain lookups to the workspace and project their result for the actor.
type OperationReader interface {
	ReadOperation(context.Context, int64, int64, string) (Operation, error)
}

type Operations struct{ Readers []OperationReader }

func (r Operations) Lookup(ctx context.Context, actor, w int64, id string) (Operation, error) {
	for _, reader := range r.Readers {
		op, err := reader.ReadOperation(ctx, w, actor, id)
		if errors.Is(err, fault.NotFound) {
			continue
		}
		return op, err
	}
	return Operation{}, fault.NotFound
}

type Page struct {
	After int64
	AsOf  time.Time
	Limit int
}

type Workspace struct {
	ID   int64  `json:"workspace_id,string"`
	Name string `json:"name"`
}

type GrantVersion struct {
	MemberID    int64     `json:"member_id,string"`
	Version     int64     `json:"version,string"`
	EffectiveAt time.Time `json:"effective_at"`
}
