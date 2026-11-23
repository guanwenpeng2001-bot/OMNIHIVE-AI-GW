package mixins

import (
	"context"
	"fmt"

	"entgo.io/ent"
	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/intercept"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

// WorkspaceMixin applies the same ownership predicate to queries and mutations.
type WorkspaceMixin struct{ mixin.Schema }

func (WorkspaceMixin) Fields() []ent.Field {
	return []ent.Field{field.Int64("workspace_id").Immutable()}
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
			if !scoped && mutation.Op().Is(ent.OpCreate) {
				if value, explicit := mutation.Field("workspace_id"); explicit {
					id, ok := value.(int64)
					if !ok || id < 0 {
						return nil, fmt.Errorf("invalid workspace ID")
					}
					if id > 0 {
						return next.Mutate(ctx, mutation)
					}
				}
				m, ok := mutation.(interface{ Client() *dbent.Client })
				if !ok {
					return nil, fmt.Errorf("workspace mutation has no database client")
				}
				var err error
				workspaceID, err = domain.ResolveDefaultWorkspaceID(ctx, m.Client())
				if err != nil {
					return nil, err
				}
				scoped = true
			}
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
