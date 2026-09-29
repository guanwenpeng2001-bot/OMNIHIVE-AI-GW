package service

import "sync"

// OpsDiagnosticsSnapshot is process-local, lifetime telemetry, not a filtered
// dashboard window or a billing ledger. No caller-controlled labels are retained.
// Error events are NOT attempts: only an actual send site may start an attempt.
type OpsDiagnosticsSnapshot struct {
	// Send and parser telemetry is not installed. Omit its counters so
	// missing evidence cannot look like healthy zeroes.
	AttemptCoverage               string        `json:"attempt_coverage"`
	RawUsageCoverage              string        `json:"raw_usage_coverage"`
	BillingAdjustmentCoverage     string        `json:"billing_adjustment_coverage"`
	Scope                         string        `json:"scope"`
	IngressStarted                uint64        `json:"ingress_started"`
	IngressCompleted              uint64        `json:"ingress_completed"`
	WSTurns                       uint64        `json:"ws_turns"`
	OpenAISelections              uint64        `json:"openai_selections"`
	UpstreamErrorEvents           uint64        `json:"upstream_error_events"`
	UpstreamHTTPErrorEvents       uint64        `json:"upstream_http_error_events"`
	UpstreamTransportErrorEvents  uint64        `json:"upstream_transport_error_events"`
	SuppressedUpstreamEvents      uint64        `json:"suppressed_upstream_events"`
	SuppressedRuleFailures        uint64        `json:"suppressed_rule_failures"`
	SuppressedSettingsFailures    uint64        `json:"suppressed_settings_failures"`
	BillingAdjustmentObservations uint64        `json:"billing_adjustment_observations"`
	BillingInputCacheUsage        OpsCacheUsage `json:"billing_input_cache_usage"`
	BillingAdjustedCacheUsage     OpsCacheUsage `json:"billing_adjusted_cache_usage"`
}

// OpsCacheUsage contains token categories observed at billing entry and after
// its policy adjustments; the input may already include response-side overrides.
// Input inclusion semantics remain provider-specific; these
// totals must not be used to infer a cross-provider cache hit ratio or charge.
type OpsCacheUsage struct {
	Input      int64 `json:"input"`
	Read       int64 `json:"read"`
	Creation   int64 `json:"creation"`
	Creation5m int64 `json:"creation_5m"`
	Creation1h int64 `json:"creation_1h"`
}

var opsDiagnostics struct {
	sync.Mutex
	value OpsDiagnosticsSnapshot
}

func SnapshotOpsDiagnostics() OpsDiagnosticsSnapshot {
	opsDiagnostics.Lock()
	defer opsDiagnostics.Unlock()
	v := opsDiagnostics.value
	v.BillingAdjustmentCoverage = "gateway_record_usage_core_only"
	v.Scope = "process_lifetime_global"
	v.AttemptCoverage = "not_instrumented"
	v.RawUsageCoverage = "not_instrumented"
	return v
}

func updateOpsDiagnostics(f func(*OpsDiagnosticsSnapshot)) {
	opsDiagnostics.Lock()
	defer opsDiagnostics.Unlock()
	f(&opsDiagnostics.value)
}

func BeginOpsIngress() func() {
	updateOpsDiagnostics(func(v *OpsDiagnosticsSnapshot) { v.IngressStarted++ })
	var once sync.Once
	return func() {
		once.Do(func() { updateOpsDiagnostics(func(v *OpsDiagnosticsSnapshot) { v.IngressCompleted++ }) })
	}
}

func RecordOpsSuppressedFailure(rule bool) {
	updateOpsDiagnostics(func(v *OpsDiagnosticsSnapshot) {
		if rule {
			v.SuppressedRuleFailures++
		} else {
			v.SuppressedSettingsFailures++
		}
	})
}

func recordOpsUpstreamEvent(event OpsUpstreamErrorEvent) {
	updateOpsDiagnostics(func(v *OpsDiagnosticsSnapshot) {
		v.UpstreamErrorEvents++
		if event.SkipMonitoring {
			v.SuppressedUpstreamEvents++
		}
		// Credential acquisition and local policy failures are not generation
		// transport failures. Retry summaries also must not count as sends.
		if event.Stage == string(GatewayFailureStageAccountAuth) {
			return
		}
		switch {
		case event.UpstreamStatusCode >= 400:
			v.UpstreamHTTPErrorEvents++
		case event.Kind == "request_error":
			v.UpstreamTransportErrorEvents++
		}
	})
}

// Incoming billing tokens may already include response-side TTL overrides.
// Deliberately keep these observations separate from provider-raw usage.
func RecordOpsBillingCacheAdjustment(before, after OpsCacheUsage) {
	updateOpsDiagnostics(func(v *OpsDiagnosticsSnapshot) {
		v.BillingAdjustmentObservations++
		addOpsCacheUsage(&v.BillingInputCacheUsage, before)
		addOpsCacheUsage(&v.BillingAdjustedCacheUsage, after)
	})
}

func addOpsCacheUsage(dst *OpsCacheUsage, src OpsCacheUsage) {
	dst.Input += src.Input
	dst.Read += src.Read
	dst.Creation += src.Creation
	dst.Creation5m += src.Creation5m
	dst.Creation1h += src.Creation1h
}

func opsClaudeCacheUsage(usage ClaudeUsage) OpsCacheUsage {
	return OpsCacheUsage{Input: int64(usage.InputTokens), Read: int64(usage.CacheReadInputTokens),
		Creation: int64(usage.CacheCreationInputTokens), Creation5m: int64(usage.CacheCreation5mTokens), Creation1h: int64(usage.CacheCreation1hTokens)}
}
