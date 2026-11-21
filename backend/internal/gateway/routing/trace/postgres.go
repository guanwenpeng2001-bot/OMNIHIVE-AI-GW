package trace

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
)

const maxStoredCandidates = 64

type Postgres struct{ db *sql.DB }

func NewPostgres(db *sql.DB) *Postgres { return &Postgres{db: db} }

// Record stores an execution-scoped projection with trace-local candidate identifiers.
func (p *Postgres) Record(ctx context.Context, execution journal.AdmittedExecution, d DecisionTrace) error {
	if d.Mode != Execution || execution.RequestID == "" {
		return nil
	}
	d = project(d)
	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, `
INSERT INTO gateway_routing_decisions (request_id, snapshot_id, decision)
SELECT request_id, $4, $5::jsonb FROM gateway_executions
WHERE request_id=$1 AND workspace_id=$2 AND project_id=$3
ON CONFLICT DO NOTHING`, execution.RequestID, execution.Scope.WorkspaceID, execution.Scope.ProjectID, d.SnapshotID, string(body))
	return err
}

func project(d DecisionTrace) DecisionTrace {
	d.Candidates = append([]Candidate{}, d.Candidates...)
	if d.Selected != nil {
		selected := *d.Selected
		d.Selected = &selected
	}
	selectedIndex := -1
	for i, candidate := range d.Candidates {
		if d.Selected != nil && candidate.ID == d.Selected.CandidateID {
			selectedIndex = i
			break
		}
	}
	if len(d.Candidates) > maxStoredCandidates {
		// Keep the selected candidate and the first three exclusions even for wide pools.
		keep := make([]Candidate, 0, maxStoredCandidates)
		priority := make(map[int]bool, 4)
		if selectedIndex >= 0 {
			priority[selectedIndex] = true
		}
		exclusions := 0
		for i, candidate := range d.Candidates {
			if candidate.State == "excluded" && exclusions < 3 {
				priority[i] = true
				exclusions++
			}
		}
		available := maxStoredCandidates - len(priority)
		for i, candidate := range d.Candidates {
			if priority[i] {
				keep = append(keep, candidate)
			} else if available > 0 {
				keep = append(keep, candidate)
				available--
			}
		}
		d.Candidates = keep
		d.Uncertainties = append(append([]string{}, d.Uncertainties...), "TRACE_TRUNCATED")
	}
	selectedID := ""
	if d.Selected != nil {
		selectedID = d.Selected.CandidateID
		d.Selected.CandidateID = "candidate-selected"
	}
	for i := range d.Candidates {
		id := "candidate-" + strconv.Itoa(i+1)
		if d.Selected != nil && selectedID == d.Candidates[i].ID {
			d.Selected.CandidateID = id
		}
		d.Candidates[i].ID = id
	}
	return d
}
