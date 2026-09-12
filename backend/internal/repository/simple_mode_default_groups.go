package repository

import (
	"context"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const simpleModeDefaultGroupDescription = "Auto-created default group"

func ensureSimpleModeDefaultGroups(ctx context.Context, client *dbent.Client) error {
	if client == nil {
		return fmt.Errorf("nil ent client")
	}

	if err := backfillSimpleModeGrokDefaultImageGeneration(ctx, client); err != nil {
		return err
	}

	for _, platform := range service.ConcretePlatforms() {
		if err := createGroupIfNotExists(ctx, client, platform+"-default", platform); err != nil {
			return err
		}
	}

	// Keep Antigravity's historical two-group minimum as well as its canonical
	// default. Existing numbered or operator-managed groups count toward it.
	count, err := client.Group.Query().
		Where(group.PlatformEQ(service.PlatformAntigravity), group.DeletedAtIsNil()).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("count groups for platform %s: %w", service.PlatformAntigravity, err)
	}
	for i := count; i < 2; i++ {
		name := fmt.Sprintf("%s-default-%d", service.PlatformAntigravity, i+1)
		if err := createGroupIfNotExists(ctx, client, name, service.PlatformAntigravity); err != nil {
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
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1.0).
		SetIsExclusive(false).
		SetAllowImageGeneration(platform == service.PlatformGrok).
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
			group.NameEQ(service.PlatformGrok+"-default"),
			group.PlatformEQ(service.PlatformGrok),
			group.DescriptionEQ(simpleModeDefaultGroupDescription),
			group.StatusEQ(service.StatusActive),
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
