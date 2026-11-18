package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/schema/mixins"
)

// CredentialSecret contains account credentials, never deployment bootstrap keys.
type CredentialSecret struct{ ent.Schema }

func (CredentialSecret) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.WorkspaceMixin{}}
}

func (CredentialSecret) Fields() []ent.Field {
	return []ent.Field{
		field.String("reference").Unique().Immutable().NotEmpty(),
		field.Int64("account_id").Unique().Immutable(),
		field.Int64("version").Default(1).Positive(),
		field.Text("ciphertext").Sensitive(),
		field.Time("revoked_at").Optional().Nillable(),
	}
}
