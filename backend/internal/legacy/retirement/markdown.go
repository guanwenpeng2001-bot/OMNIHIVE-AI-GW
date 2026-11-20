package retirement

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

func number[T int | int64](v *T) string {
	if v == nil {
		return "unknown"
	}
	return fmt.Sprint(*v)
}

func timestamp(t *time.Time) string {
	if t == nil {
		return "unknown"
	}
	return t.UTC().Format(time.RFC3339)
}

func value(v *string) string {
	if v == nil {
		return "unknown"
	}
	return *v
}

func (r Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Compatibility retirement report\n\nDry run. Registration visible: %t. BE-23 checkpoint: %s.\n\n", r.RegisterAvailable, r.Migration.Checkpoint.State)
	for _, domain := range domains {
		fmt.Fprintf(&b, "## %s\n\n| Contract / owner | Legacy surface → replacement | Deployed versions | Supported callers | Published UTC window | Decision | Exit gate |\n| --- | --- | --- | ---: | --- | --- | --- |\n", domain)
		for _, c := range r.Entries {
			if c.Domain != domain {
				continue
			}
			versions := strings.Join(c.DeployedVersions, ", ")
			if versions == "" {
				versions = "unknown"
			}
			fmt.Fprintf(&b, "| `%s` / %s | %s → %s | %s | %s | %s → %s | %s (%s) | %s |\n", c.ID, c.Owner, c.Legacy, c.Replacement, versions, number(c.RemainingCallers), timestamp(c.WindowStart), timestamp(c.WindowEnd), c.State, c.Reason, c.ExitGate)
		}
		b.WriteByte('\n')
	}
	b.WriteString("## Registered evidence\n\n| Contract | Observed at / expires | Observed callers / complete | Census / publication references | Minimum binary / schema | Reconciliation / recovery references |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, c := range r.Entries {
		if c.EvidenceID == nil {
			continue
		}
		fmt.Fprintf(&b, "| `%s` | %s / %s | %s / %t | %s / %s | %s / %s | %s / %s |\n", c.ID, timestamp(c.ObservedAt), timestamp(c.ValidUntil), number(c.ObservedCallers), c.Complete, value(c.EvidenceID), value(c.NoticeID), value(c.MinimumBinary), number(c.MinimumSchema), value(c.ReconciliationID), value(c.RecoveryDrillID))
	}
	b.WriteString("\n## Storage observations\n\n| Source | Rows |\n| --- | ---: |\n")
	for _, k := range slices.Sorted(maps.Keys(r.Counts)) {
		fmt.Fprintf(&b, "| %s | %s |\n", k, number(r.Counts[k]))
	}
	for _, k := range slices.Sorted(maps.Keys(r.Migration.Counts)) {
		fmt.Fprintf(&b, "| %s | %s |\n", k, number(r.Migration.Counts[k].Rows))
	}
	for _, k := range slices.Sorted(maps.Keys(r.Migration.Unmapped)) {
		fmt.Fprintf(&b, "| %s | %s |\n", k, number(r.Migration.Unmapped[k]))
	}
	return b.String()
}

func (p Plan) Markdown() string {
	var b strings.Builder
	b.WriteString("# Domain retirement review order\n\nDry run. Review requires the owner to validate release, reconciliation and recovery evidence.\n\n")
	for i, d := range p.Domains {
		fmt.Fprintf(&b, "%d. **%s** — review %d, hold %d, preserve %d, previously unregistered %d.\n", i+1, d.Domain, len(d.Review), len(d.Hold), len(d.Preserve), len(d.Baseline))
		if len(d.Review) > 0 {
			fmt.Fprintf(&b, "   Review: %s.\n", strings.Join(d.Review, ", "))
		}
	}
	return b.String()
}
