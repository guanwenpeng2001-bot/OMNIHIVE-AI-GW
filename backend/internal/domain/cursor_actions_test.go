package domain

import (
	"strings"
	"testing"
)

// The Cursor matrix must be fully tri-stated: every gateway action is either
// bound to a verified adapter, recorded as a hard conflict with evidence, or
// listed explicitly as still pending verification.
func TestCursorActionMatrixIsFullyTriStated(t *testing.T) {
	implemented := map[string]string{
		"chat":         "gateway.chat",
		"messages":     "gateway.messages",
		"responses":    "gateway.responses",
		"count_tokens": "cursor.count_tokens",
	}
	hardConflict := []string{
		"responses.compact", "generate_content", "embeddings", "rerank",
		"image.generate", "image.edit", "image.variation", "image.async", "image.batch",
		"video.generate", "video.edit", "video.extend", "video.status", "video.content",
		"audio.tts", "audio.stt", "audio.translate", "voice", "realtime",
		"search", "alpha_search", "live", "responses.websocket",
	}
	pending := []string{"native", "video.cancel"}

	identity := ActionIdentity{Platform: PlatformCursor}
	cells := map[string]ActionDescriptor{}
	for _, action := range GatewayActions() {
		cells[action] = CursorAction(identity, action)
	}
	if len(cells) != len(GatewayActions()) {
		t.Fatalf("missing cursor cells: %d of %d", len(cells), len(GatewayActions()))
	}
	for _, action := range GatewayActions() {
		cell := cells[action]
		t.Logf("MATRIX|cursor|%s|%s|%s|%s", cell.Action, cell.Status, cell.Adapter, cell.Evidence)
		if cell.Evidence == "" {
			t.Fatalf("cursor %s lacks evidence", action)
		}
		switch cell.Status {
		case ActionImplemented:
			adapter, expected := implemented[action]
			if !expected {
				t.Fatalf("cursor %s claims an adapter without an evidence row: %+v", action, cell)
			}
			if cell.Adapter != adapter {
				t.Fatalf("cursor %s: got adapter %s, want %s", action, cell.Adapter, adapter)
			}
			if cell.CredentialPolicy == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
				t.Fatalf("cursor %s implemented cell lacks bindings: %+v", action, cell)
			}
			if !strings.Contains(cell.Evidence, "2026-09") {
				t.Fatalf("cursor %s evidence lacks a dated anchor: %s", action, cell.Evidence)
			}
		case ActionHardConflict:
			if cell.Adapter != "" {
				t.Fatalf("cursor %s hard conflict advertises an adapter: %+v", action, cell)
			}
			if !strings.Contains(cell.Evidence, "2026-09-21") {
				t.Fatalf("cursor %s hard conflict lacks the review date: %s", action, cell.Evidence)
			}
			// Every absence row must rest on the live first-party API inventory,
			// not only on repository inference.
			if !strings.Contains(cell.Evidence, "cursor.com/docs") {
				t.Fatalf("cursor %s hard conflict lacks a first-party API anchor: %s", action, cell.Evidence)
			}
		case ActionUnverified:
			if cell.Adapter != "" {
				t.Fatalf("cursor %s unverified cell advertises an adapter: %+v", action, cell)
			}
			if !strings.Contains(cell.Evidence, "2026-09-21") {
				t.Fatalf("cursor %s unverified cell lacks the review date: %s", action, cell.Evidence)
			}
		default:
			t.Fatalf("cursor %s lacks a tri-state: %+v", action, cell)
		}
	}
	for action := range implemented {
		if cells[action].Status != ActionImplemented {
			t.Fatalf("cursor %s must stay implemented", action)
		}
	}
	for _, action := range hardConflict {
		if cells[action].Status != ActionHardConflict {
			t.Fatalf("cursor %s: got %s, want hard_conflict with evidence", action, cells[action].Status)
		}
	}
	for _, action := range pending {
		if cells[action].Status != ActionUnverified {
			t.Fatalf("cursor %s: got %s, want unverified pending verification", action, cells[action].Status)
		}
	}
	if len(implemented)+len(hardConflict)+len(pending) != len(GatewayActions()) {
		t.Fatalf("cursor cells are not fully accounted for: %d+%d+%d of %d", len(implemented), len(hardConflict), len(pending), len(GatewayActions()))
	}
}

// The 2026-09-21 live first-party review must stay visible in the rows a
// reviewer reads. Cursor does publish image inputs and one agent-task family,
// which is exactly why the generation and media cells are hard conflicts rather
// than silent absences, and why the visual-input boundary is the text-only Run
// content codec instead of a claimed image capability.
func TestCursorActionEvidenceStatesTheVerifiedInputAndTaskBoundary(t *testing.T) {
	identity := ActionIdentity{Platform: PlatformCursor}
	for name, anchor := range map[string]string{"public API": cursorPublicAPIAnchor, "cloud agent": cursorCloudAgentAnchor} {
		if !strings.Contains(anchor, "cursor.com/docs") || !strings.Contains(anchor, "2026-09-21") {
			t.Fatalf("cursor %s anchor lacks its first-party URL or review date: %s", name, anchor)
		}
	}
	if !strings.Contains(cursorImageInputAnchor, "cursor.com/docs/cloud-agent/api/endpoints") ||
		!strings.Contains(cursorImageInputAnchor, "cursor.com/docs/sdk/typescript.md") ||
		!strings.Contains(cursorImageInputAnchor, "internal/pkg/cursor/chat_content.go") {
		t.Fatalf("cursor image-input anchor must bound both the capability and the gateway boundary: %s", cursorImageInputAnchor)
	}
	// The anchors must carry the first-party wording that bounds the claim.
	if !strings.Contains(cursorPublicAPIAnchor, "not a standalone model-inference or chat-completions API") {
		t.Fatalf("cursor public API anchor drops the first-party wording: %s", cursorPublicAPIAnchor)
	}
	if !strings.Contains(cursorCloudAgentAnchor, "prompt.images") {
		t.Fatalf("cursor cloud agent anchor drops the verified image-input contract: %s", cursorCloudAgentAnchor)
	}
	if evidence := CursorAction(identity, "image.generate").Evidence; !strings.Contains(evidence, "image inputs") || !strings.Contains(evidence, "text-to-image generation endpoint") || !strings.Contains(evidence, "SDKImage") {
		t.Fatalf("cursor image.generate must separate verified image input from generation: %s", evidence)
	}
	if evidence := CursorAction(identity, "image.async").Evidence; !strings.Contains(evidence, "agent/run lifecycle") {
		t.Fatalf("cursor image.async must name the only published task family: %s", evidence)
	}
	if evidence := CursorAction(identity, "chat").Evidence; !strings.Contains(evidence, "internal/pkg/cursor/chat_content.go") || !strings.Contains(evidence, "relay accounts forward the body") {
		t.Fatalf("cursor chat must bind the text-only content boundary to its codec: %s", evidence)
	}
	if evidence := cursorPendingCells["native"]; !strings.Contains(evidence, "Cloud Agents API") {
		t.Fatalf("cursor native cell must name the published API families: %s", evidence)
	}
}

// Cursor keeps its own scheduling identity: the text cells stay on the shared
// trunks, and the platform is never renormalised into the OpenAI trunk.
func TestCursorActionPreservesCursorIdentity(t *testing.T) {
	for _, hostedType := range []string{"", AccountTypeOAuth, AccountTypeAPIKey, AccountTypeUpstream} {
		identity := ActionIdentity{Platform: PlatformCursor, HostedAccountType: hostedType}
		for action, adapter := range cursorActionAdapters {
			cell := CursorAction(identity, action)
			if cell.Status != ActionImplemented || cell.Adapter != adapter {
				t.Fatalf("cursor %s/%s: got %s/%s, want implemented/%s", hostedType, action, cell.Status, cell.Adapter, adapter)
			}
			if strings.HasPrefix(adapter, "openai.") {
				t.Fatalf("cursor %s must not be normalised into the OpenAI trunk: %s", action, adapter)
			}
		}
	}
	// The shared matrix already resolves the same adapters for Cursor. The hook
	// that routes Cursor cells through CursorAction must not widen or narrow the
	// implemented set, and the pending and hard-conflict rows stay fail-closed.
	for _, hostedType := range []string{"", AccountTypeOAuth, AccountTypeAPIKey, AccountTypeUpstream} {
		identity := ActionIdentity{Platform: PlatformCursor, HostedAccountType: hostedType}
		for _, action := range GatewayActions() {
			shared := PlatformAction(identity, action)
			cursorCell := CursorAction(identity, action)
			if shared.Status == ActionImplemented {
				if cursorCell.Status != ActionImplemented || cursorCell.Adapter != shared.Adapter {
					t.Fatalf("cursor %s/%s diverges from the shared matrix: shared %s/%s, cursor %s/%s", hostedType, action, shared.Status, shared.Adapter, cursorCell.Status, cursorCell.Adapter)
				}
			}
			if cursorCell.Status != ActionImplemented && cursorCell.Adapter != "" {
				t.Fatalf("cursor %s/%s is not implemented but advertises an adapter: %+v", hostedType, action, cursorCell)
			}
		}
	}
}

// The three verified Cursor account forms carry different credentials, and a
// foreign hosted identity never resolves a Cursor cell.
func TestCursorActionCredentialFormsAndForeignIdentity(t *testing.T) {
	policies := map[string]string{}
	for _, hostedType := range []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeUpstream} {
		identity := ActionIdentity{Platform: PlatformCursor, HostedAccountType: hostedType}
		cell := CursorAction(identity, "chat")
		if cell.CredentialPolicy == "" {
			t.Fatalf("cursor %s chat lacks a credential policy", hostedType)
		}
		policies[hostedType] = cell.CredentialPolicy
	}
	if policies[AccountTypeOAuth] == policies[AccountTypeAPIKey] ||
		policies[AccountTypeOAuth] == policies[AccountTypeUpstream] ||
		policies[AccountTypeAPIKey] == policies[AccountTypeUpstream] {
		t.Fatalf("cursor account forms must not share a credential policy: %+v", policies)
	}
	countTokens := CursorAction(ActionIdentity{Platform: PlatformCursor}, "count_tokens")
	if !strings.Contains(countTokens.UsagePolicy, "non-billable") {
		t.Fatalf("cursor count_tokens must stay non-billable: %+v", countTokens)
	}
	for _, hostedType := range []string{AccountTypeBedrock, AccountTypeServiceAccount} {
		for _, action := range GatewayActions() {
			cell := CursorAction(ActionIdentity{Platform: PlatformCursor, HostedAccountType: hostedType}, action)
			if cell.Status != ActionUnverified || cell.Adapter != "" {
				t.Fatalf("cursor+%s %s must stay unverified without an adapter: %+v", hostedType, action, cell)
			}
		}
	}
	// The resolver owns Cursor only: another platform must never receive Cursor rows.
	foreign := CursorAction(ActionIdentity{Platform: PlatformQianwen}, "chat")
	if foreign.Status != ActionUnverified || foreign.Adapter != "" {
		t.Fatalf("cursor resolver must not claim another platform: %+v", foreign)
	}
}
