package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"strconv"
	"sync"
	"time"
)

type EgressSource string

const (
	EgressSourceDirect              EgressSource = "direct"
	EgressSourceSingleProxy         EgressSource = "single_proxy"
	EgressSourceGroupBinding        EgressSource = "group_binding"
	EgressSourceGroupFallbackDirect EgressSource = "group_fallback_direct"
	EgressSourceGroupFallbackProxy  EgressSource = "group_fallback_proxy"
)

// EgressTarget is an operation's selected route, never account configuration.
type EgressTarget struct {
	URL      string
	ProxyID  int64
	Source   EgressSource
	Resolved bool
}

type accountEgress struct {
	mu         sync.Mutex
	runtime    *ProxyGroupRuntime
	target     EgressTarget
	committed  bool
	alternated bool
}

// AttachEgress starts a fresh operation without modifying logical identity.
// Call only on a private account copy, never a shared scheduler snapshot.
func (a *Account) AttachEgress(runtime *ProxyGroupRuntime) {
	a.egress = &accountEgress{runtime: runtime}
}

// ResolvedEgress is observational only; catalog/logging paths never resolve or
// contact a cache merely to inspect an account's configuration.
func (a *Account) ResolvedEgress() (EgressTarget, bool) {
	if a == nil || a.egress == nil {
		return EgressTarget{}, false
	}
	a.egress.mu.Lock()
	defer a.egress.mu.Unlock()
	return a.egress.target, a.egress.target.Resolved
}

// EgressProxyURL fails closed for a configured group. An unresolved group is
// never represented by the empty proxy URL used by legacy host-default routing.
func (a *Account) EgressProxyURL(ctx context.Context) (string, error) {
	if a == nil {
		return "", ErrProxyGroupUnavailable
	}
	if a.StagedProxyPending() {
		return "", ErrStagedProxyPending
	}
	if a.ProxyGroupID == nil {
		if a.ProxyID != nil && a.Proxy != nil {
			return a.Proxy.URL(), nil
		}
		return "", nil
	}
	if a.ProxyID != nil {
		return "", ErrProxyBindingConflict
	}
	if a.egress == nil {
		return "", ErrProxyGroupUnavailable
	}
	a.egress.mu.Lock()
	defer a.egress.mu.Unlock()
	if !a.egress.target.Resolved {
		if a.egress.runtime == nil {
			return "", ErrProxyGroupUnavailable
		}
		target, err := a.egress.runtime.resolve(ctx, a)
		if err != nil {
			return "", err
		}
		a.egress.target = target
	}
	return a.egress.target.URL, nil
}

type ProxyGroupRuntime struct {
	groups  ProxyGroupRepository
	proxies ProxyRepository
	cache   ProxyGroupCache
	now     func() time.Time
}

func NewProxyGroupRuntime(groups ProxyGroupRepository, proxies ProxyRepository, cache ProxyGroupCache) *ProxyGroupRuntime {
	return &ProxyGroupRuntime{groups: groups, proxies: proxies, cache: cache, now: time.Now}
}

func (r *ProxyGroupRuntime) ApplyEgress(ctx context.Context, account *Account) (*Account, EgressTarget, error) {
	if account == nil {
		return nil, EgressTarget{}, ErrProxyGroupUnavailable
	}
	copy := *account
	copy.AttachEgress(r)
	target, err := r.resolve(ctx, &copy)
	if err != nil {
		return nil, EgressTarget{}, err
	}
	copy.egress.target = target
	return &copy, target, nil
}

func (r *ProxyGroupRuntime) resolve(ctx context.Context, a *Account) (EgressTarget, error) {
	return r.resolveExcluding(ctx, a, 0, false)
}

func (r *ProxyGroupRuntime) resolveExcluding(ctx context.Context, a *Account, excludedProxyID int64, advanceAfterFailure bool) (EgressTarget, error) {
	if a.StagedProxyPending() {
		return EgressTarget{}, ErrStagedProxyPending
	}
	if a.ProxyGroupID == nil {
		if a.ProxyID != nil && a.Proxy != nil {
			return EgressTarget{a.Proxy.URL(), *a.ProxyID, EgressSourceSingleProxy, true}, nil
		}
		return EgressTarget{Source: EgressSourceDirect, Resolved: true}, nil
	}
	if a.ProxyID != nil {
		return EgressTarget{}, ErrProxyBindingConflict
	}
	if a.ID <= 0 {
		return EgressTarget{}, ErrProxyGroupUnavailable
	}
	bindingID := egressBindingAccountID(a)
	workspaceID := a.WorkspaceID
	if workspaceID <= 0 {
		workspaceID = domain.ResourceWorkspaceID(ctx)
	}
	return r.selectTargetExcluding(ctx, *a.ProxyGroupID, workspaceID, bindingID, strconv.FormatInt(bindingID, 10), excludedProxyID, advanceAfterFailure)
}

func eligibleProxy(p *Proxy, workspaceID int64, now time.Time) bool {
	return p != nil && p.ID > 0 && p.WorkspaceID == workspaceID && p.Status == StatusActive && p.DeletedAt == nil && !p.IsExpired(now)
}

// EligibleNode intentionally ignores descriptive tags and unknown probe metadata.
func EligibleNode(member ProxyGroupMember, group *ProxyGroup, now time.Time) bool {
	return group != nil && member.GroupID == group.ID && member.WorkspaceID == group.WorkspaceID && member.Proxy != nil && member.ProxyID == member.Proxy.ID && eligibleProxy(member.Proxy, group.WorkspaceID, now)
}

func (r *ProxyGroupRuntime) candidates(ctx context.Context, groupID, workspaceID int64) (*ProxyGroup, map[int64]*Proxy, error) {
	if r == nil || r.groups == nil || r.cache == nil {
		return nil, nil, ErrProxyGroupUnavailable
	}
	g, err := r.groups.GetByID(ctx, groupID)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: group lookup: %w", ErrProxyGroupUnavailable, err)
	}
	if g == nil || g.ID != groupID || g.DeletedAt != nil || g.Status != StatusActive || (workspaceID > 0 && g.WorkspaceID != workspaceID) {
		return nil, nil, ErrProxyGroupUnavailable
	}
	nodes := make(map[int64]*Proxy)
	for _, member := range g.Members {
		if !EligibleNode(member, g, r.now()) {
			continue
		}
		cooldown, err := r.cache.GetCooldown(ctx, member.ProxyID)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: cooldown lookup: %w", ErrProxyGroupUnavailable, err)
		}
		if cooldown == nil {
			nodes[member.ProxyID] = member.Proxy
		}
	}
	return g, nodes, nil
}

func bindingTTL(g *ProxyGroup) time.Duration {
	seconds := g.BindingTTLSeconds
	if seconds <= 0 {
		seconds = DefaultProxyGroupBindingTTLSeconds
	}
	return time.Duration(seconds) * time.Second
}

func rendezvousProxy(groupID int64, seed string, nodes map[int64]*Proxy) int64 {
	var best int64
	var score [32]byte
	for id := range nodes {
		h := sha256.Sum256([]byte(strconv.FormatInt(groupID, 10) + "/" + seed + "/" + strconv.FormatInt(id, 10)))
		comparison := bytes.Compare(h[:], score[:])
		if best == 0 || comparison > 0 || (comparison == 0 && id < best) {
			best, score = id, h
		}
	}
	return best
}

func (r *ProxyGroupRuntime) selectTarget(ctx context.Context, groupID, workspaceID, accountID int64, seed string) (EgressTarget, error) {
	return r.selectTargetExcluding(ctx, groupID, workspaceID, accountID, seed, 0, false)
}

// rendezvousProxyAfter advances in the same descending score / ascending ID order
// as initial allocation. The failed node's score remains a cursor even if it has
// left the group; wrap only when no eligible node follows it.
func rendezvousProxyAfter(groupID int64, seed string, nodes map[int64]*Proxy, failedProxyID int64) int64 {
	failedScore := sha256.Sum256([]byte(strconv.FormatInt(groupID, 10) + "/" + seed + "/" + strconv.FormatInt(failedProxyID, 10)))
	var next int64
	var nextScore [32]byte
	for id := range nodes {
		h := sha256.Sum256([]byte(strconv.FormatInt(groupID, 10) + "/" + seed + "/" + strconv.FormatInt(id, 10)))
		comparison := bytes.Compare(h[:], failedScore[:])
		if comparison > 0 || (comparison == 0 && id <= failedProxyID) {
			continue
		}
		comparison = bytes.Compare(h[:], nextScore[:])
		if next == 0 || comparison > 0 || (comparison == 0 && id < next) {
			next, nextScore = id, h
		}
	}
	if next != 0 {
		return next
	}
	return rendezvousProxy(groupID, seed, nodes)
}

func (r *ProxyGroupRuntime) selectTargetExcluding(ctx context.Context, groupID, workspaceID, accountID int64, seed string, excludedProxyID int64, advanceAfterFailure bool) (EgressTarget, error) {
	// Retry only cache contention/revocation, never network operations. Every
	// winner is revalidated against a fresh atomic group membership snapshot.
	for attempt := 0; attempt < 4; attempt++ {
		g, nodes, err := r.candidates(ctx, groupID, workspaceID)
		if err != nil {
			return EgressTarget{}, err
		}
		delete(nodes, excludedProxyID)
		if accountID == 0 {
			if id := rendezvousProxy(groupID, seed, nodes); id > 0 {
				return EgressTarget{nodes[id].URL(), id, EgressSourceGroupBinding, true}, nil
			}
			return r.fallbackExcluding(ctx, g, excludedProxyID)
		}
		bound, err := r.cache.GetBinding(ctx, groupID, accountID)
		if err != nil {
			return EgressTarget{}, fmt.Errorf("%w: binding lookup: %w", ErrProxyGroupUnavailable, err)
		}
		if bound == 0 {
			selected := rendezvousProxy(groupID, seed, nodes)
			if advanceAfterFailure && excludedProxyID > 0 {
				selected = rendezvousProxyAfter(groupID, seed, nodes, excludedProxyID)
			}
			if selected == 0 {
				fallback, err := r.fallbackExcluding(ctx, g, excludedProxyID)
				if err != nil || fallback.ProxyID == 0 {
					return fallback, err
				}
				selected = fallback.ProxyID
			}
			bound, err = r.cache.TrySetBindingNX(ctx, groupID, accountID, selected, bindingTTL(g))
			if err != nil {
				return EgressTarget{}, fmt.Errorf("%w: binding selection: %w", ErrProxyGroupUnavailable, err)
			}
		}
		g, nodes, err = r.candidates(ctx, groupID, workspaceID)
		if err != nil {
			return EgressTarget{}, err
		}
		delete(nodes, excludedProxyID)
		p := nodes[bound]
		source := EgressSourceGroupBinding
		if p == nil && bound != excludedProxyID && g.FallbackMode == FallbackModeProxy && g.FallbackProxyID != nil && *g.FallbackProxyID == bound {
			p, err = r.fallbackProxy(ctx, g)
			if err != nil {
				return EgressTarget{}, err
			}
			source = EgressSourceGroupFallbackProxy
		}
		if p == nil {
			if _, err = r.cache.DeleteBindingIfValue(ctx, groupID, accountID, bound); err != nil {
				return EgressTarget{}, fmt.Errorf("%w: winner eviction: %w", ErrProxyGroupUnavailable, err)
			}
			continue
		}
		ok, err := r.cache.TouchBindingIfValue(ctx, groupID, accountID, bound, bindingTTL(g))
		if err != nil {
			return EgressTarget{}, fmt.Errorf("%w: binding renewal: %w", ErrProxyGroupUnavailable, err)
		}
		if ok {
			return EgressTarget{p.URL(), p.ID, source, true}, nil
		}
	}
	return EgressTarget{}, ErrProxyGroupUnavailable
}

func (r *ProxyGroupRuntime) fallback(ctx context.Context, g *ProxyGroup) (EgressTarget, error) {
	return r.fallbackExcluding(ctx, g, 0)
}

func (r *ProxyGroupRuntime) fallbackExcluding(ctx context.Context, g *ProxyGroup, excludedProxyID int64) (EgressTarget, error) {
	switch g.FallbackMode {
	case FallbackModeDirect:
		return EgressTarget{Source: EgressSourceGroupFallbackDirect, Resolved: true}, nil
	case FallbackModeProxy:
		if g.FallbackProxyID != nil && *g.FallbackProxyID == excludedProxyID {
			return EgressTarget{}, ErrProxyGroupUnavailable
		}
		p, err := r.fallbackProxy(ctx, g)
		if err != nil {
			return EgressTarget{}, err
		}
		if p != nil {
			return EgressTarget{p.URL(), p.ID, EgressSourceGroupFallbackProxy, true}, nil
		}
	}
	return EgressTarget{}, ErrProxyGroupUnavailable
}

func (r *ProxyGroupRuntime) fallbackProxy(ctx context.Context, g *ProxyGroup) (*Proxy, error) {
	if g.FallbackProxyID == nil || r.proxies == nil {
		return nil, nil
	}
	p, err := r.proxies.GetByID(ctx, *g.FallbackProxyID)
	if errors.Is(err, ErrProxyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: fallback lookup: %w", ErrProxyGroupUnavailable, err)
	}
	if p == nil || p.ID != *g.FallbackProxyID || !eligibleProxy(p, g.WorkspaceID, r.now()) {
		return nil, nil
	}
	cooled, err := r.cache.GetCooldown(ctx, p.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: fallback cooldown: %w", ErrProxyGroupUnavailable, err)
	}
	if cooled != nil {
		return nil, nil
	}
	return p, nil
}

func (r *ProxyGroupRuntime) ResolveForNewSession(ctx context.Context, groupID int64, sessionSeed string) (EgressTarget, error) {
	if groupID <= 0 || sessionSeed == "" {
		return EgressTarget{}, ErrProxyGroupUnavailable
	}
	return r.selectTarget(ctx, groupID, domain.ResourceWorkspaceID(ctx), 0, sessionSeed)
}

func (r *ProxyGroupRuntime) ConfirmSessionBinding(ctx context.Context, group *ProxyGroup, accountID, proxyID int64) error {
	return r.confirmSessionBinding(ctx, group, accountID, proxyID, "")
}

func (r *ProxyGroupRuntime) confirmSessionBinding(ctx context.Context, group *ProxyGroup, accountID, proxyID int64, expectedURL string) error {
	if group == nil || accountID <= 0 {
		return ErrProxyBindingConflict
	}
	g, nodes, err := r.candidates(ctx, group.ID, group.WorkspaceID)
	if err != nil {
		return err
	}
	if nodes[proxyID] == nil || (expectedURL != "" && nodes[proxyID].URL() != expectedURL) {
		return ErrProxyGroupUnavailable
	}
	winner, err := r.cache.TrySetBindingNX(ctx, g.ID, accountID, proxyID, bindingTTL(g))
	if err != nil {
		return fmt.Errorf("%w: session binding: %w", ErrProxyGroupUnavailable, err)
	}
	if winner != proxyID {
		return ErrProxyBindingConflict
	}
	_, nodes, err = r.candidates(ctx, g.ID, g.WorkspaceID)
	if err != nil || nodes[proxyID] == nil || (expectedURL != "" && nodes[proxyID].URL() != expectedURL) {
		_, _ = r.cache.DeleteBindingIfValue(ctx, g.ID, accountID, proxyID)
		if err != nil {
			return err
		}
		return ErrProxyGroupUnavailable
	}
	return nil
}

// ProxyConnectFailure is produced only when the transport proves the proxy
// endpoint itself failed before any application bytes were sent. Destination
// CONNECT failures, HTTP status codes and cancellation cannot construct it.
type ProxyConnectFailure struct{ Cause error }

// ProxyTLSHandshakeFailure permits only an operation-local alternate. A
// destination handshake failure is never evidence for global node cooldown.
// Producers must prove no connection or application write on any prior attempt.
type ProxyTLSHandshakeFailure struct{ Cause error }

func (e *ProxyTLSHandshakeFailure) Error() string { return e.Cause.Error() }
func (e *ProxyTLSHandshakeFailure) Unwrap() error { return e.Cause }

func (e *ProxyConnectFailure) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "proxy connection failed before application write"
}
func (e *ProxyConnectFailure) Unwrap() error { return e.Cause }

func (r *ProxyGroupRuntime) ReportTransportFailure(ctx context.Context, groupID, accountID, proxyID int64, cause error) error {
	var failure *ProxyConnectFailure
	if !errors.As(cause, &failure) || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return nil
	}
	if groupID <= 0 || accountID <= 0 || proxyID <= 0 {
		return ErrProxyBindingConflict
	}
	now := r.now()
	_, err := r.cache.SetCooldownIfAbsent(ctx, ProxyCooldown{ProxyID: proxyID, Reason: "proxy_connect_failed", ObservedAt: now, ExpiresAt: now.Add(10 * time.Minute)}, 10*time.Minute)
	if err != nil {
		return err
	}
	_, err = r.cache.DeleteBindingIfValue(ctx, groupID, accountID, proxyID)
	return err
}

func (r *ProxyGroupRuntime) ClearCooldown(ctx context.Context, proxyID int64) error {
	return r.cache.ClearCooldown(ctx, proxyID)
}

func egressBindingAccountID(a *Account) int64 {
	if a.ParentAccountID != nil && *a.ParentAccountID > 0 {
		return *a.ParentAccountID
	}
	return a.ID
}
