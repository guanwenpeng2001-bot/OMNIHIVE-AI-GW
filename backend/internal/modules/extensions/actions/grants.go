// Package actions reads the host's reviewed, activation-specific grants.
package actions

import (
	"context"
	"database/sql"
)

type Target struct {
	Workspace, Project, Account, CredentialVersion int64
	CredentialReference, Model, Capability         string
}

type Grant struct {
	ID, BindingID, PluginID int64
	Version, ArchiveDigest  string
	Rollout                 int
}

type Reader interface {
	LookupExtensionAction(context.Context, Target) (Grant, error)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Lookup(ctx context.Context, db queryer, t Target) (Grant, error) {
	var g Grant
	err := db.QueryRowContext(ctx, `SELECT id,binding_id,plugin_id,package_version,archive_sha256,rollout_percent
		FROM omnihive_verified_extension_actions WHERE workspace_id=$1 AND project_id=$2 AND account_id=$3
		AND credential_version=$4 AND model=$5 AND action=$6 AND credential_reference=$7`,
		t.Workspace, t.Project, t.Account, t.CredentialVersion, t.Model, t.Capability, t.CredentialReference).
		Scan(&g.ID, &g.BindingID, &g.PluginID, &g.Version, &g.ArchiveDigest, &g.Rollout)
	return g, err
}

// InRollout uses the host's stable account bucket across inventory and dispatch.
func InRollout(account int64, percent int) bool {
	return account > 0 && int(Bucket(account)) < percent
}

func Bucket(account int64) uint64 {
	value := uint64(account)
	value ^= value >> 33
	value *= 0xff51afd7ed558ccd
	value ^= value >> 33
	return value % 100
}
