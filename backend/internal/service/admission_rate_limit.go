package service

import (
	"net/http"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 准入限额语义的单一来源。
//
// 这里只描述"用户/组维度"的有效限额，绝不描述上游账号额度：账号池的可用额度是
// 运营内部事实，客户端的退避依据只能来自网关自己的窗口。
//
// 设计要点：
//   - RPM 计数使用固定分钟窗口（与 repository/user_rpm_cache.go 的 key 口径一致）。
//   - 重置时间只能来自真实窗口边界（Redis 服务端分钟或已持久化的窗口起点），
//     没有窗口证据时不允许用本机时钟"推算"一个看起来合理的 reset。
//   - 组覆盖与用户兜底的优先级在 ResolveEffectiveRPMLimits 里集中表达，
//     展示与判定共用同一份实现，避免两处各写一套规则。

// RPMWindowDuration 是用户/组/账号 RPM 计数器的固定窗口长度。
// 必须与 repository 层分钟 key 的口径保持一致。
const RPMWindowDuration = time.Minute

// AdmissionRateLimitSource 标识一条有效 RPM 限额的归属维度。
type AdmissionRateLimitSource string

const (
	// AdmissionRateLimitSourceUser 是用户级全局天花板（始终参与判定）。
	AdmissionRateLimitSourceUser AdmissionRateLimitSource = "user"
	// AdmissionRateLimitSourceGroup 是分组自身容量（仅在该用户无覆盖时生效）。
	AdmissionRateLimitSourceGroup AdmissionRateLimitSource = "group"
	// AdmissionRateLimitSourceUserGroupOverride 是 (用户,分组) 专属限额，替代分组容量。
	AdmissionRateLimitSourceUserGroupOverride AdmissionRateLimitSource = "user_group_override"
)

// EffectiveRPM 是一条对当前请求实际生效的 RPM 限额。
type EffectiveRPM struct {
	Source        AdmissionRateLimitSource
	Limit         int
	ScopeID       int64
	WindowSeconds int
}

// ResolveEffectiveRPMLimits 解析 (用户,分组) 上实际生效的 RPM 限额。
//
// 优先级（与 BillingCacheService.checkRPM 的判定完全同源）：
//  1. (用户,分组) 覆盖存在时替代分组容量；覆盖值 0 表示该维度免检。
//  2. 用户级全局上限始终生效，不会被分组或覆盖取消。
//
// 返回顺序即展示顺序：更细的维度在前，全局天花板在后。
func ResolveEffectiveRPMLimits(userRPMLimit int, userID int64, groupID int64, groupRPMLimit int, override *int) []EffectiveRPM {
	limits := make([]EffectiveRPM, 0, 2)
	switch {
	case override != nil && *override > 0:
		limits = append(limits, EffectiveRPM{
			Source:        AdmissionRateLimitSourceUserGroupOverride,
			Limit:         *override,
			ScopeID:       groupID,
			WindowSeconds: int(RPMWindowDuration / time.Second),
		})
	case override == nil && groupRPMLimit > 0:
		limits = append(limits, EffectiveRPM{
			Source:        AdmissionRateLimitSourceGroup,
			Limit:         groupRPMLimit,
			ScopeID:       groupID,
			WindowSeconds: int(RPMWindowDuration / time.Second),
		})
	}
	if userRPMLimit > 0 {
		limits = append(limits, EffectiveRPM{
			Source:        AdmissionRateLimitSourceUser,
			Limit:         userRPMLimit,
			ScopeID:       userID,
			WindowSeconds: int(RPMWindowDuration / time.Second),
		})
	}
	return limits
}

// RPMWindowResetAt 返回包含 now 的固定分钟窗口的结束时刻。
// 只用于把 Redis 服务端时间换算成窗口边界，不用于替代服务端时间来源。
func RPMWindowResetAt(now time.Time) time.Time {
	return now.Truncate(RPMWindowDuration).Add(RPMWindowDuration)
}

// RetryAfterSecondsUntil 把重置时刻换算成 Retry-After 秒数。
// 已过期（或零值）返回 0，表示调用方必须省略 Retry-After 而不是编造一个值。
func RetryAfterSecondsUntil(resetAt, now time.Time) int {
	if resetAt.IsZero() {
		return 0
	}
	remaining := resetAt.Sub(now)
	if remaining <= 0 {
		return 0
	}
	seconds := int64(remaining / time.Second)
	if remaining%time.Second > 0 {
		seconds++
	}
	if seconds <= 0 {
		return 0
	}
	return int(seconds)
}

// RateLimitHeaderValues 是回给客户端的限额事实（仅用户/组维度）。
type RateLimitHeaderValues struct {
	Limit     int
	Remaining int
	ResetAt   time.Time
}

// 准入拒绝错误上携带的真实限额事实键。
// 只允许装用户/组维度的限额，不得装上游账号额度。
const (
	AdmissionWindowResetsAtMetadataKey = "window_resets_at"
	AdmissionRateLimitLimitKey         = "rate_limit_limit"
	AdmissionRateLimitUsedKey          = "rate_limit_used"
)

// AdmissionRateLimitFacts 读回错误携带的真实限额事实。
// 第二个返回值为 false 表示这个错误没有任何可信的窗口/限额证据，
// 调用方必须省略相应头，不得用本机时钟或上游账号额度补齐。
func AdmissionRateLimitFacts(err error) (RateLimitHeaderValues, bool) {
	appErr := infraerrors.FromError(err)
	if appErr == nil || len(appErr.Metadata) == 0 {
		return RateLimitHeaderValues{}, false
	}
	var values RateLimitHeaderValues
	found := false
	if raw := appErr.Metadata[AdmissionWindowResetsAtMetadataKey]; raw != "" {
		if resetAt, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
			values.ResetAt = resetAt
			found = true
		}
	}
	if raw := appErr.Metadata[AdmissionRateLimitLimitKey]; raw != "" {
		if limit, parseErr := strconv.Atoi(raw); parseErr == nil && limit > 0 {
			values.Limit = limit
			found = true
		}
	}
	if raw := appErr.Metadata[AdmissionRateLimitUsedKey]; raw != "" {
		if used, parseErr := strconv.Atoi(raw); parseErr == nil && used >= 0 && values.Limit > 0 {
			values.Remaining = values.Limit - used
		}
	}
	return values, found
}

// ApplyRateLimitHeaders 写出 X-RateLimit-* 与 Retry-After。
//
// 只写已知的真实值：没有 reset 证据时不写 X-RateLimit-Reset-Requests，
// 也不写 Retry-After，避免客户端按伪造的退避时间重试。
// 上游账号额度的任何字段都不允许经过这里。
func ApplyRateLimitHeaders(header http.Header, values RateLimitHeaderValues, now time.Time) {
	if header == nil {
		return
	}
	if values.Limit > 0 {
		header.Set("X-RateLimit-Limit-Requests", strconv.Itoa(values.Limit))
		remaining := values.Remaining
		if remaining < 0 {
			remaining = 0
		}
		if remaining > values.Limit {
			remaining = values.Limit
		}
		header.Set("X-RateLimit-Remaining-Requests", strconv.Itoa(remaining))
	}
	if seconds := RetryAfterSecondsUntil(values.ResetAt, now); seconds > 0 {
		header.Set("X-RateLimit-Reset-Requests", strconv.Itoa(seconds))
		header.Set("Retry-After", strconv.Itoa(seconds))
	}
}

// APIKeyEffectiveRPM 是用户可见的有效 RPM 说明。
//
// 只描述用户/组维度的限额，绝不包含上游账号额度：那是运营内部事实。
// 展示顺序即判定顺序（更细的维度在前，用户级天花板在后），与
// BillingCacheService.checkRPM 共用 ResolveEffectiveRPMLimits。
type APIKeyEffectiveRPM struct {
	Limits []EffectiveRPM `json:"limits"`
	// WindowResetsAt 是当前 RPM 窗口的真实结束时刻；没有服务端时间证据时为 nil，
	// 表示"暂时无法给出可信重置时间"，而不是"不会重置"。
	WindowResetsAt *time.Time `json:"window_resets_at,omitempty"`
	// Priority 是来源语义的机器可读说明：组覆盖替代分组容量，用户级上限始终生效。
	Priority string `json:"priority"`
}

// EffectiveRPMPriority 是对外承诺的优先级语义，与 ResolveEffectiveRPMLimits 一致。
const EffectiveRPMPriority = "user_group_override_then_group_then_user_ceiling"
