package status

import (
	"math"
	"time"
)

// A mergeable histogram lets every window use its own sample distribution.
// The open-ended bucket has no numeric upper bound.
var ttftBucketMax = [...]int{50, 100, 200, 400, 800, 1600, 3200, 6400, 12800, 30000, 60000}

type bucket struct {
	Counts
	ttft         []int64
	lastObserved *time.Time
	lastSuccess  *time.Time
}

func (b bucket) percentile(n int64, fraction float64) *int {
	rank := int64(math.Ceil(fraction * float64(n)))
	for i, upper := range ttftBucketMax {
		rank -= b.ttft[i]
		if rank <= 0 {
			return &upper
		}
	}
	return nil
}

func measure(b bucket, reason string) Measurement {
	out := Measurement{
		Counts: b.Counts, State: Unknown, SampleCount: b.samples(), MinimumSampleCount: minimumSamples,
	}
	for _, n := range b.ttft {
		out.TTFTSampleCount += n
	}
	switch {
	case reason != "":
	case out.SampleCount < minimumSamples:
		reason = "INSUFFICIENT_SAMPLES"
	case b.UnknownOutcomes > 0:
		reason = "OUTCOME_UNKNOWN"
	default:
		rate := 100 * float64(b.Succeeded) / float64(out.SampleCount)
		out.SuccessRate = &rate
		switch {
		case rate < 90:
			out.State = Outage
		case rate < 99:
			out.State = Degraded
		default:
			out.State = Operational
		}
		if out.TTFTSampleCount >= minimumSamples {
			out.TTFTMedianMS = b.percentile(out.TTFTSampleCount, 0.50)
			out.TTFTP95MS = b.percentile(out.TTFTSampleCount, 0.95)
		}
		if b.StreamSamples >= minimumSamples {
			rate := 100 * float64(b.StreamSucceeded) / float64(b.StreamSamples)
			out.StreamCompletionRate = &rate
		}
	}
	if reason != "" {
		out.Reason = &reason
	}
	return out
}

func freshness(b bucket, observed, view time.Time) (state string, staleAt *time.Time) {
	if observed.IsZero() {
		return Unknown, nil
	}
	deadline := observed.Add(staleAfter)
	if b.lastObserved != nil {
		deadline = b.lastObserved.Add(staleAfter)
		if !view.Before(deadline) {
			return "stale", &deadline
		}
	}
	if !view.Before(deadline) {
		return "stale", &deadline
	}
	if b.lastObserved == nil {
		return Unknown, nil
	}
	return "fresh", &deadline
}

func worse(a, b string) string {
	rank := func(s string) int {
		switch s {
		case Outage:
			return 4
		case Degraded:
			return 3
		case Maintenance:
			return 2
		case Unknown:
			return 1
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}
