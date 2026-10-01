package service

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/ent/proxy"
)

// UpdateAccountCredentialsIfUnchanged serializes credential replacement with
// configuration writes. A refresh never installs a token into a changed identity.
// Draft edits reuse their enclosing confirmation transaction.
func (s *AuthPendingIdentityService) UpdateAccountCredentialsIfUnchanged(ctx context.Context, expected *Account, admin AdminService, input *UpdateAccountInput) (*Account, error) {
	if expected == nil || expected.IsShadow() {
		return nil, ErrAccountCredentialDraftConflict
	}
	callerContext := ctx
	transaction := dbent.TxFromContext(ctx)
	owned := transaction == nil
	if owned {
		var err error
		transaction, err = s.entClient.Tx(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = transaction.Rollback() }()
		ctx = dbent.NewTxContext(ctx, transaction)
	}
	stored, err := transaction.Account.Query().Where(account.IDEQ(expected.ID), account.DeletedAtIsNil()).ForUpdate().Only(ctx)
	if err != nil {
		return nil, err
	}
	current := &Account{ID: stored.ID, Platform: stored.Platform, Type: stored.Type, Credentials: stored.Credentials, Extra: stored.Extra, ProxyID: stored.ProxyID, ProxyGroupID: stored.ProxyGroupID, ParentAccountID: stored.ParentAccountID, QuotaDimension: string(stored.QuotaDimension)}
	baseline := *expected
	baseline.Proxy = nil
	if AccountRuntimeIdentityKey(current) != AccountRuntimeIdentityKey(&baseline) || current.ModelCatalogBaseline() != expected.ModelCatalogBaseline() {
		return nil, ErrAccountCredentialDraftConflict
	}
	if expected.Proxy != nil {
		storedProxy, err := transaction.Proxy.Query().Where(proxy.IDEQ(expected.Proxy.ID), proxy.DeletedAtIsNil()).ForUpdate().Only(ctx)
		if err != nil {
			return nil, err
		}
		current.Proxy = &Proxy{ID: storedProxy.ID, Protocol: storedProxy.Protocol, Host: storedProxy.Host, Port: storedProxy.Port, Status: storedProxy.Status, FallbackMode: storedProxy.FallbackMode, BackupProxyID: storedProxy.BackupProxyID}
		if storedProxy.Username != nil {
			current.Proxy.Username = *storedProxy.Username
		}
		if storedProxy.Password != nil {
			current.Proxy.Password = *storedProxy.Password
		}
		if AccountRuntimeIdentityKey(current) != AccountRuntimeIdentityKey(expected) {
			return nil, ErrAccountCredentialDraftConflict
		}
	}
	updated, err := admin.UpdateAccount(ctx, expected.ID, input)
	if err != nil || !owned {
		return updated, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	InvalidateAvailableModelsCacheForAccount(expected)
	return admin.GetAccount(callerContext, expected.ID)
}
