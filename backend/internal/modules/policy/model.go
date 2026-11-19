package policy

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
)

type Scope struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Project string `json:"project_id,omitempty"` // Required for an API key.
}

func ID(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != s {
		return 0
	}
	return n
}

func (s Scope) ProjectID() int64 {
	if s.Type == "project" {
		return ID(s.ID)
	}
	return ID(s.Project)
}

func (s Scope) Valid(workspace int64) bool {
	switch s.Type {
	case "workspace":
		return workspace > 0 && ID(s.ID) == workspace && s.Project == ""
	case "project":
		return ID(s.ID) > 0 && s.Project == ""
	case "api_key":
		return ID(s.ID) > 0 && ID(s.Project) > 0
	case "platform":
		return workspace == 0 && s.ID == "platform" && s.Project == ""
	case "request":
		return s.ID == "request" && s.Project == ""
	}
	return false
}

type List struct {
	Mode   string   `json:"mode"`
	Values []string `json:"values,omitempty"`
}

func validText(s string, max int) bool {
	return s != "" && strings.TrimSpace(s) == s && len(s) <= max && strings.IndexFunc(s, unicode.IsControl) < 0
}
func validIDs(v []string) bool {
	if len(v) > 256 {
		return false
	}
	seen := make(map[string]bool, len(v))
	for _, x := range v {
		if !validText(x, 200) || seen[x] {
			return false
		}
		seen[x] = true
	}
	return true
}
func (l *List) valid() bool {
	if l == nil {
		return true
	}
	switch l.Mode {
	case "inherit", "deny_all":
		return len(l.Values) == 0
	case "allow_list":
		return len(l.Values) > 0 && validIDs(l.Values)
	}
	return false
}

type Constraints struct {
	Models            *List       `json:"models,omitempty"`
	Regions           *List       `json:"regions,omitempty"`
	Retention         string      `json:"retention,omitempty"`
	Training          string      `json:"training,omitempty"`
	MaxInputRate      *money.Rate `json:"max_input_rate,omitempty"`
	Required          []string    `json:"required_capabilities,omitempty"`
	RequireKnownQuota bool        `json:"require_known_quota,omitempty"`
}
type Candidates struct {
	Pools           []string `json:"pool_ids,omitempty"`
	DeniedProviders []string `json:"denied_provider_ids,omitempty"`
	Expansion       *bool    `json:"allow_expansion,omitempty"`
}
type Preferences struct {
	Template  string    `json:"template,omitempty"`
	Providers *[]string `json:"ordered_provider_ids,omitempty"`
}
type Fallback struct {
	Endpoint *bool     `json:"endpoint,omitempty"`
	Models   *[]string `json:"model_ids,omitempty"`
	Platform *bool     `json:"byok_to_platform,omitempty"`
}
type Definition struct {
	Name        string      `json:"name"`
	Scope       Scope       `json:"scope"`
	Constraints Constraints `json:"constraints"`
	Candidates  Candidates  `json:"candidates"`
	Preferences Preferences `json:"preferences"`
	Fallback    Fallback    `json:"fallback"`
}

func (d Definition) Validate(workspace int64) error {
	c := d.Constraints
	switch {
	case !validText(d.Name, 120):
		return fault.Invalid("/name")
	case !d.Scope.Valid(workspace):
		return fault.Invalid("/scope")
	case !c.Models.valid():
		return fault.Invalid("/constraints/models")
	case !c.Regions.valid():
		return fault.Invalid("/constraints/regions")
	case !slices.Contains([]string{"", "inherit", "strict_zero"}, c.Retention):
		return fault.Invalid("/constraints/retention")
	case !slices.Contains([]string{"", "inherit", "prohibited"}, c.Training):
		return fault.Invalid("/constraints/training")
	case c.MaxInputRate != nil && (c.MaxInputRate.Currency() != "USD" || c.MaxInputRate.Unit() != "input_token" || c.MaxInputRate.Per().String() != "1000000"):
		return fault.Invalid("/constraints/max_input_rate")
	case !validIDs(c.Required):
		return fault.Invalid("/constraints/required_capabilities")
	case !validIDs(d.Candidates.Pools):
		return fault.Invalid("/candidates/pool_ids")
	case !validIDs(d.Candidates.DeniedProviders):
		return fault.Invalid("/candidates/denied_provider_ids")
	case !slices.Contains([]string{"", "balanced", "stable", "low_latency", "lowest_cost"}, d.Preferences.Template):
		return fault.Invalid("/preferences/template")
	case d.Preferences.Providers != nil && !validIDs(*d.Preferences.Providers):
		return fault.Invalid("/preferences/ordered_provider_ids")
	case d.Fallback.Models != nil && !validIDs(*d.Fallback.Models):
		return fault.Invalid("/fallback/model_ids")
	}
	return nil
}

type Layer struct {
	Version    string     `json:"version"`
	Definition Definition `json:"definition"`
}

type Head struct {
	ID         string     `json:"id"`
	Active     string     `json:"active_version"`
	Draft      string     `json:"draft_version"`
	Base       string     `json:"base_version"`
	ETag       int64      `json:"etag,string"`
	Definition Definition `json:"definition"`
}
type Version struct {
	ID          string    `json:"id"`
	Base        string    `json:"base_version"`
	Actor       int64     `json:"actor_id,string"`
	PublishedAt time.Time `json:"published_at"`
	Note        string    `json:"change_note"`
}

type Publish struct {
	Draft      string `json:"draft_version"`
	Base       string `json:"base_version"`
	Simulation string `json:"simulation_id"`
	Note       string `json:"change_note"`
}
type Rollback struct {
	From    string `json:"from_version"`
	Restore string `json:"restore_version"`
	Note    string `json:"change_note"`
}
type Impact struct {
	Projects int64 `json:"projects,string"`
	Keys     int64 `json:"keys,string"`
}
type Publication struct {
	ID          string    `json:"id"`
	Active      string    `json:"active_version"`
	Previous    string    `json:"previous_version"`
	PublishedAt time.Time `json:"published_at"`
	Epoch       int64     `json:"revocation_epoch,string"`
	Impact      Impact    `json:"impact"`
}

var (
	Conflict        = &fault.Fault{Status: 409, Reason: "POLICY_CONFLICT", Message: "The published policy or draft has changed."}
	StaleSimulation = &fault.Fault{Status: 409, Reason: "SIMULATION_STALE", Message: "Run a new simulation against the current draft and snapshot."}
	Expired         = &fault.Fault{Status: 410, Reason: "SNAPSHOT_EXPIRED", Message: "Refresh the authorized routing snapshot."}
)

func scopeKey(w int64, s Scope) string { return fmt.Sprintf("%d/%s/%s", w, s.Type, s.ID) }
