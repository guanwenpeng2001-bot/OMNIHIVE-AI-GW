package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

type OAuthEgressSelection = domain.OAuthEgressSelection

func (a *Account) OAuthEgressSelection(ctx context.Context) (*OAuthEgressSelection, error) {
	if a.ProxyGroupID == nil {
		return nil, nil
	}
	if _, err := a.EgressProxyURL(ctx); err != nil {
		return nil, err
	}
	target, ok := a.ResolvedEgress()
	if !ok {
		return nil, ErrProxyGroupUnavailable
	}
	return &OAuthEgressSelection{GroupID: *a.ProxyGroupID, WorkspaceID: a.WorkspaceID, ProxyID: target.ProxyID, URL: target.URL, Source: string(target.Source)}, nil
}

func ValidateDraftForExistingAccount(draft, existing *Account) error {
	if draft.ProxyGroupID == nil && existing.ProxyGroupID == nil {
		return nil
	}
	if draft.ProxyGroupID == nil || existing.ProxyGroupID == nil || *draft.ProxyGroupID != *existing.ProxyGroupID {
		return ErrProxyBindingConflict
	}
	return ValidateDraftProxyChoice(draft, existing.ProxyID, existing.ProxyGroupID)
}

func resolveOAuthProxy(ctx context.Context, runtime *ProxyGroupRuntime, repo ProxyRepository, node *int64, groups []*int64) (string, *OAuthEgressSelection, error) {
	seed, err := randomOpaqueToken(24)
	if err != nil {
		return "", nil, err
	}
	selection, err := resolveOAuthEgress(ctx, runtime, node, groups, seed)
	if err != nil {
		return "", nil, err
	}
	if selection != nil {
		return selection.URL, selection, nil
	}
	if normalizedProxyID(node) == nil {
		return "", nil, nil
	}
	url, err := resolveGoogleAccountProxyURL(ctx, &Account{ProxyID: node}, repo)
	return url, nil, err
}

func (s *AuthPendingIdentityService) ResolveAuthorizationEgress(ctx context.Context, node, group *int64) (*OAuthEgressSelection, error) {
	seed, err := randomOpaqueToken(24)
	if err != nil {
		return nil, err
	}
	var runtime *ProxyGroupRuntime
	if s != nil {
		runtime = s.egressRuntime
	}
	return resolveOAuthEgress(ctx, runtime, node, []*int64{group}, seed)
}

func CopyCredentialDraftEgress(draft, account *Account) error {
	if err := ValidateDraftProxyChoice(draft, account.ProxyID, account.ProxyGroupID); err != nil {
		return err
	}
	if draft.oauthEgress != nil {
		account.ProxyGroupID = draft.ProxyGroupID
		account.WorkspaceID = draft.WorkspaceID
		account.oauthEgress = draft.oauthEgress
		account.egress = draft.egress
	}
	return nil
}

func (s *AuthPendingIdentityService) PreparePreviewEgress(ctx context.Context, account *Account) error {
	if account.ProxyGroupID == nil || account.egress != nil {
		return nil
	}
	selected, err := s.ResolveAuthorizationEgress(ctx, account.ProxyID, account.ProxyGroupID)
	if err != nil {
		return err
	}
	if selected == nil {
		return ErrProxyGroupUnavailable
	}
	account.egress = &accountEgress{target: EgressTarget{URL: selected.URL, ProxyID: selected.ProxyID, Source: EgressSource(selected.Source), Resolved: true}}
	return nil
}

func resolveOAuthEgress(ctx context.Context, runtime *ProxyGroupRuntime, proxyID *int64, groupIDs []*int64, seed string) (*OAuthEgressSelection, error) {
	var groupID *int64
	if len(groupIDs) > 0 {
		groupID = groupIDs[0]
	}
	if err := validateAccountProxyChoice(proxyID, groupID); err != nil {
		return nil, err
	}
	if normalizedProxyID(groupID) == nil {
		return nil, nil
	}
	if runtime == nil {
		return nil, ErrProxyGroupUnavailable
	}
	target, err := runtime.ResolveForNewSession(ctx, *groupID, seed)
	if err != nil {
		return nil, err
	}
	return &OAuthEgressSelection{GroupID: *groupID, WorkspaceID: domain.ResourceWorkspaceID(ctx), ProxyID: target.ProxyID, URL: target.URL, Source: string(target.Source)}, nil
}

func validateOAuthSessionEgress(selection *OAuthEgressSelection, proxyID, groupID *int64) error {
	if err := validateAccountProxyChoice(proxyID, groupID); err != nil {
		return err
	}
	if selection == nil {
		if normalizedProxyID(groupID) != nil {
			return ErrProxyBindingConflict
		}
		return nil
	}
	if normalizedProxyID(proxyID) != nil || (groupID != nil && *groupID != selection.GroupID) {
		return ErrProxyBindingConflict
	}
	return nil
}

func (r *ProxyGroupRuntime) confirmOAuthDraftBinding(ctx context.Context, selected *OAuthEgressSelection, accountID int64) error {
	if selected == nil || accountID <= 0 {
		return ErrProxyBindingConflict
	}
	g, nodes, err := r.candidates(ctx, selected.GroupID, selected.WorkspaceID)
	if err != nil {
		return err
	}
	if selected.Source == string(EgressSourceGroupBinding) {
		p := nodes[selected.ProxyID]
		if p == nil || p.URL() != selected.URL {
			return ErrProxyGroupUnavailable
		}
		return r.confirmSessionBinding(ctx, g, accountID, selected.ProxyID, selected.URL)
	}
	if selected.Source == string(EgressSourceGroupFallbackDirect) && len(nodes) > 0 {
		return ErrProxyBindingConflict
	}
	target, err := r.fallback(ctx, g)
	if err != nil {
		return err
	}
	if target.ProxyID != selected.ProxyID || target.URL != selected.URL || string(target.Source) != selected.Source {
		return ErrProxyBindingConflict
	}
	if target.ProxyID == 0 {
		return nil
	}
	winner, err := r.cache.TrySetBindingNX(ctx, g.ID, accountID, target.ProxyID, bindingTTL(g))
	if err != nil {
		return err
	}
	if winner != target.ProxyID {
		return ErrProxyBindingConflict
	}
	current, _, err := r.candidates(ctx, selected.GroupID, selected.WorkspaceID)
	if err == nil {
		fresh, fallbackErr := r.fallback(ctx, current)
		err = fallbackErr
		if err == nil && (fresh.ProxyID != selected.ProxyID || fresh.URL != selected.URL || string(fresh.Source) != selected.Source) {
			err = ErrProxyBindingConflict
		}
	}
	if err != nil {
		_, _ = r.cache.DeleteBindingIfValue(ctx, selected.GroupID, accountID, selected.ProxyID)
		return err
	}
	return nil
}
