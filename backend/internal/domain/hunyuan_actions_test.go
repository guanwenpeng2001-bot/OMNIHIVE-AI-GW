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
	// Promoted by multimodal-ingress: adapter, dispatch key and evidence-driven
	// chat-only gates landed together, so these cells are implemented now. The
	// responses cell binds the shared OpenAI responses trunk on the TokenHub
	// Responses compatibility mode; the messages cell binds the Anthropic path on
	// the same site (x-api-key).
	implementedWithAdapter := []string{
		"image.generate", "image.edit", "video.generate", "video.status",
		"video.content", "audio.tts", "audio.stt", "voice", "embeddings", "count_tokens",
		"responses", "messages",
	}
	unverifiedWithoutAdapter := []string{"image.async", "video.edit", "native"}

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

	for _, action := range implementedWithAdapter {
		cell := seen[action]
		if cell.Status != ActionImplemented {
			t.Fatalf("hunyuan %s: got %s, want implemented after the chat-only release", action, cell.Status)
		}
		key := hunyuanActionAdapters[action]
		if cell.Adapter != key {
			t.Fatalf("hunyuan %s adapter: got %q, want %q", action, cell.Adapter, key)
		}
		if cell.CredentialPolicy == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
			t.Fatalf("hunyuan %s lacks policy bindings: %+v", action, cell)
		}
		if evidence := hunyuanActionEvidence[action]; cell.Evidence != evidence {
			t.Fatalf("hunyuan %s evidence: got %q, want %q", action, cell.Evidence, evidence)
		}
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

	for _, action := range unverifiedWithoutAdapter {
		cell := seen[action]
		if cell.Status != ActionUnverified {
			t.Fatalf("hunyuan %s: got %s, want unverified with the verification result", action, cell.Status)
		}
		if !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("hunyuan %s unverified cell lacks dated verification: %s", action, cell.Evidence)
		}
	}

	if hunyuanActionAdapters["chat"] != "openai.chat" {
		t.Fatalf("hunyuan chat trunk moved: %v", hunyuanActionAdapters)
	}
}

// Adapter keys must stay bindable: every bound cell names a key whose handler
// exists in this branch, and the ingress handoff list covers each key once.
func TestHunyuanBoundAdapterKeysMatchTheirHandlers(t *testing.T) {
	expected := map[string]string{
		"chat":           "openai.chat",
		"messages":       "openai.messages",
		"responses":      "openai.responses",
		"count_tokens":   "openai.count_tokens",
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
	if len(hunyuanActionAdapters) != len(expected) {
		t.Fatalf("unexpected bound adapter count: %d", len(hunyuanActionAdapters))
	}
	for action, key := range expected {
		if got := hunyuanActionAdapters[action]; got != key {
			t.Fatalf("hunyuan %s adapter key: got %q, want %q", action, got, key)
		}
		if evidence := hunyuanActionEvidence[action]; strings.TrimSpace(evidence) == "" {
			t.Fatalf("hunyuan %s has no protocol evidence", action)
		}
	}
	for action := range hunyuanActionAdapters {
		if _, documented := hunyuanActionEvidence[action]; !documented {
			t.Fatalf("hunyuan %s is bound without an evidence row", action)
		}
	}
}

// A bound cell must carry the full policy set instead of a bare status.
func TestHunyuanBoundCellCarriesPolicies(t *testing.T) {
	for action, key := range hunyuanActionAdapters {
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

// The two text protocol cells TokenHub publishes next to Chat Completions must
// bind the shared trunk with the protocol, path and credential facts that
// produced the verdict, so neither cell reads as a bare status.
func TestHunyuanResponsesAndMessagesCellsBindTheSharedTrunk(t *testing.T) {
	for _, cell := range []struct {
		action, adapter, pathAnchor, credentialAnchor string
	}{
		{"responses", "openai.responses", "{base}/responses", "Authorization: Bearer"},
		{"messages", "openai.messages", "{base}/v1/messages", "x-api-key"},
	} {
		descriptor := PlatformAction(ActionIdentity{Platform: PlatformHunyuan, HostedAccountType: AccountTypeAPIKey}, cell.action)
		if descriptor.Status != ActionImplemented || descriptor.Adapter != cell.adapter {
			t.Fatalf("hunyuan %s: got %s/%s, want implemented/%s", cell.action, descriptor.Status, descriptor.Adapter, cell.adapter)
		}
		if !strings.Contains(descriptor.Evidence, cell.pathAnchor) || !strings.Contains(descriptor.Evidence, cell.credentialAnchor) {
			t.Fatalf("hunyuan %s evidence lacks the documented path or credential: %s", cell.action, descriptor.Evidence)
		}
		if !strings.Contains(descriptor.Evidence, "1823/") || !strings.Contains(descriptor.Evidence, "2026-09-21") {
			t.Fatalf("hunyuan %s evidence lacks a dated first-party anchor: %s", cell.action, descriptor.Evidence)
		}
		if descriptor.CredentialPolicy == "" || descriptor.ModelPolicy == "" || descriptor.UsagePolicy == "" || descriptor.Lifecycle == "" {
			t.Fatalf("hunyuan %s lacks policy bindings: %+v", cell.action, descriptor)
		}
		if !strings.Contains(descriptor.CredentialPolicy, "x-api-key") {
			t.Fatalf("hunyuan %s must keep the documented credential split: %+v", cell.action, descriptor)
		}
	}
	// The descriptor protocol facts these cells bind on stay published, so the
	// dispatch gate and the model catalog read one authority.
	platform, known := LookupPlatform(PlatformHunyuan)
	if !known || platform.ResponsesPOST != EndpointNative || platform.MessagesPOST != EndpointNative {
		t.Fatalf("hunyuan protocol facts moved: %+v", platform)
	}
}
