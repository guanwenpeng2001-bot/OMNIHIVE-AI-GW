package connections

import (
	"bytes"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

const SchemaVersion = "1"

type Field struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	Required  bool   `json:"required"`
	Component string `json:"component"`
}
type Schema struct {
	Provider  string            `json:"provider"`
	Version   string            `json:"schema_version"`
	AuthTypes []string          `json:"auth_types"`
	Fields    []Field           `json:"fields"`
	Actions   []string          `json:"supported_actions"`
	Workflows map[string]string `json:"authorization_workflows,omitempty"`
}

func Schemas(provider, auth string) []Schema {
	list := []Schema{}
	for _, d := range domain.PlatformDescriptors() {
		if provider != "" && provider != d.ID {
			continue
		}
		types := []string{"api_key"}
		if d.OAuth {
			types = append(types, "oauth")
		}
		if d.ID == domain.PlatformCursor {
			types = []string{"oauth"}
		}
		if d.ID == domain.PlatformAnthropic {
			types = append(types, "setup-token", "service_account")
		}
		if d.ID == domain.PlatformGemini {
			types = append(types, "service_account")
		}
		if d.HostedAccountType != "" {
			types = []string{d.HostedAccountType}
		}
		if auth != "" && !slices.Contains(types, auth) {
			continue
		}
		s := Schema{Provider: d.ID, Version: SchemaVersion, AuthTypes: types, Actions: []string{}, Fields: []Field{{"name", "string", true, "TextInput"}, {"endpoint", "http_url", false, "EndpointInput"}, {"network_policy_id", "id", false, "ProxyPicker"}, {"credential", "secret", true, "SecretInput"}, {"model_ids", "string_list", false, "ModelPicker"}, {"pool_ids", "id_list", false, "PoolPicker"}, {"limits.concurrency", "integer", true, "NumberInput"}}}
		if d.ID == domain.PlatformKilo || d.ID == domain.PlatformOpenCodeGo {
			s.Fields = append(s.Fields, Field{"account_mode", "string", false, "Select"})
			// Credential requirements depend on the selected lane; server validation
			// remains authoritative for paid modes.
			for i := range s.Fields {
				if s.Fields[i].Path == "credential" {
					s.Fields[i].Required = false
				}
			}
		}
		hosted := d.HostedAccountType
		if auth != "" {
			hosted = auth
			if hosted == "api_key" {
				hosted = domain.AccountTypeAPIKey
			}
		}
		for _, action := range domain.GatewayActions() {
			if domain.PlatformAction(domain.ActionIdentity{Platform: d.ID, HostedAccountType: hosted}, action).Status == domain.ActionImplemented {
				s.Actions = append(s.Actions, action)
			}
		}
		if path := map[string]string{"anthropic": "accounts/generate-auth-url", "openai": "openai/generate-auth-url", "gemini": "gemini/oauth/auth-url", "antigravity": "antigravity/oauth/auth-url", "grok": "grok/oauth/auth-url", "workbuddy": "workbuddy/oauth/start"}[d.ID]; path != "" {
			s.Workflows = map[string]string{"oauth": "/api/v1/workspaces/{workspace_id}/" + path}
		}
		if d.ID == domain.PlatformCursor {
			s.Workflows = map[string]string{"oauth": "/api/v1/workspaces/{workspace_id}/accounts/credential-drafts"}
		}
		if d.ID == domain.PlatformAnthropic {
			s.Workflows["setup-token"] = "/api/v1/workspaces/{workspace_id}/accounts/generate-setup-token-url"
		}
		if d.ID == domain.PlatformWorkBuddy {
			s.Workflows["poll"] = "/api/v1/workspaces/{workspace_id}/workbuddy/oauth/poll"
		}
		if len(s.Workflows) > 0 && d.ID != domain.PlatformWorkBuddy {
			s.Workflows["apply"] = "/api/v1/workspaces/{workspace_id}/connections/{connection_id}/authorization"
		}
		list = append(list, s)
	}
	slices.SortFunc(list, func(a, b Schema) int { return strings.Compare(a.Provider, b.Provider) })
	return list
}

type Limits struct {
	Concurrency int `json:"concurrency"`
}
type Create struct {
	AccountMode     string            `json:"account_mode,omitempty"`
	Provider        string            `json:"provider"`
	SchemaVersion   string            `json:"schema_version"`
	AuthType        string            `json:"auth_type"`
	Name            string            `json:"name"`
	Endpoint        string            `json:"endpoint"`
	NetworkPolicyID string            `json:"network_policy_id"`
	HeaderOverrides map[string]string `json:"header_overrides,omitempty"`
}

func (in Create) validateSchema() error {
	if in.Provider == "" {
		return fault.Invalid("/provider")
	}
	schemas := Schemas(in.Provider, "")
	if len(schemas) == 0 {
		return fault.Invalid("/provider")
	}
	if !slices.Contains(schemas[0].AuthTypes, in.AuthType) {
		return fault.Invalid("/auth_type")
	}
	if in.AccountMode != "" && !validAccountMode(in.Provider, in.AuthType, in.AccountMode) {
		return fault.Invalid("/account_mode")
	}
	if in.SchemaVersion != schemas[0].Version {
		return fault.Invalid("/schema_version")
	}
	return nil
}

type Patch struct {
	Metadata        map[string]any    `json:"metadata,omitempty"`
	AccountMode     *string           `json:"account_mode,omitempty"`
	Name            *string           `json:"name,omitempty"`
	Endpoint        *string           `json:"endpoint,omitempty"`
	NetworkPolicyID *string           `json:"network_policy_id,omitempty"`
	HeaderOverrides map[string]string `json:"header_overrides,omitempty"`
	Models          []string          `json:"model_ids,omitempty"`
	Pools           []string          `json:"pool_ids,omitempty"`
	Limits          *Limits           `json:"limits,omitempty"`
}

func (p *Patch) UnmarshalJSON(data []byte) error {
	var keys map[string]json.RawMessage
	if json.Unmarshal(data, &keys) != nil || keys == nil {
		return fault.Invalid("/patch")
	}
	for key, value := range keys {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fault.Invalid("/" + key)
		}
	}
	type wirePatch Patch
	var v wirePatch
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return err
	}
	*p = Patch(v)
	return nil
}

// Preserve explicit empty maps and lists so patches can clear existing values.
func (p Patch) Snapshot() map[string]any {
	m := map[string]any{}
	if p.Metadata != nil {
		m["metadata"] = p.Metadata
	}
	if p.AccountMode != nil {
		m["account_mode"] = *p.AccountMode
	}
	if p.Name != nil {
		m["name"] = *p.Name
	}
	if p.Endpoint != nil {
		m["endpoint"] = *p.Endpoint
	}
	if p.NetworkPolicyID != nil {
		m["network_policy_id"] = *p.NetworkPolicyID
	}
	if p.HeaderOverrides != nil {
		m["header_overrides"] = p.HeaderOverrides
	}
	if p.Models != nil {
		m["model_ids"] = p.Models
	}
	if p.Pools != nil {
		m["pool_ids"] = p.Pools
	}
	if p.Limits != nil {
		m["limits"] = p.Limits
	}
	return m
}

type Check struct {
	Name   string  `json:"name"`
	State  string  `json:"state"`
	Reason *string `json:"reason"`
	Detail string  `json:"detail,omitempty"`
}
type Verification struct {
	ID         string    `json:"verification_id"`
	Checks     []Check   `json:"checks"`
	Models     []string  `json:"models"`
	VerifiedAt time.Time `json:"verified_at"`
	Expires    time.Time `json:"expires_at"`
}
type Connection struct {
	ID        int64 `json:"id,string"`
	Workspace int64 `json:"workspace_id,string"`
	Create
	Metadata     map[string]any        `json:"metadata,omitempty"`
	State        string                `json:"state"`
	Version      int64                 `json:"version,string"`
	Credential   *credentials.Metadata `json:"credential,omitempty"`
	Models       []string              `json:"model_ids"`
	Pools        []string              `json:"pool_ids"`
	Limits       Limits                `json:"limits"`
	Verification *Verification         `json:"last_verification"`
	Quota        map[string]any        `json:"quota"`
	Allowed      []string              `json:"allowed_actions"`
	Redactions   []string              `json:"redactions"`
}

func (c Connection) Project(a identity.Access) any {
	if a.Require("connections.read") != nil {
		return map[string]any{"version": strconv.FormatInt(c.Version, 10), "redactions": []string{"connection", "credential"}}
	}
	c.Metadata = safeWorkBuddyMetadata(c.Provider, c.Metadata)
	c.Allowed = []string{}
	c.Redactions = []string{}
	for _, s := range a.Actions {
		if strings.HasPrefix(s, "connections.") || strings.HasPrefix(s, "credentials.") || s == "pools.assign" {
			c.Allowed = append(c.Allowed, s)
		}
	}
	if a.Require("credentials.read_metadata") != nil {
		c.Credential = nil
		c.Redactions = append(c.Redactions, "credential")
	}
	if len(c.HeaderOverrides) != 0 && a.Require("connections.edit") != nil {
		c.HeaderOverrides = nil
		c.Redactions = append(c.Redactions, "header_overrides")
	}
	return c
}

func normalizeConnectionHeaders(account *service.Account, headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return headers, nil
	}
	if !account.IsHeaderOverrideEligible() {
		return nil, fault.Invalid("/header_overrides")
	}
	input := map[string]any{"header_overrides": headers}
	if err := service.NormalizeHeaderOverrideCredentials(input); err != nil {
		return nil, fault.Invalid("/header_overrides")
	}
	normalized := make(map[string]string, len(headers))
	rawHeaders, _ := input["header_overrides"].(map[string]any)
	for name, value := range rawHeaders {
		if text, ok := value.(string); ok && text != "" {
			normalized[name] = text
		}
	}
	return normalized, nil
}
func ValidText(s string, max int) bool {
	return utf8.ValidString(s) && s != "" && len(s) <= max && strings.TrimSpace(s) == s && strings.IndexFunc(s, unicode.IsControl) < 0
}
func ID(s string) (int64, error) {
	id, e := strconv.ParseInt(s, 10, 64)
	if e != nil || id <= 0 || strconv.FormatInt(id, 10) != s {
		return 0, fault.Invalid("/id")
	}
	return id, nil
}
func ValidEndpoint(s string) bool {
	if s == "" {
		return true
	}
	u, err := url.Parse(s)
	return err == nil && len(s) <= 2048 && (u.Scheme == "https" || u.Scheme == "http") &&
		u.Hostname() != "" && u.User == nil && !strings.ContainsAny(s, "?#\\") && u.RawPath == ""
}

func endpointChanged(current string, proposed *string) bool {
	return proposed != nil && strings.TrimRight(*proposed, "/") != strings.TrimRight(current, "/")
}
func validatePatch(p Patch) error {
	if p.Name != nil && !ValidText(*p.Name, 100) {
		return fault.Invalid("/name")
	}
	if p.Endpoint != nil && !ValidEndpoint(*p.Endpoint) {
		return fault.Invalid("/endpoint")
	}
	if p.NetworkPolicyID != nil && *p.NetworkPolicyID != "" {
		if _, e := ID(*p.NetworkPolicyID); e != nil {
			return fault.Invalid("/network_policy_id")
		}
	}
	if p.Limits != nil && (p.Limits.Concurrency < 1 || p.Limits.Concurrency > 10000) {
		return fault.Invalid("/limits/concurrency")
	}
	if len(p.Models) > 1024 || len(p.Pools) > 100 {
		return fault.Invalid("/assignments")
	}
	for _, ids := range [][]string{p.Models, p.Pools} {
		seen := map[string]bool{}
		for _, s := range ids {
			if !ValidText(s, 256) || seen[s] {
				return fault.Invalid("/assignments")
			}
			seen[s] = true
		}
	}
	for _, s := range p.Pools {
		if _, err := ID(s); err != nil {
			return fault.Invalid("/pool_ids")
		}
	}
	return nil
}
func conflict(reason string) error {
	return &fault.Fault{Status: 409, Reason: reason, Message: "Reload the connection and review its current state."}
}

type Item struct {
	Verification string `json:"verification_id,omitempty"`
	ID           string `json:"id"`
	Base         int64  `json:"base_version,string"`
	State        string `json:"state"`
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`
	Version      int64  `json:"version,string,omitempty"`
}
type Preview struct {
	ID      string    `json:"preview_id"`
	Base    int64     `json:"base_version,string,omitempty"`
	Adds    []string  `json:"adds"`
	Removes []string  `json:"removes"`
	Blocked []string  `json:"blocked"`
	Items   []Item    `json:"items"`
	Expires time.Time `json:"expires_at"`
}
type Pool struct {
	ID          int64    `json:"id,string"`
	Name        string   `json:"name"`
	Connections []string `json:"connection_ids"`
	Max         int      `json:"max_concurrency"`
	Version     int64    `json:"version,string"`
}
type Event struct {
	ID      int64     `json:"id,string"`
	Type    string    `json:"type"`
	Version int64     `json:"version,string"`
	At      time.Time `json:"at"`
	Actor   *string   `json:"actor_id"`
}

// Public lanes use the same versioned secret lifecycle with an empty/public key.
func (c Connection) credentialPayload(change credentials.Change) (map[string]any, error) {
	if change.Operation != "set" {
		if change.Operation == "keep" && change.Metadata != nil && c.Provider == service.PlatformWorkBuddy {
			without := change
			without.Metadata = nil
			if _, err := without.Payload(c.AuthType); err != nil {
				return nil, err
			}
			values := map[string]any{}
			if err := c.workBuddyMetadata(values, change.Metadata); err != nil {
				return nil, err
			}
			return values, nil
		}
		return change.Payload(c.AuthType)
	}
	if len(change.Metadata) > 0 && c.Provider != service.PlatformWorkBuddy {
		return nil, fault.Invalid("/metadata")
	}
	if c.AuthType == "api_key" && (c.Provider == service.PlatformKilo && c.AccountMode == "apikey" || c.Provider == service.PlatformOpenCodeGo && c.AccountMode != "zen_free") && strings.TrimSpace(change.Secret) == "public" {
		return nil, fault.Invalid("/secret")
	}
	a := &service.Account{Platform: c.Provider, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"account_mode": c.AccountMode, "api_key": strings.TrimSpace(change.Secret)}}
	if c.AuthType == "api_key" && (a.IsKiloFree() || a.IsOpenCodeZenFree()) {
		if change.Type != c.AuthType || change.RefreshToken != "" {
			return nil, fault.Invalid("/type")
		}
		if err := service.NormalizeFreeModelCredentials(a); err != nil {
			return nil, err
		}
		return map[string]any{"api_key": a.GetCredential("api_key")}, nil
	}
	values, err := change.Payload(c.AuthType)
	if err == nil && values != nil && c.Provider == service.PlatformKilo {
		a.Credentials["api_key"] = values["api_key"]
		if err = service.NormalizeFreeModelCredentials(a); err != nil {
			return nil, err
		}
	}
	if err != nil || values == nil {
		return values, err
	}
	if c.Provider == service.PlatformWorkBuddy {
		if err := c.workBuddyMetadata(values, change.Metadata); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func validAccountMode(provider, auth, mode string) bool {
	return auth == "api_key" && ((provider == service.PlatformKilo && slices.Contains([]string{"free", "apikey"}, mode)) || (provider == service.PlatformOpenCodeGo && slices.Contains([]string{"go", "zen", "zen_free"}, mode)))
}

func (c Connection) modeChanged(p Patch) bool {
	return p.AccountMode != nil && *p.AccountMode != c.AccountMode
}

func (c Connection) validateModePatch(p Patch) error {
	if p.AccountMode == nil {
		return nil
	}
	if !validAccountMode(c.Provider, c.AuthType, *p.AccountMode) {
		return fault.Invalid("/account_mode")
	}
	// A mode selects a different authorization lane. Explicitly clear the current
	// version first so a public credential cannot become a paid credential.
	wasPublic := c.AccountMode == "free" || c.AccountMode == "zen_free"
	willBePublic := *p.AccountMode == "free" || *p.AccountMode == "zen_free"
	if wasPublic != willBePublic && c.Credential != nil && c.Credential.Version > 0 && c.Credential.State != "revoked" {
		return conflict("CLEAR_CREDENTIAL_BEFORE_MODE_CHANGE")
	}
	return nil
}

var workBuddyMetadataFields = []string{"site", "uid", "domain", "enterprise_id", "expires_at", "refresh_expires_at"}

func (c Connection) workBuddyMetadata(values, metadata map[string]any) error {
	for key, value := range safeWorkBuddyMetadata(c.Provider, c.Metadata) {
		values[key] = value
	}
	allowed := []string{"site"}
	if c.AuthType == "oauth" {
		allowed = workBuddyMetadataFields
	}
	for key, value := range metadata {
		if !slices.Contains(allowed, key) {
			return fault.Invalid("/metadata")
		}
		if value == nil || value == "" {
			values[key] = ""
			continue
		}
		switch key {
		case "expires_at", "refresh_expires_at":
			switch value.(type) {
			case string, float64, int, int64, json.Number:
			default:
				return fault.Invalid("/metadata")
			}
			if value == "" {
				return fault.Invalid("/metadata")
			}
		default:
			text, ok := value.(string)
			if !ok || !ValidText(strings.TrimSpace(text), 2048) {
				return fault.Invalid("/metadata")
			}
			value = strings.TrimSpace(text)
			if key == "site" {
				site := strings.ToLower(strings.TrimSpace(text))
				sites := []string{"cn", "global"}
				if c.AuthType == "oauth" {
					sites = append(sites, "china", "workbuddy", "intl", "international", "workbuddy-ai", "workbuddy_ai")
				}
				if !slices.Contains(sites, site) {
					return fault.Invalid("/metadata")
				}
				value = service.NormalizeWorkBuddySite(site, "", "")
			}
		}
		values[key] = value
	}
	kind := service.AccountTypeAPIKey
	if c.AuthType == "oauth" {
		kind = service.AccountTypeOAuth
	}
	a := &service.Account{Platform: c.Provider, Type: kind, Credentials: values}
	if err := service.NormalizeConnectionCredentialMetadata(a); err != nil {
		return fault.Invalid("/metadata")
	}
	for key, value := range a.Credentials {
		values[key] = value
	}
	return nil
}

// Defense in depth at both storage scan and DTO projection boundaries.
func safeWorkBuddyMetadata(provider string, values map[string]any) map[string]any {
	if provider != service.PlatformWorkBuddy {
		return nil
	}
	out := map[string]any{}
	for _, key := range workBuddyMetadataFields {
		v, ok := values[key]
		if !ok {
			continue
		}
		switch v.(type) {
		case string:
			out[key] = v
		case float64, int, int64, json.Number:
			if key == "expires_at" || key == "refresh_expires_at" {
				out[key] = v
			}
		}
	}
	return out
}
