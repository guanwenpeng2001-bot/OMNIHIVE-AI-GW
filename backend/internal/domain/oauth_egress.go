package domain

// OAuthEgressSelection is private server session data. It travels with the
// credential draft, never with browser-supplied credential options or responses.
type OAuthEgressSelection struct {
	GroupID     int64  `json:"group_id"`
	WorkspaceID int64  `json:"workspace_id"`
	ProxyID     int64  `json:"proxy_id"`
	URL         string `json:"url"`
	Source      string `json:"source"`
}
