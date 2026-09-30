//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type postActivationCatalogRepo struct {
	*visiblePolicyAccountRepo
	ownershipReads int
}

func (repo *postActivationCatalogRepo) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	repo.ownershipReads++
	return nil, errors.New("scheduler unavailable; catalog snapshot remains available")
}

func TestPostActivationAggregateCatalogUsesSnapshot(t *testing.T) {
	account := postActivationAccount(t, 50)
	account.GroupIDs = []int64{73}
	account.Credentials["model_mapping"] = map[string]any{"selected-private-alias": "composer-2.5"}
	repo := &postActivationCatalogRepo{visiblePolicyAccountRepo: &visiblePolicyAccountRepo{accounts: []Account{*account}}}
	gateway := &GatewayService{accountRepo: repo}
	groupID := int64(73)
	ctx := WithVisibleModelKeyPolicy(context.Background(), []string{"selected-private-alias"})
	view, metadata, err := gateway.VisibleModelsWithMetadata(ctx, &groupID, PlatformComposite)
	require.NoError(t, err)
	require.Equal(t, []string{"selected-private-alias"}, view.Models)
	require.NotEmpty(t, metadata["selected-private-alias"])
	require.Nil(t, metadata["selected-private-alias"][0].SupportsFunctionCalling, "missing optional capability remains unknown")
	require.Zero(t, repo.ownershipReads, "ownership must use the same scoped membership snapshot")
	for _, optionalMetadata := range []any{nil, "malformed optional metadata"} {
		repo.accounts[0].Extra[UpstreamModelMetadataExtraKey] = optionalMetadata
		view, metadata, err = gateway.VisibleModelsWithMetadata(ctx, &groupID, PlatformComposite)
		require.NoError(t, err)
		require.Equal(t, []string{"selected-private-alias"}, view.Models)
		require.Nil(t, metadata["selected-private-alias"][0].SupportsFunctionCalling)
	}
	view, _, err = gateway.VisibleModelsWithMetadata(WithVisibleModelKeyPolicy(context.Background(), []string{}), &groupID, PlatformComposite)
	require.NoError(t, err)
	require.Empty(t, view.Models, "key deny-all still applies")
}

func postActivationAccount(t *testing.T, id int) *Account {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("testdata/post_activation/account-%d-synthetic-fixture.json", id))
	require.NoError(t, err)
	var account Account
	require.NoError(t, json.Unmarshal(data, &account))
	return &account
}

type postActivationSendRevocationRepo struct {
	*forwardCatalogRepo
}

func (repo *postActivationSendRevocationRepo) GetModelCatalogAccount(ctx context.Context, id int64) (*Account, error) {
	account, err := repo.forwardCatalogRepo.GetModelCatalogAccount(ctx, id)
	if err == nil && repo.reads > 1 {
		revoked := *account
		revoked.Extra = map[string]any{UpstreamModelAllowlistExtraKey: AccountModelAllowlist{Enabled: true}, UpstreamModelCatalogStateExtraKey: account.ModelCatalogState()}
		return &revoked, nil
	}
	return account, err
}

func TestPostActivationNativeCursorEntries(t *testing.T) {
	for _, protocol := range []string{"chat_completions", "responses", "anthropic"} {
		for _, scenario := range []string{"selected", "alias", "revoked_at_send"} {
			t.Run(protocol+"/"+scenario, func(t *testing.T) {
				account := postActivationAccount(t, 50)
				publicModel := "composer-2.5"
				if scenario == "alias" {
					publicModel = "my-composer"
					account.Credentials["model_mapping"].(map[string]any)[publicModel] = "composer-2.5"
				}
				account.Credentials["expires_at"] = time.Now().Add(3 * time.Hour).Format(time.RFC3339)
				oauthRepo := &cursorOAuthMemoryRepo{account: account}
				sessions := ProvideCursorSessionProvider(newCursorMemoryCache(), NewOAuthRefreshAPI(oauthRepo, nil))
				sessions.refresher.refresh = func(context.Context, string) (cursor.OAuthTokens, error) {
					return cursor.OAuthTokens{AccessToken: "synthetic-refreshed", ExpiresAt: time.Now().Add(time.Hour)}, nil
				}
				gateway := NewCursorGatewayService(sessions, nil)
				_, sessionErr := sessions.GetSessionToken(context.Background(), account, "")
				require.NoError(t, sessionErr)
				calls := 0
				gateway.native = &cursor.Client{BaseURL: "https://cursor.invalid", HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					if request.URL.Path != cursor.RunEndpoint {
						return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil))}, nil
					}
					calls++
					header := make([]byte, 5)
					_, err := io.ReadFull(request.Body, header)
					require.NoError(t, err)
					payload := make([]byte, binary.BigEndian.Uint32(header[1:]))
					_, err = io.ReadFull(request.Body, payload)
					require.NoError(t, err)
					require.Contains(t, string(payload), "composer-2.5-fast")
					var frames bytes.Buffer
					for _, encoded := range []string{"0a090a070a0568656c6c6f", "0a067204080c1005"} {
						payload, decodeErr := hex.DecodeString(encoded)
						require.NoError(t, decodeErr)
						frames.WriteByte(0)
						require.NoError(t, binary.Write(&frames, binary.BigEndian, uint32(len(payload))))
						frames.Write(payload)
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/connect+proto"}}, Body: io.NopCloser(bytes.NewReader(frames.Bytes()))}, nil
				})}}
				plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: publicModel, Protocol: protocol})
				plan.catalogRepo = &forwardCatalogRepo{current: account}
				if scenario == "revoked_at_send" {
					plan.catalogRepo = &postActivationSendRevocationRepo{forwardCatalogRepo: &forwardCatalogRepo{current: account}}
				}
				installUnlimitedTPMFixture(plan)
				ctx := WithGatewayRequestPlan(context.Background(), plan)
				recorder := httptest.NewRecorder()
				requestContext, _ := gin.CreateTestContext(recorder)
				requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, nil).WithContext(ctx)
				body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":16}`, publicModel))
				var err error
				switch protocol {
				case "responses":
					_, err = gateway.ForwardAsResponses(ctx, requestContext, account, []byte(fmt.Sprintf(`{"model":%q,"input":"hello"}`, publicModel)))
				case "anthropic":
					_, err = gateway.ForwardAsAnthropic(ctx, requestContext, account, body)
				default:
					_, err = gateway.ForwardAsChatCompletions(ctx, requestContext, account, body)
				}
				if scenario == "revoked_at_send" {
					var failure *UpstreamFailoverError
					require.ErrorAs(t, err, &failure)
					require.Equal(t, ModelBindingRejectedReason, failure.Reason)
					require.Zero(t, calls)
					return
				}
				require.NoError(t, err)
				require.Equal(t, 1, calls)
				require.Contains(t, recorder.Body.String(), "hello")
				require.Equal(t, publicModel, gjson.Get(recorder.Body.String(), "model").String())
			})
		}
	}
}

func TestPostActivationNativeGrokEntries(t *testing.T) {
	for _, protocol := range []string{"chat_completions", "chat_raw", "responses", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			account := postActivationAccount(t, 53)
			account.Credentials["expires_at"] = time.Now().Add(3 * time.Hour).Format(time.RFC3339)
			repo := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{account.ID: account}}}
			upstream := &httpUpstreamRecorder{resp: grokChatBridgeCompletedResponse("resp_fixture", 0)}
			if protocol == "chat_raw" {
				upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(`{"id":"chat_fixture","object":"chat.completion","model":"grok-4.6","choices":[{"index":0,"message":{"role":"assistant","content":"cached ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))}
			}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), accountRepo: repo, httpUpstream: upstream, grokTokenProvider: NewGrokTokenProvider(repo, nil)}
			plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: "grok-4.6", Protocol: protocol})
			installUnlimitedTPMFixture(plan)
			ctx := WithGatewayRequestPlan(context.Background(), plan)
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Set("api_key", &APIKey{ID: 145})
			body := []byte(`{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}],"max_tokens":256,"prompt_cache_key":"fixture-session"}`)
			if protocol == "responses" {
				body = []byte(`{"model":"grok-4.6","input":"hello"}`)
			}
			if protocol == "chat_raw" {
				body = []byte(`{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}],"max_tokens":16}`)
			}
			requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, bytes.NewReader(body)).WithContext(ctx)
			var result *OpenAIForwardResult
			var err error
			switch protocol {
			case "responses":
				result, err = gateway.Forward(ctx, requestContext, account, body)
			case "anthropic":
				result, err = gateway.ForwardAsAnthropic(ctx, requestContext, account, body, "", "")
			default:
				result, err = gateway.ForwardAsChatCompletions(ctx, requestContext, account, body, "", "")
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, "grok-4.6", gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, "grok-4.6", result.UpstreamModel)
			require.Contains(t, recorder.Body.String(), "cached ok")
		})
	}
}

func TestPostActivationGrokDeniedSelectionNeverSends(t *testing.T) {
	for _, protocol := range []string{"chat_completions", "responses", "anthropic"} {
		for _, scenario := range []string{"revoked", "unselected"} {
			t.Run(protocol+"/"+scenario, func(t *testing.T) {
				selected := postActivationAccount(t, 53)
				selected.Credentials["expires_at"] = time.Now().Add(3 * time.Hour).Format(time.RFC3339)
				current := postActivationAccount(t, 53)
				if scenario == "revoked" {
					current.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true}
				}
				model := "grok-4.6"
				if scenario == "unselected" {
					model = "unselected-model"
				}
				repo := &forwardCatalogRepo{current: current}
				upstream := &httpUpstreamRecorder{}
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), accountRepo: repo, httpUpstream: upstream, grokTokenProvider: NewGrokTokenProvider(repo, nil)}
				plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: model, Protocol: protocol})
				installUnlimitedTPMFixture(plan)
				ctx := WithGatewayRequestPlan(context.Background(), plan)
				requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
				requestContext.Set("api_key", &APIKey{ID: 145})
				body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":256}`, model))
				if protocol == "responses" {
					body = []byte(fmt.Sprintf(`{"model":%q,"input":"hello"}`, model))
				}
				requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, bytes.NewReader(body)).WithContext(ctx)
				var err error
				switch protocol {
				case "responses":
					_, err = gateway.Forward(ctx, requestContext, selected, body)
				case "anthropic":
					_, err = gateway.ForwardAsAnthropic(ctx, requestContext, selected, body, "", "")
				default:
					_, err = gateway.ForwardAsChatCompletions(ctx, requestContext, selected, body, "", "")
				}
				require.Error(t, err)
				require.Nil(t, upstream.lastReq, "no rejected selection may reach transport")
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, ModelBindingRejectedReason, failure.Reason)
			})
		}
	}
}

func TestPostActivationForwardBindingPreservesRevocation(t *testing.T) {
	for _, id := range []int{50, 51, 53} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			account := postActivationAccount(t, id)
			model := "composer-2.5"
			if id == 53 {
				model = "grok-4.6"
			}
			selected := *account
			repo := &forwardCatalogRepo{current: account}
			plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: model, Protocol: "chat_completions"})
			installUnlimitedTPMFixture(plan)
			ctx := WithGatewayRequestPlan(context.Background(), plan)
			require.NoError(t, validateAccountForwardModel(ctx, repo, account, model, "chat_completions"), "full stored account")
			require.ErrorIs(t, validateAccountForwardModel(ctx, repo, &selected, "unselected", "chat_completions"), ErrAccountModelUnsupported)
			account.Extra = map[string]any{UpstreamModelAllowlistExtraKey: AccountModelAllowlist{Enabled: true}, UpstreamModelCatalogStateExtraKey: account.ModelCatalogState()}
			require.ErrorIs(t, validateAccountForwardModel(ctx, repo, &selected, model, "chat_completions"), ErrAccountModelUnsupported, "current deny-all selection")
		})
	}
}

func TestPostActivationCursorQuotaAndSelection(t *testing.T) {
	for _, scenario := range []string{"quota", "revoked", "unselected"} {
		for _, protocol := range []string{"chat_completions", "responses", "anthropic"} {
			t.Run(scenario+"/"+protocol, func(t *testing.T) {
				gateway, _, exchanges := cursorTestGateway()
				account := postActivationAccount(t, 51)
				current := postActivationAccount(t, 51)
				if scenario == "revoked" {
					current.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true}
				}
				model := "composer-2.5"
				if scenario == "unselected" {
					model = "composer-2.5-fast"
				}
				calls := 0
				gateway.native = cursorDriverStub{run: func(context.Context, cursor.Credentials, []cursor.ChatMessage, string, func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
					calls++
					return cursor.TokenUsage{}, &cursor.ConnectError{Code: "resource_exhausted", Message: "private upstream detail"}
				}}
				plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: model, Protocol: protocol})
				plan.catalogRepo = &forwardCatalogRepo{current: current}
				installUnlimitedTPMFixture(plan)
				ctx := WithGatewayRequestPlan(context.Background(), plan)
				recorder := httptest.NewRecorder()
				requestContext, _ := gin.CreateTestContext(recorder)
				requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, nil).WithContext(ctx)
				body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":16}`, model))
				var err error
				switch protocol {
				case "responses":
					_, err = gateway.ForwardAsResponses(ctx, requestContext, account, []byte(fmt.Sprintf(`{"model":%q,"input":"hello"}`, model)))
				case "anthropic":
					_, err = gateway.ForwardAsAnthropic(ctx, requestContext, account, body)
				default:
					_, err = gateway.ForwardAsChatCompletions(ctx, requestContext, account, body)
				}
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.False(t, failure.IsCredentialFailure())
				if scenario == "quota" {
					require.Equal(t, 1, calls)
					require.EqualValues(t, 1, exchanges.Load(), "quota does not refresh credentials")
					require.Equal(t, 429, failure.ClientStatusCode)
					require.Equal(t, GatewayFailureReason("resource_exhausted"), failure.Reason)
					require.False(t, failure.RetryableOnSameAccount)
					require.True(t, failure.ShouldRetryNextAccount())
					require.NotContains(t, string(failure.ResponseBody), "private")
				} else {
					require.Zero(t, calls)
					require.Equal(t, ModelBindingRejectedReason, failure.Reason)
				}
			})
		}
	}
}
