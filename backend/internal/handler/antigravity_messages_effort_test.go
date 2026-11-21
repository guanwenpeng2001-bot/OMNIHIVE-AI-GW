//go:build unit

package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type messagesEffortUpstream struct {
	service.HTTPUpstream
	bodies [][]byte
}

func (upstream *messagesEffortUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	upstream.bodies = append(upstream.bodies, body)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1}}}\n\n"))}, nil
}

func TestAntigravityMessagesEffortPublicBranches(t *testing.T) {
	for _, platform := range []string{service.PlatformGemini, service.PlatformAntigravity, service.PlatformComposite} {
		for _, stream := range []bool{false, true} {
			for _, scenario := range []struct {
				name, model, target, controls, wantModel, wantLevel string
				invalid                                             bool
			}{
				{"alias_low", "public-flash", "gemini-3-flash-high", `"output_config":{"effort":"low"}`, "gemini-3-flash-low", "LOW", false},
				{"policy_denied", "public-flash", "gemini-3-flash-high", `"output_config":{"effort":"low"}`, "", "", false},
				{"alias_medium", "public-flash", "gemini-3-flash-high", `"output_config":{"effort":"medium"}`, "gemini-3-flash-medium", "MEDIUM", false},
				{"alias_high", "public-flash", "gemini-3-flash-low", `"output_config":{"effort":"high"}`, "gemini-3-flash-high", "HIGH", false},
				{"suffix_low", "gemini-3-flash-high", "gemini-3-flash-high", `"output_config":{"effort":"low"}`, "gemini-3-flash-low", "LOW", false},
				{"tiered_medium", "gemini-3-flash-tiered", "gemini-3-flash-tiered", `"output_config":{"effort":"medium"}`, "gemini-3-flash-medium", "MEDIUM", false},
				{"bare", "gemini-3-flash", "gemini-3-flash", `"output_config":{"effort":"low"}`, "gemini-3-flash", "LOW", false},
				{"bare_catalog", "gemini-3-flash", "", `"output_config":{"effort":"low"}`, "gemini-3-flash-low", "LOW", false},
				{"bare_catalog_legacy", "gemini-3-flash", "", `"temperature":0.5`, "gemini-3-flash-high", "", false},
				{"unknown_exact", "gemini-future-flash", "gemini-future-flash", `"output_config":{"effort":"medium"}`, "gemini-future-flash", "MEDIUM", false},
				{"custom_suffix", "gemini-future-flash-custom", "gemini-future-flash-custom", `"output_config":{"effort":"low"}`, "gemini-future-flash-custom", "LOW", false},
				{"mapped_family", "gemini-other-high", "gemini-3-flash-high", `"output_config":{"effort":"low"}`, "gemini-3-flash-low", "LOW", false},
				{"non_gemini_alias", "gemini-public-high", "claude-sonnet-4-5", `"output_config":{"effort":"low"}`, "claude-sonnet-4-5", "", false},
				{"legacy", "public-flash", "gemini-3-flash-high", `"temperature":0.5`, "gemini-3-flash-high", "", false},
				{"legacy_budget", "public-flash", "gemini-3-flash-high", `"thinking":{"type":"enabled","budget_tokens":4000}`, "gemini-3-flash-high", "", false},
				{"adaptive", "public-flash", "gemini-3-flash-high", `"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}`, "gemini-3-flash-low", "LOW", false},
				{"xhigh", "public-flash", "gemini-3-flash-high", `"output_config":{"effort":"xhigh"}`, "", "", true},
				{"max", "public-flash", "gemini-3-flash-high", `"output_config":{"effort":"max"}`, "", "", true},
				{"invalid", "public-flash", "gemini-3-flash-high", `"output_config":{"effort":"banana"}`, "", "", true},
				{"minimal", "public-flash", "gemini-3-flash-high", `"output_config":{"effort":"minimal"}`, "", "", true},
				{"null", "public-flash", "gemini-3-flash-high", `"output_config":{"effort":null}`, "", "", true},
				{"object", "public-flash", "gemini-3-flash-high", `"output_config":{"effort":{}}`, "", "", true},
				{"web_search_downgrade", "public-flash", "gemini-3-flash-high", `"output_config":{"effort":"low"},"tools":[{"type":"web_search_20250305","name":"web_search"}]`, "", "", true},
				{"budget_conflict", "public-flash", "gemini-3-flash-high", `"thinking":{"type":"enabled","budget_tokens":1024},"output_config":{"effort":"low"}`, "", "", true},
				{"disabled_conflict", "public-flash", "gemini-3-flash-high", `"thinking":{"type":"disabled"},"output_config":{"effort":"low"}`, "", "", true},
			} {
				t.Run(fmt.Sprintf("%s/stream=%t/%s", platform, stream, scenario.name), func(t *testing.T) {
					cfg := &config.Config{RunMode: config.RunModeSimple}
					cfg.Gateway.MaxLineSize = 1024 * 1024
					group := &service.Group{ID: 3, Platform: platform, Status: service.StatusActive, Hydrated: true}
					mapping := map[string]any{scenario.model: scenario.target}
					if scenario.target == "" {
						delete(mapping, scenario.model)
					}
					models := []string{scenario.target, "gemini-3-flash-low", "gemini-3-flash-medium", "gemini-3-flash-high"}
					catalog := service.AccountModelCatalogSnapshot{Complete: true}
					for _, model := range models {
						if model == "" {
							continue
						}
						mapping[model] = model
						catalog.DiscoveredModels = append(catalog.DiscoveredModels, model)
						catalog.EligibleModels = append(catalog.EligibleModels, service.AccountCatalogModel{ID: model, Endpoints: []string{"generate_content"}, EvidenceKind: "fixture"})
					}
					account := preparedGatewayFixtureAccount(service.Account{ID: 7, Name: "effort-fixture", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "fixture", "project_id": "fixture-project", "model_mapping": mapping}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: group.ID}}})
					account.Extra[service.UpstreamModelCatalogExtraKey] = catalog
					account.Extra["mixed_scheduling"] = true
					accounts := []*service.Account{&account}
					handler, cleanup := newTestGatewayHandler(t, group, accounts)
					t.Cleanup(cleanup)
					handler.cfg = cfg
					usage := &cursorRouteUsageRepo{}
					snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
					handler.gatewayService = service.ProvideGatewayService(nil, &fakeGroupRepo{group: group}, usage, nil, nil, nil, nil, nil, cfg, snapshot, nil, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
					upstream := &messagesEffortUpstream{}
					handler.antigravityGatewayService = service.NewAntigravityGatewayService(nil, nil, nil, &service.AntigravityTokenProvider{}, nil, upstream, service.NewSettingService(&ingressRejectSettingRepo{}, cfg), nil)
					body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":%t,%s}`, scenario.model, stream, scenario.controls)
					client, recorder := requestPlanHTTPContext("/v1/messages", body, group)
					policyModels := []string{scenario.model}
					if scenario.name == "policy_denied" {
						policyModels = []string{"gemini-3-flash-low"}
					}
					requestContext := service.WithVisibleModelKeyPolicy(client.Request.Context(), policyModels)
					if platform == service.PlatformComposite {
						requestContext = service.WithResolvedTargetPlatform(requestContext, service.PlatformAntigravity)
					}
					client.Request = client.Request.WithContext(requestContext)
					handler.Messages(client)
					if scenario.name == "policy_denied" {
						require.Len(t, upstream.bodies, 0)
						require.GreaterOrEqual(t, recorder.Code, 400)
						return
					}
					if scenario.invalid {
						require.Len(t, upstream.bodies, 0, "invalid effort must never reach upstream")
						require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
						require.Contains(t, recorder.Body.String(), "output_config.effort")
						return
					}
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					require.Len(t, upstream.bodies, 1)
					captured := upstream.bodies[0]
					require.Equal(t, scenario.wantModel, gjson.GetBytes(captured, "model").String(), string(captured))
					require.Equal(t, scenario.wantLevel, gjson.GetBytes(captured, "request.generationConfig.thinkingConfig.thinkingLevel").String(), string(captured))
					require.True(t, gjson.GetBytes(captured, "request.contents").IsArray(), string(captured))
					require.False(t, gjson.GetBytes(captured, "request.messages").Exists())
					if scenario.wantLevel != "" {
						require.False(t, gjson.GetBytes(captured, "request.generationConfig.thinkingConfig.thinkingBudget").Exists())
					}
					if scenario.name == "legacy_budget" {
						require.EqualValues(t, 4000, gjson.GetBytes(captured, "request.generationConfig.thinkingConfig.thinkingBudget").Int())
					}
					plan := service.GatewayRequestPlanFromContext(client.Request.Context())
					require.Equal(t, scenario.model, plan.Snapshot().Model)
					require.True(t, plan.State().Finalized)
					require.True(t, plan.State().UsageClaimed)
					require.Len(t, usage.logs, 1)
					require.Equal(t, scenario.model, usage.logs[0].RequestedModel)
					require.NotNil(t, usage.logs[0].UpstreamModel)
					require.Equal(t, scenario.wantModel, *usage.logs[0].UpstreamModel)
					if scenario.wantLevel != "" {
						require.NotNil(t, usage.logs[0].ReasoningEffort)
						require.Equal(t, strings.ToLower(scenario.wantLevel), *usage.logs[0].ReasoningEffort)
					}
					if stream {
						require.Contains(t, recorder.Body.String(), "event: message_start")
					} else {
						var response map[string]any
						require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
						require.Equal(t, "message", response["type"])
						require.Equal(t, scenario.model, response["model"])
					}
				})
			}
		}
	}
}
