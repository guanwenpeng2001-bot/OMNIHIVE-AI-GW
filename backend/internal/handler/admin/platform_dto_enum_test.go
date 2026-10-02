//go:build unit

package admin

import (
	"fmt"
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

func TestOpenRouterAdminPlatformBinding(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		request func() any
		body    string
	}{
		{"create-account", func() any { return &CreateAccountRequest{} }, `{"name":"test","platform":%q,"type":"apikey","credentials":{"api_key":"fixture"}}`},
		{"create-group", func() any { return &CreateGroupRequest{} }, `{"name":"test","platform":%q}`},
		{"update-group", func() any { return &UpdateGroupRequest{} }, `{"platform":%q}`},
		{"composite-route", func() any { return &CompositeRouteRequest{} }, `{"public_model":"vendor/model","target_platform":%q}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			require.NoError(t, bindGroupPlatformJSON(t, fixture.request(), fmt.Sprintf(fixture.body, "openrouter")))
			require.Error(t, bindGroupPlatformJSON(t, fixture.request(), fmt.Sprintf(fixture.body, "unknown")))
		})
	}
	// Storage support does not authorize active monitor creation or updates.
	require.Error(t, bindGroupPlatformJSON(t, &channelMonitorCreateRequest{}, `{"name":"test","provider":"openrouter","interval_seconds":60}`))
	require.Error(t, bindGroupPlatformJSON(t, &channelMonitorUpdateRequest{}, `{"provider":"openrouter"}`))
	require.Error(t, bindGroupPlatformJSON(t, &channelMonitorTemplateCreateRequest{}, `{"name":"test","provider":"openrouter"}`))
}

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
	// Migration 282 replaces both historical 264 provider CHECKs. Validate the
	// current storage authority without rewriting the historical migration.
	migration, err := os.ReadFile("../../../migrations/282_openrouter_platform.sql")
	require.NoError(t, err)
	var monitors []string
	for _, constraint := range []string{"channel_monitors_provider_check", "channel_monitor_request_templates_provider_check"} {
		match := regexp.MustCompile(`ADD CONSTRAINT ` + constraint + `\s+CHECK \(provider IN \(([^)]+)\)\)`).FindStringSubmatch(string(migration))
		require.Len(t, match, 2, "%s must define the current storage enum", constraint)
		monitors = nil
		for _, value := range strings.Split(match[1], ",") {
			monitors = append(monitors, strings.Trim(strings.TrimSpace(value), "'"))
		}
		require.ElementsMatch(t, concrete, monitors, "%s must retain every concrete platform", constraint)
	}
	require.ElementsMatch(t, domain.ConcretePlatforms(), concrete)
	// Storage preserves passive-only platforms such as Cursor and OpenRouter,
	// but V1 monitor DTOs must not grant writes or unsupported active probes.
	openRouter, ok := domain.LookupPlatform(domain.PlatformOpenRouter)
	require.True(t, ok)
	require.True(t, openRouter.PassiveMonitor)
	require.False(t, openRouter.ActiveProbe)
	require.False(t, openRouter.QuotaMonitor)
	var writableMonitors []string
	for _, provider := range monitors {
		if descriptor, ok := domain.LookupPlatform(provider); ok && (descriptor.ActiveProbe || descriptor.QuotaMonitor) {
			writableMonitors = append(writableMonitors, provider)
		}
	}
	monitors = writableMonitors
	require.Len(t, monitors, 12)
	require.NotContains(t, monitors, domain.PlatformOpenRouter)
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
