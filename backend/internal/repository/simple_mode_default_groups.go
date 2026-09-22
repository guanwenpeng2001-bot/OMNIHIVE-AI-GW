package repository

import (
	"context"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

const simpleModeDefaultGroupDescription = "Auto-created default group"

func ensureSimpleModeDefaultGroups(ctx context.Context, client *dbent.Client) error {
	if client == nil {
		return fmt.Errorf("nil ent client")
	}

	if err := backfillSimpleModeGrokDefaultImageGeneration(ctx, client); err != nil {
		return err
	}

	for _, platform := range domain.ConcretePlatforms() {
		if err := createGroupIfNotExists(ctx, client, platform+"-default", platform); err != nil {
			return err
		}
	}

	// Keep Antigravity's historical two-group minimum as well as its canonical
	// default. Existing numbered or operator-managed groups count toward it.
	count, err := client.Group.Query().
		Where(group.PlatformEQ(domain.PlatformAntigravity), group.DeletedAtIsNil()).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("count groups for platform %s: %w", domain.PlatformAntigravity, err)
	}
	for i := count; i < 2; i++ {
		name := fmt.Sprintf("%s-default-%d", domain.PlatformAntigravity, i+1)
		if err := createGroupIfNotExists(ctx, client, name, domain.PlatformAntigravity); err != nil {
			return err
		}
	}

	return nil
}

func createGroupIfNotExists(ctx context.Context, client *dbent.Client, name, platform string) error {
	exists, err := client.Group.Query().
		Where(group.NameEQ(name), group.DeletedAtIsNil()).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check group exists %s: %w", name, err)
	}
	if exists {
		return nil
	}

	_, err = client.Group.Create().
		SetName(name).
		SetDescription(simpleModeDefaultGroupDescription).
		SetPlatform(platform).
		SetStatus(domain.StatusActive).
		SetSubscriptionType(domain.SubscriptionTypeStandard).
		SetRateMultiplier(1.0).
		SetIsExclusive(false).
		SetAllowImageGeneration(platform == domain.PlatformGrok).
		Save(ctx)
	if err != nil {
		if dbent.IsConstraintError(err) {
			// Concurrent server startups may race on creation; treat as success.
			return nil
		}
		return fmt.Errorf("create default group %s: %w", name, err)
	}
	return nil
}

func backfillSimpleModeGrokDefaultImageGeneration(ctx context.Context, client *dbent.Client) error {
	_, err := client.Group.Update().
		Where(
			group.NameEQ(domain.PlatformGrok+"-default"),
			group.PlatformEQ(domain.PlatformGrok),
			group.DescriptionEQ(simpleModeDefaultGroupDescription),
			group.StatusEQ(domain.StatusActive),
			group.AllowImageGenerationEQ(false),
			group.DeletedAtIsNil(),
		).
		SetAllowImageGeneration(true).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("backfill auto-created grok default image generation: %w", err)
	}
	return nil
}
