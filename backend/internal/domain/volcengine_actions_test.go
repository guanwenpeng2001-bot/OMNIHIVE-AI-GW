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
	implemented := map[string]string{
		"chat": "openai.chat",
	}
	// Adapters exist and are wire-tested, but the ChatOnly platform lock keeps
	// every non-chat endpoint out of account selection, so these cells stay
	// unverified until the descriptor change lands with the ingress wiring.
	pending := map[string]string{
		"embeddings":     "volcengine.embeddings",
		"image.generate": "volcengine.images",
		"image.edit":     "volcengine.images",
		"video.generate": "volcengine.video.generate",
		"video.edit":     "volcengine.video.edit",
		"video.extend":   "volcengine.video.extend",
		"video.status":   "volcengine.video.status",
		"video.content":  "volcengine.video.content",
	}
	hardConflict := []string{
		"responses.compact", "generate_content", "image.variation", "image.async",
		"image.batch", "search", "alpha_search", "live", "responses.websocket",
	}
	unverified := []string{
		"messages", "responses", "count_tokens", "rerank", "audio.tts", "audio.stt",
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
	for action, adapter := range pending {
		cell := seen[action]
		if cell.Status != ActionUnverified || cell.Adapter != "" {
			t.Fatalf("volcengine %s: got %s/%s, want unverified while the platform lock is in place", action, cell.Status, cell.Adapter)
		}
		if !strings.Contains(cell.Evidence, adapter) {
			t.Fatalf("volcengine %s pending evidence must name the adapter %s: %s", action, adapter, cell.Evidence)
		}
		if !strings.Contains(cell.Evidence, "ChatOnly") || !strings.Contains(cell.Evidence, "docs/82379/") {
			t.Fatalf("volcengine %s pending evidence must record the blocking gate and its document: %s", action, cell.Evidence)
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
		_, isPending := volcenginePendingAdapters[action]
		_, isConflict := volcengineHardConflicts[action]
		_, isUnverified := volcengineUnverifiedReasons[action]
		if isImplemented && isPending {
			t.Fatalf("volcengine %s is both implemented and pending", action)
		}
		if cell.Status == ActionImplemented && !isImplemented {
			t.Fatalf("volcengine %s is implemented without a bound adapter row", action)
		}
		if cell.Status == ActionHardConflict && !isConflict {
			t.Fatalf("volcengine %s claims a hard conflict without a recorded row", action)
		}
		if cell.Status == ActionUnverified && !isPending && !isUnverified {
			t.Fatalf("volcengine %s is unverified without a recorded reason", action)
		}
		if !isImplemented && !isPending && !isConflict && !isUnverified {
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

	// An unknown supplier still resolves to unverified: this file must not leak
	// Volcengine adapters into other platforms.
	other := PlatformAction(ActionIdentity{Platform: PlatformQianfan}, "image.generate")
	if other.Status != ActionUnverified || other.Adapter != "" {
		t.Fatalf("volcengine adapters leaked into another platform: %+v", other)
	}
}
