package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func readPlazaAccounts(ctx context.Context, tx *sql.Tx, groupID int64) ([]service.Account, string, error) {
	var platform string
	if err := tx.QueryRowContext(ctx, `SELECT platform FROM groups WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, groupID).Scan(&platform); err != nil {
		return nil, "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT accounts.id, accounts.platform, jsonb_build_object('model_mapping', accounts.credentials->'model_mapping'), accounts.extra FROM accounts JOIN account_groups ON account_groups.account_id=accounts.id WHERE account_groups.group_id=$1 AND accounts.deleted_at IS NULL ORDER BY accounts.id FOR SHARE OF accounts, account_groups`, groupID)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rows.Close() }()
	accounts := []service.Account{}
	for rows.Next() {
		var account service.Account
		var credentials, extra []byte
		if err := rows.Scan(&account.ID, &account.Platform, &credentials, &extra); err != nil {
			return nil, "", err
		}
		if err := json.Unmarshal(credentials, &account.Credentials); err != nil {
			return nil, "", err
		}
		if err := json.Unmarshal(extra, &account.Extra); err != nil {
			return nil, "", err
		}
		accounts = append(accounts, account)
	}
	return accounts, platform, rows.Err()
}

func (repository *channelRepository) plazaTransaction(ctx context.Context, channelID, groupID int64, action func(*sql.Tx, *service.Channel, *service.PlazaCandidateView) error) error {
	channel, err := repository.GetByID(ctx, channelID)
	if err != nil {
		return err
	}
	return repository.runInTx(ctx, func(tx *sql.Tx) error {
		var updatedAt time.Time
		if err := tx.QueryRowContext(ctx, `SELECT updated_at FROM channels WHERE id=$1 FOR UPDATE`, channelID).Scan(&updatedAt); err != nil {
			return err
		}
		if !updatedAt.Equal(channel.UpdatedAt) {
			return service.ErrPlazaConfirmationConflict
		}
		accounts, platform, err := readPlazaAccounts(ctx, tx, groupID)
		if err != nil {
			return err
		}
		view, err := service.BuildPlazaCandidates(channel, groupID, platform, accounts)
		if err != nil {
			return err
		}
		return action(tx, channel, view)
	})
}

func (repository *channelRepository) PlazaCandidates(ctx context.Context, channelID, groupID int64) (*service.PlazaCandidateView, error) {
	var result *service.PlazaCandidateView
	err := repository.plazaTransaction(ctx, channelID, groupID, func(_ *sql.Tx, _ *service.Channel, view *service.PlazaCandidateView) error { result = view; return nil })
	return result, err
}

func (repository *channelRepository) ConfirmPlazaModels(ctx context.Context, channelID int64, input service.PlazaConfirmationInput, actorID int64) error {
	return repository.plazaTransaction(ctx, channelID, input.GroupID, func(tx *sql.Tx, channel *service.Channel, view *service.PlazaCandidateView) error {
		var now time.Time
		if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if err := service.ApplyPlazaConfirmation(channel, view, input, actorID, now); err != nil {
			return err
		}
		encoded, err := json.Marshal(channel.FeaturesConfig[service.PlazaConfirmationConfigKey])
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE channels SET features_config=jsonb_set(COALESCE(features_config,'{}'::jsonb), '{model_plaza_confirmation}', $2::jsonb), updated_at=$3 WHERE id=$1`, channelID, string(encoded), now); err != nil {
			return err
		}
		log := &service.AuditLog{CreatedAt: now, ActorUserID: &actorID, ActorRole: "admin", Action: "channel.plaza.confirm", Method: "PUT", Path: "/api/v1/admin/channels/" + strconv.FormatInt(channelID, 10), StatusCode: 200, Extra: map[string]any{"channel_id": channelID, "group_id": input.GroupID, "before": view.Selected, "after": input.Models, "revision": input.Revision}}
		_, err = tx.ExecContext(ctx, auditLogInsertSQL, auditLogInsertValues(log)...)
		return err
	})
}
