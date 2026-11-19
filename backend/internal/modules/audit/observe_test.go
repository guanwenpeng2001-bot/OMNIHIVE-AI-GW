package audit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type probeTracer struct {
	trace.Tracer
	got chan trace.SpanConfig
}

func (t probeTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if baggage.FromContext(ctx).Len() != 0 || name != "audit.request" {
		panic("unexpected tracing context")
	}
	t.got <- trace.NewSpanStartConfig(opts...)
	return t.Tracer.Start(ctx, name, opts...)
}
func TestObserverDropsWithoutWaitingAndExportsOnlyFixedLabels(t *testing.T) {
	o := NewObserver()
	for i := 0; i < cap(o.queue); i++ {
		require.True(t, o.Offer(Observation{At: time.Now(), Success: true}))
	}
	require.False(t, o.Offer(Observation{}))
	require.EqualValues(t, 1, o.Dropped())
	got := make(chan trace.SpanConfig, cap(o.queue))
	o.tracer = probeTracer{noop.NewTracerProvider().Tracer("test"), got}
	b, _ := baggage.Parse("token=canary")
	ctx, cancel := context.WithCancel(baggage.ContextWithBaggage(t.Context(), b))
	done := make(chan struct{})
	go func() { defer close(done); o.Run(ctx, nil, nil) }()
	select {
	case cfg := <-got:
		require.True(t, cfg.NewRoot())
		attrs := cfg.Attributes()
		require.Len(t, attrs, 2)
		require.Equal(t, "scope.read", attrs[0].Value.AsString())
		require.True(t, attrs[1].Value.AsBool())
	case <-time.After(time.Second):
		t.Fatal("no observation")
	}
	cancel()
	<-done
}
