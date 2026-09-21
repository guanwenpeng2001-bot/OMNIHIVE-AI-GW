package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"log/slog"
	"time"
)

// ModelCatalogGenerationReader reads the primary database, never Redis or a
// consumer watermark. Notifications can be lost without making a cache trusted.
type ModelCatalogGenerationReader interface {
	ModelCatalogGeneration(context.Context, string) (int64, error)
}

func modelCatalogGeneration(ctx context.Context, repository any, scope string) (int64, bool, error) {
	reader, ok := repository.(ModelCatalogGenerationReader)
	if !ok {
		return 0, false, nil
	}
	generation, err := reader.ModelCatalogGeneration(ctx, scope)
	return generation, true, err
}

func catalogVersionedCacheKey(ctx context.Context, repository any, key string) (string, error) {
	if workspaceID, scoped := domain.WorkspaceScope(ctx); scoped {
		key = fmt.Sprintf("workspace:%d|%s", workspaceID, key)
	}
	generation, authoritative, err := modelCatalogGeneration(ctx, repository, "catalog")
	if err != nil {
		return "", err
	}
	if !authoritative {
		return key, nil
	}
	return fmt.Sprintf("%s|generation:%d", key, generation), nil
}

// Versioned keys also fence a load that finishes after invalidation: its value
// can only be read using the old authoritative version.
func InvalidateModelCatalogCaches() {
	if gateway := availableModelsCacheInvalidator.Load(); gateway != nil && gateway.modelsListCache != nil {
		gateway.modelsListCache.Flush()
	}
}

// Capture before editing; after the operation reread committed identities. A
// failed write therefore does not clear state, while a partially successful
// multi-step admin operation still invalidates the identities it changed.
func (s *adminServiceImpl) captureRuntimeInvalidation(ctx context.Context, account *Account) func() {
	if s.runtimeInvalidator == nil || account == nil {
		return func() {}
	}
	baseline, err := loadRuntimeCredentialOwner(ctx, s.accountRepo, account)
	if err != nil {
		return func() {}
	}
	before := []*Account{snapshotOAuthRefreshAccount(baseline)}
	shadows, err := s.accountRepo.ListShadowsByParent(ctx, account.ID)
	if err == nil {
		for _, shadow := range shadows {
			loaded, loadErr := s.accountRepo.GetByID(ctx, shadow.ID)
			if loadErr != nil {
				continue
			}
			if snapshot, err := AccountWithRuntimeCredentialOwner(loaded, account); err == nil {
				before = append(before, snapshot)
			}
		}
	}
	return func() {
		invalidateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		for _, snapshot := range before {
			current, err := s.accountRepo.GetByID(invalidateCtx, snapshot.ID)
			if err != nil && !errors.Is(err, ErrAccountNotFound) {
				continue
			}
			if current != nil {
				current, err = loadRuntimeCredentialOwner(invalidateCtx, s.accountRepo, current)
				if err != nil {
					current = nil
				}
			}
			if err := s.runtimeInvalidator.Invalidate(invalidateCtx, snapshot, current); err != nil {
				slog.WarnContext(invalidateCtx, "runtime cache invalidation failed", "account_id", snapshot.ID, "error", err)
			}
		}
	}
}
