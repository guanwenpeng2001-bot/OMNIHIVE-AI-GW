package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// compositeOrderedTargetAccountRepo 按平台返回可调度账号，用于验证有序目标的跨平台推进。
type compositeOrderedTargetAccountRepo struct {
	AccountRepository
	byPlatform map[string][]Account
	listed     []string
}

func (r *compositeOrderedTargetAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]Account, error) {
	r.listed = append(r.listed, platform)
	return append([]Account(nil), r.byPlatform[platform]...), nil
}

func (r *compositeOrderedTargetAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]Account, error) {
	r.listed = append(r.listed, platform)
	return append([]Account(nil), r.byPlatform[platform]...), nil
}

func (r *compositeOrderedTargetAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

// Anthropic/Gemini 分组默认走混合平台列表（同平台 + Antigravity）。
func (r *compositeOrderedTargetAccountRepo) ListSchedulableByGroupIDAndPlatforms(_ context.Context, _ int64, platforms []string) ([]Account, error) {
	return r.collectPlatforms(platforms), nil
}

func (r *compositeOrderedTargetAccountRepo) ListSchedulableByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	return r.collectPlatforms(platforms), nil
}

func (r *compositeOrderedTargetAccountRepo) ListSchedulableUngroupedByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	return r.collectPlatforms(platforms), nil
}

func (r *compositeOrderedTargetAccountRepo) collectPlatforms(platforms []string) []Account {
	var collected []Account
	for _, platform := range platforms {
		r.listed = append(r.listed, platform)
		collected = append(collected, r.byPlatform[platform]...)
	}
	return collected
}

func compositeTargetIntPtr(value int) *int { return &value }

// 只有会在每次尝试内改写上游模型的主干才启用有序目标推进。
func compositeTargetAdvancementContext() context.Context {
	return WithCompositeTargetAdvancement(context.Background())
}

func compositeOrderedTargetTestAccount(id int64, platform, accountType string) Account {
	return Account{
		ID:          id,
		Platform:    platform,
		Type:        accountType,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 4,
	}
}

// 目标 0（Anthropic）没有可用账号时，本次尝试必须推进到目标 1（OpenAI），
// 并使用目标 1 声明的 upstream 模型做候选过滤。
func TestSelectCompositeDecisionTargetAdvancesAcrossPlatforms(t *testing.T) {
	repo := &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{
		PlatformOpenAI: {compositeOrderedTargetTestAccount(201, PlatformOpenAI, AccountTypeAPIKey)},
	}}
	svc := &GatewayService{accountRepo: repo}
	groupID := int64(21)

	decision := CompositeRouteDecision{
		Matched:        true,
		PublicModel:    "public-model",
		TargetPlatform: PlatformAnthropic,
		UpstreamModel:  "claude-sonnet-4-6",
		Targets: []CompositeRouteTarget{
			{Position: 0, TargetPlatform: PlatformAnthropic, UpstreamModel: "claude-sonnet-4-6", SchedulingWeight: 100, Enabled: true},
			{Position: 1, TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", SchedulingWeight: 100, Enabled: true},
		},
	}

	selected, err := svc.selectCompositeDecisionTarget(compositeTargetAdvancementContext(), &groupID, decision, decision.PublicModel, "chat")
	require.NoError(t, err)
	require.Equal(t, PlatformOpenAI, selected.TargetPlatform)
	require.Equal(t, "gpt-5", selected.UpstreamModel)
	require.Equal(t, PlatformAnthropic, repo.listed[0], "先探测第一个目标")
	require.Contains(t, repo.listed, PlatformOpenAI, "第一个目标无候选后必须继续探测第二个目标")

	// 目标上下文必须能被调度栈读取，供权重与亲和性 TTL 使用。
	stored, ok := CompositeRouteTargetsFromContext(WithCompositeRouteDecision(context.Background(), selected))
	require.True(t, ok)
	require.Equal(t, selected.Targets, stored)
	active, ok := ActiveCompositeTargetFromContext(WithCompositeRouteDecision(context.Background(), selected))
	require.True(t, ok)
	require.Equal(t, PlatformOpenAI, active.TargetPlatform)
}

// 目标 0 的账号已被本次请求排除时必须推进到目标 1，而不是在目标 0 上报无账号。
func TestSelectCompositeDecisionTargetRespectsExcludedAccounts(t *testing.T) {
	repo := &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{
		PlatformAnthropic: {compositeOrderedTargetTestAccount(301, PlatformAnthropic, AccountTypeOAuth)},
		PlatformOpenAI:    {compositeOrderedTargetTestAccount(302, PlatformOpenAI, AccountTypeAPIKey)},
	}}
	svc := &GatewayService{accountRepo: repo}
	groupID := int64(21)

	decision := CompositeRouteDecision{
		Matched:        true,
		PublicModel:    "public-model",
		TargetPlatform: PlatformAnthropic,
		UpstreamModel:  "claude-sonnet-4-6",
		Targets: []CompositeRouteTarget{
			{Position: 0, TargetPlatform: PlatformAnthropic, UpstreamModel: "claude-sonnet-4-6", SchedulingWeight: 100, Enabled: true},
			{Position: 1, TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", SchedulingWeight: 100, Enabled: true},
		},
	}

	// 没有排除时停在目标 0。
	selected, err := svc.selectCompositeDecisionTarget(compositeTargetAdvancementContext(), &groupID, decision, decision.PublicModel, "chat")
	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, selected.TargetPlatform)

	excluded := WithCompositeExcludedAccounts(context.Background(), map[int64]struct{}{301: {}})
	selected, err = svc.selectCompositeDecisionTarget(WithCompositeTargetAdvancement(excluded), &groupID, decision, decision.PublicModel, "chat")
	require.NoError(t, err)
	require.Equal(t, PlatformOpenAI, selected.TargetPlatform)
	require.Equal(t, "gpt-5", selected.UpstreamModel)
}

// 目标 0 的 region 约束没有匹配账号时推进到下一个目标；全部目标耗尽时保留最后一个，
// 由既有「无可用账号」处理接管，而不是静默换成别的语义。
func TestSelectCompositeDecisionTargetHonorsConstraintsAndExhaustion(t *testing.T) {
	usEast := compositeOrderedTargetTestAccount(401, PlatformBedrock, AccountTypeBedrock)
	usEast.Credentials = map[string]any{"aws_region": "us-east-1"}
	repo := &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{
		PlatformBedrock: {usEast},
	}}
	svc := &GatewayService{accountRepo: repo}
	groupID := int64(21)

	decision := CompositeRouteDecision{
		Matched:        true,
		PublicModel:    "public-model",
		TargetPlatform: PlatformBedrock,
		UpstreamModel:  "us.anthropic.claude-sonnet-4-6",
		Targets: []CompositeRouteTarget{
			{Position: 0, TargetPlatform: PlatformBedrock, Region: "eu-west-1", UpstreamModel: "eu.anthropic.claude-sonnet-4-6", SchedulingWeight: 100, Enabled: true},
			{Position: 1, TargetPlatform: PlatformBedrock, Region: "us-east-1", UpstreamModel: "us.anthropic.claude-sonnet-4-6", SchedulingWeight: 100, Enabled: true},
		},
	}

	selected, err := svc.selectCompositeDecisionTarget(compositeTargetAdvancementContext(), &groupID, decision, decision.PublicModel, "messages")
	require.NoError(t, err)
	require.Equal(t, "us.anthropic.claude-sonnet-4-6", selected.UpstreamModel)
	require.Equal(t, PlatformBedrock, selected.TargetPlatform)

	// 同一平台的两个目标只能靠固定选中目标区分：只按平台匹配会张冠李戴。
	active, ok := ActiveCompositeTargetFromContext(WithCompositeRouteDecision(context.Background(), selected))
	require.True(t, ok)
	require.Equal(t, "us-east-1", active.Region)
	require.Equal(t, 1, active.Position, "region 约束不匹配的目标必须被跳过")

	empty := &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{}}
	exhausted, err := (&GatewayService{accountRepo: empty}).selectCompositeDecisionTarget(compositeTargetAdvancementContext(), &groupID, decision, decision.PublicModel, "messages")
	require.NoError(t, err)
	exhaustedActive, ok := ActiveCompositeTargetFromContext(WithCompositeRouteDecision(context.Background(), exhausted))
	require.True(t, ok)
	require.Equal(t, 1, exhaustedActive.Position)
}

// 已知动作在全部目标上都没有 adapter 时必须显式报错，不能静默退化成单目标路由。
func TestSelectCompositeDecisionTargetRejectsUnsupportedAction(t *testing.T) {
	repo := &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{
		PlatformAnthropic: {compositeOrderedTargetTestAccount(501, PlatformAnthropic, AccountTypeOAuth)},
	}}
	svc := &GatewayService{accountRepo: repo}
	groupID := int64(21)

	decision := CompositeRouteDecision{
		Matched:        true,
		PublicModel:    "public-model",
		TargetPlatform: PlatformAnthropic,
		UpstreamModel:  "claude-sonnet-4-6",
		Targets: []CompositeRouteTarget{
			{Position: 0, TargetPlatform: PlatformAnthropic, UpstreamModel: "claude-sonnet-4-6", SchedulingWeight: 100, Enabled: true},
		},
	}

	_, err := svc.selectCompositeDecisionTarget(compositeTargetAdvancementContext(), &groupID, decision, decision.PublicModel, "embeddings")
	require.ErrorIs(t, err, ErrCompositeTargetActionUnsupported)
	require.Empty(t, repo.listed, "动作不支持时不得再去探测候选账号")
}

// 没有启用推进的入口必须保持中间件已解析的目标（请求体里就是它的模型），
// 既不得跳到别的平台，也不得改变已解析的目标模型。
func TestCompositeTargetAdvancementRequiresWiredTrunk(t *testing.T) {
	repo := &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{
		PlatformOpenAI: {compositeOrderedTargetTestAccount(651, PlatformOpenAI, AccountTypeAPIKey)},
	}}
	svc := &GatewayService{accountRepo: repo}
	groupID := int64(21)

	decision := CompositeRouteDecision{
		Matched:        true,
		PublicModel:    "public-model",
		TargetPlatform: PlatformAnthropic,
		UpstreamModel:  "claude-sonnet-4-6",
		Targets: []CompositeRouteTarget{
			{Position: 0, TargetPlatform: PlatformAnthropic, UpstreamModel: "claude-sonnet-4-6", SchedulingWeight: 100, Enabled: true},
			{Position: 1, TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", SchedulingWeight: 100, Enabled: true},
		},
	}

	resolved, err := svc.selectCompositeDecisionTarget(context.Background(), &groupID, decision, decision.PublicModel, "chat")
	require.NoError(t, err)
	require.Equal(t, decision, resolved, "未启用推进的入口必须原样保留已解析目标")
	require.Empty(t, repo.listed, "未启用推进时不得探测其它目标平台")

	openAISvc := &OpenAIGatewayService{accountRepo: repo}
	_, ok, err := openAISvc.CompositeTargetForAttempt(WithCompositeRouteDecision(context.Background(), decision), &groupID)
	require.NoError(t, err)
	require.False(t, ok, "未启用推进时 trunk 不得改写本次尝试的目标")
	require.Empty(t, repo.listed)
}

// OpenAI 主干由 handler 显式传平台，因此按同一份声明顺序推进目标。
func TestCompositeTargetForAttemptAdvancesInOpenAITrunk(t *testing.T) {
	repo := &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{
		PlatformGemini: {compositeOrderedTargetTestAccount(601, PlatformGemini, AccountTypeServiceAccount)},
	}}
	svc := &OpenAIGatewayService{accountRepo: repo}
	groupID := int64(21)

	decision := CompositeRouteDecision{
		Matched:        true,
		PublicModel:    "public-model",
		TargetPlatform: PlatformOpenAI,
		UpstreamModel:  "gpt-5",
		Targets: []CompositeRouteTarget{
			{Position: 0, TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", SchedulingWeight: 100, Enabled: true},
			{Position: 1, TargetPlatform: PlatformGemini, UpstreamModel: "gemini-2.5-pro", SchedulingWeight: 100, Enabled: true},
		},
	}
	ctx := WithCompositeTargetAdvancement(WithCompositeRouteDecision(context.Background(), decision))

	target, ok, err := svc.CompositeTargetForAttempt(ctx, &groupID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, PlatformGemini, target.TargetPlatform)
	require.Equal(t, "gemini-2.5-pro", target.UpstreamModel)

	// 排除 Gemini 唯一账号后，目标顺序已经没有候选：保留最后一个目标，
	// 由既有「无可用账号」处理给出显式错误，而不是回到第一个目标。
	excluded := WithCompositeTargetAdvancement(WithCompositeRouteDecision(
		WithCompositeExcludedAccounts(context.Background(), map[int64]struct{}{601: {}}),
		decision,
	))
	target, ok, err = svc.CompositeTargetForAttempt(excluded, &groupID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, PlatformGemini, target.TargetPlatform)

	// 无有序目标的请求不参与复合推进。
	_, ok, err = svc.CompositeTargetForAttempt(context.Background(), &groupID)
	require.NoError(t, err)
	require.False(t, ok)
}

// 有序目标声明的亲和性 TTL 必须在 OpenAI 主干的粘性绑定上生效（覆盖默认 TTL），
// 未声明的请求保持原有默认值。
func TestOpenAIStickySessionTTLHonorsCompositeTargetOverride(t *testing.T) {
	recording := &compositeTargetRecordingStickyCache{}
	svc := &OpenAIGatewayService{cache: recording}
	groupID := int64(21)

	require.NoError(t, svc.BindStickySession(context.Background(), &groupID, "session-default", 701))
	require.Equal(t, openaiStickySessionTTL, recording.ttlFor("openai:session-default"))

	target := CompositeRouteTarget{
		Position:           1,
		TargetPlatform:     PlatformOpenAI,
		SchedulingWeight:   CompositeTargetDefaultWeight,
		AffinityTTLSeconds: 300,
		Enabled:            true,
	}
	decision := CompositeRouteDecision{
		Matched:        true,
		PublicModel:    "public-model",
		TargetPlatform: PlatformOpenAI,
		Targets:        []CompositeRouteTarget{target},
	}
	ctx := WithCompositeTargetSelectionContext(WithCompositeRouteDecision(context.Background(), decision), target)
	require.NoError(t, svc.BindStickySession(ctx, &groupID, "session-target", 702))
	require.Equal(t, 300*time.Second, recording.ttlFor("openai:session-target"))

	// 没有声明 TTL 的目标保持 OpenAI 主干原有默认值。
	plainTarget := CompositeRouteTarget{TargetPlatform: PlatformOpenAI, SchedulingWeight: CompositeTargetDefaultWeight, Enabled: true}
	plainCtx := WithCompositeTargetSelectionContext(
		WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, TargetPlatform: PlatformOpenAI, Targets: []CompositeRouteTarget{plainTarget}}),
		plainTarget,
	)
	require.NoError(t, svc.BindStickySession(plainCtx, &groupID, "session-plain", 703))
	require.Equal(t, openaiStickySessionTTL, recording.ttlFor("openai:session-plain"))
}

// V2 亲和性键走「准入后延迟绑定」：实际写入发生在另一个 ctx 上，请求级目标 TTL
// 必须一并带过去，否则可配置 TTL 只对直接绑定生效。
func TestDeferredStickyBindingKeepsRequestAffinityTTL(t *testing.T) {
	recording := &compositeTargetRecordingStickyCache{}
	svc := &GatewayService{cache: recording}
	groupID := int64(21)
	sessionHash := "v2:binding:sha256:deferred-ttl-fixture"

	plan := NewGatewayRequestPlan(GatewayRequestSpec{})
	ctx := WithStickySessionTTL(WithGatewayRequestPlan(context.Background(), plan), 90*time.Second)

	require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ctx, &groupID, sessionHash, 901))
	require.Zero(t, recording.ttlFor(sessionHash), "延迟绑定在准入前不得写粘性会话")

	require.NoError(t, admitGatewayRequestBinding(ctx, 901))
	require.Equal(t, 90*time.Second, recording.ttlFor(sessionHash))
}

// compositeTargetRecordingStickyCache 记录每次粘性绑定实际使用的 TTL。
type compositeTargetRecordingStickyCache struct {
	GatewayCache
	ttls map[string]time.Duration
}

func (c *compositeTargetRecordingStickyCache) ttlFor(sessionHash string) time.Duration {
	if c.ttls == nil {
		return 0
	}
	return c.ttls[sessionHash]
}

func (c *compositeTargetRecordingStickyCache) SetSessionAccountID(_ context.Context, _ int64, sessionHash string, _ int64, ttl time.Duration) error {
	if c.ttls == nil {
		c.ttls = make(map[string]time.Duration)
	}
	c.ttls[sessionHash] = ttl
	return nil
}

func (c *compositeTargetRecordingStickyCache) GetSessionAccountID(_ context.Context, _ int64, sessionHash string) (int64, error) {
	if c.ttls == nil {
		return 0, ErrStickySessionNotFound
	}
	if _, ok := c.ttls[sessionHash]; !ok {
		return 0, ErrStickySessionNotFound
	}
	return 1, nil
}

func (c *compositeTargetRecordingStickyCache) RefreshSessionTTL(_ context.Context, _ int64, sessionHash string, ttl time.Duration) error {
	if c.ttls == nil {
		c.ttls = make(map[string]time.Duration)
	}
	c.ttls[sessionHash] = ttl
	return nil
}

func (c *compositeTargetRecordingStickyCache) DeleteSessionAccountID(_ context.Context, _ int64, sessionHash string) error {
	delete(c.ttls, sessionHash)
	return nil
}

// compositeTargetRecordingConcurrencyCache 记录软负载分母与硬并发上限的实际入参。
type compositeTargetRecordingConcurrencyCache struct {
	ConcurrencyCache
	batchDenominators map[int64]int
	acquiredHardCap   map[int64]int
}

func (c *compositeTargetRecordingConcurrencyCache) GetAccountsLoadBatch(_ context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	if c.batchDenominators == nil {
		c.batchDenominators = make(map[int64]int)
	}
	loadMap := make(map[int64]*AccountLoadInfo, len(accounts))
	for _, account := range accounts {
		c.batchDenominators[account.ID] = account.MaxConcurrency
		loadMap[account.ID] = &AccountLoadInfo{AccountID: account.ID, LoadRate: 0}
	}
	return loadMap, nil
}

func (c *compositeTargetRecordingConcurrencyCache) AcquireAccountSlot(_ context.Context, accountID int64, maxConcurrency int, _ string) (bool, error) {
	if c.acquiredHardCap == nil {
		c.acquiredHardCap = make(map[int64]int)
	}
	c.acquiredHardCap[accountID] = maxConcurrency
	return true, nil
}

func (c *compositeTargetRecordingConcurrencyCache) ReleaseAccountSlot(context.Context, int64, string) error {
	return nil
}

// scheduling_weight 只调节软负载分母；硬并发仍由 account.Concurrency 在抢槽时强制，
// 两者互不影响（旧 load_factor 语义不受权重改写）。
func TestSchedulingWeightScalesSoftLoadOnly(t *testing.T) {
	account := Account{
		ID:          901,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 7,
		LoadFactor:  compositeTargetIntPtr(3),
	}
	recording := &compositeTargetRecordingConcurrencyCache{}
	concurrencyService := NewConcurrencyService(recording)
	concurrencyService.SetAccountLoadBatchCacheTTL(0)

	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	svc := &GatewayService{
		cfg:                cfg,
		accountRepo:        &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{PlatformAnthropic: {account}}},
		concurrencyService: concurrencyService,
	}

	target := CompositeRouteTarget{
		Position:         0,
		TargetPlatform:   PlatformAnthropic,
		UpstreamModel:    "claude-sonnet-4-6",
		SchedulingWeight: 10000,
		Enabled:          true,
	}
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched:        true,
		PublicModel:    "public-model",
		TargetPlatform: PlatformAnthropic,
		UpstreamModel:  target.UpstreamModel,
		Targets:        []CompositeRouteTarget{target},
		SelectedTarget: &target,
	})

	selection, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "", "public-model", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, int64(901), selection.Account.ID)

	// 软负载分母按权重放大：load_factor 3 × (10000/100) = 300。
	require.Equal(t, 300, recording.batchDenominators[901])
	// 硬并发上限仍是账号自己的 Concurrency，权重不改变它。
	require.Equal(t, 7, recording.acquiredHardCap[901])
}
