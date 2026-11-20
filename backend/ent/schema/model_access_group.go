package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/schema/mixins"
)

type ModelAccessGroup struct{ ent.Schema }

func (ModelAccessGroup) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }
func (ModelAccessGroup) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("workspace_id"), field.String("name").NotEmpty().MaxLen(100),
		field.JSON("models", []string{}), field.Int64("revision").Default(1),
	}
}
func (ModelAccessGroup) Indexes() []ent.Index {
	return []ent.Index{index.Fields("workspace_id", "name").Unique(), index.Fields("id", "workspace_id").Unique()}
}
