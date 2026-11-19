// Package byok owns the scoped ordinary-API-key pilot. Shared capacity cannot
// acquire secrets from the pilot vault through the native account inventory.
package byok

import (
	"slices"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
)

const Manage = "credentials.byok.manage"

type Scope struct {
	Projects []string `json:"project_ids"`
	Members  []string `json:"member_ids"`
	Keys     []string `json:"key_ids"`
	Models   []string `json:"model_ids"`
}

type Policy struct {
	Projects []string `json:"allowed_project_ids"`
	Members  []string `json:"allowed_member_ids"`
	Keys     []string `json:"allowed_key_ids"`
	Models   []string `json:"allowed_model_ids"`
	Priority int      `json:"priority"`
	Fallback bool     `json:"allow_platform_fallback"`
	Budget   bool     `json:"include_external_in_budget"`
}

type CreateInput struct {
	Provider string `json:"provider"`
	Auth     string `json:"auth_type"`
	Secret   string `json:"secret"`
	Policy
}

type Credential struct {
	ID            int64      `json:"id,string"`
	Workspace     int64      `json:"owner_workspace_id,string"`
	Provider      string     `json:"provider"`
	Version       int64      `json:"version,string"`
	SecretVersion int64      `json:"credential_version,string"`
	State         string     `json:"state"`
	Fingerprint   string     `json:"fingerprint"`
	VerifiedUntil *time.Time `json:"verified_until"`
	Scope         Scope      `json:"scope"`
	Priority      int        `json:"priority"`
	Fallback      bool       `json:"allow_platform_fallback"`
	Budget        bool       `json:"include_external_in_budget"`
}

func decimalID(id int64) string { return strconv.FormatInt(id, 10) }

func validList(values []string, ids bool) bool {
	if len(values) == 0 || len(values) > 100 {
		return false
	}
	for i, v := range values {
		if !connections.ValidText(v, 128) || slices.Contains(values[:i], v) {
			return false
		}
		if ids {
			if _, err := connections.ID(v); err != nil {
				return false
			}
		}
	}
	return true
}

func (p Policy) validate(project int64) error {
	if !validList(p.Projects, true) || !validList(p.Members, true) || !validList(p.Keys, true) || !validList(p.Models, false) || p.Priority < 0 || p.Priority > 1000 {
		return fault.Invalid("/policy")
	}
	if project != 0 && !slices.Equal(p.Projects, []string{decimalID(project)}) {
		return fault.Forbidden
	}
	return nil
}

var (
	unavailable = &fault.Fault{Status: 409, Reason: "BYOK_UNAVAILABLE", Message: "The credential is unavailable or verification has expired."}
	unverified  = &fault.Fault{Status: 422, Reason: "BYOK_VERIFICATION_FAILED", Message: "The provider could not verify this API key and its models."}
	unfunded    = &fault.Fault{Status: 409, Reason: "FUNDING_NOT_AUTHORIZED", Message: "A sufficient live reservation and frozen price are required."}
)
