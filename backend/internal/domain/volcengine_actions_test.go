package domain

import (
	"strings"
	"testing"
)

// The Volcengine Ark matrix must be fully tri-stated: every cell is either
// bound to a serving adapter, recorded as a hard conflict with first-party
// evidence, or explicitly unverified with the reason that blocks it. A cell may
// only be implemented when a Volcengine account can actually execute it
// (routes/gateway_protocol_matrix_test.go enforces the same invariant).
func TestVolcengineActionMatrixIsFullyTriStated(t *testing.T) {
	// Adapters exist and are wire-tested; multimodal-ingress registered every
	// dispatch key and lifted the chat-only platform lock, so these cells are
	// executable by a Volcengine Ark API-key account now. The responses cell
	// binds the shared OpenAI responses trunk on the native Ark Responses API;
	// Messages converts to this trunk or uses the configured plan Anthropic base.
	implemented := map[string]string{
		"messages":         "openai.messages",
		"generate_content": "gateway.generate_content_compat",
		"chat":             "openai.chat",
		"responses":        "openai.responses",
		"count_tokens":     "openai.count_tokens",
		"embeddings":       "volcengine.embeddings",
		"image.generate":   "volcengine.images",
		"image.edit":       "volcengine.images",
		"video.generate":   "volcengine.video.generate",
		"video.edit":       "volcengine.video.edit",
		"video.extend":     "volcengine.video.extend",
		"video.status":     "volcengine.video.status",
		"video.content":    "volcengine.video.content",
	}
	hardConflict := []string{
		"responses.compact", "image.variation", "image.async",
		"image.batch", "search", "alpha_search", "live", "responses.websocket",
	}
	unverified := []string{
		"video.cancel",
		"rerank", "audio.tts", "audio.stt",
		"audio.translate", "voice", "realtime", "native",
	}

	matrix := PlatformActionMatrix(ActionIdentity{Platform: PlatformVolcengine})
	if len(matrix) != len(GatewayActions()) {
		t.Fatalf("missing volcengine cells: %d of %d", len(matrix), len(GatewayActions()))
	}
	seen := map[string]ActionDescriptor{}
	for _, cell := range matrix {
		seen[cell.Action] = cell
		if strings.TrimSpace(cell.Evidence) == "" {
			t.Fatalf("volcengine cell %s lacks evidence", cell.Action)
		}
		switch cell.Status {
		case ActionImplemented:
			if cell.Adapter == "" || cell.CredentialPolicy == "" || cell.ModelPolicy == "" || cell.UsagePolicy == "" || cell.Lifecycle == "" {
				t.Fatalf("incomplete volcengine adapter binding: %+v", cell)
			}
		case ActionHardConflict:
			if cell.Adapter != "" {
				t.Fatalf("volcengine hard conflict must not advertise an adapter: %+v", cell)
			}
		case ActionUnverified:
			if cell.Adapter != "" {
				t.Fatalf("volcengine unverified cell must not advertise an adapter: %+v", cell)
			}
		default:
			t.Fatalf("volcengine cell %s has no tri-state: %+v", cell.Action, cell)
		}
	}

	for action, adapter := range implemented {
		cell := seen[action]
		if cell.Status != ActionImplemented || cell.Adapter != adapter {
			t.Fatalf("volcengine %s: got %s/%s, want implemented/%s", action, cell.Status, cell.Adapter, adapter)
		}
		if !strings.Contains(cell.Evidence, "docs/82379/") || !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("volcengine %s evidence lacks a dated first-party document: %s", action, cell.Evidence)
		}
	}
	for action, adapter := range implemented {
		cell := seen[action]
		if cell.Status != ActionImplemented || cell.Adapter != adapter {
			t.Fatalf("volcengine %s: got %s/%s, want implemented/%s", action, cell.Status, cell.Adapter, adapter)
		}
		if !strings.Contains(cell.Evidence, "docs/82379/") || !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("volcengine %s evidence lacks a dated first-party document: %s", action, cell.Evidence)
		}
	}
	for _, action := range hardConflict {
		cell := seen[action]
		if cell.Status != ActionHardConflict {
			t.Fatalf("volcengine %s: got %s, want hard_conflict with evidence", action, cell.Status)
		}
		if !strings.Contains(cell.Evidence, "docs/") || !strings.Contains(cell.Evidence, "2026-09-21") {
			t.Fatalf("volcengine %s hard conflict lacks dated first-party evidence: %s", action, cell.Evidence)
		}
	}
	for _, action := range unverified {
		cell := seen[action]
		if cell.Status != ActionUnverified {
			t.Fatalf("volcengine %s: got %s, want unverified with a recorded reason", action, cell.Status)
		}
		if len(cell.Evidence) < 80 {
			t.Fatalf("volcengine %s unverified cell lacks a substantive reason: %s", action, cell.Evidence)
		}
	}

	// Every cell must be classified exactly once: no action is silently missing
	// from the implemented, pending, conflict and reason sets.
	for _, action := range GatewayActions() {
		cell := seen[action]
		_, isImplemented := implemented[action]
		_, isConflict := volcengineHardConflicts[action]
		_, isUnverified := volcengineUnverifiedReasons[action]
		if cell.Status == ActionImplemented && !isImplemented {
			t.Fatalf("volcengine %s is implemented without a bound adapter row", action)
		}
		if cell.Status == ActionHardConflict && !isConflict {
			t.Fatalf("volcengine %s claims a hard conflict without a recorded row", action)
		}
		if cell.Status == ActionUnverified && !isUnverified {
			t.Fatalf("volcengine %s is unverified without a recorded reason", action)
		}
		if !isImplemented && !isConflict && !isUnverified {
			t.Fatalf("volcengine %s has no recorded evidence row", action)
		}
	}

	// The Ark data plane authenticates with the account's API key only: a
	// foreign hosted credential type must never inherit these adapters.
	for _, hosted := range []string{AccountTypeBedrock, AccountTypeServiceAccount, AccountTypeOAuth} {
		for _, action := range []string{"chat", "image.generate", "video.generate", "embeddings"} {
			cell := PlatformAction(ActionIdentity{Platform: PlatformVolcengine, HostedAccountType: hosted}, action)
			if cell.Status != ActionUnverified || cell.Adapter != "" {
				t.Fatalf("volcengine+%s %s must stay unverified: %+v", hosted, action, cell)
			}
		}
	}

	// A platform without a landed adapter branch still resolves to unverified:
	// this file must not leak Volcengine adapters into other platforms.
	other := PlatformAction(ActionIdentity{Platform: PlatformMistral}, "image.generate")
	if other.Status != ActionUnverified || other.Adapter != "" {
		t.Fatalf("volcengine adapters leaked into another platform: %+v", other)
	}
}
