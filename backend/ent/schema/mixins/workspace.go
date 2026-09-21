package mixins

import (
	"context"
	"fmt"

	"entgo.io/ent"
	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
	"github.com/Wei-Shaw/sub2api/ent/intercept"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// WorkspaceMixin applies the same ownership predicate to queries and mutations.
type WorkspaceMixin struct{ mixin.Schema }

func (WorkspaceMixin) Fields() []ent.Field {
	return []ent.Field{field.Int64("workspace_id").Default(domain.DefaultWorkspaceID).Immutable()}
}

func (WorkspaceMixin) Interceptors() []ent.Interceptor {
	return []ent.Interceptor{intercept.TraverseFunc(func(ctx context.Context, query intercept.Query) error {
		if workspaceID, scoped := domain.WorkspaceScope(ctx); scoped {
			query.WhereP(sql.FieldEQ("workspace_id", workspaceID))
		}
		return nil
	})}
}

func (WorkspaceMixin) Hooks() []ent.Hook {
	return []ent.Hook{func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			workspaceID, scoped := domain.WorkspaceScope(ctx)
			if !scoped {
				return next.Mutate(ctx, mutation)
			}
			if workspaceID <= 0 {
				return nil, fmt.Errorf("invalid workspace scope")
			}
			if mutation.Op().Is(ent.OpCreate) {
				if err := mutation.SetField("workspace_id", workspaceID); err != nil {
					return nil, err
				}
			} else {
				predicate, ok := mutation.(interface{ WhereP(...func(*sql.Selector)) })
				if !ok {
					return nil, fmt.Errorf("workspace mutation does not support ownership predicate")
				}
				predicate.WhereP(sql.FieldEQ("workspace_id", workspaceID))
			}
			return next.Mutate(ctx, mutation)
		})
	}}
}
