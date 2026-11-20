package readmodel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
)

type Budget struct {
	Mode     string      `json:"mode"`
	Limit    money.Money `json:"limit"`
	Timezone string      `json:"timezone"`
	Period   string      `json:"window"`
	External bool        `json:"include_byok_external_estimates"`
}

func bounds(at time.Time, z, period string) (time.Time, time.Time, error) {
	if err := ValidateZone(z); err != nil {
		return time.Time{}, time.Time{}, err
	}
	loc, _ := time.LoadLocation(z)
	t := at.In(loc)
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	var end time.Time
	switch period {
	case "calendar_day":
		end = start.AddDate(0, 0, 1)
	case "calendar_week":
		start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
		end = start.AddDate(0, 0, 7)
	case "calendar_month":
		start = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
		end = start.AddDate(0, 1, 0)
	default:
		return time.Time{}, time.Time{}, fault.Invalid("/window")
	}
	return start.UTC(), end.UTC(), nil
}

type Rule struct {
	ID         string            `json:"id"`
	Version    string            `json:"version"`
	Scope      map[string]string `json:"scope"`
	Owner      string            `json:"owner_id"`
	Shared     string            `json:"shared_allowance_id"`
	Source     string            `json:"source"`
	Mode       string            `json:"mode"`
	Unit       string            `json:"unit"`
	Limit      *string           `json:"limit"`
	Used       *string           `json:"used"`
	Reserved   *string           `json:"reserved"`
	Available  *string           `json:"available"`
	Overrun    *string           `json:"overrun"`
	Period     string            `json:"window"`
	Timezone   string            `json:"timezone"`
	Reset      *time.Time        `json:"resets_at"`
	Freshness  string            `json:"freshness"`
	Observed   *time.Time        `json:"observed_at"`
	AllowExtra bool              `json:"allow_extra_spend"`
	External   bool              `json:"include_byok_external_estimates"`
	Action     *string           `json:"adjust_action"`
	CanAdjust  bool              `json:"can_adjust"`
}

func ptr[T any](v T) *T { return &v }
func rule(id, kind, scope string, owner int64) Rule {
	return Rule{ID: id, Shared: id, Source: kind, Owner: fmt.Sprint(owner), Scope: map[string]string{"type": scope, "id": fmt.Sprint(owner)}, Mode: "hard", Unit: "USD", Freshness: "fresh", Version: "1", Period: "lifetime", Timezone: "UTC"}
}

func projectRule(ctx context.Context, q queryer, sc Scope, currency string, at time.Time, adjust bool) (Rule, error) {
	r := rule("project-"+currency, "budget", "project", sc.Project)
	r.Owner = fmt.Sprint(sc.Workspace)
	r.Unit = currency
	r.CanAdjust = adjust
	r.Action = ptr("billing.budgets.write")
	err := q.QueryRowContext(ctx, `SELECT ceiling::text,mode,period,timezone,include_external,version::text,effective_at FROM billing_budget_versions
 WHERE workspace_id=$1 AND project_id=$2 AND currency=$3 AND effective_at<=$4 ORDER BY version DESC LIMIT 1`, sc.Workspace, sc.Project, currency, at).Scan(&r.Limit, &r.Mode, &r.Period, &r.Timezone, &r.External, &r.Version, &r.Observed)
	if err != nil {
		return r, err
	}
	start, end, err := bounds(at, r.Timezone, r.Period)
	if err != nil {
		return r, err
	}
	r.Reset = &end
	r.AllowExtra = r.Mode == "soft"
	err = q.QueryRowContext(ctx, `WITH used AS (
 SELECT coalesce(sum(CASE WHEN c.pricing_state='priced' AND rc.billing_command->'charged_amount'->>'currency'=c.currency
   THEN (rc.billing_command->'charged_amount'->>'amount')::numeric ELSE c.charge END),0) AS u
 FROM billing_cost_evidence c
 LEFT JOIN usage_settlement_receipts rc ON rc.id=c.settlement_id
 LEFT JOIN billing_project_holds h ON (h.workspace_id,h.project_id,h.currency,h.request_id)=(c.workspace_id,c.project_id,c.currency,coalesce(rc.billing_command->>'project_reservation_ref',c.request_id))
 WHERE c.workspace_id=$1 AND c.project_id=$2 AND c.currency=$3 AND coalesce(h.admitted_at,c.at)>=$4
 AND coalesce(h.admitted_at,c.at)<$5 AND c.settled_at<=$6),
 held AS (
 SELECT coalesce(sum(h.amount),0) AS h FROM billing_project_holds h
 WHERE workspace_id=$1 AND project_id=$2 AND currency=$3 AND admitted_at>=$4 AND admitted_at<$5 AND admitted_at<=$6
 AND (released_at IS NULL OR released_at>$6) AND NOT EXISTS (
 SELECT 1 FROM billing_cost_evidence c LEFT JOIN usage_settlement_receipts rc ON rc.id=c.settlement_id
 WHERE (c.workspace_id,c.project_id,c.currency,coalesce(rc.billing_command->>'project_reservation_ref',c.request_id))=(h.workspace_id,h.project_id,h.currency,h.request_id)
 AND c.settled_at<=$6))
 SELECT round(u,8)::text,round(h,8)::text,round(greatest($7::numeric-u-h,0),8)::text,round(greatest(u+h-$7::numeric,0),8)::text FROM used,held`,
		sc.Workspace, sc.Project, currency, start, end, at, *r.Limit).Scan(&r.Used, &r.Reserved, &r.Available, &r.Overrun)
	return r, err
}
func (s Store) UpdateBudget(ctx context.Context, sc Scope, id, key string, expected int64, b Budget) (Command, error) {
	if b.Limit.Currency() == "" {
		return Command{}, fault.Invalid("/limit")
	}
	if id != "project-"+string(b.Limit.Currency()) {
		return Command{}, fault.Invalid("/id")
	}
	if b.Mode != "soft" && b.Mode != "hard" || b.Limit.Amount().Compare(money.Amount{}) < 0 {
		return Command{}, fault.Invalid("/limit")
	}
	if _, _, err := bounds(time.Now(), b.Timezone, b.Period); err != nil {
		return Command{}, err
	}
	return s.command(ctx, sc, "budgets/"+id, key, []any{expected, b}, func(ctx context.Context, tx *sql.Tx, c Command) (any, error) {
		// Serialize first creation as well as changes to an existing currency rule.
		var p int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, sc.Workspace, sc.Project).Scan(&p)
		if err != nil {
			return nil, found(err)
		}
		var curr int64
		err = tx.QueryRowContext(ctx, `SELECT version FROM billing_project_budgets WHERE workspace_id=$1 AND project_id=$2 AND currency=$3 FOR UPDATE`, sc.Workspace, sc.Project, b.Limit.Currency()).Scan(&curr)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if expected != curr {
			return nil, fault.Stale(curr)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO billing_project_budgets(workspace_id,project_id,currency,mode,ceiling,period,timezone,include_external,version)
   VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(workspace_id,project_id,currency) DO UPDATE SET
   mode=excluded.mode,ceiling=excluded.ceiling,period=excluded.period,timezone=excluded.timezone,include_external=excluded.include_external,version=excluded.version`, sc.Workspace, sc.Project, b.Limit.Currency(), b.Mode, b.Limit.String(), b.Period, b.Timezone, b.External, curr+1)
		if err != nil {
			return nil, err
		}
		var now time.Time
		if err = tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			return nil, err
		}
		return projectRule(ctx, tx, sc, string(b.Limit.Currency()), now, true)
	})
}

var Exhausted = &fault.Fault{Status: 402, Reason: "BUDGET_EXHAUSTED", Message: "The project budget is exhausted."}

// Reserve runs in the admission transaction before dispatch. The caller uses
// the same request ID for the journal and this hold. Receipt settlement then
// supersedes the hold. Release an unused hold on a known no-dispatch outcome.
func Reserve(ctx context.Context, tx *sql.Tx, sc Scope, request string, amount money.Money) error {
	_, err := reserve(ctx, tx, sc, request, amount.Currency(), &amount)
	return err
}

func reserve(ctx context.Context, tx *sql.Tx, sc Scope, request string, currency money.Currency, amount *money.Money) (bool, error) {
	if request == "" || (amount != nil && (amount.Currency() != currency || amount.Amount().Compare(money.Amount{}) < 0)) {
		return false, fault.Invalid("/reservation")
	}
	var mode string
	err := tx.QueryRowContext(ctx, `SELECT mode FROM billing_project_budgets WHERE workspace_id=$1 AND project_id=$2 AND currency=$3 FOR UPDATE`, sc.Workspace, sc.Project, currency).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// An unknown tariff cannot establish a bound on hard-budget spending.
	if amount == nil {
		if mode == "hard" {
			return false, errors.New("project budget estimate unavailable")
		}
		return false, nil
	}
	var same, active bool
	err = tx.QueryRowContext(ctx, `SELECT amount=$5,released_at IS NULL FROM billing_project_holds WHERE workspace_id=$1 AND project_id=$2 AND currency=$3 AND request_id=$4`, sc.Workspace, sc.Project, currency, request, amount.String()).Scan(&same, &active)
	if err == nil {
		if same && active {
			return true, nil
		}
		return false, fault.Conflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var at time.Time
	if err = tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&at); err != nil {
		return false, err
	}
	r, err := projectRule(ctx, tx, sc, string(currency), at, false)
	if err != nil {
		return false, err
	}
	if r.Mode == "hard" {
		available, err := money.NewMoney(currency, *r.Available)
		if err != nil {
			return false, err
		}
		if amount.Amount().Compare(available.Amount()) > 0 {
			return false, Exhausted
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO billing_project_holds(workspace_id,project_id,currency,request_id,amount,admitted_at) VALUES($1,$2,$3,$4,$5,$6)`, sc.Workspace, sc.Project, currency, request, amount.String(), at)
	return err == nil, err
}

const releaseProjectHoldSQL = `UPDATE billing_project_holds SET released_at=clock_timestamp()
 WHERE workspace_id=$1 AND project_id=$2 AND currency=$3 AND request_id=$4 AND released_at IS NULL`

func Release(ctx context.Context, tx *sql.Tx, sc Scope, request string, currency money.Currency) error {
	_, err := tx.ExecContext(ctx, releaseProjectHoldSQL, sc.Workspace, sc.Project, currency, request)
	return err
}

// ReserveProjectBudget returns whether the project has a matching budget hold.
func (s Store) ReserveProjectBudget(ctx context.Context, owner scope.Authorized, request string, amount *money.Money) (bool, error) {
	if s.DB == nil {
		return false, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	held, err := reserve(ctx, tx, Scope{Workspace: owner.WorkspaceID, Project: owner.ProjectID}, request, "USD", amount)
	if err != nil {
		return false, err
	}
	return held, tx.Commit()
}

func (s Store) ReleaseProjectBudget(ctx context.Context, owner scope.Authorized, request string) error {
	if s.DB == nil {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, releaseProjectHoldSQL, owner.WorkspaceID, owner.ProjectID, "USD", request)
	return err
}
