package domain

import (
	"strings"
	"testing"
)

// OpenAI cells verified against the official OpenAI API specification
// (openai/openai-openapi@master, 224 paths) and the video/audio references on
// 2026-09-21. The realtime cell is deliberately absent: the published adapter
// had no registered handler, so it is now unverified (see below).
func TestOpenAIActionMatrixVerifiedCells(t *testing.T) {
	implemented := map[string]string{
		"image.variation": "openai.image.variation",
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

// The realtime cell is the counterexample the handler-binding suite now
// enforces: the published adapter had no registered dispatch key, so no identity
// may advertise it as implemented until an ingress actually serves /v1/realtime.
// The evidence has to name that state instead of silently dropping the adapter.
func TestOpenAIActionMatrixRealtimeCellStaysUnverified(t *testing.T) {
	for _, hostedType := range []string{"", AccountTypeAPIKey, AccountTypeOAuth} {
		cell := PlatformAction(ActionIdentity{Platform: PlatformOpenAI, HostedAccountType: hostedType}, "realtime")
		if cell.Status == ActionImplemented || cell.Adapter != "" {
			t.Fatalf("realtime must not advertise an adapter without a registered handler: %+v", cell)
		}
		if cell.Status != ActionUnverified {
			t.Fatalf("realtime is not implemented, so it stays unverified: %+v", cell)
		}
		if cell.Evidence == "" || !strings.Contains(cell.Evidence, "action_not_implemented") {
			t.Fatalf("realtime evidence must state the actual ingress behaviour: %+v", cell)
		}
	}
	// The neighbouring platform keeps its own adapter: a cell is a platform
	// column, not a global ban.
	if grok := PlatformAction(ActionIdentity{Platform: PlatformGrok, HostedAccountType: AccountTypeAPIKey}, "realtime"); grok.Status != ActionImplemented || grok.Adapter != "grok.realtime" {
		t.Fatalf("grok realtime cell regressed: %+v", grok)
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
	for _, action := range []string{"audio.tts", "audio.stt", "voice", "generate_content", "native"} {
		cell := PlatformAction(ActionIdentity{Platform: PlatformOpenAI, HostedAccountType: AccountTypeAPIKey}, action)
		if cell.Status != ActionUnverified || cell.Adapter != "" {
			t.Fatalf("pre-ingress cell moved early: %+v", cell)
		}
	}
}
