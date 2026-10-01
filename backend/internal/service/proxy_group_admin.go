package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// ProxyGroupAdminService manages configuration and observations, never account routing.
type ProxyGroupAdminService struct {
	groups  ProxyGroupRepository
	proxies ProxyRepository
	cache   ProxyGroupCache
}

func NewProxyGroupAdminService(groups ProxyGroupRepository, proxies ProxyRepository, cache ProxyGroupCache) *ProxyGroupAdminService {
	return &ProxyGroupAdminService{groups: groups, proxies: proxies, cache: cache}
}

type ProxyGroupAdminInput struct {
	Name, Description, Region, Status, FallbackMode *string
	TargetServices                                  *[]string
	FallbackProxyID                                 NullableInt64Update
	BindingTTLSeconds                               *int
}

type ProxyGroupAdminView struct {
	Group                  *ProxyGroup
	Members                []ProxyGroupMemberObservation
	SchedulableMemberCount int
	BoundAccountCount      int64
}

type ProxyGroupMemberObservation struct {
	Proxy    *Proxy
	Cooldown *ProxyCooldown
}

type ProxyGroupBindingObservation struct {
	AccountID   int64      `json:"account_id"`
	AccountName string     `json:"account_name"`
	ProxyID     *int64     `json:"proxy_id"`
	ProxyName   string     `json:"proxy_name,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Eligible    bool       `json:"eligible"`
}

func (s *ProxyGroupAdminService) group(ctx context.Context, id int64) (*ProxyGroup, error) {
	g, err := s.groups.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if g == nil || g.DeletedAt != nil {
		return nil, ErrProxyGroupNotFound
	}
	if workspaceID, scoped := domain.WorkspaceScope(ctx); scoped && g.WorkspaceID != workspaceID {
		return nil, ErrWorkspaceNotFound
	}
	return g, nil
}

func (s *ProxyGroupAdminService) Get(ctx context.Context, id int64) (*ProxyGroupAdminView, error) {
	g, err := s.group(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.observe(ctx, g)
}

func (s *ProxyGroupAdminService) List(ctx context.Context, params pagination.PaginationParams, status, targetService, search string) ([]ProxyGroupAdminView, *pagination.PaginationResult, error) {
	if status != "" && status != StatusActive && status != "inactive" {
		return nil, nil, invalidProxyGroup("invalid status")
	}
	groups, page, err := s.groups.List(ctx, params, status, targetService, strings.TrimSpace(search))
	if err != nil {
		return nil, nil, err
	}
	out := make([]ProxyGroupAdminView, 0, len(groups))
	for i := range groups {
		if workspaceID, scoped := domain.WorkspaceScope(ctx); scoped && groups[i].WorkspaceID != workspaceID {
			return nil, nil, ErrWorkspaceNotFound
		}
		view, err := s.observe(ctx, &groups[i])
		if err != nil {
			return nil, nil, err
		}
		out = append(out, *view)
	}
	return out, page, nil
}

func (s *ProxyGroupAdminService) observe(ctx context.Context, g *ProxyGroup) (*ProxyGroupAdminView, error) {
	members, err := s.groups.ListMembers(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	view := &ProxyGroupAdminView{Group: g, Members: make([]ProxyGroupMemberObservation, 0, len(members))}
	now := time.Now()
	for _, member := range members {
		p := member.Proxy
		if p == nil || p.DeletedAt != nil || p.WorkspaceID != g.WorkspaceID {
			continue
		}
		cooldown, err := s.cache.GetCooldown(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if cooldown != nil && !cooldown.ExpiresAt.After(now) {
			cooldown = nil
		}
		view.Members = append(view.Members, ProxyGroupMemberObservation{Proxy: p, Cooldown: cooldown})
		if proxyGroupAdminCandidate(p, g.WorkspaceID, cooldown, now) {
			view.SchedulableMemberCount++
		}
	}
	view.BoundAccountCount, err = s.groups.CountAccountsByGroupID(ctx, g.ID)
	return view, err
}

// This is a basic candidate count, not a claim that a target service is reachable.
func proxyGroupAdminCandidate(p *Proxy, workspaceID int64, cooldown *ProxyCooldown, now time.Time) bool {
	return p != nil && p.WorkspaceID == workspaceID && p.DeletedAt == nil && p.IsActive() && !p.IsExpired(now) && (cooldown == nil || !cooldown.ExpiresAt.After(now))
}

func invalidProxyGroup(message string) error {
	return infraerrors.BadRequest("INVALID_PROXY_GROUP", message)
}

func (s *ProxyGroupAdminService) apply(ctx context.Context, g *ProxyGroup, in ProxyGroupAdminInput) error {
	if in.Name != nil {
		g.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		g.Description = *in.Description
	}
	if in.Region != nil {
		g.Region = strings.TrimSpace(*in.Region)
	}
	if in.Status != nil {
		g.Status = *in.Status
	}
	if in.FallbackMode != nil {
		g.FallbackMode = *in.FallbackMode
	}
	if in.TargetServices != nil {
		g.TargetServices = append([]string{}, (*in.TargetServices)...)
	}
	if in.BindingTTLSeconds != nil {
		g.BindingTTLSeconds = *in.BindingTTLSeconds
	}
	if in.FallbackProxyID.Set {
		g.FallbackProxyID = in.FallbackProxyID.Value
		if g.FallbackProxyID != nil && *g.FallbackProxyID == 0 {
			g.FallbackProxyID = nil
		}
	}
	if g.Name == "" || len([]rune(g.Name)) > 100 {
		return invalidProxyGroup("name must contain 1 to 100 characters")
	}
	if len([]rune(g.Region)) > 100 {
		return invalidProxyGroup("region must contain at most 100 characters")
	}
	if g.Status != StatusActive && g.Status != "inactive" {
		return invalidProxyGroup("invalid status")
	}
	// time.Duration must also represent the configured TTL without overflowing.
	if g.BindingTTLSeconds <= 0 || int64(g.BindingTTLSeconds) > int64((1<<63-1)/time.Second) {
		return invalidProxyGroup("binding TTL must be positive and representable")
	}
	switch g.FallbackMode {
	case FallbackModeNone, FallbackModeDirect:
		if g.FallbackProxyID != nil {
			return invalidProxyGroup("fallback proxy requires proxy mode")
		}
	case FallbackModeProxy:
		if g.FallbackProxyID == nil || *g.FallbackProxyID <= 0 {
			return invalidProxyGroup("fallback proxy is required")
		}
		if _, err := s.proxyInWorkspace(ctx, *g.FallbackProxyID, g.WorkspaceID); err != nil {
			return err
		}
	default:
		return invalidProxyGroup("invalid fallback mode")
	}
	return nil
}

func (s *ProxyGroupAdminService) proxyInWorkspace(ctx context.Context, id, workspaceID int64) (*Proxy, error) {
	p, err := s.proxies.GetByID(domain.WithWorkspaceScope(ctx, workspaceID), id)
	if err != nil {
		return nil, err
	}
	if p == nil || p.DeletedAt != nil {
		return nil, ErrProxyNotFound
	}
	if p.WorkspaceID != workspaceID {
		return nil, ErrWorkspaceNotFound
	}
	return p, nil
}

func (s *ProxyGroupAdminService) Create(ctx context.Context, in ProxyGroupAdminInput) (*ProxyGroupAdminView, error) {
	g := &ProxyGroup{WorkspaceID: domain.ResourceWorkspaceID(ctx), Status: StatusActive, FallbackMode: FallbackModeNone, BindingTTLSeconds: DefaultProxyGroupBindingTTLSeconds, TargetServices: []string{}}
	if err := s.apply(ctx, g, in); err != nil {
		return nil, err
	}
	if err := s.groups.Create(ctx, g); err != nil {
		return nil, err
	}
	return s.observe(ctx, g)
}

func (s *ProxyGroupAdminService) Update(ctx context.Context, id int64, in ProxyGroupAdminInput) (*ProxyGroupAdminView, error) {
	g, err := s.group(ctx, id)
	if err != nil {
		return nil, err
	}
	updated := *g
	if err := s.apply(ctx, &updated, in); err != nil {
		return nil, err
	}
	if err := s.groups.Update(ctx, &updated); err != nil {
		return nil, err
	}
	return s.observe(ctx, &updated)
}

func (s *ProxyGroupAdminService) Delete(ctx context.Context, id int64) error {
	if _, err := s.group(ctx, id); err != nil {
		return err
	}
	return s.groups.Delete(ctx, id)
}

func (s *ProxyGroupAdminService) ReplaceMembers(ctx context.Context, id int64, ids []int64) (*ProxyGroupAdminView, error) {
	g, err := s.group(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, proxyID := range ids {
		if proxyID <= 0 {
			return nil, invalidProxyGroup("member IDs must be positive")
		}
		if _, err := s.proxyInWorkspace(ctx, proxyID, g.WorkspaceID); err != nil {
			return nil, err
		}
	}
	// Repository validates every reference again in the replacement transaction.
	if err := s.groups.ReplaceMembers(ctx, id, ids); err != nil {
		return nil, err
	}
	return s.observe(ctx, g)
}

func (s *ProxyGroupAdminService) Bindings(ctx context.Context, id int64) ([]ProxyGroupBindingObservation, error) {
	g, err := s.group(ctx, id)
	if err != nil {
		return nil, err
	}
	accounts, err := s.groups.ListAccountSummariesByGroupID(ctx, id)
	if err != nil {
		return nil, err
	}
	members, err := s.groups.ListMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	memberIDs := make(map[int64]bool, len(members))
	for _, m := range members {
		memberIDs[m.ProxyID] = true
	}
	out := make([]ProxyGroupBindingObservation, 0, len(accounts))
	owners := make(map[int64]ProxyGroupBindingObservation, len(accounts))
	for _, account := range accounts {
		ownerID := proxyGroupBindingOwner(account)
		if observed, ok := owners[ownerID]; ok {
			observed.AccountID, observed.AccountName = account.ID, account.Name
			out = append(out, observed)
			continue
		}
		item := ProxyGroupBindingObservation{AccountID: account.ID, AccountName: account.Name}
		proxyID, err := s.cache.GetBinding(ctx, id, ownerID)
		if err != nil {
			return nil, err
		}
		if proxyID > 0 {
			observedAt := time.Now()
			ttl, err := s.cache.GetBindingTTL(ctx, id, ownerID)
			if err != nil {
				return nil, err
			}
			current, err := s.cache.GetBinding(ctx, id, ownerID)
			if err != nil {
				return nil, err
			}
			if ttl > 0 && current == proxyID {
				p, err := s.proxyInWorkspace(ctx, proxyID, g.WorkspaceID)
				if err != nil && !errors.Is(err, ErrProxyNotFound) && !errors.Is(err, ErrWorkspaceNotFound) {
					return nil, err
				}
				if err == nil {
					now := time.Now()
					expires := observedAt.Add(ttl)
					item.ProxyID, item.ProxyName, item.ExpiresAt = &proxyID, p.Name, &expires
					cooldown, err := s.cache.GetCooldown(ctx, proxyID)
					if err != nil {
						return nil, err
					}
					item.Eligible = g.Status == StatusActive && memberIDs[proxyID] && proxyGroupAdminCandidate(p, g.WorkspaceID, cooldown, now)
				}
			}
		}
		owners[ownerID] = item
		out = append(out, item)
	}
	return out, nil
}

func (s *ProxyGroupAdminService) ResetBindings(ctx context.Context, id int64) (int, error) {
	if _, err := s.group(ctx, id); err != nil {
		return 0, err
	}
	accounts, err := s.groups.ListAccountSummariesByGroupID(ctx, id)
	if err != nil {
		return 0, err
	}
	cleared := 0
	owners := make(map[int64]bool, len(accounts))
	for _, account := range accounts {
		ownerID := proxyGroupBindingOwner(account)
		if owners[ownerID] {
			continue
		}
		owners[ownerID] = true
		proxyID, err := s.cache.GetBinding(ctx, id, ownerID)
		if err != nil {
			return cleared, err
		}
		if proxyID == 0 {
			continue
		}
		deleted, err := s.cache.DeleteBindingIfValue(ctx, id, ownerID, proxyID)
		if err != nil {
			return cleared, err
		}
		if deleted {
			cleared++
		}
	}
	return cleared, nil
}

func (s *ProxyGroupAdminService) ClearCooldown(ctx context.Context, proxyID int64) error {
	return s.cache.ClearCooldown(ctx, proxyID)
}

func proxyGroupBindingOwner(account ProxyAccountSummary) int64 {
	if account.ParentAccountID != nil && *account.ParentAccountID > 0 {
		return *account.ParentAccountID
	}
	return account.ID
}
