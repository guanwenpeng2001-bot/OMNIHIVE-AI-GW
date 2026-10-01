package domain

import (
	"strings"
	"testing"
)

// Public text conversion does not imply native vendor protocol support.
func TestMistralPublicTextProtocolsUseChatBridge(t *testing.T) {
	for _, action := range []string{"messages", "responses"} {
		cell := PlatformAction(ActionIdentity{Platform: PlatformMistral, HostedAccountType: AccountTypeAPIKey}, action)
		if cell.Status != ActionImplemented || cell.Adapter != "openai."+action || !strings.Contains(cell.Evidence, "converted") {
			t.Fatalf("missing public %s bridge: %+v", action, cell)
		}
	}
	platform, _ := LookupPlatform(PlatformMistral)
	if platform.MessagesPOST != EndpointBridge || platform.ResponsesPOST != EndpointBridge || platform.NativeMultiProtocolResponses {
		t.Fatalf("public bridges must not claim native protocols: %+v", platform)
	}
}

// The four Mistral data-plane actions that service/mistral_actions.go implements
// must be reachable through the action matrix: implemented, bound to their own
// mistral.* adapter and anchored on the dated official specification. The two
// protocol cells use the existing text converters.
func TestMistralActionMatrixIsFullyEvidenced(t *testing.T) {
	implementedWithAdapter := []string{"embeddings", "audio.stt", "audio.tts", "voice"}

	matrix := PlatformActionMatrix(ActionIdentity{Platform: PlatformMistral, HostedAccountType: AccountTypeAPIKey})
	if len(matrix) != len(GatewayActions()) {
		t.Fatalf("missing mistral cells: %d of %d", len(matrix), len(GatewayActions()))
	}
	seen := map[string]ActionDescriptor{}
	for _, cell := range matrix {
		seen[cell.Action] = cell
		if cell.Evidence == "" {
			t.Fatalf("mistral cell %s lacks evidence", cell.Action)
		}
		if cell.Status != ActionImplemented && cell.Adapter != "" {
			t.Fatalf("only implemented cells may advertise an adapter: %+v", cell)
		}
	}

	// The chat trunk stays on the shared OpenAI-compatible adapter; nothing in
	// this task may move it.
	chat := seen["chat"]
	if chat.Status != ActionImplemented || chat.Adapter != "openai.chat" {
		t.Fatalf("mistral chat trunk moved: %+v", chat)
	}
	if chat.CredentialPolicy == "" || chat.ModelPolicy == "" || chat.UsagePolicy == "" || chat.Lifecycle == "" {
		t.Fatalf("mistral chat cell lacks policy bindings: %+v", chat)
	}

	for _, action := range implementedWithAdapter {
		cell := seen[action]
		if cell.Status != ActionImplemented {
			t.Fatalf("mistral %s: got %s, want implemented once the native adapter is wired", action, cell.Status)
		}
		key := mistralActionAdapters[action]
		if key == "" {
			t.Fatalf("mistral %s has no adapter authority row", action)
		}
		if cell.Adapter != key {
			t.Fatalf("mistral %s adapter: got %q, want %q", action, cell.Adapter, key)
		}
		if cell.CredentialPolicy == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
			t.Fatalf("mistral %s lacks policy bindings: %+v", action, cell)
		}
		if evidence := mistralActionEvidence[action]; cell.Evidence != evidence {
			t.Fatalf("mistral %s evidence: got %q, want %q", action, cell.Evidence, evidence)
		}
		if !strings.Contains(cell.Evidence, "mistralai/platform-docs-public") || !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("mistral %s lacks the dated official specification anchor: %s", action, cell.Evidence)
		}
		if !strings.Contains(cell.Evidence, "service/mistral_actions.go") && !strings.Contains(cell.Evidence, "service/mistral_adapter.go") {
			t.Fatalf("mistral %s evidence names no in-repo wire contract: %s", action, cell.Evidence)
		}
	}

	// The two verified protocol absences are untouched by this task.
	for _, action := range []string{"messages", "responses"} {
		cell := seen[action]
		if cell.Status != ActionImplemented || cell.Adapter != "openai."+action {
			t.Fatalf("mistral %s must use the existing text converter: %+v", action, cell)
		}
		if cell.Evidence != mistralActionEvidence[action] {
			t.Fatalf("mistral %s conversion evidence moved: %s", action, cell.Evidence)
		}
	}

	// A non-API-key hosted credential carries no Mistral data-plane credential.
	for _, hosted := range []string{AccountTypeOAuth, AccountTypeBedrock} {
		for _, action := range append([]string{"chat"}, implementedWithAdapter...) {
			cell := PlatformAction(ActionIdentity{Platform: PlatformMistral, HostedAccountType: hosted}, action)
			if cell.Status == ActionImplemented || cell.Adapter != "" {
				t.Fatalf("mistral+%s %s must not bind a data-plane adapter: %+v", hosted, action, cell)
			}
		}
	}
}

// Binding the Mistral cells must not move a sibling chat-only platform's cells:
// each platform keeps its own adapter table, and no mistral.* key may appear on
// another platform's descriptor.
func TestMistralActionBindingDoesNotMoveSiblingPlatforms(t *testing.T) {
	siblings := []struct {
		platform string
		adapters map[string]string
	}{
		{PlatformHunyuan, hunyuanActionAdapters},
		{PlatformVolcengine, volcengineActionAdapters},
		{PlatformQianfan, qianfanActionAdapters},
	}
	actions := []string{"chat", "embeddings", "audio.stt", "audio.tts", "voice"}
	for _, sibling := range siblings {
		for _, action := range actions {
			cell := PlatformAction(ActionIdentity{Platform: sibling.platform, HostedAccountType: AccountTypeAPIKey}, action)
			if strings.HasPrefix(cell.Adapter, "mistral.") {
				t.Fatalf("%s %s inherited a mistral adapter: %+v", sibling.platform, action, cell)
			}
			if cell.Status == ActionImplemented && cell.Adapter != sibling.adapters[action] {
				t.Fatalf("%s %s adapter moved: got %q, want %q", sibling.platform, action, cell.Adapter, sibling.adapters[action])
			}
		}
	}
	// The shared dispatch key the Mistral chat cell binds must stay the key the
	// siblings already bind, so one trunk keeps serving every chat-only entry.
	for _, sibling := range siblings {
		cell := PlatformAction(ActionIdentity{Platform: sibling.platform, HostedAccountType: AccountTypeAPIKey}, "chat")
		if cell.Adapter != "openai.chat" {
			t.Fatalf("%s chat trunk moved: %+v", sibling.platform, cell)
		}
	}
}

// A bound cell must carry the full policy set instead of a bare status, and the
// finalizer must refuse a foreign hosted credential with the dated reason the
// sibling chat-only entries publish.
func TestMistralBoundCellCarriesPoliciesAndRefusesForeignHostedCredentials(t *testing.T) {
	for action, key := range mistralActionAdapters {
		cell := mistralActionDescriptor(ActionDescriptor{Identity: ActionIdentity{Platform: PlatformMistral, HostedAccountType: AccountTypeAPIKey}, Action: action}, action, key)
		if cell.Status != ActionImplemented || cell.Adapter != key {
			t.Fatalf("mistral %s did not bind to %s: %+v", action, key, cell)
		}
		if cell.CredentialPolicy == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
			t.Fatalf("mistral %s lacks policy bindings: %+v", action, cell)
		}
	}
	for action := range mistralActionAdapters {
		if _, documented := mistralActionEvidence[action]; !documented {
			t.Fatalf("mistral %s is bound without an evidence row", action)
		}
	}

	// The finalizer refuses a non-API-key credential with the reason instead of
	// binding an adapter. PlatformAction already rejects those identities through
	// the chat-only credential guard before this finalizer runs, so the row is the
	// same defensive duplicate the sibling Hunyuan descriptor keeps; it is
	// asserted here so the contract is stated rather than implied.
	for _, hosted := range []string{AccountTypeOAuth, AccountTypeBedrock, AccountTypeServiceAccount} {
		for _, action := range []string{"chat", "embeddings", "audio.stt", "audio.tts", "voice"} {
			cell := mistralActionDescriptor(ActionDescriptor{Identity: ActionIdentity{Platform: PlatformMistral, HostedAccountType: hosted}, Action: action}, action, mistralActionAdapters[action])
			if cell.Status == ActionImplemented || cell.Adapter != "" {
				t.Fatalf("mistral+%s %s must not bind a data-plane adapter: %+v", hosted, action, cell)
			}
			if !strings.Contains(cell.Evidence, "carries no Mistral data-plane credential") || !strings.Contains(cell.Evidence, "2026-09-21") {
				t.Fatalf("mistral+%s %s lacks the dated credential reason: %s", hosted, action, cell.Evidence)
			}
		}
	}
}
