//go:build unit || integration

package repository

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// usageLogInsertColumnListPattern 从真实下发的 INSERT 语句里截取列清单。
var usageLogInsertColumnListPattern = regexp.MustCompile(`(?s)INSERT INTO usage_logs \((.*?)\) VALUES`)

// captureUsageLogInsertColumns 通过 sqlmock 捕获 Create 实际发送的 INSERT 语句并解析其列清单，
// 因此断言对象是「代码真正发送的列」，而不是源码文本；重复列或空列都会被拒绝。
func captureUsageLogInsertColumns(t *testing.T) []string {
	t.Helper()

	var captured []string
	matcher := sqlmock.QueryMatcherFunc(func(_, actualSQL string) error {
		captured = append(captured, actualSQL)
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	createdAt := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	sessionID := "sess-contract"
	log := &service.UsageLog{
		UserID:     1,
		APIKeyID:   2,
		AccountID:  3,
		RequestID:  "client:schema-contract",
		Model:      "claude-3",
		SessionID:  &sessionID,
		TotalCost:  1,
		ActualCost: 1,
		CreatedAt:  createdAt,
	}
	mock.ExpectQuery("INSERT INTO usage_logs").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(1), createdAt))

	repo := &usageLogRepository{sql: db}
	_, err = repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.Len(t, captured, 1, "expected exactly one INSERT to be captured")

	match := usageLogInsertColumnListPattern.FindStringSubmatch(captured[0])
	require.Len(t, match, 2, "unrecognised INSERT shape:\n%s", captured[0])

	columns := make([]string, 0, len(usageLogInsertArgTypes))
	seen := make(map[string]struct{}, len(usageLogInsertArgTypes))
	for _, raw := range strings.Split(match[1], ",") {
		column := strings.TrimSpace(raw)
		if column == "" {
			continue
		}
		_, duplicate := seen[column]
		require.Falsef(t, duplicate, "INSERT column %q appears twice", column)
		seen[column] = struct{}{}
		columns = append(columns, column)
	}
	sort.Strings(columns)
	return columns
}

// usageLogColumnsMissingFrom 返回 in 中存在、reference 中缺失的列名（两个方向比较时各调用一次即可）。
func usageLogColumnsMissingFrom(reference, in []string) []string {
	referenceSet := make(map[string]struct{}, len(reference))
	for _, column := range reference {
		referenceSet[column] = struct{}{}
	}

	var missing []string
	for _, column := range in {
		if _, ok := referenceSet[column]; !ok {
			missing = append(missing, column)
		}
	}
	sort.Strings(missing)
	return missing
}
