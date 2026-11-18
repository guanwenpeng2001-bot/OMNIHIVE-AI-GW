//go:build unit

package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	dbentschema "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/schema"
	"github.com/stretchr/testify/require"
)

// TestUsageLogEntProjectionMatchesSQLInsertColumns 守卫 usage_logs 的 Ent 投影不落后于手写 SQL INSERT：
// 线上表结构由 backend/migrations/*.sql 与 usage_log_repo_insert.go 决定，Ent schema 只是模型投影，
// 两者列集合必须完全一致（名称与个数），否则 enttest 建出的表无法代表线上表，也不会有人及时发现。
func TestUsageLogEntProjectionMatchesSQLInsertColumns(t *testing.T) {
	insertColumns := captureUsageLogInsertColumns(t)
	require.NotEmpty(t, insertColumns)
	require.Contains(t, insertColumns, "session_id")
	require.Equal(t, len(usageLogInsertArgTypes), len(insertColumns),
		"INSERT 列数与 usageLogInsertArgTypes 必须一致（列清单由 usage_log_repo_insert.go 定义）")

	projection := entUsageLogProjectionColumns()
	require.Equal(t, insertColumns, projection,
		"Ent schema 与 SQL INSERT 列清单不一致：新增/删除列时必须同时更新 ent/schema/usage_log.go 并重新生成 Ent，"+
			"缺失=%v 多出=%v",
		usageLogColumnsMissingFrom(insertColumns, projection),
		usageLogColumnsMissingFrom(projection, insertColumns))
}

// entUsageLogProjectionColumns 返回 Ent schema 为 usage_logs 声明的列名。
// 自增主键 id 不出现在 Fields() 里，与手写 INSERT 的列清单一致，因此无需额外剔除。
// 唯一调用者是本文件的 unit 用例，所以它放在 unit 档里，不在 integration 档下假装可用。
func entUsageLogProjectionColumns() []string {
	fields := dbentschema.UsageLog{}.Fields()
	columns := make([]string, 0, len(fields))
	for _, f := range fields {
		columns = append(columns, f.Descriptor().Name)
	}
	sort.Strings(columns)
	return columns
}

// TestUsageLogsRuntimeUsesSQLMigrationsNotEntAutoMigrate 守卫运行期不使用 Ent 自动建表。
// 权威来源是 migrations/*.sql + internal/repository 的 SQL runner；Ent 的 Schema.Create 只允许出现在测试辅助代码里。
func TestUsageLogsRuntimeUsesSQLMigrationsNotEntAutoMigrate(t *testing.T) {
	backendRoot := filepath.Join("..", "..")
	require.DirExists(t, filepath.Join(backendRoot, "migrations"), "测试必须从 backend/internal/repository 运行")

	offenders, err := findEntAutoMigrateCalls(filepath.Join(backendRoot, "cmd"), filepath.Join(backendRoot, "internal"), filepath.Join(backendRoot, "pkg"))
	require.NoError(t, err)
	require.Empty(t, offenders,
		"运行期不得调用 Ent 自动迁移；usage_logs 等表由 backend/migrations/*.sql 建立（违规文件：%v）", offenders)
}

// TestFindEntAutoMigrateCallsDetectsViolations 用合成源码证明守卫本身能抓到 Ent 自动迁移调用。
func TestFindEntAutoMigrateCallsDetectsViolations(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "runtime.go"), []byte(`package sample

import (
	"context"
	migrate "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/migrate"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
)

func bootstrap(ctx context.Context, client *ent.Client) {
	_ = client.Schema.Create(ctx)
	_ = migrate.Create(ctx)
}
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "runtime_test.go"), []byte(`package sample

func helper(client interface{ Schema() }) {}
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean.go"), []byte(`package sample

func one() int { return 1 }
`), 0o600))

	offenders, err := findEntAutoMigrateCalls(dir)
	require.NoError(t, err)
	require.Equal(t, []string{"runtime.go"}, offenders, "必须只报运行期文件，忽略 _test.go 与无关调用")
}

// findEntAutoMigrateCalls 在给定目录树里找出运行期（非 _test.go）的 Ent 自动迁移调用：
//   - <expr>.Schema.Create(...)
//   - migrate.Create(...)
func findEntAutoMigrateCalls(roots ...string) ([]string, error) {
	var offenders []string
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if name := entry.Name(); name == "node_modules" || name == ".git" {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			if !containsEntAutoMigrateCall(file) {
				return nil
			}
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			offenders = append(offenders, filepath.ToSlash(relative))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(offenders)
	return offenders, nil
}

func containsEntAutoMigrateCall(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if found {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Create" {
			return true
		}
		switch inner := selector.X.(type) {
		case *ast.SelectorExpr:
			found = inner.Sel.Name == "Schema"
		case *ast.Ident:
			found = inner.Name == "migrate"
		}
		return !found
	})
	return found
}
