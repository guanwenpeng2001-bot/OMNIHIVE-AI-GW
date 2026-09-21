package domain

import "testing"

func TestActionMatrixCoversEveryPlatformAndAction(t *testing.T) {
	for _, platform := range PlatformDescriptors() {
		for _, hostedType := range []string{"", AccountTypeAPIKey, AccountTypeOAuth, AccountTypeBedrock, AccountTypeServiceAccount} {
			identity := ActionIdentity{Platform: platform.ID, HostedAccountType: hostedType}
			matrix := PlatformActionMatrix(identity)
			if len(matrix) != len(GatewayActions()) {
				t.Fatalf("missing actions for %s", platform.ID)
			}
			for _, cell := range matrix {
				if hostedType == "" {
					t.Logf("MATRIX|%s|%s|%s|%s|%s", platform.ID, cell.Action, cell.Status, cell.Adapter, cell.Evidence)
				}
				switch cell.Status {
				case ActionImplemented:
					if cell.Adapter == "" || cell.CredentialPolicy == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
						t.Fatalf("incomplete adapter binding: %+v", cell)
					}
				case ActionUnverified:
					if cell.Adapter != "" {
						t.Fatalf("unverified adapter advertised: %+v", cell)
					}
				case ActionHardConflict:
					if cell.Evidence == "" {
						t.Fatal("hard conflict lacks evidence")
					}
				default:
					t.Fatalf("missing tri-state: %+v", cell)
				}
				if cell.Evidence == "" {
					t.Fatalf("missing evidence: %+v", cell)
				}
			}
		}
	}
	unknown := PlatformAction(ActionIdentity{Platform: "new-provider"}, "image.generate")
	if unknown.Status != ActionUnverified || unknown.Adapter != "" {
		t.Fatalf("unknown supplier must remain unverified: %+v", unknown)
	}
}
func TestActionMatrixPreservesExistingTrunks(t *testing.T) {
	for _, platform := range []string{PlatformCursor, PlatformAnthropic, PlatformGemini, PlatformAntigravity} {
		if got := PlatformAction(ActionIdentity{Platform: platform}, "chat").Adapter; got != "gateway.chat" {
			t.Fatalf("%s moved trunks: %s", platform, got)
		}
	}
	for _, platform := range []string{PlatformVolcengine, PlatformQianfan, PlatformHunyuan, PlatformMistral} {
		if got := PlatformAction(ActionIdentity{Platform: platform}, "chat"); got.Status != ActionImplemented || got.Adapter != "openai.chat" {
			t.Fatalf("chat adapter missing: %+v", got)
		}
		if got := PlatformAction(ActionIdentity{Platform: platform}, "image.generate"); got.Status != ActionUnverified {
			t.Fatalf("absence is not supplier conflict: %+v", got)
		}
	}
	bedrock := PlatformAction(ActionIdentity{Platform: PlatformAnthropic, HostedAccountType: AccountTypeBedrock}, "messages")
	if bedrock.Adapter != "gateway.messages" || bedrock.CredentialPolicy != "bedrock account authentication (SigV4 or bearer)" {
		t.Fatalf("lost hosted credential policy: %+v", bedrock)
	}
}
