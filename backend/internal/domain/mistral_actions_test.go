package domain

import (
	"strings"
	"testing"
)

// The two protocol cells Mistral provably does not publish must be recorded as
// hard conflicts with dated first-party evidence instead of reading as unbound
// adapters, so no Composite group routes them to a Mistral account and the
// gateway answers with an explicit action rejection.
func TestMistralAbsentProtocolCellsAreHardConflicts(t *testing.T) {
	for _, action := range []string{"messages", "responses"} {
		descriptor := PlatformAction(ActionIdentity{Platform: PlatformMistral, HostedAccountType: AccountTypeAPIKey}, action)
		if descriptor.Status != ActionHardConflict {
			t.Fatalf("mistral %s: got %s, want hard_conflict with evidence", action, descriptor.Status)
		}
		if descriptor.Adapter != "" {
			t.Fatalf("mistral %s must not advertise an adapter: %+v", action, descriptor)
		}
		if !strings.Contains(descriptor.Evidence, "mistralai/platform-docs-public") || !strings.Contains(descriptor.Evidence, "2026-09-21") {
			t.Fatalf("mistral %s hard conflict lacks dated first-party evidence: %s", action, descriptor.Evidence)
		}
		if !strings.Contains(descriptor.Evidence, "no native or faithful") {
			t.Fatalf("mistral %s hard conflict must state the verified absence: %s", action, descriptor.Evidence)
		}
	}
	// Nothing was added to the platform: the protocol facts stay rejected and the
	// chat trunk stays where it was.
	platform, known := LookupPlatform(PlatformMistral)
	if !known || platform.ResponsesPOST != EndpointRejected || platform.MessagesPOST != EndpointRejected {
		t.Fatalf("mistral protocol facts moved: %+v", platform)
	}
	chat := PlatformAction(ActionIdentity{Platform: PlatformMistral, HostedAccountType: AccountTypeAPIKey}, "chat")
	if chat.Status != ActionImplemented || chat.Adapter != "openai.chat" {
		t.Fatalf("mistral chat trunk moved: %+v", chat)
	}
	// A recorded absence never leaks into another platform's cell.
	for _, other := range []string{PlatformHunyuan, PlatformVolcengine, PlatformQianfan} {
		cell := PlatformAction(ActionIdentity{Platform: other}, "responses")
		if cell.Status != ActionImplemented {
			t.Fatalf("%s responses must not inherit the mistral absence: %+v", other, cell)
		}
	}
}
