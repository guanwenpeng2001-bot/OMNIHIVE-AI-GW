package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

// SQL owns foreign keys and the exactly-one-owner constraint.
type ModelAccessRule struct{ ent.Schema }

func (ModelAccessRule) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }
func (ModelAccessRule) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("workspace_id"), field.Int64("account_id").Optional().Nillable().Unique(),
		field.Int64("api_key_id").Optional().Nillable().Unique(), field.Int64("access_group_id").Optional().Nillable(),
		field.JSON("models", []string{}).Optional(), field.JSON("exclude", []string{}).Default([]string{}), field.Int64("revision").Default(1),
	}
}
func (ModelAccessRule) Indexes() []ent.Index { return []ent.Index{index.Fields("access_group_id")} }
