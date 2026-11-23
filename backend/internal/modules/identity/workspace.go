package identity

import "time"

type WorkspaceMembership struct {
	ID             int64
	Name           string
	IsDefault      bool
	CreatedAt      time.Time
	Role           *string
	RoleSource     *string
	EffectiveAdmin bool
}

type WorkspaceAccess struct {
	ActorID          int64     `json:"actor_id,string"`
	WorkspaceID      int64     `json:"workspace_id,string"`
	ProjectID        *int64    `json:"project_id,string"`
	Role             *string   `json:"membership_role,omitempty"`
	RoleSource       *string   `json:"role_source,omitempty"`
	EffectiveAdmin   bool      `json:"effective_admin"`
	Actions          []string  `json:"actions"`
	GrantableActions *[]string `json:"grantable_actions,omitempty"`
	Version          int64     `json:"scope_version,string"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type ProjectAccess struct {
	Access
	IssuableKeyActions []string `json:"issuable_key_actions"`
}
