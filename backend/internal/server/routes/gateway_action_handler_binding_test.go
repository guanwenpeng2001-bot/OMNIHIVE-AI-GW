package routes

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/stretchr/testify/require"
)

// TestGatewayActionHandlersCoverEveryImplementedCell is the entry-to-adapter
// invariant for the whole matrix, not a second hand-maintained table: an
// implemented platform×action cell must resolve to a handler the ingress
// actually registered, for every hosted credential form the platform admits.
//
// A cell whose adapter has no registered handler is exactly the defect this
// suite exists to catch: the action gate admits the request and the dispatch
// then fails with action_not_implemented, so a branch's adapter is unreachable
// even though its service-level wire tests pass.
func TestGatewayActionHandlersCoverEveryImplementedCell(t *testing.T) {
	// Handler values are opaque closures; only key presence matters here.
	handlers := GatewayActionHandlers(&handler.Handlers{
		Gateway:       &handler.GatewayHandler{},
		OpenAIGateway: &handler.OpenAIGatewayHandler{},
	})
	// Pre-existing gaps this task does not close: the action matrix advertises
	// these cells, but the ingress serves them through a mechanism other than the
	// adapter table, so no dispatch key exists to assert here. They are named
	// with their evidence instead of being silently skipped, so any *new*
	// unregistered adapter still fails this test.
	preExisting := map[string]string{
		"openai.live":                "the Live call ingress is registered as an explicit route handler (h.OpenAIGateway.Live) and never through the adapter table",
		"openai.alpha_search":        "alpha/search is registered as an explicit route handler (h.OpenAIGateway.AlphaSearch)",
		"openai.responses.websocket": "the Responses WebSocket ingress is registered as an explicit route handler (h.OpenAIGateway.ResponsesWebSocket), not through the adapter table",
		"grok.audio.stt":             "the Grok voice family is dispatched by GatewayVoiceHandler, which resolves the endpoint itself instead of the adapter table",
		"grok.audio.tts":             "same GatewayVoiceHandler family",
		"grok.voice":                 "same GatewayVoiceHandler family",
		"grok.search":                "the Grok search routes are wrapped by GatewaySearchHandler, which resolves the adapter itself",
	}
	stillGaps := map[string]bool{}
	for _, descriptor := range domain.PlatformDescriptors() {
		types := []string{""}
		if descriptor.OAuth {
			types = append(types, "oauth")
		}
		types = append(types, "api_key")
		if descriptor.HostedAccountType != "" {
			types = append(types, descriptor.HostedAccountType)
		}
		for _, hostedType := range types {
			identity := domain.ActionIdentity{Platform: descriptor.ID, HostedAccountType: hostedType}
			for _, cell := range domain.PlatformActionMatrix(identity) {
				if cell.Status != domain.ActionImplemented {
					require.Empty(t, cell.Adapter, "%s/%s/%s must not advertise an unregistered adapter", descriptor.ID, hostedType, cell.Action)
					continue
				}
				require.NotEmpty(t, cell.Adapter, "%s/%s/%s", descriptor.ID, hostedType, cell.Action)
				if reason, known := preExisting[cell.Adapter]; known {
					if handlers[cell.Adapter] != nil {
						t.Errorf("stale pre-existing gap entry %q: the ingress registers it now (%s)", cell.Adapter, reason)
					}
					stillGaps[cell.Adapter] = true
					continue
				}
				require.NotNilf(t, handlers[cell.Adapter],
					"%s/%s/%s: implemented cell selects %q but the ingress registers no handler for it",
					descriptor.ID, hostedType, cell.Action, cell.Adapter)
			}
		}
	}
	for adapter := range preExisting {
		require.Truef(t, stillGaps[adapter], "stale pre-existing gap entry %q", adapter)
	}
}
