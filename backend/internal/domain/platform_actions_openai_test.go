package domain

import (
	"testing"
)

// OpenAI cells verified against the official OpenAI API specification
// (openai/openai-openapi@master, 224 paths) and the video/audio references on
// 2026-09-21, with the native Realtime production ingress wired separately.
func TestOpenAIActionMatrixVerifiedCells(t *testing.T) {
	implemented := map[string]string{
		"image.variation": "openai.image.variation",
		"realtime":        "openai.realtime",
		"video.generate":  "openai.video.generate",
		"video.edit":      "openai.video.edit",
		"video.extend":    "openai.video.extend",
		"video.status":    "openai.video.status",
		"video.content":   "openai.video.content",
		"audio.translate": "openai.audio.translate",
	}
	apiKeyIdentity := ActionIdentity{Platform: PlatformOpenAI, HostedAccountType: AccountTypeAPIKey}
	for action, adapter := range implemented {
		cell := PlatformAction(apiKeyIdentity, action)
		if cell.Status != ActionImplemented || cell.Adapter != adapter {
			t.Fatalf("verified cell lost its adapter: %+v", cell)
		}
		if cell.CredentialPolicy == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
			t.Fatalf("implemented cell lacks policy binding: %+v", cell)
		}
		if cell.Evidence == "" {
			t.Fatalf("implemented cell lacks evidence: %+v", cell)
		}
		// Codex/OAuth credentials do not grant the open-API data plane.
		oauthCell := PlatformAction(ActionIdentity{Platform: PlatformOpenAI, HostedAccountType: AccountTypeOAuth}, action)
		if oauthCell.Status == ActionImplemented || oauthCell.Adapter != "" {
			t.Fatalf("oauth identity must not advertise %s: %+v", action, oauthCell)
		}
	}
	for _, action := range []string{"video.generate", "video.edit", "video.extend", "video.status", "video.content"} {
		if cell := PlatformAction(apiKeyIdentity, action); cell.Lifecycle != "request plan and upstream job" {
			t.Fatalf("video cell must bind the upstream job lifecycle: %+v", cell)
		}
	}
}

func TestOpenAIRealtimeCredentialBoundary(t *testing.T) {
	for _, hostedType := range []string{"", AccountTypeAPIKey} {
		cell := PlatformAction(ActionIdentity{Platform: PlatformOpenAI, HostedAccountType: hostedType}, "realtime")
		if cell.Status != ActionImplemented || cell.Adapter != "openai.realtime" {
			t.Fatalf("native realtime missing: %+v", cell)
		}
	}
	for _, hostedType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		cell := PlatformAction(ActionIdentity{Platform: PlatformOpenAI, HostedAccountType: hostedType}, "realtime")
		if cell.Status == ActionImplemented || cell.Adapter != "" {
			t.Fatalf("Codex credentials must not grant native realtime: %+v", cell)
		}
	}
}

func TestProviderRefreshDescriptorFlagsPreserved(t *testing.T) {
	for _, platform := range []string{PlatformZhipu, PlatformCommandCode} {
		descriptor, found := LookupPlatform(platform)
		if !found || !descriptor.NativeMultiProtocolResponses || descriptor.ResponsesPOST != EndpointNative {
			t.Fatalf("provider refresh regressed: %+v", descriptor)
		}
	}
}

func TestOpenAIActionMatrixHardConflictsKeepEvidence(t *testing.T) {
	for _, action := range []string{"rerank", "search"} {
		for _, hostedType := range []string{"", AccountTypeAPIKey, AccountTypeOAuth} {
			cell := PlatformAction(ActionIdentity{Platform: PlatformOpenAI, HostedAccountType: hostedType}, action)
			if cell.Status != ActionHardConflict {
				t.Fatalf("absence of an official endpoint is a recorded hard conflict, not silence: %+v", cell)
			}
			if cell.Adapter != "" {
				t.Fatalf("hard conflict must not advertise an adapter: %+v", cell)
			}
			if cell.Evidence == "" {
				t.Fatalf("hard conflict lacks first-party evidence: %+v", cell)
			}
		}
	}
}

// The standard audio and voice routes still terminate at the Grok-only ingress;
// flipping these cells before multimodal-ingress re-registers the routes would
// leak OpenAI keys into the Grok voice handler, so they stay unverified here.
func TestOpenAIActionMatrixPreIngressCellsStayUnverified(t *testing.T) {
	for _, action := range []string{"audio.tts", "audio.stt", "voice", "native"} {
		cell := PlatformAction(ActionIdentity{Platform: PlatformOpenAI, HostedAccountType: AccountTypeAPIKey}, action)
		if cell.Status != ActionUnverified || cell.Adapter != "" {
			t.Fatalf("pre-ingress cell moved early: %+v", cell)
		}
	}
}
