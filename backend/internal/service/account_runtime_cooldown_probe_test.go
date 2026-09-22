package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeRuntimeCooldownRecord 是冷却记录的进程内替身：deadline 是冷却截止时间，
// keepUntil 是记录本身的 TTL（冷却 + 恢复窗口）。
type fakeRuntimeCooldownRecord struct {
	deadline  time.Time
	keepUntil time.Time
}

type fakeRuntimeCooldownLease struct {
	owner     string
	keepUntil time.Time
}

// fakeRuntimeCooldownCache 按真实 Redis 语义实现 RuntimeCooldownCache：
// 记录/租约都有 TTL，租约只能由 owner 续期和 compare-and-delete 释放。
// 跨实例的真实性（真实 SET NX 竞争）由 repository 包针对隔离 Redis 的用例覆盖，
// 这里覆盖卡口自身的状态机与调用点。
type fakeRuntimeCooldownCache struct {
	mu      sync.Mutex
	records map[string]fakeRuntimeCooldownRecord
	leases  map[string]fakeRuntimeCooldownLease
}

func newFakeRuntimeCooldownCache() *fakeRuntimeCooldownCache {
	return &fakeRuntimeCooldownCache{
		records: make(map[string]fakeRuntimeCooldownRecord),
		leases:  make(map[string]fakeRuntimeCooldownLease),
	}
}

// runtimeCooldownProbeTestCache 是卡口用例使用的缓存替身。运行时冷却缓存是从
// RateLimitService.tempUnschedCache 上断言出来的，所以替身还必须满足
// TempUnschedCache；临时调度状态不在这条路径上，嵌入的接口保持 nil：
// 一旦被调用会立刻 panic，而不是静默返回假数据。
type runtimeCooldownProbeTestCache struct {
	TempUnschedCache
	*fakeRuntimeCooldownCache
}

func newRuntimeCooldownProbeTestCache() *runtimeCooldownProbeTestCache {
	return &runtimeCooldownProbeTestCache{fakeRuntimeCooldownCache: newFakeRuntimeCooldownCache()}
}

func (cache *fakeRuntimeCooldownCache) GetRuntimeCooldown(_ context.Context, key string) (time.Time, bool, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	record, found := cache.records[key]
	if !found || !time.Now().Before(record.keepUntil) {
		delete(cache.records, key)
		return time.Time{}, false, nil
	}
	return record.deadline, true, nil
}

func (cache *fakeRuntimeCooldownCache) SetRuntimeCooldown(_ context.Context, key string, until time.Time) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if until.IsZero() {
		delete(cache.records, key)
		return nil
	}
	remaining := time.Until(until)
	if remaining <= 0 {
		return nil
	}
	cache.records[key] = fakeRuntimeCooldownRecord{deadline: until, keepUntil: time.Now().Add(remaining + RuntimeCooldownRecoveryWindow)}
	return nil
}

func (cache *fakeRuntimeCooldownCache) DeleteRuntimeCooldown(_ context.Context, key string) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	delete(cache.records, key)
	return nil
}

func (cache *fakeRuntimeCooldownCache) AcquireRuntimeCooldownProbe(_ context.Context, key, owner string, ttl time.Duration) (bool, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	lease, found := cache.leases[key]
	if found && time.Now().Before(lease.keepUntil) {
		return false, nil
	}
	cache.leases[key] = fakeRuntimeCooldownLease{owner: owner, keepUntil: time.Now().Add(ttl)}
	return true, nil
}

func (cache *fakeRuntimeCooldownCache) RenewRuntimeCooldownProbe(_ context.Context, key, owner string, ttl, renewBelow time.Duration) (bool, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	lease, found := cache.leases[key]
	if !found || lease.owner != owner || !time.Now().Before(lease.keepUntil) {
		return false, nil
	}
	if time.Until(lease.keepUntil) < renewBelow {
		lease.keepUntil = time.Now().Add(ttl)
		cache.leases[key] = lease
	}
	return true, nil
}

func (cache *fakeRuntimeCooldownCache) ReleaseRuntimeCooldownProbe(_ context.Context, key, owner string) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	lease, found := cache.leases[key]
	if !found || lease.owner != owner || !time.Now().Before(lease.keepUntil) {
		return nil
	}
	delete(cache.leases, key)
	return nil
}

// seedExpiredRuntimeCooldown 写入一条"冷却刚到期、记录仍在恢复窗口内"的记录，
// 等价于冷却开始时写入、现在刚好越过截止时间。
func (cache *fakeRuntimeCooldownCache) seedExpiredRuntimeCooldown(key string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	now := time.Now()
	cache.records[key] = fakeRuntimeCooldownRecord{deadline: now.Add(-time.Second), keepUntil: now.Add(RuntimeCooldownRecoveryWindow)}
}

func (cache *fakeRuntimeCooldownCache) seedCoolingDown(key string, until time.Time) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.records[key] = fakeRuntimeCooldownRecord{deadline: until, keepUntil: until.Add(RuntimeCooldownRecoveryWindow)}
}

// shortLease 把租约剩余时间改短，模拟"试探还在进行、租约快到期"。
func (cache *fakeRuntimeCooldownCache) shortLease(key string, remaining time.Duration) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	lease, found := cache.leases[key]
	if !found {
		return
	}
	lease.keepUntil = time.Now().Add(remaining)
	cache.leases[key] = lease
}

// expireLease 模拟租约 TTL 到期后 Redis 自己删键（持有者没有 Release）。
func (cache *fakeRuntimeCooldownCache) expireLease(key string) {
	cache.shortLease(key, -time.Second)
}

func (cache *fakeRuntimeCooldownCache) liveLease(key string) (fakeRuntimeCooldownLease, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	lease, found := cache.leases[key]
	if !found || !time.Now().Before(lease.keepUntil) {
		return fakeRuntimeCooldownLease{}, false
	}
	return lease, true
}

func (cache *fakeRuntimeCooldownCache) recordedCooldown(key string) bool {
	_, recorded, _ := cache.GetRuntimeCooldown(context.Background(), key)
	return recorded
}

// newRuntimeCooldownProbeService 造一个把冷却缓存指向共享替身的网关实例：
// 多个实例共享同一个替身，等价于多个进程共用一个 Redis。
func newRuntimeCooldownProbeService(cache TempUnschedCache) *AntigravityGatewayService {
	return &AntigravityGatewayService{rateLimitService: NewRateLimitService(nil, nil, nil, nil, cache)}
}

func runtimeCooldownProbeTestAccount() *Account {
	return &Account{
		ID:          8123,
		Name:        "antigravity-cooldown-probe",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "probe-token", "project_id": "probe-project"},
	}
}

func runtimeCooldownProbeTestKey(account *Account) string {
	return AccountRuntimeCooldownKey(97, account, "gemini-3-pro-high")
}

const modelCapacityExhaustedBody = `{
	"error": {
		"code": 503,
		"status": "UNAVAILABLE",
		"details": [
			{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro-high"}, "reason": "MODEL_CAPACITY_EXHAUSTED"},
			{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "1s"}
		],
		"message": "No capacity available for model gemini-3-pro-high on the server"
	}
}`

// TestRuntimeCooldownRecoveryWindowOutlastsProbeLease 固定"恢复窗口必须长于租约"这个不变量：
// 否则租约还有效、冷却记录却已经消失，新请求会绕过卡口直接试探。
func TestRuntimeCooldownRecoveryWindowOutlastsProbeLease(t *testing.T) {
	require.Greater(t, RuntimeCooldownRecoveryWindow, runtimeCooldownProbeLeaseTTL,
		"the recovery window must outlive one probe lease")
	require.Less(t, runtimeCooldownProbeRenewBelow, runtimeCooldownProbeLeaseTTL,
		"renewal must only trigger while the lease is still valid")
}

// TestRuntimeCooldownRecoveryAdmitsExactlyOneProbeAcrossInstances 是判据 5 的
// 进程内版本：冷却到期后 12 个并发实例里恰好 1 个被放行，其余全部不试探。
func TestRuntimeCooldownRecoveryAdmitsExactlyOneProbeAcrossInstances(t *testing.T) {
	cache := newRuntimeCooldownProbeTestCache()
	account := runtimeCooldownProbeTestAccount()
	key := runtimeCooldownProbeTestKey(account)
	cache.seedExpiredRuntimeCooldown(key)

	const instanceCount = 12
	instances := make([]*AntigravityGatewayService, instanceCount)
	for index := range instances {
		instances[index] = newRuntimeCooldownProbeService(cache)
	}

	probes := make([]*RuntimeCooldownProbe, instanceCount)
	admitted := make([]bool, instanceCount)
	var ready, finished sync.WaitGroup
	ready.Add(instanceCount)
	finished.Add(instanceCount)
	start := make(chan struct{})
	for index := range instances {
		go func(instanceIndex int) {
			defer finished.Done()
			ready.Done()
			<-start
			probes[instanceIndex], admitted[instanceIndex] = instances[instanceIndex].AdmitRuntimeCooldownRecovery(context.Background(), key)
		}(index)
	}
	ready.Wait()
	close(start)
	finished.Wait()

	admittedCount := 0
	winnerIndex := -1
	for index, ok := range admitted {
		if ok {
			admittedCount++
			winnerIndex = index
		}
	}
	require.Equal(t, 1, admittedCount, "exactly one instance may probe after the cooldown expires")
	require.NotNil(t, probes[winnerIndex], "the winner must hold the cross-instance lease")
	for index, probe := range probes {
		if index != winnerIndex {
			require.Nil(t, probe, "losers must not hold a lease")
		}
	}
	t.Logf("concurrent instances=%d admitted=%d refused=%d lease_owner=%s", instanceCount, admittedCount, instanceCount-admittedCount, probes[winnerIndex].owner)

	// 胜出者释放后，下一个恢复窗口的请求可以再次被放行。
	probes[winnerIndex].Release()
	nextProbe, nextAdmitted := instances[0].AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.True(t, nextAdmitted)
	require.NotNil(t, nextProbe, "releasing the lease must let the next probe proceed")
	nextProbe.Release()
}

// TestRuntimeCooldownGateSeparatesCoolingRecordFromAbsence 覆盖卡口的三个入口状态：
// 冷却中不放行、无记录放行且不占租约、恢复窗口内放行并占用租约。
func TestRuntimeCooldownGateSeparatesCoolingRecordFromAbsence(t *testing.T) {
	cache := newRuntimeCooldownProbeTestCache()
	service := newRuntimeCooldownProbeService(cache)
	account := runtimeCooldownProbeTestAccount()
	key := runtimeCooldownProbeTestKey(account)

	// 没有冷却记录：常规请求放行，且不需要租约。
	probe, admitted := service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.True(t, admitted)
	require.Nil(t, probe)
	_, found := cache.liveLease(key)
	require.False(t, found, "normal traffic must not take the recovery lease")

	// 冷却期内：不放行，也不占用租约。
	cache.seedCoolingDown(key, time.Now().Add(antigravityModelCapacityCooldown))
	probe, admitted = service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.False(t, admitted)
	require.Nil(t, probe)

	// 冷却到期（恢复窗口内）：唯一胜出者放行并占用租约，同时到达的其他实例不放行。
	cache.seedExpiredRuntimeCooldown(key)
	probe, admitted = service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.True(t, admitted)
	require.NotNil(t, probe)
	loser, loserAdmitted := service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.False(t, loserAdmitted)
	require.Nil(t, loser)
	probe.Release()
}

// TestRuntimeCooldownProbeLeaseOwnershipIsChecked 覆盖"租约能校验归属"：
// 非 owner 释放无效；租约过期被别人接手后，前持有者的 Release 不能删掉新租约。
func TestRuntimeCooldownProbeLeaseOwnershipIsChecked(t *testing.T) {
	cache := newRuntimeCooldownProbeTestCache()
	service := newRuntimeCooldownProbeService(cache)
	account := runtimeCooldownProbeTestAccount()
	key := runtimeCooldownProbeTestKey(account)
	cache.seedExpiredRuntimeCooldown(key)

	holder, admitted := service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.True(t, admitted)
	require.NotNil(t, holder)

	require.NoError(t, cache.ReleaseRuntimeCooldownProbe(context.Background(), key, "foreign-owner"))
	lease, found := cache.liveLease(key)
	require.True(t, found)
	require.Equal(t, holder.owner, lease.owner, "a foreign owner must not release the lease")

	// 租约到期后另一个实例接手：前持有者的 Release 必须是 no-op。
	cache.expireLease(key)
	successor, successorAdmitted := service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.True(t, successorAdmitted)
	require.NotNil(t, successor)
	holder.Release()
	lease, found = cache.liveLease(key)
	require.True(t, found)
	require.Equal(t, successor.owner, lease.owner, "the expired holder must not release the successor's lease")
	successor.Release()
	_, found = cache.liveLease(key)
	require.False(t, found)
}

// TestRuntimeCooldownProbeRenewOnlyExtendsHeldLeaseWhileLow 覆盖"租约能续期"：
// 剩余租约充足时不写；剩余不足时延长；丢掉租约后不能再延长别人的租约。
func TestRuntimeCooldownProbeRenewOnlyExtendsHeldLeaseWhileLow(t *testing.T) {
	cache := newRuntimeCooldownProbeTestCache()
	service := newRuntimeCooldownProbeService(cache)
	account := runtimeCooldownProbeTestAccount()
	key := runtimeCooldownProbeTestKey(account)
	cache.seedExpiredRuntimeCooldown(key)

	probe, admitted := service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.True(t, admitted)
	require.NotNil(t, probe)
	before, found := cache.liveLease(key)
	require.True(t, found)

	probe.Renew(context.Background())
	afterPlenty, found := cache.liveLease(key)
	require.True(t, found)
	require.Equal(t, before.keepUntil, afterPlenty.keepUntil, "a lease with enough TTL must not be rewritten")

	// 剩余租约不足：续期必须把租约恢复到完整 TTL。
	cache.shortLease(key, runtimeCooldownProbeRenewBelow/4)
	probe.Renew(context.Background())
	renewed, found := cache.liveLease(key)
	require.True(t, found)
	require.Greater(t, time.Until(renewed.keepUntil), runtimeCooldownProbeRenewBelow, "renewal must restore the full lease TTL")

	// 租约被别的实例接手后，前持有者的 Renew 不能再延长它。
	cache.expireLease(key)
	successor, successorAdmitted := service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.True(t, successorAdmitted)
	cache.shortLease(key, time.Second)
	probe.Renew(context.Background())
	stillShort, found := cache.liveLease(key)
	require.True(t, found)
	require.Equal(t, successor.owner, stillShort.owner)
	require.Less(t, time.Until(stillShort.keepUntil), 2*time.Second, "the expired holder must not renew the successor's lease")
	successor.Release()
}

// TestRuntimeCooldownProbeFailureKeepsCoolingAndFreesLease 是判据 6 的失败用例：
// 试探失败 → 账号重新进入冷却（不放行），租约被释放给下一个恢复窗口。
func TestRuntimeCooldownProbeFailureKeepsCoolingAndFreesLease(t *testing.T) {
	cache := newRuntimeCooldownProbeTestCache()
	service := newRuntimeCooldownProbeService(cache)
	account := runtimeCooldownProbeTestAccount()
	key := runtimeCooldownProbeTestKey(account)
	cache.seedExpiredRuntimeCooldown(key)

	probe, admitted := service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.True(t, admitted)
	require.NotNil(t, probe)

	service.markRuntimeCooldownFailed(context.Background(), key)

	refusedProbe, refusedAdmitted := service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.False(t, refusedAdmitted, "a failed probe must put the scope back into cooldown")
	require.Nil(t, refusedProbe)
	until, recorded := service.runtimeCooldown(context.Background(), key)
	require.True(t, recorded)
	require.True(t, time.Now().Before(until), "the cooldown deadline must be in the future again")
	t.Logf("failed probe re-cooled the scope until=%s recorded=%v", until.Format(time.RFC3339Nano), recorded)

	probe.Release()
	_, found := cache.liveLease(key)
	require.False(t, found, "the failed probe must free its lease")
}

// TestRuntimeCooldownProbeSuccessClearsCooldownAndFreesLease 是判据 6 的成功用例：
// 试探成功 → 冷却记录被清除（不再拦截常规请求），租约被释放。
func TestRuntimeCooldownProbeSuccessClearsCooldownAndFreesLease(t *testing.T) {
	cache := newRuntimeCooldownProbeTestCache()
	service := newRuntimeCooldownProbeService(cache)
	account := runtimeCooldownProbeTestAccount()
	key := runtimeCooldownProbeTestKey(account)
	cache.seedExpiredRuntimeCooldown(key)

	probe, admitted := service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.True(t, admitted)
	require.NotNil(t, probe)

	service.markRuntimeCooldownRecovered(context.Background(), key)
	require.False(t, cache.recordedCooldown(key), "a successful probe must clear the cooldown record")

	nextProbe, nextAdmitted := service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.True(t, nextAdmitted, "cleared cooldown admits normal traffic immediately")
	require.Nil(t, nextProbe, "normal traffic after recovery must not need a lease")
	t.Logf("recovered scope admitted normal traffic without a lease, recorded=%v", cache.recordedCooldown(key))

	probe.Release()
	_, found := cache.liveLease(key)
	require.False(t, found, "the successful probe must free its lease")
}

func newModelCapacityExhaustedResponse() (*http.Response, []byte) {
	body := []byte(modelCapacityExhaustedBody)
	return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}, body
}

func newModelCapacityRetryParams(upstream HTTPUpstream, account *Account) antigravityRetryLoopParams {
	return antigravityRetryLoopParams{
		ctx:          context.Background(),
		prefix:       "[cooldown-probe-test]",
		account:      account,
		accessToken:  "probe-token",
		action:       "generateContent",
		body:         []byte(`{"input":"probe"}`),
		httpUpstream: upstream,
		accountRepo:  &stubAntigravityAccountRepo{},
		// 与 runtimeCooldownProbeTestKey 使用同一个 group，确保冷却记录命中同一作用域。
		groupID: 97,
		handleError: func(context.Context, string, *Account, int, http.Header, []byte, string, int64, string, bool) *handleModelRateLimitResult {
			return nil
		},
	}
}

// TestHandleSmartRetryModelCapacityProbeWinnerRetriesAndClearsCooldown 验证调用点：
// 唯一胜出者照常重试，成功后清掉冷却记录并释放租约。
func TestHandleSmartRetryModelCapacityProbeWinnerRetriesAndClearsCooldown(t *testing.T) {
	cache := newRuntimeCooldownProbeTestCache()
	service := newRuntimeCooldownProbeService(cache)
	account := runtimeCooldownProbeTestAccount()
	key := runtimeCooldownProbeTestKey(account)
	cache.seedExpiredRuntimeCooldown(key)

	leaseHeldWhileProbing := false
	upstream := &queuedHTTPUpstreamStub{
		responses: []*http.Response{{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}},
		onCall: func(*http.Request, *queuedHTTPUpstreamStub) {
			if _, held := cache.liveLease(key); held {
				leaseHeldWhileProbing = true
			}
		},
	}
	resp, body := newModelCapacityExhaustedResponse()
	result := service.handleSmartRetry(newModelCapacityRetryParams(upstream, account), resp, body, "https://ag-probe.test", 0, []string{"https://ag-probe.test"})

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	require.NotNil(t, result.resp)
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	require.Equal(t, 1, upstream.callCount, "the winning probe must retry the upstream once")
	require.True(t, leaseHeldWhileProbing, "the winner must hold the cross-instance lease while probing")
	require.False(t, cache.recordedCooldown(key), "a successful probe must clear the cooldown record")
	_, leaseFound := cache.liveLease(key)
	require.False(t, leaseFound, "the winning probe must release its lease")
}

// TestHandleSmartRetryModelCapacityProbeLoserFailsFastWithoutRetrying 验证调用点：
// 拿不到租约的请求不干等、也不再撞账号，直接按既有语义返回上游错误。
func TestHandleSmartRetryModelCapacityProbeLoserFailsFastWithoutRetrying(t *testing.T) {
	cache := newRuntimeCooldownProbeTestCache()
	service := newRuntimeCooldownProbeService(cache)
	account := runtimeCooldownProbeTestAccount()
	key := runtimeCooldownProbeTestKey(account)
	cache.seedExpiredRuntimeCooldown(key)

	// 另一个实例正在试探：它持有租约。
	probingInstance := newRuntimeCooldownProbeService(cache)
	probe, admitted := probingInstance.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.True(t, admitted)
	require.NotNil(t, probe)
	defer probe.Release()

	upstream := &queuedHTTPUpstreamStub{}
	resp, body := newModelCapacityExhaustedResponse()
	startedAt := time.Now()
	result := service.handleSmartRetry(newModelCapacityRetryParams(upstream, account), resp, body, "https://ag-probe.test", 0, []string{"https://ag-probe.test"})
	elapsed := time.Since(startedAt)

	require.NotNil(t, result)
	require.Equal(t, smartRetryActionBreakWithResp, result.action)
	require.Nil(t, result.err)
	require.Nil(t, result.switchError, "MODEL_CAPACITY_EXHAUSTED must not switch accounts (shared capacity)")
	require.NotNil(t, result.resp)
	require.Equal(t, http.StatusServiceUnavailable, result.resp.StatusCode)
	returnedBody, err := io.ReadAll(result.resp.Body)
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(returnedBody), "the loser must return the upstream error unchanged")
	require.Zero(t, upstream.callCount, "a loser must not retry the upstream account at all")
	require.Less(t, elapsed, 2*antigravityModelCapacityRetryWait, "a loser must fail fast instead of waiting for the retry budget (60 × 1s)")
	loserProbe, loserAdmitted := service.AdmitRuntimeCooldownRecovery(context.Background(), key)
	require.False(t, loserAdmitted, "the lease stays with the probing instance")
	require.Nil(t, loserProbe)
	t.Logf("loser failed fast in %s while the probing instance kept the lease", elapsed.Round(time.Millisecond))
}
