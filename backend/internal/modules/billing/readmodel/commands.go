package readmodel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
)

type Command struct {
	ID, Route string
	Scope
	Result  json.RawMessage
	Expires time.Time
}
type commandEffect func(context.Context, *sql.Tx, Command) (any, error)

// Claim, mutate, and store the successful response in one transaction. A
// conflicting INSERT waits for the winning transaction before reading it.
func (s Store) command(ctx context.Context, scope Scope, route, key string, input any, effect commandEffect) (Command, error) {
	var cmd Command
	if strings.TrimSpace(key) == "" || len(key) > 200 {
		return cmd, fault.Invalid("/Idempotency-Key")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return cmd, err
	}
	fp, hash := sha256.Sum256(body), sha256.Sum256([]byte(key))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return cmd, err
	}
	defer tx.Rollback()
	cmd.Scope, cmd.ID, cmd.Route = scope, uuid.NewString(), route
	err = tx.QueryRowContext(ctx, `INSERT INTO billing_commands(id,workspace_id,project_id,actor_id,route,key_hash,fingerprint)
 VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING RETURNING expires_at`, cmd.ID, scope.Workspace, scope.Project, scope.Actor, route, hash[:], fp[:]).Scan(&cmd.Expires)
	if errors.Is(err, sql.ErrNoRows) {
		var same bool
		err = tx.QueryRowContext(ctx, `SELECT id,expires_at,result,fingerprint=$6 FROM billing_commands
  WHERE workspace_id=$1 AND project_id=$2 AND actor_id=$3 AND route=$4 AND key_hash=$5`, scope.Workspace, scope.Project, scope.Actor, route, hash[:], fp[:]).Scan(&cmd.ID, &cmd.Expires, &cmd.Result, &same)
		if err == nil && !same {
			return cmd, fault.Conflict
		}
	} else if err == nil {
		var out any
		out, err = effect(ctx, tx, cmd)
		if err != nil {
			return cmd, err
		}
		cmd.Result, err = json.Marshal(out)
		if err != nil {
			return cmd, err
		}
		_, err = tx.ExecContext(ctx, "UPDATE billing_commands SET result=$2::jsonb WHERE id=$1", cmd.ID, string(cmd.Result))
	}
	if err != nil {
		return cmd, err
	}
	return cmd, tx.Commit()
}

func (s Store) Operation(ctx context.Context, w, actor int64, id string) (Command, error) {
	var c Command
	c.ID, c.Workspace, c.Actor = id, w, actor
	err := s.DB.QueryRowContext(ctx, `SELECT project_id,route,result,expires_at FROM billing_commands WHERE workspace_id=$1 AND actor_id=$2 AND id=$3 AND (route='exports' OR route LIKE 'resolve/%')`, w, actor, id).Scan(&c.Project, &c.Route, &c.Result, &c.Expires)
	return c, found(err)
}
func (s Store) Export(ctx context.Context, sc Scope, key string, w Window) (Command, error) {
	if err := w.Validate(); err != nil {
		return Command{}, err
	}
	return s.command(ctx, sc, "exports", key, w, func(ctx context.Context, tx *sql.Tx, c Command) (any, error) {
		_, err := tx.ExecContext(ctx, `INSERT INTO billing_export_rows(export_id,at,request_id,settlement_id,model_id,currency,billing_source,state,charge,price_version,legacy_estimate)
  SELECT $1,at,request_id,settlement_id,model_id,currency,billing_source,state,charge,price_version,legacy_estimate
  FROM billing_cost_at(now()) WHERE workspace_id=$2 AND project_id=$3 AND at>=$4 AND at<$5 AND currency=$6`, c.ID, sc.Workspace, sc.Project, w.From, w.To, w.Currency)
		return map[string]any{"download_url": fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/billing/exports/%s/download", sc.Workspace, sc.Project, c.ID), "expires_at": c.Expires}, err
	})
}

var Expired = &fault.Fault{Status: 410, Reason: "ARTIFACT_EXPIRED", Message: "The export has expired."}

func (s Store) Download(ctx context.Context, sc Scope, id string, w io.Writer) error {
	c, err := s.Operation(ctx, sc.Workspace, sc.Actor, id)
	if err != nil {
		return err
	}
	if c.Project != sc.Project || c.Route != "exports" {
		return fault.NotFound
	}
	if !time.Now().Before(c.Expires) {
		return Expired
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT at,request_id,coalesce(settlement_id::text,''),coalesce(model_id,''),currency,billing_source,state,coalesce(charge::text,''),coalesce(price_version,''),coalesce(legacy_estimate::text,'')
 FROM billing_export_rows WHERE export_id=$1 ORDER BY at DESC,request_id DESC`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	csvw := csv.NewWriter(w)
	if err = csvw.Write([]string{"at", "request_id", "settlement_id", "model_id", "currency", "billing_source", "state", "platform_charge", "price_version", "estimate"}); err != nil {
		return err
	}
	for rows.Next() {
		var at time.Time
		var record [10]string
		if err = rows.Scan(&at, &record[1], &record[2], &record[3], &record[4], &record[5], &record[6], &record[7], &record[8], &record[9]); err != nil {
			return err
		}
		record[0] = at.UTC().Format(time.RFC3339Nano)
		// Model and price version are free text and may be opened by spreadsheet applications.
		for _, idx := range []int{3, 8} {
			if strings.ContainsAny(firstCharacter(strings.TrimSpace(record[idx])), "=+-@") || strings.HasPrefix(record[idx], "\t") || strings.HasPrefix(record[idx], "\r") {
				record[idx] = "'" + record[idx]
			}
		}
		if err = csvw.Write(record[:]); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	csvw.Flush()
	return csvw.Error()
}
func firstCharacter(s string) string {
	if s == "" {
		return ""
	}
	return s[:1]
}

type Adjustment struct {
	Entry  string      `json:"ledger_entry_id"`
	Amount money.Money `json:"amount"`
	Reason string      `json:"reason"`
	Note   string      `json:"note"`
}

func (s Store) Adjust(ctx context.Context, sc Scope, key string, a Adjustment) (Command, error) {
	if a.Amount.Amount().Compare(money.Amount{}) == 0 || a.Reason != "billing_correction" || len(a.Note) > 1024 {
		return Command{}, fault.Invalid("/adjustment")
	}
	return s.command(ctx, sc, "adjustments", key, a, func(ctx context.Context, tx *sql.Tx, c Command) (any, error) {
		var payer, receipt, original, funding int64
		var request, currency string
		err := tx.QueryRowContext(ctx, `SELECT f.user_id,p.settlement_id,p.event_id,p.funding_account_id,p.request_id,p.currency
    FROM billing_scoped_postings p JOIN billing_funding_accounts f ON f.id=p.funding_account_id
    WHERE p.workspace_id=$1 AND p.project_id=$2 AND p.event_id::text=$3`, sc.Workspace, sc.Project, a.Entry).Scan(&payer, &receipt, &original, &funding, &request, &currency)
		if err != nil {
			return nil, found(err)
		}
		if string(a.Amount.Currency()) != currency || currency != "USD" {
			return nil, fault.Invalid("/amount/currency")
		}
		// The BE-11 wallet trigger writes the balanced posting in this transaction.
		var event int64
		res, err := tx.ExecContext(ctx, `UPDATE users SET balance=balance+$2::numeric,updated_at=now()
   WHERE id=$1 AND deleted_at IS NULL AND balance+$2::numeric>=0 AND abs(balance+$2::numeric)<1000000000000`, payer, a.Amount.String())
		if err != nil {
			return nil, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, &fault.Fault{Status: 409, Reason: "BALANCE_OUT_OF_RANGE", Message: "The adjustment exceeds the wallet bounds."}
		}
		err = tx.QueryRowContext(ctx, `SELECT id FROM billing_wallet_events WHERE funding_account_id=$1 AND transaction_id=pg_current_xact_id() AND kind='balance_change'`, funding).Scan(&event)
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO billing_adjustment_effects(id,workspace_id,project_id,request_id,receipt_id,original_event_id,event_id,currency,reason,note)
    VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, c.ID, sc.Workspace, sc.Project, request, receipt, original, event, currency, a.Reason, a.Note)
		return map[string]string{"id": c.ID, "ledger_entry_id": fmt.Sprint(event), "reverses_id": a.Entry, "state": "posted"}, err
	})
}

type Resolution struct {
	Action   string `json:"action"`
	Evidence string `json:"evidence_id"`
	Note     string `json:"note"`
	Version  int64  `json:"-"`
}

func (s Store) CaseProject(ctx context.Context, w int64, id string) (int64, error) {
	var p int64
	err := s.DB.QueryRowContext(ctx, "SELECT project_id FROM billing_cases WHERE workspace_id=$1 AND id=$2", w, id).Scan(&p)
	return p, found(err)
}
func (s Store) Resolve(ctx context.Context, sc Scope, id, key string, input Resolution) (Command, error) {
	if input.Action != "attach_evidence" || strings.TrimSpace(input.Evidence) == "" || len(input.Evidence) > 255 || len(input.Note) > 1024 {
		return Command{}, fault.Invalid("/evidence_id")
	}
	return s.command(ctx, sc, "resolve/"+id, key, []any{input.Version, input}, func(ctx context.Context, tx *sql.Tx, c Command) (any, error) {
		var current int64
		if err := tx.QueryRowContext(ctx, "SELECT version FROM billing_cases WHERE workspace_id=$1 AND project_id=$2 AND id=$3", sc.Workspace, sc.Project, id).Scan(&current); err != nil {
			return nil, found(err)
		}
		if input.Version != current || current != 1 {
			return nil, fault.Stale(current)
		}
		var receipt *string
		var original json.RawMessage
		err := tx.QueryRowContext(ctx, `INSERT INTO billing_case_resolutions(id,workspace_id,project_id,case_id,request_id,at,currency,reason,difference,receipt_id,evidence_id,note)
  SELECT $1,workspace_id,project_id,id,request_id,at,currency,reason,difference,settlement_id,$5,$6 FROM billing_discrepancies
  WHERE workspace_id=$2 AND project_id=$3 AND id=$4 ON CONFLICT DO NOTHING RETURNING receipt_id::text,
  jsonb_build_object('id',case_id,'request_id',request_id,'reason',reason,'difference',billing_money(currency,difference),'currency',currency,'state','needs_review','version','1')`, c.ID, sc.Workspace, sc.Project, id, input.Evidence, input.Note).Scan(&receipt, &original)
		if errors.Is(err, sql.ErrNoRows) {
			var version int64
			err = tx.QueryRowContext(ctx, `SELECT version FROM billing_cases WHERE workspace_id=$1 AND project_id=$2 AND id=$3`, sc.Workspace, sc.Project, id).Scan(&version)
			if err != nil {
				return nil, found(err)
			}
			return nil, fault.Stale(version)
		}
		return map[string]any{"case_id": id, "original_case": original, "settlement_id": receipt, "evidence_id": input.Evidence, "audit_reference": c.ID, "state": "resolved"}, err
	})
}
func (w Window) Validate() error {
	if w.From.IsZero() || w.To.IsZero() || !w.From.Before(w.To) {
		return fault.Invalid("/from")
	}
	if err := ValidateZone(w.Timezone); err != nil {
		return err
	}
	if _, err := money.NewMoney(money.Currency(w.Currency), "0"); err != nil {
		return fault.Invalid("/currency")
	}
	return nil
}
func ValidateZone(z string) error {
	if z == "" || z == "Local" || len(z) > 100 {
		return fault.Invalid("/timezone")
	}
	if _, err := time.LoadLocation(z); err != nil {
		return fault.Invalid("/timezone")
	}
	return nil
}
