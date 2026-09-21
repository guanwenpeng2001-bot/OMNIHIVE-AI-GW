package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func compositeTargetTestAccount(platform, accountType string) *Account {
	return &Account{Platform: platform, Type: accountType}
}

func TestCompositeNormalizeRouteTargets(t *testing.T) {
	tooMany := make([]CompositeRouteTarget, 0, CompositeTargetMaxCount+1)
	for i := 0; i <= CompositeTargetMaxCount; i++ {
		tooMany = append(tooMany, CompositeRouteTarget{TargetPlatform: PlatformOpenAI, Enabled: true})
	}

	tests := []struct {
		name       string
		targets    []CompositeRouteTarget
		want       []CompositeRouteTarget
		wantErrIs  error
		wantErrAny bool
	}{
		{
			name:    "empty non nil list clears the ordered targets",
			targets: []CompositeRouteTarget{},
			want:    []CompositeRouteTarget{},
		},
		{
			name: "defaults weight latency and normalizes constraints",
			targets: []CompositeRouteTarget{{
				TargetPlatform: " OpenAI ",
				Region:         " US-EAST-1 ",
				Provider:       " AWS ",
				Enabled:        true,
			}},
			want: []CompositeRouteTarget{{
				Position:          0,
				TargetPlatform:    PlatformOpenAI,
				Region:            "us-east-1",
				Provider:          "aws",
				LatencyPreference: CompositeTargetLatencyDefault,
				SchedulingWeight:  CompositeTargetDefaultWeight,
				Enabled:           true,
			}},
		},
		{
			name: "low latency preference is preserved",
			targets: []CompositeRouteTarget{{
				TargetPlatform:    PlatformOpenAI,
				LatencyPreference: " Low_Latency ",
				Enabled:           true,
			}},
			want: []CompositeRouteTarget{{
				TargetPlatform:    PlatformOpenAI,
				LatencyPreference: CompositeTargetLatencyLowLatency,
				SchedulingWeight:  CompositeTargetDefaultWeight,
				Enabled:           true,
			}},
		},
		{
			name: "unknown latency preference falls back to default",
			targets: []CompositeRouteTarget{{
				TargetPlatform:    PlatformOpenAI,
				LatencyPreference: "fastest",
				Enabled:           true,
			}},
			want: []CompositeRouteTarget{{
				TargetPlatform:    PlatformOpenAI,
				LatencyPreference: CompositeTargetLatencyDefault,
				SchedulingWeight:  CompositeTargetDefaultWeight,
				Enabled:           true,
			}},
		},
		{
			name: "position is rewritten to the declared order",
			targets: []CompositeRouteTarget{
				{Position: 7, TargetPlatform: PlatformOpenAI, SchedulingWeight: 200, Enabled: true},
				{Position: 3, TargetPlatform: PlatformAnthropic, SchedulingWeight: 50, Enabled: true},
			},
			want: []CompositeRouteTarget{
				{Position: 0, TargetPlatform: PlatformOpenAI, LatencyPreference: CompositeTargetLatencyDefault, SchedulingWeight: 200, Enabled: true},
				{Position: 1, TargetPlatform: PlatformAnthropic, LatencyPreference: CompositeTargetLatencyDefault, SchedulingWeight: 50, Enabled: true},
			},
		},
		{
			name: "affinity ttl bounds are accepted",
			targets: []CompositeRouteTarget{
				{TargetPlatform: PlatformOpenAI, AffinityTTLSeconds: CompositeTargetMinAffinityTTLSeconds, Enabled: true},
				{TargetPlatform: PlatformAnthropic, AffinityTTLSeconds: CompositeTargetMaxAffinityTTLSeconds, Enabled: true},
			},
			want: []CompositeRouteTarget{
				{Position: 0, TargetPlatform: PlatformOpenAI, LatencyPreference: CompositeTargetLatencyDefault, SchedulingWeight: CompositeTargetDefaultWeight, AffinityTTLSeconds: CompositeTargetMinAffinityTTLSeconds, Enabled: true},
				{Position: 1, TargetPlatform: PlatformAnthropic, LatencyPreference: CompositeTargetLatencyDefault, SchedulingWeight: CompositeTargetDefaultWeight, AffinityTTLSeconds: CompositeTargetMaxAffinityTTLSeconds, Enabled: true},
			},
		},
		{
			name:      "too many targets are rejected",
			targets:   tooMany,
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:      "non concrete platform is rejected",
			targets:   []CompositeRouteTarget{{TargetPlatform: PlatformComposite, Enabled: true}},
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:      "empty platform is rejected",
			targets:   []CompositeRouteTarget{{TargetPlatform: " ", Enabled: true}},
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:      "all disabled targets are rejected",
			targets:   []CompositeRouteTarget{{TargetPlatform: PlatformOpenAI}, {TargetPlatform: PlatformAnthropic}},
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:      "negative weight is rejected",
			targets:   []CompositeRouteTarget{{TargetPlatform: PlatformOpenAI, SchedulingWeight: -1, Enabled: true}},
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:      "weight above the maximum is rejected",
			targets:   []CompositeRouteTarget{{TargetPlatform: PlatformOpenAI, SchedulingWeight: 10001, Enabled: true}},
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:      "affinity ttl below the minimum is rejected",
			targets:   []CompositeRouteTarget{{TargetPlatform: PlatformOpenAI, AffinityTTLSeconds: 1, Enabled: true}},
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:      "affinity ttl just below the minimum is rejected",
			targets:   []CompositeRouteTarget{{TargetPlatform: PlatformOpenAI, AffinityTTLSeconds: 59, Enabled: true}},
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:      "affinity ttl above the maximum is rejected",
			targets:   []CompositeRouteTarget{{TargetPlatform: PlatformOpenAI, AffinityTTLSeconds: 86401, Enabled: true}},
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:      "region with invalid characters is rejected",
			targets:   []CompositeRouteTarget{{TargetPlatform: PlatformOpenAI, Region: "us east 1", Enabled: true}},
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:      "region with an invalid leading character is rejected",
			targets:   []CompositeRouteTarget{{TargetPlatform: PlatformOpenAI, Region: "-us-east-1", Enabled: true}},
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:      "provider with invalid characters is rejected",
			targets:   []CompositeRouteTarget{{TargetPlatform: PlatformOpenAI, Provider: "aws/bedrock", Enabled: true}},
			wantErrIs: ErrCompositeTargetInvalid,
		},
		{
			name:       "hosted account type conflict is rejected",
			targets:    []CompositeRouteTarget{{TargetPlatform: PlatformBedrock, AccountType: AccountTypeOAuth, Enabled: true}},
			wantErrAny: true,
		},
		{
			name:    "hosted platform keeps its account type",
			targets: []CompositeRouteTarget{{TargetPlatform: PlatformBedrock, AccountType: AccountTypeBedrock, Enabled: true}},
			want: []CompositeRouteTarget{{
				TargetPlatform:    PlatformBedrock,
				AccountType:       AccountTypeBedrock,
				LatencyPreference: CompositeTargetLatencyDefault,
				SchedulingWeight:  CompositeTargetDefaultWeight,
				Enabled:           true,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeCompositeRouteTargets(tt.targets)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				return
			}
			if tt.wantErrAny {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCompositeEffectiveTargetsSynthesizesLegacyTarget(t *testing.T) {
	route := CompositeModelRoute{
		PublicModel:    "gpt-5",
		MatchType:      CompositeRouteMatchExact,
		TargetPlatform: PlatformOpenAI,
		AccountType:    AccountTypeOAuth,
		UpstreamModel:  "gpt-5",
	}

	targets := route.EffectiveTargets()

	require.Equal(t, []CompositeRouteTarget{{
		Position:          0,
		TargetPlatform:    PlatformOpenAI,
		AccountType:       AccountTypeOAuth,
		UpstreamModel:     "gpt-5",
		LatencyPreference: CompositeTargetLatencyDefault,
		SchedulingWeight:  CompositeTargetDefaultWeight,
		Enabled:           true,
	}}, targets)

	primary, ok := route.PrimaryTarget()
	require.True(t, ok)
	require.Equal(t, targets[0], primary)
}

func TestCompositeEffectiveTargetsKeepsEnabledDeclaredOrder(t *testing.T) {
	route := CompositeModelRoute{
		TargetPlatform: PlatformOpenAI,
		Targets: []CompositeRouteTarget{
			{Position: 0, TargetPlatform: PlatformAnthropic, UpstreamModel: "claude-sonnet-4-6", Enabled: true},
			{Position: 1, TargetPlatform: PlatformGemini, Enabled: false},
			{Position: 2, TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Enabled: true},
		},
	}

	targets := route.EffectiveTargets()

	require.Equal(t, []CompositeRouteTarget{
		{Position: 0, TargetPlatform: PlatformAnthropic, UpstreamModel: "claude-sonnet-4-6", Enabled: true},
		{Position: 2, TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Enabled: true},
	}, targets)

	primary, ok := route.PrimaryTarget()
	require.True(t, ok)
	require.Equal(t, PlatformAnthropic, primary.TargetPlatform)
}

func TestCompositePrimaryTargetWithoutEnabledTargets(t *testing.T) {
	route := CompositeModelRoute{
		Targets: []CompositeRouteTarget{
			{Position: 0, TargetPlatform: PlatformAnthropic},
			{Position: 1, TargetPlatform: PlatformOpenAI},
		},
	}

	require.Empty(t, route.EffectiveTargets())
	_, ok := route.PrimaryTarget()
	require.False(t, ok)
}

func TestCompositeAffinityTTLDuration(t *testing.T) {
	tests := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{name: "unset keeps the legacy default", seconds: 0, want: 0},
		{name: "below the minimum is ignored", seconds: 1, want: 0},
		{name: "just below the minimum is ignored", seconds: 59, want: 0},
		{name: "minimum is accepted", seconds: CompositeTargetMinAffinityTTLSeconds, want: time.Minute},
		{name: "configured ttl is converted", seconds: 120, want: 120 * time.Second},
		{name: "maximum is accepted", seconds: CompositeTargetMaxAffinityTTLSeconds, want: 24 * time.Hour},
		{name: "above the maximum is ignored", seconds: 86401, want: 0},
		{name: "negative is ignored", seconds: -1, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := CompositeRouteTarget{AffinityTTLSeconds: tt.seconds}
			require.Equal(t, tt.want, target.AffinityTTLDuration())
		})
	}
}

func TestCompositeAccountRoutingRegion(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    string
	}{
		{name: "nil account has no region", account: nil, want: ""},
		{
			name: "extra region wins over credentials",
			account: &Account{
				Extra:       map[string]any{"region": " EU-WEST-1 "},
				Credentials: map[string]any{"aws_region": "us-east-1"},
			},
			want: "eu-west-1",
		},
		{
			name:    "bedrock aws_region credential",
			account: &Account{Credentials: map[string]any{"aws_region": "us-east-1"}},
			want:    "us-east-1",
		},
		{
			name:    "vertex location credential",
			account: &Account{Credentials: map[string]any{"location": "asia-northeast1"}},
			want:    "asia-northeast1",
		},
		{
			name:    "region credential",
			account: &Account{Credentials: map[string]any{"region": "us-central1"}},
			want:    "us-central1",
		},
		{
			name:    "vertex_location credential",
			account: &Account{Credentials: map[string]any{"vertex_location": "europe-west4"}},
			want:    "europe-west4",
		},
		{
			name: "non string extra region falls back to credentials",
			account: &Account{
				Extra:       map[string]any{"region": 42},
				Credentials: map[string]any{"aws_region": "us-east-1"},
			},
			want: "us-east-1",
		},
		{
			name:    "account without region attributes",
			account: &Account{Platform: PlatformOpenAI},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, accountRoutingRegion(tt.account))
		})
	}
}

func TestCompositeAccountRoutingProvider(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    string
	}{
		{name: "nil account has no provider", account: nil, want: ""},
		{
			name: "extra provider wins",
			account: &Account{
				Extra:       map[string]any{"provider": " AWS "},
				Credentials: map[string]any{"provider": "gcp"},
			},
			want: "aws",
		},
		{
			name:    "extra vendor is used",
			account: &Account{Extra: map[string]any{"vendor": "OpenAI"}},
			want:    "openai",
		},
		{
			name:    "provider credential is used",
			account: &Account{Credentials: map[string]any{"provider": "gcp"}},
			want:    "gcp",
		},
		{
			name:    "vendor credential is used",
			account: &Account{Credentials: map[string]any{"vendor": "Azure"}},
			want:    "azure",
		},
		{
			name:    "platform is the provider fallback",
			account: compositeTargetTestAccount(PlatformOpenAI, AccountTypeOAuth),
			want:    "openai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, accountRoutingProvider(tt.account))
		})
	}
}

func TestCompositeTargetAllowsAccount(t *testing.T) {
	tests := []struct {
		name    string
		target  CompositeRouteTarget
		account *Account
		want    bool
	}{
		{
			name:    "nil account never satisfies a target",
			target:  CompositeRouteTarget{TargetPlatform: PlatformOpenAI},
			account: nil,
			want:    false,
		},
		{
			name:    "empty constraints allow any matching account",
			target:  CompositeRouteTarget{TargetPlatform: PlatformOpenAI},
			account: compositeTargetTestAccount(PlatformOpenAI, AccountTypeOAuth),
			want:    true,
		},
		{
			name:    "region constraint is satisfied by the credential",
			target:  CompositeRouteTarget{TargetPlatform: PlatformOpenAI, Region: "us-east-1"},
			account: &Account{Platform: PlatformOpenAI, Credentials: map[string]any{"aws_region": "us-east-1"}},
			want:    true,
		},
		{
			name:    "region constraint rejects another region",
			target:  CompositeRouteTarget{TargetPlatform: PlatformOpenAI, Region: "us-east-1"},
			account: &Account{Platform: PlatformOpenAI, Credentials: map[string]any{"aws_region": "us-west-2"}},
			want:    false,
		},
		{
			name:    "region constraint fails closed without evidence",
			target:  CompositeRouteTarget{TargetPlatform: PlatformOpenAI, Region: "us-east-1"},
			account: compositeTargetTestAccount(PlatformOpenAI, AccountTypeOAuth),
			want:    false,
		},
		{
			name:    "provider constraint is satisfied by the extra attribute",
			target:  CompositeRouteTarget{TargetPlatform: PlatformOpenAI, Provider: "aws"},
			account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{"provider": "aws"}},
			want:    true,
		},
		{
			name:    "provider constraint is satisfied by the platform fallback",
			target:  CompositeRouteTarget{TargetPlatform: PlatformOpenAI, Provider: "openai"},
			account: compositeTargetTestAccount(PlatformOpenAI, AccountTypeOAuth),
			want:    true,
		},
		{
			name:    "provider constraint rejects another provider",
			target:  CompositeRouteTarget{TargetPlatform: PlatformOpenAI, Provider: "aws"},
			account: compositeTargetTestAccount(PlatformOpenAI, AccountTypeOAuth),
			want:    false,
		},
		{
			name:    "account type constraint rejects a mismatch",
			target:  CompositeRouteTarget{TargetPlatform: PlatformAnthropic, AccountType: AccountTypeOAuth},
			account: compositeTargetTestAccount(PlatformAnthropic, AccountTypeAPIKey),
			want:    false,
		},
		{
			name:    "hosted platform implies the bedrock account type",
			target:  CompositeRouteTarget{TargetPlatform: PlatformBedrock},
			account: compositeTargetTestAccount(PlatformAnthropic, AccountTypeOAuth),
			want:    false,
		},
		{
			name:    "hosted platform accepts the bedrock account type",
			target:  CompositeRouteTarget{TargetPlatform: PlatformBedrock},
			account: compositeTargetTestAccount(PlatformAnthropic, AccountTypeBedrock),
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, compositeTargetAllowsAccount(tt.target, tt.account))
		})
	}
}

func TestCompositeTargetLoadDenominator(t *testing.T) {
	tests := []struct {
		name   string
		base   int
		weight int
		want   int
	}{
		{name: "zero weight keeps the base", base: 10, weight: 0, want: 10},
		{name: "negative weight keeps the base", base: 10, weight: -5, want: 10},
		{name: "default weight keeps the base", base: 10, weight: CompositeTargetDefaultWeight, want: 10},
		{name: "double weight doubles the base", base: 10, weight: 200, want: 20},
		{name: "half weight halves the base", base: 10, weight: 50, want: 5},
		{name: "one and a half weight", base: 10, weight: 150, want: 15},
		{name: "maximum weight scales the base", base: 1, weight: 10000, want: 100},
		{name: "rounds to the nearest integer", base: 7, weight: 33, want: 2},
		{name: "tiny weight never drops below one", base: 1, weight: 1, want: 1},
		{name: "tiny base never drops below one", base: 3, weight: 10, want: 1},
		{name: "zero base never drops below one", base: 0, weight: 200, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, compositeTargetLoadDenominator(tt.base, tt.weight))
		})
	}
}

func TestCompositeTargetActionGatedTargets(t *testing.T) {
	anthropic := CompositeRouteTarget{Position: 0, TargetPlatform: PlatformAnthropic, Enabled: true}
	openai := CompositeRouteTarget{Position: 1, TargetPlatform: PlatformOpenAI, Enabled: true}
	gemini := CompositeRouteTarget{Position: 2, TargetPlatform: PlatformGemini, Enabled: true}

	tests := []struct {
		name      string
		targets   []CompositeRouteTarget
		action    string
		want      []CompositeRouteTarget
		wantErrIs error
	}{
		{
			name:    "no targets stay empty",
			targets: nil,
			action:  "chat",
			want:    nil,
		},
		{
			name:    "unknown action keeps every target",
			targets: []CompositeRouteTarget{anthropic, openai},
			action:  "",
			want:    []CompositeRouteTarget{anthropic, openai},
		},
		{
			name:    "known action keeps implemented adapters in order",
			targets: []CompositeRouteTarget{anthropic, openai},
			action:  "embeddings",
			want:    []CompositeRouteTarget{openai},
		},
		{
			name:    "implemented adapter is kept",
			targets: []CompositeRouteTarget{anthropic},
			action:  "chat",
			want:    []CompositeRouteTarget{anthropic},
		},
		{
			name:    "messages adapter is implemented for anthropic",
			targets: []CompositeRouteTarget{anthropic},
			action:  "messages",
			want:    []CompositeRouteTarget{anthropic},
		},
		{
			name:      "no implemented target fails explicitly",
			targets:   []CompositeRouteTarget{anthropic, gemini},
			action:    "embeddings",
			wantErrIs: ErrCompositeTargetActionUnsupported,
		},
		{
			name:      "hard conflict target fails explicitly",
			targets:   []CompositeRouteTarget{openai, anthropic},
			action:    "rerank",
			wantErrIs: ErrCompositeTargetActionUnsupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compositeTargetActionGatedTargets(tt.targets, tt.action)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCompositeWalkTargets(t *testing.T) {
	targets := []CompositeRouteTarget{
		{Position: 0, TargetPlatform: PlatformAnthropic, Enabled: true},
		{Position: 1, TargetPlatform: PlatformOpenAI, Enabled: true},
		{Position: 2, TargetPlatform: PlatformGemini, Enabled: true},
	}

	t.Run("empty list reports no target", func(t *testing.T) {
		calls := 0
		_, ok := compositeWalkTargets(context.Background(), nil, func(context.Context, CompositeRouteTarget) bool {
			calls++
			return true
		})
		require.False(t, ok)
		require.Zero(t, calls)
	})

	t.Run("single target skips the candidate predicate", func(t *testing.T) {
		calls := 0
		got, ok := compositeWalkTargets(context.Background(), targets[:1], func(context.Context, CompositeRouteTarget) bool {
			calls++
			return false
		})
		require.True(t, ok)
		require.Equal(t, targets[0], got)
		require.Zero(t, calls)
	})

	t.Run("first target with a candidate wins", func(t *testing.T) {
		seen := make([]string, 0, len(targets))
		got, ok := compositeWalkTargets(context.Background(), targets, func(_ context.Context, candidate CompositeRouteTarget) bool {
			seen = append(seen, candidate.TargetPlatform)
			return candidate.TargetPlatform == PlatformOpenAI
		})
		require.True(t, ok)
		require.Equal(t, targets[1], got)
		require.Equal(t, []string{PlatformAnthropic, PlatformOpenAI}, seen)
	})

	t.Run("exhausted targets fall back to the last one", func(t *testing.T) {
		calls := 0
		got, ok := compositeWalkTargets(context.Background(), targets, func(context.Context, CompositeRouteTarget) bool {
			calls++
			return false
		})
		require.True(t, ok)
		require.Equal(t, targets[len(targets)-1], got)
		require.Equal(t, len(targets), calls)
	})
}
