package integrationtests_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestCoreModelFlowPostgres is one stored account -> selection -> group -> issued
// credential -> authenticated catalog/inference -> owner usage -> revocation
// scenario. It intentionally needs an exclusive, migrated PostgreSQL job_test;
// compile/skip is not evidence that this chain passed. No t.Parallel: catalog
// environment and the assigned database belong to this scenario for its lifetime.
func TestCoreModelFlowPostgres(t *testing.T) {
	f := newCoreFlowFixture(t)
	created := f.ok("POST", "/api/v1/admin/accounts", map[string]any{
		"name": "core-flow-account", "platform": "openai", "type": "apikey", "concurrency": 4,
		"credentials": map[string]any{"api_key": "upstream-fixture-only", "base_url": f.upstream.server.URL, "model_mapping": map[string]string{}},
		"extra":       map[string]any{"openai_responses_supported": true, "openai_passthrough": false},
	}, false)
	f.accountID = created.Get("data.id").Int()
	require.Positive(t, f.accountID)
	require.True(t, f.savedAccount().Schedulable)
	preview := f.ok("POST", fmt.Sprintf("/api/v1/admin/accounts/%d/models/sync-upstream", f.accountID), nil, false)
	require.Contains(t, preview.Raw, "Discovered-Unselected")
	require.NotContains(t, preview.Raw, corePrivateModel)
	presets := f.ok("GET", "/api/v1/admin/platform-model-presets", nil, false)
	require.NotContains(t, presets.Raw, corePrivateModel, "exact private ID is absent from platform presets too")
	require.NoError(t, f.refresh([]string{corePricedModel, "Discovered-Unselected"}, false))
	selected := map[string]string{corePricedAlias: corePricedModel, corePrivateModel: corePrivateModel}
	f.saveSelection(selected, false, []string{})
	require.Equal(t, selected, f.savedAccount().GetModelMapping(), "selection survived repository reload")

	group := f.ok("POST", "/api/v1/admin/groups", map[string]any{"name": "core-flow-group", "platform": "openai", "rate_multiplier": 1, "subscription_type": "standard",
		"model_pricing": []any{map[string]any{"platform": "openai", "models": []string{corePricedModel}, "billing_mode": "token", "input_price": 4e-6, "output_price": 12e-6}},
	}, false)
	f.groupID = group.Get("data.id").Int()
	require.Positive(t, f.groupID)
	f.ok("PUT", fmt.Sprintf("/api/v1/admin/accounts/%d", f.accountID), map[string]any{"group_ids": []int64{f.groupID}}, false)
	require.Equal(t, []int64{f.groupID}, f.savedAccount().GroupIDs)
	issued := f.ok("POST", "/api/v1/keys", map[string]any{"name": "core-flow-key", "group_id": f.groupID}, false)
	f.keyID, f.secret = issued.Get("data.api_key.id").Int(), issued.Get("data.secret").String()
	require.Positive(t, f.keyID)
	require.True(t, len(f.secret) > 20, "API must issue a usable secret")
	var persistedGroup, persistedOwner int64
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT group_id,user_id FROM api_keys WHERE id=$1`, f.keyID).Scan(&persistedGroup, &persistedOwner))
	require.Equal(t, f.groupID, persistedGroup)
	require.Equal(t, f.userID, persistedOwner)
	require.Equal(t, http.StatusUnauthorized, f.request("GET", "/v1/models", nil, false).Code)
	f.assertModels(corePricedAlias, corePrivateModel)
	f.assertRejected("Discovered-Unselected")
	capabilities := f.ok("GET", "/v1/models/catalog", nil, true)
	privateCapabilitiesFound := false
	for _, entry := range capabilities.Get("data").Array() {
		if entry.Get("id").String() != corePrivateModel {
			continue
		}
		privateCapabilitiesFound = true
		for _, field := range []string{"input_context", "max_output_tokens", "supports_vision", "reasoning"} {
			fact := entry.Get("capabilities." + field)
			require.True(t, fact.Get("unknown").Bool(), field)
			require.Equal(t, gjson.Null, fact.Get("guaranteed").Type, field)
			require.Empty(t, fact.Get("available").Array(), field)
		}
	}
	require.True(t, privateCapabilitiesFound)
	t.Log("API account creation, discovery preview, atomic selection, grouping, secret issuance and authenticated catalogs passed")

	// Same credential exercises real response JSON/SSE parsing and receipt writes.
	for _, scenario := range []struct {
		name, model     string
		stream, missing bool
	}{
		{"private_json", corePrivateModel, false, false},
		{"private_sse", corePrivateModel, true, false},
		{"private_missing_json", corePrivateModel, false, true},
		{"private_missing_sse", corePrivateModel, true, true},
		{"priced_json", corePricedAlias, false, false},
		{"priced_sse", corePricedAlias, true, false},
		{"priced_missing_json", corePricedAlias, false, true},
		{"priced_missing_sse", corePricedAlias, true, true},
	} {
		if !t.Run(scenario.name, func(t *testing.T) {
			parentTest := f.t
			f.t = t
			defer func() { f.t = parentTest }()
			input := "hello"
			if scenario.missing {
				input = "omit declaration"
			}
			before := len(f.upstream.calls())
			response := f.request("POST", "/v1/responses", map[string]any{"model": scenario.model, "input": input, "stream": scenario.stream}, true)
			require.Equal(t, 200, response.Code, response.Body.String())
			require.Len(t, f.upstream.calls(), before+1)
			wireModel := scenario.model
			if wireModel == corePricedAlias {
				wireModel = corePricedModel
			}
			require.Equal(t, wireModel, f.upstream.calls()[before], "exact upstream wire ID including case/suffix")
			if scenario.stream {
				require.Contains(t, response.Header().Get("Content-Type"), "text/event-stream")
				require.Contains(t, response.Body.String(), "response.completed")
			}
			var receiptID int64
			require.Eventually(t, func() bool {
				var count int
				err := f.db.QueryRowContext(f.ctx, `SELECT count(*),coalesce(max(id),0) FROM usage_settlement_receipts WHERE api_key_id=$1`, f.keyID).Scan(&count, &receiptID)
				return err == nil && count == before+1
			}, 5*time.Second, 20*time.Millisecond, "one durable receipt per completed request")
			var usageID sql.NullInt64
			var receiptState, pricingState, usageJSON string
			require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT state,pricing_state,usage_json::text,usage_log_id FROM usage_settlement_receipts WHERE id=$1`, receiptID).Scan(&receiptState, &pricingState, &usageJSON, &usageID))
			var evidence service.UsageLog
			require.NoError(t, json.Unmarshal([]byte(usageJSON), &evidence))
			require.Equal(t, scenario.model, evidence.RequestedModel)
			require.Equal(t, 100, evidence.InputTokens)
			require.Equal(t, 20, evidence.OutputTokens)
			require.Equal(t, scenario.stream, evidence.Stream)
			if scenario.missing {
				require.Nil(t, evidence.UpstreamResponseModel)
			} else {
				require.NotNil(t, evidence.UpstreamResponseModel)
				require.Equal(t, wireModel+"-Declared", *evidence.UpstreamResponseModel)
			}
			if scenario.model == corePrivateModel {
				// Existing billing contract deliberately retains unpriced evidence
				// for review instead of inventing a free settled usage row.
				require.Equal(t, "needs_review", receiptState)
				require.Equal(t, "unpriced", pricingState)
				require.False(t, usageID.Valid)
				require.Equal(t, corePrivateModel, evidence.Model)
				user, err := f.users.GetByID(f.ctx, f.userID)
				require.NoError(t, err)
				require.Equal(t, float64(100), user.Balance)
				return
			}
			require.Equal(t, "settled", receiptState)
			require.Equal(t, "priced", pricingState)
			require.True(t, usageID.Valid)
			require.NotNil(t, evidence.UpstreamModel)
			require.Equal(t, wireModel, *evidence.UpstreamModel)
			var raw string
			require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT row_to_json(u)::text FROM usage_logs u WHERE id=$1`, usageID.Int64).Scan(&raw))
			stored := gjson.Parse(raw)
			require.Equal(t, scenario.model, stored.Get("requested_model").String())
			require.Equal(t, wireModel, stored.Get("upstream_model").String())
			require.EqualValues(t, 100, stored.Get("input_tokens").Int())
			require.EqualValues(t, 20, stored.Get("output_tokens").Int())
			if scenario.missing {
				require.Equal(t, gjson.Null, stored.Get("upstream_response_model").Type)
			} else {
				require.Equal(t, wireModel+"-Declared", stored.Get("upstream_response_model").String())
			}
			// Explicit group price: $4/M input + $12/M output; rate 1.
			require.InDelta(t, 100*4e-6+20*12e-6, stored.Get("actual_cost").Float(), 1e-9)
			owner := f.ok("GET", fmt.Sprintf("/api/v1/usage/%d", usageID.Int64), nil, false).Get("data")
			require.Equal(t, scenario.model, owner.Get("model").String(), "owner DTO exposes requested model as model")
			require.Equal(t, stored.Get("upstream_response_model").String(), owner.Get("upstream_response_model").String())
			if scenario.missing {
				require.False(t, owner.Get("upstream_response_model").Exists())
				require.False(t, owner.Get("upstream_model_mismatch").Exists())
			} else {
				require.True(t, owner.Get("upstream_model_mismatch").Bool())
			}
			require.False(t, owner.Get("upstream_model").Exists(), "wire ID remains admin-only")
			otherRequest := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/usage/%d", usageID.Int64), nil)
			otherRequest.Header.Set("X-Fixture-Other-User", "true")
			otherResponse := httptest.NewRecorder()
			f.router.ServeHTTP(otherResponse, otherRequest)
			require.Equal(t, http.StatusNotFound, otherResponse.Code)
		}) {
			return
		}
	}
	list := f.ok("GET", "/api/v1/usage", nil, false)
	require.Len(t, list.Get("data.items").Array(), 4)
	adminList := f.ok("GET", fmt.Sprintf("/api/v1/admin/usage?api_key_id=%d", f.keyID), nil, false)
	require.Len(t, adminList.Get("data.items").Array(), 4)
	for _, entry := range adminList.Get("data.items").Array() {
		require.Equal(t, corePricedModel, entry.Get("upstream_model").String(), "wire model is exposed only to admin")
		require.Equal(t, corePricedAlias, entry.Get("model").String())
	}
	user, err := f.users.GetByID(f.ctx, f.userID)
	require.NoError(t, err)
	require.InDelta(t, 100-4*(100*4e-6+20*12e-6), user.Balance, 1e-9)

	for _, refresh := range []struct {
		name   string
		models []string
		fail   bool
	}{
		{"new candidate", []string{corePricedModel, "Discovered-Unselected", "New-Unselected"}, false},
		{"selected missing from discovery", []string{"New-Unselected"}, false},
		{"discovery failure", nil, true},
	} {
		err := f.refresh(refresh.models, refresh.fail)
		if refresh.fail {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, selected, f.savedAccount().GetModelMapping(), refresh.name)
		f.assertModels(corePricedAlias, corePrivateModel)
		f.assertSuccessfulRequest(corePrivateModel)
	}
	// Warm catalogs above must not hide deletion or let the next request through.
	f.saveSelection(map[string]string{corePrivateModel: corePrivateModel}, false, []string{})
	f.assertModels(corePrivateModel)
	f.assertRejected(corePricedAlias)
	f.assertSuccessfulRequest(corePrivateModel)
	f.saveSelection(selected, false, []string{})
	f.assertModels(corePricedAlias, corePrivateModel)
	f.saveSelection(nil, true, []string{corePricedModel})
	f.assertModels(corePricedAlias)
	f.assertRejected(corePrivateModel)
	f.assertSuccessfulRequest(corePricedAlias)
	var receipts, settled, unpriced, free int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*),count(*) FILTER (WHERE state='settled'),count(*) FILTER (WHERE state='needs_review' AND pricing_state='unpriced'),count(*) FILTER (WHERE pricing_state='free') FROM usage_settlement_receipts WHERE api_key_id=$1`, f.keyID).Scan(&receipts, &settled, &unpriced, &free))
	require.Equal(t, 13, receipts)
	require.Equal(t, 5, settled)
	require.Equal(t, 8, unpriced)
	require.Zero(t, free, "unknown prices must never be represented as free")
	user, err = f.users.GetByID(f.ctx, f.userID)
	require.NoError(t, err)
	require.InDelta(t, 100-5*(100*4e-6+20*12e-6), user.Balance, 1e-9)
	cacheWarmed := false
	for _, cacheKey := range f.redisServer.Keys() {
		if strings.HasPrefix(cacheKey, "apikey:auth:") {
			cacheWarmed = true
		}
	}
	require.True(t, cacheWarmed, "revocation must be exercised with an existing auth cache entry")
	beforeRevoke := len(f.upstream.calls())
	f.ok("DELETE", fmt.Sprintf("/api/v1/keys/%d", f.keyID), nil, false)
	for _, path := range []string{"/v1/models", "/v1/models/catalog"} {
		require.Equal(t, http.StatusUnauthorized, f.request("GET", path, nil, true).Code)
	}
	require.Equal(t, http.StatusUnauthorized, f.request("POST", "/v1/responses", map[string]any{"model": corePricedAlias, "input": "revoked"}, true).Code)
	require.Len(t, f.upstream.calls(), beforeRevoke)
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM usage_settlement_receipts WHERE api_key_id=$1`, f.keyID).Scan(&receipts))
	require.Equal(t, 13, receipts, "rejected requests must not create inference receipts")
	t.Log("refresh invariance, deletion, allowlist narrowing, warmed-key revocation and owner isolation passed")
}

func (f *coreFlowFixture) assertModels(expected ...string) {
	f.t.Helper()
	for _, path := range []string{"/v1/models", "/v1/models/catalog"} {
		response := f.ok("GET", path, nil, true)
		models := []string{}
		for _, model := range response.Get("data").Array() {
			models = append(models, model.Get("id").String())
		}
		require.ElementsMatch(f.t, expected, models, path)
	}
}

func (f *coreFlowFixture) assertRejected(model string) {
	f.t.Helper()
	before := len(f.upstream.calls())
	response := f.request("POST", "/v1/responses", map[string]any{"model": model, "input": "denied", "stream": false}, true)
	require.Equal(f.t, http.StatusNotFound, response.Code, response.Body.String())
	require.Equal(f.t, "not_found_error", gjson.GetBytes(response.Body.Bytes(), "error.type").String())
	require.Equal(f.t, "model_not_found", gjson.GetBytes(response.Body.Bytes(), "error.code").String())
	require.Len(f.t, f.upstream.calls(), before, "rejected model must not reach upstream")
}

func (f *coreFlowFixture) assertSuccessfulRequest(model string) {
	f.t.Helper()
	before := len(f.upstream.calls())
	response := f.request("POST", "/v1/responses", map[string]any{"model": model, "input": "after refresh or narrowing", "stream": false}, true)
	require.Equal(f.t, http.StatusOK, response.Code, response.Body.String())
	require.Len(f.t, f.upstream.calls(), before+1)
	wireModel := model
	if model == corePricedAlias {
		wireModel = corePricedModel
	}
	require.Equal(f.t, wireModel, f.upstream.calls()[before])
}

// Compile-time guard against accidentally replacing the real repository with a
// fixture implementation while extending this chain.
var _ service.HTTPUpstream = (*coreFlowUpstream)(nil)
