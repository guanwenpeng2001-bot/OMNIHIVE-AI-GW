//go:build integration

package repository

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	grants "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/extensions/actions"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2/actions"
	"github.com/stretchr/testify/require"
)

func TestExtensionActionApprovalLifecycle(t *testing.T) {
	ctx := t.Context()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	id := func(q string, args ...any) int64 {
		t.Helper()
		var value int64
		require.NoError(t, tx.QueryRowContext(ctx, q, args...).Scan(&value))
		return value
	}
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := tx.ExecContext(ctx, q, args...)
		require.NoError(t, err)
	}
	code := uuid.NewString()
	w := id("INSERT INTO workspaces(name) VALUES($1) RETURNING id", code)
	p := id("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", w)
	user := id("INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", code+"@example.invalid")
	a := id("INSERT INTO accounts(workspace_id,name,platform,type) VALUES($1,$2,'openai','oauth') RETURNING id", w, code)
	exec("INSERT INTO project_resources(kind,resource_id,workspace_id,project_id) VALUES('accounts',$1,$2,$3) ON CONFLICT(kind,resource_id) DO UPDATE SET project_id=excluded.project_id", a, w, p)
	exec("INSERT INTO credential_secrets(account_id,workspace_id,reference,version,ciphertext) VALUES($1,$2,$3,1,'opaque')", a, w, code)
	exec("UPDATE accounts SET credentials=jsonb_build_object('credential_secret_reference',$2::text) WHERE id=$1", a, code)
	archive := []byte("signed fixture")
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	manifest := `{"schema_version":2,"publisher_id":"reviewed","capabilities":[{"id":"provider.responses.v1","platform":"openai","account_type":"oauth"}]}`
	plugin := id(`INSERT INTO sub2api_plugin_installations(plugin_key,name,version,manifest,artifact_data,artifact_path,install_path,binary_path,binary_sha256,signature_status,state)
		VALUES($1,'fixture','1.0.0',$2,$3,'fixture','fixture','fixture',$4,'trusted','enabled') RETURNING id`, code, manifest, archive, strings.Repeat("a", 64))
	bind := func() int64 {
		return id(`INSERT INTO sub2api_plugin_bindings(plugin_id,capability,platform,account_type,enabled)
		VALUES($1,$2,'openai','oauth',true) RETURNING id`, plugin, actions.ProviderResponses)
	}
	b := bind()
	const approve = `INSERT INTO omnihive_extension_action_grants(binding_id,action,package_version,archive_sha256,workspace_id,project_id,account_id,credential_version,model,approved_by,verification_id,verified_at,expires_at)
		VALUES($1,$2,'1.0.0',$3,$4,$5,$6,1,'reviewed-model',$7,$8,now(),now()+interval '1 hour')`
	target := grants.Target{Workspace: w, Project: p, Account: a, CredentialVersion: 1, CredentialReference: code, Model: "reviewed-model", Capability: actions.ProviderResponses}
	missing := func(s grants.Target) {
		t.Helper()
		_, err := grants.Lookup(ctx, tx, s)
		require.ErrorIs(t, err, sql.ErrNoRows)
	}
	missing(target)
	exec("SAVEPOINT invalid_digest")
	_, err = tx.ExecContext(ctx, approve, b, actions.ProviderResponses, strings.Repeat("b", 64), w, p, a, user, uuid.New())
	require.Error(t, err)
	exec("ROLLBACK TO SAVEPOINT invalid_digest")
	exec(approve, b, actions.ProviderResponses, digest, w, p, a, user, uuid.New())
	current, err := grants.Lookup(ctx, tx, target)
	require.NoError(t, err)
	require.Equal(t, b, current.BindingID)
	require.Equal(t, digest, current.ArchiveDigest)
	for _, alter := range []func(*grants.Target){
		func(s *grants.Target) { s.Workspace++ }, func(s *grants.Target) { s.Project++ }, func(s *grants.Target) { s.Account++ },
		func(s *grants.Target) { s.CredentialVersion++ }, func(s *grants.Target) { s.CredentialReference = "foreign" },
		func(s *grants.Target) { s.Model = "other" }, func(s *grants.Target) { s.Capability = actions.TaskImageGenerate },
	} {
		other := target
		alter(&other)
		missing(other)
	}
	exec("SAVEPOINT rotate")
	exec("UPDATE credential_secrets SET version=version+1 WHERE account_id=$1", a)
	missing(target)
	exec("ROLLBACK TO SAVEPOINT rotate")
	exec("UPDATE sub2api_plugin_installations SET version='0.9.0' WHERE id=$1", plugin)
	missing(target)
	exec("UPDATE sub2api_plugin_installations SET version='1.0.0' WHERE id=$1", plugin)
	missing(target)
	exec(approve, b, actions.ProviderResponses, digest, w, p, a, user, uuid.New())
	exec("DELETE FROM sub2api_plugin_bindings WHERE id=$1", b)
	bind()
	missing(target)
}
