//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func createEncryptedCatalogFixture(t *testing.T, ctx context.Context, client *dbent.Client, database *sql.DB, repository *accountRepository, account *service.Account) *service.Account {
	t.Helper()
	extra := account.Extra
	account.Extra = nil
	require.NoError(t, repository.Create(ctx, account))
	if extra != nil {
		payload, err := json.Marshal(extra)
		require.NoError(t, err)
		_, err = database.ExecContext(ctx, `UPDATE accounts SET extra=$2::jsonb WHERE id=$1`, account.ID, payload)
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		_, _ = database.ExecContext(context.Background(), `DELETE FROM credential_secrets WHERE account_id=$1`, account.ID)
	})
	created, err := repository.GetByID(ctx, account.ID)
	require.NoError(t, err)
	if _, referenced := service.CredentialReference(created.Credentials); referenced {
		created.Credentials[service.CredentialSecretVersionKey] = service.CredentialVersion(created.Credentials)
	}
	_, err = database.ExecContext(ctx, `DELETE FROM audit_logs WHERE extra->>'account_id'=$1`, strconv.FormatInt(account.ID, 10))
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE account_id=$1`, account.ID)
	require.NoError(t, err)
	return created
}

// Legacy fixture adapters keep the state-oriented assertions on the unified writer.
func (r *accountRepository) UpdateAccountModelAllowlist(ctx context.Context, id int64, baseline service.AccountModelCatalogBaseline, allowlist service.AccountModelAllowlist) error {
	_, err := r.ReplaceAccountModelSelectionScope(ctx, id, baseline, allowlist, nil)
	return err
}

func (r *accountRepository) ReplaceAccountModelSelectionScope(ctx context.Context, id int64, baseline service.AccountModelCatalogBaseline, allowlist service.AccountModelAllowlist, mapping *map[string]string) ([]string, error) {
	command := service.ModelSelectionCommand{
		Mode: service.ModelSelectionInherit, Reason: allowlist.Reason, ActorID: allowlist.ConfirmedBy, Source: service.ModelSelectionAdmin,
		ExpectedRulesRevision: &baseline.RulesRevision, ExpectedIdentityRevision: &baseline.IdentityRevision,
	}
	if allowlist.Enabled {
		command.Mode = service.ModelSelectionRestricted
		command.Models = allowlist.Models
		if len(allowlist.Models) == 0 {
			command.Mode = service.ModelSelectionDenyAll
		}
	}
	if mapping != nil {
		command.ModelMapping = *mapping
	}
	result, err := r.UpdateModelSelection(ctx, id, service.ModelSelectionAdmin, func(account *service.Account) (service.ModelSelectionResult, error) {
		return service.ResolveLockedModelSelection(account, command)
	})
	if errors.Is(err, service.ErrModelSelectionConflict) || errors.Is(err, service.ErrAccountNotFound) {
		return nil, service.ErrAccountModelCatalogConflict
	}
	return result.NotDiscovered, err
}
