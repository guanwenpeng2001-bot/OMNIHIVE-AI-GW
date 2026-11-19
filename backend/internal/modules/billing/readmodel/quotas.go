package readmodel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/timezone"
	"github.com/shopspring/decimal"
)

type QuotaFilter struct {
	Key, Member int64
	Model       string
	At          time.Time
}
type EffectiveQuota struct {
	AsOf     time.Time      `json:"as_of"`
	Funding  *string        `json:"funding_account_id"`
	Rules    []Rule         `json:"rules"`
	Blocked  bool           `json:"blocked"`
	Blocking []string       `json:"blocking_rule_ids"`
	External map[string]any `json:"external_exposure"`
}

const scopedKeys = `WITH keys AS (
 SELECT k.* FROM api_keys k JOIN projects d ON d.workspace_id=k.workspace_id AND d.is_default
 LEFT JOIN project_resources p ON p.kind='api_keys' AND p.resource_id=k.id AND p.workspace_id=k.workspace_id
 LEFT JOIN project_key_policies x ON x.api_key_id=k.id AND x.workspace_id=k.workspace_id
 WHERE k.workspace_id=$1 AND coalesce(x.project_id,p.project_id,d.id)=$2 AND k.user_id=$3 AND k.deleted_at IS NULL
 AND ($4::bigint=0 OR k.id=$4) AND ($5::text='' OR x.model_mode IS NULL OR x.model_mode='inherit' OR (x.model_mode='allow_list' AND $5=ANY(x.models)))
 AND NOT EXISTS(SELECT 1 FROM identity_scope_quarantine q WHERE q.kind='api_keys' AND q.resource_id=k.id)) `

func (s Store) Quotas(ctx context.Context, sc Scope, f QuotaFilter, adjust bool) (EffectiveQuota, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return EffectiveQuota{}, err
	}
	defer tx.Rollback()
	at := f.At
	if at.IsZero() {
		if err = tx.QueryRowContext(ctx, "SELECT now()").Scan(&at); err != nil {
			return EffectiveQuota{}, err
		}
	}
	out := EffectiveQuota{AsOf: at.UTC(), Rules: []Rule{}, Blocking: []string{}, External: map[string]any{"state": "unknown", "risk_limit": nil}}
	payer := f.Member
	if payer == 0 {
		payer = sc.Actor
	}
	if payer != sc.Actor {
		var member bool
		err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND user_id=$2)", sc.Workspace, payer).Scan(&member)
		if err != nil {
			return out, err
		}
		if !member {
			return out, fault.NotFound
		}
	}
	args := []any{sc.Workspace, sc.Project, payer, f.Key, f.Model}
	if f.Key != 0 || f.Model != "" {
		var has bool
		err = tx.QueryRowContext(ctx, scopedKeys+"SELECT EXISTS(SELECT 1 FROM keys)", args...).Scan(&has)
		if err != nil {
			return out, err
		}
		if !has {
			return out, fault.NotFound
		}
	}
	// Wallet balances already reflect wallet-backed holds. Promotional credit
	// is a component of this same wallet and has no separately recorded balance.
	wallet := rule("wallet-"+fmt.Sprint(payer), "wallet", "member", payer)
	err = tx.QueryRowContext(ctx, `SELECT f.id::text,sum(e.amount)::text,max(e.posted_at) FROM billing_funding_accounts f
 LEFT JOIN billing_wallet_events e ON e.funding_account_id=f.id AND e.posted_at<=$2 WHERE f.user_id=$1 AND f.currency='USD' GROUP BY f.id`, payer, at).Scan(&out.Funding, &wallet.Available, &wallet.Observed)
	if err != nil && err != sql.ErrNoRows {
		return out, err
	}
	if wallet.Observed == nil {
		wallet.Freshness = "unknown"
	} else {
		err = tx.QueryRowContext(ctx, `SELECT round(coalesce(sum(hold_amount),0),8)::text FROM api_key_budget_reservations
            WHERE user_id=$1 AND balance_held AND created_at<=$2 AND (status='reserved' OR updated_at>$2)`, payer, at).Scan(&wallet.Reserved)
		if err != nil {
			return out, err
		}
	}
	out.Rules = append(out.Rules, wallet)

	var gifts *string
	err = tx.QueryRowContext(ctx, `SELECT sum(bonus_amount)::text FROM promo_code_usages WHERE user_id=$1 AND used_at<=$2`, payer, at).Scan(&gifts)
	if err != nil {
		return out, err
	}
	if gifts != nil {
		gift := rule("gift-"+fmt.Sprint(payer), "gift", "member", payer)
		gift.Shared = wallet.Shared
		gift.Limit = gifts
		gift.Mode = "informational"
		gift.Observed = ptr(at)
		gift.Freshness = "partial"
		out.Rules = append(out.Rules, gift)
	}

	// EXISTS prevents multiplying one subscription or platform allowance by
	// the number of eligible keys/models sharing it.
	rows, err := tx.QueryContext(ctx, scopedKeys+`
 SELECT 'subscription',s.id,s.starts_at,s.expires_at,s.updated_at,v.period,v.ceiling::text,v.used::text,v.start
 FROM user_subscriptions s JOIN groups g ON g.id=s.group_id AND g.workspace_id=$1 AND g.deleted_at IS NULL
 CROSS JOIN LATERAL(VALUES ('daily',g.daily_limit_usd,s.daily_usage_usd,s.daily_window_start),
 ('weekly',g.weekly_limit_usd,s.weekly_usage_usd,s.weekly_window_start),('monthly',g.monthly_limit_usd,s.monthly_usage_usd,s.monthly_window_start))v(period,ceiling,used,start)
 WHERE s.user_id=$3 AND s.deleted_at IS NULL AND s.status='active' AND s.starts_at<=$6 AND s.expires_at>$6 AND v.ceiling IS NOT NULL
 AND EXISTS(SELECT 1 FROM keys k WHERE k.group_id=s.group_id)
 UNION ALL
 SELECT 'quota',q.id,NULL,NULL,q.updated_at,v.period,v.ceiling::text,v.used::text,v.start
 FROM user_platform_quotas q CROSS JOIN LATERAL(VALUES ('daily',q.daily_limit_usd,q.daily_usage_usd,q.daily_window_start),
 ('weekly',q.weekly_limit_usd,q.weekly_usage_usd,q.weekly_window_start),('monthly',q.monthly_limit_usd,q.monthly_usage_usd,q.monthly_window_start))v(period,ceiling,used,start)
 WHERE q.user_id=$3 AND q.deleted_at IS NULL AND v.ceiling IS NOT NULL AND EXISTS(
 SELECT 1 FROM keys k JOIN groups g ON g.id=k.group_id AND g.workspace_id=$1 WHERE g.platform=q.platform)
 ORDER BY 1,2,6`, append(args, at)...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var kind, period, limit, used string
		var id int64
		var since, until, start *time.Time
		var observed time.Time
		if err = rows.Scan(&kind, &id, &since, &until, &observed, &period, &limit, &used, &start); err != nil {
			rows.Close()
			return out, err
		}
		r := rule(fmt.Sprintf("%s-%d-%s", kind, id, period), kind, "member", payer)
		r.Timezone = timezone.Location().String()
		r.Period = period
		r.Limit = &limit
		r.Observed = &observed
		if observed.After(at) {
			r.Freshness = "unknown"
			r.Limit = nil
			r.Observed = nil
		} else {
			r.Reset = legacyReset(kind, period, start, since, until)
			if r.Reset != nil && !at.Before(*r.Reset) {
				used = "0"
				r.Reset = nextReset(kind, period, *r.Reset, at, until)
			}
			setUsage(&r, used, "0")
		}
		out.Rules = append(out.Rules, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}

	rows, err = tx.QueryContext(ctx, scopedKeys+`SELECT k.id,k.updated_at,r.source,r.period,r.zone,r.ceiling::text,
 r.used::text,r.held::text,r.overrun::text,r.resets_at FROM keys k
 CROSS JOIN LATERAL(VALUES
 ('key_quota','lifetime','UTC',quota,quota_used,0,NULL::numeric,NULL::timestamptz),
 ('key_budget',budget_period_mode,budget_timezone,budget_limit,budget_used,budget_reserved,budget_overrun,budget_period_end),
 ('key_rate_5h','rolling_5h','UTC',rate_limit_5h,usage_5h,0,NULL,window_5h_start+interval '5 hours'),
 ('key_rate_1d','rolling_1d','UTC',rate_limit_1d,usage_1d,0,NULL,window_1d_start+interval '24 hours'),
 ('key_rate_7d','rolling_7d','UTC',rate_limit_7d,usage_7d,0,NULL,window_7d_start+interval '7 days')
 )r(source,period,zone,ceiling,used,held,overrun,resets_at)
 WHERE r.ceiling>0 AND r.period<>'off' ORDER BY k.id,r.source`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var keyID int64
		var updated time.Time
		var source, period, zone, ceiling, used, held string
		var overrun *string
		var reset *time.Time
		if err = rows.Scan(&keyID, &updated, &source, &period, &zone, &ceiling, &used, &held, &overrun, &reset); err != nil {
			rows.Close()
			return out, err
		}
		r := rule(fmt.Sprintf("%s-%d", source, keyID), source, "key", keyID)
		r.Period, r.Timezone, r.Observed, r.Reset, r.Limit = period, zone, &updated, reset, &ceiling
		switch {
		case updated.After(at):
			r.Freshness = "unknown"
			r.Limit = nil
			r.Observed = nil
			r.Reset = nil
		case reset != nil && !at.Before(*reset) && source == "key_budget":
			r.Freshness = "stale"
		case reset != nil && !at.Before(*reset):
			setUsage(&r, "0", "0")
			r.Reset = nil // The next charge opens the new rolling window.
		default:
			setUsage(&r, used, held)
			if overrun != nil {
				r.Overrun = overrun
			}
		}
		out.Rules = append(out.Rules, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	units, err := tx.QueryContext(ctx, `SELECT DISTINCT currency FROM billing_budget_versions WHERE workspace_id=$1 AND project_id=$2 AND effective_at<=$3 ORDER BY currency`, sc.Workspace, sc.Project, at)
	if err != nil {
		return out, err
	}
	var currencies []string
	for units.Next() {
		var unit string
		if err = units.Scan(&unit); err != nil {
			units.Close()
			return out, err
		}
		currencies = append(currencies, unit)
	}
	err = units.Err()
	units.Close()
	if err != nil {
		return out, err
	}
	for _, unit := range currencies {
		r, err := projectRule(ctx, tx, sc, unit, at, adjust)
		if err != nil {
			return out, err
		}
		out.Rules = append(out.Rules, r)
	}
	hasSubscription := false
	for _, r := range out.Rules {
		if r.Source == "subscription" {
			hasSubscription = true
			break
		}
	}
	for _, r := range out.Rules {
		// Without a selected key, key/platform/subscription rules explain each
		// option. Only project-wide rules can block every option in this view.
		selectable := f.Key != 0 || r.Source == "budget"
		// This legacy payer uses USD. Independent budgets in other currencies
		// remain visible and apply only to admission in their own currency.
		if r.Unit == "USD" && r.Mode == "hard" && selectable && r.Available != nil && !positive(*r.Available) && (r.Source != "wallet" || !hasSubscription) {
			out.Blocking = append(out.Blocking, r.ID)
		}
	}
	out.Blocked = len(out.Blocking) > 0
	return out, tx.Commit()
}
func positive(value string) bool {
	d, err := decimal.NewFromString(value)
	return err == nil && d.IsPositive()
}
func setUsage(r *Rule, used, reserved string) {
	ceiling, _ := decimal.NewFromString(*r.Limit)
	u, _ := decimal.NewFromString(used)
	h, _ := decimal.NewFromString(reserved)
	diff := ceiling.Sub(u).Sub(h)
	r.Used, r.Reserved = &used, &reserved
	r.Available = ptr(decimal.Max(diff, decimal.Zero).String())
	r.Overrun = ptr(decimal.Max(diff.Neg(), decimal.Zero).String())
}
func nextReset(source, period string, reset, at time.Time, until *time.Time) *time.Time {
	if period == "daily" {
		reset = timezone.StartOfDay(at).AddDate(0, 0, 1)
	} else if period == "weekly" && source == "quota" {
		reset = timezone.StartOfWeek(at).AddDate(0, 0, 7)
	} else {
		duration := 30 * 24 * time.Hour
		if strings.HasPrefix(period, "week") {
			duration = 7 * 24 * time.Hour
		}
		reset = reset.Add((at.Sub(reset)/duration + 1) * duration)
	}
	if until != nil && reset.After(*until) {
		return until
	}
	return &reset
}
