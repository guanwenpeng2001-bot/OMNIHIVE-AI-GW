package money_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestV1Replay(t *testing.T) {
	data, err := os.ReadFile("../../../../contracts/migrations/BE-02/v1-replay.json")
	require.NoError(t, err)
	var fixtures []struct {
		Name   string                      `json:"name"`
		Frozen json.RawMessage             `json:"frozen_snapshot"`
		Raw    service.UsageBillingCommand `json:"raw_command"`
		Posted service.UsageBillingCommand `json:"normalized_command"`
		Shadow struct {
			Lines []struct {
				Rate  money.Rate
				Usage money.Quantity
			}
			Multiplier money.Amount
			RawDiff    string `json:"expected_raw_difference"`
			ChargeDiff string `json:"expected_charge_difference"`
		}
	}
	require.NoError(t, json.Unmarshal(data, &fixtures))
	require.Len(t, fixtures, 4)
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			replayed, err := service.RecalculatePricingSnapshot(string(f.Frozen))
			require.NoError(t, err)
			var original bytes.Buffer
			require.NoError(t, json.Compact(&original, f.Frozen))
			require.Equal(t, original.String(), replayed.PricingSnapshot)
			command := f.Raw
			command.Normalize()
			require.Equal(t, f.Posted, command)
			command.Normalize()
			require.Equal(t, f.Posted, command)
			if f.Name == "half-tie" {
				command.RequestFingerprint = ""
				command.Normalize()
				require.NotEqual(t, f.Posted.RequestFingerprint, command.RequestFingerprint)
			}

			var frozen struct {
				Actual  json.Number `json:"actual_cost"`
				Charged json.Number `json:"charged_amount"`
			}
			require.NoError(t, json.Unmarshal(f.Frozen, &frozen))
			historical, err := money.ParseV1Number("USD", frozen.Actual)
			require.NoError(t, err)
			posted, err := money.ParseV1Number("USD", frozen.Charged)
			require.NoError(t, err)
			charge, err := posted.RoundCharge()
			require.NoError(t, err)
			actual, err := (money.Amount{}).Exact("USD")
			require.NoError(t, err)
			for _, l := range f.Shadow.Lines {
				c, err := l.Rate.Cost(l.Usage)
				require.NoError(t, err)
				actual, err = actual.Add(c)
				require.NoError(t, err)
			}
			actual, err = actual.Multiply(f.Shadow.Multiplier)
			require.NoError(t, err)
			result, err := money.CompareHistorical(actual, historical, charge)
			require.NoError(t, err)
			require.Equal(t, f.Shadow.RawDiff, result.RawDifference.Ratio().RatString())
			require.Equal(t, f.Shadow.ChargeDiff, result.ChargeDifference.String())
		})
	}
}

func TestV1Number(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"1e-9", "1/1000000000"}, {"-3E+2", "-300"}, {"-0", "0"}, {"0.30000000000000004", "7500000000000001/25000000000000000"},
	} {
		e, err := money.ParseV1Number("USD", json.Number(test.in))
		require.NoError(t, err)
		require.Equal(t, test.want, e.Ratio().RatString())
	}
	for _, s := range []string{"NaN", "1e99999999", "0e-99999999", "Infinity", "3e308", "null", "+1", "1e", `"1"`, ""} {
		_, err := money.ParseV1Number("USD", json.Number(s))
		require.Error(t, err)
	}
}
