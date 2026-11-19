// Package retirement inventories the legacy contracts and their deployment evidence.
package retirement

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/csv"
	"fmt"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/migration"
)

//go:embed catalog.tsv
var catalog string

// Domains are in review order. Changes within a domain still require its owner's approval.
var domains = []string{"control", "identity", "credentials", "catalog", "execution", "finance", "plugins", "operations", "runtime"}

type Contract struct {
	ID          string `json:"id"`
	Domain      string `json:"domain"`
	Kind        string `json:"kind"`
	Owner       string `json:"owner"`
	Legacy      string `json:"legacy"`
	Replacement string `json:"replacement"`
	ExitGate    string `json:"exit_gate"`
	Observation
	RemainingCallers *int64 `json:"remaining_supported_callers"`
	State            string `json:"state"`
	Reason           string `json:"reason"`
}

// A complete census covers all supported deployments, including offline consumers.
// Evidence references identify records in the operator's restricted review system.
type Observation struct {
	EvidenceID       *string    `json:"evidence_id"`
	ObservedAt       *time.Time `json:"observed_at"`
	DeployedVersions []string   `json:"deployed_versions"`
	ObservedCallers  *int64     `json:"observed_callers"`
	Complete         bool       `json:"complete_census"`
	CoverageFrom     *time.Time `json:"coverage_from"`
	ValidUntil       *time.Time `json:"valid_until"`
	NoticeID         *string    `json:"notice_id"`
	WindowStart      *time.Time `json:"window_start"`
	WindowEnd        *time.Time `json:"window_end"`
	MinimumBinary    *string    `json:"minimum_binary"`
	MinimumSchema    *int       `json:"minimum_schema"`
	ReconciliationID *string    `json:"reconciliation_id"`
	RecoveryDrillID  *string    `json:"recovery_drill_id"`
}

type Report struct {
	DryRun            bool              `json:"dry_run"`
	RegisterAvailable bool              `json:"register_available"`
	Migration         migration.Report  `json:"migration_status"`
	Counts            map[string]*int64 `json:"storage_counts"`
	Entries           []Contract        `json:"contracts"`
}

func Scan(ctx context.Context, app, restore *sql.DB) (Report, error) {
	// BE-23 Status includes the inventory counters and reads a persisted checkpoint.
	status, err := migration.NewReader(app, restore).Status(ctx)
	if err != nil {
		return Report{}, err
	}
	tx, err := app.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL search_path = pg_catalog; SET LOCAL row_security = off; SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return Report{}, err
	}
	observations, available, err := readObservations(ctx, tx)
	if err != nil {
		return Report{}, err
	}
	counts, err := readCounts(ctx, tx)
	if err != nil {
		return Report{}, err
	}
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp()`).Scan(&now); err != nil {
		return Report{}, err
	}
	entries, err := contracts(observations, now)
	if err != nil {
		return Report{}, err
	}
	if err := tx.Commit(); err != nil {
		return Report{}, err
	}
	return Report{DryRun: true, RegisterAvailable: available, Migration: status, Counts: counts, Entries: entries}, nil
}

func contracts(observations map[string]Observation, now time.Time) ([]Contract, error) {
	r := csv.NewReader(strings.NewReader(catalog))
	r.Comma = '\t'
	r.FieldsPerRecord = 7
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read retirement catalog: %w", err)
	}
	entries := make([]Contract, 0, len(rows)-1)
	for _, v := range rows[1:] {
		c := Contract{ID: v[0], Domain: v[1], Kind: v[2], Owner: v[3], Legacy: v[4], Replacement: v[5], ExitGate: v[6], Observation: observations[v[0]]}
		c.assess(now)
		entries = append(entries, c)
	}
	return entries, nil
}

func (c *Contract) assess(now time.Time) {
	current := c.Complete && c.EvidenceID != nil && len(c.DeployedVersions) > 0 &&
		c.ObservedAt != nil && !c.ObservedAt.After(now) && c.ValidUntil != nil && c.ValidUntil.After(now)
	c.RemainingCallers = nil
	if current {
		c.RemainingCallers = c.ObservedCallers
	}
	c.State = "hold"
	switch {
	case c.Kind == "preserve":
		c.State, c.Reason = "preserve", "retained_contract"
	case c.Kind == "baseline":
		c.State, c.Reason = "baseline", "already_unregistered_before_319"
	case c.Kind != "projection":
		c.Reason = "owner_migration_required"
	case c.RemainingCallers == nil:
		c.Reason = "unknown_or_stale_census"
	case *c.RemainingCallers != 0:
		c.Reason = "supported_callers_remain"
	case c.NoticeID == nil || c.WindowStart == nil || c.WindowEnd == nil ||
		!c.WindowEnd.After(*c.WindowStart) || c.ObservedAt.Before(*c.WindowEnd) ||
		c.CoverageFrom == nil || c.CoverageFrom.After(*c.WindowStart):
		c.Reason = "published_window_unverified"
	case c.MinimumBinary == nil || c.MinimumSchema == nil || c.ReconciliationID == nil || c.RecoveryDrillID == nil:
		c.Reason = "reconciliation_or_rollback_floor_unverified"
	default:
		c.State, c.Reason = "review", "verify_owner_gate_and_release_evidence"
	}
}

type DomainPlan struct {
	Domain   string   `json:"domain"`
	Review   []string `json:"review"`
	Hold     []string `json:"hold"`
	Preserve []string `json:"preserve"`
	Baseline []string `json:"baseline"`
}

type Plan struct {
	DryRun  bool         `json:"dry_run"`
	Domains []DomainPlan `json:"domain_order"`
}

func (r Report) Plan() Plan {
	plan := Plan{DryRun: true}
	for _, domain := range domains {
		d := DomainPlan{Domain: domain, Review: []string{}, Hold: []string{}, Preserve: []string{}, Baseline: []string{}}
		for _, c := range r.Entries {
			if c.Domain != domain {
				continue
			}
			switch c.State {
			case "review":
				d.Review = append(d.Review, c.ID)
			case "preserve":
				d.Preserve = append(d.Preserve, c.ID)
			case "baseline":
				d.Baseline = append(d.Baseline, c.ID)
			default:
				d.Hold = append(d.Hold, c.ID)
			}
		}
		plan.Domains = append(plan.Domains, d)
	}
	return plan
}
