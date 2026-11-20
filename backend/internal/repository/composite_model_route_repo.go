package repository

import (
	"context"
	"encoding/json"
	"fmt"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/compositemodelroute"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type compositeModelRouteRepository struct {
	client *dbent.Client
}

func NewCompositeModelRouteRepository(client *dbent.Client) service.CompositeModelRouteRepository {
	return &compositeModelRouteRepository{client: client}
}

func (r *compositeModelRouteRepository) ListByGroup(ctx context.Context, groupID int64, includeDisabled bool) ([]service.CompositeModelRoute, error) {
	q := clientFromContext(ctx, r.client).CompositeModelRoute.Query().
		Where(compositemodelroute.GroupIDEQ(groupID)).
		Order(
			dbent.Asc(compositemodelroute.FieldPriority),
			dbent.Asc(compositemodelroute.FieldID),
		)
	if !includeDisabled {
		q = q.Where(compositemodelroute.EnabledEQ(true))
	}
	rows, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.CompositeModelRoute, 0, len(rows))
	for _, row := range rows {
		out = append(out, *compositeModelRouteEntityToService(row))
	}
	return out, nil
}

func (r *compositeModelRouteRepository) Create(ctx context.Context, route *service.CompositeModelRoute) error {
	if route == nil {
		return service.ErrCompositeRouteNotFound
	}
	orderedTargets, err := marshalCompositeRouteTargets(route.Targets)
	if err != nil {
		return err
	}
	create := clientFromContext(ctx, r.client).CompositeModelRoute.Create().
		SetGroupID(route.GroupID).
		SetPublicModel(route.PublicModel).
		SetMatchType(route.MatchType).
		SetTargetPlatform(route.TargetPlatform).
		SetAccountType(route.AccountType).
		SetUpstreamModel(route.UpstreamModel).
		SetEndpoint(route.Endpoint).
		SetPriority(route.Priority).
		SetEnabled(route.Enabled).
		SetNotes(route.Notes)
	if len(orderedTargets) > 0 {
		create = create.SetOrderedTargets(orderedTargets)
	}
	created, err := create.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, nil, service.ErrCompositeRouteExists)
	}
	if route.TargetPlatform == service.PlatformBedrock || route.TargetPlatform == "vertex" {
		created, err = clientFromContext(ctx, r.client).CompositeModelRoute.Get(ctx, created.ID)
		if err != nil {
			return err
		}
	}
	*route = *compositeModelRouteEntityToService(created)
	return nil
}

func (r *compositeModelRouteRepository) Update(ctx context.Context, route *service.CompositeModelRoute) error {
	if route == nil {
		return service.ErrCompositeRouteNotFound
	}
	orderedTargets, err := marshalCompositeRouteTargets(route.Targets)
	if err != nil {
		return err
	}
	update := clientFromContext(ctx, r.client).CompositeModelRoute.UpdateOneID(route.ID).
		SetPublicModel(route.PublicModel).
		SetMatchType(route.MatchType).
		SetTargetPlatform(route.TargetPlatform).
		SetAccountType(route.AccountType).
		SetUpstreamModel(route.UpstreamModel).
		SetEndpoint(route.Endpoint).
		SetPriority(route.Priority).
		SetEnabled(route.Enabled).
		SetNotes(route.Notes)
	if len(orderedTargets) == 0 {
		update = update.ClearOrderedTargets()
	} else {
		update = update.SetOrderedTargets(orderedTargets)
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrCompositeRouteNotFound, service.ErrCompositeRouteExists)
	}
	*route = *compositeModelRouteEntityToService(updated)
	return nil
}

// marshalCompositeRouteTargets stores the ordered fallback list. An empty list
// clears the column: the route then falls back to the legacy single target.
func marshalCompositeRouteTargets(targets []service.CompositeRouteTarget) ([]byte, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(targets)
	if err != nil {
		return nil, fmt.Errorf("marshal composite route targets: %w", err)
	}
	return encoded, nil
}

func (r *compositeModelRouteRepository) Delete(ctx context.Context, id int64) error {
	err := clientFromContext(ctx, r.client).CompositeModelRoute.DeleteOneID(id).Exec(ctx)
	return translatePersistenceError(err, service.ErrCompositeRouteNotFound, nil)
}

func (r *compositeModelRouteRepository) DeleteByGroup(ctx context.Context, groupID int64) error {
	_, err := clientFromContext(ctx, r.client).CompositeModelRoute.Delete().
		Where(compositemodelroute.GroupIDEQ(groupID)).
		Exec(ctx)
	return err
}

func compositeModelRouteEntityToService(row *dbent.CompositeModelRoute) *service.CompositeModelRoute {
	if row == nil {
		return nil
	}
	route := &service.CompositeModelRoute{
		ID:             row.ID,
		GroupID:        row.GroupID,
		PublicModel:    row.PublicModel,
		MatchType:      row.MatchType,
		TargetPlatform: row.TargetPlatform,
		AccountType:    row.AccountType,
		UpstreamModel:  row.UpstreamModel,
		Endpoint:       row.Endpoint,
		Priority:       row.Priority,
		Enabled:        row.Enabled,
		Notes:          derefString(row.Notes),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
	if len(row.OrderedTargets) > 0 {
		var targets []service.CompositeRouteTarget
		if err := json.Unmarshal(row.OrderedTargets, &targets); err == nil {
			route.Targets = targets
		}
	}
	return route
}
