package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/schema/mixins"
)

// Workspace is a resource ownership boundary, independent of global user roles.
type Workspace struct{ ent.Schema }

func (Workspace) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }

func (Workspace) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().MaxLen(100),
		field.Bool("is_default").Default(false),
	}
}
