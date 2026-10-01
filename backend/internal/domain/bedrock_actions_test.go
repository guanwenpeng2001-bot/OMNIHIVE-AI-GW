package domain

import (
	"strings"
	"testing"
)

// TestBedrockActionMatrix pins the Bedrock tri-state table: every implemented
// cell binds an adapter and a complete policy set, verified absences are hard
// conflicts with evidence, and verified-but-deferred cells keep their blocker
// instead of a silent unverified row.
func TestBedrockActionMatrix(t *testing.T) {
	implemented := map[string]string{
		"chat":            "gateway.chat",
		"messages":        "gateway.messages",
		"responses":       "gateway.responses",
		"count_tokens":    "gateway.count_tokens",
		"embeddings":      "bedrock.embeddings",
		"rerank":          "bedrock.rerank",
		"image.generate":  "bedrock.images",
		"image.edit":      "bedrock.images",
		"image.variation": "bedrock.image.variation",
	}
	hardConflicts := []string{"image.async", "image.batch", "video.edit", "video.extend", "audio.tts", "audio.stt", "audio.translate", "voice", "search", "alpha_search", "live", "responses.websocket", "native"}
	deferred := map[string]string{"video.generate": "StartAsyncInvoke", "video.status": "GetAsyncInvoke", "video.content": "output.mp4", "realtime": "InvokeModelWithBidirectionalStream", "responses.compact": "compact-2026-01-12"}

	for _, hostedType := range []string{"", AccountTypeBedrock} {
		identity := ActionIdentity{Platform: PlatformBedrock, HostedAccountType: hostedType}
		for action, adapter := range implemented {
			cell := PlatformAction(identity, action)
			if cell.Status != ActionImplemented || cell.Adapter != adapter {
				t.Fatalf("hosted %q cell %s = %s/%s, want implemented %s", hostedType, action, cell.Status, cell.Adapter, adapter)
			}
			if cell.CredentialPolicy != "bedrock account authentication (SigV4 or bearer)" {
				t.Fatalf("cell %s lost the Bedrock credential policy: %q", action, cell.CredentialPolicy)
			}
			if cell.Evidence == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
				t.Fatalf("incomplete Bedrock cell %s: %+v", action, cell)
			}
		}
		for _, action := range hardConflicts {
			cell := PlatformAction(identity, action)
			if cell.Status != ActionHardConflict {
				t.Fatalf("cell %s = %s, want hard_conflict", action, cell.Status)
			}
			if cell.Adapter != "" || cell.Evidence == "" {
				t.Fatalf("hard conflict without evidence or with adapter: %+v", cell)
			}
		}
		for action, evidenceFragment := range deferred {
			cell := PlatformAction(identity, action)
			if cell.Status != ActionUnverified || cell.Adapter != "" {
				t.Fatalf("cell %s = %s/%s, want unverified without adapter", action, cell.Status, cell.Adapter)
			}
			if !strings.Contains(cell.Evidence, evidenceFragment) {
				t.Fatalf("cell %s lost its deferral evidence: %q", action, cell.Evidence)
			}
		}
	}
}

// TestBedrockHostedAnthropicTrunkKeepsItsAdapter pins the legacy hosted identity
// (Anthropic product entry, Bedrock credentials): the trunk adapter stays, and
// only count_tokens gets the native Bedrock evidence.
func TestBedrockHostedAnthropicTrunkKeepsItsAdapter(t *testing.T) {
	identity := ActionIdentity{Platform: PlatformAnthropic, HostedAccountType: AccountTypeBedrock}
	for action, adapter := range map[string]string{"messages": "gateway.messages", "chat": "gateway.chat", "responses": "gateway.responses", "count_tokens": "gateway.count_tokens"} {
		cell := PlatformAction(identity, action)
		if cell.Status != ActionImplemented || cell.Adapter != adapter {
			t.Fatalf("hosted Bedrock cell %s = %s/%s, want %s", action, cell.Status, cell.Adapter, adapter)
		}
	}
	countTokens := PlatformAction(identity, "count_tokens")
	if !strings.Contains(countTokens.Evidence, "count-tokens") {
		t.Fatalf("hosted Bedrock count_tokens lost the native operation evidence: %q", countTokens.Evidence)
	}
	if cell := PlatformAction(identity, "audio.tts"); cell.Status == ActionHardConflict {
		t.Fatal("the hosted Anthropic entry must keep its own column; the Bedrock conflicts belong to PlatformBedrock")
	}
}
