package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestKiloPlatformMigrationOnlyAddsKilo(t *testing.T) {
	body := compatibleIdentitySQL(t, "336_kilo_platform.sql")
	prior := compatibleIdentitySQL(t, "335_workbuddy_platform.sql")
	remaining := regexp.MustCompile(`(?m)--[^\n]*`).ReplaceAllString(body, "")
	for _, constraint := range compatibleIdentityConstraints {
		before := compatibleIdentityValues(t, prior, constraint.name)
		after := compatibleIdentityValues(t, body, constraint.name)
		require.ElementsMatch(t, append(before, "kilo"), after)
		for _, id := range domain.ConcretePlatforms() {
			require.Contains(t, after, id)
		}
		for _, pattern := range []string{
			`ALTER TABLE ` + constraint.table + `\s+DROP CONSTRAINT IF EXISTS ` + constraint.name + `;`,
			`(?s)ALTER TABLE ` + constraint.table + `\s+ADD CONSTRAINT ` + constraint.name + `\s+CHECK \(` + constraint.column + ` IN \(\s*'[a-z0-9_]+'(?:\s*,\s*'[a-z0-9_]+')*\s*\)\);`,
		} {
			expression := regexp.MustCompile(pattern)
			require.Len(t, expression.FindAllString(remaining, -1), 1)
			remaining = expression.ReplaceAllString(remaining, "")
		}
	}
	require.Empty(t, strings.TrimSpace(remaining), "no account mutation or unrelated DDL")
}
