package byok

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

type SettlementAmounts struct {
	Source                                               string
	ReservationID                                        int64
	ExternalState                                        string
	ExternalEstimate, PlatformServiceFee, PlatformCharge *money.Money
}

// SettlementAmounts derives the receipt inputs for a billable attempt using
// the immutable admission tariff and observed dimensions. The receipt owner
// decides acceptance and performs the deduplicated wallet transaction.
func (s *Store) SettlementAmounts(ctx context.Context, a scope.Authorized, attemptID string, observed *pricing.Usage) (SettlementAmounts, error) {
	var out SettlementAmounts
	var doc []byte
	e := s.db.QueryRowContext(ctx, `SELECT f.source,coalesce(f.reservation_id,0),f.frozen_price FROM byok_attempt_funding f
       JOIN gateway_execution_attempts t USING(attempt_id) JOIN gateway_executions x ON x.request_id=t.request_id AND x.workspace_id=f.workspace_id
       WHERE f.attempt_id=$1 AND x.workspace_id=$2 AND x.project_id=$3 AND x.api_key_id=$4 AND x.actor_id=$5 AND x.owner_id=$6 AND x.funding_user_id=$7`,
		attemptID, a.WorkspaceID, a.ProjectID, a.KeyID, a.ActorID, a.KeyOwnerID, a.FundingUserID).Scan(&out.Source, &out.ReservationID, &doc)
	if errors.Is(e, sql.ErrNoRows) {
		return out, fault.NotFound
	}
	if e != nil {
		return out, e
	}
	var f frozenPrice
	if e = json.Unmarshal(doc, &f); e != nil {
		return out, e
	}
	if out.Source == "byok" {
		if f.Fee == nil {
			return out, unfunded
		}
		out.ExternalState = "unknown"
		out.PlatformServiceFee = f.Fee
		out.PlatformCharge = f.Fee
	} else {
		out.ExternalState = "not_applicable"
	}
	if f.Card == nil || observed == nil {
		return out, nil
	}
	f.Card.Usage = *observed
	result, e := f.Card.Calculate()
	if e != nil {
		return out, e
	}
	if out.Source == "wallet" {
		out.PlatformCharge = result.Charge
	} else if result.Charge != nil {
		out.ExternalEstimate = result.Charge
		out.ExternalState = "estimated"
	}
	return out, nil
}
