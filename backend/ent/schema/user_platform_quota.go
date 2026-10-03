package schema

import (
	"fmt"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

// UserPlatformQuota holds the schema definition for per-user per-platform quota.
type UserPlatformQuota struct {
	ent.Schema
}

func (UserPlatformQuota) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_platform_quotas"},
	}
}

func (UserPlatformQuota) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (UserPlatformQuota) Fields() []ent.Field {
	return []ent.Field{
		field.String("account_type").MaxLen(32).Default(""),
		field.Int64("user_id"),
		field.String("platform").
			MaxLen(32).
			NotEmpty().
			Validate(func(s string) error {
				// Storage identities must match migration 284, including quota-only vertex.
				// Keep the finite list stable even when the embedded registry is empty.
				switch s {
				case "anthropic", "openai", "gemini", "antigravity", "grok",
					"kimi", "zhipu", "deepseek", "minimax", "opencode_go", "cursor", "qianwen", "commandcode", "qianfan", "hunyuan", "mistral", "openrouter", "bedrock", "volcengine", "vertex",
					"a6api", "aicodemirror", "aicodewith", "aicoding", "aigocode",
					"aihubmix", "amux", "apikey_fun", "apinebula", "astron_coding",
					"atlascloud", "azure_openai", "bailing", "byteplus", "ccsub",
					"cherryin", "claudeapi", "claudecn", "code0", "code_9527",
					"compshare", "compshare_coding", "crazyrouter", "crazyrouter_intl", "cubence",
					"dmxapi", "dmxapi_com", "dmxapi_ssvip", "eflowcode", "fennoai",
					"fluxa", "jiekou", "kat_coder", "longcat", "micu",
					"mimo", "mimo_token_cn", "modelscope", "novita", "nvidia",
					"packycode", "pateway", "pipellm", "ppio", "qiniu",
					"qwencloud", "qwencloud_maas_token", "qwencloud_token", "relaxycode", "rightcode",
					"runapi", "shengsuanyun", "siliconflow_cn", "siliconflow_global", "soleapi",
					"soshow", "sssaicode", "stepfun_api", "stepfun_api_intl", "stepfun_plan",
					"stepfun_plan_intl", "subrouter", "sudocode_chat", "sudocode_us", "teamorouter",
					"teamorouter_com", "therouter", "together", "xycai", "zetaapi", "github_copilot", "nous_research":
					return nil
				default:
					return fmt.Errorf("platform %q is not allowed", s)
				}
			}),

		// 日 / 周 / 月 USD 上限：
		//   nil / not set → 无限额（完全放行）
		//   0            → 完全禁用（任何请求都会被拒绝，因为 usage >= 0 恒成立）
		//   > 0          → USD 限额上限
		field.Float("daily_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("weekly_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("monthly_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),

		// 当前窗口已用量（USD，preflight 时与 limit 比较）
		field.Float("daily_usage_usd").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("weekly_usage_usd").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("monthly_usage_usd").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),

		// 窗口起点（NULL = 首次还未初始化，由 InitWindowStarts 用 COALESCE 兜底）
		field.Time("daily_window_start").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("weekly_window_start").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("monthly_window_start").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (UserPlatformQuota) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("platform_quotas").
			Field("user_id").
			Unique().
			Required(),
	}
}

func (UserPlatformQuota) Indexes() []ent.Index {
	return []ent.Index{
		// 软删除友好：只对未删记录唯一
		index.Fields("user_id", "platform").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("user_id"),
	}
}
