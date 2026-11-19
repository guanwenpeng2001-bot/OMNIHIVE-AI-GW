package audit

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Observation intentionally carries no request context, baggage, URLs, headers,
// errors, free-form labels or identifiers supplied by a client.
type Observation struct {
	Scope          Scope
	Write, Success bool
	At             time.Time
	Duration       time.Duration
}
type Observer struct {
	queue   chan Observation
	dropped atomic.Uint64
	tracer  trace.Tracer
	latency metric.Float64Histogram
}

func NewObserver() *Observer {
	const name = "omnihive.audit"
	latency, _ := otel.Meter(name).Float64Histogram("audit.request.duration", metric.WithUnit("s"))
	return &Observer{queue: make(chan Observation, 256), tracer: otel.Tracer(name), latency: latency}
}
func (o *Observer) Offer(v Observation) bool {
	select {
	case o.queue <- v:
		return true
	default:
		o.dropped.Add(1)
		return false
	}
}
func (o *Observer) Dropped() uint64 { return o.dropped.Load() }
func (o *Observer) Run(ctx context.Context, store *Store, onError func()) {
	o.RunWithExport(ctx, store, onError, nil)
}

// RunWithExport forwards safe observations to a nonblocking workspace export sink.
func (o *Observer) RunWithExport(ctx context.Context, store *Store, onError func(), offer func(Observation) bool) {
	for {
		select {
		case <-ctx.Done():
			return
		case v := <-o.queue:
			if offer != nil {
				offer(v)
			}
			action := "scope.read"
			if v.Write {
				action = "scope.write"
			}
			attrs := []attribute.KeyValue{attribute.String("operation", action), attribute.Bool("success", v.Success)}
			// Fresh root also excludes request-scoped tracestate and propagation baggage.
			root := context.Background()
			_, span := o.tracer.Start(root, "audit.request", trace.WithNewRoot(), trace.WithTimestamp(v.At), trace.WithAttributes(attrs...))
			span.End(trace.WithTimestamp(v.At.Add(v.Duration)))
			o.latency.Record(root, v.Duration.Seconds(), metric.WithAttributes(attrs...))
			if v.Success && store != nil {
				persist, cancel := context.WithTimeout(ctx, 2*time.Second)
				_, err := store.db.ExecContext(persist, `INSERT INTO scoped_audit_events(workspace_id,project_id,actor_id,resource,action,effect,at)
  VALUES($1,$2,$3,'project',$4,'observed',$5)`, v.Scope.Workspace, v.Scope.Project, v.Scope.Actor, action, v.At)
				cancel()
				if err != nil && onError != nil {
					onError()
				}
			}
		}
	}
}
