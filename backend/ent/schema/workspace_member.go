package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/schema/mixins"
)

// WorkspaceMember delegates access without changing the user's global role.
type WorkspaceMember struct{ ent.Schema }

func (WorkspaceMember) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }

func (WorkspaceMember) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("workspace_id"),
		field.Int64("user_id"),
		field.Enum("role").Values("member", "admin"),
		// SQL migration 337 is authoritative. Ent declares this persisted source
		// marker so future generation never proposes dropping the column.
		field.Bool("inherits_global_role").Default(false).
			Comment("Whether the membership role follows the user's global role; false means independently assigned."),
	}
}

func (WorkspaceMember) Indexes() []ent.Index {
	return []ent.Index{index.Fields("workspace_id", "user_id").Unique()}
}
