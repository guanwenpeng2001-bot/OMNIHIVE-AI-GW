package status

import "time"

const (
	PublishGrant = "ops.incidents.publish"

	minimumSamples = 20
	historyDays    = 90
	publicationLag = 30 * time.Second
	staleAfter     = 5 * time.Minute

	Operational = "operational"
	Degraded    = "degraded"
	Outage      = "outage"
	Maintenance = "maintenance"
	Unknown     = "unknown"
)

type componentSpec struct {
	id, name string
	measured bool
}

var components = []componentSpec{
	{"inference", "Inference gateway", true},
	{"openai_compatible", "OpenAI-Compatible Gateway", true},
	{"anthropic_compatible", "Anthropic-Compatible Gateway", true},
	{"gemini_compatible", "Gemini-Compatible Gateway", true},
	{"console_auth", "Console & Auth", false},
	{"async_workers", "Async Task Workers", false},
}

// Counts contain only aggregate, allowlisted categories. The denominator
// excludes admission denials and client cancellations.
type Counts struct {
	Succeeded           int64 `json:"succeeded,string"`
	GatewayErrors       int64 `json:"gateway_errors,string"`
	UpstreamErrors      int64 `json:"upstream_errors,string"`
	UpstreamRateLimits  int64 `json:"upstream_rate_limits,string"`
	StreamInterruptions int64 `json:"stream_interruptions,string"`
	UnknownOutcomes     int64 `json:"unknown_outcomes,string"`
	AdmissionDenials    int64 `json:"admission_denials,string"`
	ClientCancellations int64 `json:"client_cancellations,string"`
	Attempts            int64 `json:"attempts,string"`
	FirstByteTimeouts   int64 `json:"first_byte_timeouts,string"`
	StreamSamples       int64 `json:"stream_samples,string"`
	StreamSucceeded     int64 `json:"stream_succeeded,string"`
}

func (c Counts) samples() int64 {
	return c.Succeeded + c.GatewayErrors + c.UpstreamErrors + c.UpstreamRateLimits + c.StreamInterruptions + c.UnknownOutcomes
}

type Measurement struct {
	State                string   `json:"state"`
	Reason               *string  `json:"reason"`
	SampleCount          int64    `json:"sample_count,string"`
	MinimumSampleCount   int64    `json:"minimum_sample_count,string"`
	SuccessRate          *float64 `json:"success_rate"`
	TTFTSampleCount      int64    `json:"ttft_sample_count,string"`
	TTFTMedianMS         *int     `json:"ttft_p50_ms"`
	TTFTP95MS            *int     `json:"ttft_p95_ms"`
	StreamCompletionRate *float64 `json:"stream_completion_rate"`
	Counts               Counts   `json:"counts"`
}

type Daily struct {
	Date        string   `json:"date"`
	State       string   `json:"state"`
	Reason      *string  `json:"reason"`
	SampleCount int64    `json:"sample_count,string"`
	SuccessRate *float64 `json:"success_rate"`
}

type Component struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Measurement
	ObservedState string     `json:"observed_state"`
	Source        string     `json:"source"`
	Freshness     string     `json:"freshness"`
	StaleAfter    *time.Time `json:"stale_after"`
	LastSuccess   *time.Time `json:"last_success"`
	History       []Daily    `json:"history"`
}

type Definitions struct {
	SuccessRate                string   `json:"success_rate"`
	StreamCompletionRate       string   `json:"stream_completion_rate"`
	TTFT                       string   `json:"ttft"`
	ExcludedCategories         []string `json:"excluded_categories"`
	ReportedCategories         []string `json:"reported_categories"`
	AttemptsCountedSeparately  bool     `json:"attempts_counted_separately"`
	AggregationIntervalSeconds int      `json:"aggregation_interval_seconds"`
	PublicationDelaySeconds    int      `json:"publication_delay_seconds"`
}

type Summary struct {
	AsOf                time.Time         `json:"as_of"`
	WindowStart         time.Time         `json:"window_start"`
	WindowEnd           time.Time         `json:"window_end"`
	DataLagSeconds      *int64            `json:"data_lag_seconds"`
	Overall             string            `json:"overall"`
	Components          []Component       `json:"components"`
	Incidents           []IncidentSummary `json:"incidents"`
	ActiveIncidentCount int64             `json:"active_incident_count,string"`
	Definitions         Definitions       `json:"definitions"`
}

// ViewTime advances only on publication ticks so a cached representation ages
// consistently even while the collector is stopped.
func ViewTime(now time.Time) time.Time {
	return observationEnd(now).Add(publicationLag)
}

func observationEnd(now time.Time) time.Time {
	return now.UTC().Add(-publicationLag).Truncate(time.Minute)
}

func dayStart(at time.Time) time.Time { return at.UTC().Truncate(24 * time.Hour) }

func oldestDay(asOf time.Time) time.Time {
	return dayStart(asOf).AddDate(0, 0, -(historyDays - 1))
}
