package repository

import (
	"context"
	"fmt"
	"time"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/setting"
	dbuser "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/user"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

const (
	simpleModeAdminConcurrencyUpgradeKey = "simple_mode_admin_concurrency_upgraded_30"
	simpleModeLegacyAdminConcurrency     = 5
	simpleModeTargetAdminConcurrency     = 30
)

func ensureSimpleModeAdminConcurrency(ctx context.Context, client *dbent.Client) error {
	if client == nil {
		return fmt.Errorf("nil ent client")
	}

	upgraded, err := client.Setting.Query().Where(setting.KeyEQ(simpleModeAdminConcurrencyUpgradeKey)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check admin concurrency upgrade marker: %w", err)
	}
	if upgraded {
		return nil
	}

	if _, err := client.User.Update().
		Where(
			dbuser.RoleEQ(domain.RoleAdmin),
			dbuser.ConcurrencyEQ(simpleModeLegacyAdminConcurrency),
		).
		SetConcurrency(simpleModeTargetAdminConcurrency).
		Save(ctx); err != nil {
		return fmt.Errorf("upgrade simple mode admin concurrency: %w", err)
	}

	now := time.Now()
	if err := client.Setting.Create().
		SetKey(simpleModeAdminConcurrencyUpgradeKey).
		SetValue(now.Format(time.RFC3339)).
		SetUpdatedAt(now).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx); err != nil {
		return fmt.Errorf("persist admin concurrency upgrade marker: %w", err)
	}

	return nil
}
