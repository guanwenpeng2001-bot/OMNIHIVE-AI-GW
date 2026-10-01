package domain

import (
	"strings"
	"testing"
)

// The OpenCode matrix must be fully tri-stated: every cell is either bound to a
// verified gateway adapter, recorded as a verified absence (hard conflict), or
// left unverified with the reason it is not wired yet. Silently empty cells and
// adapter names without a serving entry are both failures.
func TestOpenCodeActionMatrixIsFullyVerified(t *testing.T) {
	implemented := map[string]string{
		"chat":             "openai.chat",
		"messages":         "openai.messages",
		"responses":        "openai.responses",
		"count_tokens":     "openai.count_tokens",
		"generate_content": "opencode.generate_content",
	}
	hardConflict := []string{
		"responses.compact", "embeddings", "rerank",
		"image.generate", "image.edit", "image.variation", "image.async", "image.batch",
		"video.generate", "video.edit", "video.extend", "video.status", "video.content", "video.cancel",
		"audio.tts", "audio.stt", "audio.translate", "voice",
		"realtime", "search", "alpha_search", "live", "responses.websocket",
	}
	// System One is verified upstream but has no gateway ingress yet, so it must
	// stay unverified instead of advertising an unwired adapter.
	unverified := []string{"native"}

	matrix := PlatformActionMatrix(ActionIdentity{Platform: PlatformOpenCodeGo})
	if len(matrix) != len(GatewayActions()) {
		t.Fatalf("missing opencode cells: %d of %d", len(matrix), len(GatewayActions()))
	}
	seen := map[string]ActionDescriptor{}
	for _, cell := range matrix {
		seen[cell.Action] = cell
		if cell.Evidence == "" {
			t.Fatalf("opencode cell %s has no evidence", cell.Action)
		}
		if cell.Status == ActionHardConflict && cell.Adapter != "" {
			t.Fatalf("hard conflict must not advertise an adapter: %+v", cell)
		}
		if cell.Status == ActionUnverified && cell.Adapter != "" {
			t.Fatalf("unverified cell must not advertise an adapter: %+v", cell)
		}
	}
	for action, adapter := range implemented {
		cell := seen[action]
		if cell.Status != ActionImplemented || cell.Adapter != adapter {
			t.Fatalf("opencode %s: got %s/%s, want implemented/%s", action, cell.Status, cell.Adapter, adapter)
		}
		if cell.CredentialPolicy == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
			t.Fatalf("opencode %s implemented cell lacks bindings: %+v", action, cell)
		}
		if !strings.Contains(cell.Evidence, openCodeActionEvidenceDate) {
			t.Fatalf("opencode %s evidence lacks the evidence date: %s", action, cell.Evidence)
		}
	}
	for _, action := range hardConflict {
		cell := seen[action]
		if cell.Status != ActionHardConflict {
			t.Fatalf("opencode %s: got %s, want hard_conflict with evidence", action, cell.Status)
		}
		if !strings.Contains(cell.Evidence, openCodeActionEvidenceDate) {
			t.Fatalf("opencode %s hard conflict lacks dated evidence: %s", action, cell.Evidence)
		}
	}
	for _, action := range unverified {
		cell := seen[action]
		if cell.Status != ActionUnverified {
			t.Fatalf("opencode %s: got %s, want unverified", action, cell.Status)
		}
		if !strings.Contains(cell.Evidence, openCodeActionEvidenceDate) {
			t.Fatalf("opencode %s unverified cell must state its verified upstream route and date: %s", action, cell.Evidence)
		}
	}
	if len(implemented)+len(hardConflict)+len(unverified) != len(GatewayActions()) {
		t.Fatalf("opencode tri-state does not cover every action: %d cells accounted for", len(implemented)+len(hardConflict)+len(unverified))
	}
	// The platform is API-key only, and the OpenCode-owned Google ingress says so
	// itself: no other hosted credential may resolve to it.
	for _, hosted := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeBedrock, AccountTypeServiceAccount} {
		cell := PlatformAction(ActionIdentity{Platform: PlatformOpenCodeGo, HostedAccountType: hosted}, "generate_content")
		if cell.Status != ActionUnverified || cell.Adapter != "" {
			t.Fatalf("opencode+%s must not resolve the OpenCode-owned ingress: %+v", hosted, cell)
		}
	}
	// The shared trunks are not an OpenCode-owned ingress and keep their existing
	// hosted-credential behavior; this test pins that the finalizer does not
	// silently rewrite them.
	if got := PlatformAction(ActionIdentity{Platform: PlatformOpenCodeGo, HostedAccountType: AccountTypeOAuth}, "chat").Adapter; got != "openai.chat" {
		t.Fatalf("opencode OAuth chat must keep the shared trunk adapter, got %q", got)
	}
	// A non-OpenCode platform must not inherit any of these rows.
	if got := PlatformAction(ActionIdentity{Platform: PlatformMistral}, "generate_content"); got.Status != ActionImplemented || got.Adapter != "gateway.generate_content_compat" {
		t.Fatalf("openCode rows leaked into another platform: %+v", got)
	}
}

// Native Zen Google and converted Go/text calls share one public ingress.
func TestOpenCodeGoogleCellDistinguishesNativeAndConvertedPaths(t *testing.T) {
	cell := PlatformAction(ActionIdentity{Platform: PlatformOpenCodeGo}, "generate_content")
	if cell.Status != ActionImplemented || cell.Adapter != "opencode.generate_content" {
		t.Fatalf("google cell must stay bound to the OpenCode adapter: %+v", cell)
	}
	for _, anchor := range []string{"opencode.generate_content", "native Google column", "convert through"} {
		if !strings.Contains(cell.Evidence, anchor) {
			t.Fatalf("google cell must explain its native and converted paths %q: %s", anchor, cell.Evidence)
		}
	}
	if !strings.Contains(cell.Lifecycle, "native Zen Google SSE") {
		t.Fatalf("google cell must state the plan scoping of the Google surface: %s", cell.Lifecycle)
	}
	if !strings.Contains(cell.UsagePolicy, ":countTokens") {
		t.Fatalf("google cell must state that :countTokens is a free estimate: %s", cell.UsagePolicy)
	}
}
