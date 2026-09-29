//go:build unit

package admin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

// Discover every platform enum tag, including nested and newly added DTOs.
// Domain constants and the monitor migration are independent authorities;
// a missing provider in a copied binding tag must not become a passing fixture.
func TestAllPlatformDTOEnumsMatchAuthorities(t *testing.T) {
	constants, err := parser.ParseFile(token.NewFileSet(), "../../domain/constants.go", nil, 0)
	require.NoError(t, err)
	var concrete []string
	ast.Inspect(constants, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if strings.HasPrefix(name.Name, "Platform") && name.Name != "PlatformComposite" && i < len(spec.Values) {
				if lit, ok := spec.Values[i].(*ast.BasicLit); ok {
					value, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					concrete = append(concrete, value)
				}
			}
		}
		return true
	})
	migration, err := os.ReadFile("../../../migrations/264_platform_monitor_storage.sql")
	require.NoError(t, err)
	match := regexp.MustCompile(`provider IN \(([^)]+)\)`).FindStringSubmatch(string(migration))
	require.Len(t, match, 2)
	var monitors []string
	for _, value := range strings.Split(match[1], ",") {
		monitors = append(monitors, strings.Trim(strings.TrimSpace(value), "'"))
	}
	require.ElementsMatch(t, domain.ConcretePlatforms(), concrete)
	// Storage preserves historical Cursor rows, but V1 monitor DTOs must not
	// grant permission to create/edit them or enable unsupported active probes.
	require.ElementsMatch(t, concrete, monitors, "monitor storage must retain every concrete platform")
	var writableMonitors []string
	for _, provider := range monitors {
		if descriptor, ok := domain.LookupPlatform(provider); ok && (descriptor.ActiveProbe || descriptor.QuotaMonitor) {
			writableMonitors = append(writableMonitors, provider)
		}
	}
	monitors = writableMonitors
	require.Len(t, monitors, 12)
	checked := 0
	require.NoError(t, filepath.WalkDir("..", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err)
		ast.Inspect(file, func(n ast.Node) bool {
			field, ok := n.(*ast.Field)
			if !ok || field.Tag == nil {
				return true
			}
			raw, err := strconv.Unquote(field.Tag.Value)
			require.NoError(t, err)
			tag := reflect.StructTag(raw)
			jsonName := strings.Split(tag.Get("json"), ",")[0]
			if jsonName != "platform" && jsonName != "provider" && jsonName != "target_platform" && jsonName != "platforms" {
				return true
			}
			for _, validation := range strings.Split(tag.Get("binding"), ",") {
				if !strings.HasPrefix(validation, "oneof=") {
					continue
				}
				got := strings.Fields(strings.TrimPrefix(validation, "oneof="))
				want := append([]string(nil), concrete...)
				if strings.Contains(filepath.Base(path), "channel_monitor") {
					want = append([]string(nil), monitors...)
				} else if filepath.Base(path) == "group_handler.go" && jsonName == "platform" {
					want = append(want, "composite")
				}
				sort.Strings(got)
				sort.Strings(want)
				require.Equal(t, want, got, "%s: %s enum drift", path, jsonName)
				checked++
			}
			return true
		})
		return nil
	}))
	require.GreaterOrEqual(t, checked, 6, "enum scan unexpectedly missed known DTOs")
}
