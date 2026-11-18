package attempt

import (
	"testing"
	"time"
)

func BenchmarkAttemptDecision(b *testing.B) {
	c := New(1)
	f := Failure{AccountID: 7}
	budget := Budget{Number: 1, Remaining: 1, Acceptance: NotSent, Deadline: time.Now().Add(time.Minute)}
	b.ReportAllocs()
	for b.Loop() {
		c.SwitchCount = 0
		c.Decide(budget, f)
	}
}

func TestAttemptBudgetDecisions(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	for _, tc := range []struct {
		name   string
		budget Budget
		action Action
		reason string
	}{
		{"lost write", Budget{Number: 1, Remaining: 2, Acceptance: Unknown, Deadline: deadline}, Stop, "acceptance_unknown"},
		{"exhausted", Budget{Number: 3, Remaining: 0, Acceptance: NotSent, Deadline: deadline}, Stop, "attempt_limit"},
		{"expired", Budget{Number: 1, Remaining: 2, Acceptance: NotSent, Deadline: time.Now().Add(-time.Second)}, Stop, "deadline"},
		{"canceled", Budget{Number: 1, Remaining: 2, Acceptance: Accepted, Deadline: deadline, StopReason: "canceled"}, Cancel, "canceled"},
		{"output", Budget{Number: 1, Remaining: 2, Acceptance: Accepted, Deadline: deadline, StopReason: "response_committed"}, Stop, "response_committed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(2)
			d := c.Decide(tc.budget, Failure{AccountID: 3, Retry: RetryPolicy{Enabled: true, Limit: 3}})
			if d.Action != tc.action || d.Reason != tc.reason || c.SwitchCount != 0 || len(d.Excluded) != 0 {
				t.Fatalf("decision = %+v", d)
			}
		})
	}
}

func TestAttemptSameAccountThenSwitch(t *testing.T) {
	c := New(1)
	f := Failure{AccountID: 42, Retry: RetryPolicy{Enabled: true, Limit: 1}}
	b := Budget{Number: 1, Remaining: 2, Acceptance: NotSent}
	d := c.Decide(b, f)
	if d.Action != RetrySame || len(d.Excluded) != 0 || c.SwitchCount != 0 {
		t.Fatal(d)
	}
	b.Number, b.Remaining = 2, 1
	d = c.Decide(b, f)
	if d.Action != Switch || len(d.Excluded) != 1 || d.Excluded[0] != 42 || c.SwitchCount != 1 {
		t.Fatal(d)
	}
	c.FailedAccountIDs[13] = struct{}{}
	if len(d.Excluded) != 1 {
		t.Fatal("decision changed after reselection")
	}
}
