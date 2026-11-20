package service

import (
	"context"
	"strings"
	"time"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

const (
	CompositeRouteMatchExact  = "exact"
	CompositeRouteMatchPrefix = "prefix"

	CompositeRouteEndpointAny             = "any"
	CompositeRouteEndpointMessages        = "messages"
	CompositeRouteEndpointCountTokens     = "count_tokens"
	CompositeRouteEndpointResponses       = "responses"
	CompositeRouteEndpointChatCompletions = "chat_completions"
	CompositeRouteEndpointEmbeddings      = "embeddings"
	CompositeRouteEndpointImages          = "images"
	CompositeRouteEndpointGemini          = "gemini"

	CompositeRouteSourceExplicit = "route"
	CompositeRouteSourceDetector = "detector"
	CompositeRouteSourceAccount  = "account_model"
)

// CompositeModelOwnership identifies the concrete provider that exposes a
// public model through an account-level exact mapping.
type CompositeModelOwnership struct {
	AccountType    string `json:"account_type,omitempty"`
	TargetPlatform string
	Matched        bool
	Ambiguous      bool
}

type CompositeModelOwnershipResolver func(context.Context, int64, string) (CompositeModelOwnership, error)

var (
	ErrCompositeRouteNotFound = infraerrors.NotFound("COMPOSITE_ROUTE_NOT_FOUND", "composite route not found")
	ErrCompositeRouteExists   = infraerrors.Conflict("COMPOSITE_ROUTE_EXISTS", "composite route already exists")
)

// CompositeModelRoute maps one public model identifier in a composite group to
// the concrete provider/model that should handle the request. Targets carries
// the optional ordered fallback list; when it is empty the legacy single-target
// columns define the only target.
type CompositeModelRoute struct {
	ID             int64                  `json:"id"`
	GroupID        int64                  `json:"group_id"`
	PublicModel    string                 `json:"public_model"`
	MatchType      string                 `json:"match_type"`
	AccountType    string                 `json:"account_type,omitempty"`
	TargetPlatform string                 `json:"target_platform"`
	UpstreamModel  string                 `json:"upstream_model"`
	Endpoint       string                 `json:"endpoint"`
	Priority       int                    `json:"priority"`
	Enabled        bool                   `json:"enabled"`
	Notes          string                 `json:"notes"`
	Targets        []CompositeRouteTarget `json:"targets,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

type CompositeRoutePreviewRequest struct {
	Model    string `json:"model"`
	Endpoint string `json:"endpoint"`
}

type CompositeRouteDecision struct {
	Matched        bool                   `json:"matched"`
	Source         string                 `json:"source"`
	GroupID        int64                  `json:"group_id"`
	PublicModel    string                 `json:"public_model"`
	AccountType    string                 `json:"account_type,omitempty"`
	TargetPlatform string                 `json:"target_platform"`
	UpstreamModel  string                 `json:"upstream_model"`
	Endpoint       string                 `json:"endpoint"`
	Targets        []CompositeRouteTarget `json:"targets,omitempty"`
	// SelectedTarget 是本次尝试实际使用的有序目标（含权重、region/provider 约束与亲和性
	// TTL）。同一平台可以有多个目标（例如两个区域各一个 Bedrock 目标），只按平台匹配会
	// 张冠李戴，所以选中目标必须随决策一起传递。
	SelectedTarget *CompositeRouteTarget `json:"selected_target,omitempty"`
	Route          *CompositeModelRoute  `json:"route,omitempty"`
	Reason         string                `json:"reason,omitempty"`
}

type CompositeRouteInput struct {
	AccountTypeSet bool
	PublicModel    string
	MatchType      string
	AccountType    string `json:"account_type,omitempty"`
	TargetPlatform string
	UpstreamModel  string
	Endpoint       string
	Priority       int
	Enabled        bool
	Notes          string
	// Targets replaces the ordered fallback list when non-nil. An empty non-nil
	// list clears it back to the legacy single target; nil keeps the stored list.
	Targets []CompositeRouteTarget `json:"targets,omitempty"`
}

type CompositeModelRouteRepository interface {
	ListByGroup(ctx context.Context, groupID int64, includeDisabled bool) ([]CompositeModelRoute, error)
	Create(ctx context.Context, route *CompositeModelRoute) error
	Update(ctx context.Context, route *CompositeModelRoute) error
	Delete(ctx context.Context, id int64) error
	DeleteByGroup(ctx context.Context, groupID int64) error
}

func normalizeCompositeRouteEndpoint(endpoint string) string {
	endpoint = strings.ToLower(strings.TrimSpace(endpoint))
	if endpoint == "" {
		return CompositeRouteEndpointAny
	}
	switch endpoint {
	case CompositeRouteEndpointMessages,
		CompositeRouteEndpointCountTokens,
		CompositeRouteEndpointResponses,
		CompositeRouteEndpointChatCompletions,
		CompositeRouteEndpointEmbeddings,
		CompositeRouteEndpointImages,
		CompositeRouteEndpointGemini:
		return endpoint
	default:
		return CompositeRouteEndpointAny
	}
}

func normalizeCompositeRouteMatchType(matchType string) string {
	matchType = strings.ToLower(strings.TrimSpace(matchType))
	switch matchType {
	case CompositeRouteMatchPrefix:
		return CompositeRouteMatchPrefix
	default:
		return CompositeRouteMatchExact
	}
}

func normalizeCompositeRouteInput(input CompositeRouteInput) (CompositeRouteInput, error) {
	input.PublicModel = strings.TrimSpace(input.PublicModel)
	input.MatchType = normalizeCompositeRouteMatchType(input.MatchType)
	input.TargetPlatform = strings.TrimSpace(input.TargetPlatform)
	input.UpstreamModel = strings.TrimSpace(input.UpstreamModel)
	input.Endpoint = normalizeCompositeRouteEndpoint(input.Endpoint)
	if input.Targets != nil {
		targets, err := normalizeCompositeRouteTargets(input.Targets)
		if err != nil {
			return input, err
		}
		input.Targets = targets
		// 旧列镜像首个启用目标：回滚到只认单目标的二进制时仍按首个目标路由（可降级）。
		if primary, ok := (CompositeModelRoute{Targets: targets}).PrimaryTarget(); ok {
			input.TargetPlatform = primary.TargetPlatform
			input.AccountType = primary.AccountType
			input.UpstreamModel = primary.UpstreamModel
		}
	}
	// 仅对 exact 路由把空 upstream_model 回填成 public_model：exact 命中时请求模型
	// 恒等于 public_model，回填只影响持久化/后台展示，保留原有契约不变。
	// prefix 路由留空则不回填——Resolve 会回退到具体请求模型，从而透传原始模型
	// （否则 public=deepseek-v4 的前缀路由会把 deepseek-v4-flash / deepseek-v4-pro
	// 都塌缩成固定的 deepseek-v4）。显式填写 upstream_model 时任何模式都原样固定转发。
	if input.UpstreamModel == "" && input.MatchType == CompositeRouteMatchExact {
		input.UpstreamModel = input.PublicModel
	}
	input.Notes = strings.TrimSpace(input.Notes)
	return input, nil
}
