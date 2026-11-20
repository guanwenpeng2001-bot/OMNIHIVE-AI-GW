package admin

import (
	"context"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type codexImportDraftPlan struct {
	NewCount, ExistingCount, Skipped, Failed int
	NewAccounts                              []*service.CreateAccountInput
	ExistingAccounts                         []struct {
		id    int64
		input *service.UpdateAccountInput
	}
}

// Project server-side drafts for the existing identity/merge behavior assertions.
// No write is simulated: the tests inspect the actual preparation output, while
// the PostgreSQL confirmation test exercises applying it to an existing account.
func planCodexImportForTest(t *testing.T, handler *AccountHandler, request CodexSessionImportRequest, entries []codexImportEntry) (codexImportDraftPlan, error) {
	t.Helper()
	pending, client := credentialDraftTestClient(t)
	handler.SetCredentialDraftService(pending)
	prepared, err := handler.prepareCodexSessionDrafts(context.Background(), "browser", request, entries)
	plan := codexImportDraftPlan{Skipped: prepared.Skipped, Failed: prepared.Failed}
	if err != nil {
		return plan, err
	}
	require.Zero(t, prepared.Created)
	require.Zero(t, prepared.Updated)
	require.Zero(t, client.Account.Query().CountX(context.Background()))
	for _, ready := range prepared.Ready {
		account, _, err := pending.ResolveAccountCredentialDraft(context.Background(), ready.Draft.Reference, "browser")
		if err != nil {
			return plan, err
		}
		payload := ready.Account
		if payload.ExistingAccountID == 0 {
			plan.NewCount++
			plan.NewAccounts = append(plan.NewAccounts, &service.CreateAccountInput{
				Name: payload.Name, Platform: payload.Platform, Type: payload.Type, Credentials: account.Credentials,
				Extra: payload.Extra, ExpiresAt: payload.ExpiresAt, AutoPauseOnExpired: payload.AutoPauseOnExpired,
			})
		} else {
			plan.ExistingCount++
			plan.ExistingAccounts = append(plan.ExistingAccounts, struct {
				id    int64
				input *service.UpdateAccountInput
			}{payload.ExistingAccountID, &service.UpdateAccountInput{
				Credentials: account.Credentials, Extra: payload.Extra, ExpiresAt: payload.ExpiresAt, AutoPauseOnExpired: payload.AutoPauseOnExpired,
			}})
		}
	}
	return plan, nil
}
