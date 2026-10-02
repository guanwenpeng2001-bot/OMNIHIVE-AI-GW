package domain

import "testing"

func TestOpenRouterCoreContract(t *testing.T) {
	p, ok := LookupPlatform("openrouter")
	if !ok || !p.ChatOnly || p.MessagesPOST != EndpointBridge || p.ResponsesPOST != EndpointBridge || p.NativeMultiProtocolResponses {
		t.Fatalf("missing OpenRouter chat bridge: %+v", p)
	}
	for _, action := range []string{"chat", "messages", "responses", "generate_content"} {
		cell := PlatformAction(ActionIdentity{Platform: "openrouter", HostedAccountType: AccountTypeAPIKey}, action)
		if cell.Status != ActionImplemented || cell.Adapter == "" {
			t.Fatalf("missing %s adapter: %+v", action, cell)
		}
	}
	for _, hosted := range []string{AccountTypeOAuth, AccountTypeBedrock, AccountTypeServiceAccount} {
		for _, cell := range PlatformActionMatrix(ActionIdentity{Platform: PlatformOpenRouter, HostedAccountType: hosted}) {
			if cell.Status == ActionImplemented {
				t.Fatalf("unsupported credential acquired an adapter: %+v", cell)
			}
		}
	}
}
