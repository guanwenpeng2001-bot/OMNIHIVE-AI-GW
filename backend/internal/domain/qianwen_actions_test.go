package domain

import (
	"strings"
	"testing"
)

// The Qianwen matrix advertises only installed adapters. Provider features
// without a standard handler stay unverified; documented absences stay conflicts.
func TestQianwenActionMatrixMatchesImplementedAdapters(t *testing.T) {
	implemented := map[string]string{
		"video.cancel":   "qianwen.video.cancel",
		"chat":           "openai.chat",
		"messages":       "openai.messages",
		"responses":      "openai.responses",
		"count_tokens":   "openai.count_tokens",
		"embeddings":     "qianwen.embeddings",
		"rerank":         "qianwen.rerank",
		"image.generate": "qianwen.images",
		"image.edit":     "qianwen.image.edit",
		"image.async":    "qianwen.native",
		"video.generate": "qianwen.video.generate",
		"video.status":   "qianwen.video.status",
		"video.content":  "qianwen.video.content",
		"audio.tts":      "qianwen.audio.tts",
		"audio.stt":      "qianwen.audio.stt",
		"realtime":       "qianwen.realtime",
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
		if cell.Action == "video.edit" || cell.Action == "video.extend" || cell.Action == "voice" {
			if cell.Status != ActionUnverified || cell.Adapter != "" {
				t.Fatalf("unimplemented standard action advertised: %+v", cell)
			}
			if !strings.Contains(cell.Evidence, "Native-only") {
				t.Fatalf("native-only support must remain explicit: %+v", cell)
			}
			continue
		}
		if cell.Status == ActionUnverified {
			t.Fatalf("unexpected unverified action: %+v", cell)
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
		if !strings.Contains(cell.Evidence, "2026-") && !strings.Contains(cell.Evidence, "dev-docs") && !strings.Contains(cell.Evidence, "docs/") {
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
