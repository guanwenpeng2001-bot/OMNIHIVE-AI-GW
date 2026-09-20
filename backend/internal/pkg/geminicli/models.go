package geminicli

// Model represents a selectable Gemini model for UI/testing purposes.
// Keep JSON fields consistent with existing frontend expectations.
type Model struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

// Legacy Code Assist protocol restrictions are independent of preset inventory.
const (
	GoogleOneLegacyFlashModel = "gemini-2.0-flash"
	GoogleOneFlashModel       = "gemini-2.5-flash"
	GoogleOneProModel         = "gemini-2.5-pro"
)

// DefaultTestModel is the default model to preselect in test flows.
const DefaultTestModel = "gemini-2.0-flash"
