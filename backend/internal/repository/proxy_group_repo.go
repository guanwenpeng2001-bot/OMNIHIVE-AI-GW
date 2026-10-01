package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/ent/proxygroup"
	"github.com/Wei-Shaw/sub2api/ent/proxygroupmember"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type proxyGroupRepository struct{ client *dbent.Client }

func NewProxyGroupRepository(client *dbent.Client) service.ProxyGroupRepository {
	return &proxyGroupRepository{client: client}
}

func proxyGroupEntityToService(m *dbent.ProxyGroup) *service.ProxyGroup {
	if m == nil {
		return nil
	}
	return &service.ProxyGroup{ID: m.ID, WorkspaceID: m.WorkspaceID, Name: m.Name,
		Description: m.Description, Region: m.Region, TargetServices: append([]string{}, m.TargetServices...),
		Status: m.Status, FallbackMode: m.FallbackMode, FallbackProxyID: m.FallbackProxyID,
		BindingTTLSeconds: m.BindingTTLSeconds, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, DeletedAt: m.DeletedAt}
}

func validateProxyGroup(g *service.ProxyGroup) error {
	if g == nil {
		return fmt.Errorf("proxy group is required")
	}
	if strings.TrimSpace(g.Name) == "" {
		return fmt.Errorf("proxy group name is required")
	}
	if g.Status == "" {
		g.Status = service.StatusActive
	}
	if g.Status != service.StatusActive && g.Status != "inactive" {
		return fmt.Errorf("invalid proxy group status")
	}
	if g.FallbackMode == "" {
		g.FallbackMode = service.FallbackModeNone
	}
	switch g.FallbackMode {
	case service.FallbackModeNone, service.FallbackModeDirect:
		if g.FallbackProxyID != nil {
			return fmt.Errorf("fallback proxy requires proxy fallback mode")
		}
	case service.FallbackModeProxy:
		if g.FallbackProxyID == nil || *g.FallbackProxyID <= 0 {
			return fmt.Errorf("fallback proxy is required")
		}
	default:
		return fmt.Errorf("invalid proxy group fallback mode")
	}
	if g.BindingTTLSeconds == 0 {
		g.BindingTTLSeconds = service.DefaultProxyGroupBindingTTLSeconds
	}
	if g.BindingTTLSeconds < 0 {
		return fmt.Errorf("binding TTL must be positive")
	}
	return nil
}

// Reuse caller transactions; otherwise all multi-statement mutations commit together.
func (r *proxyGroupRepository) transaction(ctx context.Context, fn func(context.Context, *dbent.Client) error) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return fn(ctx, tx.Client())
	}
	tx, err := r.client.Tx(ctx)
	if err == dbent.ErrTxStarted {
		return fn(ctx, r.client)
	}
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(dbent.NewTxContext(ctx, tx), tx.Client()); err != nil {
		return err
	}
	return tx.Commit()
}

func validateGroupProxy(ctx context.Context, client *dbent.Client, id *int64, workspaceID int64) error {
	if id == nil {
		return nil
	}
	exists, err := client.Proxy.Query().Where(proxy.IDEQ(*id), proxy.WorkspaceIDEQ(workspaceID), proxy.DeletedAtIsNil()).Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return service.ErrProxyNotFound
	}
	return nil
}

func (r *proxyGroupRepository) Create(ctx context.Context, g *service.ProxyGroup) error {
	if err := validateProxyGroup(g); err != nil {
		return err
	}
	var created *dbent.ProxyGroup
	err := r.transaction(ctx, func(ctx context.Context, client *dbent.Client) error {
		workspaceID := domain.ResourceWorkspaceID(ctx)
		if err := validateGroupProxy(ctx, client, g.FallbackProxyID, workspaceID); err != nil {
			return err
		}
		var err error
		created, err = client.ProxyGroup.Create().SetName(g.Name).SetDescription(g.Description).SetRegion(g.Region).
			SetTargetServices(append([]string{}, g.TargetServices...)).SetStatus(g.Status).SetFallbackMode(g.FallbackMode).
			SetNillableFallbackProxyID(g.FallbackProxyID).SetBindingTTLSeconds(g.BindingTTLSeconds).SetWorkspaceID(workspaceID).Save(ctx)
		return translatePersistenceError(err, service.ErrProxyGroupNotFound, service.ErrProxyBindingConflict)
	})
	if err == nil {
		*g = *proxyGroupEntityToService(created)
	}
	return err
}

func (r *proxyGroupRepository) GetByID(ctx context.Context, id int64) (*service.ProxyGroup, error) {
	m, err := clientFromContext(ctx, r.client).ProxyGroup.Query().Where(proxygroup.IDEQ(id), proxygroup.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrProxyGroupNotFound, nil)
	}
	g := proxyGroupEntityToService(m)
	g.Members, err = r.ListMembers(ctx, id)
	return g, err
}

func (r *proxyGroupRepository) List(ctx context.Context, params pagination.PaginationParams, status, targetService, search string) ([]service.ProxyGroup, *pagination.PaginationResult, error) {
	q := clientFromContext(ctx, r.client).ProxyGroup.Query().Where(proxygroup.DeletedAtIsNil())
	if status != "" {
		q.Where(proxygroup.StatusEQ(status))
	}
	if targetService != "" {
		q.Where(func(s *entsql.Selector) {
			s.Where(sqljson.ValueContains(proxygroup.FieldTargetServices, targetService))
		})
	}
	if search = strings.TrimSpace(search); search != "" {
		q.Where(proxygroup.NameContainsFold(search))
	}
	count, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	orderField := proxygroup.FieldID
	switch params.SortBy {
	case "name":
		orderField = proxygroup.FieldName
	case "created_at":
		orderField = proxygroup.FieldCreatedAt
	case "updated_at":
		orderField = proxygroup.FieldUpdatedAt
	}
	if params.NormalizedSortOrder(pagination.SortOrderDesc) == pagination.SortOrderAsc {
		q.Order(dbent.Asc(orderField), dbent.Asc(proxygroup.FieldID))
	} else {
		q.Order(dbent.Desc(orderField), dbent.Desc(proxygroup.FieldID))
	}
	rows, err := q.Offset(params.Offset()).Limit(params.Limit()).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	result := make([]service.ProxyGroup, 0, len(rows))
	for _, row := range rows {
		result = append(result, *proxyGroupEntityToService(row))
	}
	return result, paginationResultFromTotal(int64(count), params), nil
}

func lockProxyGroup(ctx context.Context, client *dbent.Client, id int64) (*dbent.ProxyGroup, error) {
	g, err := client.ProxyGroup.Query().Where(proxygroup.IDEQ(id), proxygroup.DeletedAtIsNil()).ForUpdate().Only(ctx)
	return g, translatePersistenceError(err, service.ErrProxyGroupNotFound, nil)
}

func (r *proxyGroupRepository) Update(ctx context.Context, g *service.ProxyGroup) error {
	if err := validateProxyGroup(g); err != nil {
		return err
	}
	var updated *dbent.ProxyGroup
	err := r.transaction(ctx, func(ctx context.Context, client *dbent.Client) error {
		current, err := lockProxyGroup(ctx, client, g.ID)
		if err != nil {
			return err
		}
		if err := validateGroupProxy(ctx, client, g.FallbackProxyID, current.WorkspaceID); err != nil {
			return err
		}
		b := client.ProxyGroup.UpdateOneID(g.ID).SetName(g.Name).SetDescription(g.Description).SetRegion(g.Region).
			SetTargetServices(append([]string{}, g.TargetServices...)).SetStatus(g.Status).SetFallbackMode(g.FallbackMode).SetBindingTTLSeconds(g.BindingTTLSeconds)
		if g.FallbackProxyID == nil {
			b.ClearFallbackProxyID()
		} else {
			b.SetFallbackProxyID(*g.FallbackProxyID)
		}
		updated, err = b.Save(ctx)
		return translatePersistenceError(err, service.ErrProxyGroupNotFound, service.ErrProxyBindingConflict)
	})
	if err == nil {
		members := g.Members
		*g = *proxyGroupEntityToService(updated)
		g.Members = members
	}
	return err
}

func (r *proxyGroupRepository) Delete(ctx context.Context, id int64) error {
	return r.transaction(ctx, func(ctx context.Context, client *dbent.Client) error {
		if _, err := lockProxyGroup(ctx, client, id); err != nil {
			return err
		}
		// Check every workspace: a corrupt legacy reference must not be silently orphaned.
		rows, err := client.QueryContext(ctx, "SELECT COUNT(*) FROM accounts WHERE proxy_group_id=$1 AND deleted_at IS NULL", id)
		if err != nil {
			return err
		}
		var count int64
		if rows.Next() {
			err = rows.Scan(&count)
		}
		rowErr := rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		if rowErr != nil {
			return rowErr
		}
		if count > 0 {
			return service.ErrProxyGroupInUse
		}
		return client.ProxyGroup.UpdateOneID(id).SetDeletedAt(time.Now()).Exec(ctx)
	})
}

func (r *proxyGroupRepository) ReplaceMembers(ctx context.Context, id int64, ids []int64) error {
	return r.transaction(ctx, func(ctx context.Context, client *dbent.Client) error {
		group, err := lockProxyGroup(ctx, client, id)
		if err != nil {
			return err
		}
		unique := make([]int64, 0, len(ids))
		seen := map[int64]bool{}
		for _, id := range ids {
			if id <= 0 {
				return service.ErrProxyNotFound
			}
			if !seen[id] {
				seen[id] = true
				unique = append(unique, id)
			}
		}
		if len(unique) > 0 {
			count, err := client.Proxy.Query().Where(proxy.IDIn(unique...), proxy.WorkspaceIDEQ(group.WorkspaceID), proxy.DeletedAtIsNil()).Count(ctx)
			if err != nil {
				return err
			}
			if count != len(unique) {
				return service.ErrProxyNotFound
			}
		}
		if _, err := client.ProxyGroupMember.Delete().Where(proxygroupmember.GroupIDEQ(id)).Exec(ctx); err != nil {
			return err
		}
		for _, proxyID := range unique {
			if err := client.ProxyGroupMember.Create().SetGroupID(id).SetProxyID(proxyID).SetWorkspaceID(group.WorkspaceID).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *proxyGroupRepository) ListMembers(ctx context.Context, id int64) ([]service.ProxyGroupMember, error) {
	client := clientFromContext(ctx, r.client)
	group, err := client.ProxyGroup.Query().Where(proxygroup.IDEQ(id), proxygroup.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrProxyGroupNotFound, nil)
	}
	rows, err := client.ProxyGroupMember.Query().Where(proxygroupmember.GroupIDEQ(id), proxygroupmember.WorkspaceIDEQ(group.WorkspaceID)).
		WithProxy(func(q *dbent.ProxyQuery) { q.Where(proxy.DeletedAtIsNil(), proxy.WorkspaceIDEQ(group.WorkspaceID)) }).Order(dbent.Asc(proxygroupmember.FieldProxyID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.ProxyGroupMember, 0, len(rows))
	for _, m := range rows {
		if m.Edges.Proxy != nil {
			out = append(out, service.ProxyGroupMember{ID: m.ID, WorkspaceID: m.WorkspaceID, GroupID: m.GroupID, ProxyID: m.ProxyID, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, Proxy: proxyEntityToService(m.Edges.Proxy)})
		}
	}
	return out, nil
}

func (r *proxyGroupRepository) CountAccountsByGroupID(ctx context.Context, id int64) (int64, error) {
	client := clientFromContext(ctx, r.client)
	group, err := client.ProxyGroup.Query().Where(proxygroup.IDEQ(id), proxygroup.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		return 0, translatePersistenceError(err, service.ErrProxyGroupNotFound, nil)
	}
	n, err := client.Account.Query().Where(account.ProxyGroupIDEQ(id), account.WorkspaceIDEQ(group.WorkspaceID), account.DeletedAtIsNil()).Count(ctx)
	return int64(n), err
}

func (r *proxyGroupRepository) ListAccountSummariesByGroupID(ctx context.Context, id int64) ([]service.ProxyAccountSummary, error) {
	client := clientFromContext(ctx, r.client)
	group, err := client.ProxyGroup.Query().Where(proxygroup.IDEQ(id), proxygroup.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrProxyGroupNotFound, nil)
	}
	rows, err := client.Account.Query().Where(account.ProxyGroupIDEQ(id), account.WorkspaceIDEQ(group.WorkspaceID), account.DeletedAtIsNil()).
		Select(account.FieldID, account.FieldName, account.FieldPlatform, account.FieldType, account.FieldNotes, account.FieldParentAccountID).Order(dbent.Asc(account.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.ProxyAccountSummary, 0, len(rows))
	for _, m := range rows {
		out = append(out, service.ProxyAccountSummary{ID: m.ID, Name: m.Name, Platform: m.Platform, Type: m.Type, Notes: m.Notes, ParentAccountID: m.ParentAccountID})
	}
	return out, nil
}
