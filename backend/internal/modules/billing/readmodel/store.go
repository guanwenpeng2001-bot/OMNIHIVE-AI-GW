// Package readmodel projects scope-proven request, receipt and wallet evidence.
package readmodel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

type Store struct{ DB *sql.DB }
type Scope struct{ Workspace, Project, Actor int64 }
type Window struct {
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Timezone string    `json:"timezone"`
	Currency string    `json:"currency"`
}
type Filter struct {
	Window
	Key, Member                                               int64
	Model, Source, State, Group, Request, Settlement, Funding string
}
type Seek struct {
	At time.Time
	ID string
}
type Page struct {
	Items []json.RawMessage `json:"items"`
	AsOf  time.Time         `json:"as_of"`
	Next  *string           `json:"next_cursor"`
}
type Result struct {
	Page
	Last *Seek `json:"-"`
}
type Activity struct {
	Result
	Window
	Totals       json.RawMessage `json:"totals"`
	Completeness json.RawMessage `json:"data_completeness"`
}

func found(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fault.NotFound
	}
	return err
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type predicate struct {
	parts []string
	args  []any
}

func (p *predicate) add(column, op string, value any) {
	p.args = append(p.args, value)
	p.parts = append(p.parts, fmt.Sprintf("%s %s $%d", column, op, len(p.args)))
}
func (p predicate) where() string { return strings.Join(p.parts, " AND ") }
func filters(s Scope, f Filter, at string, cutoff time.Time) predicate {
	var p predicate
	p.add("workspace_id", "=", s.Workspace)
	if s.Project > 0 {
		p.add("project_id", "=", s.Project)
	}
	if f.Currency != "" {
		p.add("currency", "=", f.Currency)
	}
	if !f.From.IsZero() {
		p.add(at, ">=", f.From)
	}
	if !f.To.IsZero() {
		p.add(at, "<", f.To)
	}
	p.add(at, "<=", cutoff)
	if f.Key > 0 {
		p.add("api_key_id", "=", f.Key)
	}
	if f.Member > 0 {
		p.add("member_id", "=", f.Member)
	}
	for _, v := range []struct{ column, value string }{{"model_id", f.Model}, {"billing_source", f.Source}, {"state", f.State}, {"request_id", f.Request}, {"settlement_id::text", f.Settlement}, {"funding_account_id::text", f.Funding}} {
		if v.value != "" {
			p.add(v.column, "=", v.value)
		}
	}
	return p
}

// The page subquery supplies (at,id,item). Every tie is ordered using the C
// collation so the seek comparison and the database sort use the same order.
func page(ctx context.Context, q queryer, source string, args []any, after Seek, limit int, asOf time.Time) (Result, error) {
	result := Result{Page: Page{Items: []json.RawMessage{}, AsOf: asOf}}
	query := "SELECT at,id,item FROM (" + source + ") items"
	if after.ID != "" {
		query += fmt.Sprintf(` WHERE (at,id COLLATE "C") < ($%d,$%d)`, len(args)+1, len(args)+2)
		args = append(args, after.At, after.ID)
	}
	query += fmt.Sprintf(` ORDER BY at DESC,id COLLATE "C" DESC LIMIT $%d`, len(args)+1)
	rows, err := q.QueryContext(ctx, query, append(args, limit+1)...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	var last Seek
	for rows.Next() {
		if len(result.Items) == limit {
			result.Last = &last
			break
		}
		var raw json.RawMessage
		if err = rows.Scan(&last.At, &last.ID, &raw); err != nil {
			return result, err
		}
		result.Items = append(result.Items, raw)
	}
	return result, rows.Err()
}

const ledger = `(SELECT p.*, p.event_id::text AS id,p.settlement_id::text AS effect_id,'charge'::text AS kind,NULL::text AS reverses_id
 FROM billing_scoped_postings p
 UNION ALL
 SELECT a.workspace_id,a.project_id,a.request_id,a.receipt_id,c.api_key_id,c.member_id,c.model_id,c.billing_source,c.state,
 e.id,e.funding_account_id,e.currency,e.amount,e.posted_at,e.id::text,a.id::text,'adjustment',a.original_event_id::text
 FROM billing_adjustment_effects a JOIN billing_wallet_events e ON e.id=a.event_id
 JOIN billing_cost_evidence c ON (c.workspace_id,c.project_id,c.request_id,c.settlement_id)=(a.workspace_id,a.project_id,a.request_id,a.receipt_id)) entries`

func (s Store) List(ctx context.Context, sc Scope, kind string, f Filter, after Seek, limit int, asOf time.Time) (Result, error) {
	var query, at string
	switch kind {
	case "charges":
		at = "at"
		query = `SELECT at,request_id AS id,jsonb_build_object('request_id',request_id,'settlement_id',settlement_id::text,'at',at,
 'model_id',model_id,'billing_source',billing_source,'state',state,'estimate',billing_money(currency,legacy_estimate),'platform_charge',billing_money(currency,charge),'currency',currency,
 'price_version',price_version,'detail_url',format('/api/v2/workspaces/%s/projects/%s/requests/%s',workspace_id,project_id,request_id)) AS item`
	case "ledger":
		at = "posted_at"
		query = `SELECT posted_at AS at,id,jsonb_build_object('id',id,'effect_id',effect_id,'kind',kind,'request_id',request_id,'settlement_id',settlement_id::text,
 'funding_account_id',funding_account_id::text,'signed_amount',billing_money(currency,amount),'posted_at',posted_at,'reverses_id',reverses_id) AS item FROM ` + ledger
	case "reconciliation":
		at = "at"
		query = `SELECT at,id,jsonb_build_object('id',id,'project_id',project_id::text,'request_id',request_id,'settlement_id',settlement_id::text,
 'state',state,'reason',reason,'difference',billing_money(currency,difference),'currency',currency,'version',version::text) AS item FROM billing_cases`
	default:
		return Result{}, fault.Invalid("/list")
	}
	p := filters(sc, f, at, asOf)
	if kind == "charges" {
		p.args = append(p.args, asOf)
		query += fmt.Sprintf(" FROM billing_cost_at($%d)", len(p.args))
	}
	return page(ctx, s.DB, query+" WHERE "+p.where(), p.args, after, limit, asOf)
}

func (s Store) Activity(ctx context.Context, sc Scope, f Filter, after Seek, limit int, asOf time.Time) (Activity, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Activity{}, err
	}
	defer tx.Rollback()
	out := Activity{Window: f.Window}
	p := filters(sc, f, "at", asOf)
	// Currency is required even for empty results. SQL NULLs retain missing
	// measurements. Counts describe the evidence actually available.
	if f.Currency == "" {
		return out, fault.Invalid("/currency")
	}
	p.args = append(p.args, asOf)
	clause := fmt.Sprintf(" FROM billing_cost_at($%d) WHERE ", len(p.args)) + p.where()
	p.args = append(p.args, f.Currency)
	currency := fmt.Sprintf("$%d::text", len(p.args))
	totals := `SELECT jsonb_build_object('logical_requests',count(*)::text,'attempts',sum(attempts)::text,
 'observed_usage_requests',count(*) FILTER(WHERE input_tokens IS NOT NULL OR output_tokens IS NOT NULL)::text,
 'input_tokens',sum(input_tokens)::text,'output_tokens',sum(output_tokens)::text,
 'platform_charge',billing_money(UNIT,sum(charge)),'subscription_consumption',billing_money(UNIT,sum(charge) FILTER(WHERE billing_source='subscription')),
 'reserved',billing_money(UNIT,sum(reserved)),'catalog_reference',NULL,'byok_external_estimate',NULL,'platform_service_fee',NULL,
 'pending_requests',count(*) FILTER(WHERE state='pending')::text,'needs_review_requests',count(*) FILTER(WHERE state='needs_review')::text),
 CASE WHEN count(*) FILTER(WHERE charge IS NULL OR attempts IS NULL OR (input_tokens IS NULL AND output_tokens IS NULL))>0
 THEN '{"state":"partial","missing":[{"section":"billing_or_usage","reason":"EVIDENCE_NOT_RECORDED"}]}'::jsonb
 ELSE '{"state":"complete","missing":[]}'::jsonb END`
	err = tx.QueryRowContext(ctx, strings.ReplaceAll(totals, "UNIT", currency)+clause, p.args...).Scan(&out.Totals, &out.Completeness)
	if err != nil {
		return out, err
	}
	group := map[string]string{"model": "model_id", "member": "member_id::text", "key": "api_key_id::text", "workspace": "workspace_id::text", "project": "project_id::text"}[f.Group]
	if f.Group == "day" {
		p.args = append(p.args, f.Timezone)
		group = fmt.Sprintf("to_char(at AT TIME ZONE $%d,'YYYY-MM-DD')", len(p.args))
	}
	if group == "" {
		return out, fault.Invalid("/group_by")
	}
	filterKey := map[string]string{"model": "model_id", "key": "key_id", "member": "member_id", "project": "project_id", "workspace": "workspace_id", "day": "day"}[f.Group]
	query := `SELECT 'epoch'::timestamptz AS at,coalesce('1'||(` + group + `),'0') AS id,jsonb_build_object('group',` + group + `,
 'logical_requests',count(*)::text,'attempts',sum(attempts)::text,'input_tokens',sum(input_tokens)::text,'output_tokens',sum(output_tokens)::text,
 'platform_charge',billing_money(` + currency + `,sum(charge)),'filter',jsonb_build_object('` + filterKey + `',` + group + `)) AS item` + clause + " GROUP BY " + group
	out.Result, err = page(ctx, tx, query, p.args, after, limit, asOf)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
