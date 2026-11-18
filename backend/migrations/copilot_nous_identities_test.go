package migrations_test

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestCopilotNousIdentityMigrationFiniteExpansion(test *testing.T) {
	ids := append(strings.Fields(compatibleIdentityIDs), "github_copilot", "nous_research")
	sort.Strings(ids)
	require.Len(test, ids, 72)
	require.Equal(test, "5f4ee54c46f62235239a4d9196dc840a3075b48133bee60afefd89e4f2b502bf", fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(ids, "\n")+"\n"))))
	var embedded []string
	for _, provider := range domain.CompatibleProviders() {
		embedded = append(embedded, provider.ID)
	}
	for _, id := range ids {
		require.Contains(test, embedded, id)
	}
	body := compatibleIdentitySQL(test, "284_copilot_nous_identities.sql")
	prior := compatibleIdentitySQL(test, compatibleIdentityMigration)
	remaining := regexp.MustCompile(`(?m)--[^\n]*`).ReplaceAllString(body, "")
	for _, constraint := range compatibleIdentityConstraints {
		before := compatibleIdentityValues(test, prior, constraint.name)
		after := compatibleIdentityValues(test, body, constraint.name)
		require.ElementsMatch(test, append(before, "github_copilot", "nous_research"), after)
		if constraint.table == "user_platform_quotas" {
			require.Len(test, after, 92)
			require.Contains(test, after, "vertex")
		} else {
			require.Len(test, after, 91)
			require.NotContains(test, after, "vertex")
		}
		for _, id := range ids {
			require.Contains(test, after, id)
		}
		drop := `ALTER TABLE ` + constraint.table + `\s+DROP CONSTRAINT IF EXISTS ` + constraint.name + `;`
		add := `(?s)ALTER TABLE ` + constraint.table + `\s+ADD CONSTRAINT ` + constraint.name + `\s+CHECK \(` + constraint.column + ` IN \(\s*'[a-z0-9_]+'(?:\s*,\s*'[a-z0-9_]+')*\s*\)\);`
		for _, pattern := range []string{drop, add} {
			expression := regexp.MustCompile(pattern)
			require.Len(test, expression.FindAllString(remaining, -1), 1)
			remaining = expression.ReplaceAllString(remaining, "")
		}
	}
	require.Empty(test, strings.TrimSpace(remaining))
}
