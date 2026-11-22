package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/schema/mixins"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// compositeRoutePlatforms mirrors the SQL CHECK last extended by migrations
// 335/336. SQL migrations remain authoritative; keeping this validator here
// catches an invalid route before it reaches that final database guard.
var compositeRoutePlatforms = func() map[string]struct{} {
	values := strings.Fields(`
		anthropic openai gemini antigravity grok kimi zhipu deepseek minimax opencode_go
		cursor qianwen commandcode qianfan hunyuan mistral openrouter bedrock volcengine a6api
		aicodemirror aicodewith aicoding aigocode aihubmix amux apikey_fun apinebula astron_coding
		atlascloud azure_openai bailing byteplus ccsub cherryin claudeapi claudecn code0 code_9527
		compshare compshare_coding crazyrouter crazyrouter_intl cubence dmxapi dmxapi_com dmxapi_ssvip
		eflowcode fennoai fluxa jiekou kat_coder longcat micu mimo mimo_token_cn modelscope novita
		nvidia packycode pateway pipellm ppio qiniu qwencloud qwencloud_maas_token qwencloud_token
		relaxycode rightcode runapi shengsuanyun siliconflow_cn siliconflow_global soleapi soshow
		sssaicode stepfun_api stepfun_api_intl stepfun_plan stepfun_plan_intl subrouter sudocode_chat
		sudocode_us teamorouter teamorouter_com therouter together xycai zetaapi github_copilot
		nous_research astron_token mimo_token_sg mimo_token_eu kaggle workbuddy kilo`)
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		allowed[value] = struct{}{}
	}
	return allowed
}()

func validateCompositeRoutePlatform(platform string) error {
	if _, ok := compositeRoutePlatforms[platform]; !ok {
		return fmt.Errorf("target platform %q is not allowed", platform)
	}
	return nil
}

// CompositeModelRoute holds model routing aliases for composite groups.
type CompositeModelRoute struct {
	ent.Schema
}

func (CompositeModelRoute) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "composite_model_routes"},
	}
}

func (CompositeModelRoute) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (CompositeModelRoute) Fields() []ent.Field {
	return []ent.Field{
		field.String("account_type").MaxLen(32).Default(""),
		field.Int64("group_id"),
		field.String("public_model").
			MaxLen(200).
			NotEmpty().
			Comment("Client-facing model identifier or prefix."),
		field.String("match_type").
			MaxLen(20).
			Default("exact").
			Comment("exact or prefix."),
		field.String("target_platform").
			MaxLen(50).
			Default(domain.PlatformOpenAI).
			Validate(validateCompositeRoutePlatform).
			Comment("Concrete provider platform."),
		field.String("upstream_model").
			MaxLen(200).
			Default("").
			Comment("Provider model identifier; empty means public_model."),
		field.String("endpoint").
			MaxLen(50).
			Default("any").
			Comment("Endpoint scope such as any, messages, responses, chat_completions."),
		field.Int("priority").
			Default(100).
			Comment("Lower values win within the same match strength."),
		field.Bool("enabled").
			Default(true),
		field.String("notes").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.JSON("ordered_targets", json.RawMessage{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("有序目标列表：跨平台 fallback 顺序、权重、区域/供应商约束、延迟偏好与亲和性 TTL"),
	}
}

func (CompositeModelRoute) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("group", Group.Type).
			Unique().
			Required().
			Field("group_id"),
	}
}

func (CompositeModelRoute) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("group_id"),
		index.Fields("group_id", "enabled"),
		index.Fields("group_id", "endpoint"),
		index.Fields("group_id", "target_platform"),
		index.Fields("deleted_at"),
		index.Fields("priority"),
	}
}
