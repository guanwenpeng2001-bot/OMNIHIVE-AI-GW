package service

import "time"

// RFC3339 strings with different offsets do not sort chronologically. Keep a
// total order even for legacy malformed dates so retention selects consistently.
func backupStartedAfter(a, b BackupRecord) bool {
	left, leftErr := time.Parse(time.RFC3339, a.StartedAt)
	right, rightErr := time.Parse(time.RFC3339, b.StartedAt)
	if (leftErr == nil) != (rightErr == nil) {
		return leftErr == nil
	}
	if leftErr == nil && !left.Equal(right) {
		return left.After(right)
	}
	if a.StartedAt != b.StartedAt {
		return a.StartedAt > b.StartedAt
	}
	return a.ID > b.ID
}
