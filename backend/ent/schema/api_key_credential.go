package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/schema/mixins"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

// APIKeyCredential stores one versioned credential for an API key. The secret is
// never persisted; only its one-way digest and a short display suffix are. A key
// keeps its identity id across rotations, so history and billing stay attached.
type APIKeyCredential struct {
	ent.Schema
}

func (APIKeyCredential) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "api_key_credentials"},
	}
}

func (APIKeyCredential) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.WorkspaceMixin{},
	}
}

func (APIKeyCredential) Fields() []ent.Field {
	return []ent.Field{
		field.String("credential_digest").
			MaxLen(64).
			NotEmpty().
			Unique().
			Immutable().
			Sensitive().
			Comment("Hex SHA-256 of the secret; also the auth cache identifier"),
		field.Int64("api_key_id"),
		field.String("display_suffix").
			MaxLen(16).
			Default("").
			Comment("Non-secret tail kept for display only"),
		field.String("status").
			MaxLen(20).
			Default(domain.CredentialStatusCurrent).
			Comment("current, grace or revoked"),
		field.Time("valid_until").
			Optional().
			Nillable().
			Comment("Grace deadline; nil means no time bound"),
		field.Time("revoked_at").
			Optional().
			Nillable(),
	}
}

func (APIKeyCredential) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("api_key", APIKey.Type).
			Ref("credentials").
			Field("api_key_id").
			Unique().
			Required(),
	}
}

func (APIKeyCredential) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("api_key_id"),
		index.Fields("status"),
		index.Fields("valid_until"),
	}
}
