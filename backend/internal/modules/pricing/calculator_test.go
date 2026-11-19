package pricing

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
)

func rate(t *testing.T, kind, amount string, unit money.Unit, per string) Rule {
	t.Helper()
	r, err := money.NewRate("USD", amount, unit, per)
	require.NoError(t, err)
	return Rule{Kind: kind, Rate: r}
}

func qty(t *testing.T, unit money.Unit, value string) money.Quantity {
	t.Helper()
	q, err := money.NewQuantity(unit, value)
	require.NoError(t, err)
	return q
}

func TestExactCalculator(t *testing.T) {
	input := rate(t, "input", "0.000078125", "input_token", "1")
	tier := rate(t, "input", "0.0001", "input_token", "1")
	bound := qty(t, "token", "128000")
	tier.MinContext = &bound
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	factor, err := money.Parse("2")
	require.NoError(t, err)
	for _, tc := range []struct {
		name          string
		rules         []Rule
		dimensions    []Dimension
		context       string
		schedules     []Schedule
		state, charge string
	}{
		{"half-up", []Rule{input}, []Dimension{{"input", qty(t, "input_token", "1"), false}}, "1", nil, "estimated", "0.00007813"},
		{"free", []Rule{rate(t, "input", "0", "input_token", "1")}, []Dimension{{"input", qty(t, "input_token", "2"), false}}, "2", nil, "free", "0.00000000"},
		{"unpriced", nil, []Dimension{{"input", qty(t, "input_token", "2"), false}}, "2", nil, "unpriced", ""},
		{"context at boundary", []Rule{input, tier}, []Dimension{{"input", qty(t, "input_token", "1"), false}}, "128000", nil, "estimated", "0.00007813"},
		{"context above boundary", []Rule{input, tier}, []Dimension{{"input", qty(t, "input_token", "1"), false}}, "128001", nil, "estimated", "0.00010000"},
		{"time band", []Rule{input}, []Dimension{{"input", qty(t, "input_token", "1"), false}}, "1", []Schedule{{Timezone: "UTC", Start: "10:00", End: "11:00", Multiplier: factor}}, "estimated", "0.00015625"},
		{"cache bands", []Rule{rate(t, "cache_read", "0.001", "cache_read_token", "1"), rate(t, "cache_write", "0.002", "cache_write_token", "1"), rate(t, "cache_write_1h", "0.003", "cache_write_token", "1")},
			[]Dimension{{"cache_read", qty(t, "cache_read_token", "2"), false}, {"cache_write", qty(t, "cache_write_token", "3"), false}, {"cache_write_1h", qty(t, "cache_write_token", "4"), false}}, "9", nil, "estimated", "0.02000000"},
		{"media search and reasoning", []Rule{rate(t, "image", "0.07", "image", "1"), rate(t, "search", "1", "call", "1000"), rate(t, "reasoning", "0.0002", "reasoning_token", "1"), rate(t, "audio", "1", "hour", "1")},
			[]Dimension{{"image", qty(t, "image", "2"), false}, {"search", qty(t, "call", "3"), false}, {"reasoning", qty(t, "reasoning_token", "4"), false}, {"audio", qty(t, "second", "1.25"), false}}, "0", nil, "estimated", "0.14414722"},
		{"sum before rounding", []Rule{rate(t, "input", "0.000000004", "input_token", "1"), rate(t, "output", "0.000000004", "output_token", "1")},
			[]Dimension{{"input", qty(t, "input_token", "1"), false}, {"output", qty(t, "output_token", "1"), false}}, "1", nil, "estimated", "0.00000001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frozen := Frozen{Version: Version{ID: "v1", Source: "group", Currency: "USD", Rules: tc.rules, Schedules: tc.schedules},
				Usage: Usage{Context: qty(t, "token", tc.context), Dimensions: tc.dimensions}, At: at}
			cost, err := frozen.Calculate()
			require.NoError(t, err)
			require.Equal(t, tc.state, cost.State)
			if tc.charge == "" {
				require.Nil(t, cost.Charge)
			} else {
				require.JSONEq(t, `{"currency":"USD","amount":"`+tc.charge+`"}`, string(marshal(t, cost.Charge)))
			}
			var replay Frozen
			require.NoError(t, json.Unmarshal(marshal(t, frozen), &replay))
			frozen.Version.Rules = nil
			again, err := replay.Calculate()
			require.NoError(t, err)
			require.JSONEq(t, string(marshal(t, cost)), string(marshal(t, again)))
		})
	}
}

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	require.NoError(t, err)
	return b
}

func TestVersionPrecedence(t *testing.T) {
	at := time.Now().UTC()
	past := at.Add(-time.Hour)
	for _, tc := range []struct {
		name           string
		group, channel []Version
		want           string
		err            error
	}{
		{"group", []Version{{ID: "group"}}, []Version{{ID: "channel"}}, "group", nil},
		{"channel after group expired", []Version{{ID: "group", EffectiveUntil: &past}}, []Version{{ID: "channel"}}, "channel", nil},
		{"published before future channel", nil, []Version{{ID: "channel", EffectiveFrom: at.Add(time.Hour)}}, "published", nil},
		{"ambiguous group", []Version{{ID: "one"}, {ID: "two"}}, []Version{{ID: "channel"}}, "", ErrAmbiguous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := Resolve(Candidates{Group: tc.group, Channel: tc.channel, Published: []Version{{ID: "published"}}, Fallback: []Version{{ID: "fallback"}}}, at)
			require.ErrorIs(t, err, tc.err)
			if tc.err == nil {
				require.Equal(t, tc.want, v.ID)
			}
		})
	}
}
