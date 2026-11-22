package postgres

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/stretchr/testify/require"
)

func TestEffectiveKeyModelsCanonicalNamedExcludeAndLegacyIntersection(t *testing.T) {
	a, b := catalog.StableID("openai", "a"), catalog.StableID("openai", "b")
	for _, tc := range []struct {
		name, legacy, models, excluded, named, wantMode, source string
		legacyModels, want, wantExclude                         []string
		group                                                   driver.Value
		missing                                                 bool
	}{
		{name: "inherit", excluded: `[]`, wantMode: "inherit", source: "model_access_rules"},
		{name: "explicit empty denies", models: `[]`, excluded: `[]`, want: []string{}, wantMode: "deny_all", source: "model_access_rules"},
		{name: "named intersection and canonical exclusion", group: int64(7), models: `["a","b"]`, named: `["` + a + `","` + b + `"]`, excluded: `["` + b + `"]`, want: []string{a}, wantExclude: []string{b}, wantMode: "allow_list", source: "model_access_rules"},
		{name: "inherit exclusion is still enforced", excluded: `["a"]`, wantExclude: []string{a}, wantMode: "inherit", source: "model_access_rules"},
		{name: "old conflicting policies intersect", legacy: "allow_list", legacyModels: []string{"a"}, models: `["a","b"]`, excluded: `[]`, want: []string{a}, wantMode: "allow_list", source: "legacy_intersection"},
		{name: "old disjoint policies deny", legacy: "allow_list", legacyModels: []string{"b"}, models: `["a"]`, excluded: `[]`, want: []string{}, wantMode: "deny_all", source: "legacy_intersection"},
		{name: "old deny cannot widen", legacy: "deny_all", models: `["a"]`, excluded: `[]`, want: []string{}, wantMode: "deny_all", source: "legacy_intersection"},
		{name: "missing canonical retains old allowlist", legacy: "allow_list", legacyModels: []string{"a"}, missing: true, want: []string{a}, wantMode: "allow_list", source: "legacy_intersection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			rows := sqlmock.NewRows([]string{"access_group_id", "models", "exclude", "named"})
			if !tc.missing {
				var models, named driver.Value
				if tc.models != "" {
					models = []byte(tc.models)
				}
				if tc.named != "" {
					named = []byte(tc.named)
				}
				rows.AddRow(tc.group, models, []byte(tc.excluded), named)
			}
			mock.ExpectQuery("SELECT r.access_group_id").WithArgs(int64(2), int64(9)).WillReturnRows(rows)
			mode, models, excludes, source, err := EffectiveKeyModelRule(context.Background(), db, 2, 9, "openai", tc.legacy, tc.legacyModels)
			require.NoError(t, err)
			require.Equal(t, tc.wantMode, mode)
			require.Equal(t, tc.want, models)
			require.ElementsMatch(t, tc.wantExclude, excludes)
			require.Equal(t, tc.source, source)
			policy := identity.KeyPolicy{Actions: []string{"text.chat"}, Platform: "openai", Mode: mode, Models: models, ExcludedModels: excludes}
			if len(tc.wantExclude) > 0 {
				require.False(t, policy.Allows("text.chat", []string{tc.wantExclude[0]}))
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCanonicalKeyWriteKeepsProjectionInSameTransactionAndCAS(t *testing.T) {
	for _, stale := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectBegin()
		affected := int64(1)
		if stale {
			affected = 0
		}
		mock.ExpectExec("INSERT INTO model_access_rules").WithArgs(int64(2), int64(9), nil, `[]`, `[]`, int64(3)).WillReturnResult(sqlmock.NewResult(0, affected))
		if !stale {
			mock.ExpectExec("UPDATE project_key_policies SET model_mode='inherit'").WithArgs(int64(9), int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
		}
		mock.ExpectRollback()
		tx, err := db.Begin()
		require.NoError(t, err)
		err = WriteKeyModelRule(context.Background(), tx, 2, 9, identity.ModelRule{Models: []string{}, Revision: 3})
		if stale {
			require.ErrorIs(t, err, fault.Conflict)
		} else {
			require.NoError(t, err)
		}
		require.NoError(t, tx.Rollback())
		require.NoError(t, mock.ExpectationsWereMet())
		db.Close()
	}
}
