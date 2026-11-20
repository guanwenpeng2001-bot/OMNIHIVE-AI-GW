//go:build unit

package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const commandCodeCreditFixture = `{"error":{"type":"invalid_request_error","message":"You have insufficient credits to make this request. Please purchase more credits to continue using the service."}}`
const commandCodeCreditModel = "deepseek/deepseek-v4.1-flash"

type commandCodeCreditReceiptRepo struct {
	*unlimitedTPMReceiptStub
	receipts []*service.UsageSettlementReceipt
}

func (repo *commandCodeCreditReceiptRepo) Persist(_ context.Context, receipt *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	repo.receipts = append(repo.receipts, receipt)
	return receipt, nil
}

type commandCodeCreditTransport struct {
	service.HTTPUpstream
	hits             []int64
	models           []string
	allFail          bool
	firstBody        string
	firstStatus      int
	usage            *cursorRouteUsageRepo
	receipts         *commandCodeCreditReceiptRepo
	native           string
	transportFailure bool
	partialStream    bool
}

func (upstream *commandCodeCreditTransport) Do(request *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(request.Body)
	upstream.hits = append(upstream.hits, accountID)
	upstream.models = append(upstream.models, gjson.GetBytes(body, "model").String())
	if upstream.transportFailure {
		return nil, errors.New("synthetic transport acceptance unknown")
	}
	if upstream.partialStream {
		partial := "data: {\"id\":\"partial_credit\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek/deepseek-v4.1-flash\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"accepted partial output\"},\"finish_reason\":null}]}\n\ndata: " + commandCodeCreditFixture + "\n\n"
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(partial))}, nil
	}
	if accountID == 47 || upstream.allFail {
		status := upstream.firstStatus
		if status == 0 {
			status = 400
		}
		errorBody := upstream.firstBody
		if errorBody == "" {
			errorBody = commandCodeCreditFixture
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(errorBody))}, nil
	}
	response := `{"id":"cc_success","object":"chat.completion","model":"deepseek/deepseek-v4.1-flash","choices":[{"index":0,"message":{"role":"assistant","content":"credit fallback OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	contentType := "application/json"
	if gjson.GetBytes(body, "stream").Bool() {
		contentType = "text/event-stream"
		response = "data: {\"id\":\"cc_success\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek/deepseek-v4.1-flash\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"credit fallback OK\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"cc_success\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n"
	}
	if strings.HasSuffix(request.URL.Path, "/responses") {
		response = `{"id":"resp_credit_success","object":"response","model":"deepseek/deepseek-v4.1-flash","status":"completed","output":[{"id":"msg_credit_success","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"credit fallback OK"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`
		if gjson.GetBytes(body, "stream").Bool() {
			response = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + response + "}\n\n"
		}
	}
	if strings.HasSuffix(request.URL.Path, "/messages") {
		response = `{"id":"msg_credit_success","type":"message","role":"assistant","model":"deepseek/deepseek-v4.1-flash","content":[{"type":"text","text":"credit fallback OK"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`
		if gjson.GetBytes(body, "stream").Bool() {
			response = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_credit_success\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"deepseek/deepseek-v4.1-flash\",\"content\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"credit fallback OK\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
		}
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(response))}, nil
}

func commandCodeCreditRouter(t *testing.T, upstream *commandCodeCreditTransport, platform string) (*gin.Engine, *grokCredentialHandlerRepo, func()) {
	t.Helper()
	repo := &grokCredentialHandlerRepo{}
	for _, id := range []int64{47, 48} {
		repo.accounts = append(repo.accounts, service.Account{ID: id, Name: fmt.Sprint(id), Platform: service.PlatformCommandCode, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Priority: int(id), Concurrency: 1, Credentials: map[string]any{"api_key": "synthetic-key", "model_mapping": map[string]any{commandCodeCreditModel: commandCodeCreditModel}}})
	}
	if upstream.native != "" {
		for index := range repo.accounts {
			repo.accounts[index].Credentials["api_protocol"] = upstream.native
		}
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxAccountSwitches = 3
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	upstream.usage = &cursorRouteUsageRepo{}
	upstream.receipts = &commandCodeCreditReceiptRepo{unlimitedTPMReceiptStub: &unlimitedTPMReceiptStub{}}
	gateway := service.ProvideOpenAIGatewayService(repo, upstream.usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(upstream.receipts, nil, nil))
	cache := &concurrencyCacheMock{acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }, acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }}
	handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(cache), billingCache, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
	groupID := int64(67)
	apiKey := &service.APIKey{ID: 143, GroupID: &groupID, User: &service.User{ID: 1, Status: service.StatusActive}, Group: &service.Group{ID: groupID, Platform: platform, Status: service.StatusActive}}
	router := gin.New()
	router.Use(func(requestContext *gin.Context) {
		requestContext.Set(string(middleware.ContextKeyAPIKey), apiKey)
		requestContext.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
		if platform == service.PlatformComposite {
			decision := service.CompositeRouteDecision{Matched: true, Source: service.CompositeRouteSourceExplicit, GroupID: groupID, PublicModel: commandCodeCreditModel, TargetPlatform: service.PlatformCommandCode, UpstreamModel: commandCodeCreditModel}
			requestContext.Request = requestContext.Request.WithContext(service.WithCompositeRouteDecision(requestContext.Request.Context(), decision))
		}
		requestContext.Next()
	})
	router.POST("/v1/chat/completions", handler.ChatCompletions)
	router.POST("/v1/responses", handler.Responses)
	router.POST("/v1/messages", handler.Messages)
	return router, repo, billingCache.Stop
}

func TestCommandCodeCreditHandlerFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformCommandCode, service.PlatformComposite} {
		for _, native := range []string{"", service.APIProtocolResponses, service.APIProtocolAnthropic} {
			for _, endpoint := range []string{"chat/completions", "responses", "messages"} {
				for _, stream := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/native=%s/stream=%t", platform, endpoint, native, stream), func(t *testing.T) {
						upstream := &commandCodeCreditTransport{native: native}
						router, repo, cleanup := commandCodeCreditRouter(t, upstream, platform)
						defer cleanup()
						body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":%t}`, commandCodeCreditModel, stream)
						if endpoint == "responses" {
							body = fmt.Sprintf(`{"model":%q,"input":"hello","stream":%t}`, commandCodeCreditModel, stream)
						}
						recorder := httptest.NewRecorder()
						request := httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, bytes.NewBufferString(body))
						request.Header.Set("Content-Type", "application/json")
						router.ServeHTTP(recorder, request)
						require.Equal(t, 200, recorder.Code, recorder.Body.String())
						require.Contains(t, recorder.Body.String(), "credit fallback OK")
						require.Equal(t, []int64{47, 48}, upstream.hits)
						require.Equal(t, []string{commandCodeCreditModel, commandCodeCreditModel}, upstream.models)
						require.Empty(t, repo.setErrorIDs)
						require.Empty(t, repo.setTempIDs)
						require.Empty(t, repo.rateLimitIDs)
						require.Len(t, upstream.receipts.receipts, 1, "only the successful attempt persists a usage receipt")
						require.Equal(t, int64(48), upstream.receipts.receipts[0].Usage.AccountID)
						require.Empty(t, upstream.usage.logs, "unknown fixture pricing is retained for review, not invented as a priced usage row")
						for _, account := range repo.accounts {
							require.True(t, account.Schedulable)
							require.Equal(t, service.StatusActive, account.Status)
						}
					})
				}
			}
		}
	}
}

func TestCommandCodeCreditHandlerFailureBoundaries(t *testing.T) {
	for _, endpoint := range []string{"chat/completions", "responses", "messages"} {
		for _, scenario := range []string{"all_credit_failed", "only_first_selected", "ordinary_bad_request", "transport_unknown"} {
			t.Run(endpoint+"/"+scenario, func(t *testing.T) {
				upstream := &commandCodeCreditTransport{allFail: scenario == "all_credit_failed", transportFailure: scenario == "transport_unknown"}
				if scenario == "ordinary_bad_request" {
					upstream.firstBody = `{"error":{"type":"invalid_request_error","message":"invalid messages"}}`
				}
				router, repo, cleanup := commandCodeCreditRouter(t, upstream, service.PlatformCommandCode)
				defer cleanup()
				if scenario == "only_first_selected" {
					repo.accounts[1].Credentials["model_mapping"] = map[string]any{"unrelated": "unrelated"}
				}
				body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":32}`, commandCodeCreditModel)
				if endpoint == "responses" {
					body = fmt.Sprintf(`{"model":%q,"input":"hello"}`, commandCodeCreditModel)
				}
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, bytes.NewBufferString(body))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(recorder, request)
				require.Empty(t, upstream.usage.logs, "failed attempts never record success usage")
				require.Empty(t, upstream.receipts.receipts, "failed credit attempts do not create billable receipts")
				require.Empty(t, repo.setErrorIDs)
				require.Empty(t, repo.setTempIDs)
				if scenario == "all_credit_failed" || scenario == "only_first_selected" {
					if scenario == "all_credit_failed" {
						require.Equal(t, []int64{47, 48}, upstream.hits)
					} else {
						require.Equal(t, []int64{47}, upstream.hits)
					}
					require.Equal(t, 400, recorder.Code)
					require.JSONEq(t, commandCodeCreditFixture, recorder.Body.String())
				} else {
					require.Equal(t, []int64{47}, upstream.hits)
					if scenario == "ordinary_bad_request" {
						require.Equal(t, 400, recorder.Code)
					} else {
						require.Equal(t, 502, recorder.Code)
					}
				}
			})
		}
	}
}

func TestCommandCodeCreditAfterStreamOutputDoesNotReplay(t *testing.T) {
	for _, endpoint := range []string{"chat/completions", "responses", "messages"} {
		t.Run(endpoint, func(t *testing.T) {
			upstream := &commandCodeCreditTransport{partialStream: true}
			router, _, cleanup := commandCodeCreditRouter(t, upstream, service.PlatformCommandCode)
			defer cleanup()
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":true}`, commandCodeCreditModel)
			if endpoint == "responses" {
				body = fmt.Sprintf(`{"model":%q,"input":"hello","stream":true}`, commandCodeCreditModel)
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, bytes.NewBufferString(body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			require.Equal(t, []int64{47}, upstream.hits, "a later body error cannot replay an accepted streamed request")
			require.Contains(t, recorder.Body.String(), "accepted partial output")
			require.LessOrEqual(t, len(upstream.receipts.receipts), 1)
		})
	}
}
