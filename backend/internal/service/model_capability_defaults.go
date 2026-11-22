package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ModelCapabilityIdentityFingerprint binds deployment facts to the same identity
// used by catalog CAS. Stable OAuth subjects survive ordinary token refreshes.
func ModelCapabilityIdentityFingerprint(account *Account) string {
	identity := map[string]any{"platform": account.Platform, "type": account.Type, "parent_account_id": account.ParentAccountID}
	for _, key := range AccountCatalogIdentityCredentialFields(account.Platform) {
		if value, exists := account.Credentials[key]; exists {
			identity[key] = value
		}
	}
	stableSubject := false
	for _, key := range AccountCatalogSubjectFields(account.Platform) {
		stableSubject = stableSubject || account.GetCredential(key) != ""
	}
	if !stableSubject && account.Credentials["access_token"] != nil {
		identity["access_token"] = account.Credentials["access_token"]
	}
	raw, _ := json.Marshal(identity)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// Read only the validated declaration's specification fields. Neither model
// membership nor endpoint authorization can be supplied by this envelope.
func accountDeclaredCapabilities(account *Account) (map[string]UpstreamModelMetadata, error) {
	if account == nil || account.Extra[ModelCapabilityDefaultsExtraKey] == nil {
		return nil, nil
	}
	raw, err := json.Marshal(account.Extra[ModelCapabilityDefaultsExtraKey])
	if err != nil {
		return nil, fmt.Errorf("invalid model capability declaration")
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var envelope struct {
		SchemaVersion       int                        `json:"schema_version"`
		Reason              string                     `json:"reason"`
		IdentityRevision    string                     `json:"identity_revision"`
		IdentityFingerprint string                     `json:"identity_fingerprint"`
		ConfirmedBy         string                     `json:"confirmed_by"`
		ConfirmedAt         string                     `json:"confirmed_at"`
		Models              map[string]json.RawMessage `json:"models"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.SchemaVersion != 1 || strings.TrimSpace(envelope.Reason) == "" || len(envelope.Models) == 0 || len(envelope.Models) > 1000 || len(raw) > 1<<20 {
		return nil, fmt.Errorf("invalid model capability declaration envelope")
	}
	if envelope.IdentityFingerprint != ModelCapabilityIdentityFingerprint(account) || envelope.IdentityRevision != strconv.FormatInt(account.ModelCatalogBaseline().IdentityRevision, 10) {
		return nil, fmt.Errorf("model capability declaration identity is stale or unbound; reconfirm deployment facts")
	}
	result := make(map[string]UpstreamModelMetadata, len(envelope.Models))
	for id, body := range envelope.Models {
		var metadata UpstreamModelMetadata
		var aliases struct {
			MaxInputTokens int64 `json:"max_input_tokens"`
			Tools          *struct {
				FunctionCalling *bool `json:"function_calling"`
			} `json:"tools"`
		}
		if json.Unmarshal(body, &metadata) != nil || json.Unmarshal(body, &aliases) != nil || id == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, "*?\r\n\t") {
			return nil, fmt.Errorf("invalid model capability declaration entry")
		}
		metadata.ID, metadata.ProviderProfile = id, resolveCapabilityProfile(account)
		metadata.FieldEvidence = nil
		metadata.SupportedMethods, metadata.EndpointStatus = nil, ""
		if metadata.ContextWindow == 0 && aliases.MaxInputTokens > 0 {
			metadata.ContextWindow, metadata.ContextLimitKind = aliases.MaxInputTokens, "input"
		}
		if metadata.SupportsFunctionCalling == nil && aliases.Tools != nil {
			metadata.SupportsFunctionCalling = aliases.Tools.FunctionCalling
		}
		if metadata.InputModalities != nil && metadata.InputModalitiesStatus == "" {
			metadata.InputModalitiesStatus = "complete"
		}
		metadata.ReasoningControl = normalizeReasoningControl(metadata)
		projectReasoningEffort(&metadata)
		stampCapabilityEvidence(&metadata, "account-declaration", capabilityHash(raw), metadata.ProviderProfile, id, "identity_bound_exact", "declared")
		result[id] = metadata
	}
	return result, nil
}
