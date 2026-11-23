package dto

// EffectiveConfigItem is a redacted, authoritative view of one registered setting.
// Sensitive items always carry a null value and use Configured for presence.
type EffectiveConfigItem struct {
	Key               string   `json:"key"`
	Group             string   `json:"group"`
	ValueType         string   `json:"value_type"`
	Value             any      `json:"value"`
	Source            string   `json:"source"`
	Authority         string   `json:"authority"`
	FallbackChain     []string `json:"fallback_chain"`
	RestartRequired   bool     `json:"restart_required"`
	Resettable        bool     `json:"resettable"`
	Sensitive         bool     `json:"sensitive"`
	State             string   `json:"state"`
	Configured        *bool    `json:"configured"`
	BlockedBy         string   `json:"blocked_by,omitempty"`
	InstallSeedSource string   `json:"install_seed_source,omitempty"`
	Baseline          any      `json:"baseline,omitempty"`
	ErrorCode         string   `json:"error_code,omitempty"`
}

type EffectiveConfigResponse struct {
	ConfigFile  string                `json:"config_file"`
	GeneratedAt string                `json:"generated_at"`
	Items       []EffectiveConfigItem `json:"items"`
	NextCursor  string                `json:"next_cursor,omitempty"`
}

type ResetConfigRequest struct {
	Keys []string `json:"keys"`
}

type ResetConfigResponse struct {
	Settings    map[string]any        `json:"settings"`
	Items       []EffectiveConfigItem `json:"items"`
	GeneratedAt string                `json:"generated_at"`
}
