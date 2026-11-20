package catalog

import "time"

// Compatibility is reviewed, endpoint-specific evidence for an already visible model.
type Compatibility struct {
	Action         string                 `json:"action"`
	Protocol       string                 `json:"protocol"`
	AccountMode    string                 `json:"account_mode"`
	AdapterVersion string                 `json:"adapter_version"`
	Maturity       string                 `json:"maturity"`
	Version        int64                  `json:"publication_version,string"`
	PublishedAt    time.Time              `json:"published_at"`
	Evidence       *CompatibilityEvidence `json:"evidence"`
	Deprecation    *DeprecationNotice     `json:"deprecation"`
}

type CompatibilityEvidence struct {
	ID          string     `json:"evidence_id"`
	Source      string     `json:"source"`
	State       string     `json:"state"`
	Freshness   string     `json:"freshness"`
	SampleCount int        `json:"sample_count,string"`
	WindowStart time.Time  `json:"window_start"`
	ObservedAt  time.Time  `json:"observed_at"`
	VerifiedAt  *time.Time `json:"verified_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	FixtureID   *string    `json:"fixture_id"`
}

func (e CompatibilityEvidence) At(now time.Time) CompatibilityEvidence {
	e.Freshness = "unknown"
	if !now.Before(e.ExpiresAt) {
		e.Freshness = "stale"
	} else if e.State != "unknown" && !now.Before(e.ObservedAt) {
		e.Freshness = "fresh"
	}
	if e.State == "success" {
		e.VerifiedAt = &e.ObservedAt
	}
	return e
}

type DeprecationNotice struct {
	Reason      string    `json:"reason"`
	AnnouncedAt time.Time `json:"announced_at"`
	SunsetAt    time.Time `json:"sunset_at"`
}
