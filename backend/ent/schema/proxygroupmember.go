package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

type ProxyGroupMember struct{ ent.Schema }

func (ProxyGroupMember) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "proxy_group_members"}}
}
func (ProxyGroupMember) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.WorkspaceMixin{}}
}
func (ProxyGroupMember) Fields() []ent.Field {
	return []ent.Field{field.Int64("group_id"), field.Int64("proxy_id")}
}
func (ProxyGroupMember) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("group", ProxyGroup.Type).Ref("members").Field("group_id").Unique().Required(),
		edge.To("proxy", Proxy.Type).Field("proxy_id").Unique().Required(),
	}
}
func (ProxyGroupMember) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("group_id", "proxy_id").Unique(), index.Fields("workspace_id"), index.Fields("proxy_id"),
	}
}
