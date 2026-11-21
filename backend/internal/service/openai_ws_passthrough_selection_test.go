package service

import (
	"context"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestWSPassthroughFrameTypeSelectionAndCount(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI)
	account.Credentials["model_mapping"] = map[string]string{"selected": "selected"}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: "selected", Protocol: "responses", Action: "responses"})
	reservations := &tpmBoundaryRepository{}
	plan.settlement = NewUsageSettlementService(reservations, nil, nil)
	upstream := newStagedPassthroughConn()
	checked := &attemptFrameConn{FrameConn: upstream, accountID: account.ID, account: &account, plan: func() *GatewayRequestPlan { return plan }}
	require.ErrorIs(t, checked.WriteFrame(context.Background(), coderws.MessageText, []byte(`{"type":" response.create ","model":"unselected"}`)), ErrAccountModelUnsupported)
	require.Empty(t, upstream.writes)
	for _, payload := range []string{`{"type":"response.create","model":"selected"}`, `{"type":" response.create ","model":"selected"}`, `{"type":"custom.event","model":"unselected"}`} {
		require.NoError(t, checked.WriteFrame(context.Background(), coderws.MessageText, []byte(payload)))
	}
	require.Len(t, upstream.writes, 3)
	require.EqualValues(t, 2, checked.sentResponseCreates.Load())
	require.Len(t, reservations.commands, 2, "trimmed generation events must retain TPM admission; auxiliary events must not consume a generation")
	<-upstream.writes
	require.Equal(t, `{"type":" response.create ","model":"selected"}`, string(<-upstream.writes), "admission normalization must not rewrite the transmitted frame")
}
