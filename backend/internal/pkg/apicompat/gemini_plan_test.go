package apicompat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
)

func TestMessagesToGeminiPlanWire(t *testing.T) {
	plan := MessagesToGeminiPlan([]byte(`{"model":"fixture","messages":[],"metadata":{"user_id":"fixture"},"cache_control":{},"x/~":true}`))
	ctx := protocol.WithConversionPlan(context.Background(), plan)
	plan.Diagnostics[0].Field = "/changed"
	recorded, ok := protocol.ConversionPlanFromContext(ctx)
	if !ok {
		t.Fatal("missing conversion plan")
	}
	got, err := json.Marshal(recorded)
	const want = `{"mode":"bridge","source_protocol":"anthropic_messages","target_protocol":"gemini_generate","diagnostics":[{"field":"/cache_control","reason":"field_not_forwarded","severity":"warning"},{"field":"/metadata","reason":"field_not_forwarded","severity":"warning"},{"field":"/x~1~0","reason":"field_not_forwarded","severity":"warning"}]}`
	if err != nil || string(got) != want {
		t.Fatalf("plan = %s, error = %v", got, err)
	}
}
