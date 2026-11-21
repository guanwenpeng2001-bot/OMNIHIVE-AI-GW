package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

const (
	ChannelTelemetryOtherModel      = "__other__"
	ChannelTelemetryTaxonomyVersion = 1
)

var (
	ErrChannelTelemetryInvalidRange   = errors.New("invalid channel monitor v2 range")
	ErrChannelTelemetryInvalidGroupBy = errors.New("invalid channel monitor v2 group_by")
	ErrChannelTelemetryInvalidConfig  = errors.New("invalid channel monitor v2 config")
	ErrChannelTelemetryConfigConflict = errors.New("channel monitor v2 config was modified")
)

type ChannelTelemetryGroupBy string

const (
	ChannelTelemetryGroupByPlatform           ChannelTelemetryGroupBy = "platform"
	ChannelTelemetryGroupByPlatformGroup      ChannelTelemetryGroupBy = "platform_group"
	ChannelTelemetryGroupByPlatformModel      ChannelTelemetryGroupBy = "platform_model"
	ChannelTelemetryGroupByPlatformGroupModel ChannelTelemetryGroupBy = "platform_group_model"
)

type ChannelTelemetryPlatformConfig struct {
	Platform string   `json:"platform"`
	Enabled  bool     `json:"enabled"`
	Models   []string `json:"models"`
}

type ChannelTelemetryConfig struct {
	Version                int                              `json:"version"`
	Enabled                bool                             `json:"enabled"`
	RefreshIntervalSeconds int                              `json:"refresh_interval_seconds"`
	Platforms              []ChannelTelemetryPlatformConfig `json:"platforms"`
	GroupIDs               []int64                          `json:"group_ids"`
	HealthThresholds       ChannelTelemetryHealthThresholds `json:"health_thresholds"`
	// IgnoredErrorCategories are excluded from error_rate / health scoring.
	// They still appear in the error breakdown with ignored=true (greyed in UI).
	// Unknown categories always roll into "other" via the taxonomy classifier.
	IgnoredErrorCategories []string  `json:"ignored_error_categories"`
	UpdatedAt              time.Time `json:"updated_at"`
	UpdatedBy              *int64    `json:"updated_by,omitempty"`
}

// ChannelTelemetryErrorCategories is the ordered, versioned taxonomy used by the
// classifier and admin settings UI. Unmatched errors become "other".
var ChannelTelemetryErrorCategories = []string{
	"content_policy",
	"authentication",
	"context_limit",
	"invalid_request",
	"model_unsupported",
	"group_access",
	"quota_or_balance",
	"account_pool_unavailable",
	"rate_or_capacity",
	"timeout",
	"transport_or_stream",
	"upstream_forbidden",
	"not_found",
	"client_cancelled",
	"upstream_5xx",
	"internal",
	"other",
}

type ChannelTelemetryFilter struct {
	WorkspaceID int64
	Range       string
	Platforms   []string
	GroupIDs    []int64
	// AllowedGroupIDs is the authenticated viewer's server-derived group scope.
	// RestrictGroups distinguishes an ordinary user with no allowed groups from
	// the unrestricted admin/configured scope represented by an empty slice.
	AllowedGroupIDs []int64
	RestrictGroups  bool
	Models          []string
	Start           time.Time
	End             time.Time
	Bucket          time.Duration
}

type ChannelTelemetryMetric struct {
	SuccessRequests          int64                   `json:"success_requests"`
	ErrorRequests            int64                   `json:"error_requests"`
	RequestCount             int64                   `json:"request_count"`
	InputTokens              int64                   `json:"input_tokens"`
	OutputTokens             int64                   `json:"output_tokens"`
	CacheCreationTokens      int64                   `json:"cache_creation_tokens"`
	CacheReadTokens          int64                   `json:"cache_read_tokens"`
	TokenCount               int64                   `json:"token_count"`
	RPM                      float64                 `json:"rpm"`
	TPM                      float64                 `json:"tpm"`
	ErrorRate                float64                 `json:"error_rate"`
	SuccessRate              float64                 `json:"success_rate"`
	CacheRate                float64                 `json:"cache_rate"`
	CacheRateNumerator       int64                   `json:"cache_rate_numerator"`
	CacheRateDenominator     int64                   `json:"cache_rate_denominator"`
	TTFT                     ChannelTelemetryLatency `json:"ttft"`
	Duration                 ChannelTelemetryLatency `json:"duration"`
	UpstreamAffectedRequests *int64                  `json:"upstream_affected_requests,omitempty"`
	UpstreamAttemptCount     *int64                  `json:"upstream_attempt_count,omitempty"`
}

type ChannelTelemetryLatency struct {
	SampleCount int64    `json:"sample_count"`
	P50Ms       *int64   `json:"p50_ms"`
	P90Ms       *int64   `json:"p90_ms"`
	P95Ms       *int64   `json:"p95_ms"`
	AvgMs       *float64 `json:"avg_ms"`
}

type ChannelTelemetryHealth struct {
	Overall   string `json:"overall"`
	ErrorRate string `json:"error_rate"`
	TTFT      string `json:"ttft"`
	Cache     string `json:"cache"`
	// Score is 0–100 when samples are sufficient; omitted/null when unknown.
	// Overall blends error-rate, TTFT p50, and cache rate (weights in Thresholds).
	Score          *float64                         `json:"score,omitempty"`
	ErrorRateScore *float64                         `json:"error_rate_score,omitempty"`
	TTFTScore      *float64                         `json:"ttft_score,omitempty"`
	CacheScore     *float64                         `json:"cache_score,omitempty"`
	MinimumSample  int64                            `json:"minimum_sample"`
	Thresholds     ChannelTelemetryHealthThresholds `json:"thresholds"`
}

type ChannelTelemetryHealthThresholds struct {
	// MinimumSample is required before scoring request/latency/cache signals.
	MinimumSample int64 `json:"minimum_sample"`
	// WarningErrorRate / CriticalErrorRate map to discrete bands for legacy UI.
	WarningErrorRate  float64 `json:"warning_error_rate"`
	CriticalErrorRate float64 `json:"critical_error_rate"`
	// TargetTTFTMs is the primary TTFT p50 budget (ms). At or below this is full TTFT score.
	TargetTTFTMs   int64 `json:"target_ttft_ms"`
	WarningTTFTMs  int64 `json:"warning_ttft_ms"`
	CriticalTTFTMs int64 `json:"critical_ttft_ms"`
	// WarningCacheRate / CriticalCacheRate: cache rate below these → warning/critical bands.
	// Higher cache rate is better; defaults 20% warning / 5% critical.
	WarningCacheRate  float64 `json:"warning_cache_rate"`
	CriticalCacheRate float64 `json:"critical_cache_rate"`
	// ErrorWeight + TTFTWeight + CacheWeight should sum to 1.0.
	ErrorWeight float64 `json:"error_weight"`
	TTFTWeight  float64 `json:"ttft_weight"`
	CacheWeight float64 `json:"cache_weight"`
}

type ChannelTelemetryCoverage struct {
	RequestedStart time.Time `json:"requested_start"`
	// RequestedEnd is the exclusive upper bound of the UI-selected window
	// (filter.End). Charts/matrices should plot [RequestedStart, RequestedEnd)
	// even when CoverageStart is later (partial backfill).
	RequestedEnd          time.Time `json:"requested_end"`
	CoverageStart         time.Time `json:"coverage_start"`
	DataThrough           time.Time `json:"data_through"`
	ComputedAt            time.Time `json:"computed_at"`
	AggregationLagSeconds int64     `json:"aggregation_lag_seconds"`
	// CoverageComplete is true when aggregated history reaches the requested
	// window start (backfill / product coverage for this range is done).
	// Trailing lag — data_through behind filter.End because UI ends align to
	// the next bucket and may sit slightly in the future — does not make this
	// false once history is covered.
	CoverageComplete bool `json:"coverage_complete"`
	BucketSeconds    int  `json:"bucket_seconds"`
	// Bootstrap is set while the first-upgrade historical backfill toward the
	// product windows (90m / 24h / 7d / 30d) is still running. Omitted or
	// inactive once the 30d UI range is fully covered; longer retention (90d)
	// may continue silently without a progress banner.
	Bootstrap *ChannelTelemetryBootstrap `json:"bootstrap,omitempty"`
}

// ChannelTelemetryBootstrap reports first-upgrade aggregation progress for the UI.
// Progress is measured against the longest product window (30d), not full 90d retention.
type ChannelTelemetryBootstrap struct {
	// Active is true while historical backfill has not yet covered the 30d product window.
	Active bool `json:"active"`
	// ProgressPercent is 0–100 of history covered from now back toward TargetStart.
	ProgressPercent int `json:"progress_percent"`
	// CoveredFrom is the earliest minute already recomputed (backfill cursor).
	CoveredFrom time.Time `json:"covered_from,omitempty"`
	// TargetStart is now−30d (product bootstrap goal for 90m/24h/7d/30d).
	TargetStart time.Time `json:"target_start,omitempty"`
}

// ChannelTelemetryAggregationWatermark is the durable aggregator cursor state.
type ChannelTelemetryAggregationWatermark struct {
	UsageCoverageStart time.Time
	ErrorCoverageStart time.Time
	DataThrough        time.Time
	LastSuccessfulAt   time.Time
	// BackfillCursor is the earliest start already recomputed; zero when never set.
	BackfillCursor time.Time
	// HasData is true once any successful recompute wrote data_through.
	HasData bool
}

type ChannelTelemetryTrendPoint struct {
	BucketStart time.Time              `json:"bucket_start"`
	Metrics     ChannelTelemetryMetric `json:"metrics"`
	Health      ChannelTelemetryHealth `json:"health"`
}

type ChannelTelemetrySnapshot struct {
	Config   ChannelTelemetryConfig       `json:"config"`
	Coverage ChannelTelemetryCoverage     `json:"coverage"`
	Metrics  ChannelTelemetryMetric       `json:"metrics"`
	Health   ChannelTelemetryHealth       `json:"health"`
	Trend    []ChannelTelemetryTrendPoint `json:"trend"`
}

type ChannelTelemetryDimension struct {
	Value        string `json:"value"`
	Label        string `json:"label"`
	Platform     string `json:"platform,omitempty"`
	RequestCount int64  `json:"request_count"`
}

type ChannelTelemetryGroupDimension struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Platform     string `json:"platform,omitempty"`
	RequestCount int64  `json:"request_count"`
}

type ChannelTelemetryDimensions struct {
	Platforms []ChannelTelemetryDimension      `json:"platforms"`
	Groups    []ChannelTelemetryGroupDimension `json:"groups"`
	Models    []ChannelTelemetryDimension      `json:"models"`
}

type ChannelTelemetryModelRow struct {
	Platform string                 `json:"platform"`
	Model    string                 `json:"model"`
	Metrics  ChannelTelemetryMetric `json:"metrics"`
	Health   ChannelTelemetryHealth `json:"health"`
}

type ChannelTelemetryMatrixRow struct {
	Platform  string                       `json:"platform"`
	GroupID   *int64                       `json:"group_id,omitempty"`
	GroupName string                       `json:"group_name,omitempty"`
	Model     string                       `json:"model,omitempty"`
	Metrics   ChannelTelemetryMetric       `json:"metrics"`
	Health    ChannelTelemetryHealth       `json:"health"`
	Buckets   []ChannelTelemetryTrendPoint `json:"buckets"`
}

type ChannelTelemetryMatrix struct {
	GroupBy  ChannelTelemetryGroupBy     `json:"group_by"`
	Coverage ChannelTelemetryCoverage    `json:"coverage"`
	Items    []ChannelTelemetryMatrixRow `json:"items"`
}

type ChannelTelemetryErrorRow struct {
	Category string                        `json:"category"`
	Count    int64                         `json:"count"`
	Rate     float64                       `json:"rate"`
	Details  []ChannelTelemetryErrorDetail `json:"details,omitempty"`
	// Ignored is true when this category is excluded from error_rate scoring
	// via config.ignored_error_categories. Still shown in breakdown (UI greys it).
	Ignored bool `json:"ignored"`
}

type ChannelTelemetryErrorDetail struct {
	Platform           string `json:"platform,omitempty"`
	Model              string `json:"model,omitempty"`
	ErrorType          string `json:"error_type,omitempty"`
	StatusCode         int    `json:"status_code,omitempty"`
	UpstreamStatusCode int    `json:"upstream_status_code,omitempty"`
	Message            string `json:"message,omitempty"`
	Count              int64  `json:"count"`
}

type ChannelTelemetryUserRow struct {
	UserID       *int64                 `json:"user_id,omitempty"`
	Rank         int                    `json:"rank"`
	Email        string                 `json:"email,omitempty"`
	Username     string                 `json:"username,omitempty"`
	DisplayLabel string                 `json:"display_label"`
	IsSelf       bool                   `json:"is_self"`
	CanDrilldown bool                   `json:"can_drilldown"`
	Metrics      ChannelTelemetryMetric `json:"metrics"`
}

type ChannelTelemetryList[T any] struct {
	Coverage ChannelTelemetryCoverage `json:"coverage"`
	Items    []T                      `json:"items"`
}

type ChannelTelemetryRepository interface {
	GetConfig(ctx context.Context) (*ChannelTelemetryConfig, error)
	UpdateConfig(ctx context.Context, config ChannelTelemetryConfig, expectedVersion int) (*ChannelTelemetryConfig, error)
	GetDimensions(ctx context.Context, filter ChannelTelemetryFilter, config ChannelTelemetryConfig) (*ChannelTelemetryDimensions, error)
	GetSnapshot(ctx context.Context, filter ChannelTelemetryFilter, config ChannelTelemetryConfig, includeAdmin bool) (*ChannelTelemetrySnapshot, error)
	GetModels(ctx context.Context, filter ChannelTelemetryFilter, config ChannelTelemetryConfig, includeAdmin bool) (*ChannelTelemetryList[ChannelTelemetryModelRow], error)
	GetMatrix(ctx context.Context, filter ChannelTelemetryFilter, config ChannelTelemetryConfig, groupBy ChannelTelemetryGroupBy, includeAdmin bool) (*ChannelTelemetryMatrix, error)
	// GetErrors loads category rates. When includeAdmin is false, implementations
	// must omit error Details (no ops_error_logs sample scan) for privacy.
	GetErrors(ctx context.Context, filter ChannelTelemetryFilter, config ChannelTelemetryConfig, includeAdmin bool) (*ChannelTelemetryList[ChannelTelemetryErrorRow], error)
	GetUsers(ctx context.Context, filter ChannelTelemetryFilter, config ChannelTelemetryConfig, includeAdmin bool) (*ChannelTelemetryList[ChannelTelemetryUserRow], error)
	// GetAggregationWatermark loads durable backfill / coverage cursors for the
	// passive aggregator (and bootstrap progress). Missing row → zero value, nil error.
	GetAggregationWatermark(ctx context.Context) (*ChannelTelemetryAggregationWatermark, error)
	RecomputeRange(ctx context.Context, start, end time.Time) error
}

// ChannelTelemetryBootstrapProductWindow is the longest UI range that must be
// filled on first upgrade before the bootstrap banner disappears (30d).
const ChannelTelemetryBootstrapProductWindow = 30 * 24 * time.Hour

// ChannelTelemetryBootstrapProgress builds the optional UI progress payload.
// now and coveredFrom should be UTC minute-truncated when possible.
// When history is complete for the 30d product window, returns nil (no banner).
func ChannelTelemetryBootstrapProgress(now, coveredFrom time.Time, hasData bool) *ChannelTelemetryBootstrap {
	now = now.UTC().Truncate(time.Minute)
	targetStart := now.Add(-ChannelTelemetryBootstrapProductWindow)
	if !hasData {
		return &ChannelTelemetryBootstrap{
			Active:          true,
			ProgressPercent: 0,
			TargetStart:     targetStart,
		}
	}
	if coveredFrom.IsZero() {
		// Have data_through but no cursor yet — treat as barely started.
		return &ChannelTelemetryBootstrap{
			Active:          true,
			ProgressPercent: 0,
			TargetStart:     targetStart,
		}
	}
	coveredFrom = coveredFrom.UTC().Truncate(time.Minute)
	if !coveredFrom.After(targetStart) {
		// 30d product windows fully covered; hide progress (90d retention may continue).
		return nil
	}
	total := now.Sub(targetStart).Seconds()
	if total <= 0 {
		return nil
	}
	done := now.Sub(coveredFrom).Seconds()
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	// Round to nearest percent; any non-zero history shows at least 1% so the
	// bar moves after the first 2h bootstrap tick (2h/30d ≈ 0.3%).
	pct := int((done/total)*100 + 0.5)
	if done > 0 && pct < 1 {
		pct = 1
	}
	if pct > 100 {
		pct = 100
	}
	if pct >= 100 {
		// Cursor still slightly after target due to rounding — keep active until
		// coveredFrom <= targetStart so the banner does not flash 100% then vanish.
		pct = 99
	}
	return &ChannelTelemetryBootstrap{
		Active:          true,
		ProgressPercent: pct,
		CoveredFrom:     coveredFrom,
		TargetStart:     targetStart,
	}
}

type ChannelTelemetryService struct {
	repo     ChannelTelemetryRepository
	settings channelMonitorRuntimeReader
	now      func() time.Time
}

func NewChannelTelemetryService(repo ChannelTelemetryRepository) *ChannelTelemetryService {
	return &ChannelTelemetryService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

// SetRuntimeReader wires optional settings for privacy flags (hide throughput).
func (s *ChannelTelemetryService) SetRuntimeReader(r channelMonitorRuntimeReader) {
	if s == nil {
		return
	}
	s.settings = r
}

func (s *ChannelTelemetryService) hideThroughputForViewer(ctx context.Context, admin bool) bool {
	if admin {
		return false
	}
	// Privacy is fail-closed: an absent or unavailable settings reader must not
	// expose fleet-scale rates to ordinary users.
	if s == nil || s.settings == nil {
		return true
	}
	return s.settings.GetChannelMonitorRuntime(ctx).HideThroughput
}

func (s *ChannelTelemetryService) hideUserRankingForViewer(ctx context.Context, admin bool) bool {
	if admin {
		return false
	}
	// Missing reader keeps the current ranking tab visible. Unlike throughput,
	// ranking is already public and must stay on until an operator turns it off.
	if s == nil || s.settings == nil {
		return false
	}
	return s.settings.GetChannelMonitorRuntime(ctx).HideUserRanking
}

func (s *ChannelTelemetryService) GetConfig(ctx context.Context) (*ChannelTelemetryConfig, error) {
	return s.repo.GetConfig(ctx)
}

func (s *ChannelTelemetryService) getEnabledConfig(ctx context.Context) (*ChannelTelemetryConfig, error) {
	cfg, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled {
		return nil, ErrChannelMonitorDisabled
	}
	return cfg, nil
}

func (s *ChannelTelemetryService) UpdateConfig(ctx context.Context, cfg ChannelTelemetryConfig, expectedVersion int, actorID int64) (*ChannelTelemetryConfig, error) {
	if err := normalizeChannelTelemetryConfig(&cfg); err != nil {
		return nil, err
	}
	cfg.UpdatedBy = &actorID
	return s.repo.UpdateConfig(ctx, cfg, expectedVersion)
}

func (s *ChannelTelemetryService) ParseFilter(rangeValue string, platforms, models []string, groupIDs []int64) (ChannelTelemetryFilter, error) {
	now := s.now().UTC()
	var window, bucket time.Duration
	switch strings.TrimSpace(rangeValue) {
	case "", "90m":
		rangeValue, window, bucket = "90m", 90*time.Minute, 5*time.Minute
	case "24h":
		window, bucket = 24*time.Hour, time.Hour
	case "7d":
		window, bucket = 7*24*time.Hour, 12*time.Hour
	case "30d":
		window, bucket = 30*24*time.Hour, 24*time.Hour
	default:
		return ChannelTelemetryFilter{}, fmt.Errorf("%w: %s", ErrChannelTelemetryInvalidRange, rangeValue)
	}
	start, end := now.Add(-window), now
	if bucket > time.Minute {
		// Align display windows to a fixed number of whole buckets. The trailing
		// bucket may point slightly into the future; SQL simply has no future rows,
		// while the current partial bucket can still be shown and recomputed often.
		end = now.Truncate(bucket).Add(bucket)
		start = end.Add(-window)
	}
	return ChannelTelemetryFilter{
		Range: rangeValue, Platforms: normalizeStringSet(platforms), Models: normalizeStringSet(models), GroupIDs: normalizeInt64Set(groupIDs),
		Start: start, End: end, Bucket: bucket,
	}, nil
}

func (s *ChannelTelemetryService) Dimensions(ctx context.Context, filter ChannelTelemetryFilter) (*ChannelTelemetryDimensions, error) {
	cfg, err := s.getEnabledConfig(ctx)
	if err != nil {
		return nil, err
	}
	dims, err := s.repo.GetDimensions(ctx, filter, *cfg)
	if err != nil {
		return nil, err
	}
	// Dimension request_count is operational volume; strip for non-admin callers
	// at the API edge. Dimensions is shared by user/admin routes — redaction is
	// applied in the handler for user routes only, so keep raw here.
	return dims, nil
}

func (s *ChannelTelemetryService) Snapshot(ctx context.Context, filter ChannelTelemetryFilter, admin bool) (*ChannelTelemetrySnapshot, error) {
	cfg, err := s.getEnabledConfig(ctx)
	if err != nil {
		return nil, err
	}
	snap, err := s.repo.GetSnapshot(ctx, filter, *cfg, admin)
	if err != nil {
		return nil, err
	}
	if !admin && snap != nil {
		redactChannelTelemetrySnapshot(snap, s.hideThroughputForViewer(ctx, admin))
	}
	return snap, nil
}

func (s *ChannelTelemetryService) Models(ctx context.Context, filter ChannelTelemetryFilter, admin bool) (*ChannelTelemetryList[ChannelTelemetryModelRow], error) {
	cfg, err := s.getEnabledConfig(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.repo.GetModels(ctx, filter, *cfg, admin)
	if err != nil {
		return nil, err
	}
	if !admin && list != nil {
		hideTP := s.hideThroughputForViewer(ctx, admin)
		for i := range list.Items {
			redactChannelTelemetryMetric(&list.Items[i].Metrics, hideTP)
		}
	}
	return list, nil
}

func (s *ChannelTelemetryService) Matrix(ctx context.Context, filter ChannelTelemetryFilter, groupBy ChannelTelemetryGroupBy, admin bool) (*ChannelTelemetryMatrix, error) {
	if !groupBy.Valid() {
		return nil, fmt.Errorf("%w: %s", ErrChannelTelemetryInvalidGroupBy, groupBy)
	}
	cfg, err := s.getEnabledConfig(ctx)
	if err != nil {
		return nil, err
	}
	matrix, err := s.repo.GetMatrix(ctx, filter, *cfg, groupBy, admin)
	if err != nil {
		return nil, err
	}
	if !admin && matrix != nil {
		hideTP := s.hideThroughputForViewer(ctx, admin)
		for i := range matrix.Items {
			redactChannelTelemetryMetric(&matrix.Items[i].Metrics, hideTP)
			for j := range matrix.Items[i].Buckets {
				redactChannelTelemetryMetric(&matrix.Items[i].Buckets[j].Metrics, hideTP)
			}
		}
	}
	return matrix, nil
}

func ParseChannelTelemetryGroupBy(value string) (ChannelTelemetryGroupBy, error) {
	groupBy := ChannelTelemetryGroupBy(strings.TrimSpace(value))
	if groupBy == "" {
		// Default presentation: platform / group (matches operator mental model).
		groupBy = ChannelTelemetryGroupByPlatformGroup
	}
	if !groupBy.Valid() {
		return "", fmt.Errorf("%w: %s", ErrChannelTelemetryInvalidGroupBy, value)
	}
	return groupBy, nil
}

func (g ChannelTelemetryGroupBy) Valid() bool {
	switch g {
	case ChannelTelemetryGroupByPlatform, ChannelTelemetryGroupByPlatformGroup, ChannelTelemetryGroupByPlatformModel, ChannelTelemetryGroupByPlatformGroupModel:
		return true
	default:
		return false
	}
}

func (s *ChannelTelemetryService) Errors(ctx context.Context, filter ChannelTelemetryFilter) (*ChannelTelemetryList[ChannelTelemetryErrorRow], error) {
	return s.ErrorsForViewer(ctx, filter, false)
}

// ErrorsForViewer returns the error breakdown.
// Non-admin callers receive category rates + ignored flags only: absolute Count
// is zeroed and Details (upstream messages / status codes / volume) are omitted.
func (s *ChannelTelemetryService) ErrorsForViewer(ctx context.Context, filter ChannelTelemetryFilter, admin bool) (*ChannelTelemetryList[ChannelTelemetryErrorRow], error) {
	cfg, err := s.getEnabledConfig(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.repo.GetErrors(ctx, filter, *cfg, admin)
	if err != nil {
		return nil, err
	}
	if !admin && list != nil {
		for i := range list.Items {
			list.Items[i].Count = 0
			list.Items[i].Details = nil
		}
	}
	return list, nil
}

// RedactChannelTelemetryDimensions clears absolute request counts on filter chips.
func RedactChannelTelemetryDimensions(dims *ChannelTelemetryDimensions) {
	if dims == nil {
		return
	}
	for i := range dims.Platforms {
		dims.Platforms[i].RequestCount = 0
	}
	for i := range dims.Models {
		dims.Models[i].RequestCount = 0
	}
	for i := range dims.Groups {
		dims.Groups[i].RequestCount = 0
	}
}

func redactChannelTelemetrySnapshot(snap *ChannelTelemetrySnapshot, hideThroughput bool) {
	if snap == nil {
		return
	}
	redactChannelTelemetryMetric(&snap.Metrics, hideThroughput)
	for i := range snap.Trend {
		redactChannelTelemetryMetric(&snap.Trend[i].Metrics, hideThroughput)
	}
	// Public snapshot only needs display thresholds + refresh cadence, not
	// operational allow-lists (group_ids, model inventories, ignored categories).
	redactChannelTelemetryPublicConfig(&snap.Config)
}

// redactChannelTelemetryPublicConfig strips operator-policy fields from config
// embedded in user-facing snapshots. Full config remains on admin /config.
func redactChannelTelemetryPublicConfig(cfg *ChannelTelemetryConfig) {
	if cfg == nil {
		return
	}
	cfg.GroupIDs = nil
	cfg.IgnoredErrorCategories = nil
	cfg.UpdatedBy = nil
	for i := range cfg.Platforms {
		cfg.Platforms[i].Models = nil
	}
}

// redactChannelTelemetryMetric zeros absolute volume counters while keeping rates
// (error_rate, success_rate, cache_rate) and latency percentiles.
// When hideThroughput is true, also zeros RPM/TPM so users cannot reverse-estimate
// fleet scale from rate × window length.
func redactChannelTelemetryMetric(m *ChannelTelemetryMetric, hideThroughput bool) {
	if m == nil {
		return
	}
	m.SuccessRequests = 0
	m.ErrorRequests = 0
	m.RequestCount = 0
	m.InputTokens = 0
	m.OutputTokens = 0
	m.CacheCreationTokens = 0
	m.CacheReadTokens = 0
	m.TokenCount = 0
	m.CacheRateNumerator = 0
	m.CacheRateDenominator = 0
	// Latency sample_count is also a volume signal.
	m.TTFT.SampleCount = 0
	m.Duration.SampleCount = 0
	m.UpstreamAffectedRequests = nil
	m.UpstreamAttemptCount = nil
	if hideThroughput {
		m.RPM = 0
		m.TPM = 0
	}
}

func (s *ChannelTelemetryService) Users(ctx context.Context, filter ChannelTelemetryFilter, viewerID int64, admin bool) (*ChannelTelemetryList[ChannelTelemetryUserRow], error) {
	cfg, err := s.getEnabledConfig(ctx)
	if err != nil {
		return nil, err
	}
	if s.hideUserRankingForViewer(ctx, admin) {
		return &ChannelTelemetryList[ChannelTelemetryUserRow]{Items: []ChannelTelemetryUserRow{}}, nil
	}
	result, err := s.repo.GetUsers(ctx, filter, *cfg, admin)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &ChannelTelemetryList[ChannelTelemetryUserRow]{}
	}
	selfIndex := -1
	for i := range result.Items {
		result.Items[i].Rank = i + 1
		if result.Items[i].UserID != nil && *result.Items[i].UserID == viewerID {
			selfIndex = i
			result.Items[i].IsSelf = true
		}
	}
	// Viewer with no traffic in this window is still shown (and highlighted) so
	// ranking always answers "where am I?" — not only when already in the top list.
	if selfIndex < 0 && viewerID > 0 {
		id := viewerID
		selfRow := ChannelTelemetryUserRow{
			UserID:       &id,
			Rank:         0, // unranked / no traffic in window
			IsSelf:       true,
			CanDrilldown: true,
			DisplayLabel: "Me",
			Metrics:      ChannelTelemetryMetric{},
		}
		result.Items = append(result.Items, selfRow)
		selfIndex = len(result.Items) - 1
	}
	result.Items = channelTelemetryTopUsersWithSelf(result.Items, selfIndex, 10)
	hideTP := s.hideThroughputForViewer(ctx, admin)
	if admin {
		// Keep identity for admin; still mark self for UI highlight.
		for i := range result.Items {
			if result.Items[i].UserID != nil && *result.Items[i].UserID == viewerID {
				result.Items[i].IsSelf = true
				if result.Items[i].DisplayLabel == "" || result.Items[i].DisplayLabel == "Me" {
					// Prefer real label when available from repo.
					if result.Items[i].Username != "" {
						result.Items[i].DisplayLabel = result.Items[i].Username
					} else if result.Items[i].Email != "" {
						result.Items[i].DisplayLabel = result.Items[i].Email
					} else {
						result.Items[i].DisplayLabel = "Me"
					}
				}
			}
		}
		return result, nil
	}
	for i := range result.Items {
		redactChannelTelemetryMetric(&result.Items[i].Metrics, hideTP)
	}
	for i := range result.Items {
		row := &result.Items[i]
		if row.UserID != nil && *row.UserID == viewerID {
			row.IsSelf, row.CanDrilldown, row.DisplayLabel = true, true, "Me"
			continue
		}
		row.UserID, row.Email, row.Username, row.CanDrilldown = nil, "", "", false
		row.DisplayLabel = fmt.Sprintf("Other user #%d", i+1)
	}
	return result, nil
}

func channelTelemetryTopUsersWithSelf(items []ChannelTelemetryUserRow, selfIndex int, limit int) []ChannelTelemetryUserRow {
	if limit <= 0 {
		return items
	}
	if len(items) <= limit {
		return items
	}
	out := append([]ChannelTelemetryUserRow(nil), items[:limit]...)
	if selfIndex >= limit && selfIndex < len(items) {
		out = append(out, items[selfIndex])
	}
	return out
}

func normalizeChannelTelemetryConfig(cfg *ChannelTelemetryConfig) error {
	if cfg.RefreshIntervalSeconds == 0 {
		cfg.RefreshIntervalSeconds = 300
	}
	if cfg.RefreshIntervalSeconds != 60 && cfg.RefreshIntervalSeconds != 300 {
		return fmt.Errorf("%w: refresh_interval_seconds must be 60 or 300", ErrChannelTelemetryInvalidConfig)
	}
	var err error
	cfg.GroupIDs, err = normalizeChannelTelemetryGroupIDs(cfg.GroupIDs)
	if err != nil {
		return err
	}
	cfg.IgnoredErrorCategories = normalizeChannelTelemetryIgnoredCategories(cfg.IgnoredErrorCategories)
	cfg.HealthThresholds = NormalizeChannelTelemetryHealthThresholds(cfg.HealthThresholds)
	seen := make(map[string]struct{}, len(cfg.Platforms))
	for i := range cfg.Platforms {
		p := &cfg.Platforms[i]
		p.Platform = strings.ToLower(strings.TrimSpace(p.Platform))
		if p.Platform == "" {
			return fmt.Errorf("%w: empty platform", ErrChannelTelemetryInvalidConfig)
		}
		descriptor, ok := domain.LookupPlatform(p.Platform)
		if !ok || !descriptor.PassiveMonitor {
			return fmt.Errorf("%w: unsupported platform %s", ErrChannelTelemetryInvalidConfig, p.Platform)
		}
		if _, ok := seen[p.Platform]; ok {
			return fmt.Errorf("%w: duplicate platform %s", ErrChannelTelemetryInvalidConfig, p.Platform)
		}
		seen[p.Platform] = struct{}{}
		p.Models = normalizeStringSet(p.Models)
	}
	sort.Slice(cfg.Platforms, func(i, j int) bool { return cfg.Platforms[i].Platform < cfg.Platforms[j].Platform })
	return nil
}

// DefaultChannelTelemetryIgnoredErrorCategories are factory defaults for
// ignored_error_categories: excluded from error_rate / health scoring only.
// Operators can clear or extend via admin config.
var DefaultChannelTelemetryIgnoredErrorCategories = []string{
	"authentication",
	"client_cancelled",
	"content_policy",
	"context_limit",
	"group_access",
	"model_unsupported",
	"not_found",
	"quota_or_balance",
}

func DefaultChannelTelemetryHealthThresholds() ChannelTelemetryHealthThresholds {
	return ChannelTelemetryHealthThresholds{
		MinimumSample:     50,
		WarningErrorRate:  0.05,
		CriticalErrorRate: 0.20,
		TargetTTFTMs:      3000,
		WarningTTFTMs:     3000,
		CriticalTTFTMs:    10000,
		// A zero/zero cache threshold means cache misses do not affect health
		// until an operator explicitly configures cache scoring.
		WarningCacheRate:  0,
		CriticalCacheRate: 0,
		ErrorWeight:       0.60,
		TTFTWeight:        0.20,
		CacheWeight:       0.20,
	}
}

func NormalizeChannelTelemetryHealthThresholds(in ChannelTelemetryHealthThresholds) ChannelTelemetryHealthThresholds {
	def := DefaultChannelTelemetryHealthThresholds()
	if in.MinimumSample <= 0 {
		in.MinimumSample = def.MinimumSample
	}
	if in.MinimumSample < 1 {
		in.MinimumSample = 1
	}
	if in.MinimumSample > 10000 {
		in.MinimumSample = 10000
	}
	if in.WarningErrorRate <= 0 {
		in.WarningErrorRate = def.WarningErrorRate
	}
	if in.CriticalErrorRate <= 0 {
		in.CriticalErrorRate = def.CriticalErrorRate
	}
	if in.CriticalErrorRate < in.WarningErrorRate {
		in.CriticalErrorRate = in.WarningErrorRate
	}
	if in.TargetTTFTMs <= 0 {
		in.TargetTTFTMs = def.TargetTTFTMs
	}
	if in.WarningTTFTMs <= 0 {
		in.WarningTTFTMs = def.WarningTTFTMs
	}
	if in.WarningTTFTMs < in.TargetTTFTMs {
		in.WarningTTFTMs = in.TargetTTFTMs + 1
	}
	if in.CriticalTTFTMs <= 0 {
		in.CriticalTTFTMs = def.CriticalTTFTMs
	}
	if in.CriticalTTFTMs < in.WarningTTFTMs {
		in.CriticalTTFTMs = in.WarningTTFTMs
	}
	if in.WarningCacheRate < 0 {
		in.WarningCacheRate = 0
	}
	if in.CriticalCacheRate < 0 {
		in.CriticalCacheRate = 0
	}
	if in.WarningCacheRate > 1 {
		in.WarningCacheRate = 1
	}
	if in.CriticalCacheRate > 1 {
		in.CriticalCacheRate = 1
	}
	if in.CriticalCacheRate > in.WarningCacheRate {
		in.CriticalCacheRate = in.WarningCacheRate
	}
	if in.ErrorWeight <= 0 && in.TTFTWeight <= 0 && in.CacheWeight <= 0 {
		in.ErrorWeight, in.TTFTWeight, in.CacheWeight = def.ErrorWeight, def.TTFTWeight, def.CacheWeight
	}
	return in
}

func normalizeChannelTelemetryIgnoredCategories(values []string) []string {
	allowed := make(map[string]struct{}, len(ChannelTelemetryErrorCategories))
	for _, c := range ChannelTelemetryErrorCategories {
		allowed[c] = struct{}{}
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := allowed[value]; !ok {
			// Unknown labels are dropped so a typo cannot silently create a new category.
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// ChannelTelemetryIgnoredCategorySet returns a set for O(1) membership checks.
func ChannelTelemetryIgnoredCategorySet(cfg ChannelTelemetryConfig) map[string]struct{} {
	set := make(map[string]struct{}, len(cfg.IgnoredErrorCategories))
	for _, c := range cfg.IgnoredErrorCategories {
		set[c] = struct{}{}
	}
	return set
}

func normalizeStringSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeInt64Set(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func normalizeChannelTelemetryGroupIDs(values []int64) ([]int64, error) {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			return nil, fmt.Errorf("%w: group_ids must be positive", ErrChannelTelemetryInvalidConfig)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func ChannelTelemetryHealthFor(metrics ChannelTelemetryMetric) ChannelTelemetryHealth {
	return ChannelTelemetryHealthForWithThresholds(metrics, DefaultChannelTelemetryHealthThresholds())
}

func ChannelTelemetryHealthForWithThresholds(metrics ChannelTelemetryMetric, thresholds ChannelTelemetryHealthThresholds) ChannelTelemetryHealth {
	thresholds = NormalizeChannelTelemetryHealthThresholds(thresholds)
	result := ChannelTelemetryHealth{
		Overall: "unknown", ErrorRate: "unknown", TTFT: "unknown", Cache: "unknown",
		MinimumSample: thresholds.MinimumSample, Thresholds: thresholds,
	}

	type scored struct {
		score  float64
		weight float64
		band   string
	}
	parts := make([]scored, 0, 3)

	if metrics.RequestCount >= result.MinimumSample {
		s := errorRateScore(metrics.ErrorRate, thresholds.CriticalErrorRate)
		result.ErrorRateScore = &s
		result.ErrorRate = healthBand(metrics.ErrorRate, thresholds.WarningErrorRate, thresholds.CriticalErrorRate)
		parts = append(parts, scored{score: s, weight: thresholds.ErrorWeight, band: result.ErrorRate})
	}
	// Prefer p50 for TTFT scoring; fall back to p95 only if p50 is missing.
	if metrics.TTFT.SampleCount >= result.MinimumSample {
		var ttftMs *int64
		if metrics.TTFT.P50Ms != nil {
			ttftMs = metrics.TTFT.P50Ms
		} else if metrics.TTFT.P95Ms != nil {
			ttftMs = metrics.TTFT.P95Ms
		}
		if ttftMs != nil {
			s := ttftP50Score(float64(*ttftMs), float64(thresholds.TargetTTFTMs), float64(thresholds.CriticalTTFTMs))
			result.TTFTScore = &s
			result.TTFT = healthBand(float64(*ttftMs), float64(thresholds.WarningTTFTMs), float64(thresholds.CriticalTTFTMs))
			parts = append(parts, scored{score: s, weight: thresholds.TTFTWeight, band: result.TTFT})
		}
	}
	// Cache: need a meaningful denominator; higher rate is better.
	if metrics.CacheRateDenominator >= result.MinimumSample {
		s := cacheRateScore(metrics.CacheRate)
		if thresholds.WarningCacheRate <= 0 && thresholds.CriticalCacheRate <= 0 {
			// A zero/zero cache threshold means "do not penalize cache misses".
			s = 100
		}
		result.CacheScore = &s
		// Invert for healthBand (lower is worse): use (1 - rate) against warning/critical floors.
		result.Cache = cacheRateBand(metrics.CacheRate, thresholds.WarningCacheRate, thresholds.CriticalCacheRate)
		parts = append(parts, scored{score: s, weight: thresholds.CacheWeight, band: result.Cache})
	}

	if len(parts) == 0 {
		return result
	}
	var weightSum, scoreSum float64
	for _, p := range parts {
		weightSum += p.weight
		scoreSum += p.weight * p.score
	}
	if weightSum <= 0 {
		return result
	}
	overall := scoreSum / weightSum
	result.Score = &overall
	result.Overall = scoreBand(overall)
	return result
}

// errorRateScore maps error rate to 0–100. 0% → 100; at/above critical → 0 (linear).
func errorRateScore(errorRate, critical float64) float64 {
	if critical <= 0 {
		critical = 0.05
	}
	if errorRate <= 0 {
		return 100
	}
	if errorRate >= critical {
		return 0
	}
	return 100 * (1 - errorRate/critical)
}

// ttftP50Score maps TTFT p50 ms to 0–100.
// At/below target → 100; at/above critical → 0; linear in between.
func ttftP50Score(p50Ms, targetMs, criticalMs float64) float64 {
	if targetMs <= 0 {
		targetMs = 2500
	}
	if criticalMs <= targetMs {
		criticalMs = targetMs * 2.4
	}
	if p50Ms <= targetMs {
		return 100
	}
	if p50Ms >= criticalMs {
		return 0
	}
	return 100 * (1 - (p50Ms-targetMs)/(criticalMs-targetMs))
}

// cacheRateScore maps cache hit rate to 0–100 (higher is better, linear).
func cacheRateScore(cacheRate float64) float64 {
	if cacheRate <= 0 {
		return 0
	}
	if cacheRate >= 1 {
		return 100
	}
	return 100 * cacheRate
}

// cacheRateBand: below critical → critical; below warning → warning; else healthy.
func cacheRateBand(cacheRate, warning, critical float64) string {
	if cacheRate < critical {
		return "critical"
	}
	if cacheRate < warning {
		return "warning"
	}
	return "healthy"
}

// scoreBand maps continuous 0–100 scores to coarse labels for legacy consumers.
func scoreBand(score float64) string {
	switch {
	case score >= 80:
		return "healthy"
	case score >= 50:
		return "warning"
	default:
		return "critical"
	}
}

func healthBand(value, warning, critical float64) string {
	if value >= critical {
		return "critical"
	}
	if value >= warning {
		return "warning"
	}
	return "healthy"
}
