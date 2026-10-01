package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/ent/proxygroup"
	"github.com/Wei-Shaw/sub2api/ent/proxygroupmember"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func validateAccountProxyGroup(ctx context.Context, client *dbent.Client, id *int64, workspaceID int64) error {
	if id == nil {
		return nil
	}
	group, err := lockProxyGroup(ctx, client, *id)
	if err != nil {
		return err
	}
	if group.WorkspaceID != workspaceID {
		return service.ErrProxyGroupNotFound
	}
	return nil
}

func (r *accountRepository) loadAccountProxyGroups(ctx context.Context, accounts []*dbent.Account) (map[int64]*service.ProxyGroup, error) {
	ids := make([]int64, 0)
	for _, a := range accounts {
		if a.ProxyGroupID != nil {
			ids = append(ids, *a.ProxyGroupID)
		}
	}
	out := make(map[int64]*service.ProxyGroup)
	if len(ids) == 0 {
		return out, nil
	}
	ids = uniquePositiveInt64s(ids)
	for start := 0; start < len(ids); start += postgresParameterBatchSize {
		end := min(start+postgresParameterBatchSize, len(ids))
		rows, err := clientFromContext(ctx, r.client).ProxyGroup.Query().Where(proxygroup.IDIn(ids[start:end]...), proxygroup.DeletedAtIsNil()).
			WithMembers(func(q *dbent.ProxyGroupMemberQuery) {
				q.Order(dbent.Asc(proxygroupmember.FieldProxyID)).WithProxy(func(p *dbent.ProxyQuery) { p.Where(proxy.DeletedAtIsNil()) })
			}).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, g := range rows {
			value := proxyGroupEntityToService(g)
			value.Members = make([]service.ProxyGroupMember, 0, len(g.Edges.Members))
			for _, m := range g.Edges.Members {
				if m.WorkspaceID == g.WorkspaceID && m.Edges.Proxy != nil && m.Edges.Proxy.WorkspaceID == g.WorkspaceID {
					value.Members = append(value.Members, service.ProxyGroupMember{ID: m.ID, WorkspaceID: m.WorkspaceID, GroupID: m.GroupID, ProxyID: m.ProxyID, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, Proxy: proxyEntityToService(m.Edges.Proxy)})
				}
			}
			out[g.ID] = value
		}
	}
	return out, nil
}
