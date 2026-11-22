package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestCompatibleBillingPlanBulkSQL(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		patch map[string]any
		clear bool
	}{
		{"null clears", map[string]any{"billing_plan": nil}, true},
		{"empty clears", map[string]any{"billing_plan": ""}, true},
		{"selected merges", map[string]any{"billing_plan": "pro"}, false},
		{"omitted preserves", map[string]any{"api_protocol": "adaptive"}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			matcher := sqlmock.QueryMatcherFunc(func(_, query string) error {
				if strings.Contains(query, "account_configuration_owner_lock") {
					return nil
				}
				merge := "credentials = COALESCE(credentials, '{}'::jsonb) || $1::jsonb"
				if scenario.clear {
					merge = "credentials = (COALESCE(credentials, '{}'::jsonb) || $1::jsonb) - 'billing_plan'"
				}
				if !strings.Contains(query, merge) || !strings.Contains(query, "WHERE id = ANY($2) AND deleted_at IS NULL AND workspace_id = 9") {
					return fmt.Errorf("missing atomic credential merge or account/workspace scope: %s", query)
				}
				if !scenario.clear && strings.Contains(query, "- 'billing_plan'") {
					return fmt.Errorf("plan deleted without a clear marker")
				}
				return nil
			})
			database, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
			require.NoError(t, err)
			defer func() {
				mock.ExpectClose()
				require.NoError(t, database.Close())
			}()
			// A zero-row result avoids scheduler side effects; this test inspects the
			// actual repository query and parameter binding without executing writes.
			mock.ExpectBegin()
			expectAccountConfigurationPreflight(mock)
			mock.ExpectExec("update").WithArgs(sqlmock.AnyArg(), pq.Array([]int64{12})).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectCommit()
			repository := &accountRepository{sql: database}
			_, err = repository.BulkUpdate(domain.WithWorkspaceScope(context.Background(), 9), []int64{12}, service.AccountBulkUpdate{Credentials: scenario.patch})
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
