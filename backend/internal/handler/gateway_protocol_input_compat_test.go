//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type protocolInputReceiptRepository struct {
	unlimitedTPMReceiptStub
	receipt *service.UsageSettlementReceipt
}

func (s *protocolInputReceiptRepository) Persist(_ context.Context, receipt *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	copy := *receipt
	s.receipt = &copy
	return s.receipt, nil
}

type protocolInputUpstream struct {
	service.HTTPUpstream
	plan  *service.GatewayRequestPlan
	body  []byte
	calls int
}

func (s *protocolInputUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.calls++
	s.plan = service.GatewayRequestPlanFromContext(request.Context())
	var err error
	s.body, err = io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	body := `{"id":"resp_protocol_input","model":"protocol-input-fixture","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":5,"output_tokens":2}}`
	contentType := "application/json"
	if gjson.GetBytes(s.body, "stream").Bool() {
		contentType = "text/event-stream"
		body = "data: {\"type\":\"response.completed\",\"response\":" + body + "}\n\ndata: [DONE]\n\n"
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

// An intentionally unpriced model exercises the mandatory review receipt instead
// of inventing pricing facts or bypassing billing with a successful usage row.
func TestGatewayProtocolInputCompatibilityRetainsRequestPlanAndReceipt(t *testing.T) {
	for _, endpoint := range []string{"messages", "chat/completions", "responses"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://api.deepseek.com", "model_mapping": map[string]any{"protocol-input-fixture": "protocol-input-fixture"}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
			account.Extra["openai_responses_supported"] = true
			repository := &openAIWSUsageHandlerAccountRepoStub{account: account}
			usage := &cursorRouteUsageRepo{}
			upstream := &protocolInputUpstream{}
			receipts := &protocolInputReceiptRepository{}
			gateway := service.ProvideOpenAIGatewayService(repository, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			defer billing.Stop()
			handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			group := &service.Group{ID: 3, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, AllowMessagesDispatch: true}
			body := `{"model":"protocol-input-fixture","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":false,"tools":[{"name":"lookup","input_schema":{"type":"object","required":null,"properties":{"payload":{"default":{"required":null}}}}}]}`
			if endpoint == "chat/completions" {
				body = `{"model":"protocol-input-fixture","messages":[{"role":"user","content":"hello"}],"stream":false}`
			}
			if endpoint == "responses" {
				body = `{"model":"protocol-input-fixture","input":[{"type":"message","id":"msg_` + strings.Repeat("x", 65) + `","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AQID"}]}],"stream":false}`
			}
			requestContext, recorder := requestPlanHTTPContext("/v1/"+endpoint, body, group)
			switch endpoint {
			case "messages":
				handler.Messages(requestContext)
			case "chat/completions":
				handler.ChatCompletions(requestContext)
			default:
				handler.Responses(requestContext)
			}
			require.Equal(t, 200, recorder.Code, recorder.Body.String())
			require.Equal(t, 1, upstream.calls)
			require.NotNil(t, upstream.plan)
			require.Len(t, upstream.plan.Attempts(), 1)
			require.True(t, upstream.plan.State().Finalized)
			require.True(t, upstream.plan.State().UsageClaimed)
			require.NotNil(t, receipts.receipt)
			require.Equal(t, service.UsageSettlementNeedsReview, receipts.receipt.State)
			require.Equal(t, receipts.receipt.Key, upstream.plan.State().ReceiptKey)
			require.EqualValues(t, 2, receipts.receipt.Key.APIKeyID)
			require.EqualValues(t, 1, receipts.receipt.Key.UserID)
			require.EqualValues(t, 7, receipts.receipt.Command.AccountID)
			require.Equal(t, 5, receipts.receipt.Usage.InputTokens)
			require.Equal(t, 2, receipts.receipt.Usage.OutputTokens)
			require.Equal(t, "protocol-input-fixture", gjson.GetBytes(upstream.body, "model").String())
			switch endpoint {
			case "messages":
				require.False(t, gjson.GetBytes(upstream.body, "tools.0.parameters.required").Exists())
				require.Equal(t, gjson.Null, gjson.GetBytes(upstream.body, "tools.0.parameters.properties.payload.default.required").Type)
			case "chat/completions":
				require.Equal(t, "message", gjson.GetBytes(upstream.body, "input.0.type").String())
			default:
				require.False(t, gjson.GetBytes(upstream.body, "input.0.id").Exists())
				require.Equal(t, "data:image/png;base64,AQID", gjson.GetBytes(upstream.body, "input.0.content.0.url").String())
			}
		})
	}
}
