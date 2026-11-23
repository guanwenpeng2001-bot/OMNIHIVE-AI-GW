package identity

import "time"

type RequestAccess struct {
	RequestedProjectID *int64 `json:"requested_project_id,string,omitempty"`
	Note               string `json:"note"`
}
type DecideAccess struct {
	Decision      string   `json:"decision"`
	ProjectID     int64    `json:"project_id,string,omitempty"`
	AddActions    []string `json:"add_actions,omitempty"`
	MemberVersion int64    `json:"member_version,string,omitempty"`
}
type AccessRequest struct {
	ID                 int64      `json:"id,string"`
	WorkspaceID        int64      `json:"workspace_id,string"`
	RequesterID        int64      `json:"requester_id,string"`
	RequestedProjectID *int64     `json:"requested_project_id,string"`
	Note               string     `json:"note"`
	State              string     `json:"state"`
	Version            int64      `json:"version,string"`
	CreatedAt          time.Time  `json:"created_at"`
	ApprovedProjectID  *int64     `json:"approved_project_id,string"`
	DecidedAt          *time.Time `json:"decided_at"`
}
