//go:build unit

package service

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks/media"
	"github.com/stretchr/testify/require"
)

func TestMediaTaskNativeRatesAndSelectedAccount(t *testing.T) {
	price := 1.25
	key := &APIKey{Group: &Group{AudioTTSPricePerMillionChars: &price, VideoModelPrices: map[string]map[string]float64{"wan2.6-t2v": {"720p": .125}}}}
	gateway := &OpenAIGatewayService{}
	for _, tc := range []struct{ kind, model, unit, quantity, total string }{
		{"audio", "qwen3-tts-flash", "character", "25", "0.00003125"},
		{"video", "wan2.6-t2v", "second", "5", "0.62500000"},
	} {
		p, ok := media.Lookup(tc.kind, tc.model)
		require.True(t, ok)
		rate, err := gateway.QianwenTaskRate(t.Context(), key, p)
		require.NoError(t, err)
		require.Equal(t, tc.unit, string(rate.Unit()))
		usage, err := money.NewQuantity(money.Unit(tc.unit), tc.quantity)
		require.NoError(t, err)
		exact, err := rate.Cost(usage)
		require.NoError(t, err)
		cost, err := exact.RoundCharge()
		require.NoError(t, err)
		require.Equal(t, tc.total, cost.String())
		ctx := WithMediaTaskBinding(t.Context(), MediaTaskBinding{Model: p.Model, Account: 8, Kind: p.Kind})
		account := &Account{ID: 8, Platform: PlatformQianwen, Type: AccountTypeAPIKey}
		require.NoError(t, ValidateMediaTaskAccount(ctx, account, p.Model))
		account.ID = 9
		require.ErrorIs(t, ValidateMediaTaskAccount(ctx, account, p.Model), media.Unsupported)
		account.ID = 8
		account.Platform = PlatformOpenAI
		require.ErrorIs(t, ValidateMediaTaskAccount(ctx, account, p.Model), media.Unsupported)
	}
}
