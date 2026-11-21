package service

import "strings"

// V2 paths can carry artifact tickets, provider locators and opaque client IDs.
// Only the session identity and bounded execution metadata cross this boundary,
// including when serving records captured by an older binary.
func scopedAuditProjection(v *AuditLog) *AuditLog {
	if v == nil || !strings.HasPrefix(v.Path, "/api/v2/") {
		return v
	}
	p := *v
	p.CredentialMasked = ""
	p.UserAgent = ""
	if strings.HasPrefix(v.Action, "playground.") || strings.HasPrefix(v.Action, "requests.") {
		p.Extra = auditSafeExtra(v.Extra)
	}
	return &p
}

// Management readers receive the historical action and its disposition. Raw
// bodies, arbitrary extra fields, credential fragments and user agents may have
// been collected under a previous redaction policy.
func auditManagementView(v *AuditLog) *AuditLog {
	if v == nil {
		return nil
	}
	p := *scopedAuditProjection(v)
	p.CredentialMasked = ""
	p.UserAgent = ""
	return &p
}

func auditSafeExtra(extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(extra))
	for key, value := range extra {
		switch key {
		case "submitted_diff", "diff", "reason", "params", "workspace_id", "project_id", "result", "error_code", "outcome":
			if key == "params" {
				if params, ok := value.(map[string]string); ok {
					converted := make(map[string]any, len(params))
					for name, param := range params {
						converted[name] = param
					}
					value = converted
				}
			}
			out[key] = value
		}
	}
	return out
}
