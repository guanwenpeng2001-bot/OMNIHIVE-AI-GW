package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/schema/mixins"
)

type ProxyGroup struct{ ent.Schema }

func (ProxyGroup) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "proxy_groups"}}
}
func (ProxyGroup) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.WorkspaceMixin{}, mixins.SoftDeleteMixin{}}
}
func (ProxyGroup) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100).NotEmpty(),
		field.String("description").Default(""),
		field.String("region").MaxLen(100).Default(""),
		field.JSON("target_services", []string{}).Default(func() []string { return []string{} }),
		field.String("status").MaxLen(20).Default("active"),
		field.String("fallback_mode").MaxLen(20).Default("none"),
		field.Int64("fallback_proxy_id").Optional().Nillable(),
		field.Int("binding_ttl_seconds").Default(86400).Positive(),
	}
}
func (ProxyGroup) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("fallback_proxy", Proxy.Type).Field("fallback_proxy_id").Unique(),
		edge.To("members", ProxyGroupMember.Type),
		edge.From("accounts", Account.Type).Ref("proxy_group"),
	}
}
func (ProxyGroup) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("workspace_id", "status"), index.Fields("deleted_at"), index.Fields("fallback_proxy_id"),
	}
}
