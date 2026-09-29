package service

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
)

type ConfigurationAuditActor struct {
	UserID    int64  `json:"user_id"`
	Role      string `json:"role"`
	RequestID string `json:"request_id"`
}

type configurationAuditActorKey struct{}

func WithConfigurationAuditActor(ctx context.Context, actor ConfigurationAuditActor) context.Context {
	return context.WithValue(ctx, configurationAuditActorKey{}, actor)
}

func ConfigurationActor(ctx context.Context) ConfigurationAuditActor {
	actor, _ := ctx.Value(configurationAuditActorKey{}).(ConfigurationAuditActor)
	return actor
}

// ConfigurationFieldDiff records presence and change, never values. This also
// protects arbitrary provider-specific fields and secrets pasted into free text.
// Resource identity, actor and revision are recorded separately by the writer.
type ConfigurationFieldDiff struct {
	Field     string `json:"field"`
	Operation string `json:"operation"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
}

func ConfigurationDiff(before, after map[string]any) []ConfigurationFieldDiff {
	fields := make(map[string]struct{}, len(before)+len(after))
	for field := range before {
		fields[field] = struct{}{}
	}
	for field := range after {
		fields[field] = struct{}{}
	}
	names := make([]string, 0, len(fields))
	for field := range fields {
		names = append(names, field)
	}
	sort.Strings(names)
	changes := make([]ConfigurationFieldDiff, 0)
	for _, field := range names {
		previous, existed := before[field]
		current, exists := after[field]
		if existed == exists && reflect.DeepEqual(previous, current) {
			continue
		}
		change := ConfigurationFieldDiff{Field: field, Operation: "replace"}
		if existed {
			change.Before = "[redacted]"
		} else {
			change.Operation = "add"
		}
		if exists {
			change.After = "[redacted]"
		} else {
			change.Operation = "remove"
		}
		changes = append(changes, change)
	}
	return changes
}

// SubmittedConfigurationDiff is additive request evidence, not a fabricated
// before/after database snapshot. A successful request can be an idempotent
// no-op; committed resource mutations supply their own versioned diff records.
func SubmittedConfigurationDiff(raw []byte) []ConfigurationFieldDiff {
	var submitted map[string]any
	if len(raw) > AuditRequestBodyCaptureLimit || json.Unmarshal(raw, &submitted) != nil {
		return nil
	}
	changes := ConfigurationDiff(nil, submitted)
	for index := range changes {
		changes[index].Operation = "submitted"
	}
	return changes
}
