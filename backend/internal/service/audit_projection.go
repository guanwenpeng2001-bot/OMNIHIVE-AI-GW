package service

import "strings"

// V2 paths can carry artifact tickets, provider locators and opaque client IDs.
// Only the session identity and bounded execution metadata cross this boundary,
// including when serving records captured by an older binary.
func scopedAuditProjection(v *AuditLog) *AuditLog {
	if v == nil || !strings.HasPrefix(v.Path, "/api/v2/") {
		return v
	}
	action := "scope.write"
	if v.Method == "GET" || v.Method == "HEAD" {
		action = "scope.read"
	}
	// Playground and replay routes publish their own server-derived action and a
	// fixed private-body placeholder. Keep those bounded metadata fields while
	// still stripping user agents and credential masks.
	private := strings.HasPrefix(v.Action, "playground.") || strings.HasPrefix(v.Action, "requests.")
	if private {
		action = v.Action
	}
	p := &AuditLog{ID: v.ID, CreatedAt: v.CreatedAt, ActorUserID: v.ActorUserID, Method: v.Method,
		Path: "/api/v2/", Action: action, StatusCode: v.StatusCode, LatencyMs: v.LatencyMs}
	if private {
		p.RequestBody = v.RequestBody
		p.Extra = v.Extra
	}
	return p
}

// Management readers receive the historical action and its disposition. Raw
// bodies, arbitrary extra fields, credential fragments and user agents may have
// been collected under a previous redaction policy.
func auditManagementView(v *AuditLog) *AuditLog {
	if v == nil {
		return nil
	}
	p := *scopedAuditProjection(v)
	p.RequestBody = ""
	p.Extra = nil
	p.CredentialMasked = ""
	p.UserAgent = ""
	return &p
}
