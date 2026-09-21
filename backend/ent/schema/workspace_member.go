package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

// WorkspaceMember delegates access without changing the user's global role.
type WorkspaceMember struct{ ent.Schema }

func (WorkspaceMember) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }

func (WorkspaceMember) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("workspace_id"),
		field.Int64("user_id"),
		field.Enum("role").Values("member", "admin"),
	}
}

func (WorkspaceMember) Indexes() []ent.Index {
	return []ent.Index{index.Fields("workspace_id", "user_id").Unique()}
}
