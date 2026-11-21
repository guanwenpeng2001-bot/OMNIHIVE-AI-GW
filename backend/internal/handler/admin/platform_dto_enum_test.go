//go:build unit

package admin

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
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
	migration, err := os.ReadFile("../../../migrations/335_workbuddy_platform.sql")
	require.NoError(t, err)
	var monitors []string
	for _, constraint := range []string{"channel_monitors_provider_check", "channel_monitor_request_templates_provider_check"} {
		match := regexp.MustCompile(`ADD CONSTRAINT ` + constraint + `\s+CHECK \(provider IN \(([^)]+)\)\)`).FindStringSubmatch(string(migration))
		require.Len(t, match, 2, "%s must define the current storage enum", constraint)
		monitors = nil
		for _, value := range strings.Split(match[1], ",") {
			monitors = append(monitors, strings.Trim(strings.TrimSpace(value), "'"))
		}
		require.ElementsMatch(t, domain.ConcretePlatforms(), monitors, "%s must retain every concrete platform", constraint)
	}
	var legacy []string
	for _, platform := range domain.ConcretePlatforms() {
		if _, compatible := domain.LookupCompatibleProvider(platform); !compatible {
			legacy = append(legacy, platform)
		}
	}
	require.ElementsMatch(t, legacy, concrete)
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
	require.Len(t, monitors, 13)
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
				if validation == "concrete_platform" || validation == "group_platform" {
					want := "concrete_platform"
					if filepath.Base(path) == "group_handler.go" && jsonName == "platform" {
						want = "group_platform"
					}
					require.Equal(t, want, validation)
					checked++
					continue
				}
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

func installAdminCompatibleFixture(test *testing.T) {
	test.Helper()
	test.Cleanup(domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{{
		ID: "example-admin", Label: "Example", LabelZh: "Example", BaseSource: "preset", DefaultMode: "coding",
		EvidenceURL: "https://example.com/docs", EvidenceObserved: "2026-10-03",
		Modes: []domain.CompatibleMode{
			{Mode: "coding", DefaultProtocol: domain.APIProtocolChatCompletions,
				Endpoints: []domain.CompatibleEndpoint{{Protocol: domain.APIProtocolChatCompletions, BaseURL: "https://example.com/v1", Auth: "bearer"}},
				Bridges:   []string{domain.CompatibleBridgeMessagesToChat}, Discovery: "none", CapabilityProfile: "example-admin-coding"},
			{Mode: "payg", DefaultProtocol: domain.APIProtocolResponses,
				Endpoints: []domain.CompatibleEndpoint{{Protocol: domain.APIProtocolResponses, BaseURL: "https://example.com/v1", Auth: "bearer"}},
				Discovery: "none", CapabilityProfile: "example-admin-payg"},
		},
	}}))
}

func TestCompatibleAdminPlatformBinding(test *testing.T) {
	installAdminCompatibleFixture(test)
	for _, platform := range domain.ConcretePlatforms() {
		require.NoError(test, bindGroupPlatformJSON(test, &CreateAccountRequest{}, fmt.Sprintf(`{"name":"a","platform":%q,"type":"apikey","credentials":{}}`, platform)))
		require.NoError(test, bindGroupPlatformJSON(test, &CreateGroupRequest{}, fmt.Sprintf(`{"name":"g","platform":%q}`, platform)))
		require.NoError(test, bindGroupPlatformJSON(test, &UpdateGroupRequest{}, fmt.Sprintf(`{"platform":%q}`, platform)))
		require.NoError(test, bindGroupPlatformJSON(test, &CompositeRouteRequest{}, fmt.Sprintf(`{"public_model":"m","target_platform":%q}`, platform)))
	}
	for _, platform := range []string{"unknown", "Example-admin", "example-admin ", "vertex", "composite"} {
		require.Error(test, bindGroupPlatformJSON(test, &CreateAccountRequest{}, fmt.Sprintf(`{"name":"a","platform":%q,"type":"apikey","credentials":{}}`, platform)))
		require.Error(test, bindGroupPlatformJSON(test, &CompositeRouteRequest{}, fmt.Sprintf(`{"public_model":"m","target_platform":%q}`, platform)))
	}
	require.NoError(test, bindGroupPlatformJSON(test, &CreateGroupRequest{}, `{"name":"g","platform":"composite"}`))
	require.NoError(test, bindGroupPlatformJSON(test, &CreateGroupRequest{}, `{"name":"g"}`))
	for _, accountType := range []string{"oauth", "setup-token", "upstream", "bedrock", "service_account"} {
		require.Error(test, bindGroupPlatformJSON(test, &CreateAccountRequest{}, fmt.Sprintf(`{"name":"a","platform":"example-admin","type":%q}`, accountType)))
	}
	for _, credentials := range []string{
		`{}`, `{"account_mode":"coding","api_protocol":"adaptive"}`, `{"account_mode":"payg","api_protocol":"responses"}`,
	} {
		require.NoError(test, bindGroupPlatformJSON(test, &CreateAccountRequest{}, `{"name":"a","platform":"example-admin","type":"apikey","credentials":`+credentials+`}`))
	}
	for _, credentials := range []string{
		`{"account_mode":"unknown"}`, `{"account_mode":null}`, `{"account_mode":3}`,
		`{"api_protocol":"unknown"}`, `{"api_protocol":null}`, `{"api_protocol":false}`,
		`{"account_mode":"coding","api_protocol":"responses"}`, `{"account_mode":"payg","api_protocol":"chat_completions"}`,
		`{"api_protocol":"anthropic"}`,
	} {
		require.Error(test, bindGroupPlatformJSON(test, &CreateAccountRequest{}, `{"name":"a","platform":"example-admin","type":"apikey","credentials":`+credentials+`}`))
	}
}

type compatibleAdminCapture struct {
	*stubAdminService
	account service.Account
	updated *service.UpdateAccountInput
	created *service.CreateAccountInput
}

func (capture *compatibleAdminCapture) GetAccount(context.Context, int64) (*service.Account, error) {
	return &capture.account, nil
}
func (capture *compatibleAdminCapture) CreateAccount(_ context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	capture.created = input
	capture.account = service.Account{ID: 1, Platform: input.Platform, Type: input.Type, Credentials: input.Credentials}
	return &capture.account, nil
}
func (capture *compatibleAdminCapture) UpdateAccount(_ context.Context, _ int64, input *service.UpdateAccountInput) (*service.Account, error) {
	capture.updated = input
	return &capture.account, nil
}
func compatibleRequest(handler gin.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(method, "/accounts/1", bytes.NewBufferString(body))
	ginContext.Request.Header.Set("Content-Type", "application/json")
	ginContext.Params = gin.Params{{Key: "id", Value: "1"}}
	handler(ginContext)
	return recorder
}
func TestCompatibleAdminAccountHandlers(test *testing.T) {
	installAdminCompatibleFixture(test)
	svc := &compatibleAdminCapture{stubAdminService: newStubAdminService()}
	handler := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	created := compatibleRequest(handler.Create, http.MethodPost, `{"name":"a","platform":"example-admin","type":"apikey","credentials":{"api_key":"test-secret","account_mode":"coding","api_protocol":"chat_completions","base_url":"https://custom.example/v1","model_mapping":{"local":"remote"},"credential_ref":"ref-1","api_base_urls":{"chat_completions":"https://custom.example/chat","responses":"https://custom.example/responses"}}}`)
	require.Equal(test, http.StatusOK, created.Code, created.Body.String())
	require.Equal(test, "example-admin", svc.created.Platform)
	require.NotContains(test, created.Body.String(), "test-secret")
	before := svc.account.Credentials
	updated := compatibleRequest(handler.Update, http.MethodPut, `{"credentials":{"api_protocol":"adaptive","api_base_urls":{"chat_completions":"https://edited.example/chat"}}}`)
	require.Equal(test, http.StatusOK, updated.Code, updated.Body.String())
	require.Equal(test, "adaptive", svc.updated.Credentials["api_protocol"])
	require.Equal(test, map[string]any{"chat_completions": "https://edited.example/chat", "responses": "https://custom.example/responses"}, svc.updated.Credentials["api_base_urls"])
	require.Equal(test, "https://custom.example/chat", before["api_base_urls"].(map[string]any)["chat_completions"])
	for _, key := range []string{"api_key", "account_mode", "base_url", "model_mapping", "credential_ref"} {
		require.Equal(test, before[key], svc.updated.Credentials[key], key)
	}
	require.Equal(test, "chat_completions", before["api_protocol"], "validation must not mutate persisted credentials")
	for _, body := range []string{`{"type":"oauth"}`, `{"type":"bedrock"}`, `{"type":"service_account"}`, `{"credentials":{"account_mode":"payg"}}`, `{"credentials":{"api_protocol":"responses"}}`} {
		svc.updated = nil
		recorder := compatibleRequest(handler.Update, http.MethodPut, body)
		require.Equal(test, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		require.Nil(test, svc.updated)
		require.NotContains(test, recorder.Body.String(), "test-secret")
	}
	for _, body := range []string{`{"name":"renamed"}`, `{"credentials":{}}`} {
		require.Equal(test, http.StatusOK, compatibleRequest(handler.Update, http.MethodPut, body).Code)
		require.Empty(test, svc.updated.Credentials)
	}
	for _, body := range []string{`{"name":"a","platform":"example-admin","type":"oauth"}`, `{"name":"a","platform":"example-admin","type":"apikey","credentials":{"account_mode":"bad"}}`} {
		svc.created = nil
		require.Equal(test, http.StatusBadRequest, compatibleRequest(handler.Create, http.MethodPost, body).Code)
		require.Nil(test, svc.created)
	}
}

func TestCompatibleAdminFilterAndBudgetIdentity(test *testing.T) {
	installAdminCompatibleFixture(test)
	svc := newStubAdminService()
	handler := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/accounts?platform=example-admin", nil)
	handler.List(ginContext)
	require.Equal(test, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(test, "example-admin", svc.lastListAccounts.platform)
	repo := &upsertCapturingQuotaRepo{}
	userHandler := buildTestHandler(repo, &billingCacheStub{})
	ginContext, recorder = putReq(test, `{"quotas":[{"platform":"example-admin","daily_limit_usd":10}]}`)
	userHandler.UpdateUserPlatformQuotas(ginContext)
	require.Equal(test, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(test, repo.upsertCalls, 1)
	require.Equal(test, "example-admin", repo.upsertCalls[0].records[0].Platform)
}

func TestCompatibleAdminHostedGroupRestrictions(test *testing.T) {
	installAdminCompatibleFixture(test)
	for _, fixture := range []struct {
		platform, accountType string
		valid                 bool
	}{
		{"anthropic", "bedrock", true}, {"anthropic", "service_account", true}, {"gemini", "service_account", true}, {"bedrock", "bedrock", true},
		{"openai", "bedrock", false}, {"bedrock", "service_account", false}, {"example-admin", "bedrock", false}, {"example-admin", "service_account", false}, {"example-admin", "oauth", false},
	} {
		for _, target := range []any{&CreateGroupRequest{}, &UpdateGroupRequest{}, &CompositeRouteRequest{}} {
			body := fmt.Sprintf(`{"name":"g","platform":%q,"target_platform":%q,"public_model":"m","account_type":%q}`, fixture.platform, fixture.platform, fixture.accountType)
			err := bindGroupPlatformJSON(test, target, body)
			if fixture.valid {
				require.NoError(test, err, body)
			} else {
				require.Error(test, err, body)
			}
		}
	}
}

func TestCompatibleAdminCodingOnlyAndResourceBase(test *testing.T) {
	provider := domain.CompatibleProvider{
		ID: "example-resource", Label: "Example", LabelZh: "Example", BaseSource: "account", DefaultMode: "coding",
		EvidenceURL: "https://example.com/docs", EvidenceObserved: "2026-10-03",
		Modes: []domain.CompatibleMode{{Mode: "coding", DefaultProtocol: domain.APIProtocolResponses,
			Endpoints: []domain.CompatibleEndpoint{{Protocol: domain.APIProtocolResponses, Path: "/openai/v1", Auth: "api_key_header"}},
			Discovery: "none", CapabilityProfile: "example-resource"}},
	}
	test.Cleanup(domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{provider}))
	for _, fixture := range []struct {
		credentials string
		valid       bool
	}{
		{`{"base_url":"https://resource.example.com","api_key":"fixture"}`, true},
		{`{"base_url":"https://resource.example.com","account_mode":"payg"}`, false},
		{`{"api_key":"fixture"}`, false}, {`{"base_url":" "}`, false}, {`{"base_url":123}`, false},
	} {
		err := bindGroupPlatformJSON(test, &CreateAccountRequest{}, `{"name":"a","platform":"example-resource","type":"apikey","credentials":`+fixture.credentials+`}`)
		if fixture.valid {
			require.NoError(test, err)
		} else {
			require.Error(test, err)
		}
	}
}

func TestCompatibleAdminGroupHandlers(test *testing.T) {
	installAdminCompatibleFixture(test)
	svc := newStubAdminService()
	handler := NewGroupHandler(svc, nil, nil)
	recorder := compatibleRequest(handler.Create, http.MethodPost, `{"name":"g","platform":"example-admin"}`)
	require.Equal(test, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(test, svc.createdGroups, 1)
	require.Equal(test, "example-admin", svc.createdGroups[0].Platform)
	recorder = compatibleRequest(handler.Update, http.MethodPut, `{"platform":"example-admin"}`)
	require.Equal(test, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(test, svc.updatedGroups, 1)
	require.Equal(test, "example-admin", svc.updatedGroups[0].Platform)
	for _, body := range []string{`{"name":"g","platform":"unknown"}`, `{"name":"g","platform":"example-admin","account_type":"bedrock"}`} {
		require.Equal(test, http.StatusBadRequest, compatibleRequest(handler.Create, http.MethodPost, body).Code)
		require.Equal(test, http.StatusBadRequest, compatibleRequest(handler.Update, http.MethodPut, body).Code)
	}
	require.Len(test, svc.createdGroups, 1)
	require.Len(test, svc.updatedGroups, 1)
}

func TestCompatibleAdminBatchCreateBinding(test *testing.T) {
	installAdminCompatibleFixture(test)
	handler := &AccountHandler{}
	// Invalid nested identities must be rejected before transactions or services.
	recorder := compatibleRequest(handler.BatchCreate, http.MethodPost, `{"accounts":[{"name":"a","platform":"example-admin","type":"oauth","credentials":{}}]}`)
	require.Equal(test, http.StatusBadRequest, recorder.Code, recorder.Body.String())
}
