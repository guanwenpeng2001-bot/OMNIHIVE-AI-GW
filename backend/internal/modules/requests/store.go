package requests

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/lib/pq"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) List(ctx context.Context, p Projection, page Page) ([]Summary, error) {
	if page.Limit < 1 || page.Limit > 100 || page.AsOf.IsZero() {
		return nil, fault.Invalid("/limit")
	}
	if len(page.FundingSources) != 0 && !p.Finance {
		return nil, fault.Forbidden
	}
	var after *time.Time
	if page.After.ID != "" {
		after = &page.After.StartedAt
	}
	rows, err := s.db.QueryContext(ctx, anchors+`
	  AND ($5::timestamptz IS NULL OR started_at >= $5)
	  AND ($6::timestamptz IS NULL OR started_at < $6)
	  AND ($7::timestamptz IS NULL OR (started_at, id) < ($7, $8))
	  AND ($9::bigint[] IS NULL OR api_key_id = ANY($9))`+observations+`
	SELECT `+summaryColumns+` FROM projected
	WHERE ($10::text[] IS NULL OR requested_model = ANY($10))
	  AND ($11::text[] IS NULL OR protocol = ANY($11))
	  AND ($12::text[] IS NULL OR outcome = ANY($12))
	  AND ($13::text[] IS NULL OR settlement_state = ANY($13))
	  AND ($14::text[] IS NULL OR funding_source = ANY($14))
	  AND ($15::text[] IS NULL OR reason = ANY($15))
	ORDER BY started_at DESC, id DESC LIMIT $16`,
		p.WorkspaceID, p.ProjectID, page.AsOf, p.Finance, postgresBound(page.From), postgresBound(page.To), after, page.After.ID,
		pq.Array(page.KeyIDs), pq.Array(page.Models), pq.Array(page.Protocols), pq.Array(page.Outcomes),
		pq.Array(page.Settlement), pq.Array(page.FundingSources), pq.Array(page.Reasons), page.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Summary, 0, page.Limit+1)
	for rows.Next() {
		var row summaryRow
		if err := rows.Scan(row.targets()...); err != nil {
			return nil, err
		}
		item, err := row.project(p)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type summaryRow struct {
	Summary
	modern bool
	charge *string
}

func (r *summaryRow) targets() []any {
	return []any{&r.ID, &r.StartedAt, &r.modern, &r.RequestedModel, &r.Protocol, &r.Outcome, &r.Reason,
		&r.SettlementState, &r.charge, &r.AttemptCount}
}

func (r *summaryRow) project(p Projection) (Summary, error) {
	item := r.Summary
	item.StartedAt = item.StartedAt.UTC()
	item.StartSource = "admission"
	if !r.modern {
		item.StartSource = "legacy_observation"
	}
	item.DetailURL = p.RequestURL(item.ID)
	item.Redactions = []string{}
	if !p.Finance {
		item.Redactions = append(item.Redactions, "financial_details")
		return item, nil
	}
	var err error
	item.ChargedAmount, err = usd(r.charge)
	return item, err
}

func usd(value *string) (*money.Money, error) {
	if value == nil {
		return nil, nil
	}
	amount, err := money.NewMoney("USD", *value)
	if err != nil {
		return nil, err
	}
	return &amount, nil
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fault.NotFound
	}
	return err
}

// Stored instants have microsecond precision. Ceiling both bounds preserves
// an inclusive lower bound and an exclusive upper bound for finer inputs.
func postgresBound(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	at := value.Truncate(time.Microsecond)
	if at.Before(*value) {
		at = at.Add(time.Microsecond)
	}
	return &at
}
