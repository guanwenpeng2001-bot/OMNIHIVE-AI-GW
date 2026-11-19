package requests

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
	"github.com/lib/pq"
)

type detailRow struct {
	summaryRow
	key                          int64
	lastAttempt                  *int
	actor, owner                 *int64
	origin                       string
	effectiveModel               *string
	journalID, priceVersion      *string
	ended, settled               *time.Time
	receiptID, usageID           *int64
	receiptAttemptID, completion *string
	receiptState, source         string
	pricingState                 *string
	responseState                int
	usageDuration, usageTTFT     *string
	duration, ttft               *string
	routeLatency, opsAccount     *int64
	upstreamStatus               *int
	usageJSON                    []byte
	inputTokens, outputTokens    *int64
}

func lookup(ctx context.Context, tx *sql.Tx, p Projection, id string) (detailRow, error) {
	var row detailRow
	err := tx.QueryRowContext(ctx, anchors+` AND id = $5`+observations+`
SELECT `+summaryColumns+`, api_key_id, last_attempt_ordinal, actor_id, owner_id, origin, effective_model,
       journal_id::text, price_version, observed_finish, settled_at,
       observed_receipt_id, observed_usage_id, receipt_attempt_id::text, completion_state,
       receipt_state, funding_source, pricing_state, response_state, usage_duration_ms, usage_ttft_ms,
       COALESCE(usage_duration_ms, ops_duration_ms), COALESCE(usage_ttft_ms, ops_ttft_ms),
       routing_latency_ms, ops_account_id, upstream_status_code,
       jsonb_build_object('InputTokens', usage_json->'InputTokens', 'OutputTokens', usage_json->'OutputTokens',
                          'presence', usage_json->'token_usage_observation'),
       input_tokens, output_tokens
FROM projected`, p.WorkspaceID, p.ProjectID, time.Now().UTC(), p.Finance, id).Scan(
		append(row.targets(), &row.key, &row.lastAttempt, &row.actor, &row.owner, &row.origin, &row.effectiveModel,
			&row.journalID, &row.priceVersion, &row.ended, &row.settled,
			&row.receiptID, &row.usageID, &row.receiptAttemptID, &row.completion,
			&row.receiptState, &row.source, &row.pricingState, &row.responseState, &row.usageDuration, &row.usageTTFT,
			&row.duration, &row.ttft,
			&row.routeLatency, &row.opsAccount, &row.upstreamStatus,
			&row.usageJSON, &row.inputTokens, &row.outputTokens)...)
	return row, notFound(err)
}

func (s *Store) Detail(ctx context.Context, p Projection, id string) (Detail, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Detail{}, err
	}
	defer tx.Rollback()
	row, err := lookup(ctx, tx, p, id)
	if err != nil {
		return Detail{}, err
	}
	item, err := row.project(p)
	if err != nil {
		return Detail{}, err
	}
	out := Detail{
		Summary: item, WorkspaceID: p.WorkspaceID, ProjectID: p.ProjectID, ActorID: row.actor,
		APIKeyID: row.key, KeyOwnerID: row.owner, Origin: row.origin, Action: action(item.Protocol), EffectiveModel: row.effectiveModel,
		EndedAt: utc(row.ended), Timings: Timings{Route: row.routeLatency, UpstreamTTFT: observedMS(row.ttft), Total: observedMS(row.duration)},
		Timeline: []Milestone{}, Attempts: []Attempt{}, AbandonedAttempts: []Attempt{}, Usage: row.usage(),
		Route: Route{ExplainURL: p.RequestURL(id) + "/route-explain"}, TaskIDs: []string{},
		Diagnostics: []Diagnostic{}, DataCompleteness: complete(), AllowedActions: []string{},
	}
	if p.Export {
		out.AllowedActions = append(out.AllowedActions, ExportGrant)
	}
	if item.Reason != nil {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Reason: *item.Reason})
	}
	switch row.journalID {
	case nil:
		out.DataCompleteness.missing("timeline", "LEGACY_NOT_RECORDED")
		out.DataCompleteness.missing("attempts", "LEGACY_NOT_RECORDED")
	default:
		out.addMilestone("admitted", item.StartedAt, "gateway_admission")
		if out.EndedAt != nil {
			if row.origin == "job" {
				out.addMilestone("execution_finished", *out.EndedAt, "gateway_finalization")
			} else if item.Outcome == "interrupted" || item.Outcome == "canceled" || item.Outcome == "unknown" {
				out.addMilestone("client_ended", *out.EndedAt, "gateway_finalization")
			} else {
				out.addMilestone("client_completed", *out.EndedAt, "gateway_finalization")
			}
			if !out.EndedAt.Before(item.StartedAt) {
				total := out.EndedAt.Sub(item.StartedAt).Milliseconds()
				out.Timings.Total = &total
			}
		}
		err = row.readAttempts(ctx, tx, p, &out)
		if err != nil {
			return Detail{}, err
		}
		out.TaskIDs, err = taskLinks(ctx, tx, p.Scope, *row.journalID)
		if err != nil {
			return Detail{}, err
		}
	}
	if row.settled != nil {
		out.addMilestone("settled", *row.settled, "settlement_receipt")
	}
	slices.SortStableFunc(out.Timeline, func(a, b Milestone) int { return a.At.Compare(b.At) })

	explanation, err := explain(ctx, tx, p, row.ID, row.journalID)
	if err != nil {
		return Detail{}, err
	}
	out.DataCompleteness.Missing = append(out.DataCompleteness.Missing, explanation.DataCompleteness.Missing...)
	if decision := explanation.Trace; decision != nil {
		out.TraceID = &decision.SnapshotID
		out.Route.SnapshotID = &decision.SnapshotID
		out.Route.PolicyVersion = nonempty(decision.PolicyVersion)
		if decision.Selected != nil {
			out.Route.CandidateID = nonempty(decision.Selected.CandidateID)
		}
	}
	if out.Conversion == nil {
		out.DataCompleteness.missing("conversion", row.missingReason())
	}
	if out.Usage.Level == "unknown" {
		out.DataCompleteness.missing("usage", row.missingReason())
	}
	if row.receiptID == nil {
		out.DataCompleteness.missing("billing.receipt", "RECEIPT_UNCONFIRMED")
	} else if p.Finance && row.priceVersion == nil {
		out.DataCompleteness.missing("billing.price_version", row.missingReason())
	}
	if len(out.DataCompleteness.Missing) != 0 {
		out.DataCompleteness.State = "partial"
	}
	if err := row.readBilling(ctx, tx, p, &out); err != nil {
		return Detail{}, err
	}
	return out, tx.Commit()
}

func (r detailRow) missingReason() string {
	if r.journalID == nil {
		return "LEGACY_NOT_RECORDED"
	}
	return "EVIDENCE_NOT_RECORDED"
}

func (d *Detail) addMilestone(kind string, at time.Time, source string) {
	d.Timeline = append(d.Timeline, Milestone{Kind: kind, At: at.UTC(), Source: source})
}

func (r detailRow) readAttempts(ctx context.Context, tx *sql.Tx, p Projection, d *Detail) error {
	rows, err := tx.QueryContext(ctx, `
SELECT a.attempt_id::text, a.ordinal, a.dispatched_at, a.acceptance, a.outcome, a.abandoned_uncertain,
       a.decision->>'reason', a.conversion_plan, a.account_id,
       connection.account_id, connection.name, review.reason
FROM gateway_executions e
JOIN gateway_execution_attempts a USING (request_id)
LEFT JOIN gateway_execution_reconciliation review USING (request_id, attempt_id)
LEFT JOIN provider_connections connection
    ON $4 AND connection.account_id = a.account_id AND connection.workspace_id = e.workspace_id
    AND NOT EXISTS (
        SELECT 1 FROM project_resources p
        WHERE p.kind = 'accounts' AND p.resource_id = a.account_id
          AND (p.workspace_id, p.project_id) <> (e.workspace_id, e.project_id)
    )
WHERE e.workspace_id = $1 AND e.project_id = $2 AND e.request_id = $3::uuid
  AND a.dispatched_at IS NOT NULL
ORDER BY a.ordinal`, p.WorkspaceID, p.ProjectID, r.ID, p.Connections)
	if err != nil {
		return err
	}
	defer rows.Close()

	identityRedacted := false
	for rows.Next() {
		var a Attempt
		var transportOutcome string
		var decisionReason, review *string
		var account *int64
		var plan []byte
		if err := rows.Scan(&a.ID, &a.Number, &a.StartedAt, &a.Acceptance, &transportOutcome,
			&a.AbandonedUncertain, &decisionReason, &plan, &account, &a.ConnectionID, &a.ConnectionName, &review); err != nil {
			return err
		}
		a.StartedAt = utc(a.StartedAt)
		a.Reason = attemptReason(decisionReason)
		a.ReceiptState = "unconfirmed"
		a.Conversion = decodeConversion(plan)
		a.Outcome = "unknown"
		identityRedacted = identityRedacted || (account != nil && a.ConnectionID == nil)
		if a.AbandonedUncertain {
			d.AbandonedAttempts = append(d.AbandonedAttempts, a)
			d.Diagnostics = append(d.Diagnostics, Diagnostic{Reason: "ABANDONED_ACCEPTANCE_UNKNOWN", AttemptID: &a.ID})
			continue
		}

		last := r.lastAttempt != nil && a.Number == *r.lastAttempt
		linked := r.receiptAttemptID != nil && a.ID == *r.receiptAttemptID
		if last && r.responseState == 2 { // BE-03 SemanticOutput
			observed := true
			a.SemanticOutput = &observed
		}
		switch {
		case linked:
			a.ReceiptState = r.receiptState
			a.DurationMS, a.UpstreamTTFTMS = observedMS(r.usageDuration), observedMS(r.usageTTFT)
			a.Outcome = "succeeded"
			if r.completion != nil && *r.completion == "interrupted" {
				a.Outcome = "interrupted"
			}
			// The journal has the permit time, and the receipt has upstream
			// TTFT. Their sum estimates output time, not client delivery.
			if a.UpstreamTTFTMS != nil && a.StartedAt != nil {
				first := a.StartedAt.Add(time.Duration(*a.UpstreamTTFTMS) * time.Millisecond)
				if d.EndedAt == nil || !first.After(*d.EndedAt) {
					d.addMilestone("first_output", first, "upstream_ttft_estimate")
				}
			}
		case transportOutcome == "not_sent" || transportOutcome == "rejected":
			a.Outcome = "failed"
			observed := false
			a.SemanticOutput = &observed
		case last && d.Outcome == "running":
			a.Outcome = "running"
		}
		if last && (d.Outcome == "failed" || d.Outcome == "interrupted") && account != nil && r.opsAccount != nil &&
			*account == *r.opsAccount && decisionReason == nil {
			a.Status = r.upstreamStatus
			if a.Outcome == "unknown" {
				a.Outcome = d.Outcome
			}
		}
		if review != nil && *review == "missing_receipt" {
			d.Diagnostics = append(d.Diagnostics, Diagnostic{Reason: "RECEIPT_UNCONFIRMED", AttemptID: &a.ID})
		}
		if a.Conversion != nil && (linked || (r.receiptAttemptID == nil && last)) {
			d.Conversion = a.Conversion
		}
		d.Attempts = append(d.Attempts, a)
	}
	if identityRedacted {
		d.Redactions = append(d.Redactions, "connection_identity")
	}
	return rows.Err()
}

func (r detailRow) readBilling(ctx context.Context, tx *sql.Tx, p Projection, d *Detail) error {
	b := Billing{State: r.SettlementState, ReceiptState: r.receiptState, LedgerEntryIDs: []string{}}
	d.Billing = b
	if !p.Finance {
		return nil
	}

	b.Source, b.PriceVersion, b.SettlementID, b.PricingState = &r.source, r.priceVersion, r.receiptID, r.pricingState
	b.PlatformCharge = d.ChargedAmount
	var hold *string
	err := tx.QueryRowContext(ctx, `
SELECT funded.id, reservation.id, reservation.status, reservation.hold_amount::text,
       CASE WHEN reservation.id IS NOT NULL THEN obligation.reservation_id IS NOT NULL END,
       ARRAY(SELECT event.id::text
             FROM billing_settlement_outbox settled
             JOIN billing_wallet_events event ON event.transaction_id = settled.transaction_id
             WHERE settled.receipt_id = anchor.receipt AND event.funding_account_id = funded.id
             ORDER BY event.id)
FROM (
    SELECT request.funding_user_id, request.api_key_id, request.journal_id, $4::bigint AS receipt
    FROM request_anchors request WHERE workspace_id = $1 AND project_id = $2 AND id = $3
) anchor
LEFT JOIN gateway_executions execution ON execution.request_id = anchor.journal_id
LEFT JOIN billing_funding_accounts funded ON funded.user_id = anchor.funding_user_id AND funded.currency = 'USD'
LEFT JOIN api_key_budget_reservations reservation
    ON reservation.api_key_id = anchor.api_key_id AND reservation.user_id = anchor.funding_user_id
    AND reservation.request_id = NULLIF(execution.reservation_ref, '')
LEFT JOIN gateway_execution_release_obligations obligation
    ON obligation.workspace_id = $1 AND obligation.project_id = $2 AND obligation.request_id = anchor.journal_id
    AND obligation.reservation_id = reservation.id`, p.WorkspaceID, p.ProjectID, r.ID, r.receiptID).Scan(
		&d.FundingAccountID, &b.ReservationID, &b.ReservationState, &hold, &b.ReleasePending, pq.Array(&b.LedgerEntryIDs))
	if err != nil {
		return err
	}
	b.Reserved, err = usd(hold)
	if err != nil {
		return err
	}
	if b.PlatformCharge != nil || b.Reserved != nil {
		b.Currency = nonempty("USD")
	}
	if b.PlatformCharge != nil {
		b.RoundingRule = nonempty("half_away_from_zero_8")
	}
	d.Billing = b
	return nil
}

func (r detailRow) usage() Usage {
	usage := Usage{Level: "unknown", Source: "unconfirmed", Units: []Unit{}}
	input, output := r.inputTokens, r.outputTokens
	if r.receiptID != nil {
		var evidence struct {
			InputTokens  *int64
			OutputTokens *int64
			Presence     struct {
				InputKnown  bool `json:"input_tokens_known"`
				OutputKnown bool `json:"output_tokens_known"`
			} `json:"presence"`
		}
		// Decode independent fields so corrupt historical counters do not make
		// the rest of a receipt unavailable.
		_ = json.Unmarshal(r.usageJSON, &evidence)
		input = observedTokens(evidence.InputTokens, evidence.Presence.InputKnown, input)
		output = observedTokens(evidence.OutputTokens, evidence.Presence.OutputKnown, output)
		usage.Source = "settlement_receipt"
	} else {
		input = observedTokens(nil, false, input)
		output = observedTokens(nil, false, output)
		if r.usageID != nil {
			usage.Source = "legacy_usage"
		}
	}
	if input != nil {
		usage.Units = append(usage.Units, Unit{Kind: "input_token", Quantity: *input})
	}
	if output != nil {
		usage.Units = append(usage.Units, Unit{Kind: "output_token", Quantity: *output})
	}
	if len(usage.Units) != 0 {
		usage.Level = "observed"
	}
	return usage
}

func observedTokens(receipt *int64, known bool, legacy *int64) *int64 {
	switch {
	case receipt != nil && (*receipt > 0 || (known && *receipt == 0)):
		return receipt
	case legacy != nil && *legacy > 0:
		return legacy
	default:
		return nil
	}
}

func observedMS(value *string) *int64 {
	if value == nil {
		return nil
	}
	ms, err := strconv.ParseInt(*value, 10, 32)
	if err != nil || ms < 0 {
		return nil
	}
	return &ms
}

func utc(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	t := value.UTC()
	return &t
}

func nonempty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func action(protocol *string) *string {
	if protocol == nil {
		return nil
	}
	switch *protocol {
	case "openai_embeddings", "gemini_embedding":
		return nonempty("text.embeddings")
	case "openai_images":
		return nonempty("image.generate")
	case "openai_video":
		return nonempty("video.generate")
	case "openai_chat", "openai_responses", "anthropic_messages", "gemini_generate":
		return nonempty("text.chat")
	default:
		return nil
	}
}

func attemptReason(value *string) *string {
	if value == nil {
		return nil
	}
	switch *value {
	case "first_output_timeout_abandoned", "acceptance_unknown", "canceled", "deadline",
		"attempt_limit", "same_account_retry", "account_switch", "switch_limit":
		return value
	default:
		return nil
	}
}

func decodeConversion(data []byte) *protocol.ConversionPlan {
	if len(data) == 0 {
		return nil
	}
	var plan protocol.ConversionPlan
	if json.Unmarshal(data, &plan) != nil {
		return nil
	}
	switch plan.Mode {
	case protocol.Native, protocol.Bridge, protocol.Rejected:
		plan.Diagnostics = append([]protocol.Diagnostic{}, plan.Diagnostics...)
		return &plan
	default:
		return nil
	}
}

func taskLinks(ctx context.Context, tx *sql.Tx, scope Scope, journalID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT legacy_id FROM gateway_execution_legacy_ids
WHERE workspace_id = $1 AND project_id = $2 AND request_id = $3::uuid AND legacy_kind = 'upstream_job'
ORDER BY legacy_id`, scope.WorkspaceID, scope.ProjectID, journalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
