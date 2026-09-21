package domain

import (
	"strings"
	"testing"
)

// Every Qianfan cell must carry a tri-state plus the dated verification result
// that produced it: an adapter binding, a hard conflict with evidence, or an
// explicit unverified row that names the adapter and gate blocking a bind.
func TestQianfanActionMatrixIsFullyEvidenced(t *testing.T) {
	hardConflict := []string{
		"messages", "responses.compact", "generate_content", "image.variation",
		"image.async", "video.edit", "video.extend", "alpha_search", "live",
		"responses.websocket",
	}
	unverifiedWithAdapter := []string{
		"embeddings", "rerank", "image.generate", "image.edit", "video.generate",
		"video.status", "video.content", "audio.tts", "voice", "search",
	}
	unverifiedWithoutAdapter := []string{"responses", "count_tokens", "image.batch", "audio.stt", "audio.translate", "realtime", "native"}

	matrix := PlatformActionMatrix(ActionIdentity{Platform: PlatformQianfan})
	if len(matrix) != len(GatewayActions()) {
		t.Fatalf("missing qianfan cells: %d of %d", len(matrix), len(GatewayActions()))
	}
	seen := map[string]ActionDescriptor{}
	for _, cell := range matrix {
		seen[cell.Action] = cell
		if cell.Evidence == "" {
			t.Fatalf("qianfan cell %s lacks evidence", cell.Action)
		}
		if cell.Status != ActionImplemented && cell.Adapter != "" {
			t.Fatalf("only implemented cells may advertise an adapter: %+v", cell)
		}
	}

	// Chat stays on the existing OpenAI-compatible trunk that already serves
	// Qianfan; nothing in this branch may move it.
	chat := seen["chat"]
	if chat.Status != ActionImplemented || chat.Adapter != "openai.chat" {
		t.Fatalf("qianfan chat trunk moved: %+v", chat)
	}
	if chat.CredentialPolicy == "" || chat.ModelPolicy == "" || chat.UsagePolicy == "" || chat.Lifecycle == "" {
		t.Fatalf("qianfan chat cell lacks policy bindings: %+v", chat)
	}

	for _, action := range hardConflict {
		cell := seen[action]
		if cell.Status != ActionHardConflict {
			t.Fatalf("qianfan %s: got %s, want hard_conflict with evidence", action, cell.Status)
		}
		if !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("qianfan %s hard conflict lacks dated evidence: %s", action, cell.Evidence)
		}
	}

	// Cells whose adapter exists in this branch must never read as a bare
	// "unsupported": they name the adapter and the runtime gate blocking it.
	for _, action := range unverifiedWithAdapter {
		cell := seen[action]
		if cell.Status != ActionUnverified {
			t.Fatalf("qianfan %s: got %s, want unverified while the chat-only classification blocks the bind", action, cell.Status)
		}
		if !strings.Contains(cell.Evidence, "chat-only") && !strings.Contains(cell.Evidence, "classification") {
			t.Fatalf("qianfan %s must name the runtime gate that blocks the bind: %s", action, cell.Evidence)
		}
		if !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("qianfan %s lacks the dated verification anchor: %s", action, cell.Evidence)
		}
	}

	for _, action := range unverifiedWithoutAdapter {
		cell := seen[action]
		if cell.Status != ActionUnverified {
			t.Fatalf("qianfan %s: got %s, want unverified with the verification result", action, cell.Status)
		}
		if !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("qianfan %s unverified cell lacks dated verification: %s", action, cell.Evidence)
		}
	}
}

// The independent Qianfan credential surfaces stay documented per adapter so a
// hosted credential never silently inherits ModelBuilder bearer authorization.
func TestQianfanAdapterCredentialSurfacesStaySeparate(t *testing.T) {
	search := qianfanActionDescriptor(ActionDescriptor{Action: "search"}, "search", "qianfan.search")
	if !strings.Contains(search.CredentialPolicy, "X-Appbuilder-Authorization") {
		t.Fatalf("qianfan search must bind the AppBuilder credential surface: %+v", search)
	}
	for _, action := range []string{"embeddings", "rerank", "image.generate", "video.generate"} {
		cell := qianfanActionDescriptor(ActionDescriptor{Action: action}, action, "qianfan."+action)
		if !strings.Contains(cell.CredentialPolicy, "ModelBuilder") {
			t.Fatalf("qianfan %s must bind the ModelBuilder bearer surface: %+v", action, cell)
		}
	}
	for _, action := range []string{"audio.tts", "voice"} {
		cell := qianfanActionDescriptor(ActionDescriptor{Action: action}, action, "qianfan."+action)
		if !strings.Contains(cell.CredentialPolicy, "speech") {
			t.Fatalf("qianfan %s must bind the Baidu speech surface: %+v", action, cell)
		}
	}
	// A bound media adapter must publish usage and lifecycle policies as well.
	video := qianfanActionDescriptor(ActionDescriptor{Action: "video.generate"}, "video.generate", "qianfan.video.generate")
	if video.Status != ActionImplemented || video.Lifecycle != "request plan and upstream task binding" || !strings.Contains(video.UsagePolicy, "deferred") {
		t.Fatalf("qianfan video cell lacks the deferred billing binding: %+v", video)
	}
}

// Hosted identity dimensions must not manufacture Qianfan adapters: the platform
// is API-key only, so a foreign hosted account type resolves no cell.
func TestQianfanHostedIdentitiesDoNotManufactureAdapters(t *testing.T) {
	for _, hosted := range []string{AccountTypeBedrock, AccountTypeServiceAccount} {
		for _, action := range []string{"video.generate", "image.generate", "embeddings", "search"} {
			cell := PlatformAction(ActionIdentity{Platform: PlatformQianfan, HostedAccountType: hosted}, action)
			if cell.Status != ActionUnverified || cell.Adapter != "" {
				t.Fatalf("qianfan+%s %s must stay unverified: %+v", hosted, action, cell)
			}
		}
	}
}
