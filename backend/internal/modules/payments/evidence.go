package payments

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
)

// The project and request are proven by the BE-16 adjustment. Payment orders
// themselves belong to personal funding and do not imply a project.
type EvidenceScope struct{ Workspace, Project, Actor int64 }

type Evidence struct {
	ID                     string       `json:"id"`
	Kind                   string       `json:"kind"`
	BillingSource          string       `json:"billing_source"`
	EntitlementConsumption *money.Money `json:"entitlement_consumption"`
	PlatformCharge         *money.Money `json:"platform_charge"`
	References             *References  `json:"references,omitempty"`
}
type References struct {
	SourceCommand     string  `json:"source_command_id"`
	SourceWallet      *string `json:"source_wallet_event_id"`
	OriginalWallet    string  `json:"original_wallet_event_id"`
	AdjustmentWallet  string  `json:"adjustment_wallet_event_id"`
	AdjustmentCommand string  `json:"adjustment_command_id"`
}

// LinkAdjustment attaches explicitly reviewed evidence. The adjustment, its
// original posting and the entitlement must all identify the same payer.
func (s Store) LinkAdjustment(ctx context.Context, sc EvidenceScope, adj uuid.UUID, event int64, canViewPayers bool) error {
	var ok bool
	err := s.DB.QueryRowContext(ctx, `WITH eligible AS (
 SELECT e.id,a.id AS adj FROM billing_adjustment_effects a
 JOIN billing_wallet_events w ON w.id=a.original_event_id
 JOIN billing_wallet_events delta ON delta.id=a.event_id AND delta.funding_account_id=w.funding_account_id
 JOIN billing_funding_accounts f ON f.id=w.funding_account_id
 JOIN payment_entitlement_events e ON e.id=$4 AND e.payer_id=f.user_id
 WHERE a.workspace_id=$1 AND a.project_id=$2 AND a.id=$3 AND
 (e.payer_id=$5 OR ($6 AND EXISTS(SELECT 1 FROM workspace_members m WHERE m.workspace_id=$1 AND m.user_id=e.payer_id)))),
 linked AS (INSERT INTO payment_adjustment_links(entitlement_event_id,adjustment_command_id,actor_id)
 SELECT id,adj,$5 FROM eligible ON CONFLICT DO NOTHING)
 SELECT EXISTS(SELECT 1 FROM eligible)`, sc.Workspace, sc.Project, adj, event, sc.Actor, canViewPayers).Scan(&ok)
	if err == nil && !ok {
		return sql.ErrNoRows
	}
	return err
}

func (s Store) Evidence(ctx context.Context, sc EvidenceScope, adj uuid.UUID) ([]Evidence, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id,e.kind,e.payer_id=$4,e.command_id,l.wallet_event_id,a.original_event_id,a.event_id,a.id,coalesce(cost.billing_source,'unknown'),cost.charge,cost.currency
 FROM billing_adjustment_effects a
 LEFT JOIN billing_cost_evidence cost ON cost.workspace_id=a.workspace_id AND cost.project_id=a.project_id AND cost.settlement_id=a.receipt_id AND cost.request_id=a.request_id
 LEFT JOIN payment_adjustment_links x ON x.adjustment_command_id=a.id
 LEFT JOIN payment_entitlement_events e ON e.id=x.entitlement_event_id
 LEFT JOIN payment_wallet_links l ON l.entitlement_event_id=e.id
 WHERE a.workspace_id=$1 AND a.project_id=$2 AND a.id=$3 ORDER BY e.id`, sc.Workspace, sc.Project, adj, sc.Actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Evidence{}
	found := false
	for rows.Next() {
		found = true
		var id, kind, cmd, source sql.NullString
		var owner sql.NullBool
		var billingSource string
		var charge, currency sql.NullString
		var ref References
		if err = rows.Scan(&id, &kind, &owner, &cmd, &source, &ref.OriginalWallet, &ref.AdjustmentWallet, &ref.AdjustmentCommand, &billingSource, &charge, &currency); err != nil {
			return nil, err
		}
		if !id.Valid {
			continue
		}
		e := Evidence{ID: id.String, Kind: kind.String, BillingSource: billingSource}
		if charge.Valid && currency.Valid {
			amount, err := money.NewMoney(money.Currency(currency.String), charge.String)
			if err != nil {
				return nil, err
			}
			switch billingSource {
			case "wallet":
				e.PlatformCharge = &amount
			case "subscription":
				e.EntitlementConsumption = &amount
			}
		}
		if owner.Bool {
			ref.SourceCommand = cmd.String
			if source.Valid {
				ref.SourceWallet = &source.String
			}
			e.References = &ref
		}
		out = append(out, e)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, sql.ErrNoRows
	}
	return out, nil
}
