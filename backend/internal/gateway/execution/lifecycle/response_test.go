package lifecycle

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestResponseLifecycle(t *testing.T) {
	for _, delivered := range []bool{true, false} {
		var r Response
		if r.State() != PreOutput {
			t.Fatal(r.State())
		}
		r.Heartbeat()
		if r.State() != HeartbeatOnly || r.ReplayBlocked() {
			t.Fatal(r.State())
		}
		r.Semantic()
		r.Heartbeat()
		if r.State() != SemanticOutput || !r.ReplayBlocked() {
			t.Fatal(r.State())
		}
		var emissions atomic.Int32
		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				if r.BeginTerminal() {
					emissions.Add(1)
					r.FinishTerminal(delivered)
				}
			})
		}
		wg.Wait()
		want := TerminalFailed
		if delivered {
			want = Terminal
		}
		if r.State() != want || emissions.Load() != 1 {
			t.Fatalf("state=%v emissions=%d", r.State(), emissions.Load())
		}
	}
}

func BenchmarkResponseLifecycle(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		var r Response
		r.Heartbeat()
		r.Semantic()
		if r.BeginTerminal() {
			r.FinishTerminal(true)
		}
	}
}
