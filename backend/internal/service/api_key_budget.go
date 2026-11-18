package service

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

// API Key period-budget modes.
//
// The period budget is deliberately separate from the legacy USD windows
// (rate_limit_5h/_1d/_7d) and from RPM/TPM: those keep their existing columns
// and semantics. A key without a period budget behaves exactly as before.
const (
	APIKeyBudgetModeOff      = "off"
	APIKeyBudgetModeDuration = "duration"
	APIKeyBudgetModeCalendar = "calendar"
)

// Duration-mode start alignment.
const (
	// APIKeyBudgetAnchorFirstUse starts a new window when the first request of
	// the window arrives. This is the legacy 5h behaviour.
	APIKeyBudgetAnchorFirstUse = "first_use"
	// APIKeyBudgetAnchorDay aligns the window start to midnight in the policy
	// timezone of the day the window starts. This is the legacy 1d/7d behaviour
	// (with UTC) and keeps those anchors bit-compatible.
	APIKeyBudgetAnchorDay = "day"
)

// Calendar-mode units.
const (
	APIKeyBudgetCalendarDay   = "day"
	APIKeyBudgetCalendarWeek  = "week"
	APIKeyBudgetCalendarMonth = "month"
)

// Legacy anchor names. They stay usable and are reported by name so the panel
// can show the exact window the user configured.
const (
	APIKeyLegacyBudgetAnchor5h = "5h"
	APIKeyLegacyBudgetAnchor1d = "1d"
	APIKeyLegacyBudgetAnchor7d = "7d"
)

// Budget errors.
var (
	ErrAPIKeyBudgetExceeded = infraerrors.TooManyRequests(
		"API_KEY_BUDGET_EXCEEDED",
		"api key period budget exhausted",
	)
	ErrAPIKeyBudgetInsufficientBalance = infraerrors.TooManyRequests(
		"INSUFFICIENT_BALANCE",
		"insufficient account balance for the estimated request cost",
	)
	ErrAPIKeyBudgetEnforced = infraerrors.Forbidden(
		"API_KEY_BUDGET_ENFORCED",
		"this api key budget is enforced by the platform and cannot be changed or cleared",
	)
	ErrAPIKeyUsageResetForbidden = infraerrors.Forbidden(
		"API_KEY_USAGE_RESET_FORBIDDEN",
		"consumed api key usage cannot be reset from the panel; ask an administrator for an audited adjustment",
	)
	ErrAPIKeyBudgetInvalid = infraerrors.BadRequest(
		"API_KEY_BUDGET_INVALID",
		"invalid api key period budget",
	)
	ErrAPIKeyBudgetTimezoneInvalid = infraerrors.BadRequest(
		"API_KEY_BUDGET_TIMEZONE_INVALID",
		"invalid IANA timezone for the api key period budget",
	)
	ErrAPIKeyBudgetUnavailable = infraerrors.ServiceUnavailable(
		"API_KEY_BUDGET_UNAVAILABLE",
		"api key period budget enforcement is unavailable",
	)
)

// APIKeyBudgetPolicy is the period-budget configuration of one API key.
type APIKeyBudgetPolicy struct {
	Mode            string  `json:"mode"`
	LimitUSD        float64 `json:"limit_usd"`
	DurationSeconds int64   `json:"duration_seconds,omitempty"`
	Anchor          string  `json:"anchor,omitempty"`
	CalendarUnit    string  `json:"calendar_unit,omitempty"`
	Timezone        string  `json:"timezone,omitempty"`
	// Enforced marks a platform-mandated budget. The key owner can never edit,
	// clear or zero it; only an audited administrative adjustment can move it.
	Enforced bool `json:"enforced"`
}

// IsActive reports whether the policy actually bounds spend.
func (p APIKeyBudgetPolicy) IsActive() bool {
	return p.normalizedMode() != APIKeyBudgetModeOff && p.LimitUSD > 0
}

func (p APIKeyBudgetPolicy) normalizedMode() string {
	mode := strings.ToLower(strings.TrimSpace(p.Mode))
	if mode == "" {
		return APIKeyBudgetModeOff
	}
	return mode
}

// Location resolves the policy timezone, defaulting to UTC.
func (p APIKeyBudgetPolicy) Location() *time.Location {
	name := strings.TrimSpace(p.Timezone)
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// EffectiveTimezoneName is the IANA name actually used for boundaries.
func (p APIKeyBudgetPolicy) EffectiveTimezoneName() string {
	name := strings.TrimSpace(p.Timezone)
	if name == "" {
		return "UTC"
	}
	if _, err := time.LoadLocation(name); err != nil {
		return "UTC"
	}
	return name
}

// Normalize fills mode-specific defaults and returns the canonical policy.
func (p APIKeyBudgetPolicy) Normalize() APIKeyBudgetPolicy {
	p.Mode = p.normalizedMode()
	p.Timezone = p.EffectiveTimezoneName()
	switch p.Mode {
	case APIKeyBudgetModeDuration:
		if p.Anchor != APIKeyBudgetAnchorDay {
			p.Anchor = APIKeyBudgetAnchorFirstUse
		}
		p.CalendarUnit = ""
	case APIKeyBudgetModeCalendar:
		switch strings.ToLower(strings.TrimSpace(p.CalendarUnit)) {
		case APIKeyBudgetCalendarWeek:
			p.CalendarUnit = APIKeyBudgetCalendarWeek
		case APIKeyBudgetCalendarMonth:
			p.CalendarUnit = APIKeyBudgetCalendarMonth
		default:
			p.CalendarUnit = APIKeyBudgetCalendarDay
		}
		p.DurationSeconds = 0
		p.Anchor = ""
	default:
		p.Mode = APIKeyBudgetModeOff
		p.DurationSeconds = 0
		p.Anchor = ""
		p.CalendarUnit = ""
	}
	if p.Mode == APIKeyBudgetModeOff {
		p.LimitUSD = 0
		p.Enforced = false
	}
	return p
}

// Validate rejects policies the period engine cannot honour.
func (p APIKeyBudgetPolicy) Validate() error {
	switch strings.ToLower(strings.TrimSpace(p.Mode)) {
	case "", APIKeyBudgetModeOff, APIKeyBudgetModeDuration, APIKeyBudgetModeCalendar:
	default:
		return fmt.Errorf("%w: unknown period mode %q", ErrAPIKeyBudgetInvalid, p.Mode)
	}
	policy := p.Normalize()
	if math.IsNaN(policy.LimitUSD) || math.IsInf(policy.LimitUSD, 0) || policy.LimitUSD < 0 {
		return fmt.Errorf("%w: limit must be finite and non-negative", ErrAPIKeyBudgetInvalid)
	}
	switch policy.Mode {
	case APIKeyBudgetModeOff:
		return nil
	case APIKeyBudgetModeDuration:
		if policy.DurationSeconds <= 0 {
			return fmt.Errorf("%w: duration period requires duration_seconds > 0", ErrAPIKeyBudgetInvalid)
		}
		if policy.DurationSeconds > int64((366*24*time.Hour)/time.Second) {
			return fmt.Errorf("%w: duration must not exceed 366 days", ErrAPIKeyBudgetInvalid)
		}
	case APIKeyBudgetModeCalendar:
		if policy.CalendarUnit == "" {
			return fmt.Errorf("%w: calendar period requires a unit", ErrAPIKeyBudgetInvalid)
		}
	}
	if policy.LimitUSD <= 0 {
		return fmt.Errorf("%w: active period budget requires limit_usd > 0", ErrAPIKeyBudgetInvalid)
	}
	if strings.TrimSpace(p.Timezone) != "" {
		if _, err := time.LoadLocation(strings.TrimSpace(p.Timezone)); err != nil {
			return fmt.Errorf("%w: %q", ErrAPIKeyBudgetTimezoneInvalid, strings.TrimSpace(p.Timezone))
		}
	}
	return nil
}

// PeriodBounds returns the window that contains now. currentStart is the
// persisted window start (nil when the key never had a window). rolled reports
// that the persisted window is stale, so the caller must freeze it and start a
// new one. The returned instants are UTC.
func (p APIKeyBudgetPolicy) PeriodBounds(now time.Time, currentStart *time.Time) (start, end time.Time, rolled bool) {
	policy := p.Normalize()
	loc := policy.Location()
	now = now.UTC()
	switch policy.Mode {
	case APIKeyBudgetModeDuration:
		duration := time.Duration(policy.DurationSeconds) * time.Second
		if duration <= 0 {
			return time.Time{}, time.Time{}, false
		}
		if currentStart != nil && !currentStart.IsZero() {
			existingStart := currentStart.UTC()
			existingEnd := existingStart.Add(duration)
			if now.Before(existingEnd) {
				return existingStart, existingEnd, false
			}
		}
		start = policy.durationAnchorStart(now, loc)
		return start, start.Add(duration), true
	case APIKeyBudgetModeCalendar:
		windowStart, windowEnd := calendarBudgetWindow(now.In(loc), policy.CalendarUnit, loc)
		start, end = windowStart.UTC(), windowEnd.UTC()
		if currentStart != nil && !currentStart.IsZero() && currentStart.UTC().Equal(start) {
			return start, end, false
		}
		return start, end, true
	default:
		return time.Time{}, time.Time{}, false
	}
}

// durationAnchorStart aligns a fresh duration window.
func (p APIKeyBudgetPolicy) durationAnchorStart(now time.Time, loc *time.Location) time.Time {
	if p.Anchor == APIKeyBudgetAnchorDay {
		local := now.In(loc)
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).UTC()
	}
	return now.UTC()
}

// calendarBudgetWindow returns the calendar window containing localNow.
// Weeks start on Monday, matching the calendar unit users expect.
func calendarBudgetWindow(localNow time.Time, unit string, loc *time.Location) (time.Time, time.Time) {
	dayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	switch unit {
	case APIKeyBudgetCalendarWeek:
		// Go's Weekday() is Sunday=0; shift so Monday is the first day.
		offset := (int(localNow.Weekday()) + 6) % 7
		start := dayStart.AddDate(0, 0, -offset)
		return start, start.AddDate(0, 0, 7)
	case APIKeyBudgetCalendarMonth:
		start := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, 0)
	default:
		return dayStart, dayStart.AddDate(0, 0, 1)
	}
}

// LegacyBudgetAnchorPreset maps one of the historical 5h/1d/7d USD windows to
// an explicit period-budget policy. The anchor name is retained so the panel can
// show "5h"/"1d"/"7d" instead of a generic duration.
type LegacyBudgetAnchorPreset struct {
	Anchor string
	Label  string
	Policy APIKeyBudgetPolicy
}

// LegacyAPIKeyBudgetPresets lists the compatible anchors in ascending duration.
func LegacyAPIKeyBudgetPresets() []LegacyBudgetAnchorPreset {
	return []LegacyBudgetAnchorPreset{
		{
			Anchor: APIKeyLegacyBudgetAnchor5h,
			Label:  "5h",
			Policy: APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, DurationSeconds: int64((5 * time.Hour) / time.Second), Anchor: APIKeyBudgetAnchorFirstUse, Timezone: "UTC"},
		},
		{
			Anchor: APIKeyLegacyBudgetAnchor1d,
			Label:  "1d",
			Policy: APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, DurationSeconds: int64((24 * time.Hour) / time.Second), Anchor: APIKeyBudgetAnchorDay, Timezone: "UTC"},
		},
		{
			Anchor: APIKeyLegacyBudgetAnchor7d,
			Label:  "7d",
			Policy: APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, DurationSeconds: int64((7 * 24 * time.Hour) / time.Second), Anchor: APIKeyBudgetAnchorDay, Timezone: "UTC"},
		},
	}
}

// LegacyAPIKeyBudgetPolicy resolves a legacy anchor name to its policy.
func LegacyAPIKeyBudgetPolicy(anchor string, limitUSD float64, enforced bool) (APIKeyBudgetPolicy, bool) {
	name := strings.ToLower(strings.TrimSpace(anchor))
	for _, preset := range LegacyAPIKeyBudgetPresets() {
		if preset.Anchor == name {
			policy := preset.Policy
			policy.LimitUSD = limitUSD
			policy.Enforced = enforced
			return policy, true
		}
	}
	return APIKeyBudgetPolicy{}, false
}

// LegacyBudgetAnchorForPolicy returns the historical anchor name when the policy
// is exactly one of the compatible presets, so the API can keep reporting
// "5h"/"1d"/"7d" instead of a raw duration.
func LegacyBudgetAnchorForPolicy(policy APIKeyBudgetPolicy) string {
	policy = policy.Normalize()
	if policy.Mode != APIKeyBudgetModeDuration {
		return ""
	}
	for _, preset := range LegacyAPIKeyBudgetPresets() {
		candidate := preset.Policy.Normalize()
		if policy.DurationSeconds == candidate.DurationSeconds &&
			policy.Anchor == candidate.Anchor &&
			policy.EffectiveTimezoneName() == candidate.Timezone {
			return preset.Anchor
		}
	}
	return ""
}

// DescribeAPIKeyBudgetPolicy renders the period for API/UI display. Legacy
// anchors keep their historical label; other policies describe themselves.
func DescribeAPIKeyBudgetPolicy(policy APIKeyBudgetPolicy) string {
	policy = policy.Normalize()
	if policy.Mode == APIKeyBudgetModeOff || policy.LimitUSD <= 0 {
		return ""
	}
	if anchor := LegacyBudgetAnchorForPolicy(policy); anchor != "" {
		return anchor + " rolling window"
	}
	switch policy.Mode {
	case APIKeyBudgetModeDuration:
		duration := time.Duration(policy.DurationSeconds) * time.Second
		alignment := "rolling from first use"
		if policy.Anchor == APIKeyBudgetAnchorDay {
			alignment = "aligned to the day boundary in " + policy.EffectiveTimezoneName()
		}
		return fmt.Sprintf("every %s, %s", humanizeBudgetDuration(duration), alignment)
	case APIKeyBudgetModeCalendar:
		return fmt.Sprintf("calendar %s in %s", policy.CalendarUnit, policy.EffectiveTimezoneName())
	default:
		return ""
	}
}

func humanizeBudgetDuration(duration time.Duration) string {
	switch {
	case duration%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", int64(duration/(24*time.Hour)))
	case duration%time.Hour == 0:
		return fmt.Sprintf("%dh", int64(duration/time.Hour))
	case duration%time.Minute == 0:
		return fmt.Sprintf("%dm", int64(duration/time.Minute))
	default:
		return fmt.Sprintf("%ds", int64(duration/time.Second))
	}
}

// APIKeyBudgetState is the display projection of a key's period budget.
type APIKeyBudgetState struct {
	Policy        APIKeyBudgetPolicy `json:"policy"`
	PeriodStart   *time.Time         `json:"period_start,omitempty"`
	PeriodEnd     *time.Time         `json:"period_end,omitempty"`
	LimitUSD      float64            `json:"limit_usd"`
	UsedUSD       float64            `json:"used_usd"`
	ReservedUSD   float64            `json:"reserved_usd"`
	OverrunUSD    float64            `json:"overrun_usd"`
	RemainingUSD  float64            `json:"remaining_usd"`
	LegacyAnchor  string             `json:"legacy_anchor,omitempty"`
	PeriodDisplay string             `json:"period_display,omitempty"`
}

// RemainingAfterReservations is the spend still available to new requests.
func (s APIKeyBudgetState) RemainingAfterReservations() float64 {
	if s.LimitUSD <= 0 {
		return -1
	}
	remaining := s.LimitUSD - s.UsedUSD - s.ReservedUSD
	if remaining < 0 {
		return 0
	}
	return remaining
}

// NewAPIKeyBudgetState builds the display projection from a policy and counters.
func NewAPIKeyBudgetState(policy APIKeyBudgetPolicy, periodStart, periodEnd *time.Time, used, reserved, overrun float64) APIKeyBudgetState {
	policy = policy.Normalize()
	state := APIKeyBudgetState{
		Policy:        policy,
		PeriodStart:   periodStart,
		PeriodEnd:     periodEnd,
		LimitUSD:      policy.LimitUSD,
		UsedUSD:       used,
		ReservedUSD:   reserved,
		OverrunUSD:    overrun,
		LegacyAnchor:  LegacyBudgetAnchorForPolicy(policy),
		PeriodDisplay: DescribeAPIKeyBudgetPolicy(policy),
	}
	state.RemainingUSD = state.RemainingAfterReservations()
	return state
}

// SortAPIKeyBudgetPresets is a stable helper for UI payloads.
func SortAPIKeyBudgetPresets(presets []LegacyBudgetAnchorPreset) []LegacyBudgetAnchorPreset {
	out := append([]LegacyBudgetAnchorPreset(nil), presets...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Policy.DurationSeconds < out[j].Policy.DurationSeconds
	})
	return out
}
