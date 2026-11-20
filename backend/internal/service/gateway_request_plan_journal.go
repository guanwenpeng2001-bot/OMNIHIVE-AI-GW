package service

import (
	"context"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
)

// AdmitRealtimeHandshake records the account selected for the native socket
// before dialing it. Realtime enforces its unbounded-session TPM policy at the
// dialer and retains its existing deadline and per-event budget checks.
func (p *GatewayRequestPlan) AdmitRealtimeHandshake(ctx context.Context, account int64) (func(error), error) {
	if p == nil {
		return nil, journal.ErrDispatch
	}
	if p.admissionErr != nil {
		return nil, p.admissionErr
	}
	at, err := p.journalDispatch(ctx, account, 1)
	if err != nil {
		return nil, err
	}
	return func(err error) {
		outcome := "transport_response"
		if err != nil {
			outcome = "unknown"
		}
		p.observeJournalAttempt(at, outcome)
	}, nil
}
