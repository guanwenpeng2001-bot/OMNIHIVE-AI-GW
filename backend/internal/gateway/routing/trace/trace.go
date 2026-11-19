package trace

import (
	"context"
	"time"
)

type Mode string

const (
	Execution  Mode = "execution"
	Simulation Mode = "simulation"
)

type Origin struct {
	Constraint string `json:"constraint"`
	ScopeType  string `json:"scope_type"`
	ScopeID    string `json:"scope_id"`
	Version    string `json:"version"`
}

type Reason struct {
	Code   string `json:"code"`
	Stage  string `json:"stage"`
	Origin string `json:"origin,omitempty"`
}

type Factor struct {
	Name          string     `json:"name"`
	Value         string     `json:"value"`
	Unit          string     `json:"unit"`
	Source        string     `json:"source"`
	Freshness     string     `json:"freshness"`
	WindowSeconds *int       `json:"window_seconds"`
	Samples       *int64     `json:"samples,string"`
	ObservedAt    *time.Time `json:"observed_at"`
}

type Stage struct {
	Name           string `json:"name"`
	InputCount     int    `json:"input_count,string"`
	RemainingCount int    `json:"remaining_count,string"`
}

type Candidate struct {
	ID      string   `json:"id"`
	State   string   `json:"state"`
	Rank    *int     `json:"rank"`
	Reasons []Reason `json:"reasons"`
	Factors []Factor `json:"factors"`
}

type Selection struct {
	ModelID       string `json:"model_id"`
	CandidateID   string `json:"candidate_id"`
	FundingSource string `json:"funding_source"`
}

type DecisionTrace struct {
	SchemaVersion    int         `json:"schema_version"`
	Mode             Mode        `json:"mode"`
	SnapshotID       string      `json:"snapshot_id"`
	AsOf             time.Time   `json:"as_of"`
	PolicyVersion    string      `json:"policy_version"`
	CatalogVersion   string      `json:"catalog_version"`
	PriceVersion     string      `json:"price_version"`
	Seed             uint64      `json:"seed,string"`
	Origins          []Origin    `json:"origins"`
	Stages           []Stage     `json:"stages"`
	Candidates       []Candidate `json:"candidates"`
	Selected         *Selection  `json:"selected"`
	Uncertainties    []string    `json:"uncertainties"`
	Redactions       []string    `json:"redactions"`
	ExecutionOutcome string      `json:"execution_outcome,omitempty"`
	SelectionLayer   string      `json:"selection_layer,omitempty"`
}

type sinkKey struct{}

type Sink func(context.Context, DecisionTrace)

func WithSink(ctx context.Context, sink Sink) context.Context {
	return context.WithValue(ctx, sinkKey{}, sink)
}

func Emit(ctx context.Context, decision DecisionTrace) {
	if sink, ok := ctx.Value(sinkKey{}).(Sink); ok && sink != nil {
		sink(ctx, decision)
	}
}
