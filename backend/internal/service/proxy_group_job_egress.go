package service

import "context"

// ProxyGroupJobEgress persists only route identifiers, never a proxy URL or
// credentials. Long-running jobs must not choose a different node on a poll.
type ProxyGroupJobEgress struct {
	ProxyGroupID int64        `json:"proxy_group_id"`
	ProxyID      int64        `json:"proxy_id"`
	Source       EgressSource `json:"source"`
}

func (a *Account) PinJobEgress(ctx context.Context) (*ProxyGroupJobEgress, error) {
	if a == nil {
		return nil, ErrAccountNotFound
	}
	if a.ProxyGroupID == nil {
		return nil, nil
	}
	if _, err := a.EgressProxyURL(ctx); err != nil {
		return nil, err
	}
	a.egress.mu.Lock()
	defer a.egress.mu.Unlock()
	// Persisting this route precedes submission. Refuse to move it later even
	// when a transport error would otherwise allow a safe node retry.
	a.egress.committed = true
	target := a.egress.target
	return &ProxyGroupJobEgress{ProxyGroupID: *a.ProxyGroupID, ProxyID: target.ProxyID, Source: target.Source}, nil
}

func (a *Account) RestoreJobEgress(ctx context.Context, snapshot *ProxyGroupJobEgress) error {
	if a == nil {
		return ErrAccountNotFound
	}
	if snapshot == nil {
		if a.ProxyGroupID != nil {
			return ErrProxyGroupUnavailable
		}
		return nil
	}
	if a.ProxyGroupID == nil || *a.ProxyGroupID != snapshot.ProxyGroupID {
		return ErrProxyBindingConflict
	}
	if a.egress == nil || a.egress.runtime == nil {
		return ErrProxyGroupUnavailable
	}
	r := a.egress.runtime
	g, nodes, err := r.candidates(ctx, snapshot.ProxyGroupID, a.WorkspaceID)
	if err != nil {
		return err
	}
	target := EgressTarget{ProxyID: snapshot.ProxyID, Source: snapshot.Source, Resolved: true}
	switch snapshot.Source {
	case EgressSourceGroupBinding:
		p := nodes[snapshot.ProxyID]
		if p == nil {
			return ErrProxyGroupUnavailable
		}
		target.URL = p.URL()
	case EgressSourceGroupFallbackProxy, EgressSourceGroupFallbackDirect:
		fallback, err := r.fallback(ctx, g)
		if err != nil {
			return err
		}
		if fallback.ProxyID != snapshot.ProxyID || fallback.Source != snapshot.Source {
			return ErrProxyBindingConflict
		}
		target = fallback
	default:
		return ErrProxyGroupUnavailable
	}
	a.egress = &accountEgress{runtime: r, target: target, committed: true}
	return nil
}
