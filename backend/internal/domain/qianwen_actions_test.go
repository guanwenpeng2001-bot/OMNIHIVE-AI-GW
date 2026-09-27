package domain

import (
	"strings"
	"testing"
)

// The Qianwen matrix must be fully tri-stated: every cell is either bound to
// a verified adapter or recorded as a hard conflict with evidence.
func TestQianwenActionMatrixIsFullyVerified(t *testing.T) {
	implemented := map[string]string{
		"chat":           "openai.chat",
		"messages":       "openai.messages",
		"responses":      "openai.responses",
		"count_tokens":   "openai.count_tokens",
		"embeddings":     "qianwen.embeddings",
		"rerank":         "qianwen.rerank",
		"image.generate": "qianwen.images",
		"image.edit":     "qianwen.native",
		"image.async":    "qianwen.native",
		"video.generate": "qianwen.native",
		"video.edit":     "qianwen.native",
		"video.extend":   "qianwen.native",
		"video.status":   "qianwen.native",
		"video.content":  "qianwen.native",
		"audio.tts":      "qianwen.native",
		"audio.stt":      "qianwen.native",
		"voice":          "qianwen.native",
		"realtime":       "qianwen.native",
		"native":         "qianwen.native",
	}
	hardConflict := []string{
		"responses.compact", "generate_content", "image.variation", "image.batch",
		"audio.translate", "search", "alpha_search", "live", "responses.websocket",
	}
	matrix := PlatformActionMatrix(ActionIdentity{Platform: PlatformQianwen})
	if len(matrix) != len(GatewayActions()) {
		t.Fatalf("missing qianwen cells: %d of %d", len(matrix), len(GatewayActions()))
	}
	seen := map[string]ActionDescriptor{}
	for _, cell := range matrix {
		seen[cell.Action] = cell
		if cell.Action == "video.cancel" {
			// The newly added cancellation action has no verified Qianwen adapter.
			if cell.Status != ActionUnverified || cell.Adapter != "" || !strings.Contains(cell.Evidence, "2026-09-22") {
				t.Fatalf("qianwen cancellation must remain explicitly unverified: %+v", cell)
			}
			continue
		}
		if cell.Status == ActionUnverified {
			t.Fatalf("qianwen cell %s left unverified after evidence pass: %s", cell.Action, cell.Evidence)
		}
		if cell.Status == ActionHardConflict && cell.Adapter != "" {
			t.Fatalf("hard conflict must not advertise an adapter: %+v", cell)
		}
	}
	for action, adapter := range implemented {
		cell := seen[action]
		if cell.Status != ActionImplemented || cell.Adapter != adapter {
			t.Fatalf("qianwen %s: got %s/%s, want implemented/%s", action, cell.Status, cell.Adapter, adapter)
		}
		if cell.CredentialPolicy == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
			t.Fatalf("qianwen %s implemented cell lacks bindings: %+v", action, cell)
		}
		if !strings.Contains(cell.Evidence, "2026-09") && !strings.Contains(cell.Evidence, "dev-docs") && !strings.Contains(cell.Evidence, "docs/") {
			t.Fatalf("qianwen %s evidence lacks dated documentation: %s", action, cell.Evidence)
		}
	}
	for _, action := range hardConflict {
		cell := seen[action]
		if cell.Status != ActionHardConflict {
			t.Fatalf("qianwen %s: got %s, want hard_conflict with evidence", action, cell.Status)
		}
		if !strings.Contains(cell.Evidence, "2026-09-22") {
			t.Fatalf("qianwen %s hard conflict lacks dated evidence: %s", action, cell.Evidence)
		}
	}
	// Hosted identity dimensions do not manufacture adapters: qianwen is API-key
	// only, and a foreign hosted account type must never resolve qianwen cells.
	for _, hosted := range []string{AccountTypeBedrock, AccountTypeServiceAccount} {
		cell := PlatformAction(ActionIdentity{Platform: PlatformQianwen, HostedAccountType: hosted}, "video.generate")
		if cell.Status != ActionUnverified || cell.Adapter != "" {
			t.Fatalf("qianwen+%s must stay unverified: %+v", hosted, cell)
		}
	}
}
