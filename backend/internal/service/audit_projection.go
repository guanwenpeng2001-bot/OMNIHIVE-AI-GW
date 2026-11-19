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
	return &AuditLog{ID: v.ID, CreatedAt: v.CreatedAt, ActorUserID: v.ActorUserID, Method: v.Method,
		Path: "/api/v2/", Action: action, StatusCode: v.StatusCode, LatencyMs: v.LatencyMs}
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
