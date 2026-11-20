package attempt

import (
	"context"
	"slices"
	"time"
)

type Action string

const (
	RetrySame Action = "retry_same_account"
	Switch    Action = "switch_account"
	Stop      Action = "stop"
	Cancel    Action = "cancel"
)

type Acceptance string

const (
	NotSent  Acceptance = "not_sent"
	Accepted Acceptance = "accepted"
	Unknown  Acceptance = "unknown"
)

const FirstOutputTimeoutAbandoned = "first_output_timeout_abandoned"

type AttemptDecision struct {
	AbandonedUncertain bool       `json:"abandoned_uncertain"`
	AttemptNumber      int        `json:"attempt_number"`
	Action             Action     `json:"action"`
	Reason             string     `json:"reason"`
	Excluded           []int64    `json:"excluded"`
	Acceptance         Acceptance `json:"acceptance"`
	Remaining          int        `json:"remaining"`
	Deadline           time.Time  `json:"deadline"`
}

type Recorder interface {
	RecordAttemptDecision(context.Context, AttemptDecision)
}

type recorderKey struct{}

func WithRecorder(ctx context.Context, recorder Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, recorder)
}

func Record(ctx context.Context, d AttemptDecision) {
	if ctx != nil {
		if recorder, ok := ctx.Value(recorderKey{}).(Recorder); ok {
			recorder.RecordAttemptDecision(ctx, d)
		}
	}
}

// Budget is a snapshot of actual sends, including provider repairs and fallback transports.
type Budget struct {
	AbandonedUncertain bool
	ReplayAvailable    bool
	Number             int
	Remaining          int
	Acceptance         Acceptance
	Deadline           time.Time
	StopReason         string
}

type Failure struct {
	AccountID  int64
	StopReason string
	Retry      RetryPolicy
}

type Controller struct {
	MaxSwitches           int
	SwitchCount           int
	FailedAccountIDs      map[int64]struct{}
	SameAccountRetryCount map[int64]int
}

func New(maxSwitches int) Controller {
	return Controller{
		MaxSwitches:           maxSwitches,
		FailedAccountIDs:      make(map[int64]struct{}),
		SameAccountRetryCount: make(map[int64]int),
	}
}

func (c *Controller) Decide(b Budget, f Failure) AttemptDecision {
	d := AttemptDecision{AbandonedUncertain: b.AbandonedUncertain, AttemptNumber: b.Number, Remaining: b.Remaining, Deadline: b.Deadline, Acceptance: b.Acceptance, Action: Stop}
	now := time.Now()
	switch {
	case b.StopReason == "canceled":
		d.Action, d.Reason = Cancel, b.StopReason
	case !b.Deadline.IsZero() && !now.Before(b.Deadline):
		d.Reason = "deadline"
	case b.StopReason == "acceptance_unknown" && b.ReplayAvailable:
		d.Reason = b.StopReason
		if b.Acceptance == Unknown && b.Remaining > 0 && f.StopReason == "" && c.SwitchCount < c.MaxSwitches {
			c.FailedAccountIDs[f.AccountID] = struct{}{}
			c.SwitchCount++
			d.Action, d.Reason = Switch, FirstOutputTimeoutAbandoned
		}
	case b.StopReason != "":
		d.Reason = b.StopReason
	case b.Acceptance == Unknown:
		d.Reason = "acceptance_unknown"
	case b.Remaining == 0:
		d.Reason = "attempt_limit"
	case f.StopReason != "":
		d.Reason = f.StopReason
	case f.Retry.Allows(c.SameAccountRetryCount[f.AccountID], now):
		c.SameAccountRetryCount[f.AccountID]++
		d.Action, d.Reason = RetrySame, "same_account_retry"
	default:
		c.FailedAccountIDs[f.AccountID] = struct{}{}
		if c.SwitchCount >= c.MaxSwitches {
			d.Reason = "switch_limit"
		} else {
			c.SwitchCount++
			d.Action, d.Reason = Switch, "account_switch"
		}
	}
	d.Excluded = make([]int64, 0, len(c.FailedAccountIDs))
	for id := range c.FailedAccountIDs {
		d.Excluded = append(d.Excluded, id)
	}
	slices.Sort(d.Excluded)
	return d
}

type RetryPolicy struct {
	Enabled    bool
	Limit      int
	ErrorLimit int
	Deadline   time.Time
	Delay      time.Duration
	Transient  bool
}

func (p RetryPolicy) Allows(count int, now time.Time) bool {
	if !p.Enabled || (!p.Deadline.IsZero() && !now.Before(p.Deadline)) {
		return false
	}
	if p.ErrorLimit > 0 {
		return count < min(p.Limit, p.ErrorLimit)
	}
	return !p.Deadline.IsZero() || count < p.Limit
}

func (p RetryPolicy) Backoff(count int) time.Duration {
	if p.Delay > 0 {
		return p.Delay
	}
	delay := 500 * time.Millisecond
	if p.Transient && count > 1 {
		delay <<= min(count-1, 4)
	}
	return delay
}
