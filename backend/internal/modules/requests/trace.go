package requests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
)

func (s *Store) Explain(ctx context.Context, p Projection, id string) (Explanation, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Explanation{}, err
	}
	defer tx.Rollback()
	var journalID *string
	err = tx.QueryRowContext(ctx, `
SELECT journal_id::text FROM request_anchors WHERE workspace_id = $1 AND project_id = $2 AND id = $3`,
		p.WorkspaceID, p.ProjectID, id).Scan(&journalID)
	if err != nil {
		return Explanation{}, notFound(err)
	}
	out, err := explain(ctx, tx, p, id, journalID)
	if err != nil {
		return Explanation{}, err
	}
	return out, tx.Commit()
}

func explain(ctx context.Context, tx *sql.Tx, p Projection, id string, journalID *string) (Explanation, error) {
	out := Explanation{RequestID: id, DataCompleteness: complete()}
	if journalID == nil {
		out.DataCompleteness.missing("route", "LEGACY_NOT_RECORDED")
		return out, nil
	}
	var body []byte
	err := tx.QueryRowContext(ctx, `
SELECT d.decision
FROM gateway_executions e JOIN gateway_routing_decisions d USING (request_id)
WHERE e.workspace_id = $1 AND e.project_id = $2 AND e.request_id = $3::uuid
ORDER BY d.recorded_at DESC, d.snapshot_id DESC LIMIT 1`, p.WorkspaceID, p.ProjectID, *journalID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		out.DataCompleteness.missing("route", "TRACE_NOT_RECORDED")
		return out, nil
	}
	if err != nil {
		return Explanation{}, err
	}
	var decision trace.DecisionTrace
	if json.Unmarshal(body, &decision) != nil || decision.Mode != trace.Execution || decision.SchemaVersion != 1 {
		out.DataCompleteness.missing("route", "TRACE_VERSION_UNSUPPORTED")
		return out, nil
	}
	projectTrace(p, &decision)
	out.Trace = &decision
	if slices.Contains(decision.Uncertainties, "TRACE_TRUNCATED") {
		out.DataCompleteness.missing("route.candidates", "TRACE_TRUNCATED")
	}
	return out, nil
}

func projectTrace(p Projection, d *trace.DecisionTrace) {
	d.Origins = append([]trace.Origin{}, d.Origins...)
	d.Stages = append([]trace.Stage{}, d.Stages...)
	d.Candidates = append([]trace.Candidate{}, d.Candidates...)
	d.Uncertainties = append([]string{}, d.Uncertainties...)
	d.Redactions = append([]string{}, d.Redactions...)
	if !p.Connections && !slices.Contains(d.Redactions, "connection_identity") {
		d.Redactions = append(d.Redactions, "connection_identity")
	}
	if !p.Finance {
		d.PriceVersion = ""
		d.Redactions = append(d.Redactions, "financial_details")
	}
	selected := ""
	if d.Selected != nil {
		selected = d.Selected.CandidateID
		d.Selected.CandidateID = "candidate-selected"
		if !p.Finance {
			d.Selected.FundingSource = ""
		}
	}
	for i := range d.Candidates {
		c := &d.Candidates[i]
		id := "candidate-" + strconv.Itoa(i+1)
		if d.Selected != nil && c.ID == selected {
			d.Selected.CandidateID = id
		}
		c.ID = id
		c.Reasons = append([]trace.Reason{}, c.Reasons...)
		c.Factors = append([]trace.Factor{}, c.Factors...)
		if !p.Finance {
			c.Factors = slices.DeleteFunc(c.Factors, func(f trace.Factor) bool {
				switch f.Name {
				case "priority", "load", "queue", "error_rate", "ttft", "ttft_p50", "ttft_p95", "throughput":
					return f.Unit != "score" && f.Unit != "ms" && f.Unit != "tokens/s"
				default:
					return true
				}
			})
		}
	}
}
