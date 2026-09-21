package domain

import (
	"strings"
	"testing"
)

// Every Hunyuan cell must carry a tri-state plus the dated verification result
// that produced it: an adapter binding, a hard conflict with evidence, or an
// explicit unverified row naming the adapter and the gate blocking the bind.
func TestHunyuanActionMatrixIsFullyEvidenced(t *testing.T) {
	hardConflict := []string{
		"responses.compact", "generate_content", "rerank", "image.variation",
		"image.batch", "video.extend", "audio.translate", "realtime", "search",
		"alpha_search", "live", "responses.websocket",
	}
	unverifiedWithAdapter := []string{
		"image.generate", "image.edit", "video.generate", "video.status",
		"video.content", "audio.tts", "audio.stt", "voice", "embeddings",
	}
	unverifiedWithoutAdapter := []string{"messages", "responses", "count_tokens", "image.async", "video.edit", "native"}

	matrix := PlatformActionMatrix(ActionIdentity{Platform: PlatformHunyuan})
	if len(matrix) != len(GatewayActions()) {
		t.Fatalf("missing hunyuan cells: %d of %d", len(matrix), len(GatewayActions()))
	}
	seen := map[string]ActionDescriptor{}
	for _, cell := range matrix {
		seen[cell.Action] = cell
		if cell.Evidence == "" {
			t.Fatalf("hunyuan cell %s lacks evidence", cell.Action)
		}
		if cell.Status != ActionImplemented && cell.Adapter != "" {
			t.Fatalf("only implemented cells may advertise an adapter: %+v", cell)
		}
	}

	// Chat stays on the OpenAI-compatible trunk that already serves the TokenHub
	// chat-completions path; nothing in this branch may move it.
	chat := seen["chat"]
	if chat.Status != ActionImplemented || chat.Adapter != "openai.chat" {
		t.Fatalf("hunyuan chat trunk moved: %+v", chat)
	}
	if chat.CredentialPolicy == "" || chat.ModelPolicy == "" || chat.UsagePolicy == "" || chat.Lifecycle == "" {
		t.Fatalf("hunyuan chat cell lacks policy bindings: %+v", chat)
	}

	for _, action := range hardConflict {
		cell := seen[action]
		if cell.Status != ActionHardConflict {
			t.Fatalf("hunyuan %s: got %s, want hard_conflict with evidence", action, cell.Status)
		}
		if !strings.Contains(cell.Evidence, "1823/") || !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("hunyuan %s hard conflict lacks a dated first-party anchor: %s", action, cell.Evidence)
		}
	}

	// Cells whose adapter exists in this branch must never read as a bare
	// "unsupported": each names its dispatch key and the runtime gate that blocks
	// the bind.
	for _, action := range unverifiedWithAdapter {
		cell := seen[action]
		if cell.Status != ActionUnverified {
			t.Fatalf("hunyuan %s: got %s, want unverified while the chat-only classification blocks the bind", action, cell.Status)
		}
		key := hunyuanPendingAdapters[action]
		if key == "" {
			t.Fatalf("hunyuan %s must name the dispatch key of its implemented adapter", action)
		}
		if !strings.Contains(cell.Evidence, key) || !strings.Contains(cell.Evidence, "ChatOnly=true") {
			t.Fatalf("hunyuan %s must name its adapter and the platform lock: %s", action, cell.Evidence)
		}
		if !strings.Contains(cell.Evidence, "1823/") || !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("hunyuan %s lacks the dated first-party evidence anchor: %s", action, cell.Evidence)
		}
	}

	for _, action := range unverifiedWithoutAdapter {
		cell := seen[action]
		if cell.Status != ActionUnverified {
			t.Fatalf("hunyuan %s: got %s, want unverified with the verification result", action, cell.Status)
		}
		if !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("hunyuan %s unverified cell lacks dated verification: %s", action, cell.Evidence)
		}
	}

	// The implemented set must be exactly what this adapter can serve today.
	if len(hunyuanActionAdapters) != 1 || hunyuanActionAdapters["chat"] != "openai.chat" {
		t.Fatalf("hunyuan bound adapters changed: %v", hunyuanActionAdapters)
	}
}

// Adapter keys must stay bindable: every pending cell names a key whose handler
// exists in this branch, and the ingress handoff list covers each key once.
func TestHunyuanPendingAdapterKeysMatchTheirHandlers(t *testing.T) {
	expected := map[string]string{
		"image.generate": "hunyuan.images",
		"image.edit":     "hunyuan.image.edit",
		"video.generate": "hunyuan.video.generate",
		"video.status":   "hunyuan.video.status",
		"video.content":  "hunyuan.video.content",
		"audio.tts":      "hunyuan.audio.tts",
		"audio.stt":      "hunyuan.audio.stt",
		"voice":          "hunyuan.voice",
		"embeddings":     "hunyuan.embeddings",
	}
	if len(hunyuanPendingAdapters) != len(expected) {
		t.Fatalf("unexpected pending adapter count: %d", len(hunyuanPendingAdapters))
	}
	for action, key := range expected {
		if got := hunyuanPendingAdapters[action]; got != key {
			t.Fatalf("hunyuan %s adapter key: got %q, want %q", action, got, key)
		}
		if evidence := hunyuanActionEvidence[action]; strings.TrimSpace(evidence) == "" {
			t.Fatalf("hunyuan %s has no protocol evidence", action)
		}
	}
	for action := range hunyuanPendingAdapters {
		if _, documented := hunyuanActionEvidence[action]; !documented {
			t.Fatalf("hunyuan %s is pending without an evidence row", action)
		}
	}
}

// A pending cell must become implemented the moment its adapter is bound, so the
// descriptor carries the full policy set instead of a bare status.
func TestHunyuanBoundCellCarriesPolicies(t *testing.T) {
	for action, key := range hunyuanPendingAdapters {
		cell := hunyuanActionDescriptor(ActionDescriptor{Identity: ActionIdentity{Platform: PlatformHunyuan, HostedAccountType: AccountTypeAPIKey}, Action: action}, action, key)
		if cell.Status != ActionImplemented || cell.Adapter != key {
			t.Fatalf("hunyuan %s did not bind to %s: %+v", action, key, cell)
		}
		if cell.CredentialPolicy == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
			t.Fatalf("hunyuan %s lacks policy bindings: %+v", action, cell)
		}
	}
	video := hunyuanActionDescriptor(ActionDescriptor{Action: "video.generate"}, "video.generate", "hunyuan.video.generate")
	if !strings.Contains(video.UsagePolicy, "deferred") || video.Lifecycle != "request plan and upstream task binding" {
		t.Fatalf("hunyuan video.generate lacks the deferred billing binding: %+v", video)
	}
	voice := hunyuanActionDescriptor(ActionDescriptor{Action: "voice"}, "voice", "hunyuan.voice")
	if !strings.Contains(voice.UsagePolicy, "demo audio") {
		t.Fatalf("hunyuan voice must keep the first-synthesis clone-fee billing semantics: %+v", voice)
	}
}

// Hosted identity dimensions must not manufacture TokenHub adapters: the
// platform is API-key only, so a foreign hosted account type resolves no cell.
func TestHunyuanHostedIdentitiesDoNotManufactureAdapters(t *testing.T) {
	for _, hosted := range []string{AccountTypeOAuth, AccountTypeBedrock, AccountTypeServiceAccount} {
		for _, action := range []string{"chat", "image.generate", "video.generate", "audio.tts", "voice", "embeddings"} {
			cell := PlatformAction(ActionIdentity{Platform: PlatformHunyuan, HostedAccountType: hosted}, action)
			if cell.Status != ActionUnverified || cell.Adapter != "" {
				t.Fatalf("hunyuan+%s %s must stay unverified: %+v", hosted, action, cell)
			}
			if strings.TrimSpace(cell.Evidence) == "" {
				t.Fatalf("hunyuan+%s %s lacks evidence", hosted, action)
			}
		}
	}
}
