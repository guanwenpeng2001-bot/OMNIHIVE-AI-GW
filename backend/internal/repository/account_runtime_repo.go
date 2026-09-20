package repository

import (
	"context"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type runtimeWriteLocked struct{}

func runtimeIdentityGuardRequired(ctx context.Context, id int64) bool {
	return ctx.Value(runtimeWriteLocked{}) == nil && service.AccountRuntimeCacheSuffix(ctx, id) != strconv.FormatInt(id, 10)
}

// All identity-bearing account and proxy rows stay locked through the write.
func (r *accountRepository) withRuntimeIdentity(ctx context.Context, id int64, identity string, write func(context.Context, *accountRepository) error) error {
	transaction := dbent.TxFromContext(ctx)
	owned := transaction == nil
	if owned {
		var err error
		transaction, err = r.client.Tx(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = transaction.Rollback() }()
	}
	lockedCtx := context.WithValue(dbent.NewTxContext(ctx, transaction), runtimeWriteLocked{}, true)
	client := transaction.Client()
	rows, err := client.QueryContext(lockedCtx, "SELECT id FROM accounts WHERE deleted_at IS NULL AND (id = $1 OR id = (SELECT parent_account_id FROM accounts WHERE id = $1)) ORDER BY id FOR UPDATE", id)
	if err != nil {
		return err
	}
	foundAccount := false
	for rows.Next() {
		var lockedID int64
		if err := rows.Scan(&lockedID); err != nil {
			_ = rows.Close()
			return err
		}
		foundAccount = foundAccount || lockedID == id
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if !foundAccount {
		return service.ErrAccountRuntimeIdentityChanged
	}
	entity, err := client.Account.Get(lockedCtx, id)
	if err != nil {
		return service.ErrAccountRuntimeIdentityChanged
	}
	load := func(entity *dbent.Account) (*service.Account, error) {
		account := accountEntityToService(entity)
		if entity.ProxyID != nil {
			proxyRows, err := client.QueryContext(lockedCtx, "SELECT id FROM proxies WHERE id = $1 AND deleted_at IS NULL FOR SHARE", *entity.ProxyID)
			if err != nil {
				return nil, err
			}
			found := proxyRows.Next()
			err = proxyRows.Err()
			_ = proxyRows.Close()
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, service.ErrAccountRuntimeIdentityChanged
			}
			proxy, err := client.Proxy.Get(lockedCtx, *entity.ProxyID)
			if err != nil {
				return nil, err
			}
			account.Proxy = proxyEntityToService(proxy)
		}
		return account, nil
	}
	current, err := load(entity)
	if err != nil {
		return err
	}
	if current.ParentAccountID != nil {
		ownerRows, err := client.QueryContext(lockedCtx, "SELECT id FROM accounts WHERE id = $1 AND deleted_at IS NULL FOR UPDATE", *current.ParentAccountID)
		if err != nil {
			return err
		}
		ownerFound := ownerRows.Next()
		ownerErr := ownerRows.Err()
		_ = ownerRows.Close()
		if ownerErr != nil {
			return ownerErr
		}
		if !ownerFound {
			return service.ErrAccountRuntimeIdentityChanged
		}
		parent, err := client.Account.Get(lockedCtx, *current.ParentAccountID)
		if err != nil {
			return service.ErrAccountRuntimeIdentityChanged
		}
		owner, err := load(parent)
		if err != nil {
			return err
		}
		current, err = service.AccountWithRuntimeCredentialOwner(current, owner)
		if err != nil {
			return err
		}
	}
	if service.AccountRuntimeIdentityKey(current) != identity {
		return service.ErrAccountRuntimeIdentityChanged
	}
	repository := &accountRepository{client: client, sql: client}
	if err := write(lockedCtx, repository); err != nil {
		return err
	}
	if owned {
		if err := transaction.Commit(); err != nil {
			return err
		}
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return nil
}

func (r *accountRepository) UpdateAccountRuntimeSnapshot(ctx context.Context, expected *service.Account, updates map[string]any, sessionWindowEnd *time.Time) error {
	if expected == nil {
		return service.ErrAccountNilInput
	}
	return r.withRuntimeIdentity(ctx, expected.ID, service.AccountRuntimeIdentityKey(expected), func(lockedCtx context.Context, repository *accountRepository) error {
		if err := repository.UpdateExtra(lockedCtx, expected.ID, updates); err != nil {
			return err
		}
		if sessionWindowEnd != nil {
			return repository.UpdateSessionWindowEnd(lockedCtx, expected.ID, *sessionWindowEnd)
		}
		return nil
	})
}

func (r *accountRepository) UpdateAccountRuntimeCredentials(ctx context.Context, expected *service.Account, credentials map[string]any) error {
	if expected == nil {
		return service.ErrAccountNilInput
	}
	return r.withRuntimeIdentity(ctx, expected.ID, service.AccountRuntimeIdentityKey(expected), func(lockedCtx context.Context, repository *accountRepository) error {
		return repository.UpdateCredentials(lockedCtx, expected.ID, credentials)
	})
}

var _ service.AccountRuntimeSnapshotWriter = (*accountRepository)(nil)
var _ service.AccountRuntimeCredentialsWriter = (*accountRepository)(nil)

// External transactions invalidate only after their actual commit, never after
// an inner repository call returns while its changes are still uncommitted.
func invalidateCatalogAfterWrite(ctx context.Context) {
	if transaction := dbent.TxFromContext(ctx); transaction != nil {
		transaction.OnCommit(func(next dbent.Committer) dbent.Committer {
			return dbent.CommitFunc(func(commitCtx context.Context, transaction *dbent.Tx) error {
				if err := next.Commit(commitCtx, transaction); err != nil {
					return err
				}
				service.InvalidateModelCatalogCaches()
				return nil
			})
		})
		return
	}
	service.InvalidateModelCatalogCaches()
}
