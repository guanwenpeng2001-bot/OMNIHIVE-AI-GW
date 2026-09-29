package domain

import (
	"strings"
	"testing"
)

// The Antigravity matrix must record what the CloudCode v1internal protocol can
// actually serve: every implemented cell names its adapter, every verified
// absence is a hard conflict with dated evidence, and nothing is inflated to
// implemented to make the matrix look complete.
func TestAntigravityActionMatrixTriState(t *testing.T) {
	implemented := map[string]string{
		"chat":             "gateway.chat",
		"messages":         "gateway.messages",
		"responses":        "gateway.responses",
		"count_tokens":     "gateway.count_tokens",
		"generate_content": "gateway.generate_content",
		"image.generate":   "antigravity.images",
		"image.edit":       "antigravity.images",
	}
	hardConflict := []string{
		"responses.compact", "responses.websocket", "embeddings", "rerank",
		"image.variation", "image.async", "image.batch",
		"video.generate", "video.edit", "video.extend", "video.status", "video.content",
		"audio.tts", "audio.stt", "audio.translate", "voice",
		"realtime", "search", "alpha_search", "live",
	}
	// "native" stays honestly unverified: Antigravity has no separate /api/v1 family.
	unverified := []string{"native"}

	matrix := PlatformActionMatrix(ActionIdentity{Platform: PlatformAntigravity})
	if len(matrix) != len(GatewayActions()) {
		t.Fatalf("missing antigravity cells: %d of %d", len(matrix), len(GatewayActions()))
	}
	seen := map[string]ActionDescriptor{}
	for _, cell := range matrix {
		seen[cell.Action] = cell
	}
	for action, adapter := range implemented {
		cell := seen[action]
		if cell.Status != ActionImplemented || cell.Adapter != adapter {
			t.Fatalf("antigravity %s: got %s/%s, want implemented/%s", action, cell.Status, cell.Adapter, adapter)
		}
		if cell.CredentialPolicy == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
			t.Fatalf("antigravity %s implemented cell lacks bindings: %+v", action, cell)
		}
		if !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("antigravity %s evidence lacks a dated check: %s", action, cell.Evidence)
		}
	}
	for _, action := range hardConflict {
		cell := seen[action]
		if cell.Status != ActionHardConflict {
			t.Fatalf("antigravity %s: got %s, want hard_conflict with evidence", action, cell.Status)
		}
		if cell.Adapter != "" {
			t.Fatalf("antigravity hard conflict %s must not advertise an adapter: %+v", action, cell)
		}
		if !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("antigravity %s hard conflict lacks dated evidence: %s", action, cell.Evidence)
		}
		evidence, ok := AntigravityHardConflictEvidence(action)
		if !ok || evidence != cell.Evidence {
			t.Fatalf("antigravity %s hard conflict evidence is not exposed for protocol errors", action)
		}
	}
	for _, action := range unverified {
		cell := seen[action]
		if cell.Status != ActionUnverified || cell.Adapter != "" {
			t.Fatalf("antigravity %s must stay unverified: %+v", action, cell)
		}
	}
}

// Antigravity credentials are OAuth with a CloudCode project identity; a foreign
// hosted account type must never manufacture an Antigravity cell.
func TestAntigravityForeignHostedIdentityStaysUnverified(t *testing.T) {
	for _, hosted := range []string{AccountTypeBedrock, AccountTypeServiceAccount} {
		for _, action := range []string{"generate_content", "image.generate", "messages"} {
			cell := PlatformAction(ActionIdentity{Platform: PlatformAntigravity, HostedAccountType: hosted}, action)
			if cell.Status != ActionUnverified || cell.Adapter != "" {
				t.Fatalf("antigravity+%s %s must stay unverified: %+v", hosted, action, cell)
			}
		}
	}
}
