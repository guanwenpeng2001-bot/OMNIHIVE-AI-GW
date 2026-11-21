// Package mediajobs binds implemented DashScope actions to operator-verified endpoints.
package mediajobs

import (
	"context"
	"database/sql"
	"errors"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks/media"
)

func Verified(ctx context.Context, db *sql.DB, workspace, project, group, account int64, p media.Profile, rate money.Rate) (media.Proof, error) {
	var v media.Proof
	expected, ok := media.Lookup(p.Kind, p.Model)
	if !ok || p != expected || rate.Currency() != "USD" || string(rate.Unit()) != p.Unit {
		return v, media.Unsupported
	}
	cell := domain.PlatformAction(domain.ActionIdentity{Platform: domain.PlatformQianwen, HostedAccountType: domain.AccountTypeAPIKey}, p.Action)
	if cell.Status != domain.ActionImplemented || cell.Evidence == "" || cell.UsagePolicy == "" {
		return v, media.Unsupported
	}
	err := db.QueryRowContext(ctx, `SELECT v.id,v.account_id,v.price_version FROM task_media_verifications v
 JOIN accounts a ON a.id=v.account_id AND a.workspace_id=v.workspace_id AND a.platform='qianwen' AND a.type='apikey' AND a.deleted_at IS NULL AND a.status='active'
 JOIN groups g ON g.id=v.group_id AND g.workspace_id=v.workspace_id AND g.platform='qianwen' AND g.deleted_at IS NULL AND g.status='active'
 JOIN account_groups ag ON ag.account_id=a.id AND ag.group_id=v.group_id
 WHERE v.workspace_id=$1 AND v.project_id=$2 AND v.group_id=$3 AND ($4::bigint=0 OR v.account_id=$4)
 AND v.model=$5 AND v.action=$6 AND v.adapter=$7 AND v.billing_unit=$8 AND v.usd_rate=$9 AND v.per_units=$10
 AND v.verified_at<=now() AND v.expires_at>now() AND v.revoked_at IS NULL ORDER BY v.verified_at DESC,v.id LIMIT 1`,
		workspace, project, group, account, p.Model, p.Action, cell.Adapter, p.Unit, rate.Amount().String(), rate.Per().String()).Scan(&v.ID, &v.Account, &v.PriceVersion)
	if errors.Is(err, sql.ErrNoRows) {
		err = media.Unsupported
	}
	return v, err
}
