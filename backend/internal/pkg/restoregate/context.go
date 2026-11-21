// Package restoregate coordinates production database access during restore.
package restoregate

import "context"

type snapshotKey struct{}

// SnapshotContext allows the restore owner to read configuration and grants
// during the fenced snapshot callback.
func SnapshotContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, snapshotKey{}, true)
}
func IsSnapshot(ctx context.Context) bool { return ctx.Value(snapshotKey{}) == true }

// LockError identifies the target relation without exposing archive SQL.
type LockError string

func (e LockError) Error() string { return string(e) }

// PostgresError carries only the server SQLSTATE.
type PostgresError string

func (e PostgresError) Error() string { return "PostgreSQL SQLSTATE " + string(e) }
