//go:build unit

package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/compare"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/quotes"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	modelpricing "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-pricing"
)

func TestCatalogQuoteCompareAndGatewayTariffParity(t *testing.T) {
	const (
		provider = service.PlatformAnthropic
		haiku    = "claude-3-haiku-20240307"
		sonnet   = "claude-3-sonnet-20240229"
		exact    = "parity-exact"
		alias    = "parity-channel-alias"
		action   = "text.chat"
		protocol = "anthropic_messages"
	)
	ctx := context.Background()
	at := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	p := catalog.Perspective{Workspace: 1, Project: 2, Actor: 4}
	group := &service.Group{ID: 3, Platform: provider, RateMultiplier: 1}
	channel := service.Channel{ID: 7, Status: service.StatusActive, GroupIDs: []int64{group.ID},
		BillingModelSource: service.BillingModelSourceChannelMapped,
		ModelMapping:       map[string]map[string]string{provider: {haiku: alias}}}
	account := &service.Account{ID: 1, Platform: provider, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{exact: haiku, alias: haiku, sonnet: sonnet}}}
	key := &service.APIKey{ID: 5, UserID: p.Actor, GroupID: &group.ID, Group: group}
	pricingDir := t.TempDir()
	cfg := &config.Config{Pricing: config.PricingConfig{DataDir: pricingDir, UpdateIntervalHours: 24,
		FallbackFile: filepath.Join(pricingDir, "fallback.json")}}
	require.NoError(t, os.WriteFile(cfg.Pricing.FallbackFile, modelpricing.JSON(), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(pricingDir, "model_pricing.json"),
		[]byte(`{"anthropic/parity-exact":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000003}}`), 0600))
	prices := service.NewPricingService(cfg, nil)
	require.NoError(t, prices.Initialize())
	t.Cleanup(prices.Stop)
	channels := service.NewChannelService(parityChannelRepo{channel: channel, group: group}, parityGroupRepo{group: group}, nil, prices, nil)
	billing := service.NewBillingService(cfg, prices)
	resolver := service.NewModelPricingResolver(channels, billing)
	book := service.NewPlatformPriceBook(resolver)
	port := legacy.PriceBookPort{PlatformPriceBook: book}
	receipts := &parityReceipts{}
	gateway := service.ProvideGatewayService(
		parityAccountRepo{account: account}, parityGroupRepo{group: group},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, billing,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		channels, resolver, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil),
	)

	scenario := quotes.Scenario{Input: "100", Output: "20"}
	for _, tc := range []struct {
		name, model, source, amount string
	}{
		{"exact provider card wins over wire fallback", exact, service.PricingSourceLiteLLM, "0.00026000"},
		{"static fallback on authorized outbound model", haiku, service.PricingSourceStaticFallback, "0.00005000"},
		{"pi reference only", sonnet, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mapping := channels.ResolveChannelMapping(ctx, group.ID, tc.model)
			wire := account.GetMappedModel(mapping.MappedModel)
			id := catalog.NewModelIdentity(provider, tc.model)
			binding := catalog.ModelBinding{Identity: id, AccountID: account.ID, Eligible: true, Funding: "wallet",
				ChannelID: channel.ID, OfferGroupID: group.ID, BillingGroupID: group.ID, RateMultiplier: group.RateMultiplier,
				Action: action, Protocol: protocol, BillingIdentity: catalog.NewModelIdentity(provider, mapping.MappedModel),
				BillingModelSource: mapping.BillingModelSource, WireModel: wire}
			display, err := port.DescribeForGroup(ctx, catalog.PriceInput{Perspective: p, Identity: id, Binding: binding, At: at, Multiplier: 1}, group)
			require.NoError(t, err)
			input := catalog.Input{Models: []catalog.Candidate{{Provider: provider, Model: tc.model, Offers: []catalog.Offer{{
				Eligible: true, Published: true, Access: true, Bindings: []catalog.ModelBinding{binding}, Price: display,
				Capabilities: []catalog.Capability{{Action: action, Protocol: protocol}},
			}}}}}
			snapshot := catalog.Project(p, input, catalog.Window{}, at)
			detail := snapshot.Details[0]
			public := catalog.Project(catalog.Perspective{Workspace: p.Workspace}, input, catalog.Window{}, at)
			publicBilling := public.Details[0].Model.Billing
			require.Equal(t, detail.Model.Billing.State, publicBilling.State)
			reader := parityCatalog{snapshot}
			quote, err := (quotes.Service{Catalog: reader, Book: book, Bindings: SalePriceBindings{Catalog: reader}}).Quote(ctx, p,
				quotes.Input{ModelID: id.ID(), Protocol: protocol, Action: action, Scenario: &scenario})
			require.NoError(t, err)
			h := New(reader, nil, cfg)
			h.SetComparePricePort(ComparePrices{Service: compare.Service{Book: book}})
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/compare", strings.NewReader(`{"model_ids":["`+id.ID()+
				`"],"protocol":"anthropic_messages","action":"text.chat","scenario":{"input_tokens":"100","output_tokens":"20"}}`))
			compared, err := h.compare(ctx, p, c)
			require.NoError(t, err)
			estimate := compared.(comparison).Rows[0].Quote
			require.Equal(t, quote.Source, estimate.Source)
			require.Equal(t, quote.PriceVersion, estimate.PriceVersion)
			require.Equal(t, quote.Charge, estimate.Charge)
			require.Equal(t, quote.BillingState, estimate.BillingState)

			playground, err := gateway.ResolvePlaygroundPrice(ctx, key, account.ID, provider, tc.model, "wallet", action, protocol, at)
			require.NoError(t, err)
			playgroundRate, err := playground.InputRate(scenario.Input)
			require.NoError(t, err)

			before := receipts.debited
			requestCtx, _ := gateway.CaptureRequestPlan(ctx, service.GatewayRequestSpec{
				ExecutionID: id.ID(), APIKeyID: key.ID, UserID: p.Actor, PricingAt: at,
			})
			usageErr := gateway.RecordUsage(requestCtx, &service.RecordUsageInput{
				Result: &service.ForwardResult{RequestID: id.ID(), Model: tc.model, UpstreamModel: wire,
					Usage: service.ClaudeUsage{InputTokens: 100, OutputTokens: 20}},
				APIKey: key, User: &service.User{ID: p.Actor}, Account: account, PricingAt: at,
				ChannelUsageFields: service.ChannelUsageFields{BillingModelSource: mapping.BillingModelSource, ChannelMappedModel: mapping.MappedModel},
			})
			receipt := receipts.latest
			require.NotNil(t, receipt)
			require.Equal(t, id.ID(), receipt.Key.RequestID)
			if tc.amount == "" {
				require.NotNil(t, detail.Model.Billing.ReferencePrice)
				require.Equal(t, "pi-ai", detail.Model.Billing.ReferencePrice.Source)
				require.Equal(t, "unpriced", detail.Model.Billing.State)
				require.Nil(t, detail.Model.Billing.SalePrice)
				require.Nil(t, publicBilling.SalePrice)
				require.Equal(t, "unpriced", quote.BillingState)
				require.Equal(t, pricing.Missing, quote.Reason)
				require.Nil(t, quote.Charge)
				require.Nil(t, quote.Source)
				require.Nil(t, quote.PriceVersion)
				require.Empty(t, playground.Version())
				require.Nil(t, playgroundRate)
				require.ErrorIs(t, usageErr, service.ErrUsageSettlementNeedsReview)
				require.Equal(t, service.UsageSettlementNeedsReview, receipt.State)
				require.Equal(t, service.PricingStateUnpriced, receipt.Usage.PricingState)
				require.Nil(t, receipt.Usage.PriceSource)
				require.Nil(t, receipt.Usage.PriceCardVersion)
				require.Zero(t, receipt.Command.BalanceCost)
				require.Equal(t, before, receipts.debited)
				return
			}

			require.NoError(t, usageErr)
			require.Equal(t, service.UsageSettlementSettled, receipt.State)
			require.Greater(t, receipts.debited, before)
			require.Equal(t, tc.amount, receipt.Command.ChargedAmount.String())
			require.Equal(t, tc.amount, quote.Charge.String())
			require.Equal(t, "priced", quote.BillingState)
			require.Equal(t, "priced", detail.Model.Billing.State)
			sale := detail.Model.Billing.SalePrice
			require.NotNil(t, sale)
			require.Equal(t, tc.source, sale.Source)
			require.Equal(t, &tc.source, quote.Source)
			require.Equal(t, quote.Source, receipt.Usage.PriceSource)
			require.NotNil(t, sale.Version)
			require.NotNil(t, publicBilling.SalePrice)
			require.Equal(t, sale.Source, publicBilling.SalePrice.Source)
			require.Equal(t, sale.Version, publicBilling.SalePrice.Version)
			require.Equal(t, tc.amount, parityDisplayCharge(t, publicBilling.SalePrice, scenario))
			require.Equal(t, sale.Version, quote.PriceVersion)
			require.Equal(t, sale.Version, receipt.Usage.PriceCardVersion)
			require.Equal(t, *sale.Version, playground.Version())
			require.NotNil(t, playgroundRate)
			require.Equal(t, tc.amount, parityDisplayCharge(t, sale, scenario))
			encoded, err := json.Marshal(estimate)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"source":"`+tc.source+`"`)
		})
	}
}

func parityDisplayCharge(t *testing.T, sale *catalog.SalePrice, scenario quotes.Scenario) string {
	t.Helper()
	in, err := money.NewQuantity("input_token", scenario.Input)
	require.NoError(t, err)
	out, err := money.NewQuantity("output_token", scenario.Output)
	require.NoError(t, err)
	input, err := sale.Input.Cost(in)
	require.NoError(t, err)
	output, err := sale.Output.Cost(out)
	require.NoError(t, err)
	total, err := input.Add(output)
	require.NoError(t, err)
	charge, err := total.RoundCharge()
	require.NoError(t, err)
	return charge.String()
}

type parityCatalog struct{ snapshot catalog.Snapshot }

func (p parityCatalog) Snapshot(context.Context, catalog.Perspective, time.Time) (catalog.Snapshot, error) {
	return p.snapshot, nil
}

type parityGroupRepo struct {
	service.GroupRepository
	group *service.Group
}

func (r parityGroupRepo) GetByID(context.Context, int64) (*service.Group, error) { return r.group, nil }

type parityChannelRepo struct {
	service.ChannelRepository
	channel service.Channel
	group   *service.Group
}

func (r parityChannelRepo) ListAll(context.Context) ([]service.Channel, error) {
	return []service.Channel{r.channel}, nil
}

func (r parityChannelRepo) GetGroupPlatforms(context.Context, []int64) (map[int64]string, error) {
	return map[int64]string{r.group.ID: r.group.Platform}, nil
}

type parityAccountRepo struct {
	service.AccountRepository
	account *service.Account
}

func (r parityAccountRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return r.account, nil
}

type parityReceipts struct {
	service.UsageSettlementReceiptRepository
	latest  *service.UsageSettlementReceipt
	debited float64
}

func (r *parityReceipts) Persist(_ context.Context, receipt *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	r.latest = receipt
	return receipt, nil
}

func (r *parityReceipts) Settle(context.Context, service.UsageSettlementKey, string) (*service.UsageSettlementResult, error) {
	r.debited += r.latest.Command.BalanceCost
	r.latest.State = service.UsageSettlementSettled
	return &service.UsageSettlementResult{Receipt: r.latest, Billing: &service.UsageBillingApplyResult{Applied: true}}, nil
}
